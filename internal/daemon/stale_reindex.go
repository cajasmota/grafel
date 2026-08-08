package daemon

// stale_reindex.go — #5907 FIX 2: the ACTION arm for stale-on-disk-format
// detection. Detection (graph.ReindexRequiredReason, surfaced on the status
// plane by statuswriter.go) has, until now, ZERO action-consumers: a repo
// whose graph.fb was written by an older grafel build than this binary
// supports sits idle forever, silently serving nothing, until a human runs
// `grafel index`. This closes that silent stall by having the engine
// auto-enqueue a reindex through the SAME requests→drain→scheduler plumbing
// Service.Index already uses — the daemon-side equivalent of the CLI's
// FormatVersionError → full-reindex fallback.
//
// The load-bearing property is the LOOP-GUARD (the #5891-class hazard): the
// engine's status writer recomputes ReindexRequired on EVERY heartbeat, so a
// naive "if required { enqueue }" would fire a reindex request storm — one per
// heartbeat for the whole time the stale graph is on disk, including the entire
// duration of the reindex it already triggered. staleReindexGuard makes the
// enqueue fire AT MOST ONCE per (repo, stale generation):
//
//   - The fingerprint is (graph.fb mtime | reindex reason). The mtime advances
//     ONLY when graph.fb is rewritten — i.e. a fresh index actually lands — so
//     every heartbeat observing the same stale file computes the SAME
//     fingerprint and is deduped. While the triggered reindex is in flight the
//     stale file is untouched (the gen `current` pointer is flipped only when
//     the new graph is complete), so the fingerprint is stable and NO second
//     request is written.
//   - When the reindex completes and the graph is current, ReindexRequired
//     flips false; maybeEnqueue then FORGETS the repo's fingerprint, so the
//     guard self-clears and a genuinely NEW stale generation later (distinct
//     mtime) can fire exactly one fresh request — but a current repo never does.

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/requests"
	"github.com/cajasmota/grafel/internal/indexstate"
)

// #6167 — migration batching policy.
//
// The loop guard above bounds requests PER REPO. Nothing bounded them ACROSS
// repos, and statusWriter.writeAll (statuswriter.go:521) walks every registered
// repo on every 5s heartbeat — so the first heartbeat after an fbversion bump
// enqueued one reindex per stale repo, all at once. A user with 140 registered
// repos measured queued=133, seven "heavy write stage starved by index churn"
// escalations and three shutdown-watchdog force-exits over 18 hours.
//
// The scheduler already caps CONCURRENCY (Config.Workers defaults to 2, plus
// RSS-budget admission — sched/scheduler.go:158, :1508). What was unbounded was
// the BACKLOG, and backlog is what hurts here for two reasons:
//
//  1. sched/stagegate.go:489 refuses the heavy-write token for as long as ANY
//     index job is in flight. A 133-deep queue means that condition is
//     continuously true, so group-algo/links only ever run via the drain-barrier
//     escalation at stagegate.go:470 — the seven WARNs in the capture.
//  2. requests.Write persists to disk. A 133-deep durable queue survives the
//     restart the user performs when the dashboard stops responding, so the
//     stampede replays on every restart.
//
// So the throttle belongs HERE, at request production, not at the drain: it
// keeps the on-disk backlog bounded and it creates the idle windows the stage
// gate needs.
//
// The outstanding set is DURABLE, in the guard's own per-repo marker
// (stale_reindex_state.go) rather than in the request queue. The request queue
// cannot serve this purpose: the drain acks and removes a request within ~2s
// while the index it dispatched runs 42s-4m53s, and the user restarts precisely
// because the daemon is busy indexing — so a queue-derived reconcile saw
// nothing outstanding and re-admitted everything (measured: 12 duplicate full
// reindexes across 6 mid-index restarts). The marker also carries the attempt
// count and the give-up flag, so failure history survives a restart loop;
// without that, a restart-prone environment reset `attempts` every time and a
// genuinely broken repo was never marked failed and never surfaced.
//
// Forfeit decisions are PER-REPO and keyed off indexstate.RepoStates(), the
// scheduler's own progress signal — but the load-bearing distinction is not
// active/inactive, it is DISPATCHED/NEVER-DISPATCHED:
//
//	!active[repo] conflates "stalled" with "never enqueued",
//	and only the first deserves an attempt penalty.
//
// A repo the scheduler is working on is protected to the hard ceiling. A repo
// we have SEEN active that has since stopped is genuinely stalled: it forfeits
// at the stalled grace and is charged an attempt, ending in a reported give-up.
// A repo we have NEVER seen active was never dispatched — orphaned by a restart
// (the drain acked and removed its request ~2s after admission, and the index
// died with the daemon) — and is abandoned with NO penalty and NO failure
// record. Charging those cost a healthy repo two attempts per restart against a
// cap of three, so two restarts of an unresponsive daemon — the exact behaviour
// this whole issue documents — marked it permanently unmigratable.
//
// #6175 removed the OTHER population from this branch. A repo the scheduler
// declines by design (EnqueueRefCommit drops linked worktrees of watched
// primaries, sched/scheduler.go:976) also never became active, so it landed here
// too — abandoned, stood aside for undispatchedBackoff, offered again, forever,
// with `grafel status` counting it as in-progress the whole time. Abandonment
// cannot tell those two apart from the outside; the ENQUEUE GATE can, and is
// asked directly (declinedFn), so a declined repo is now never admitted and is
// reported under its own state. What remains on this branch is the genuinely
// transient case, which is exactly what deserves a retry.
//
// Recovered slots are also RE-ENQUEUED (idempotently, only when nothing is
// already queued): restoring the guard's slot without restoring the scheduler's
// queue leaves the repo orphaned forever.
//
// An EMPTY indexstate snapshot means "unknown" and never triggers a forfeit,
// but note this is a narrow protection: once anything has been indexed the
// snapshot is permanently non-empty (publishRepoStatesLocked unions in
// indexedRepos), so the observedActive split above does the real work.
//
// Two limits this mechanism does NOT address, named here so they are not
// mistaken for solved:
//
//   - The idle window is idle only with respect to GUARD-ADMITTED reindexes.
//     The guard has no visibility into watcher-driven incremental indexes, so on
//     a store with 140 watched repos ordinary file-change churn can keep
//     sched.inflight > 0 straight through the cooldown and the stage gate still
//     never yields.
//   - While a heavy stage HOLDS the token (group-algo runs ~40min) nothing
//     completes, so slots do not free and the effective migration rate degrades
//     to roughly one batch per stage hold.
const (
	// defaultStaleReindexBatchSize is how many auto-reindexes may be
	// outstanding at once. Sized to the scheduler's own default worker count
	// (2) so the migration keeps the pipeline fed without ever queueing behind
	// itself.
	defaultStaleReindexBatchSize = 2

	// defaultStaleReindexCooldown is the enforced quiet period after a batch
	// fully drains, before the next is admitted. This is the idle window in
	// which a deferred heavy write stage can acquire the stage-gate token
	// cleanly instead of waiting out the drain barrier. It only needs to
	// exceed the gate's retry interval, not the stage's runtime — once the
	// stage HOLDS the token, stageBusyLocked keeps index dispatch out for as
	// long as it runs.
	//
	// Sized against stageGateRetryDefault = 15s (sched/stagegate.go:73). Review
	// finding 5: at the original 30s a starved stage got exactly two retry
	// attempts per window, so a single missed tick lost the window entirely.
	// 45s guarantees three, which is the margin one lost tick needs. The cost
	// is 15s per batch — ~17 minutes across a 140-repo migration that already
	// takes hours, which is the right trade for not silently losing the window
	// this whole mechanism exists to create.
	defaultStaleReindexCooldown = 45 * time.Second

	// defaultStaleReindexSlotTTL is the HARD ceiling, applied to a repo that
	// the scheduler says it IS working on. Such a repo is making progress by
	// every signal available, so it is protected for a long time — round 3
	// MEDIUM 3: the previous 5m deadline left 7 seconds of margin against the
	// documented 4m53s worst case, on a machine that is more loaded during a
	// migration than during the capture. The ceiling exists only so a repo
	// wedged INSIDE the scheduler cannot hold a slot forever.
	defaultStaleReindexSlotTTL = 30 * time.Minute

	// defaultStaleReindexStalledGrace is the forfeit deadline for a repo that
	// the scheduler is NOT working on. Round-3 blocker 2: round 2 keyed the
	// short deadline off "some of this batch has drained", which is never true
	// when BOTH slot-holders are wedged — so the hard TTL governed again and
	// the measured stall was 15m45s, marginally worse than before the fix. The
	// decision has to be PER-REPO, and it needs a real progress signal rather
	// than an inference from the other slots.
	//
	// indexstate.RepoStates() is that signal: a repo that is queued, indexing
	// or dirty is demonstrably being worked on. A repo that appears NOWHERE in
	// a non-empty snapshot is not — its request was dropped, its dispatch
	// failed, or it was unregistered — and 90s is ample time for an admitted
	// repo to show up there (the drain runs every 2s).
	defaultStaleReindexStalledGrace = 90 * time.Second

	// staleReindexMaxAttempts bounds how many times one repo may be admitted
	// before the migration gives up on it. Review finding 2: forfeiting a slot
	// while leaving the repo in `seen` was a SILENT PERMANENT DROP — the repo
	// served a stale graph forever and nothing said so. Now a forfeit clears
	// `seen` so the repo gets another turn, and only after this many failures
	// is it marked failed and REPORTED.
	staleReindexMaxAttempts = 3

	// defaultStaleReindexRetryBackoff is how long a forfeited repo stands aside
	// before it is eligible for another slot. Without it, bounded retry re-took
	// the slot on the very next heartbeat — the wedged repo is offered first in
	// registry order every time — so retrying it starved every repo behind it
	// for maxAttempts * the forfeit deadline. Standing aside lets the rest of
	// the migration overtake it, which is the whole point of not dropping it.
	//
	// Round 3 blocker 2: the backoff is JITTERED per repo. A uniform backoff
	// made two wedged repos forfeit together, wait together and re-pair on
	// every retry, reproducing the stall each cycle instead of dissolving it.
	defaultStaleReindexRetryBackoff = 10 * time.Minute
	// staleReindexRetryJitter is the width of the per-repo backoff spread.
	staleReindexRetryJitter = 5 * time.Minute

	// defaultStaleReindexUndispatchedBackoff is how long a repo that was
	// admitted but never picked up stands aside before the guard offers it
	// again. Round-4: such a repo must NOT be treated as a failure — it was
	// most likely orphaned by a restart. Retrying occasionally costs one request
	// write; marking it Failed would tell the user to fix a repo that is not
	// broken.
	//
	// #6175: the repos for which retrying could NEVER succeed — the ones the
	// enqueue gate drops by design — no longer reach this path at all, so this
	// backoff now applies only where another turn can actually help.
	defaultStaleReindexUndispatchedBackoff = 30 * time.Minute

	// EnvStaleReindexBatch overrides defaultStaleReindexBatchSize. Escape
	// hatch for an operator who wants a large store to migrate faster (or
	// slower) than the default. Values <= 0 fall back to the default.
	EnvStaleReindexBatch = "GRAFEL_STALE_REINDEX_BATCH"
)

// staleReindexBatchSize resolves the batch size, honouring EnvStaleReindexBatch.
func staleReindexBatchSize() int {
	if v := os.Getenv(EnvStaleReindexBatch); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultStaleReindexBatchSize
}

// staleReindexGuard tracks, per repo path, the stale-format fingerprint we have
// already auto-enqueued a reindex for, so we never enqueue twice for the same
// stale generation. Concurrency-safe: writeRepoStatusFile runs on the single
// statusWriter goroutine today, but the guard is package-global and defended by
// a mutex so a future second caller (e.g. a startup reconcile pass) cannot race
// it into a double-enqueue.
type staleReindexGuard struct {
	mu sync.Mutex
	// seen maps repoPath -> the stale fingerprint a reindex was last enqueued
	// for. Absence means "no outstanding auto-reindex for a stale generation".
	seen map[string]string

	// --- #6167 migration batching (see the "at scale" note in the file doc) ---

	// inflight maps repoPath -> the time its auto-reindex was admitted, for
	// every repo this guard has enqueued and not yet observed going current.
	// Its SIZE is the outstanding-migration count the batch bounds.
	inflight map[string]time.Time
	// admittedInBatch counts admissions since the current batch opened. The
	// batch closes at batchSize and reopens only after inflight drains AND
	// cooldown elapses — that gap is the idle window the stage gate needs.
	admittedInBatch int
	// batchDrainedAt is when inflight last became empty with a full batch
	// behind it; the cooldown is measured from here.
	batchDrainedAt time.Time

	// attempts counts how many times each repo has had an auto-reindex
	// admitted that did NOT end in a current graph (i.e. the slot was
	// forfeited rather than released). Bounded retry: a transient failure gets
	// another turn in a later batch instead of being dropped on the first.
	attempts map[string]int
	// failed records repos that exhausted staleReindexMaxAttempts. They are
	// never admitted again — and, crucially, are reported (statusfile
	// ReindexMigrationFailed → `grafel status`) rather than silently dropped.
	failed map[string]bool
	// retryAfter holds, for each forfeited repo, the earliest time it may take
	// another slot. Keeps a retry from immediately re-taking the slot it just
	// lost and starving the repos behind it.
	retryAfter map[string]time.Time
	// observedActive records repos the scheduler has been seen working on since
	// they were admitted. It is the whole basis of the round-4 distinction:
	// !active[repo] means "stalled" only if we ever saw it active, and
	// "never enqueued" otherwise — and only the first deserves a penalty.
	//
	// Its lifetime is ONE ADMISSION, so it must be cleared on every path that
	// ends one: success (releaseSlotLocked), stall (forfeitLocked) and
	// never-dispatched (abandonLocked). Clearing it only on the last made it a
	// per-PROCESS memory: a linked worktree indexed once in an early generation
	// (before its primary existed, so the enqueue gate let it through) kept
	// observedActive=true forever, so every later generation the gate dropped
	// was routed to the STALLED branch and penalised to a durable Failed — the
	// scheduler-declines-by-design case, blamed on the user.
	observedActive map[string]bool
	// reconciled guards the one-time startup rebuild of inflight/admittedInBatch
	// from the durable requests dirs. Without it a restart re-admits repos whose
	// requests are still pending, writing duplicate reindexes without bound.
	reconciled bool

	batchSize           int
	cooldown            time.Duration
	slotTTL             time.Duration
	stalledGrace        time.Duration
	retryBackoff        time.Duration
	maxAttempts         int
	retryJitter         time.Duration
	undispatchedBackoff time.Duration
	now                 func() time.Time
	// pendingFn lists already-queued requests for a repo's control-plane dir.
	// Injectable so the reconcile path is testable without a real store.
	pendingFn func(dir string) ([]requests.Record, error)
	// reconcileDirsFn enumerates the durable migration markers in the store.
	// Kept as a func for injection; returns nothing meaningful itself — the
	// records are read via reconcileStatesFn.
	reconcileDirsFn func() ([]string, error)
	// reconcileStatesFn loads every durable migration marker in the store.
	reconcileStatesFn func() ([]migrationState, error)
	// declinedFn reports whether the SCHEDULER would drop an enqueue for this
	// repo before it ever reached the admission queue — it is the very same
	// predicate the scheduler is configured with as sched.Config.SkipEnqueue
	// (installWorktreeEnqueueGate hands one function to both). nil means "no
	// gate installed", which is read as "nothing is declined".
	//
	// #6175: without it the guard could see only that a repo never became
	// active, which is also what a restart-orphaned repo looks like — so
	// abandonLocked treated the two identically and re-offered the declined repo
	// every undispatchedBackoff, forever, while `grafel status` counted it as
	// in-progress. Asking the gate answers the question exactly instead of
	// inferring it from a count of abandonments, and it stays correct in both
	// directions because the gate is re-evaluated on every heartbeat.
	declinedFn func(repoPath string) bool
	// activeFn reports which repos the scheduler is currently working on
	// (queued / indexing / dirty). A nil or empty result means "no usable
	// signal", which is treated as UNKNOWN rather than as "nothing running" —
	// right after a restart the scheduler has not published yet, and forfeiting
	// on that would be exactly wrong.
	activeFn func() map[string]bool
}

func newStaleReindexGuard() *staleReindexGuard {
	return &staleReindexGuard{
		seen:                map[string]string{},
		inflight:            map[string]time.Time{},
		attempts:            map[string]int{},
		failed:              map[string]bool{},
		retryAfter:          map[string]time.Time{},
		observedActive:      map[string]bool{},
		retryBackoff:        defaultStaleReindexRetryBackoff,
		retryJitter:         staleReindexRetryJitter,
		undispatchedBackoff: defaultStaleReindexUndispatchedBackoff,
		batchSize:           staleReindexBatchSize(),
		cooldown:            defaultStaleReindexCooldown,
		slotTTL:             defaultStaleReindexSlotTTL,
		stalledGrace:        defaultStaleReindexStalledGrace,
		maxAttempts:         staleReindexMaxAttempts,
		now:                 time.Now,
		pendingFn:           requests.ListPending,
		reconcileDirsFn: func() ([]string, error) {
			return discoverRequestsDirs(requestsRoot())
		},
		reconcileStatesFn: func() ([]migrationState, error) {
			return discoverMigrationStates(requestsRoot())
		},
		activeFn: schedulerActiveRepos,
	}
}

// defaultStaleReindexGuard is the process-wide guard used by writeRepoStatusFile.
var defaultStaleReindexGuard = newStaleReindexGuard()

// staleFingerprint identifies one stale generation of repoPath's on-disk
// graph.fb: the file mtime (advances only on a real rewrite) plus the
// reason string (which names the found format version). It is stable across
// heartbeats that observe the same stale file, and changes the moment a new
// graph is written — the two properties the loop-guard relies on.
func staleFingerprint(graphFBMtime int64, reason string) string {
	return fmt.Sprintf("%d|%s", graphFBMtime, reason)
}

// maybeEnqueue is the loop-guarded auto-reindex arm. When required is true and
// this exact (repoPath, fingerprint) has not already been enqueued, it writes a
// single KindReindex request into repoPath's control-plane requests dir — the
// engine's drain loop then applies it via scheduler.Enqueue, exactly as an
// explicit `grafel index --async` would in split mode — and records the
// fingerprint so subsequent heartbeats are deduped. When required is false it
// forgets any recorded fingerprint (self-clear). Returns true iff it wrote a
// request this call. A write failure is logged (best-effort, like the rest of
// the status writer) and NOT recorded, so the next heartbeat retries.
func (g *staleReindexGuard) maybeEnqueue(repoPath string, required bool, fingerprint string, logger *slog.Logger) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.reconcileLocked(logger)
	// The forfeit sweep must run BEFORE the inflight short-circuit below:
	// a repo holding its own slot returns early there, so leaving the sweep
	// inside admitLocked meant a wedged repo's deadline was never evaluated on
	// the heartbeats that mattered.
	g.sweepLocked(logger)

	if !required {
		g.releaseSlotLocked(repoPath)
		return false
	}
	if g.failed[repoPath] {
		return false // gave up on this repo; reported via the statusfile instead
	}
	// #6175: the scheduler will drop any enqueue for this repo, so writing one
	// buys nothing and costs a batch slot for the whole stalled grace. Give back
	// any slot it already holds (the gate can start declining a repo that was
	// admitted while it was still accepted) and report the state instead.
	//
	// releaseSlotLocked is what makes this REVERSIBLE, and the `seen` entry is
	// the load-bearing part of that: the fingerprint is the graph.fb mtime, and
	// a repo the indexer never accepts never changes it, so a retained `seen`
	// entry would suppress every admission after the gate reopened — for the
	// life of the process.
	if g.declinedLocked(repoPath) {
		g.releaseSlotLocked(repoPath)
		return false
	}
	// An outstanding request for this repo is authoritative regardless of what
	// `seen` remembers — and after a restart, `seen` remembers nothing while
	// the durable request is still queued (review finding 1). Checking inflight
	// first is what makes the restart path resume instead of re-enqueueing.
	if _, held := g.inflight[repoPath]; held {
		return false
	}
	if until, backing := g.retryAfter[repoPath]; backing && g.now().Before(until) {
		return false // forfeited recently; standing aside so others can overtake
	}
	if g.seen[repoPath] == fingerprint {
		return false // already enqueued for this exact stale generation
	}
	// #6167: batch admission. A refusal here is a DEFERRAL, not a drop — the
	// fingerprint is deliberately NOT recorded, so the next heartbeat offers
	// this repo again and it is admitted as soon as a batch opens.
	if !g.admitLocked(logger) {
		return false
	}
	if _, err := requests.Write(requestsDirForRepo(repoPath), requests.Record{
		Kind:     requests.KindReindex,
		RepoPath: repoPath,
	}); err != nil {
		if logger != nil {
			logger.Warn("statusfile: auto-reindex enqueue failed", "repo", repoPath, "err", err)
		}
		return false
	}
	g.seen[repoPath] = fingerprint
	g.inflight[repoPath] = g.now()
	g.admittedInBatch++
	// Durable marker: this is what lets a mid-index restart RESUME rather than
	// re-admit. Its lifetime is the migration's, not the request's.
	// Attempts records PAST failures only. Round 4: writing attempts+1 here and
	// then loading it back in reconcileLocked, where the next forfeit
	// incremented again, cost TWO attempts per restart cycle against a cap of
	// three — so two restarts marked a healthy repo permanently unmigratable.
	if err := writeMigrationState(repoPath, migrationState{
		AdmittedAt: g.now(),
		Attempts:   g.attempts[repoPath],
	}); err != nil && logger != nil {
		logger.Warn("statusfile: could not persist auto-reindex marker — a restart may re-admit this repo",
			"repo", repoPath, "err", err)
	}
	return true
}

// hasPendingReindexLocked reports whether repoPath already has an unapplied
// reindex request queued. MUST be called with g.mu held.
func (g *staleReindexGuard) hasPendingReindexLocked(repoPath string) bool {
	if g.pendingFn == nil {
		return false
	}
	recs, err := g.pendingFn(requestsDirForRepo(repoPath))
	if err != nil {
		// Unknown: assume something is queued rather than risk piling on.
		return true
	}
	for _, r := range recs {
		if r.Kind == requests.KindReindex && r.RepoPath == repoPath {
			return true
		}
	}
	return false
}

// migrationFailed reports whether the automatic format migration has given up
// on repoPath. Consumed by writeRepoStatusFile so the give-up is visible in
// `grafel status` instead of being a silent permanent drop.
func (g *staleReindexGuard) migrationFailed(repoPath string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.failed[repoPath]
}

// declinedLocked reports whether the scheduler's enqueue gate would drop this
// repo. MUST be called with g.mu held.
//
// Cost, stated honestly because this runs under the mutex. The common path is
// cheap: worktree.ClassifyRoot is one Lstat, and for a normal repo (`.git` is a
// directory) it returns immediately. But on the branch that matters — the path
// IS a linked worktree — the gate goes on to call reposToWatch(), which in the
// daemon is cmd/grafel.daemonReposToWatch: registry.Groups(), one
// LoadGroupConfig per group, then ResolveFleetRepoPaths, which does one os.Stat
// per fleet repo (fleet_hygiene.go:61). IsLinkedWorktreeOf then re-runs
// ClassifyRoot on the candidate.
//
// That is O(fleet size) filesystem work per stale linked worktree per heartbeat,
// and it happens twice — once here under the lock, once via notAcceptedByIndexer
// outside it. Bounded and rare (only stale-format linked worktrees reach it, on
// a 5s tick) so it is not worth caching, but it is not "one Lstat" either.
func (g *staleReindexGuard) declinedLocked(repoPath string) bool {
	return g.declinedFn != nil && g.declinedFn(repoPath)
}

// notAcceptedByIndexer reports whether repoPath is one the scheduler declines to
// accept — the third migration state (#6175). Consumed by writeRepoStatusFile so
// `grafel status` can report it as neither in-progress nor failed.
//
// A repo already marked Failed is NOT reported here: that verdict is the
// actionable one (it really was dispatched, and it really did die), and the two
// states must stay mutually exclusive so the in-progress count is not reduced
// twice for the same repo.
func (g *staleReindexGuard) notAcceptedByIndexer(repoPath string) bool {
	g.mu.Lock()
	fn := g.declinedFn
	failed := g.failed[repoPath]
	g.mu.Unlock()
	if fn == nil || failed {
		return false
	}
	return fn(repoPath)
}

// setDeclineGate installs the scheduler's enqueue-drop predicate. Called once
// during engine-plane start-up, with the same function the scheduler receives.
func (g *staleReindexGuard) setDeclineGate(fn func(repoPath string) bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.declinedFn = fn
}

// admitLocked applies the #6167 batch policy. MUST be called with g.mu held.
//
// The rule, in one sentence: admit up to batchSize repos, then admit nothing
// more until every one of them has gone current (or forfeited its slot to the
// TTL) AND cooldown has elapsed since that happened.
//
// The "wait for the batch to fully drain" part is load-bearing and is why this
// is not a plain semaphore. A semaphore of size 2 refills the instant one slot
// frees, so len(scheduler.inflight) never reaches zero and stagegate.go:489
// still never yields the heavy-write token — the exact starvation being fixed.
// Draining the whole batch first is what manufactures the idle window.
func (g *staleReindexGuard) admitLocked(logger *slog.Logger) bool {
	now := g.now()

	if g.admittedInBatch >= g.batchSize {
		if len(g.inflight) > 0 {
			return false // batch still executing
		}
		if now.Sub(g.batchDrainedAt) < g.cooldown {
			return false // idle window — hands the stage gate its chance
		}
		g.admittedInBatch = 0 // open the next batch
	}
	return g.admittedInBatch < g.batchSize
}

// sweepLocked reclaims batch slots whose holder has passed its forfeit
// deadline. MUST be called with g.mu held. It runs on EVERY heartbeat, before
// any short-circuit in maybeEnqueue, because the repo holding a wedged slot
// returns early at the inflight check — leaving the sweep inside admitLocked
// meant its deadline was never evaluated at all.
func (g *staleReindexGuard) sweepLocked(logger *slog.Logger) {
	now := g.now()

	// The progress signal, sampled once per sweep. An EMPTY snapshot means the
	// scheduler has published nothing at all — UNKNOWN, not idle — and nothing
	// is forfeited on that basis. Note this is a narrow protection: once
	// anything has ever been indexed the snapshot is permanently non-empty
	// (publishRepoStatesLocked unions in indexedRepos), so the real work is
	// done by the observedActive split below.
	var active map[string]bool
	if g.activeFn != nil {
		active = g.activeFn()
	}
	known := len(active) > 0

	for repo, at := range g.inflight {
		if known && active[repo] {
			g.observedActive[repo] = true
		}

		held := now.Sub(at)
		deadline := g.slotTTL
		penalise := true

		switch {
		case !known || active[repo]:
			// No signal, or the scheduler is demonstrably working on it. Only
			// the hard ceiling applies.
		case g.observedActive[repo]:
			// It WAS dispatched and has now stopped without completing — a
			// genuine stall. This is the case that earns an attempt.
			deadline = g.stalledGrace
		default:
			// The scheduler was never told about this repo — orphaned by a
			// restart. (A repo the enqueue gate declines by design never gets
			// here: #6175 refuses it admission up front.) Reclaim the slot so
			// the migration continues, but charge nothing — it is not broken.
			deadline = g.stalledGrace
			penalise = false
		}
		if deadline > g.slotTTL {
			deadline = g.slotTTL
		}

		if held >= deadline {
			if penalise {
				g.forfeitLocked(repo, held, deadline, logger)
			} else {
				g.abandonLocked(repo, held, logger)
			}
		}
	}
}

// abandonLocked reclaims the slot of a repo the scheduler never accepted. No
// attempt is charged and the repo is never marked failed — round-4 blockers 1
// and 2 both reduce to charging a penalty for work that was never dispatched.
// MUST be called with g.mu held.
func (g *staleReindexGuard) abandonLocked(repo string, held time.Duration, logger *slog.Logger) {
	delete(g.inflight, repo)
	delete(g.seen, repo)
	// No-op today: this path is reached only from the default branch of
	// sweepLocked, i.e. exactly when the repo was NEVER observed active, so the
	// key is absent. Kept so that "every admission-ending path clears
	// observedActive" holds structurally if those branch conditions change —
	// the round-5 blocker was one such path silently not clearing it. Mutation
	// testing correctly reports removing this line as a survivor.
	delete(g.observedActive, repo)
	g.retryAfter[repo] = g.now().Add(g.undispatchedDelayFor(repo))
	// No durable history: nothing failed, so nothing to remember.
	clearMigrationState(repo)
	if logger != nil {
		logger.Info("statusfile: auto-reindex was never picked up by the scheduler — releasing the slot without counting a failure",
			"repo", repo, "waited", held.Truncate(time.Second),
			"retry_after", g.undispatchedDelayFor(repo))
	}
	if len(g.inflight) == 0 {
		g.batchDrainedAt = g.now()
	}
}

// forfeitLocked takes a batch slot back from a repo that has held it past its
// deadline. MUST be called with g.mu held.
//
// Review finding 2: the previous behaviour left the repo in `seen`, which made
// a forfeit a SILENT PERMANENT DROP — the repo went on serving a stale graph
// with nothing anywhere saying so. Now a forfeit clears `seen` so the repo is
// retried in a later batch, and only after maxAttempts is it marked failed —
// at which point writeRepoStatusFile publishes that to the status plane.
func (g *staleReindexGuard) forfeitLocked(repo string, held, deadline time.Duration, logger *slog.Logger) {
	delete(g.inflight, repo)
	// This admission is over: the next one must judge dispatch afresh, or a
	// repo that stalled once is charged for later attempts that the scheduler
	// never accepted.
	delete(g.observedActive, repo)
	g.attempts[repo]++

	if g.attempts[repo] >= g.maxAttempts {
		g.failed[repo] = true
		_ = writeMigrationState(repo, migrationState{Attempts: g.attempts[repo], Failed: true})
		if logger != nil {
			logger.Warn("statusfile: giving up on the automatic format migration for this repo — it needs a manual reindex",
				"repo", repo, "attempts", g.attempts[repo], "held_for", held.Truncate(time.Second),
				"fix", "grafel index "+repo)
		}
	} else {
		delete(g.seen, repo) // eligible for another turn in a later batch
		g.retryAfter[repo] = g.now().Add(g.retryDelayFor(repo))
		_ = writeMigrationState(repo, migrationState{Attempts: g.attempts[repo]})
		if logger != nil {
			logger.Warn("statusfile: auto-reindex slot forfeited — will retry in a later batch",
				"repo", repo, "attempt", g.attempts[repo], "of", g.maxAttempts,
				"held_for", held.Truncate(time.Second), "deadline", deadline)
		}
	}

	if len(g.inflight) == 0 {
		g.batchDrainedAt = g.now()
	}
}

// reconcileLocked rebuilds outstanding-migration state from the DURABLE request
// queue, once per process. MUST be called with g.mu held.
//
// Review finding 1 (measured): seen/inflight are in-memory only, so a restarted
// daemon saw an empty guard while the previous run's reindex requests were
// still queued and graph.fb was still stale — and re-admitted the same repos,
// writing a duplicate full reindex (42s–4m53s each) per repo per restart, with
// no bound across restarts. Since requests.Write is durable precisely so the
// queue survives a restart, the guard has to read it back: pending KindReindex
// records ARE the outstanding set.
func (g *staleReindexGuard) reconcileLocked(logger *slog.Logger) {
	if g.reconciled {
		return
	}
	if g.reconcileStatesFn == nil {
		g.reconciled = true
		return
	}
	states, err := g.reconcileStatesFn()
	if err != nil {
		// Round-3 MEDIUM 4: do NOT latch `reconciled` here. Setting it before
		// the fallible call meant one transient glob failure — requestsRoot()
		// missing on a cold start, or a busy store — silently disabled the
		// restart remedy for the entire process behind a single Warn. Leaving
		// it unset means the next heartbeat retries.
		if logger != nil {
			logger.Warn("statusfile: could not reconcile auto-reindex state; will retry", "err", err)
		}
		return
	}
	g.reconciled = true

	now := g.now()
	for _, st := range states {
		if st.RepoPath == "" {
			continue
		}
		// Round-4 LOW 5: a repo unregistered after admission never reaches
		// maybeEnqueue(required=false), so its marker leaked and a later
		// re-registration resurrected stale attempts. Drop markers whose repo
		// is no longer on disk.
		if _, statErr := os.Stat(st.RepoPath); os.IsNotExist(statErr) {
			clearMigrationState(st.RepoPath)
			continue
		}
		if st.Failed {
			g.failed[st.RepoPath] = true
			g.attempts[st.RepoPath] = st.Attempts
			continue
		}
		if st.Attempts > 0 {
			g.attempts[st.RepoPath] = st.Attempts
		}
		if st.AdmittedAt.IsZero() {
			continue // record carries history only; no slot held
		}
		// The deadline clock restarts from process start rather than from the
		// persisted AdmittedAt: a daemon that was down for hours would
		// otherwise forfeit every recovered slot instantly.
		g.inflight[st.RepoPath] = now

		// Round-4 blocker 1: restoring the guard's slot is not enough. The
		// drain acked and REMOVED the request ~2s after admission, and the
		// index died with the daemon, so the scheduler has no memory of this
		// repo and will never index it. Without re-telling it, the slot is held
		// for work that will never happen. The previous index certainly did not
		// finish (the process died), so re-enqueueing is the correct resume,
		// not a duplicate.
		// Idempotent: if the previous run's request is still queued (the daemon
		// died before the drain reached it) there is nothing to resume, and
		// writing another would build a fresh backlog one request per restart —
		// the very stampede this issue is about, re-entered through the
		// recovery path.
		if g.hasPendingReindexLocked(st.RepoPath) {
			continue
		}
		if _, err := requests.Write(requestsDirForRepo(st.RepoPath), requests.Record{
			Kind:     requests.KindReindex,
			RepoPath: st.RepoPath,
		}); err != nil && logger != nil {
			logger.Warn("statusfile: could not re-enqueue a recovered format migration",
				"repo", st.RepoPath, "err", err)
		}
	}

	// Treat the recovered set as the current batch, so a restart does not open
	// a fresh batch on top of work that is already in progress.
	g.admittedInBatch = len(g.inflight)
	if len(g.inflight) > 0 && logger != nil {
		logger.Info("statusfile: resumed an in-progress format migration",
			"outstanding", len(g.inflight))
	}
}

// releaseSlotLocked ends repoPath's admission WITHOUT charging it anything:
// either its graph went current (the migration succeeded) or the scheduler
// stopped accepting it (#6175). It records when the batch fully drained so the
// cooldown can start. MUST be called with g.mu held.
//
// It is the single clearing point for every piece of per-admission state,
// `seen` included. That last one is not bookkeeping: `seen` is keyed on a
// fingerprint of the graph.fb mtime, so for a repo that never gets indexed the
// fingerprint never changes, and an entry left behind here blocks re-admission
// permanently. forfeitLocked and abandonLocked each clear it on their own paths;
// having this one do it too is what makes "no path ends an admission while
// leaving `seen` set" a property of the type rather than of three call sites.
func (g *staleReindexGuard) releaseSlotLocked(repoPath string) {
	// The repo is current: the migration succeeded for it. Round-3 MEDIUM 3 —
	// attempts was never cleared, so a repo that was forfeited once per
	// generation but always eventually SUCCEEDED accumulated attempts across
	// generations and was ultimately reported to the user as unmigratable.
	// Success resets the history, durable record included.
	hadState := g.attempts[repoPath] > 0
	delete(g.seen, repoPath)
	delete(g.attempts, repoPath)
	delete(g.retryAfter, repoPath)
	delete(g.observedActive, repoPath)

	_, held := g.inflight[repoPath]
	if held || hadState {
		clearMigrationState(repoPath)
	}
	if !held {
		return
	}
	delete(g.inflight, repoPath)
	if len(g.inflight) == 0 {
		g.batchDrainedAt = g.now()
	}
}

// schedulerActiveRepos reports the repos the scheduler currently has queued,
// indexing, or dirty — the progress signal the forfeit decision keys off
// (round-3 blocker 2). Returns nil when the scheduler has published nothing,
// which callers must read as "unknown", never as "idle".
func schedulerActiveRepos() map[string]bool {
	states := indexstate.RepoStates()
	if len(states) == 0 {
		return nil
	}
	out := make(map[string]bool, len(states))
	for _, st := range states {
		switch st.State {
		case indexstate.StateQueued, indexstate.StateIndexing, indexstate.StateDirty:
			out[st.Path] = true
		}
	}
	return out
}

// attemptsFor reports how many failed migration attempts are recorded for
// repoPath. Test/diagnostic accessor.
func (g *staleReindexGuard) attemptsFor(repoPath string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.attempts[repoPath]
}

// retryDelayFor spreads the retry backoff per repo so two repos that forfeit
// together do not wait together and re-pair on the next batch.
func (g *staleReindexGuard) retryDelayFor(repoPath string) time.Duration {
	return g.retryBackoff + spreadFor(repoPath, g.retryJitter)
}

// undispatchedDelayFor is the stand-aside delay for a repo the scheduler never
// accepted, jittered on the same basis.
func (g *staleReindexGuard) undispatchedDelayFor(repoPath string) time.Duration {
	if g.undispatchedBackoff <= 0 {
		return g.retryBackoff + spreadFor(repoPath, g.retryJitter)
	}
	return g.undispatchedBackoff + spreadFor(repoPath, g.retryJitter)
}

// spreadFor maps repoPath deterministically into [0, window).
//
// Round-4 MEDIUM 3: this used a 32-bit FNV hash modulo the window in
// NANOSECONDS. Sum32 maxes at 4.29e9 while a 5m window is 3e11ns, so the
// modulo never wrapped and the real spread was 0-4.295s — measured min
// 10m0.02s / max 10m3.63s across 500 repos, against a declared 0-5m. Two repos
// still landed in the same 5s heartbeat, so the stated purpose was not
// achieved. A 64-bit hash makes the modulo meaningful.
func spreadFor(repoPath string, window time.Duration) time.Duration {
	if window <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(repoPath))
	return time.Duration(h.Sum64() % uint64(window))
}
