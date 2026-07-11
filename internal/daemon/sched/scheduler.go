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
	"time"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/indexstate"
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
	// 0 (or negative) means: auto = max(2, runtime.NumCPU()/2).
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
}

// deadManTimeout is how long the scheduler waits with a non-empty pending
// queue and zero admitted jobs before force-admitting the smallest queued
// job. This is the relief valve for admission-control wedge scenarios
// (e.g. inflated RSS history predictions that exceed the budget).
const deadManTimeout = 2 * time.Minute

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
	// root-cause C / #2140 hyp-2). Capacity = max(2, NumCPU/2) unless
	// Config.AlgoCap is set. Nil means unbounded (legacy; not used in
	// production).
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
	// Per-GROUP algorithm pass (#5349 A3). Mirrors the link timer machinery:
	// debounce timer + pending flag + in-flight cancel func, all keyed by group.
	// Replaces the old per-repo algo timers — algorithms now run once over the
	// assembled group union, chained off link completion.
	groupAlgoTimers  map[string]*time.Timer
	groupAlgoPending map[string]bool
	groupAlgoCancel  map[string]context.CancelFunc
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
	indexedRepos    map[string]repoStats
	recentLog       []LogEntry

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
	LastIndex   time.Time
	LastAlgo    time.Time
	IndexCount  int64
	AlgoCount   int64
	LastErr     string
	LastPeakMB  int64 // observed peak (history) — 0 if predictor-only
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
// If cfg.AlgoCap > 0 it is returned as-is. Otherwise it is auto-tuned to
// max(2, runtime.NumCPU()/2) so that on an 8-core machine only 4 algo
// passes run in parallel, leaving headroom for the watcher, indexers, and
// the Go runtime itself.
func resolveAlgoCap(cap int) int {
	if cap > 0 {
		return cap
	}
	n := runtime.NumCPU() / 2
	if n < 2 {
		n = 2
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
	algoCap := resolveAlgoCap(cfg.AlgoCap)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	return &Scheduler{
		cfg:              cfg,
		logger:           cfg.Logger,
		enq:              make(chan enqueueRequest, 64),
		jobs:             make(chan jobToken, cfg.Workers),
		wake:             make(chan struct{}, 1),
		stop:             make(chan struct{}),
		algoSem:          make(chan struct{}, algoCap),
		inflight:         map[string]int64{},
		pendingIndex:     map[string]bool{},
		dirty:            map[string]bool{},
		pendingRefs:      map[string]string{},
		pendingCommits:   map[string]string{},
		linkTimers:       map[string]*time.Timer{},
		linkPending:      map[string]bool{},
		groupAlgoTimers:  map[string]*time.Timer{},
		groupAlgoPending: map[string]bool{},
		groupAlgoCancel:  map[string]context.CancelFunc{},
		groupAlgoArmedAt: map[string]time.Time{},
		groupAlgoFireAt:  map[string]time.Time{},
		indexedRepos:     map[string]repoStats{},
		shutdownCtx:      shutdownCtx,
		shutdownCancel:   shutdownCancel,
	}
}

// Start spins up the dedup goroutine + admission loop + worker pool +
// dead-man switch. Stop reverses it.
func (s *Scheduler) Start() {
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
	for _, t := range s.groupAlgoTimers {
		t.Stop()
	}
	for _, c := range s.groupAlgoCancel {
		c()
	}
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

	if len(s.pendingQ) == 0 || len(s.inflight) > 0 {
		// Queue is clear OR jobs are running — reset the clock.
		s.deadManAt = time.Time{}
		return
	}

	now := time.Now()
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

// busyLocked reports whether any indexing-related work is in flight or
// pending. Must be called with s.mu held. Algo/link debounce timers count
// as "busy" so we don't FreeOSMemory in the gap between an index completing
// and its downstream passes firing.
func (s *Scheduler) busyLocked() bool {
	if len(s.inflight) > 0 || s.queueLen > 0 || len(s.pendingQ) > 0 {
		return true
	}
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

// maybeReleaseMemory is the testable core of the idle-release trigger. It
// tracks when the scheduler became idle and, once idle for the debounce
// window, invokes FreeOSMemory exactly once (until the next busy→idle
// transition). Exposed as a method (not an inline closure) so tests can drive
// it with a synthetic clock and a stub FreeOSMemory. It is safe to call
// repeatedly; only the first post-debounce call in an idle period releases.
func (s *Scheduler) maybeReleaseMemory(now time.Time) {
	s.mu.Lock()
	if s.busyLocked() {
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

	s.mu.Lock()
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
	var observedPeakMB int64

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
	} else {
		s.logEvent("index_start", repoPath, "predicted="+formatMB(tok.predictedMB)+" ref="+tok.ref)
		// Observability: log goroutine identity + ref so concurrent-indexer
		// regressions are diagnosable without a pprof trace (#2141).
		s.logger.Info("indexer: starting", "repo", repoPath, "ref", tok.ref, "goroutine_id", goroutineID())

		// Spawn RSS sampler so we capture the peak the daemon hit during
		// this job. Records into history on completion.
		sampleStop := make(chan struct{})
		var sampleWG sync.WaitGroup
		sampleWG.Add(1)
		go func() {
			defer sampleWG.Done()
			t := time.NewTicker(5 * time.Second)
			defer t.Stop()
			baseline := currentProcessRSSMB()
			for {
				select {
				case <-sampleStop:
					return
				case <-t.C:
					now := currentProcessRSSMB()
					delta := now - baseline
					if delta > observedPeakMB {
						observedPeakMB = delta
					}
				}
			}
		}()

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
		// Use shutdownCtx for all child-process-spawning calls so that when
		// Stop() is called (daemon SIGTERM/SIGINT/Stop-RPC), any in-flight
		// subprocess indexer receives SIGTERM via exec.CommandContext and the
		// daemon can exit cleanly without leaving zombie processes (issue #2176).
		jobCtx := s.shutdownCtx

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

		close(sampleStop)
		sampleWG.Wait()
	}

	s.mu.Lock()
	stats := s.indexedRepos[repoPath]
	if !skip {
		stats.LastIndex = time.Now()
		stats.IndexCount++
		stats.PredictedMB = tok.predictedMB
		// #5433: record the ref this completed index ran against so
		// grafel_index_status can report indexed_ref per repo.
		stats.LastIndexedRef = tok.ref
		if observedPeakMB > 0 {
			stats.LastPeakMB = observedPeakMB
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
	} else {
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

	if followUp {
		s.logEvent("reindex_coalesced_followup", repoPath,
			"changes landed during in-flight reindex — scheduling single follow-up (#5138)")
		s.EnqueueRefCommit(repoPath, followUpRef, followUpCommit)
	}

	// History persistence happens outside the lock (its own mutex +
	// file IO). Only record when the job succeeded; failed runs may
	// have aborted before peak allocation.
	if err == nil && observedPeakMB > 0 && s.cfg.History != nil {
		s.cfg.History.Record(repoPath, observedPeakMB)
	}

	// Wake admission — capacity has freed.
	s.poke()

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
	allocDiff := observedPeakMB - tok.predictedMB
	s.logEvent("index_ok", repoPath,
		dur.String()+" peak="+formatMB(observedPeakMB))
	// #5710 follow-up: stamp entities=N so a silent 0-entity completion (e.g. an
	// empty-graph store recreation) is visible at a glance. -1 means "unknown".
	ents := -1
	if s.cfg.EntityCount != nil {
		ents = s.cfg.EntityCount(repoPath, tok.ref)
	}
	s.logger.Info("indexer: completed", "repo", repoPath, "took", dur, "peak_heap_mb", observedPeakMB, "alloc_diff_mb", allocDiff, "entities", ents)

	// Schedule the downstream cross-repo link pass for each group this repo
	// belongs to. The group-scope algorithm pass is NOT scheduled here — it is
	// chained off the SUCCESS path of the link pass (scheduleGroupAlgo, called
	// from runLinks) so that N repo reindexes coalesce into one link pass and
	// then exactly one group-algo pass (#5349 A3). The old per-repo algorithm
	// pass (scheduleAlgo) is removed: a single-repo group is the degenerate
	// one-repo union, computed by the group pass.
	if s.cfg.GroupsForRepo != nil {
		for _, g := range s.cfg.GroupsForRepo(repoPath) {
			s.scheduleLinks(g)
		}
	}
}

// scheduleLinks (re)arms the per-group link debounce timer. The 10s
// window is meant to coalesce bursts where multiple repos in a group
// re-index back-to-back.
func (s *Scheduler) scheduleLinks(group string) {
	if s.cfg.Links == nil {
		return
	}
	s.mu.Lock()
	if t, ok := s.linkTimers[group]; ok {
		t.Stop()
	}
	s.linkPending[group] = true
	s.linkTimers[group] = time.AfterFunc(s.cfg.LinkDebounce, func() {
		s.mu.Lock()
		s.linkPending[group] = false
		delete(s.linkTimers, group)
		s.mu.Unlock()
		// Pass shutdownCtx so that if Stop() is called while the link pass is
		// running (or if it ever spawns subprocesses in the future), the
		// cancellation signal propagates correctly — matching the fix applied to
		// runIndex in issue #2176/#2491. Fixes issue #2493.
		s.runLinks(s.shutdownCtx, group)
	})
	s.mu.Unlock()
}

func (s *Scheduler) runLinks(ctx context.Context, group string) {
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
	s.scheduleGroupAlgo(group)
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
func (s *Scheduler) scheduleGroupAlgo(group string) {
	if s.cfg.GroupAlgo == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.groupAlgoTimers[group] = time.AfterFunc(delay, func() {
		s.mu.Lock()
		s.groupAlgoPending[group] = false
		delete(s.groupAlgoTimers, group)
		delete(s.groupAlgoArmedAt, group) // window closed — next arm starts fresh
		delete(s.groupAlgoFireAt, group)
		// Derive the per-run cancel context from shutdownCtx (not
		// context.Background()) so that on Stop() the in-flight group-algo pass
		// — which may fork a subprocess — receives cancellation. Mirrors runIndex
		// and runLinks. Fixes the leak class of issue #2493.
		ctx, cancel := context.WithCancel(s.shutdownCtx)
		s.groupAlgoCancel[group] = cancel
		s.mu.Unlock()

		s.runGroupAlgo(ctx, group)

		s.mu.Lock()
		delete(s.groupAlgoCancel, group)
		s.mu.Unlock()
	})
}

// cancelGroupAlgoLocked stops any pending timer or cancels an in-flight
// group-algorithm pass for the given group. MUST be called with s.mu held.
func (s *Scheduler) cancelGroupAlgoLocked(group string) {
	if t, ok := s.groupAlgoTimers[group]; ok {
		t.Stop()
		delete(s.groupAlgoTimers, group)
		s.groupAlgoPending[group] = false
	}
	if c, ok := s.groupAlgoCancel[group]; ok {
		c()
		delete(s.groupAlgoCancel, group)
	}
	// Clear the max-wait window (#5450). scheduleGroupAlgo snapshots and restores
	// this for a re-arm of a still-open window; an explicit cancel (shutdown, or a
	// superseding pass) ends the window so the next arm starts a fresh budget.
	delete(s.groupAlgoArmedAt, group)
	delete(s.groupAlgoFireAt, group)
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
	capN := GroupAlgoGOMAXPROCS()
	s.logger.Info("group-algo: starting", "group", group, "cap", capN)
	select {
	case s.algoSem <- struct{}{}:
		// acquired
	case <-ctx.Done():
		s.logEvent("group_algo_cancelled", "", group+": waiting for algo-sem slot")
		return
	case <-s.stop:
		return
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
}

// Snapshot reports current scheduler state for the Status RPC.
type Snapshot struct {
	QueueLen int
	InFlight []InFlightJob
	// PendingAlgo lists groups with a debounced GROUP-algo pass armed (#5349
	// A3). The per-repo algo pass was removed; this is now group-keyed.
	PendingAlgo  []string
	PendingLinks []string
	IndexedRepos []RepoSnapshot
	RecentLog    []LogEntry

	// Budget telemetry (added with admission control).
	BudgetMB    int64
	UsedMB      int64
	BlockedJobs []string

	// CoalescedDirty lists repos that have an in-flight reindex AND received
	// further enqueues during it (#5138). Each will get exactly one follow-up
	// when its in-flight run completes — these are the requests that, before
	// coalescing, would have stacked into concurrent same-repo reindex jobs.
	CoalescedDirty []string
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
	sort.Strings(out.PendingAlgo)
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
			LastErr: st.LastErr, LastPeakMB: st.LastPeakMB, PredictedMB: st.PredictedMB,
		})
	}
	if n := len(s.recentLog); n > 0 {
		out.RecentLog = append(out.RecentLog, s.recentLog...)
	}
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
