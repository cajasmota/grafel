// Package sched is the daemon's reactive scheduler (Phase B+). The
// watcher hands off settled-repo notifications to Enqueue; the
// scheduler serialises per-repo indexes, runs them on a small worker
// pool, then schedules:
//
//   - a debounced cross-repo link recompute per group (10s),
//   - a debounced GROUP-scope graph-algorithm pass (30s), chained off the
//     SUCCESS path of the link pass (#5349 A3, epic #5350). Algorithms now run
//     ONCE over the assembled group union — so cross-repo edges are finally
//     seen — and write the <group>-algo.json overlay. This replaces the old
//     per-repo algo pass (a single-repo group is the degenerate one-repo
//     union). N repo reindexes → 1 link pass → 1 group-algo pass.
//
// The link recompute and group-algorithm pass run via caller-supplied callbacks
// so the scheduler stays free of extractor + graph package dependencies.
//
// Concurrency cap (post-#644): the scheduler now applies RSS-budget
// admission control on top of the worker pool. Before a queued job
// is dispatched to a worker, the scheduler checks that
//
//	sum(predicted RSS of currently-running jobs) + predicted RSS of
//	the new job <= BudgetMB
//
// If the budget would be exceeded the job stays in the pending queue
// and is retried as soon as a running job completes. This prevents
// the post-#639 concurrent-3-repo peak (672MB) from blowing past the
// 500MB target on the real-fixture benchmark.
//
// Ref-aware indexing (PH1b of epic #2087 / issue #2089):
// The scheduler now captures the current HEAD ref at Enqueue time (via
// RefCaptureFn) and passes it to IndexFn. This ensures that a debounced
// batch that fires after a branch switch indexes against the ref that was
// current when the event was first enqueued (i.e. the user's intent at
// the moment of the change), not the ref at eventual dispatch time.
//
// Branch-switch events (from the GitHeadPoller) use EnqueueRef directly,
// supplying the new ref that the poller already observed — no extra git
// call needed.
package sched

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/repolock"
)

// IndexFn re-indexes a single repo at a specific git ref. The scheduler
// invokes it on a worker goroutine; concurrent calls for distinct repos may
// run in parallel up to the worker-pool size, but each repo path is
// serialised against itself.
//
// ref is the git ref name (e.g. "main", "feat/x") captured at Enqueue time.
// An empty ref means the current HEAD ref could not be determined; callers
// should fall back to gitmeta.Capture(repoPath) if they need it.
type IndexFn func(ctx context.Context, repoPath string, ref string) error

// RefCaptureFn returns the current HEAD ref AND commit SHA for repoPath. Used
// to snapshot both at Enqueue time so debounced batches index against the ref
// that was active when the file-change event fired, not the ref at dispatch
// time. The commit SHA is the stable per-commit identity the #5726/#5729
// reindex circuit breaker keys on (the branch NAME is not — a fix commit on
// the same branch keeps the same name yet must reset the breaker, and a
// detached HEAD has an empty name but a perfectly good SHA).
//
// ref may be "" for a detached HEAD or non-git directory; commit may be "" for
// a non-git directory. IndexFn must tolerate an empty ref.
type RefCaptureFn func(repoPath string) (ref, commit string)

// LinksFn re-runs the cross-repo link passes for a group.
type LinksFn func(ctx context.Context, group string) error

// GroupAlgoFn runs the graph-algorithm pass ONCE over the assembled union of a
// group's per-repo graphs (community detection, PageRank, betweenness,
// articulation points) and writes the <group>-algo.json overlay (#5349 A3,
// epic #5350). It is chained off the SUCCESS path of the link pass — so N repo
// reindexes coalesce into one link pass and then one group-algo pass — and is
// cancelled+rescheduled on any further link completion. It replaces the old
// per-repo AlgoFn: a single-repo group is the degenerate one-repo union.
type GroupAlgoFn func(ctx context.Context, group string) error

// GroupsForRepoFn returns the group names a repo participates in.
// Provided by the caller so the scheduler does not import the registry.
type GroupsForRepoFn func(repoPath string) []string

// StaleGroupsFn returns the names of groups whose group-algo overlay EXISTS on
// disk but has gone STALE relative to the current per-repo graph.fb mtimes
// (#5403). It is consulted by the periodic overlay-freshness sweep so a SETTLED
// group (no recent reindex → no link pass → no scheduleGroupAlgo) still gets its
// stale overlay recomputed. Provided by the caller so the scheduler keeps no
// dependency on the registry / groupalgo packages.
//
// It MUST exclude groups with no overlay yet (those go through the normal
// first-compute link-pass chain — the sweep must not force-fire them). nil
// disables the sweep entirely.
type StaleGroupsFn func() []string

// PredictFn returns a predicted peak RSS contribution (MB) for indexing
// repoPath. Used by admission control. nil disables prediction (every
// job is admitted regardless of budget).
type PredictFn func(repoPath string) int64

// SkipEnqueueFn reports whether an enqueue for repoPath should be DROPPED
// before it ever reaches the admission queue. It is the root-cause guard for
// issue #3680: a path that is a linked git worktree of an already-indexed
// primary repo must NOT be cold-indexed as a brand-new root repo (which would
// spawn its own ~100MB full graph store and blow the RSS budget). The worktree
// subsystem still tracks such a path as an ephemeral child with aggressive
// TTLs; it simply must not become an independent root index job.
//
// Returning true silently skips the enqueue. nil disables the guard (every
// enqueue is accepted — legacy behaviour).
type SkipEnqueueFn func(repoPath string) bool

// IncrementalResult carries the outcome of a S3 incremental reindex attempt.
// Mirrors extractors.Result without importing that package here to avoid a
// circular dependency (extractors imports daemon for StateDirForRepo).
type IncrementalResult struct {
	// Done is true when the incremental patch completed and the caller should
	// NOT fall through to the full IndexFn.
	Done bool
	// FallbackReason is non-empty when Done=false (safety-net triggered or
	// too many files changed). Used only for logging.
	FallbackReason string
	// ChangedFiles is the number of files that were re-extracted.
	ChangedFiles int
}

// IncrementalFn attempts the S3 incremental file-level reindex optimisation.
// Called by the scheduler worker when GRAFEL_INCREMENTAL_REINDEX=1 is set.
// Returns done=true when the patch succeeded; done=false causes the scheduler
// to fall through to IndexFn (full reindex fallback).
type IncrementalFn func(ctx context.Context, repoPath string, ref string) IncrementalResult

// EntityCountFn returns the number of entities in the materialized graph for
// (repoPath, ref), or -1 when it cannot be determined cheaply. Injected so the
// completion log can surface entities=N without the scheduler importing the
// daemon store-layout / graph packages (avoids an import cycle). Used purely
// for observability (#5710 follow-up): a 0-entity completion is a silent-empty
// signal that must be visible at a glance. nil → entities is omitted from the
// log.
type EntityCountFn func(repoPath string, ref string) int

// Config wires the scheduler. All function fields are required; nil
// causes Enqueue to short-circuit with a logged warning.
type Config struct {
	Workers           int           // worker pool size; defaults to 2
	LinkDebounce      time.Duration // group settling window; defaults to 10s
	GroupAlgoDebounce time.Duration // group-algo settling window after a link pass; defaults to 30s

	// GroupAlgoMaxWait bounds how long a group-algo pass may be deferred by
	// continuous re-arming of GroupAlgoDebounce (#5450). The debounce coalesces
	// a burst of reindexes into one pass, but on a busy daemon where members
	// re-index back-to-back, every link completion re-arms the (long) debounce
	// and the recompute can be starved for minutes — leaving the overlay stale
	// right after a reindex. The max-wait is a ceiling on that starvation: once a
	// group's debounce has been continuously armed for GroupAlgoMaxWait, the next
	// (re-)arm fires PROMPTLY instead of resetting the full window. It coalesces
	// (one pass per window, not N) and reuses the same CPU-capped path, so it
	// cannot uncork an unbounded recompute. <=0 defaults to
	// groupAlgoMaxWaitDefault; override with GRAFEL_GROUP_ALGO_MAX_WAIT.
	GroupAlgoMaxWait time.Duration

	Index         IndexFn
	Links         LinksFn
	GroupAlgo     GroupAlgoFn
	GroupsForRepo GroupsForRepoFn
	Logger        *slog.Logger

	// StaleGroups, when non-nil together with a positive OverlaySweepInterval,
	// enables the periodic overlay-freshness sweep (#5403). It returns the
	// groups whose on-disk overlay has gone stale; the sweep re-arms a
	// (debounced + CPU-capped) group-algo pass for each, so a settled group's
	// overlay no longer waits for the next reindex to be recomputed. nil
	// disables the sweep.
	StaleGroups StaleGroupsFn

	// OverlaySweepInterval is the cadence of the overlay-freshness sweep
	// (#5403). <=0 disables it (so does a nil StaleGroups). The actual recompute
	// it triggers is the existing debounced + CPU-capped scheduleGroupAlgo path,
	// so the sweep itself only does cheap per-group stat-compares; the interval
	// is therefore deliberately coarse (default overlaySweepIntervalDefault) and
	// must stay >= the group-algo debounce so a sweep never re-arms faster than a
	// pass can settle. Override with GRAFEL_OVERLAY_SWEEP_INTERVAL.
	OverlaySweepInterval time.Duration

	// BudgetMB caps the total predicted RSS of concurrently running
	// index jobs (megabytes). 0 disables admission control entirely
	// (legacy behaviour). Default for production wiring: 500.
	BudgetMB int64

	// Predict returns a per-repo RSS prediction. If nil, every job is
	// assumed to cost 1MB (admission control still serialises but is
	// effectively disabled unless many workers are configured).
	Predict PredictFn

	// SkipEnqueue, when non-nil, is consulted at the top of EnqueueRef. When
	// it returns true the enqueue is dropped before entering the pending
	// queue. This is the worktree-churn guard for issue #3680: linked
	// worktrees of already-indexed primaries are not cold-indexed as new
	// root repos. nil = accept every enqueue (legacy behaviour).
	SkipEnqueue SkipEnqueueFn

	// History, when non-nil, overrides Predict for repos that have a
	// recorded peak. The scheduler also writes each completed job's
	// observed RSS into History.
	History *RSSHistory

	// RefCapture returns the current HEAD ref for repoPath. Called at
	// Enqueue time so the ref is captured when the file-change event fires,
	// not when the debounced job eventually runs. When nil, ref is always
	// captured as "" (which callers should treat as "unknown / use HEAD").
	RefCapture RefCaptureFn

	// AlgoCap limits how many per-repo algorithm passes (Louvain/PageRank/
	// articulation) may run concurrently. This is the fix for #2141 root
	// cause C and #2140 hyp-2: each algo pass is CPU- and heap-intensive;
	// running N simultaneously on an N-core host saturates all cores and
	// spikes RSS proportionally.
	//
	// 0 (or negative) means: auto = min(3, max(2, runtime.NumCPU()/2)),
	// clamped to a hard ceiling of 3 cores so indexing/algo work never
	// saturates the user's machine, even on large hosts.
	// Set to 1 to fully serialise algo passes.
	AlgoCap int

	// Incremental, when non-nil, is attempted before IndexFn when the
	// incremental reindex path is enabled (S3 of epic #2149, issue #2153).
	// It performs a surgical file-level graph patch that is ~25× faster
	// than a full reindex for single-file edits.
	//
	// The function returns (done=true) when the incremental patch succeeded
	// and the full IndexFn should be skipped. It returns (done=false) on any
	// precondition failure, safety-net trigger, or error — the scheduler
	// falls through to IndexFn transparently (full reindex fallback).
	//
	// Default (nil): incremental path is never tried; behaviour is identical
	// to before this field was added.
	Incremental IncrementalFn

	// EntityCount, when non-nil, returns the entity count of the materialized
	// graph for a (repoPath, ref) after an index completes. It is used only to
	// annotate the "indexer: completed" log with entities=N so a silent 0-entity
	// completion (e.g. an empty-graph store recreation, #5710) is visible.
	// nil → the entities field is omitted.
	EntityCount EntityCountFn

	// ExtractorConfig, when non-nil, is consulted by the scheduler to
	// determine whether the incremental reindex path is active (issue #2397).
	// IsIncrementalEnabled() on this config replaces the private
	// incrementalEnabled() helper that read GRAFEL_INCREMENTAL_REINDEX
	// directly, establishing a single source of truth.
	//
	// When nil the scheduler falls back to env-var reads via a nil-safe
	// ExtractorConfig.IsIncrementalEnabled() call, preserving backward
	// compatibility for callers that have not yet been migrated.
	ExtractorConfig *extractor.ExtractorConfig

	// MemReleaseDebounce is the quiet window after the scheduler goes fully
	// idle (no in-flight index, empty pending queue, no pending algo/link
	// passes) before FreeOSMemory is called once to return the retained Go
	// heap arena to the OS (#3648). The daemon reindexes frequently under
	// file-event churn, so calling FreeOSMemory after every index — a full
	// stop-the-world GC + madvise each time — would be far too costly; the
	// debounce ensures it fires at most once per idle period, after activity
	// has actually settled. <=0 defaults to memReleaseDebounceDefault. Set
	// negative-via-disable through MemReleaseDisabled.
	MemReleaseDebounce time.Duration

	// MemReleaseDisabled turns the idle FreeOSMemory trigger off entirely
	// (escape hatch / tests that don't want the goroutine). Default false.
	MemReleaseDisabled bool

	// FreeOSMemory is the function the idle trigger calls to return retained
	// heap to the OS. nil defaults to runtime/debug.FreeOSMemory. Overridable
	// so tests can assert the trigger fires without paying for a real STW GC.
	FreeOSMemory func()

	// StageGateDisabled turns off the daemon-wide heavy write-stage gate
	// (#5954), restoring the legacy behaviour where the index batch, the
	// cross-repo link pass and the group-algo pass may run CONCURRENTLY. The
	// gate is on by default: two co-resident copies of the same group union
	// graph were the measured cause of the 4.4–5.3GB whole-machine peak. Kept
	// as an escape hatch so the wall-clock cost of serialising is measurable
	// against the pre-gate baseline. Env: GRAFEL_STAGE_GATE=0.
	StageGateDisabled bool

	// StageGateRetry is how long a DEFERRED heavy stage waits before re-testing
	// the gate. <=0 defaults to stageGateRetryDefault (15s). Env:
	// GRAFEL_STAGE_GATE_RETRY.
	StageGateRetry time.Duration

	// StageGateMaxDefer bounds how long an exclusive stage may be deferred by
	// index activity before the gate raises a DRAIN BARRIER (stops admitting new
	// index jobs so the in-flight batch clears and the starved stage can acquire
	// without overlapping). This is the gate's own starvation guard — it
	// replaces, rather than shares, GroupAlgoMaxWait, because gate deferral
	// deliberately takes precedence over that max-wait. <=0 defaults to
	// stageGateMaxDeferDefault (300s). Env: GRAFEL_STAGE_GATE_MAX_DEFER.
	StageGateMaxDefer time.Duration

	// StageGateHoldMax is how long an exclusive stage may hold the token before
	// the gate declares it FORFEITED. The token is held across a subprocess
	// spawn, so a crashed/wedged child must not wedge the daemon.
	//
	// This is the SCALE-SENSITIVE knob, and it shipped mis-sized: 15m against a
	// pass that measures 32–53 minutes on the real corpus, so every run was
	// forfeited. It must exceed a LEGITIMATE group-algo pass on the deployment's
	// largest group with room for that group to grow. <=0 defaults to
	// stageGateHoldMaxDefault (4h = 4x the measured worst pass), sized in
	// stagegate.go against stageGateMeasuredPassWorst.
	//
	// A `stage_gate_forfeit` event now means a holder blew a bound no legitimate
	// pass can reach — investigate it, do not reflexively raise this.
	// Env: GRAFEL_STAGE_GATE_HOLD_MAX.
	StageGateHoldMax time.Duration

	// StageGateForfeitGrace is how long a FORFEITED stage keeps the gate closed
	// before it is cancelled and its token reclaimed. The forfeit itself neither
	// cancels nor releases (see reapStageLocked: releasing re-ran the very pass
	// it forfeited, and cancelling discarded up to 53 minutes of work); this is
	// the terminal remedy that bounds the resulting wedge. <=0 defaults to
	// stageGateForfeitGraceDefault (30m), i.e. 4h30m of total patience.
	// Env: GRAFEL_STAGE_GATE_FORFEIT_GRACE.
	StageGateForfeitGrace time.Duration

	// StageGateDrainMax bounds the drain barrier: how long index dispatch may be
	// held while waiting for the in-flight batch to clear for a starved stage.
	// The barrier only lapses once the batch has actually drained (see
	// reapStageLocked), so this is the floor on that wait, not a hard cutoff
	// while jobs are still executing. <=0 defaults to stageGateDrainMaxDefault
	// (2m). Env: GRAFEL_STAGE_GATE_DRAIN_MAX.
	StageGateDrainMax time.Duration

	// StageGateBargeMax bounds a FOREGROUND BARGE hold (see stagegate_barge.go).
	// Deliberately NOT shared with StageGateHoldMax: a rebuild runs 10–12
	// minutes, right at that 15m bound, so a barge governed by it would forfeit
	// itself mid-rebuild on exactly the longest runs and silently reintroduce
	// the overlap it exists to remove. This is a leak backstop with an
	// hour-scale default; a `stage_gate_barge_expired` event means a foreground
	// goroutine never unwound its release — investigate rather than raise it.
	// <=0 defaults to stageGateBargeMaxDefault (60m).
	// Env: GRAFEL_STAGE_GATE_BARGE_MAX.
	StageGateBargeMax time.Duration
}

// deadManTimeout is how long the scheduler waits with a non-empty pending
// queue and zero admitted jobs before force-admitting the smallest queued
// job. This is the relief valve for admission-control wedge scenarios
// (e.g. inflated RSS history predictions that exceed the budget).
const deadManTimeout = 2 * time.Minute

// yieldRetryDelay is how long the scheduler waits before re-enqueuing a repo it
// skipped because a foreground group-rebuild owned it (see runIndex). It bounds
// the retry rate so the scheduler cannot busy-loop against a still-running
// rebuild, while remaining short enough that the repo is promptly reindexed
// once the rebuild releases its claim.
const yieldRetryDelay = 3 * time.Second

// memReleaseDebounceDefault is how long the scheduler must be fully idle
// (no in-flight index, empty queue, no pending algo/link passes) before it
// calls FreeOSMemory once to hand the retained Go heap arena back to the OS
// (#3648). 30s is deliberately well past the typical file-event reindex
// cadence (~1/min under churn) so a burst of edits does not repeatedly pay
// for a stop-the-world GC + madvise. It fires at most once per idle period.
const memReleaseDebounceDefault = 30 * time.Second

// memReleaseTick is the poll interval of the idle-detection loop. It only
// reads cheap in-memory counters under the scheduler lock, so a short tick
// is fine; the actual FreeOSMemory call is gated by the debounce above.
const memReleaseTick = 5 * time.Second

// reindexFailBackoffBase and reindexFailBackoffMax bound the #5726/#5729
// per-repo reindex circuit breaker. A repo genuinely over the FlatBuffers
// 2-GiB builder cap fails the SAME way on every attempt at the SAME commit
// (the fbwriter fail-soft path — internal/graph/fbwriter/streaming.go —
// recovers the marshal panic and leaves last-good graph.fb intact, but the
// trigger conditions (watcher fs events, git-HEAD poll) are input-driven and
// unaware of the failure: any further churn at the same commit re-fires a
// doomed reindex immediately, hot-looping the marshal attempt forever
// (observed as the panic logged 74x in daemon.err in #5726). The breaker
// skips same-ref re-attempts with exponential backoff instead, and resets the
// moment the target ref changes (a new commit deserves a real try).
const (
	reindexFailBackoffBase = 30 * time.Second
	reindexFailBackoffMax  = 5 * time.Minute
)

// reindexFailBackoff returns the backoff duration for the nth consecutive
// same-ref index failure (n=1 is the first failure). Doubles each additional
// failure, capped at reindexFailBackoffMax.
func reindexFailBackoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	d := reindexFailBackoffBase
	for i := 1; i < n; i++ {
		d *= 2
		if d >= reindexFailBackoffMax {
			return reindexFailBackoffMax
		}
	}
	if d > reindexFailBackoffMax {
		return reindexFailBackoffMax
	}
	return d
}

// enqueueRequest carries a repo path plus the ref snapshot taken at
// Enqueue time. This is the unit flowing from public Enqueue → dedupLoop →
// admitLoop → workerLoop.
type enqueueRequest struct {
	repoPath string
	ref      string // captured at Enqueue time via RefCapture; "" = unknown
	commit   string // commit SHA captured alongside ref; breaker identity (#5726)
}

// Scheduler is constructed once per daemon. It owns:
//   - a bounded job channel (per-repo dedup happens before enqueue),
//   - a worker pool,
//   - per-group link debounce timers,
//   - per-repo algorithm debounce timers,
//   - an RSS-budget ledger that gates dispatch.
type Scheduler struct {
	cfg      Config
	logger   *slog.Logger
	enq      chan enqueueRequest // public enqueue input → dedup → pending queue
	jobs     chan jobToken       // admitted jobs handed to workers
	wake     chan struct{}       // poked when a worker frees budget
	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	// shutdownCtx is cancelled by Stop() so in-flight IndexFn / Incremental /
	// Clone calls — which may have spawned child processes — receive a
	// cancellation signal and can clean up before the daemon exits. Using a
	// dedicated context (rather than reusing the caller-supplied one) keeps
	// the lifecycle strictly under Scheduler control and avoids the zombie
	// accumulation described in issue #2176.
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc

	// algoSem limits the number of concurrent algorithm passes (#2141
	// root-cause C / #2140 hyp-2). Capacity = min(3, max(2, NumCPU/2))
	// unless Config.AlgoCap is set — the auto path is hard-capped at 3
	// cores so indexing never freezes the user's machine. Nil means
	// unbounded (legacy; not used in production).
	algoSem chan struct{}

	mu           sync.Mutex
	inflight     map[string]int64 // repo → predicted MB charged against the ledger
	pendingIndex map[string]bool  // repos already enqueued but not yet running
	// dirty marks repos that received an enqueue WHILE a reindex for that
	// same repo was already in-flight (#5138). Per-repo reindex coalescing:
	// at most one reindex per repo runs at a time; any number of enqueues
	// arriving mid-run collapse into this single boolean. When the in-flight
	// run finishes, runIndex consumes the marker and schedules EXACTLY ONE
	// follow-up reindex — capturing all intervening changes in one pass with
	// no lost update (the marker is set AFTER the run snapshots its input, so
	// a change landing mid-run still triggers the follow-up). N events during
	// a reindex → 1 follow-up, not N. Guarded by mu.
	dirty          map[string]bool
	pendingRefs    map[string]string // repo → ref captured at last Enqueue (overwritten on re-enqueue)
	pendingCommits map[string]string // repo → commit SHA captured at last Enqueue (mirrors pendingRefs; breaker identity #5726)
	pendingQ       []string          // ordered admission queue
	queueLen       int               // pending + admitted-but-not-yet-running
	usedMB         int64             // sum of inflight MB
	linkTimers     map[string]*time.Timer
	linkPending    map[string]bool
	// linkArm is the identity of the CURRENTLY ARMED link timer for a group,
	// and armSeq the monotonic source of those ids (shared with groupAlgoArm;
	// the two keyspaces are independent but the counter need not be). Every arm
	// site (scheduleLinks and fireLinks' defer-retry) goes through
	// armLinkTimerLocked, which allocates a new id.
	//
	// Timer.Stop() cannot stop a timer that has already fired, so CancelGroup
	// alone could not reach the in-flight AfterFunc body of a link pass whose
	// timer fired just before the group was deleted: the body went on to run
	// the deleted group's pass (or, on the gate-deferred path, to re-arm a
	// retry timer and re-register the stage deferral for the deleted group).
	// fireLinks therefore re-checks its own arm id against this map under the
	// same lock hold that acquires the stage token, and CancelGroup deletes the
	// entry — so the delete strictly orders against the fire. Guarded by mu.
	linkArm map[string]uint64
	// groupAlgoArm is the same mechanism for the group-algo family, and it
	// matters MORE there. A link pass that escapes its delete at least runs
	// under a ctx CancelGroup can still cancel; runGroupAlgo's ctx is rooted
	// only at shutdownCtx, so an escaped group-algo pass — Louvain + PageRank +
	// betweenness over the group union, in a subprocess, the heaviest job the
	// daemon runs — has nothing left to cancel it and burns cores to completion
	// for a group that no longer exists. On the gate-deferred path the escape
	// was worse than wasted work: the body called markGroupAlgoDeferredLocked
	// (indexstate.GroupAlgoBegin) after cancelGroupAlgoLocked had already
	// released the pair, leaving the process-global counter permanently +1 and
	// grafel_stats reporting a pass in flight forever.
	groupAlgoArm map[string]uint64
	armSeq       uint64
	// groupCancelSeq is the per-group CANCEL GENERATION: CancelGroup bumps it,
	// and nothing else touches it. It closes the one window the arm ids
	// structurally cannot (#6068).
	//
	// The arm id orders a delete against a timer that has already FIRED. It
	// cannot order a delete against a re-arm that happens LATER, on a path that
	// decided to re-arm while it still held s.mu but calls scheduleGroupAlgo
	// after releasing it: fireGroupAlgo's rerun path, and runLinks' chained
	// group-algo arm. A CancelGroup landing in that gap has already run — it
	// dropped the arm id and cancelled what existed — so the re-arm that follows
	// allocates a FRESH, legitimately-live arm id for a group that is gone, and
	// ~180s later the heaviest pass in the daemon runs for it under a ctx rooted
	// only at shutdownCtx.
	//
	// A continuation therefore captures the generation under the SAME hold that
	// authorised it and re-presents it to scheduleGroupAlgoFor, which refuses to
	// arm if the generation has moved. Chosen over widening the lock hold
	// because runLinks' call site cannot be brought under s.mu at all (cfg.Links
	// runs for minutes between the authorising hold and the arm), so the lock
	// change would fix one site and leave the other — and over a group-existence
	// re-check because the scheduler has no group oracle (Config exposes only
	// GroupsForRepo, keyed by repo) and CancelGroup IS the delete signal it
	// would be asking about.
	//
	// Entries are never deleted, and that is CORRECT BY DESIGN rather than an
	// oversight — do not "tidy" it into a leak fix. Deleting group's entry
	// resets its generation to 0, which is precisely the value a continuation
	// authorised before the very first CancelGroup is still holding; that
	// continuation would then match and re-arm, reopening the window this field
	// exists to close. The generation MUST outlive every other piece of the
	// group's state, including the group itself. The cost is bounded by the
	// number of distinct group names ever cancelled in one daemon lifetime (a
	// uint64 per name, and a group delete is a human-initiated operation), which
	// is not a growth curve worth trading correctness for. Guarded by mu.
	groupCancelSeq map[string]uint64
	// rearmGapHook is a test-only seam; see fireRearmGapHook. Nil in production.
	rearmGapHook atomic.Pointer[func(site, group string)]
	// Per-GROUP algorithm pass (#5349 A3). Mirrors the link timer machinery:
	// debounce timer + pending flag + in-flight cancel func, all keyed by group.
	// Replaces the old per-repo algo timers — algorithms now run once over the
	// assembled group union, chained off link completion.
	groupAlgoTimers  map[string]*time.Timer
	groupAlgoPending map[string]bool
	// groupAlgoCancel holds a per-in-flight-pass identity TOKEN (not a bare
	// cancel func) so a pass that returns LATE can distinguish its own registry
	// entry from a successor's — the same shape linkCancel uses. Blind-deleting
	// here let a late returner drop a live successor's handle, so a subsequent
	// CancelGroup/Stop had nothing to cancel and the successor ran on for a
	// deleted group (#6001).
	groupAlgoCancel map[string]*groupAlgoPassCancel
	// groupAlgoRerun marks a group whose overlay recompute was REQUESTED while a
	// pass was already in flight. The request is deliberately NOT serviced by
	// cancelling the running pass (see scheduleGroupAlgo); instead the flag is
	// consumed when that pass returns, which re-arms the debounce. It is the
	// "the newer request is not silently dropped" half of the let-it-finish
	// contract. Guarded by mu.
	groupAlgoRerun map[string]bool
	// groupAlgoArmedAt records when a group's debounce window FIRST started (and
	// is NOT reset on subsequent re-arms while still pending). It bounds debounce
	// starvation: once the window has been continuously armed for
	// GroupAlgoMaxWait, scheduleGroupAlgo fires promptly instead of resetting the
	// full debounce (#5450). Cleared when the pass fires or is cancelled. Guarded
	// by mu.
	groupAlgoArmedAt map[string]time.Time
	// groupAlgoFireAt records the wall-clock time the currently-armed pass is
	// scheduled to fire, so a re-arm never pushes a pending fire later (#5450).
	// Guarded by mu.
	groupAlgoFireAt map[string]time.Time
	// linkCancel holds a TOKEN for an IN-FLIGHT link pass, keyed by group. Set by
	// scheduleLinks' AfterFunc before runLinks (deriving a per-group ctx from
	// shutdownCtx), cleared when runLinks returns. Lets CancelGroup (a group
	// delete) interrupt an in-flight phantom-edge/link pass for JUST that group
	// without waiting for daemon shutdown.
	//
	// The value is a pointer TOKEN (not the bare CancelFunc) so a completing pass
	// only clears the entry when it is still ITS OWN token: link passes are NOT
	// single-flight (the debounce timer deletes linkTimers[group] when it fires,
	// so a scheduleLinks arriving during an in-flight runLinks arms a fresh timer
	// → a second, overlapping runLinks). Without the identity check, pass-1's
	// completion would blind-delete pass-2's still-live cancel entry, and a
	// CancelGroup landing in that window would find nothing to cancel and let the
	// deleted group's link pass run to completion — the exact re-leak. Guarded by
	// mu.
	linkCancel map[string]*linkPassCancel
	// indexCancel holds the cancel func for an IN-FLIGHT per-repo index job,
	// keyed by repo path. Set by runIndex before the index runs (deriving a
	// per-repo ctx from shutdownCtx), cleared when the job completes. Lets
	// CancelGroup cancel the in-flight reindexes of a deleted group's repos
	// (SIGKILLing the subprocess indexer child) rather than letting them run to
	// completion after the group is gone. Guarded by mu.
	indexCancel  map[string]context.CancelFunc
	indexedRepos map[string]repoStats
	recentLog    []LogEntry

	// deadManAt tracks when the pending queue became non-empty with no
	// admitted jobs. The dead-man goroutine force-admits a job when this
	// exceeds deadManTimeout. Zero means the clock is not running.
	deadManAt time.Time

	// idleSince is the wall-clock time the scheduler last became fully idle
	// (no in-flight index, empty queue, no pending algo/link passes). Zero
	// means "not currently idle". The memReleaseLoop arms FreeOSMemory off
	// this clock and the debounce window (#3648). Guarded by mu.
	idleSince time.Time
	// memReleased records whether FreeOSMemory has already fired for the
	// CURRENT idle period, so we release at most once until activity resumes
	// (which resets it). Guarded by mu.
	memReleased bool

	// --- heavy write-stage gate (#5954). See stagegate.go. ---
	// stageHolder names the exclusive stage currently holding the daemon-wide
	// heavy write-stage token ("links:<group>" / "group-algo:<group>"), or "".
	// The SHARED (index) side of the gate is not counted here: s.inflight
	// already tracks it exactly.
	stageHolder string
	// stageEpoch is the IDENTITY of the current holder, bumped on every
	// acquisition and never reset. stageHolder alone cannot serve as an
	// identity: the two exclusive stages are named "links:<group>" and
	// "group-algo:<group>", so the pass that follows an ABANDONED
	// "group-algo:acme" is very often a fresh "group-algo:acme" — the SAME
	// STRING. Without the epoch, the abandoned pass returning late would find
	// its own name in stageHolder and clear its SUCCESSOR'S token, admitting a
	// third stage beside two live ones (strictly worse than the degrade the
	// abandonment already advertises). releaseStage and setStageCancelLocked
	// therefore match on (name, epoch), not on name.
	stageEpoch int64
	// stageHeldSince is when stageHolder acquired; drives StageGateHoldMax.
	stageHeldSince time.Time
	// stageForfeitedAt is when stageHolder was declared FORFEITED (it blew
	// StageGateHoldMax), or zero. A forfeited holder still HOLDS the gate — the
	// forfeit is a diagnostic, not a release (see reapStageLocked) — so this is
	// what distinguishes "wedged but still doing real work" from "healthy", and
	// it starts the StageGateForfeitGrace clock after which the stage is
	// cancelled and its token reclaimed.
	stageForfeitedAt time.Time
	// stageForfeits counts forfeits since daemon start. Surfaced in Snapshot /
	// StageGateState so a measurement run can assert the gate never fired on
	// legitimate work; any non-zero value means a stage blew a 4h bound.
	stageForfeits int64
	// stageCancel cancels the work of stageHolder (SIGKILLing a group-algo
	// child). Registered by the holder once its context exists; the ONLY caller
	// is the forfeit-grace expiry. nil when the holder registered none.
	stageCancel func()
	// stageDeferSince records, per stage name, when its CURRENT continuous
	// deferral began. Cleared on acquire. Drives both the "deferred for N"
	// observability and the StageGateMaxDefer starvation guard.
	stageDeferSince map[string]time.Time
	// stageDrainFor / stageDrainUntil are the DRAIN BARRIER: the starved stage
	// that raised it, and the bounded deadline after which the barrier lapses so
	// index dispatch can never be blocked indefinitely.
	stageDrainFor   string
	stageDrainUntil time.Time
	// stageBarge holds the live FOREGROUND registrations (see
	// stagegate_barge.go), keyed by a monotonic id from stageBargeSeq. A
	// foreground rebuild indexes OUTSIDE the scheduler — it never populates
	// s.inflight — so without this the gate reads an idle machine for the whole
	// 10–12 minutes of a `grafel reset` and lets a background group-algo pass go
	// co-resident with the largest process on the box. Separate from stageHolder
	// because a barge must coexist with an already-running background stage
	// rather than wait for it, and because it carries its own (much longer)
	// expiry bound. Guarded by mu.
	stageBarge    map[int64]bargeHold
	stageBargeSeq int64
	// stageAdmitBlocked debounces the admit-side "held by a heavy stage" log to
	// one line per blocked period (admitLoop retries once a second).
	stageAdmitBlocked bool
	// groupAlgoDeferred marks groups whose group-algo pass is currently DEFERRED
	// by the gate. While set, the scheduler holds an indexstate.GroupAlgoBegin
	// on the group's behalf so grafel_stats does NOT report an idle daemon
	// across a deferral (an MCP consumer would otherwise read a stale overlay
	// believing the work had settled). Guarded by mu.
	groupAlgoDeferred map[string]bool
}

// jobToken couples a repo path with the predicted MB that admission
// control reserved for it, and the ref that was captured at Enqueue time
// (PH1b). The worker decrements usedMB by this exact amount on completion,
// so partial-credit history updates don't drift.
type jobToken struct {
	repoPath    string
	ref         string // git ref name captured at Enqueue time; "" = unknown
	commit      string // commit SHA captured at Enqueue time; breaker identity (#5726); "" = non-git
	predictedMB int64
}

// repoStats records what we know about each successful index pass.
type repoStats struct {
	LastIndex  time.Time
	LastAlgo   time.Time
	IndexCount int64
	AlgoCount  int64
	LastErr    string
	LastPeakMB int64 // observed peak (history) — 0 if predictor-only
	// LastPeakSrc names WHICH measure LastPeakMB is (peakSrcChildMaxRSS today).
	// #6107: LastPeakMB survives runs that measured nothing, so without this a
	// reader cannot tell a fresh child high-water mark from one carried over
	// from an older full index — the exact ambiguity the completion line was
	// restructured to remove, reappearing on the status wire.
	LastPeakSrc string
	PredictedMB int64 // last predicted MB charged for this repo
	// LastIndexedRef is the git ref the most recent COMPLETED index ran
	// against (#5433). Used by grafel_index_status to let an agent compare its
	// repo's indexed_ref against head_ref and gate on real freshness rather
	// than the process-global is_indexing flag. Empty until a first index
	// completes in this daemon's lifetime.
	LastIndexedRef string

	// FailCommit/FailCount/FailBackoffUntil/FailLoggedAt implement the
	// #5726/#5729 reindex circuit breaker. FailCommit is the commit SHA a
	// same-target reindex attempt most recently failed at (SHA, NOT branch
	// name: a fix commit on the same branch keeps the branch name but changes
	// the SHA, which must reset the breaker; a detached HEAD has no branch name
	// but a valid SHA). FailCount counts consecutive failures at that commit
	// (drives exponential backoff via reindexFailBackoff); FailBackoffUntil is
	// when the breaker next allows a real attempt at FailCommit; FailLoggedAt
	// debounces the skip-log line to at most once per backoff window. A
	// successful index, or a trigger for a DIFFERENT commit, resets all four
	// fields.
	FailCommit       string
	FailCount        int
	FailBackoffUntil time.Time
	FailLoggedAt     time.Time
}

// LogEntry is a single structured event captured for /status. Kept in
// memory only; the daemon's regular log file remains authoritative.
type LogEntry struct {
	Time time.Time `json:"time"`
	Kind string    `json:"kind"`
	Repo string    `json:"repo,omitempty"`
	Msg  string    `json:"msg"`
}

const maxRecentLog = 32

// resolveAlgoCap returns the effective concurrency cap for algorithm passes.
// If cfg.AlgoCap > 0 it is returned as-is (an explicit operator override is
// honored verbatim). Otherwise it is auto-tuned to
// min(3, max(2, runtime.NumCPU()/2)): the project hard-caps indexing/algo
// work at 3 concurrent cores regardless of host size, so a large box never
// gets its CPU saturated and the user's machine stays responsive.
func resolveAlgoCap(cap int) int {
	if cap > 0 {
		return cap
	}
	n := runtime.NumCPU() / 2
	if n < 2 {
		n = 2
	}
	if n > 3 {
		n = 3
	}
	return n
}

// New constructs a scheduler. Start must be called before Enqueue.
func New(cfg Config) *Scheduler {
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	if cfg.LinkDebounce <= 0 {
		cfg.LinkDebounce = 10 * time.Second
	}
	if cfg.GroupAlgoDebounce <= 0 {
		cfg.GroupAlgoDebounce = groupAlgoDebounceFromEnv()
	}
	if cfg.GroupAlgoMaxWait <= 0 {
		cfg.GroupAlgoMaxWait = groupAlgoMaxWaitFromEnv()
	}
	// The max-wait is the ceiling on debounce starvation; a value below the
	// debounce would defeat the coalescing entirely, so clamp it up.
	if cfg.GroupAlgoMaxWait < cfg.GroupAlgoDebounce {
		cfg.GroupAlgoMaxWait = cfg.GroupAlgoDebounce
	}
	// Overlay-freshness sweep cadence (#5403). A caller leaving this at the
	// zero value picks up GRAFEL_OVERLAY_SWEEP_INTERVAL (default 10m; "0"
	// disables). A caller that explicitly set a positive interval keeps it.
	if cfg.OverlaySweepInterval == 0 {
		cfg.OverlaySweepInterval = overlaySweepIntervalFromEnv()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil)).With("pkg", "sched")
	}
	if cfg.MemReleaseDebounce <= 0 {
		cfg.MemReleaseDebounce = memReleaseDebounceDefault
	}
	if cfg.FreeOSMemory == nil {
		cfg.FreeOSMemory = debug.FreeOSMemory
	}
	// Heavy write-stage gate (#5954). On by default; GRAFEL_STAGE_GATE=0 opts
	// out. An explicit StageGateDisabled=true always wins.
	if !cfg.StageGateDisabled && stageGateDisabledFromEnv() {
		cfg.StageGateDisabled = true
	}
	if cfg.StageGateRetry <= 0 {
		cfg.StageGateRetry = stageGateDurationFromEnv("GRAFEL_STAGE_GATE_RETRY", stageGateRetryDefault)
	}
	if cfg.StageGateMaxDefer <= 0 {
		cfg.StageGateMaxDefer = stageGateDurationFromEnv("GRAFEL_STAGE_GATE_MAX_DEFER", stageGateMaxDeferDefault)
	}
	if cfg.StageGateHoldMax <= 0 {
		cfg.StageGateHoldMax = stageGateDurationFromEnv("GRAFEL_STAGE_GATE_HOLD_MAX", stageGateHoldMaxDefault)
	}
	if cfg.StageGateForfeitGrace <= 0 {
		cfg.StageGateForfeitGrace = stageGateDurationFromEnv("GRAFEL_STAGE_GATE_FORFEIT_GRACE", stageGateForfeitGraceDefault)
	}
	if cfg.StageGateDrainMax <= 0 {
		cfg.StageGateDrainMax = stageGateDurationFromEnv("GRAFEL_STAGE_GATE_DRAIN_MAX", stageGateDrainMaxDefault)
	}
	if cfg.StageGateBargeMax <= 0 {
		cfg.StageGateBargeMax = stageGateDurationFromEnv("GRAFEL_STAGE_GATE_BARGE_MAX", stageGateBargeMaxDefault)
	}
	algoCap := resolveAlgoCap(cfg.AlgoCap)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	return &Scheduler{
		cfg:               cfg,
		logger:            cfg.Logger,
		enq:               make(chan enqueueRequest, 64),
		jobs:              make(chan jobToken, cfg.Workers),
		wake:              make(chan struct{}, 1),
		stop:              make(chan struct{}),
		algoSem:           make(chan struct{}, algoCap),
		inflight:          map[string]int64{},
		pendingIndex:      map[string]bool{},
		dirty:             map[string]bool{},
		pendingRefs:       map[string]string{},
		pendingCommits:    map[string]string{},
		linkTimers:        map[string]*time.Timer{},
		linkPending:       map[string]bool{},
		linkArm:           map[string]uint64{},
		groupAlgoArm:      map[string]uint64{},
		groupCancelSeq:    map[string]uint64{},
		groupAlgoTimers:   map[string]*time.Timer{},
		groupAlgoPending:  map[string]bool{},
		groupAlgoCancel:   map[string]*groupAlgoPassCancel{},
		groupAlgoRerun:    map[string]bool{},
		groupAlgoArmedAt:  map[string]time.Time{},
		groupAlgoFireAt:   map[string]time.Time{},
		linkCancel:        map[string]*linkPassCancel{},
		indexCancel:       map[string]context.CancelFunc{},
		indexedRepos:      map[string]repoStats{},
		stageDeferSince:   map[string]time.Time{},
		stageBarge:        map[int64]bargeHold{},
		groupAlgoDeferred: map[string]bool{},
		shutdownCtx:       shutdownCtx,
		shutdownCancel:    shutdownCancel,
	}
}

// Start spins up the dedup goroutine + admission loop + worker pool +
// dead-man switch. Stop reverses it.
func (s *Scheduler) Start() {
	// #5954: publish the foreground-barge bridge. cmd/grafel's rebuild path
	// runs in the same process but has no scheduler handle (daemon.Config
	// carries Rebuild INWARD to where the scheduler is built), so it reaches
	// the gate through the package-level sched.BargeForeground. Registering
	// here rather than at a call site in the wiring layer means the bridge can
	// never be silently left unwired — the failure mode this whole change
	// exists to fix. Withdrawn (identity-checked) in Stop.
	publishBargeBridge(s)
	s.wg.Add(1)
	go s.dedupLoop()
	s.wg.Add(1)
	go s.admitLoop()
	s.wg.Add(1)
	go s.deadManLoop()
	if !s.cfg.MemReleaseDisabled {
		s.wg.Add(1)
		go s.memReleaseLoop()
	}
	// #5403: periodic overlay-freshness sweep for settled groups. Only started
	// when a StaleGroups callback is wired AND the interval is positive.
	if s.cfg.StaleGroups != nil && s.cfg.OverlaySweepInterval > 0 {
		s.wg.Add(1)
		go s.overlaySweepLoop()
	}
	for i := 0; i < s.cfg.Workers; i++ {
		s.wg.Add(1)
		go s.workerLoop()
	}
}

// Stop signals shutdown, cancels any in-flight index jobs (so subprocess
// children receive SIGTERM via exec.CommandContext), and waits for the worker
// pool to drain. It is safe to call Stop more than once; subsequent calls are
// no-ops — the close(s.stop) and shutdownCancel() are wrapped in a sync.Once
// so a second call returns immediately after the first call's wg.Wait()
// completes. This is the fix for the double-Stop panic described in issue #2494.
func (s *Scheduler) Stop() {
	// #5954: withdraw the foreground-barge bridge before draining, so a rebuild
	// racing shutdown gets an inert no-op rather than a hold on a scheduler
	// that is going away. Identity-checked inside, so a newer scheduler that
	// already published (test suites) is not unpublished by this one.
	withdrawBargeBridge(s)
	s.stopOnce.Do(func() {
		// Cancel the shutdown context first so any in-flight IndexFn /
		// Incremental / Clone call that spawned a child process via
		// exec.CommandContext receives SIGTERM immediately. This is the fix for
		// the zombie accumulation described in issue #2176.
		s.shutdownCancel()
		close(s.stop)
	})
	s.wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.linkTimers {
		t.Stop()
	}
	// Same reason as CancelGroup: a link timer that already fired is only
	// reachable through its arm id. fireLinks also checks stopped(), so this is
	// belt-and-braces for a body that passed that check before Stop began.
	s.linkArm = map[string]uint64{}
	for _, t := range s.groupAlgoTimers {
		t.Stop()
	}
	s.groupAlgoArm = map[string]uint64{}
	for _, tok := range s.groupAlgoCancel {
		tok.cancel()
	}
	// #5954: drop every deferred-by-the-gate marker so the process-global
	// indexstate counters return to zero on shutdown (they are balanced
	// Begin/End pairs held on the scheduler's behalf, not per-goroutine defers).
	for g := range s.groupAlgoDeferred {
		s.markGroupAlgoDeferredLocked(g, false)
	}
	s.stageDeferSince = map[string]time.Time{}
	s.stageDrainFor = ""
	s.stageDrainUntil = time.Time{}
	// Live foreground barges are dropped too: their only effect is to make
	// background stages defer, and there are no background stages left to
	// defer. A rebuild goroutine that outlives Stop still holds an id-keyed
	// release closure, but releasing an absent id is a no-op by construction.
	s.stageBarge = map[int64]bargeHold{}
}

// Enqueue requests a (debounced+deduped) reindex of repoPath. The current
// HEAD ref is captured immediately via Config.RefCapture (if configured) so
// the ref is snapshotted at event-fire time, not at eventual dispatch time.
// Safe to call from arbitrary goroutines.
func (s *Scheduler) Enqueue(repoPath string) {
	ref, commit := "", ""
	if s.cfg.RefCapture != nil {
		ref, commit = s.cfg.RefCapture(repoPath)
	}
	s.EnqueueRefCommit(repoPath, ref, commit)
}

// EnqueueRef requests a (debounced+deduped) reindex of repoPath at a
// specific git ref, with no commit SHA. Retained for callers (and tests) that
// only have a ref; the #5726 circuit breaker keys on the commit SHA, so an
// EnqueueRef job carries an empty commit identity. Prefer EnqueueRefCommit on
// paths where the SHA is known (the GitHeadPoller has it as ev.NewSHA).
func (s *Scheduler) EnqueueRef(repoPath, ref string) {
	s.EnqueueRefCommit(repoPath, ref, "")
}

// EnqueueRefCommit requests a (debounced+deduped) reindex of repoPath at a
// specific git ref and commit SHA. Called directly by the GitHeadPoller
// (branch-switch events) where both the new ref and SHA have already been
// observed — no extra git call needed. The commit SHA is the identity the
// #5726/#5729 reindex circuit breaker gates on. Safe to call from arbitrary
// goroutines.
func (s *Scheduler) EnqueueRefCommit(repoPath, ref, commit string) {
	// Issue #3680: drop enqueues for linked worktrees of already-indexed
	// primaries so they never become independent root index jobs (each of
	// which would spawn its own ~100MB store and pressure the RSS budget).
	if s.cfg.SkipEnqueue != nil && s.cfg.SkipEnqueue(repoPath) {
		s.logEvent("enqueue_skipped", repoPath, "linked worktree of indexed primary — not cold-indexed as a new root (#3680)")
		return
	}
	select {
	case s.enq <- enqueueRequest{repoPath: repoPath, ref: ref, commit: commit}:
	case <-s.stop:
	}
}

// dedupLoop forwards from enq into the pending admission queue,
// suppressing duplicates for repos already pending or running. This is
// also where we cancel any scheduled algorithm pass — any new write
// activity in the repo invalidates the pending algo schedule.
//
// Ref handling (PH1b): if a repo is already pending and a new enqueue
// arrives for the same repo with a different ref (branch switch), the
// stored ref is updated to the most-recently-observed one. This ensures
// the next batch runs against the correct branch.
func (s *Scheduler) dedupLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case req := <-s.enq:
			p := req.repoPath
			s.mu.Lock()
			// (Per-repo algo cancellation removed in #5349 A3 — the algorithm
			// pass is now per-group and re-armed off link completion, so a new
			// repo enqueue no longer needs to cancel a per-repo algo timer.)
			if _, running := s.inflight[p]; running {
				// Already running for this repo (#5138): do NOT start a
				// second concurrent reindex. Mark the repo dirty so that
				// when the in-flight run completes, runIndex schedules
				// EXACTLY ONE follow-up that picks up this (and any other
				// mid-run) change. N enqueues during the run collapse into
				// this single boolean → 1 follow-up, not N. Also update the
				// stored ref so the follow-up uses the latest observed ref.
				s.dirty[p] = true
				if req.ref != "" {
					s.pendingRefs[p] = req.ref
				}
				if req.commit != "" {
					s.pendingCommits[p] = req.commit
				}
				s.publishRepoStatesLocked() // #5433: repo just went dirty.
				s.mu.Unlock()
				continue
			}
			if s.pendingIndex[p] {
				// Already pending: update the ref/commit to the latest observed.
				if req.ref != "" {
					s.pendingRefs[p] = req.ref
				}
				if req.commit != "" {
					s.pendingCommits[p] = req.commit
				}
				s.mu.Unlock()
				continue
			}
			s.pendingIndex[p] = true
			s.pendingRefs[p] = req.ref
			s.pendingCommits[p] = req.commit
			s.pendingQ = append(s.pendingQ, p)
			s.queueLen++
			// Start the dead-man clock if nothing is currently
			// running — otherwise there will be a poke on completion.
			if len(s.inflight) == 0 && s.deadManAt.IsZero() {
				s.deadManAt = time.Now()
			}
			s.publishRepoStatesLocked() // #5433: repo just entered the queue.
			s.mu.Unlock()
			s.poke()
		}
	}
}

// poke nudges the admission loop without blocking — the wake channel
// has capacity 1, so multiple poke()s coalesce into one wake-up.
func (s *Scheduler) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// admitLoop dispatches queued jobs to workers, gated by the RSS
// budget. It wakes on (a) new enqueue, (b) job completion, (c) a 1s
// safety tick (paranoid retry in case a poke ever gets lost).
func (s *Scheduler) admitLoop() {
	defer s.wg.Done()
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-s.wake:
		case <-tick.C:
		}
		s.tryAdmit()
	}
}

// deadManLoop runs a periodic check: if the pending queue has been non-empty
// with zero admitted jobs for longer than deadManTimeout, it force-admits the
// smallest predicted job so the daemon cannot wedge indefinitely. The
// force-admit overrides the budget — it is the last-resort relief valve.
//
// The dead-man clock (deadManAt) is set when a job enters the pending queue
// while the inflight map is empty, and cleared when any job is admitted.
func (s *Scheduler) deadManLoop() {
	defer s.wg.Done()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-tick.C:
			s.checkDeadMan()
		}
	}
}

// checkDeadMan inspects the dead-man state and force-admits the smallest
// pending job if the timeout has elapsed with no admitted jobs.
func (s *Scheduler) checkDeadMan() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	// #5954: a held stage token (or a drain barrier) means heavy write-side work
	// IS in progress — just not index work. The dead-man exists to detect
	// "queue non-empty and nothing is happening"; force-admitting here would
	// re-create the exact co-residency the gate removes. Both gate states are
	// independently time-bounded (StageGateHoldMax / stageGateDrainMax), so the
	// dead-man's own liveness guarantee survives the suppression.
	if len(s.pendingQ) == 0 || len(s.inflight) > 0 || s.stageBusyLocked(now) {
		// Queue is clear OR jobs are running — reset the clock.
		s.deadManAt = time.Time{}
		return
	}

	if s.deadManAt.IsZero() {
		// Start the clock: queue has jobs but nothing is running.
		s.deadManAt = now
		return
	}

	if now.Sub(s.deadManAt) < deadManTimeout {
		return // not yet timed out
	}

	// Timeout elapsed: find the smallest predicted job and force-admit it.
	// "Smallest" minimises the memory spike from the override.
	smallestIdx := 0
	smallestMB := s.predictedFor(s.pendingQ[0])
	for i := 1; i < len(s.pendingQ); i++ {
		if mb := s.predictedFor(s.pendingQ[i]); mb < smallestMB {
			smallestMB = mb
			smallestIdx = i
		}
	}
	repo := s.pendingQ[smallestIdx]
	ref := s.pendingRefs[repo]
	commit := s.pendingCommits[repo]
	// Remove from queue (preserve order for remaining entries).
	s.pendingQ = append(s.pendingQ[:smallestIdx], s.pendingQ[smallestIdx+1:]...)
	delete(s.pendingRefs, repo)
	delete(s.pendingCommits, repo)
	s.inflight[repo] = smallestMB
	s.publishIndexStateLocked()
	s.usedMB += smallestMB
	stuckFor := now.Sub(s.deadManAt).Truncate(time.Second)
	s.deadManAt = time.Time{} // reset clock; the job is now admitted
	s.logEventLocked("admit_deadman", repo,
		"force-admitted after "+stuckFor.String()+" with no progress; predicted="+formatMB(smallestMB))
	s.logger.Info("sched: dead-man: force-admitting",
		"repo", repo, "predicted_mb", smallestMB, "stuck_for", stuckFor)

	tok := jobToken{repoPath: repo, ref: ref, commit: commit, predictedMB: smallestMB}
	// Dispatch asynchronously so we don't hold the lock while blocking on
	// the jobs channel. The worker pool is guaranteed to drain the channel
	// because the pool size >= 1.
	go func() {
		select {
		case s.jobs <- tok:
		case <-s.stop:
		}
	}()
}

// memReleaseLoop polls the scheduler's activity counters and, once it has
// been fully idle for MemReleaseDebounce, calls FreeOSMemory exactly once to
// return the retained Go heap arena to the OS (#3648).
//
// Why this is needed: a full reindex transiently allocates several GB of
// heap, then frees it back to the GO RUNTIME — but Go keeps that dirty arena
// as a high-water mark and only madvise()s it back to the OS lazily. On a
// memory-pressured host macOS swaps those idle dirty pages out, inflating the
// process footprint to multiple GB (vmmap on the live daemon: 5.5GB footprint,
// 5.5GB swapped, only 176MB actually resident). FreeOSMemory forces the GC +
// scavenge so the OS reclaims the pages instead of swapping them.
//
// Why debounced (not per-index): FreeOSMemory is a synchronous
// stop-the-world GC followed by a scavenge — expensive. The daemon reindexes
// often under file-event churn (~1/min), so firing it after every index would
// thrash. We instead fire at most once per idle period, after activity has
// settled for the debounce window.
func (s *Scheduler) memReleaseLoop() {
	defer s.wg.Done()
	tick := time.NewTicker(memReleaseTick)
	defer tick.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-tick.C:
			s.maybeReleaseMemory(time.Now())
		}
	}
}

// publishIndexStateLocked mirrors the current in-flight index count to the
// process-global indexstate record so the in-daemon MCP server can surface
// `is_indexing` in grafel_stats without a scheduler reference (#P5). Must be
// called with s.mu held, immediately after any mutation of s.inflight.
func (s *Scheduler) publishIndexStateLocked() {
	indexstate.Set(len(s.inflight))
	s.publishRepoStatesLocked()
}

// publishRepoStatesLocked mirrors the scheduler's per-repo index state to the
// process-global indexstate record so the in-daemon MCP server can answer
// grafel_index_status WITHOUT a scheduler reference or a group-graph load
// (#5433). The derivation matches the doc:
//
//	inflight[repo]>0      → indexing  (and if also dirty → dirty, the strongest
//	                        signal: indexing now AND a follow-up already pending)
//	pendingIndex[repo]    → queued    (enqueued, not yet running)
//	otherwise             → current
//
// A repo is included if the scheduler knows about it at all: it is currently
// inflight/queued/dirty, OR it has a completed-index record in indexedRepos.
// Must be called with s.mu held.
func (s *Scheduler) publishRepoStatesLocked() {
	// Union of every repo the scheduler has any state for.
	seen := make(map[string]struct{})
	add := func(p string) { seen[p] = struct{}{} }
	for p := range s.inflight {
		add(p)
	}
	for p, v := range s.pendingIndex {
		if v {
			add(p)
		}
	}
	for p, v := range s.dirty {
		if v {
			add(p)
		}
	}
	for p := range s.indexedRepos {
		add(p)
	}

	out := make([]indexstate.RepoState, 0, len(seen))
	for p := range seen {
		rs := indexstate.RepoState{Path: p, HeadRef: s.pendingRefs[p]}
		if st, ok := s.indexedRepos[p]; ok {
			rs.IndexedRef = st.LastIndexedRef
		}
		dirty := s.dirty[p]
		rs.Dirty = dirty
		switch {
		case s.inflight[p] > 0 && dirty:
			// Indexing now, but changes arrived mid-run → a follow-up is
			// already pending. Surface the stronger "dirty" signal so an agent
			// knows the current run will not be the last word.
			rs.State = indexstate.StateDirty
		case s.inflight[p] > 0:
			rs.State = indexstate.StateIndexing
		case s.pendingIndex[p]:
			rs.State = indexstate.StateQueued
		default:
			rs.State = indexstate.StateCurrent
		}
		out = append(out, rs)
	}
	indexstate.SetRepoStates(out)
}

// workInFlightLocked reports whether heavy work is ACTUALLY EXECUTING right
// now: an index job on a worker, an exclusive stage holding the gate, or
// foreground work holding a barge. Must be called with s.mu held.
//
// This is the predicate FreeOSMemory is gated on, and only this one. Returning
// the heap arena to the OS under genuinely in-flight work is paid straight back
// (and costs a stop-the-world scavenge to do it), which is what busyLocked was
// protecting against and what must not regress.
//
// A BARGE counts, and did not before: a foreground rebuild is the single
// largest process on the machine and is invisible to s.inflight (it never
// enqueues), so the pre-split predicate would happily scavenge mid-rebuild.
//
// THE BARGE IS ALSO THE EXTENSION POINT for heavy work that lives outside the
// scheduler entirely. The post-rebuild analytics path (appendRebuildHistory) is
// a bare goroutine that materialises the corpus and is invisible to both
// s.inflight and this predicate; registering it via bargeForeground is all that
// is needed to make it both gate-visible and release-visible, with no change
// here. Barge holds are id-keyed (not name-keyed), so independent registrations
// never collide, never overwrite one another, and each release drops only its
// own id — two subsystems can register concurrently without coordinating.
//
// It REAPS the barge first, per bargeHeldLocked's contract. That is not
// bookkeeping hygiene here, it is the liveness bound: this predicate gates
// FreeOSMemory, and a leaked barge (a foreground goroutine that vanished without
// unwinding its release) would otherwise read as in-flight work forever and
// starve the heap release on an idle daemon — with nothing else to reap it,
// since every other reap site is a GATE decision and an idle daemon makes none.
// Reaping stops that at StageGateBargeMax. The stage holder needs no equivalent
// reap: this predicate must report a forfeited-but-resident holder as in-flight
// (it IS running), and its own bound is enforced by the gate paths.
//
// `now` is threaded from the caller rather than read as time.Now() here, because
// the caller (maybeReleaseMemory) already takes an injected clock so tests can
// drive the idle/debounce logic synthetically. Reading the wall clock instead
// would work today only because the existing tests seed bargeHold.since from
// real time; a test that advanced a synthetic `now` past StageGateBargeMax would
// silently fail to reap, and the bound this reap exists to enforce would go
// untested exactly where it was being asserted.
func (s *Scheduler) workInFlightLocked(now time.Time) bool {
	s.reapBargeLocked(now)
	return len(s.inflight) > 0 || s.stageHolder != "" || s.bargeHeldLocked()
}

// workPendingLocked reports whether heavy work is QUEUED OR WAITING but not
// executing: queued index jobs, an armed link/group-algo debounce, a stage the
// gate is deferring, or a drain barrier. Must be called with s.mu held.
//
// Split out of busyLocked because conflating the two starved the heap release.
// busyLocked treated "a stage is deferred" as work in flight, and a deferred
// stage that cannot acquire stays in stageDeferSince (and keeps
// groupAlgoPending set) for as long as it is blocked — so a single wedged gate
// meant the engine went 8+ hours without ONE FreeOSMemory, and its 1290–2112 MB
// "idle floor" was largely an un-returned watermark rather than live data.
//
// A stage that is merely waiting is not doing work, and the debounce gap is the
// BEST moment to scavenge, not the worst: the group-algo debounce is 180s
// against a 30s MemReleaseDebounce, so the pre-split rule was holding the arena
// through a two-and-a-half-minute idle window on purpose. The cost of releasing
// there is one extra tens-of-ms scavenge per cascade; the cost of not releasing
// was unbounded retention.
func (s *Scheduler) workPendingLocked() bool {
	if s.queueLen > 0 || len(s.pendingQ) > 0 {
		return true
	}
	if s.stageDrainFor != "" || len(s.stageDeferSince) > 0 {
		return true
	}
	// A live FOREGROUND BARGE is deliberately NOT tested here: it is work IN
	// FLIGHT, not pending, and workInFlightLocked already reads it (see #5954 and
	// the barge paragraph on that function's doc comment). Duplicating it here
	// would be dead weight — busyLocked ORs the two predicates — and would wrongly
	// report a running rebuild as "queued" to any future solo caller of this one.
	for _, p := range s.groupAlgoPending {
		if p {
			return true
		}
	}
	for _, p := range s.linkPending {
		if p {
			return true
		}
	}
	return false
}

// busyLocked reports whether any indexing-related work is in flight OR pending.
// Must be called with s.mu held. Callers that specifically mean "is anything
// executing" want workInFlightLocked instead.
func (s *Scheduler) busyLocked(now time.Time) bool {
	return s.workInFlightLocked(now) || s.workPendingLocked()
}

// maybeReleaseMemory is the testable core of the idle-release trigger. It
// tracks when the scheduler became idle and, once idle for the debounce
// window, invokes FreeOSMemory exactly once (until the next busy→idle
// transition). Exposed as a method (not an inline closure) so tests can drive
// it with a synthetic clock and a stub FreeOSMemory. It is safe to call
// repeatedly; only the first post-debounce call in an idle period releases.
func (s *Scheduler) maybeReleaseMemory(now time.Time) {
	s.mu.Lock()
	// Gated on IN-FLIGHT work only, never on pending work — see
	// workPendingLocked for the wedge that conflating them caused.
	if s.workInFlightLocked(now) {
		// Activity resumed (or never settled): reset the idle clock so the
		// next idle period must serve out a fresh debounce, and re-arm the
		// one-shot release.
		s.idleSince = time.Time{}
		s.memReleased = false
		s.mu.Unlock()
		return
	}
	if s.idleSince.IsZero() {
		s.idleSince = now
		s.mu.Unlock()
		return
	}
	if s.memReleased || now.Sub(s.idleSince) < s.cfg.MemReleaseDebounce {
		s.mu.Unlock()
		return
	}
	// Idle long enough and not yet released this period — fire once.
	s.memReleased = true
	idleFor := now.Sub(s.idleSince).Truncate(time.Second)
	free := s.cfg.FreeOSMemory
	s.logEventLocked("mem_release", "",
		"idle "+idleFor.String()+" — returning retained heap to OS (FreeOSMemory)")
	s.mu.Unlock()

	// Call FreeOSMemory OUTSIDE the lock: it triggers a stop-the-world GC +
	// scavenge that can take tens of ms, and we must not stall enqueue/admit
	// paths (which take s.mu) for that duration.
	if free != nil {
		t0 := time.Now()
		free()
		s.logger.Info("sched: returned idle heap to OS",
			"idle_for", idleFor, "freeosmemory_took", time.Since(t0).Truncate(time.Millisecond))
	}
}

// overlaySweepLoop periodically asks the caller-supplied StaleGroups callback
// which groups have a STALE on-disk overlay and re-arms a (debounced + CPU-
// capped) group-algo pass for each (#5403). This is the settled-group half of
// the overlay-staleness fix: scheduleGroupAlgo otherwise only fires off a link
// pass, i.e. only for ACTIVELY-reindexed groups, so a settled group whose
// overlay drifted stale would never be recomputed until its next reindex.
//
// The sweep itself is cheap — StaleGroups does only per-group stat-compares —
// and the actual recompute it triggers reuses the existing debounce + AlgoCap
// path, so it cannot uncork an uncapped pass. The interval is enforced >= the
// group-algo debounce by config, and re-arming a group that already has a
// pending/in-flight pass is suppressed, so the sweep can never thrash.
func (s *Scheduler) overlaySweepLoop() {
	defer s.wg.Done()
	tick := time.NewTicker(s.cfg.OverlaySweepInterval)
	defer tick.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-tick.C:
			s.sweepStaleOverlays()
		}
	}
}

// sweepStaleOverlays is the testable core of the overlay-freshness sweep. It
// queries StaleGroups and, for each returned group that does NOT already have a
// pending or in-flight group-algo pass, calls scheduleGroupAlgo (which debounces
// and runs the CPU-capped pass). Skipping already-armed/running groups is what
// keeps the sweep from re-arming faster than a pass can settle. Exposed as a
// method so tests can drive it directly with a fake StaleGroups set.
func (s *Scheduler) sweepStaleOverlays() {
	if s.cfg.StaleGroups == nil {
		return
	}
	stale := s.cfg.StaleGroups()
	for _, g := range stale {
		s.mu.Lock()
		busy := s.groupAlgoBusyLocked(g)
		s.mu.Unlock()
		if busy {
			// A pass is already pending or running for this group — its
			// completion will write a fresh overlay; do not re-arm (and reset the
			// debounce) underneath it.
			continue
		}
		s.logEvent("overlay_sweep_stale", "", g+": stale overlay — re-arming group-algo (#5403)")
		s.scheduleGroupAlgo(g)
	}
}

// groupAlgoBusyLocked reports whether a group already has a debounced group-algo
// pass armed OR an in-flight one running. Used by the overlay sweep to avoid
// re-arming (and resetting the debounce of) a pass that is already on its way.
// MUST be called with s.mu held.
func (s *Scheduler) groupAlgoBusyLocked(group string) bool {
	if s.groupAlgoPending[group] {
		return true
	}
	if _, running := s.groupAlgoCancel[group]; running {
		return true
	}
	return false
}

// tryAdmit walks the pending queue head-first and dispatches every job
// whose predicted MB fits the remaining budget. Jobs that don't fit
// stay in place; head-of-line blocking is intentional so the very
// largest repo cannot starve forever behind a stream of small ones.
//
// Edge case: if a single job's prediction exceeds the entire budget,
// we admit it anyway as long as nothing else is running — otherwise it
// would never run. The log records this as `admit_oversize`.
func (s *Scheduler) tryAdmit() {
	s.mu.Lock()
	for len(s.pendingQ) > 0 {
		// #5954: hold index dispatch while an exclusive heavy stage (link pass /
		// group-algo) owns the daemon-wide stage token, or while a starved stage
		// has raised the drain barrier. The queue is untouched — every release
		// pokes the admission loop, and the 1s safety tick is a second path back
		// here, so nothing is lost.
		if !s.allowIndexAdmitLocked(time.Now()) {
			if !s.stageAdmitBlocked {
				s.stageAdmitBlocked = true
				s.logEventLocked("admit_stage_defer", s.pendingQ[0],
					"holding index dispatch — "+s.stageBlockReasonLocked()+" (#5954)")
				s.logger.Info("sched: holding index dispatch behind a heavy write stage",
					"reason", s.stageBlockReasonLocked(), "queued", len(s.pendingQ))
			}
			s.mu.Unlock()
			return
		}
		s.stageAdmitBlocked = false
		repo := s.pendingQ[0]
		ref := s.pendingRefs[repo]
		commit := s.pendingCommits[repo]
		predicted := s.predictedFor(repo)
		// Admission rule.
		if s.cfg.BudgetMB > 0 {
			if s.usedMB+predicted > s.cfg.BudgetMB {
				// Allow a single oversize job through ONLY when the
				// ledger is empty — otherwise nothing would ever
				// release the budget to make room.
				if len(s.inflight) == 0 && predicted > s.cfg.BudgetMB {
					s.logEventLocked("admit_oversize", repo, "predicted MB exceeds budget; running solo")
				} else {
					s.logEventLocked("admit_defer", repo,
						"predicted="+formatMB(predicted)+" used="+formatMB(s.usedMB)+
							" budget="+formatMB(s.cfg.BudgetMB))
					s.mu.Unlock()
					return
				}
			}
		}
		// Pop and dispatch.
		s.pendingQ = s.pendingQ[1:]
		delete(s.pendingRefs, repo)
		delete(s.pendingCommits, repo)
		s.inflight[repo] = predicted
		s.publishIndexStateLocked()
		s.usedMB += predicted
		s.deadManAt = time.Time{} // job admitted — reset dead-man clock
		s.logEventLocked("admit_ok", repo,
			"predicted="+formatMB(predicted)+" used="+formatMB(s.usedMB)+" ref="+ref)
		tok := jobToken{repoPath: repo, ref: ref, commit: commit, predictedMB: predicted}
		s.mu.Unlock()
		// Block on jobs channel — workers are sized to drain this
		// without deadlock because admission already ensures we are
		// within the worker pool.
		select {
		case s.jobs <- tok:
		case <-s.stop:
			return
		}
		s.mu.Lock()
	}
	s.mu.Unlock()
}

// predictedFor returns the predicted MB for a repo, preferring history
// over the cheap source-size predictor. Always returns at least 1.
func (s *Scheduler) predictedFor(repoPath string) int64 {
	if s.cfg.History != nil {
		if mb := s.cfg.History.Predict(repoPath); mb > 0 {
			return mb
		}
	}
	if s.cfg.Predict != nil {
		if mb := s.cfg.Predict(repoPath); mb > 0 {
			return mb
		}
	}
	return 1
}

// workerLoop pulls admitted jobs and runs them under a per-repo
// serialisation lock. Concurrency is bounded both by the worker pool
// AND by RSS-budget admission.
func (s *Scheduler) workerLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case tok, ok := <-s.jobs:
			if !ok {
				return
			}
			s.runIndex(tok)
		}
	}
}

// runIndex executes IndexFn, then releases the reserved budget,
// records observed RSS into history, and schedules downstream link +
// algo passes.
func (s *Scheduler) runIndex(tok jobToken) {
	repoPath := tok.repoPath

	// #6068 (one hop upstream of runLinks): the success path below chains
	// scheduleLinks for every group this repo belongs to, after an index that
	// runs for MINUTES and outside every lock. A CancelGroup landing in that
	// span is invisible to everything downstream — by the time the chained link
	// timer fires, fireLinks reads the POST-delete generation and matches it, so
	// the group-algo guard passes and the heaviest pass in the daemon runs for a
	// group that is gone. This is reachable rather than theoretical: CancelGroup
	// deliberately leaves running any reindex whose repo a surviving group still
	// references (repoBelongsOnlyToLocked), and DeleteGroup cancels BEFORE it
	// tears the registry down, so GroupsForRepo can still name the deleted group
	// when this returns.
	//
	// So snapshot the cancel GENERATIONS under the hold that dequeues this job —
	// the hold that AUTHORISES the whole downstream chain — and re-present them
	// at the arm.
	//
	// The snapshot is of groupCancelSeq WHOLESALE, not of this repo's group
	// membership, and that distinction is the whole correctness argument. A
	// membership-keyed capture has nothing to say about a group that joins after
	// it is taken, and "nothing to say" can only be resolved as an exemption —
	// which reopens the hazard through exactly the window described above: repo
	// joins gB after the capture, gB is deleted, the terminal membership read
	// still names gB because teardown has not finished, and an exempt arm goes
	// live for a deleted group. Snapshotting generations instead turns an absent
	// name into the positive claim "generation 0 as of the hold", which a later
	// CancelGroup contradicts and the guard below then catches.
	//
	// Absent-means-0 costs nothing in over-suppression, because 0 is also the
	// current generation of every group that has never been cancelled: a
	// brand-new group arms (0 == 0), and a group re-registered under a
	// previously-cancelled name arms too (the snapshot carries that name's real
	// generation, so it matches). Only a cancel landing INSIDE this index's
	// window can produce a mismatch.
	//
	// Membership is read before the hold because cfg.GroupsForRepo is an
	// external callback and must not run under s.mu (#6060); it is only needed
	// to keep the snapshot covering names the map has not seen yet.
	var startGroups []string
	if s.cfg.GroupsForRepo != nil {
		startGroups = s.cfg.GroupsForRepo(repoPath)
	}

	s.mu.Lock()
	linkGen := make(map[string]uint64, len(s.groupCancelSeq)+len(startGroups))
	for g, v := range s.groupCancelSeq {
		linkGen[g] = v
	}
	for _, g := range startGroups {
		linkGen[g] = s.groupGenLocked(g)
	}
	s.pendingIndex[repoPath] = false
	s.queueLen--
	// #5138 no-lost-update: clear the dirty marker BEFORE the index runs so
	// that this run is treated as covering the repo state as of now. The
	// actual extraction (below) snapshots the working tree shortly after.
	// Any enqueue arriving AFTER this point re-sets dirty[repoPath] (via
	// dedupLoop, since inflight[repoPath] is still set), guaranteeing the
	// post-run check sees it and schedules exactly one follow-up. Clearing
	// it AFTER the run instead would race: a change landing between the
	// snapshot and the clear would be silently dropped.
	delete(s.dirty, repoPath)

	// #5726/#5729 circuit breaker: a repo genuinely over the FlatBuffers
	// 2-GiB cap fails the SAME way on every attempt at the SAME COMMIT — the
	// fbwriter fail-soft path recovers the marshal panic but leaves the input
	// state (watcher fs events, git-HEAD poll) unaware of the failure, so any
	// further working-tree churn at the same commit re-fires a doomed reindex
	// immediately. The breaker gates on the commit SHA (tok.commit), which is
	// stable across same-commit churn (hot-loop bounded) yet changes on every
	// new commit (a fix commit resets it immediately) and is present for a
	// detached HEAD (the branch name is not). If the breaker is open for this
	// exact commit, skip the real attempt (serve last-good graph.fb) instead
	// of re-marshaling.
	breakerStats := s.indexedRepos[repoPath]
	now := time.Now()
	skip := breakerStats.FailCommit == tok.commit && now.Before(breakerStats.FailBackoffUntil)
	shouldLogSkip := false
	if skip {
		// Log at most once per backoff window (tracking the actual window for
		// the current fail count, not a fixed interval), so a repo hot-looping
		// at the cap produces a handful of lines instead of the 74x storm the
		// breaker exists to prevent.
		window := reindexFailBackoff(breakerStats.FailCount)
		shouldLogSkip = breakerStats.FailLoggedAt.IsZero() || now.Sub(breakerStats.FailLoggedAt) >= window
		if shouldLogSkip {
			breakerStats.FailLoggedAt = now
			s.indexedRepos[repoPath] = breakerStats
		}
	}
	s.mu.Unlock()

	t0 := time.Now()

	var err error
	// observedPeakMB is the peak memory figure reported for this run, and
	// peakSrc names WHICH measure it is (#6107). The two travel together and
	// both are logged: "peak" is ambiguous in this codebase — an absolute
	// child RSS high-water mark and a daemon RSS delta are different numbers
	// with different baselines, and reporting either under a bare "peak"
	// field is how this metric came to be misread for an entire epic.
	var observedPeakMB int64
	peakSrc := peakSrcUnmeasured

	// Cross-path mutual exclusion (foreground group-rebuild ⇄ scheduler): the
	// foreground rebuild path in cmd/grafel indexes a repo directly, bypassing
	// this scheduler's per-repo in-flight guard, and both write the same
	// graph.fb. Yield to a foreground rebuild that owns (or intends to own) this
	// repo: skip THIS attempt without racing its write, and schedule a single
	// delayed retry so the repo is still reindexed once the rebuild releases.
	// A background claim also serialises the scheduler against a concurrent
	// rebuild that has not yet marked foreground intent.
	yielded := false
	backgroundRelease := func() {}
	if !skip {
		if rel, ok := repolock.DefaultRegistry.TryClaimBackground(repoPath); ok {
			backgroundRelease = rel
		} else {
			yielded = true
			s.logEvent("index_yield_foreground", repoPath,
				"foreground group-rebuild owns this repo — skipping concurrent reindex, retrying after backoff")
			s.logger.Info("sched: yielding repo to foreground group-rebuild (avoids concurrent graph.fb rewrite)",
				"repo", repoPath, "ref", tok.ref)
		}
	}
	defer backgroundRelease()

	if skip {
		err = fmt.Errorf("reindex circuit open: %d consecutive failure(s) at commit %q, retrying after backoff (#5726/#5729)", breakerStats.FailCount, tok.commit)
		if shouldLogSkip {
			s.logEvent("index_circuit_skip", repoPath,
				fmt.Sprintf("skipping reindex — %d consecutive failures at commit=%s (ref=%s), serving last-good graph, backoff until %s (#5726/#5729)",
					breakerStats.FailCount, tok.commit, tok.ref, breakerStats.FailBackoffUntil.Format(time.RFC3339)))
			s.logger.Warn("sched: reindex circuit open — skipping repeated-failure reindex, serving last-good graph",
				"repo", repoPath, "commit", tok.commit, "ref", tok.ref, "fail_count", breakerStats.FailCount,
				"backoff_until", breakerStats.FailBackoffUntil)
		}
	} else if !yielded {
		s.logEvent("index_start", repoPath, "predicted="+formatMB(tok.predictedMB)+" ref="+tok.ref)
		// Observability: log goroutine identity + ref so concurrent-indexer
		// regressions are diagnosable without a pprof trace (#2141).
		s.logger.Info("indexer: starting", "repo", repoPath, "ref", tok.ref, "goroutine_id", goroutineID())

		// #6107: there is deliberately NO in-daemon RSS sampler here. The
		// figure one produces — this process's RSS minus its RSS at job start
		// — is not this job's memory: it collapses to 0 in exactly the
		// long-lived-daemon steady state that matters, and it cannot be
		// attributed to one of several jobs sharing the address space. The
		// measurement, the evidence, and why no sampler placement fixes it are
		// in childpeak.go. The only honest peak comes from the index child's
		// kernel high-water mark, read below.
		//
		// S3: attempt incremental file-level reindex before the full index.
		// Only tried when the Incremental callback is configured AND the
		// incremental toggle is active.
		//
		// Issue #2397: consult s.cfg.ExtractorConfig.IsIncrementalEnabled()
		// (single source of truth) instead of the private incrementalEnabled()
		// helper that read GRAFEL_INCREMENTAL_REINDEX directly. The nil-
		// receiver method falls through to the env-var for backward compat.
		//
		// On success (res.Done=true) we skip the full reindex.
		// On fallback (res.Done=false) we log the reason and fall through normally.
		// Derive a PER-REPO cancel context from shutdownCtx (issue #2176 still
		// holds: rooted at shutdownCtx so daemon Stop() cancels it too). Storing
		// the per-repo cancel lets CancelGroup interrupt the in-flight reindexes
		// of a DELETED group's repos — SIGKILLing the subprocess indexer child —
		// instead of letting them burn CPU to completion after the group is gone.
		jobCtx, jobCancel := context.WithCancel(s.shutdownCtx)
		s.mu.Lock()
		s.indexCancel[repoPath] = jobCancel
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.indexCancel, repoPath)
			s.mu.Unlock()
			jobCancel()
		}()

		incrementalDone := false
		// Issue #5726 / epic #5729 — defense in depth. An index can panic for
		// reasons the fbwriter fail-soft doesn't catch (e.g. a flatbuffers 2-GiB
		// abort raised inside the streaming WriteEntity path, or any other bug in
		// an extractor). A panic here would unwind through the worker goroutine and
		// abort the ENTIRE daemon (launchd relaunches it → 60–90s reindex outage,
		// every MCP bridge severed). Recover so ANY index panic becomes a normal
		// error: the worker keeps serving, the reserved budget is released via the
		// existing err path below, and we log a clear degraded-state message.
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("index panicked (recovered): %v", r)
					s.logEvent("index_panic", repoPath, fmt.Sprintf("recovered panic during index: %v", r))
					s.logger.Error("sched: index panicked — recovered; daemon continues in degraded state (repo left at last-good graph)",
						"repo", repoPath, "ref", tok.ref, "panic", r, "stack", string(debug.Stack()))
				}
			}()

			if s.cfg.Incremental != nil && s.cfg.ExtractorConfig.IsIncrementalEnabled() {
				res := s.cfg.Incremental(jobCtx, repoPath, tok.ref)
				if res.Done {
					incrementalDone = true
					s.logEvent("incremental_ok", repoPath,
						"changed_files="+itoa(int64(res.ChangedFiles)))
				} else {
					s.logEvent("incremental_fallback", repoPath,
						"reason="+res.FallbackReason)
					s.logger.Info("sched: incremental fallback", "repo", repoPath, "reason", res.FallbackReason)
				}
			}

			if !incrementalDone && s.cfg.Index != nil {
				err = s.cfg.Index(jobCtx, repoPath, tok.ref)
			}
		}()

		// #6107: the index child's kernel high-water RSS is this run's peak,
		// and the only figure the daemon can honestly produce. On the default
		// path the index heap lives and dies in `grafel index-internal`, whose
		// pages the daemon never touches. wait4 fills ru_maxrss over the
		// child's whole lifetime, so unlike a sampler it cannot miss a peak
		// between ticks. Records stamped before t0 belong to an earlier run and
		// are dropped by takeChildPeakRSSMB rather than misattributed. No
		// child, or no rusage on this platform, leaves peakSrcUnmeasured.
		if mb, ok := takeChildPeakRSSMB(repoPath, t0); ok {
			observedPeakMB = mb
			peakSrc = peakSrcChildMaxRSS
		}

		// Release the cross-path claim the INSTANT the graph.fb write is done —
		// BEFORE the stats/follow-up/poke section below. The background claim
		// only needs to cover the write; the scheduler already serialises
		// against itself via inflight[repoPath]. Releasing at end-of-function
		// (defer) instead would leave the claim held while this run re-enqueues
		// its own #5138 coalesced follow-up and pokes admission — a second
		// worker could then admit that follow-up, have TryClaimBackground fail
		// against our not-yet-released claim, and wrongly "yield to a foreground
		// rebuild" against the scheduler's OWN just-completed run (delaying the
		// no-lost-update follow-up up to yieldRetryDelay + phantom logs). The
		// idempotent defer below still covers the skip/yield/early-return paths.
		backgroundRelease()
	}

	s.mu.Lock()
	stats := s.indexedRepos[repoPath]
	if !skip && !yielded {
		stats.LastIndex = time.Now()
		stats.IndexCount++
		stats.PredictedMB = tok.predictedMB
		// #5433: record the ref this completed index ran against so
		// grafel_index_status can report indexed_ref per repo.
		stats.LastIndexedRef = tok.ref
		// Deliberately NOT gated on err == nil, unlike History.Record below.
		// The asymmetry is the point: LastPeakMB is diagnostic state surfaced
		// by `grafel status`, and the peak of a run that died is the single
		// most useful number there when the question is "did it get OOM
		// killed". History.Record is a budget input consumed by admission, and
		// a run that aborted before its true peak would bias that budget low —
		// the dangerous direction. Same measurement, opposite risk.
		if observedPeakMB > 0 {
			stats.LastPeakMB = observedPeakMB
			stats.LastPeakSrc = peakSrc
		}
	}
	if err != nil {
		stats.LastErr = err.Error()
		if !skip {
			// A real attempt failed: bump (or start) the breaker for this
			// commit and arm exponential backoff. A skip does not re-bump —
			// the failure it reflects was already recorded.
			if stats.FailCommit == tok.commit {
				stats.FailCount++
			} else {
				stats.FailCommit = tok.commit
				stats.FailCount = 1
				stats.FailLoggedAt = time.Time{}
			}
			stats.FailBackoffUntil = time.Now().Add(reindexFailBackoff(stats.FailCount))
		}
	} else if !yielded {
		stats.LastErr = ""
		// Success resets the breaker entirely — including for a DIFFERENT
		// commit than FailCommit, which is already implied since a fresh
		// commit never matched the open breaker in the first place.
		stats.FailCommit = ""
		stats.FailCount = 0
		stats.FailBackoffUntil = time.Time{}
		stats.FailLoggedAt = time.Time{}
	}
	s.indexedRepos[repoPath] = stats
	delete(s.inflight, repoPath)
	s.publishIndexStateLocked()
	s.usedMB -= tok.predictedMB
	if s.usedMB < 0 {
		s.usedMB = 0
	}
	// #5138: consume the dirty marker while still holding the lock and
	// inflight[repoPath] is already cleared. If any enqueue arrived during
	// this run it set dirty[repoPath]=true; schedule EXACTLY ONE follow-up
	// reindex to capture all those coalesced changes in a single pass.
	// Reading-and-clearing under the lock makes this atomic w.r.t. dedupLoop:
	// either an enqueue landed before this point (caught here → one
	// follow-up) or it lands after (sees inflight cleared → enqueues a fresh
	// job normally). No double-run, no lost update.
	followUp := s.dirty[repoPath]
	if followUp {
		delete(s.dirty, repoPath)
	}
	// Prefer the latest observed ref/commit recorded while dirty; fall back to
	// the values this run used. dedupLoop overwrites pendingRefs/pendingCommits
	// on every mid-run enqueue, so they hold the most recent branch + commit
	// for the follow-up (the commit is the breaker identity, so it must ride
	// along too — otherwise a same-commit follow-up would carry an empty commit
	// and bypass the breaker).
	followUpRef := tok.ref
	if r, ok := s.pendingRefs[repoPath]; ok && r != "" {
		followUpRef = r
	}
	followUpCommit := tok.commit
	if c, ok := s.pendingCommits[repoPath]; ok && c != "" {
		followUpCommit = c
	}
	s.mu.Unlock()

	if yielded {
		// Do NOT re-enqueue immediately: the foreground rebuild that owns this
		// repo is still running, so an instant retry would busy-loop (yield →
		// re-enqueue → admit → yield). Retry once after a bounded backoff; the
		// rebuild will have released its claim by then in the common case, and
		// EnqueueRefCommit dedups so overlapping retries coalesce into one.
		s.logEvent("reindex_yield_retry", repoPath,
			"scheduling delayed reindex retry after foreground rebuild backoff")
		time.AfterFunc(yieldRetryDelay, func() {
			s.EnqueueRefCommit(repoPath, followUpRef, followUpCommit)
		})
	} else if followUp {
		s.logEvent("reindex_coalesced_followup", repoPath,
			"changes landed during in-flight reindex — scheduling single follow-up (#5138)")
		s.EnqueueRefCommit(repoPath, followUpRef, followUpCommit)
	}

	// History persistence happens outside the lock (its own mutex +
	// file IO). Only record when the job succeeded; failed runs may
	// have aborted before peak allocation.
	//
	// #6107 review: also gated on peakSrcChildMaxRSS, and that gate is
	// load-bearing rather than decorative. RSSHistory is not a log — Record is
	// a moving max that predictedFor reads on every admission decision — so an
	// entry that does not mean what the others mean is not a bad data point,
	// it is a permanently wrong budget. Only the child high-water mark
	// qualifies: an absolute figure for one process that did nothing but index
	// THIS repo. Anything measured against the daemon is a delta against a
	// different baseline, and on a daemon running several jobs it attributes
	// whatever else was allocating to whichever repo happened to be sampling.
	//
	// Known coverage loss, stated rather than implied: this fires only for FULL
	// subprocess indexes. The block above skips cfg.Index entirely when the
	// incremental path succeeds, and incremental is default-ON and runs fully
	// in-process, so an actively-maintained repo can go a long time between
	// samples. That is honest — an incremental pass has no comparable peak, and
	// the series exists to predict full-index cost — but it does mean the decay
	// in RSSHistory.Record acts slowly, over full indexes rather than over
	// wall-clock time.
	if err == nil && peakSrc == peakSrcChildMaxRSS && observedPeakMB > 0 && s.cfg.History != nil {
		s.cfg.History.Record(repoPath, observedPeakMB)
	}

	// Wake admission — capacity has freed.
	s.poke()

	if yielded {
		// Nothing was indexed (we yielded to a foreground rebuild). Do not log a
		// completion nor arm the cross-repo link pass — the rebuild owns those.
		return
	}

	if err != nil {
		// A skip already logged its own (rate-limited) index_circuit_skip
		// event above; logging index_err/"index failed" here too would
		// reproduce the exact 74x-log storm the breaker exists to prevent.
		if !skip {
			s.logEvent("index_err", repoPath, err.Error())
			s.logger.Error("sched: index failed", "repo", repoPath, "err", err, "took", time.Since(t0))
		}
		return
	}
	dur := time.Since(t0).Truncate(time.Millisecond)
	// The audit stream carries the source too. A bare "peak=" is the exact
	// ambiguity the structured line below was restructured to remove, and the
	// two streams disagreeing about what a number means is worse than either
	// alone: "peak=0 src=unmeasured" says we did not measure, "peak=0" alone
	// says we measured zero.
	s.logEvent("index_ok", repoPath,
		dur.String()+" peak="+formatMB(observedPeakMB)+" src="+peakSrc)
	// #5710 follow-up: stamp entities=N so a silent 0-entity completion (e.g. an
	// empty-graph store recreation) is visible at a glance. -1 means "unknown".
	ents := -1
	if s.cfg.EntityCount != nil {
		ents = s.cfg.EntityCount(repoPath, tok.ref)
	}
	// peak_rss_mb / peak_rss_src (#6107): resident-set size, in MiB, NEVER Go
	// heap. peak_rss_src says which measure the number is:
	//   child_maxrss — absolute peak RSS of the `grafel index-internal` child,
	//                  from wait4 ru_maxrss (the default path).
	//   unmeasured   — no figure at all: the index ran in-process (incremental,
	//                  or GRAFEL_SUBPROCESS_INDEXER=0), or the platform exposes
	//                  no rusage (Windows). peak_rss_mb is 0 and means nothing.
	// RSS is an upper bound on footprint, not the footprint: on Darwin pages
	// the runtime released with MADV_FREE stay resident until the kernel is
	// under pressure. Measured here on darwin/arm64, child ru_maxrss over live
	// heap peak is 1.02x for a held allocation and 1.09x for a churning one,
	// but 1.94x for a sawtooth that repeatedly grows and drops — the shape an
	// extractor has. Read it as a ceiling, not as a requirement.
	completedArgs := []any{"repo", repoPath, "took", dur, "peak_rss_src", peakSrc}
	// peak_vs_predicted_mb is observedPeak - predictedMB: the predictor's error
	// for this run, NOT an independent measurement (it was once logged as
	// "alloc_diff_mb", which read like one). Emitted only when there IS an
	// observed peak, because with none it collapses to -predictedMB — the
	// widely-quoted "peak_heap_mb=0 alloc_diff_mb=-1593" line was exactly that,
	// and measured nothing at all.
	// peak_rss_mb is OMITTED, not zeroed, when there is no measurement. A
	// consumer averaging or graphing the field would otherwise fold in zeros
	// that never meant "this index used no memory" — the same false number
	// peak_heap_mb=0 was. peak_rss_src is always present, so its absence is
	// never silent.
	if peakSrc != peakSrcUnmeasured {
		completedArgs = append(completedArgs,
			"peak_rss_mb", observedPeakMB,
			"peak_vs_predicted_mb", observedPeakMB-tok.predictedMB)
	}
	completedArgs = append(completedArgs, "entities", ents)
	s.logger.Info("indexer: completed", completedArgs...)

	// Schedule the downstream cross-repo link pass for each group this repo
	// belongs to. The group-scope algorithm pass is NOT scheduled here — it is
	// chained off the SUCCESS path of the link pass (scheduleGroupAlgo, called
	// from runLinks) so that N repo reindexes coalesce into one link pass and
	// then exactly one group-algo pass (#5349 A3). The old per-repo algorithm
	// pass (scheduleAlgo) is removed: a single-repo group is the degenerate
	// one-repo union, computed by the group pass.
	if s.cfg.GroupsForRepo != nil {
		for _, g := range s.cfg.GroupsForRepo(repoPath) {
			// #6068: re-present the generation snapshotted at the authorising
			// hold. A name absent from the snapshot had generation 0 then — the
			// zero value IS the claim, not a missing one — so it arms unless a
			// CancelGroup has moved it since. No exemption branch: an exemption
			// here is precisely the hole that lets a group which joined after
			// the snapshot and was then deleted arm a live timer.
			s.fireRearmGapHook("index-done", g)
			s.scheduleLinksFor(g, linkGen[g])
		}
	}
}

// scheduleLinks (re)arms the per-group link debounce timer. The 10s
// window is meant to coalesce bursts where multiple repos in a group
// re-index back-to-back.
func (s *Scheduler) scheduleLinks(group string) {
	s.scheduleLinksFor(group, groupGenFresh)
}

// scheduleLinksFor is scheduleLinks with the #6068 continuation guard, the same
// shape as scheduleGroupAlgoFor. gen is the cancel generation the caller
// snapshotted under the hold that AUTHORISED this arm; if a CancelGroup has
// landed since, the generation has moved and the arm is refused — the link pass
// it would arm chains the group-algo pass in turn, so letting it through leaks
// the heaviest pass in the daemon to a deleted group.
//
// 0 is a MEANINGFUL gen, not a "don't know": it is the generation of every group
// that has never been cancelled, so a caller whose snapshot lacks a name still
// makes a real, falsifiable claim by passing 0. Only callers with genuinely
// nothing to re-present — a fresh trigger that is not a continuation of anything
// — pass groupGenFresh, which bypasses the check outright.
//
// The guard is deliberately narrow. CancelGroup has exactly two production
// callers (Service.cancelGroupWork and the engine's KindCancelGroup drain), both
// of which mean "this group was deleted", so a moved generation is never a
// transient condition a live group can be in. Over-suppression would be the
// worse defect — a silently dropped link + group-algo pass for a group that
// still exists is invisible — and the snapshot form costs none of it: a
// never-cancelled group matches at 0, and a group re-registered under a
// previously-cancelled name matches at that name's real snapshotted generation.
// Only a cancel landing inside the authorised window can refuse an arm.
//
// The single residual: a group deleted and RE-created inside one index's window
// is live at the arm and still refused. That is self-healing rather than a
// silent loss — re-registering a group enqueues its repos, and those indexes
// chain their own arms under a fresh snapshot — and it is the same exposure the
// other two call sites already carry, not something this one introduces.
func (s *Scheduler) scheduleLinksFor(group string, gen uint64) {
	if s.cfg.Links == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != groupGenFresh && s.groupCancelSeq[group] != gen {
		s.logEventLocked("link_arm_dropped", "",
			group+": group cancelled while the index that would arm its link pass was in flight — not arming")
		return
	}
	if t, ok := s.linkTimers[group]; ok {
		t.Stop()
	}
	s.linkPending[group] = true
	s.armLinkTimerLocked(group, s.cfg.LinkDebounce)
}

// armLinkTimerLocked arms (or re-arms) group's link timer under a FRESH arm id
// and records that id in linkArm. fireLinks refuses to run for any other id, so
// the id is what makes a delete stick: CancelGroup drops linkArm[group], and a
// timer that had already fired when the delete landed — Timer.Stop() reports
// false for it and nothing else can reach its in-flight AfterFunc body — finds
// its id gone and returns without running or re-arming.
// MUST be called with s.mu held.
func (s *Scheduler) armLinkTimerLocked(group string, delay time.Duration) {
	s.armSeq++
	arm := s.armSeq
	s.linkArm[group] = arm
	s.linkTimers[group] = time.AfterFunc(delay, func() { s.fireLinks(group, arm) })
}

// fireLinks is the link-debounce timer body, split out of scheduleLinks so a
// firing timer that finds the machine BUSY can re-arm itself instead of running
// (#5954). It acquires the daemon-wide heavy write-stage token first; on failure
// the pass stays PENDING (linkPending is never cleared) and a fresh retry timer
// is armed under the same linkTimers[group] key — so CancelGroup still reaches
// it and no work is lost.
//
// arm is the identity of the timer whose body this is. The "is this arm still
// live" test and the stage acquisition happen under ONE hold of s.mu, so
// CancelGroup is strictly ordered against it: either the delete lands first and
// this returns having done nothing, or the pass has already acquired the token
// and registered its linkCancel token, which the delete then cancels. There is
// no third outcome in which a deleted group's pass runs.
func (s *Scheduler) fireLinks(group string, arm uint64) {
	if s.stopped() {
		return // shutting down — do not re-arm a retry timer that outlives Stop
	}
	name := "links:" + group
	now := time.Now()

	s.mu.Lock()
	if live, ok := s.linkArm[group]; !ok || live != arm {
		// Cancelled (group deleted) or superseded by a newer arm while this
		// timer body was in flight. Running now would run a deleted group's
		// link pass; re-arming now would resurrect the very timer and stage
		// deferral CancelGroup just dropped.
		s.mu.Unlock()
		return
	}
	stageEpoch, acquired := s.tryAcquireStageLocked(name, now)
	if !acquired {
		delay := s.noteStageDeferLocked(name, now)
		s.linkPending[group] = true
		if t, ok := s.linkTimers[group]; ok {
			t.Stop()
		}
		s.armLinkTimerLocked(group, delay)
		s.mu.Unlock()
		return
	}
	s.linkPending[group] = false
	delete(s.linkTimers, group)
	delete(s.linkArm, group)
	// Cancel generation as of the hold that authorises THIS pass (#6068). The
	// chained group-algo arm at the end of runLinks happens minutes later and
	// outside s.mu; re-presenting this generation there is what stops a group
	// deleted in the meantime from getting a fresh, live arm.
	gen := s.groupGenLocked(group)
	// Derive a PER-GROUP cancel context from shutdownCtx (not shutdownCtx
	// directly) so that a group delete can interrupt THIS group's in-flight
	// link pass via CancelGroup without waiting for daemon Stop(). Still
	// rooted at shutdownCtx so daemon shutdown also cancels it. Stored so
	// CancelGroup can reach it; cleared once runLinks returns.
	ctx, cancel := context.WithCancel(s.shutdownCtx)
	tok := &linkPassCancel{cancel: cancel}
	// Supersede any still-registered predecessor by CANCELLING it before
	// overwriting the entry — do NOT just drop it. runLinks executes on this
	// timer AfterFunc goroutine (not the worker pool), so when a link pass
	// outlasts LinkDebounce and a repo reindex re-arms scheduleLinks, pass-1
	// and pass-2 could run CONCURRENTLY. (The stage token now also serialises
	// them, but the identity check remains the cancellation-correctness
	// guarantee.) Without this cancel, only pass-2's token is in the map, so a
	// later CancelGroup reaches only pass-2 and pass-1 (the long
	// betweenness/phantom pass) runs to completion for a deleted group.
	if prev, ok := s.linkCancel[group]; ok {
		prev.cancel()
	}
	s.linkCancel[group] = tok
	// Give the gate a handle on this pass, so a forfeit that outlives its grace
	// has something to cancel instead of reclaiming the token beside a live one.
	s.setStageCancelLocked(name, stageEpoch, cancel)
	s.mu.Unlock()

	// Release on EVERY exit path — normal return, error, cancellation, panic.
	// Carries the epoch so that a pass whose token was already abandoned by the
	// forfeit-grace expiry cannot free its successor's token here.
	defer s.releaseStage(name, stageEpoch)

	s.runLinks(ctx, group, gen)

	s.mu.Lock()
	// Only clear the entry if it is still OUR token. An overlapping second
	// link pass (this group's timer re-armed while we ran) or a CancelGroup
	// may have already replaced/removed it; blind-deleting here would drop
	// the newer pass's live cancel and re-open the leak.
	if s.linkCancel[group] == tok {
		delete(s.linkCancel, group)
	}
	s.mu.Unlock()
	cancel()
}

// gen is the cancel generation captured by fireLinks when it authorised this
// pass; it gates the chained group-algo arm below (#6068).
func (s *Scheduler) runLinks(ctx context.Context, group string, gen uint64) {
	s.logEvent("links_start", "", group)
	t0 := time.Now()
	err := s.cfg.Links(ctx, group)
	if err != nil {
		s.logEvent("links_err", "", group+": "+err.Error())
		s.logger.Error("sched: links failed", "group", group, "err", err)
		return
	}
	s.logEvent("links_ok", "", group+" "+time.Since(t0).Truncate(time.Millisecond).String())

	// SUCCESS path (#5349 A3): cross-repo phantom edges are now settled in each
	// repo's graph.fb, so arm the debounced group-scope algorithm pass. Because
	// scheduleLinks already coalesced a burst of repo reindexes into this one
	// link pass, chaining the algo pass here means N file saves → 1 link pass →
	// 1 group-algo pass. Re-arm (cancel previous) on every link completion.
	//
	// The #6068 gap, in its widest form: cfg.Links above can run for minutes,
	// and this arm is not under s.mu (nor can it be). A CancelGroup at any point
	// since fireLinks authorised this pass must suppress the chained arm.
	s.fireRearmGapHook("links-done", group)
	s.scheduleGroupAlgoFor(group, gen)
}

// groupAlgoDebounceDefault is the settling window between a successful link
// pass and the group-scope algorithm pass it triggers. The group-algo pass
// (Louvain + PageRank + betweenness over the whole group union) is the
// heaviest background job the daemon runs, so the debounce is deliberately
// long: a burst of commits/reindexes within the window coalesces into ONE
// pass instead of re-firing the analytics on nearly every push. Raised from
// 30s to 180s after a CPU regression (v0.1.3) where back-to-back commits kept
// re-triggering the pass and pinned a 12-core machine for hours. The window is
// comfortably past the link-pass cadence. Override with
// GRAFEL_GROUP_ALGO_DEBOUNCE.
const groupAlgoDebounceDefault = 180 * time.Second

// groupAlgoDebounceFromEnv resolves the group-algo debounce, honoring
// GRAFEL_GROUP_ALGO_DEBOUNCE (a Go duration string, e.g. "45s"). An unset or
// unparseable value falls back to groupAlgoDebounceDefault.
func groupAlgoDebounceFromEnv() time.Duration {
	if v := strings.TrimSpace(os.Getenv("GRAFEL_GROUP_ALGO_DEBOUNCE")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return groupAlgoDebounceDefault
}

// groupAlgoMaxWaitDefault bounds how long a group-algo pass may be deferred by
// continuous re-arming of the debounce (#5450). The 180s debounce coalesces
// bursts, but a busy daemon whose members re-index back-to-back re-arms it
// forever, starving the recompute and leaving the overlay stale right after a
// reindex. The max-wait caps that starvation: after this long continuously
// armed, the pass fires promptly. 5 min is ~1.7× the debounce — comfortably past
// a normal burst so steady-state coalescing is unaffected, yet far below the 10m
// settled-group sweep so a churning group is refreshed well before the sweep
// would. Override with GRAFEL_GROUP_ALGO_MAX_WAIT.
const groupAlgoMaxWaitDefault = 300 * time.Second

// groupAlgoMaxWaitFromEnv resolves the group-algo max-wait, honoring
// GRAFEL_GROUP_ALGO_MAX_WAIT (a Go duration string, e.g. "240s"). An unset or
// unparseable value falls back to groupAlgoMaxWaitDefault.
func groupAlgoMaxWaitFromEnv() time.Duration {
	if v := strings.TrimSpace(os.Getenv("GRAFEL_GROUP_ALGO_MAX_WAIT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return groupAlgoMaxWaitDefault
}

// overlaySweepIntervalDefault is the cadence of the settled-group overlay
// freshness sweep (#5403). A long-running daemon serving a SETTLED group (one
// that is not being actively reindexed, so no link pass → no scheduleGroupAlgo
// fires) can drift into serving a STALE overlay: the group's per-repo graph.fb
// advanced past the overlay's recorded source_mtimes (e.g. via a manual
// `grafel group-algo --write` on a sibling, or a reindex that didn't chain a
// pass) yet nothing re-arms the recompute until the next reindex. The sweep
// closes that gap by periodically checking each known group's overlay staleness
// and re-arming the (debounced + CPU-capped) group-algo pass for the stale ones.
//
// 10 min is comfortably above the 180s group-algo debounce, so the sweep can
// never re-arm a pass faster than one can settle, and the per-sweep cost is just
// a handful of os.Stats per group. Set GRAFEL_OVERLAY_SWEEP_INTERVAL=0 to
// disable.
const overlaySweepIntervalDefault = 10 * time.Minute

// overlaySweepIntervalFromEnv resolves the overlay-freshness sweep interval,
// honoring GRAFEL_OVERLAY_SWEEP_INTERVAL (a Go duration string, e.g. "15m", or
// "0" to disable). An unset/unparseable value falls back to
// overlaySweepIntervalDefault. A value of exactly 0 disables the sweep
// (returned as 0); the loop is then never started.
func overlaySweepIntervalFromEnv() time.Duration {
	if v := strings.TrimSpace(os.Getenv("GRAFEL_OVERLAY_SWEEP_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return d // includes 0 = explicitly disabled
		}
	}
	return overlaySweepIntervalDefault
}

// scheduleGroupAlgo (re)arms the per-GROUP algorithm pass timer. Any pending
// pass for the group is cancelled first; a new link completion starts the
// debounce window over. The pass runs once over the assembled group union and
// writes the <group>-algo.json overlay (A2). It is the replacement for the old
// per-repo scheduleAlgo: a single-repo group is the degenerate one-repo union.
//
// Max-wait (#5450): the debounce coalesces a burst of reindexes into one pass,
// but on a busy daemon where members re-index back-to-back every link completion
// re-arms the (long) debounce, so the recompute can be starved for minutes —
// leaving the overlay stale right after a reindex. To bound that, the FIRST arm
// in a window records groupAlgoArmedAt; subsequent re-arms keep that timestamp.
// The pass is scheduled for the EARLIER of now+debounce and armedAt+maxWait;
// since the armedAt+maxWait deadline does not move across re-arms, continuous
// churn can push the fire out no later than that deadline. Still one pass per
// window (coalesced) and still on the CPU-capped path — no unbounded recompute.
//
// LET AN IN-FLIGHT PASS FINISH. A re-arm that arrives while a pass is RUNNING
// does NOT cancel it. It used to: scheduleGroupAlgo called cancelGroupAlgoLocked
// unconditionally, which SIGKILLed the group-algo child and restarted it from
// zero. On the reference 25-repo corpus, where the pass outlives the interval
// between triggers, that is an infinite churn loop — the daemon's own logs show
// 91 `group-algo: starting` events against 2 completions, i.e. the overlay was
// effectively never produced while the daemon looked permanently busy.
//
// Letting it finish is correct, not merely cheaper. The pass is a PURE
// ANNOTATION over an already-written graph.fb: it loads a SNAPSHOT of the
// per-repo graphs, computes over the union, and writes a separate overlay file.
// Its result is therefore always internally consistent — it is at worst stale
// relative to content that landed after the snapshot, never wrong. Cancelling
// trades a slightly-stale-but-real overlay for NO overlay plus a full recompute,
// and under sustained churn that trade never converges. So: the running pass
// keeps its input snapshot and runs to completion, and the newer request is
// recorded (groupAlgoRerun) and serviced by re-arming the debounce the moment
// that pass returns. Staleness is bounded by the follow-up pass; unavailability
// under the old behaviour was not bounded at all.
func (s *Scheduler) scheduleGroupAlgo(group string) {
	s.scheduleGroupAlgoFor(group, groupGenFresh)
}

// groupGenFresh marks a scheduleGroupAlgoFor call that is a FRESH trigger, not
// a continuation of work authorised earlier, and therefore has no generation to
// re-present. uint64 max is unreachable as a real generation (it counts
// CancelGroup calls for one group).
const groupGenFresh = ^uint64(0)

// groupGenLocked returns group's current cancel generation, to be re-presented
// to scheduleGroupAlgoFor by a continuation that will arm after releasing s.mu.
// MUST be called with s.mu held.
func (s *Scheduler) groupGenLocked(group string) uint64 {
	return s.groupCancelSeq[group]
}

// scheduleGroupAlgoFor is scheduleGroupAlgo with the #6068 continuation guard.
// gen is the cancel generation the caller captured under the hold that
// AUTHORISED this arm; if a CancelGroup has landed since, the generation has
// moved and the arm is refused. Callers with nothing to re-present pass
// groupGenFresh — a delete must not gate a genuinely new request, only a
// continuation of work the delete already cancelled.
func (s *Scheduler) scheduleGroupAlgoFor(group string, gen uint64) {
	if s.cfg.GroupAlgo == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != groupGenFresh && s.groupCancelSeq[group] != gen {
		// The group was deleted between the authorising hold and this arm. The
		// arm ids cannot see this: CancelGroup dropped an id we are about to
		// replace, so a fresh one here would be legitimately live for a group
		// that is gone.
		s.logEventLocked("group_algo_arm_dropped", "",
			group+": group cancelled while the pass that would re-arm it was in flight — not re-arming")
		return
	}
	// A pass is RUNNING right now. Record the request and leave it alone; the
	// completion path in fireGroupAlgo consumes the flag and re-arms.
	if _, running := s.groupAlgoCancel[group]; running {
		if !s.groupAlgoRerun[group] {
			s.groupAlgoRerun[group] = true
			s.logEventLocked("group_algo_requeued", "",
				group+": recompute requested while a pass is in flight — letting it finish, re-arming on completion")
		}
		return
	}
	// Preserve the window-start timestamp across re-arms.
	armedAt, hadWindow := s.groupAlgoArmedAt[group]
	now := time.Now()
	if !hadWindow {
		armedAt = now
	}

	// The pass fires at the EARLIER of:
	//   - now + debounce         (coalesce a burst — a fresh re-arm normally
	//                             pushes this out, the existing contract), and
	//   - armedAt + maxWait      (the hard ceiling on debounce starvation, #5450).
	// The deadline does not move across re-arms (armedAt is preserved), so a busy
	// daemon that re-arms on every link completion cannot push the pass out
	// forever — it fires no later than armedAt+maxWait.
	fireAt := now.Add(s.cfg.GroupAlgoDebounce)
	if deadline := armedAt.Add(s.cfg.GroupAlgoMaxWait); deadline.Before(fireAt) {
		fireAt = deadline
	}

	// If a pass is already pending at exactly this fire time, the deadline has
	// PINNED it (the debounce can no longer push it out): leave the existing,
	// about-to-fire timer alone instead of cancel+re-arm. Re-arming on a tight
	// cadence would otherwise keep stopping the timer just before it fires,
	// starving the pass forever — the exact bug this cap exists to prevent
	// (#5450). When fireAt still moves (normal debounce), we fall through and
	// re-arm as before.
	if s.groupAlgoPending[group] && fireAt.Equal(s.groupAlgoFireAt[group]) {
		return
	}

	delay := time.Until(fireAt)
	if delay < 0 {
		delay = 0
	}

	s.cancelGroupAlgoLocked(group)
	s.groupAlgoArmedAt[group] = armedAt
	s.groupAlgoFireAt[group] = fireAt
	s.groupAlgoPending[group] = true
	s.armGroupAlgoTimerLocked(group, delay)
}

// armGroupAlgoTimerLocked arms (or re-arms) group's group-algo timer under a
// FRESH arm id, recorded in groupAlgoArm. It is the group-algo twin of
// armLinkTimerLocked and exists for the same reason: Timer.Stop() cannot stop a
// timer that has already fired, so the id — which cancelGroupAlgoLocked deletes
// — is the only thing that can reach that in-flight body.
// MUST be called with s.mu held.
func (s *Scheduler) armGroupAlgoTimerLocked(group string, delay time.Duration) {
	s.armSeq++
	arm := s.armSeq
	s.groupAlgoArm[group] = arm
	s.groupAlgoTimers[group] = time.AfterFunc(delay, func() { s.fireGroupAlgo(group, arm) })
}

// fireGroupAlgo is the group-algo debounce timer body, split out of
// scheduleGroupAlgo so a firing timer that finds the machine BUSY can re-arm
// itself instead of running (#5954).
//
// This is where the #5450 max-wait and the stage gate meet. The max-wait forces
// the timer to FIRE promptly under sustained churn; the gate can still refuse to
// let it RUN. Deferral wins — a max-wait firing that runs anyway would make the
// gate decorative under exactly the sustained-churn conditions that produce the
// worst peaks. The gate's own bounded starvation guard (the drain barrier, see
// noteStageDeferLocked) is what keeps the pass from being starved forever.
// arm is the identity of the timer whose body this is; see groupAlgoArm. The
// check and the stage acquisition share ONE hold of s.mu, so a group delete is
// strictly ordered against a fire: either the delete lands first and this
// returns having touched nothing — in particular NOT having taken a fresh
// indexstate.GroupAlgoBegin the delete can no longer balance — or the pass
// already holds the token and has registered its groupAlgoCancel token, which
// the delete then cancels.
func (s *Scheduler) fireGroupAlgo(group string, arm uint64) {
	if s.stopped() {
		return // shutting down — do not re-arm a retry timer that outlives Stop
	}
	name := "group-algo:" + group
	now := time.Now()

	s.mu.Lock()
	if live, ok := s.groupAlgoArm[group]; !ok || live != arm {
		// Cancelled (group deleted) or superseded while this timer body was in
		// flight. Returning here is what keeps the heaviest pass in the daemon
		// from running for a group that is gone, and what keeps the deferred
		// branch below from re-taking an indexstate hold nobody will release.
		s.mu.Unlock()
		return
	}
	stageEpoch, acquired := s.tryAcquireStageLocked(name, now)
	if !acquired {
		delay := s.noteStageDeferLocked(name, now)
		// A DEFERRED pass must stay visible to grafel_stats exactly like a
		// RUNNING one, or an MCP consumer polling across the deferral sees an
		// idle daemon and trusts a stale overlay.
		s.markGroupAlgoDeferredLocked(group, true)
		s.groupAlgoPending[group] = true
		if t, ok := s.groupAlgoTimers[group]; ok {
			t.Stop()
		}
		s.groupAlgoFireAt[group] = now.Add(delay)
		s.armGroupAlgoTimerLocked(group, delay)
		s.mu.Unlock()
		return
	}
	s.groupAlgoPending[group] = false
	delete(s.groupAlgoTimers, group)
	delete(s.groupAlgoArm, group)
	delete(s.groupAlgoArmedAt, group) // window closed — next arm starts fresh
	delete(s.groupAlgoFireAt, group)
	// Derive the per-run cancel context from shutdownCtx (not
	// context.Background()) so that on Stop() the in-flight group-algo pass
	// — which may fork a subprocess — receives cancellation. Mirrors runIndex
	// and runLinks. Fixes the leak class of issue #2493.
	ctx, cancel := context.WithCancel(s.shutdownCtx)
	tok := &groupAlgoPassCancel{cancel: cancel}
	s.groupAlgoCancel[group] = tok
	// Give the gate a handle on this pass — cancelling it SIGKILLs the
	// group-algo child (subprocess_runner.go). Only the forfeit-grace expiry
	// uses it; a plain forfeit deliberately does not.
	s.setStageCancelLocked(name, stageEpoch, cancel)
	s.mu.Unlock()

	// Release on EVERY exit path — normal return, error, cancellation, panic.
	// Carries the epoch so that a pass whose token was already abandoned by the
	// forfeit-grace expiry cannot free its successor's token here.
	defer s.releaseStage(name, stageEpoch)

	s.runGroupAlgo(ctx, group)

	s.mu.Lock()
	// #6001: only clear the entry if it is still OUR token. A CancelGroup may
	// have drained the map and a successor pass registered its own handle while
	// this (long) pass was returning; blind-deleting would drop the LIVE
	// successor's cancel func, leaving a later CancelGroup/Stop with nothing to
	// cancel. fireLinks guards the identical shape at its own completion site —
	// and both now also guard the ARM, one hazard earlier: a timer that fires
	// just before the delete lands (see groupAlgoArm / linkArm).
	if s.groupAlgoCancel[group] == tok {
		delete(s.groupAlgoCancel, group)
	}
	// A recompute requested WHILE this pass ran was deliberately not serviced by
	// cancelling us (see scheduleGroupAlgo). Consume the marker now so the newer
	// content gets its own pass — this is what keeps "let it finish" from
	// silently dropping the request.
	rerun := s.groupAlgoRerun[group]
	delete(s.groupAlgoRerun, group)
	// Capture the cancel generation under the SAME hold that decides to re-arm
	// (#6068). A CancelGroup that lands BEFORE this hold has already cleared
	// groupAlgoRerun, so rerun is false and there is nothing to suppress; one
	// that lands AFTER it moves the generation, and the re-arm below is refused.
	gen := s.groupGenLocked(group)
	// Drop the deferred marker only now: runGroupAlgo holds its own
	// GroupAlgoBegin for the RUNNING window, so clearing here keeps the
	// deferred→running indexstate coverage continuous (count 1 → 2 → 1 → 0)
	// rather than dipping to zero between the two.
	s.markGroupAlgoDeferredLocked(group, false)
	s.mu.Unlock()
	cancel()
	// The #6068 gap: s.mu is released, the decision to re-arm has been taken,
	// and the arm has not happened yet. A CancelGroup landing HERE is invisible
	// to the arm id (it drops an id we are about to replace) — only the cancel
	// generation captured above can see it.
	s.fireRearmGapHook("group-algo-rerun", group)

	if rerun && !s.stopped() {
		s.logEvent("group_algo_rearm", "", group+": servicing the recompute requested during the previous pass")
		s.scheduleGroupAlgoFor(group, gen)
	}
}

// rearmGapHook is a TEST-ONLY seam, nil in production, invoked on the three
// paths that release s.mu and then re-arm downstream work (#6068):
// fireGroupAlgo's rerun completion ("group-algo-rerun"), runLinks' chained
// group-algo arm ("links-done"), and runIndex's chained link arm
// ("index-done"). It exists because those gaps cannot otherwise be driven
// deterministically — a test installs a hook that calls CancelGroup, which
// places the delete EXACTLY in the gap instead of racing for it.
//
// PRECONDITION, for any call site added later: this MUST NOT be called with
// s.mu held. The hook a test installs calls s.CancelGroup, which takes s.mu, so
// a call site under the lock self-deadlocks the moment a test installs a hook.
// All three current sites are lock-free, and that is load-bearing, not
// incidental.
//
// An atomic pointer, not a plain field: the hook is read from the timer/link
// goroutine while the test goroutine may still be storing it (#6056/#6059).
func (s *Scheduler) fireRearmGapHook(site, group string) {
	if h := s.rearmGapHook.Load(); h != nil {
		(*h)(site, group)
	}
}

// groupAlgoPassCancel is a per-in-flight-group-algo-pass identity token wrapping
// its cancel func, so a completing pass can distinguish ITS OWN registry entry
// from a successor's (#6001). Mirrors linkPassCancel.
type groupAlgoPassCancel struct {
	cancel context.CancelFunc
}

// markGroupAlgoDeferredLocked toggles a group's "deferred by the stage gate"
// marker, holding exactly one balanced indexstate.GroupAlgoBegin/End pair for
// the duration. Idempotent. MUST be called with s.mu held.
func (s *Scheduler) markGroupAlgoDeferredLocked(group string, deferred bool) {
	if s.groupAlgoDeferred[group] == deferred {
		return
	}
	if deferred {
		s.groupAlgoDeferred[group] = true
		indexstate.GroupAlgoBegin()
		return
	}
	delete(s.groupAlgoDeferred, group)
	indexstate.GroupAlgoEnd()
}

// cancelGroupAlgoLocked stops any pending timer or cancels an in-flight
// group-algorithm pass for the given group. MUST be called with s.mu held.
func (s *Scheduler) cancelGroupAlgoLocked(group string) {
	if t, ok := s.groupAlgoTimers[group]; ok {
		t.Stop()
		delete(s.groupAlgoTimers, group)
		s.groupAlgoPending[group] = false
	}
	// Stop() above is a no-op for a timer that ALREADY fired: its fireGroupAlgo
	// body is in flight and holds only its arm id, so dropping the id is what
	// stops it — see groupAlgoArm.
	delete(s.groupAlgoArm, group)
	if tok, ok := s.groupAlgoCancel[group]; ok {
		tok.cancel()
		delete(s.groupAlgoCancel, group)
	}
	// An explicit cancel (shutdown, group delete) drops a queued re-run too:
	// there is nothing left to annotate. Note scheduleGroupAlgo never reaches
	// here for a running pass — only Stop and CancelGroup do.
	delete(s.groupAlgoRerun, group)
	// Clear the max-wait window (#5450). scheduleGroupAlgo snapshots and restores
	// this for a re-arm of a still-open window; an explicit cancel (shutdown, or a
	// superseding pass) ends the window so the next arm starts a fresh budget.
	delete(s.groupAlgoArmedAt, group)
	delete(s.groupAlgoFireAt, group)
	// #5954: the pass is no longer deferred-by-the-gate — drop the marker (and
	// its balanced indexstate hold) plus any deferral bookkeeping, so a cancelled
	// pass cannot leave the daemon looking permanently busy.
	s.markGroupAlgoDeferredLocked(group, false)
	s.clearStageDeferralLocked("group-algo:" + group)
}

// clearStageDeferralLocked drops a stage's deferral bookkeeping (and the drain
// barrier if that stage owns it), so a cancelled or superseded stage does not
// keep index dispatch held or the scheduler reported busy. MUST be called with
// s.mu held.
func (s *Scheduler) clearStageDeferralLocked(name string) {
	delete(s.stageDeferSince, name)
	if s.stageDrainFor == name {
		s.stageDrainFor = ""
		s.stageDrainUntil = time.Time{}
	}
}

// linkPassCancel is a per-in-flight-link-pass identity token wrapping its cancel
// func, so a completing pass can distinguish ITS OWN registry entry from a
// newer, overlapping pass's entry (link passes are not single-flight). See the
// Scheduler.linkCancel field doc.
type linkPassCancel struct {
	cancel context.CancelFunc
}

// CancelGroup cancels ALL of a group's in-flight and pending background work:
// the debounced/in-flight group-algorithm pass, the debounced/in-flight
// cross-repo link pass, and the in-flight per-repo reindexes of the group's
// member repos. It is invoked when a group is deleted (Service.DeleteGroup) so
// the enrichment goroutines the group kicked off stop within seconds instead of
// running to completion after the group is gone (the leak: a deleted group's
// betweenness/phantom-edge/link passes kept a multi-core machine pinned).
//
// Cancellation is SCOPED to `group` only: another group's in-flight passes and
// reindexes are untouched (their timers/cancels live under different map keys,
// and a repo shared with a surviving group is left running because it still has
// a live group in GroupsForRepo — see the membership check below).
//
// It never blocks: it signals the relevant cancel funcs (which unblock ctx.Done
// consumers and SIGKILL subprocess children) and returns. It does NOT await the
// cancelled work — DeleteGroup must not deadlock behind an 11-minute pass.
func (s *Scheduler) CancelGroup(group string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Bump the cancel generation FIRST (#6068). Everything below cancels state
	// that exists NOW; the generation is what reaches the arms that do not exist
	// yet — a completion path that has already decided to re-arm, released s.mu,
	// and not yet called scheduleGroupAlgo. Bumping before the teardown means
	// there is no sub-window in which a continuation could observe the old
	// generation and still find its state cancelled.
	s.groupCancelSeq[group]++

	// Group-algo pass (pending timer + in-flight run) — already per-group.
	s.cancelGroupAlgoLocked(group)

	// Link pass: stop a pending debounce timer and cancel an in-flight run.
	if t, ok := s.linkTimers[group]; ok {
		t.Stop()
		delete(s.linkTimers, group)
		s.linkPending[group] = false
	}
	// Stop() above is a no-op for a timer that has ALREADY fired: its fireLinks
	// body is in flight on the timer goroutine and holds only its arm id.
	// Dropping the id is what stops that body — see linkArm.
	delete(s.linkArm, group)
	if tok, ok := s.linkCancel[group]; ok {
		tok.cancel()
		delete(s.linkCancel, group)
	}
	s.clearStageDeferralLocked("links:" + group)

	// In-flight per-repo reindexes whose repo belongs ONLY to this group (or to
	// this group among others that are also being torn down). A repo still
	// referenced by a surviving group reports that group via GroupsForRepo and is
	// therefore left running — we must not strip a live group's reindex.
	for repoPath, cancel := range s.indexCancel {
		if s.repoBelongsOnlyToLocked(repoPath, group) {
			cancel()
			delete(s.indexCancel, repoPath)
			s.logEventLocked("index_cancelled_group_delete", repoPath, group)
		}
	}

	// Drop any foreground mark on the group (#5954). The group is being deleted,
	// so no epoch can still be current and the epoch-scoped clear does not
	// apply — this is the one unconditional teardown. Without it a group deleted
	// and recreated inside the 30m linger window would run its first BACKGROUND
	// pass at foreground caps and un-niced, which is exactly the policy
	// inversion this epic is fixing.
	//
	// Safe under s.mu: the foreground registry has its own mutex and never
	// takes s.mu, so there is no lock-order edge to invert.
	ForgetGroupForeground(group)

	s.logEventLocked("group_cancelled", "", group+": cancelled in-flight enrichment on group delete")
}

// repoBelongsOnlyToLocked reports whether repoPath's only owning group is
// `group` (i.e. it is safe to cancel its in-flight reindex on a delete of that
// group). When GroupsForRepo is not wired (tests / degenerate config) it errs
// toward cancelling — a group delete is an explicit teardown, and the reindex
// context is rooted at shutdownCtx so worst case matches shutdown semantics.
// MUST be called with s.mu held.
func (s *Scheduler) repoBelongsOnlyToLocked(repoPath, group string) bool {
	if s.cfg.GroupsForRepo == nil {
		return true
	}
	for _, g := range s.cfg.GroupsForRepo(repoPath) {
		if g != group {
			return false
		}
	}
	return true
}

func (s *Scheduler) runGroupAlgo(ctx context.Context, group string) {
	// Acquire the concurrency semaphore before starting the CPU/heap-intensive
	// group-algorithm pass. This enforces the AlgoCap and prevents a group pass
	// from running concurrently with another capped pass (#2141 root-cause C).
	// The acquire is interruptible via ctx so a cancellation (a new link pass
	// completes, or daemon shutdown) doesn't block forever.
	// The semaphore capacity (cap(s.algoSem)) only bounds how many algo passes
	// may run CONCURRENTLY — it is NOT the CPU draw of a single pass. The actual
	// core usage of the (subprocess) pass is its GOMAXPROCS. Log that real value
	// so `cap=` reflects the cores the pass can consume, not the old, misleading
	// NumCPU/2 concurrency number (the CPU-regression diagnosis confusion).
	// Log the cap the child will ACTUALLY be spawned with, which since #5954
	// depends on whether this group is user-awaited (foreground.go). The epoch
	// is captured HERE, at the start of the pass, so the clear at the end names
	// the mark this pass was actually spawned under — a rebuild that re-marks
	// the group while this (long) pass runs must not have its mark wiped by
	// this pass finishing.
	//
	// #6108 — `cap` MUST NAME SOMETHING THAT IS ACTUALLY ENFORCED. This value is
	// the pass's GOMAXPROCS, and until #6108 it was enforced only when the pass
	// ran as a CHILD: the in-process fallback had nothing to hand it to and ran
	// at the daemon's own GOMAXPROCS, which is how `cap=2` was logged beside a
	// sustained 571.9%. Both paths now apply it (the in-process one via
	// process.WithGOMAXPROCSCap in daemonSchedulerGroupAlgo), and `mode` says
	// which enforcement is in play so the line can be read without guessing.
	fgEpoch, _ := GroupForegroundState(group)
	capN := groupAlgoChildGOMAXPROCS(group)
	mode := "in-process"
	if SubprocessIndexEnabled() {
		mode = "child"
	}
	s.logger.Info("group-algo: starting", "group", group, "cap", capN, "mode", mode)
	// Try the semaphore without blocking first, purely so a pass that is QUEUED
	// rather than running says so. "starting" followed by hours of silence has
	// two very different causes — waiting for a slot, and running slowly — and
	// the log could not distinguish them (#6108).
	select {
	case s.algoSem <- struct{}{}:
		// acquired immediately
	default:
		s.logger.Info("group-algo: waiting for an algo slot", "group", group, "slots", cap(s.algoSem))
		s.logEvent("group_algo_queued", "", group)
		select {
		case s.algoSem <- struct{}{}:
			// acquired
		case <-ctx.Done():
			s.logEvent("group_algo_cancelled", "", group+": waiting for algo-sem slot")
			return
		case <-s.stop:
			return
		}
	}
	defer func() { <-s.algoSem }()

	// Surface the in-flight group-algo pass to grafel_stats' is_indexing
	// (#5349 A3): a coordinator querying the MCP sees the daemon is busy with a
	// group pass, not just per-repo reindexes. Cleared in the deferred decrement
	// even on cancel/error.
	indexstate.GroupAlgoBegin()
	defer indexstate.GroupAlgoEnd()

	s.logEvent("group_algo_start", "", fmt.Sprintf("%s cap=%d", group, capN))
	t0 := time.Now()
	err := s.cfg.GroupAlgo(ctx, group)
	if err != nil {
		if ctx.Err() != nil {
			s.logEvent("group_algo_cancelled", "", group)
			return
		}
		s.logEvent("group_algo_err", "", group+": "+err.Error())
		s.logger.Error("sched: group-algo failed", "group", group, "err", err)
		return
	}
	s.logEvent("group_algo_ok", "", group+" "+time.Since(t0).Truncate(time.Millisecond).String())
	// #5954 wall-time: the group-algo pass is the LAST stage of the work a
	// foreground rebuild sets in motion. Its success means the graph the user
	// asked for now exists, so the group stops being "user-awaited" and any
	// later pass is background churn again, on background caps.
	//
	// Only on success, and only here: on error or cancellation the awaited
	// artifact does not exist yet, so the retry stays foreground and the linger
	// window in foreground.go is what bounds it.
	//
	// Epoch-scoped: a pass that started under one rebuild's mark and finished
	// after a NEWER rebuild re-marked the group clears nothing.
	ClearGroupForeground(group, fgEpoch)
}

// Snapshot reports current scheduler state for the Status RPC.
type Snapshot struct {
	QueueLen int
	InFlight []InFlightJob
	// PendingAlgo lists groups with a debounced GROUP-algo pass armed (#5349
	// A3). The per-repo algo pass was removed; this is now group-keyed. It also
	// covers a recompute QUEUED behind an in-flight pass (groupAlgoRerun), so a
	// deferred request is never invisible.
	PendingAlgo []string
	// GroupAlgoRunning lists groups whose annotation (group-algo) pass is
	// executing RIGHT NOW. The overlay it produces — communities, pagerank,
	// centrality — does not exist until it completes, so "my communities are
	// missing" needs this to be diagnosable from `grafel status` rather than
	// inferred from the RSS shape or the log ring.
	GroupAlgoRunning []string
	PendingLinks     []string
	IndexedRepos     []RepoSnapshot
	RecentLog        []LogEntry

	// Budget telemetry (added with admission control).
	BudgetMB    int64
	UsedMB      int64
	BlockedJobs []string

	// CoalescedDirty lists repos that have an in-flight reindex AND received
	// further enqueues during it (#5138). Each will get exactly one follow-up
	// when its in-flight run completes — these are the requests that, before
	// coalescing, would have stacked into concurrent same-repo reindex jobs.
	CoalescedDirty []string

	// --- heavy write-stage gate (#5954) telemetry. ---
	// The gate's decisions previously existed only as RecentLog entries, so an
	// operator (or a peak-RSS measurement run) could not tell from outside
	// whether the gate had fired at all. These three fields make the CURRENT
	// state directly observable via `grafel status` / the daemon status RPC.
	//
	// StageHolder names the exclusive stage holding the token ("links:<group>" /
	// "group-algo:<group>"), or "". StageDeferred lists stages currently turned
	// away by the gate. Barging lists live foreground holds ("rebuild:<group>")
	// — non-empty means a rebuild is registered and background heavy stages are
	// yielding to it. All sorted for deterministic output.
	//
	// StageForfeits counts stages that blew StageGateHoldMax since daemon start.
	// It is a FAILURE counter, not a warning: any non-zero value means a heavy
	// stage held the gate past a 4h bound. A measurement run should assert it
	// stays 0.
	//
	// StageForfeitedHolder is the LIVE counterpart of that sticky counter:
	// StageHolder is past its hold-max RIGHT NOW and the gate is deliberately
	// keeping it resident rather than releasing it. The counter alone cannot
	// answer "is the gate in a forfeit grace at this instant", which is the
	// question an operator staring at a stalled daemon actually has.
	StageHolder          string   `json:"stage_holder,omitempty"`
	StageDeferred        []string `json:"stage_deferred,omitempty"`
	Barging              []string `json:"barging,omitempty"`
	StageForfeits        int64    `json:"stage_forfeits,omitempty"`
	StageForfeitedHolder bool     `json:"stage_forfeited_holder,omitempty"`
}

// InFlightJob is one currently-running index, with its reserved MB.
type InFlightJob struct {
	Path        string `json:"path"`
	PredictedMB int64  `json:"predicted_mb"`
}

// RepoSnapshot is one repo's slice of Snapshot.
type RepoSnapshot struct {
	Path        string    `json:"path"`
	LastIndex   time.Time `json:"last_index"`
	LastAlgo    time.Time `json:"last_algo"`
	IndexCount  int64     `json:"index_count"`
	AlgoCount   int64     `json:"algo_count"`
	LastErr     string    `json:"last_err,omitempty"`
	LastPeakMB  int64     `json:"last_peak_mb,omitempty"`
	LastPeakSrc string    `json:"last_peak_src,omitempty"`
	PredictedMB int64     `json:"predicted_mb,omitempty"`
}

// Snapshot returns a defensive copy of the scheduler's user-visible
// state. Safe to call from the RPC handler.
func (s *Scheduler) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Snapshot{
		QueueLen: s.queueLen,
		BudgetMB: s.cfg.BudgetMB,
		UsedMB:   s.usedMB,
	}
	for p, mb := range s.inflight {
		out.InFlight = append(out.InFlight, InFlightJob{Path: p, PredictedMB: mb})
	}
	// Deterministic ordering — helps both /status output and tests.
	sort.Slice(out.InFlight, func(i, j int) bool { return out.InFlight[i].Path < out.InFlight[j].Path })
	// PendingAlgo now reports pending GROUP-algo passes (#5349 A3): the
	// per-repo algo pass was removed, so the field carries group names with a
	// debounced group-algo pass armed.
	for g := range s.groupAlgoPending {
		if s.groupAlgoPending[g] {
			out.PendingAlgo = append(out.PendingAlgo, g)
		}
	}
	// A recompute queued behind an in-flight pass is pending too — it just has
	// no timer yet, because the completion of the running pass is what arms it.
	for g := range s.groupAlgoRerun {
		if s.groupAlgoRerun[g] && !s.groupAlgoPending[g] {
			out.PendingAlgo = append(out.PendingAlgo, g)
		}
	}
	sort.Strings(out.PendingAlgo)
	for g := range s.groupAlgoCancel {
		out.GroupAlgoRunning = append(out.GroupAlgoRunning, g)
	}
	sort.Strings(out.GroupAlgoRunning)
	for g := range s.linkPending {
		if s.linkPending[g] {
			out.PendingLinks = append(out.PendingLinks, g)
		}
	}
	sort.Strings(out.PendingLinks)
	out.BlockedJobs = append(out.BlockedJobs, s.pendingQ...)
	for p, d := range s.dirty {
		if d {
			out.CoalescedDirty = append(out.CoalescedDirty, p)
		}
	}
	sort.Strings(out.CoalescedDirty)
	for p, st := range s.indexedRepos {
		out.IndexedRepos = append(out.IndexedRepos, RepoSnapshot{
			Path: p, LastIndex: st.LastIndex, LastAlgo: st.LastAlgo,
			IndexCount: st.IndexCount, AlgoCount: st.AlgoCount,
			LastErr: st.LastErr, LastPeakMB: st.LastPeakMB, LastPeakSrc: st.LastPeakSrc,
			PredictedMB: st.PredictedMB,
		})
	}
	if n := len(s.recentLog); n > 0 {
		out.RecentLog = append(out.RecentLog, s.recentLog...)
	}
	g := s.stageGateStateLocked()
	out.StageHolder, out.StageDeferred, out.Barging = g.Holder, g.Deferred, g.Barging
	out.StageForfeits, out.StageForfeitedHolder = g.Forfeits, g.ForfeitedHolder
	return out
}

// StageGateState is the heavy write-stage gate's live state (#5954), sampled
// cheaply. Snapshot() carries the same three fields, but Snapshot copies the
// whole RecentLog ring and every RepoSnapshot; the engine-liveness heartbeat
// samples this on a 5s tick and must not pay for that.
type StageGateState struct {
	// Holder names the EXCLUSIVE stage holding the token ("links:<group>" /
	// "group-algo:<group>"), or "" when none does.
	Holder string
	// Deferred lists stages the gate is currently turning away.
	Deferred []string
	// Barging lists live FOREGROUND holds ("rebuild:<group>"). Non-empty means
	// a rebuild is registered and background heavy stages are yielding to it.
	Barging []string
	// Forfeits counts stages that blew StageGateHoldMax since daemon start.
	// Non-zero is a FAILURE signal, not a warning — see Snapshot.StageForfeits.
	Forfeits int64
	// ForfeitedHolder is true when Holder has been forfeited and is being kept
	// resident by the gate rather than released (see reapStageLocked). Unlike
	// Forfeits, which is sticky for the life of the daemon, this is the LIVE
	// signal: the gate is inside a forfeit grace at this instant, and Holder
	// will be cancelled when it expires. Reaches `grafel status` in both
	// monolith and split mode alongside Forfeits.
	ForfeitedHolder bool
}

// StageGateState returns the gate's live state for observability. Safe to call
// from any goroutine.
func (s *Scheduler) StageGateState() StageGateState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stageGateStateLocked()
}

// stageGateStateLocked is the shared implementation. MUST be called with s.mu
// held.
//
// Reported VERBATIM, without reaping expired holders first: this is a read-only
// observation surface and must not have scheduling side effects. A holder past
// its bound therefore stays visible until a real gate decision reaps it — which
// is also the honest reading, since it genuinely is still resident.
func (s *Scheduler) stageGateStateLocked() StageGateState {
	out := StageGateState{
		Holder:          s.stageHolder,
		Forfeits:        s.stageForfeits,
		ForfeitedHolder: s.stageHolder != "" && !s.stageForfeitedAt.IsZero(),
	}
	for name := range s.stageDeferSince {
		out.Deferred = append(out.Deferred, name)
	}
	sort.Strings(out.Deferred)
	out.Barging = s.bargeNamesLocked()
	sort.Strings(out.Barging)
	return out
}

// MarkIndexed lets the daemon record a non-watcher-driven index (e.g.
// an explicit `grafel index` RPC) so Status reflects reality.
func (s *Scheduler) MarkIndexed(repoPath string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.indexedRepos[repoPath]
	stats.LastIndex = time.Now()
	stats.IndexCount++
	if err != nil {
		stats.LastErr = err.Error()
	} else {
		stats.LastErr = ""
		// #5726/#5729: an explicit `grafel index` RPC that succeeds means the
		// repo now serializes under the cap (e.g. the developer removed a huge
		// vendored tree). Clear any scheduler-armed breaker so a subsequent
		// watcher reindex is not skipped by a stale backoff window.
		stats.FailCommit = ""
		stats.FailCount = 0
		stats.FailBackoffUntil = time.Time{}
		stats.FailLoggedAt = time.Time{}
	}
	s.indexedRepos[repoPath] = stats
}

// logEvent appends to the in-memory recent-log buffer (capped at
// maxRecentLog). The daemon log file remains the authoritative store.
func (s *Scheduler) logEvent(kind, repo, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logEventLocked(kind, repo, msg)
}

// logEventLocked is the s.mu-held form used inside hot paths that
// already hold the scheduler lock.
func (s *Scheduler) logEventLocked(kind, repo, msg string) {
	s.recentLog = append(s.recentLog, LogEntry{Time: time.Now(), Kind: kind, Repo: repo, Msg: msg})
	if len(s.recentLog) > maxRecentLog {
		s.recentLog = s.recentLog[len(s.recentLog)-maxRecentLog:]
	}
}

// goroutineID extracts the current goroutine's numeric ID from the stack
// header. Used only for diagnostic log lines — never relied upon for
// correctness. Returns 0 on any parse failure.
func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// Stack header format: "goroutine <N> [..."
	s := string(buf[:n])
	const prefix = "goroutine "
	if !strings.HasPrefix(s, prefix) {
		return 0
	}
	s = s[len(prefix):]
	var id uint64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}

// formatMB is a tiny helper so the recent-log strings stay short.
func formatMB(mb int64) string {
	// Avoid pulling fmt into hot paths.
	if mb <= 0 {
		return "0MB"
	}
	return itoa(mb) + "MB"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
