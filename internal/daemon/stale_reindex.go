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
// scheduler's own progress signal, not off what the other slots are doing.
// Keying it off the other slots meant two concurrently wedged repos never
// satisfied the short-deadline condition and the migration stalled for the full
// hard ceiling (measured 15m45s). A repo the scheduler is actively working on
// is protected to the hard ceiling; a repo absent from a non-empty snapshot is
// forfeited at the stalled grace. An EMPTY snapshot means "unknown" — right
// after a restart the scheduler has not published yet — and never triggers a
// forfeit.
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
	// reconciled guards the one-time startup rebuild of inflight/admittedInBatch
	// from the durable requests dirs. Without it a restart re-admits repos whose
	// requests are still pending, writing duplicate reindexes without bound.
	reconciled bool

	batchSize    int
	cooldown     time.Duration
	slotTTL      time.Duration
	stalledGrace time.Duration
	retryBackoff time.Duration
	maxAttempts  int
	retryJitter  time.Duration
	now          func() time.Time
	// pendingFn lists already-queued requests for a repo's control-plane dir.
	// Injectable so the reconcile path is testable without a real store.
	pendingFn func(dir string) ([]requests.Record, error)
	// reconcileDirsFn enumerates the durable migration markers in the store.
	// Kept as a func for injection; returns nothing meaningful itself — the
	// records are read via reconcileStatesFn.
	reconcileDirsFn func() ([]string, error)
	// reconcileStatesFn loads every durable migration marker in the store.
	reconcileStatesFn func() ([]migrationState, error)
	// activeFn reports which repos the scheduler is currently working on
	// (queued / indexing / dirty). A nil or empty result means "no usable
	// signal", which is treated as UNKNOWN rather than as "nothing running" —
	// right after a restart the scheduler has not published yet, and forfeiting
	// on that would be exactly wrong.
	activeFn func() map[string]bool
}

func newStaleReindexGuard() *staleReindexGuard {
	return &staleReindexGuard{
		seen:         map[string]string{},
		inflight:     map[string]time.Time{},
		attempts:     map[string]int{},
		failed:       map[string]bool{},
		retryAfter:   map[string]time.Time{},
		retryBackoff: defaultStaleReindexRetryBackoff,
		retryJitter:  staleReindexRetryJitter,
		batchSize:    staleReindexBatchSize(),
		cooldown:     defaultStaleReindexCooldown,
		slotTTL:      defaultStaleReindexSlotTTL,
		stalledGrace: defaultStaleReindexStalledGrace,
		maxAttempts:  staleReindexMaxAttempts,
		now:          time.Now,
		pendingFn:    requests.ListPending,
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
		delete(g.seen, repoPath)
		g.releaseSlotLocked(repoPath)
		return false
	}
	if g.failed[repoPath] {
		return false // gave up on this repo; reported via the statusfile instead
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
	if err := writeMigrationState(repoPath, migrationState{
		AdmittedAt: g.now(),
		Attempts:   g.attempts[repoPath] + 1,
	}); err != nil && logger != nil {
		logger.Warn("statusfile: could not persist auto-reindex marker — a restart may re-admit this repo",
			"repo", repoPath, "err", err)
	}
	return true
}

// migrationFailed reports whether the automatic format migration has given up
// on repoPath. Consumed by writeRepoStatusFile so the give-up is visible in
// `grafel status` instead of being a silent permanent drop.
func (g *staleReindexGuard) migrationFailed(repoPath string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.failed[repoPath]
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

	// The progress signal, sampled once per sweep. nil means the scheduler has
	// published nothing yet (e.g. immediately after a restart) — UNKNOWN, not
	// idle — in which case every repo gets the hard ceiling and nothing is
	// forfeited early.
	var active map[string]bool
	if g.activeFn != nil {
		active = g.activeFn()
	}

	for repo, at := range g.inflight {
		held := now.Sub(at)
		// Round-3 blocker 2: the deadline is PER-REPO and keyed off whether the
		// scheduler is actually working on THIS repo, not off what the other
		// slots happen to be doing. Two concurrently wedged repos therefore
		// both forfeit at the stalled grace instead of both waiting out the
		// hard ceiling.
		deadline := g.slotTTL
		if len(active) > 0 && !active[repo] && g.stalledGrace > 0 && g.stalledGrace < deadline {
			deadline = g.stalledGrace
		}
		if held >= deadline {
			g.forfeitLocked(repo, held, deadline, logger)
		}
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
		// otherwise forfeit every recovered slot instantly and burn an attempt
		// for a repo that never got a chance to run.
		g.inflight[st.RepoPath] = now
	}
	// Treat the recovered set as the current batch, so a restart does not open
	// a fresh batch on top of work that is already in progress.
	g.admittedInBatch = len(g.inflight)
	if len(g.inflight) > 0 && logger != nil {
		logger.Info("statusfile: resumed an in-progress format migration",
			"outstanding", len(g.inflight))
	}
}

// releaseSlotLocked frees repoPath's batch slot once its graph is current
// again, recording when the batch fully drained so the cooldown can start.
// MUST be called with g.mu held.
func (g *staleReindexGuard) releaseSlotLocked(repoPath string) {
	// The repo is current: the migration succeeded for it. Round-3 MEDIUM 3 —
	// attempts was never cleared, so a repo that was forfeited once per
	// generation but always eventually SUCCEEDED accumulated attempts across
	// generations and was ultimately reported to the user as unmigratable.
	// Success resets the history, durable record included.
	hadState := g.attempts[repoPath] > 0
	delete(g.attempts, repoPath)
	delete(g.retryAfter, repoPath)

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
	if g.retryJitter <= 0 {
		return g.retryBackoff
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(repoPath))
	return g.retryBackoff + time.Duration(uint64(h.Sum32())%uint64(g.retryJitter))
}
