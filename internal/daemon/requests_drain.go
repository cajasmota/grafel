package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/requests"
	"github.com/cajasmota/grafel/internal/daemon/sched"
)

// rebuildBackoffBase / rebuildBackoffMax bound the crash-resume re-apply
// window for a KindRebuild that keeps dying mid-flight (epic #5729 issue #29,
// defect b). They mirror the scheduler's own same-ref failure breaker
// (sched.reindexFailBackoff) so a persisted-but-uncompleted rebuild is
// re-applied on a growing schedule instead of back-to-back on every 2s tick.
const (
	rebuildBackoffBase = 30 * time.Second
	rebuildBackoffMax  = 5 * time.Minute
)

// rebuildBackoff returns the backoff for the nth crash-resume attempt (n=1 is
// the first re-apply). Doubles each additional attempt, capped at
// rebuildBackoffMax. It is a package var so tests can shrink the window.
var rebuildBackoff = func(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := rebuildBackoffBase
	for i := 1; i < attempts; i++ {
		d *= 2
		if d >= rebuildBackoffMax {
			return rebuildBackoffMax
		}
	}
	if d > rebuildBackoffMax {
		return rebuildBackoffMax
	}
	return d
}

// requestsDrainInterval is how often the engine's drain loop scans for
// pending serve→engine request files (ADR-0024 PR4, epic #5729). Requests
// are also fire-and-forget from serve's perspective (Service.Index returns
// as soon as the file is written), so this interval bounds how long a
// split-mode reindex trigger can sit before the engine notices it — kept
// short since the scan itself is a cheap directory glob, not a full
// filesystem walk.
const requestsDrainInterval = 2 * time.Second

// maxRebuildAttempts bounds how many times a single KindRebuild request may be
// (re-)applied across drains/engine-restarts before it is dead-lettered
// (epic #5729, defect d). A group rebuild is multi-minute and, if the engine
// is killed mid-apply (memlimit/reaper/crash/RPC EOF) before the ack is
// written, the request is never removed — so a naive drain re-applies it on
// EVERY 2s tick and EVERY engine restart, an unbounded full-group-rebuild
// loop. Bounding attempts (persisted durably in the request file, so the count
// survives a restart) turns at-least-once-forever into at-most-N with a
// terminating dead-letter.
const maxRebuildAttempts = 3

// engineGroupRebuildGuard serialises KindRebuild applications per group on the
// ENGINE side — where rebuildFn actually executes (epic #5729, defect c).
//
// Service.groupRebuildMu (service.go) is the analogous capacity-1 guard, but in
// split mode it guards nothing: Service.Rebuild returns after merely WRITING
// the request file, never reaching that guard, and the engine drain calls
// rebuildFn directly here with no such serialisation. This guard puts the
// single-flight on the live path, so two concurrent KindRebuild applications
// for the SAME group run one rebuildFn at a time (the second waits), while
// DIFFERENT groups still proceed concurrently.
var engineGroupRebuildGuard groupRebuildGuard

// engineRebuildInFlight counts the group rebuilds this ENGINE process is
// running right now (#6014). One entry per rebuildWorker.submit goroutine, i.e.
// per group, so it is simultaneously "rebuilds in flight" and "groups actively
// rebuilding" — the engine guard is per-group and admits one at a time.
//
// WHY A COUNTER AT ALL. In split mode (the default, ADR-0024) the rebuild runs
// HERE, in the engine, while `grafel status` is answered by SERVE. Serve's own
// Service.rebuildInFlight can never be non-zero in that mode — its increment is
// below the split-mode early return — so serve has literally no local
// information from which to report a running rebuild. This counter is the real,
// work-derived number the heartbeat publishes onto the engine-liveness sidecar
// for serve to read. Serve never invents a value of its own.
var engineRebuildInFlight atomic.Int64

// EngineRebuildInFlightCount reports how many group rebuilds this process is
// running. Read by the engine-liveness heartbeat (statuswriter.go); meaningful
// only in the process that actually applies rebuilds (the engine, or a monolith
// draining its own queue).
func EngineRebuildInFlightCount() int { return int(engineRebuildInFlight.Load()) }

// groupRebuildGuard is a per-group capacity-1 semaphore keyed by group name.
type groupRebuildGuard struct {
	sems sync.Map // group name -> chan struct{} (cap 1)
}

// acquire blocks until this group's slot is free and returns a release func
// that must be called (defer) once the guarded rebuild completes.
func (g *groupRebuildGuard) acquire(group string) (release func()) {
	v, _ := g.sems.LoadOrStore(group, make(chan struct{}, 1))
	ch := v.(chan struct{})
	ch <- struct{}{}
	return func() { <-ch }
}

// discoverRequestsDirs finds every `requests/` directory under the store
// layout rooted at root (either GRAFEL_DAEMON_ROOT/state or
// $GRAFEL_HOME/store — see StateDirForRepo/repoBaseDir). The engine has no
// a priori registry of which repos have dropped a request, so discovery is
// a glob over the known `<slug-or-hash>/refs/<ref>/requests` shape rather
// than a per-repo lookup.
func discoverRequestsDirs(root string) ([]string, error) {
	if root == "" {
		return nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", "refs", "*", "requests"))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		info, statErr := os.Stat(m)
		if statErr != nil || !info.IsDir() {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// requestsRoot resolves the store root the engine should glob for
// requests/ directories: GRAFEL_DAEMON_ROOT/state when the isolated-daemon
// env var is set (tests, parallel agents), else StoreDir() — mirroring
// repoBaseDir's own resolution (state_path.go) so discovery always agrees
// with where Service.Index (via requestsDirForRepo) actually wrote.
func requestsRoot() string {
	if root := os.Getenv(EnvRoot); root != "" {
		return filepath.Join(root, "state")
	}
	return StoreDir()
}

// requestsDrainer owns a single drain pass's collaborators plus the
// background rebuild worker whose state must PERSIST across ticks (defect:
// wizard-queue starvation, epic #5729). A single instance is created by
// startRequestsDrainLoop and reused for every tick, so the worker's
// single-in-flight-per-group bookkeeping and crash-resume backoff survive
// between passes.
type requestsDrainer struct {
	scheduler *sched.Scheduler
	rebuildFn RebuildFunc
	logger    *slog.Logger
	rebuilds  *rebuildWorker
}

// newRequestsDrainer wires a drainer over the given collaborators. rebuildFn
// and scheduler may be nil (tests exercise one path at a time); logger may be
// nil.
func newRequestsDrainer(scheduler *sched.Scheduler, rebuildFn RebuildFunc, logger *slog.Logger) *requestsDrainer {
	return &requestsDrainer{
		scheduler: scheduler,
		rebuildFn: rebuildFn,
		logger:    logger,
		rebuilds:  newRebuildWorker(rebuildFn, logger),
	}
}

// drainOnce performs a single pass: discover every requests/ dir under root,
// list its pending records, and route each by kind. The CRITICAL property
// (Fix 1, epic #5729) is that the expensive, multi-minute KindRebuild is
// NEVER applied inline on this goroutine — it is handed to a separate
// single-in-flight background worker that drainOnce does NOT await. So cheap
// KindReindex requests for OTHER groups/repos keep draining every tick even
// while a group rebuild runs for minutes in the background, instead of sitting
// at "Queued" until it finishes.
//
// KindReindex (PR4) is applied inline (a cheap scheduler.Enqueue). KindRebuild
// (PR6 prerequisite, epic #5729) is dispatched to the background worker. Other
// defined-but-unproduced kinds (KindSubmitRepair / KindDocgenApply /
// KindEnrichmentEnqueue) keep the plain inline at-least-once ApplyAndAck — see
// internal/daemon/requests's Kind doc comments for why they are safe.
func (d *requestsDrainer) drainOnce(root string) error {
	dirs, err := discoverRequestsDirs(root)
	if err != nil {
		return fmt.Errorf("requests: discover dirs: %w", err)
	}
	for _, dir := range dirs {
		recs, err := requests.ListPending(dir)
		if err != nil {
			if d.logger != nil {
				d.logger.Warn("requests: list pending failed (skipping dir)", "dir", dir, "err", err)
			}
			continue
		}
		for _, rec := range recs {
			rec := rec
			if rec.Kind == requests.KindRebuild {
				// Hand off to the background worker (non-blocking). It coalesces
				// every pending same-group rebuild in this dir and applies at
				// most one at a time, keyed by group — the drain loop keeps
				// scanning/enqueuing while it runs. Submitting once per dir is
				// enough (the worker re-lists the dir itself); a second submit
				// for an already-active group is a no-op.
				d.rebuilds.submit(root, dir, rebuildGroup(rec))
				continue
			}
			apply := func(r requests.Record) error {
				return applyRequest(d.scheduler, d.rebuildFn, r)
			}
			applyErr := requests.ApplyAndAck(dir, rec, apply)
			if applyErr != nil && d.logger != nil {
				d.logger.Warn("requests: apply/ack failed", "dir", dir, "id", rec.ID, "kind", rec.Kind, "err", applyErr)
			}
		}
	}
	return nil
}

// drainRequestsOnce is the one-shot, SYNCHRONOUS drain used by tests and any
// caller that wants a single pass to complete (including any background
// rebuild it dispatches) before returning. The periodic engine loop uses a
// persistent requestsDrainer directly (see startRequestsDrainLoop) so its
// worker state survives across ticks and rebuilds do NOT block the loop.
//
// rebuildFn is the engine's in-process rebuild entrypoint (cfg.Rebuild — the
// SAME RebuildFunc Service.Rebuild calls directly in monolith/engine mode);
// may be nil. logger may be nil.
func drainRequestsOnce(root string, scheduler *sched.Scheduler, rebuildFn RebuildFunc, logger *slog.Logger) error {
	d := newRequestsDrainer(scheduler, rebuildFn, logger)
	err := d.drainOnce(root)
	d.rebuilds.waitIdle()
	return err
}

// rebuildGroup extracts the target group from a KindRebuild record's payload,
// used only as the worker's single-flight/backoff map key. A payload we can't
// decode yields "" — the worker still lists+applies the dir's rebuilds (a bad
// payload error-acks through applyRequest), it just shares the zero key.
func rebuildGroup(rec requests.Record) string {
	var args proto.RebuildArgs
	if err := json.Unmarshal(rec.Payload, &args); err != nil {
		return ""
	}
	return args.Group
}

// rebuildWorker applies KindRebuild requests OFF the drain goroutine, one at a
// time per group, so a multi-minute rebuild never starves the cheap-request
// drain (Fix 1). It also coalesces duplicate pending same-group rebuilds
// (Fix 2a) and spaces crash-resume re-applies by a growing backoff (Fix 2b).
type rebuildWorker struct {
	rebuildFn RebuildFunc
	logger    *slog.Logger

	mu          sync.Mutex
	active      map[string]struct{}  // groups with a running goroutine
	nextAttempt map[string]time.Time // group -> earliest next apply (crash backoff)
	wg          sync.WaitGroup       // tracks in-flight goroutines (waitIdle)

	// now / backoff are injectable for deterministic tests.
	now     func() time.Time
	backoff func(attempts int) time.Duration
}

func newRebuildWorker(fn RebuildFunc, logger *slog.Logger) *rebuildWorker {
	return &rebuildWorker{
		rebuildFn:   fn,
		logger:      logger,
		active:      map[string]struct{}{},
		nextAttempt: map[string]time.Time{},
		now:         time.Now,
		backoff:     rebuildBackoff,
	}
}

// submit ensures a single background goroutine is draining group's pending
// rebuilds in dir. It is non-blocking and a no-op when one is already active
// for the group (single-in-flight per group). root is where dead-letters are
// surfaced.
func (w *rebuildWorker) submit(root, dir, group string) {
	w.mu.Lock()
	if _, running := w.active[group]; running {
		w.mu.Unlock()
		return
	}
	w.active[group] = struct{}{}
	w.wg.Add(1)
	w.mu.Unlock()

	// #6014: the process-global mirror of w.active, published by the engine's
	// liveness heartbeat so a split-mode serve can report a running rebuild.
	// Incremented here — on the claim, in lockstep with w.active — rather than
	// inside runGroup, so the count can never disagree with the guard.
	engineRebuildInFlight.Add(1)

	go func() {
		defer func() {
			engineRebuildInFlight.Add(-1)
			w.mu.Lock()
			delete(w.active, group)
			w.mu.Unlock()
			w.wg.Done()
		}()
		w.runGroup(root, dir, group)
	}()
}

// waitIdle blocks until no background rebuild goroutine is running. Used by
// the synchronous drainRequestsOnce helper and tests.
func (w *rebuildWorker) waitIdle() { w.wg.Wait() }

// rebuildBucket is one set of pending KindRebuild records that are SEMANTICALLY
// identical (same Group+Slug+Wipe+Incremental — see semanticRebuildKey) and can
// therefore be coalesced into a single rebuild. survivor is the one that
// actually runs; duplicates are the coalesced-away records whose completion
// tokens are only satisfied once the survivor truly finishes.
type rebuildBucket struct {
	survivor   requests.Record
	duplicates []requests.Record
}

// runGroup partitions dir's pending rebuilds into semantic buckets and applies
// each bucket's survivor. Divergent-payload rebuilds (different slug/wipe/
// incremental) land in DIFFERENT buckets and each run — coalescing never
// collapses across differing rebuild work (review finding #1). Buckets run
// sequentially in this single per-group goroutine, so the engine-side
// single-flight is preserved and the drain loop is never blocked.
func (w *rebuildWorker) runGroup(root, dir, group string) {
	recs, err := requests.ListPending(dir)
	if err != nil {
		if w.logger != nil {
			w.logger.Warn("requests: rebuild list pending failed (skipping dir)", "dir", dir, "err", err)
		}
		return
	}
	for _, b := range partitionRebuilds(recs) {
		w.applyBucket(root, dir, group, b)
	}
}

// applyBucket applies one semantic bucket's survivor and, ONLY on the
// survivor's real completion (or dead-letter), satisfies the coalesced-away
// duplicates' completion tokens (review finding #2). On a mid-flight crash the
// duplicates are left pending so every waiting token stays true and the
// survivor is retried under backoff.
func (w *rebuildWorker) applyBucket(root, dir, group string, b rebuildBucket) {
	rec := b.survivor

	// Crash-resume backoff gate (Fix 2b): a survivor that already carries a
	// persisted attempt claim (Attempts>=1) but never completed is being
	// re-applied after a crash. Space the re-applies by a growing window keyed
	// on the attempt count instead of re-running on every 2s tick. The gate is
	// per-group; a bucket with a fresh (Attempts==0) survivor is never gated.
	if rec.Attempts >= 1 {
		w.mu.Lock()
		next, gated := w.nextAttempt[group]
		if gated && w.now().Before(next) {
			w.mu.Unlock()
			return // still backing off; a later tick re-dispatches
		}
		w.mu.Unlock()
	}

	apply := func(r requests.Record) error {
		// applyRequest holds engineGroupRebuildGuard for this group, so the
		// single-flight invariant is preserved and we must NOT re-acquire it
		// here. scheduler is irrelevant for KindRebuild.
		return applyRequest(nil, w.rebuildFn, r)
	}
	// keepAck (#5790): a WaitForCompletion rebuild's terminal ack is left on
	// disk so the serve-side waiter (awaitRebuildCompletion) can read its real
	// outcome — OK vs error/dead-letter — instead of misreading request-absence
	// as success. Fire-and-forget rebuilds keep the leak-free GC path.
	keepAck := rebuildWaitsForCompletion(rec)
	outcome, applyErr := requests.ApplyAndAckBounded(dir, rec, maxRebuildAttempts, keepAck, apply)

	switch {
	case outcome == requests.OutcomeCrashed:
		// Schedule the next re-apply behind a growing backoff (keyed on the
		// now-persisted higher attempt count) so the following ticks don't
		// hammer the doomed rebuild back-to-back. Leave the duplicates PENDING:
		// their tokens must not flip until the survivor really completes.
		w.mu.Lock()
		w.nextAttempt[group] = w.now().Add(w.backoff(rec.Attempts + 1))
		w.mu.Unlock()
		if w.logger != nil {
			w.logger.Warn("requests: rebuild apply crashed mid-flight; will retry (bounded, backing off)", "dir", dir, "id", rec.ID, "attempt", rec.Attempts+1, "max", maxRebuildAttempts)
		}
	case outcome == requests.OutcomeDeadLettered:
		w.clearBackoff(group)
		w.ackCoalesced(dir, b.duplicates, requests.StatusError,
			fmt.Sprintf("group rebuild dead-lettered after %d attempts without completing", maxRebuildAttempts))
		if err := recordDeadLetter(root, deadLetterFromRec(group, rec)); err != nil && w.logger != nil {
			w.logger.Warn("requests: surface dead-letter failed", "dir", dir, "id", rec.ID, "err", err)
		}
		if w.logger != nil {
			w.logger.Error("requests: rebuild dead-lettered after max attempts; giving up (not re-running)", "dir", dir, "id", rec.ID, "max", maxRebuildAttempts)
		}
	default:
		w.clearBackoff(group)
		status, errMsg := requests.StatusOK, ""
		if applyErr != nil {
			status, errMsg = requests.StatusError, applyErr.Error()
		}
		w.ackCoalesced(dir, b.duplicates, status, errMsg)
		if applyErr != nil && w.logger != nil {
			w.logger.Warn("requests: rebuild apply/ack failed", "dir", dir, "id", rec.ID, "attempts", rec.Attempts, "err", applyErr)
		}
	}
}

// ackCoalesced satisfies each coalesced-away duplicate's completion token AFTER
// the survivor has really finished, propagating the survivor's TERMINAL status
// (ok / error / dead-letter) to every duplicate's ack — so a WaitForCompletion
// waiter behind a coalesced-away duplicate learns the SAME real outcome as one
// behind the survivor, never a false success (#5790). It writes each ack,
// deletes the duplicate's request (so RebuildRequestPending flips false only
// now, never early — review finding #2), and GCs the ack UNLESS that duplicate
// was itself a WaitForCompletion request, in which case the ack is kept as the
// durable outcome for its waiter to read+consume (mirrors ApplyAndAckBounded's
// keepAck).
func (w *rebuildWorker) ackCoalesced(dir string, dups []requests.Record, status requests.Status, errMsg string) {
	for _, d := range dups {
		if err := requests.WriteAck(dir, d.ID, requests.Ack{Status: status, Err: errMsg}); err != nil {
			if w.logger != nil {
				w.logger.Warn("requests: coalesce-ack of duplicate rebuild failed", "dir", dir, "id", d.ID, "err", err)
			}
			continue
		}
		if err := requests.Delete(dir, d.ID); err != nil && w.logger != nil {
			w.logger.Warn("requests: coalesce-delete of duplicate rebuild failed", "dir", dir, "id", d.ID, "err", err)
		}
		if !rebuildWaitsForCompletion(d) {
			_ = requests.DeleteAck(dir, d.ID)
		}
	}
}

// rebuildWaitsForCompletion reports whether a KindRebuild record's payload asked
// the producer to block for real completion (proto.RebuildArgs.WaitForCompletion,
// #5790). Such a request's terminal ack must be KEPT so the serve-side waiter can
// read the true outcome; an undecodable payload defaults to false (fire-and-forget).
func rebuildWaitsForCompletion(rec requests.Record) bool {
	var a proto.RebuildArgs
	if err := json.Unmarshal(rec.Payload, &a); err != nil {
		return false
	}
	return a.WaitForCompletion
}

// clearBackoff drops any crash-resume backoff scheduled for group (called once
// a rebuild either completes or is dead-lettered).
func (w *rebuildWorker) clearBackoff(group string) {
	w.mu.Lock()
	delete(w.nextAttempt, group)
	w.mu.Unlock()
}

// semanticRebuildKey returns a stable key over the SEMANTIC fields of a rebuild
// payload — Group+Slug+Wipe+Incremental — the fields that determine what work
// the rebuild performs. It deliberately EXCLUDES ProgressToken (a per-caller
// wizard completion handle, not a semantic difference) and bookkeeping
// (Attempts/ID/CreatedAt). Two requests share a key iff collapsing them loses
// no rebuild work (review finding #1). A payload that cannot be decoded gets a
// unique key so it is never coalesced with anything.
func semanticRebuildKey(rec requests.Record) string {
	var a proto.RebuildArgs
	if err := json.Unmarshal(rec.Payload, &a); err != nil {
		return "undecodable:" + rec.ID
	}
	return fmt.Sprintf("g=%s\x00s=%s\x00w=%t\x00i=%t", a.Group, a.Slug, a.Wipe, a.Incremental)
}

// partitionRebuilds groups recs' KindRebuild records into semantic buckets
// (first-seen order for determinism). Within a bucket the survivor is the
// record with the HIGHEST Attempts (tie-broken by newest CreatedAt), so a fresh
// duplicate enqueue can never reset a crash-looping record's attempt budget or
// skip its backoff gate (review finding #3); the remaining records become the
// bucket's coalesced-away duplicates.
func partitionRebuilds(recs []requests.Record) []rebuildBucket {
	order := make([]string, 0)
	byKey := make(map[string][]requests.Record)
	for _, r := range recs {
		if r.Kind != requests.KindRebuild {
			continue
		}
		k := semanticRebuildKey(r)
		if _, seen := byKey[k]; !seen {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], r)
	}
	buckets := make([]rebuildBucket, 0, len(order))
	for _, k := range order {
		members := byKey[k]
		survivorIdx := 0
		for i, r := range members {
			s := members[survivorIdx]
			if r.Attempts > s.Attempts || (r.Attempts == s.Attempts && r.CreatedAt.After(s.CreatedAt)) {
				survivorIdx = i
			}
		}
		dups := make([]requests.Record, 0, len(members)-1)
		for i, r := range members {
			if i != survivorIdx {
				dups = append(dups, r)
			}
		}
		buckets = append(buckets, rebuildBucket{survivor: members[survivorIdx], duplicates: dups})
	}
	return buckets
}

// applyRequest dispatches a single drained Record to the same in-process
// call the monolith/engine makes when it owns the scheduler/rebuild
// entrypoint directly. An unknown/future kind is reported as an error in the
// ack rather than silently dropped, so the producer can observe the
// mismatch.
func applyRequest(scheduler *sched.Scheduler, rebuildFn RebuildFunc, rec requests.Record) error {
	switch rec.Kind {
	case requests.KindReindex:
		if scheduler == nil {
			return fmt.Errorf("requests: reindex request for %s but no scheduler is attached", rec.RepoPath)
		}
		scheduler.Enqueue(rec.RepoPath)
		return nil
	case requests.KindRebuild:
		if rebuildFn == nil {
			return fmt.Errorf("requests: rebuild request %s but no rebuild entrypoint is attached", rec.ID)
		}
		var args proto.RebuildArgs
		if err := json.Unmarshal(rec.Payload, &args); err != nil {
			return fmt.Errorf("requests: decode rebuild payload for %s: %w", rec.ID, err)
		}
		// Engine-side single-flight (defect c, epic #5729): serialise rebuilds
		// of the SAME group so at most one rebuildFn runs at a time on the live
		// (engine drain) path — the split-mode analogue of Service.groupRebuildMu,
		// which guards nothing here because Service.Rebuild returns before
		// reaching it. Different groups are keyed separately and still overlap.
		// The guard wraps ONLY the rebuild call and is released even if
		// rebuildFn panics (defer), so a crash cannot leak the slot.
		release := engineGroupRebuildGuard.acquire(args.Group)
		defer release()
		// Bounded-idempotency (defect d) is handled one layer up by
		// ApplyAndAckBounded, which persists an attempt claim before this runs;
		// a crash mid-rebuild is retried at most maxRebuildAttempts times across
		// engine restarts, then dead-lettered — never an unbounded redrain loop.
		_, _, err := rebuildFn(args)
		return err
	case requests.KindCancelGroup:
		// A group was deleted; cancel its in-flight enrichment + rebuild in THIS
		// (engine) process. Both are best-effort and idempotent: the scheduler
		// cancels its group-algo/link/reindex passes for the group, and the
		// package-level registry cancels an in-flight group rebuild. The group
		// key travels in RepoPath (see requests.KindCancelGroup). Never errors —
		// a cancel for a group with nothing in flight is a no-op.
		group := rec.RepoPath
		if scheduler != nil {
			scheduler.CancelGroup(group)
		}
		CancelGroupRebuild(group)
		return nil
	default:
		return fmt.Errorf("requests: unsupported kind %q", rec.Kind)
	}
}

// startRequestsDrainLoop starts the periodic drain goroutine and returns a
// stop func (ep.add-compatible) that halts it. Only meaningful when a
// scheduler is attached and SplitModeEnabled() — see the call site in
// startEnginePlane (engineplane.go), which gates on both. rebuildFn is
// threaded through to applyRequest for KindRebuild dispatch (may be nil).
func startRequestsDrainLoop(scheduler *sched.Scheduler, rebuildFn RebuildFunc, logger *slog.Logger) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})
	// One persistent drainer for the lifetime of the loop: its background
	// rebuild worker's single-in-flight-per-group + crash-resume-backoff state
	// must survive across ticks. Each tick dispatches any rebuild to that
	// worker WITHOUT awaiting it (drainOnce, not the synchronous
	// drainRequestsOnce helper), so cheap reindex requests keep draining while
	// a multi-minute rebuild runs in the background.
	drainer := newRequestsDrainer(scheduler, rebuildFn, logger)
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(requestsDrainInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := drainer.drainOnce(requestsRoot()); err != nil && logger != nil {
					logger.Warn("requests: drain pass failed", "err", err)
				}
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
		drainer.rebuilds.waitIdle()
	}
}
