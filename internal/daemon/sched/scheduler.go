// Package sched is the daemon's reactive scheduler (Phase B+). The
// watcher hands off settled-repo notifications to Enqueue; the
// scheduler serialises per-repo indexes, runs them on a small worker
// pool, then schedules:
//
//   - a debounced cross-repo link recompute per group (10s),
//   - optionally, a debounced graph-algorithm pass per repo (30s) — only
//     when GRAFEL_EAGER_ALGO=true (S2 of #2149). By default the algo
//     pass is suppressed here; rank-sensitive MCP tools trigger it on-demand
//     via the algo.Cache path instead.
//
// The link recompute and algorithm pass run via caller-supplied callbacks so
// the scheduler stays free of extractor + graph package dependencies.
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

// RefCaptureFn returns the current HEAD ref for repoPath. Used to snapshot
// the ref at Enqueue time so debounced batches index against the ref that
// was active when the file-change event fired, not the ref at dispatch time.
// May return "" for detached HEAD or non-git directories; IndexFn must
// tolerate an empty ref.
type RefCaptureFn func(repoPath string) string

// LinksFn re-runs the cross-repo link passes for a group.
type LinksFn func(ctx context.Context, group string) error

// AlgoFn runs the graph-algorithm pass for a repo (community detection,
// PageRank, articulation points). It is scheduled after a successful
// index settles and is cancelled+rescheduled on any further write.
type AlgoFn func(ctx context.Context, repoPath string) error

// GroupsForRepoFn returns the group names a repo participates in.
// Provided by the caller so the scheduler does not import the registry.
type GroupsForRepoFn func(repoPath string) []string

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

// Config wires the scheduler. All function fields are required; nil
// causes Enqueue to short-circuit with a logged warning.
type Config struct {
	Workers       int           // worker pool size; defaults to 2
	LinkDebounce  time.Duration // group settling window; defaults to 10s
	AlgoDebounce  time.Duration // per-repo algo delay; defaults to 30s
	Index         IndexFn
	Links         LinksFn
	Algorithms    AlgoFn
	GroupsForRepo GroupsForRepoFn
	Logger        *slog.Logger

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

// enqueueRequest carries a repo path plus the ref snapshot taken at
// Enqueue time. This is the unit flowing from public Enqueue → dedupLoop →
// admitLoop → workerLoop.
type enqueueRequest struct {
	repoPath string
	ref      string // captured at Enqueue time via RefCapture; "" = unknown
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
	dirty        map[string]bool
	pendingRefs  map[string]string // repo → ref captured at last Enqueue (overwritten on re-enqueue)
	pendingQ     []string          // ordered admission queue
	queueLen     int               // pending + admitted-but-not-yet-running
	usedMB       int64             // sum of inflight MB
	linkTimers   map[string]*time.Timer
	linkPending  map[string]bool
	algoTimers   map[string]*time.Timer
	algoPending  map[string]bool
	algoCancel   map[string]context.CancelFunc
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
}

// jobToken couples a repo path with the predicted MB that admission
// control reserved for it, and the ref that was captured at Enqueue time
// (PH1b). The worker decrements usedMB by this exact amount on completion,
// so partial-credit history updates don't drift.
type jobToken struct {
	repoPath    string
	ref         string // git ref name captured at Enqueue time; "" = unknown
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
	if cfg.AlgoDebounce <= 0 {
		cfg.AlgoDebounce = 30 * time.Second
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
		cfg:            cfg,
		logger:         cfg.Logger,
		enq:            make(chan enqueueRequest, 64),
		jobs:           make(chan jobToken, cfg.Workers),
		wake:           make(chan struct{}, 1),
		stop:           make(chan struct{}),
		algoSem:        make(chan struct{}, algoCap),
		inflight:       map[string]int64{},
		pendingIndex:   map[string]bool{},
		dirty:          map[string]bool{},
		pendingRefs:    map[string]string{},
		linkTimers:     map[string]*time.Timer{},
		linkPending:    map[string]bool{},
		algoTimers:     map[string]*time.Timer{},
		algoPending:    map[string]bool{},
		algoCancel:     map[string]context.CancelFunc{},
		indexedRepos:   map[string]repoStats{},
		shutdownCtx:    shutdownCtx,
		shutdownCancel: shutdownCancel,
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
	for _, t := range s.algoTimers {
		t.Stop()
	}
	for _, c := range s.algoCancel {
		c()
	}
}

// Enqueue requests a (debounced+deduped) reindex of repoPath. The current
// HEAD ref is captured immediately via Config.RefCapture (if configured) so
// the ref is snapshotted at event-fire time, not at eventual dispatch time.
// Safe to call from arbitrary goroutines.
func (s *Scheduler) Enqueue(repoPath string) {
	ref := ""
	if s.cfg.RefCapture != nil {
		ref = s.cfg.RefCapture(repoPath)
	}
	s.EnqueueRef(repoPath, ref)
}

// EnqueueRef requests a (debounced+deduped) reindex of repoPath at a
// specific git ref. Called directly by the GitHeadPoller (branch-switch
// events) where the new ref has already been observed — no extra git call
// needed. Safe to call from arbitrary goroutines.
func (s *Scheduler) EnqueueRef(repoPath, ref string) {
	// Issue #3680: drop enqueues for linked worktrees of already-indexed
	// primaries so they never become independent root index jobs (each of
	// which would spawn its own ~100MB store and pressure the RSS budget).
	if s.cfg.SkipEnqueue != nil && s.cfg.SkipEnqueue(repoPath) {
		s.logEvent("enqueue_skipped", repoPath, "linked worktree of indexed primary — not cold-indexed as a new root (#3680)")
		return
	}
	select {
	case s.enq <- enqueueRequest{repoPath: repoPath, ref: ref}:
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
			s.cancelAlgoLocked(p)
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
				s.mu.Unlock()
				continue
			}
			if s.pendingIndex[p] {
				// Already pending: update the ref to the latest observed value.
				if req.ref != "" {
					s.pendingRefs[p] = req.ref
				}
				s.mu.Unlock()
				continue
			}
			s.pendingIndex[p] = true
			s.pendingRefs[p] = req.ref
			s.pendingQ = append(s.pendingQ, p)
			s.queueLen++
			// Start the dead-man clock if nothing is currently
			// running — otherwise there will be a poke on completion.
			if len(s.inflight) == 0 && s.deadManAt.IsZero() {
				s.deadManAt = time.Now()
			}
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
	// Remove from queue (preserve order for remaining entries).
	s.pendingQ = append(s.pendingQ[:smallestIdx], s.pendingQ[smallestIdx+1:]...)
	delete(s.pendingRefs, repo)
	s.inflight[repo] = smallestMB
	s.publishIndexStateLocked()
	s.usedMB += smallestMB
	stuckFor := now.Sub(s.deadManAt).Truncate(time.Second)
	s.deadManAt = time.Time{} // reset clock; the job is now admitted
	s.logEventLocked("admit_deadman", repo,
		"force-admitted after "+stuckFor.String()+" with no progress; predicted="+formatMB(smallestMB))
	s.logger.Info("sched: dead-man: force-admitting",
		"repo", repo, "predicted_mb", smallestMB, "stuck_for", stuckFor)

	tok := jobToken{repoPath: repo, ref: ref, predictedMB: smallestMB}
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
}

// busyLocked reports whether any indexing-related work is in flight or
// pending. Must be called with s.mu held. Algo/link debounce timers count
// as "busy" so we don't FreeOSMemory in the gap between an index completing
// and its downstream passes firing.
func (s *Scheduler) busyLocked() bool {
	if len(s.inflight) > 0 || s.queueLen > 0 || len(s.pendingQ) > 0 {
		return true
	}
	for _, p := range s.algoPending {
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
		s.inflight[repo] = predicted
		s.publishIndexStateLocked()
		s.usedMB += predicted
		s.deadManAt = time.Time{} // job admitted — reset dead-man clock
		s.logEventLocked("admit_ok", repo,
			"predicted="+formatMB(predicted)+" used="+formatMB(s.usedMB)+" ref="+ref)
		tok := jobToken{repoPath: repo, ref: ref, predictedMB: predicted}
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
	s.mu.Unlock()

	t0 := time.Now()
	s.logEvent("index_start", repoPath, "predicted="+formatMB(tok.predictedMB)+" ref="+tok.ref)
	// Observability: log goroutine identity + ref so concurrent-indexer
	// regressions are diagnosable without a pprof trace (#2141).
	s.logger.Info("indexer: starting", "repo", repoPath, "ref", tok.ref, "goroutine_id", goroutineID())

	// Spawn RSS sampler so we capture the peak the daemon hit during
	// this job. Records into history on completion.
	sampleStop := make(chan struct{})
	var sampleWG sync.WaitGroup
	var observedPeakMB int64
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

	var err error
	incrementalDone := false
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

	close(sampleStop)
	sampleWG.Wait()

	s.mu.Lock()
	stats := s.indexedRepos[repoPath]
	stats.LastIndex = time.Now()
	stats.IndexCount++
	stats.PredictedMB = tok.predictedMB
	if observedPeakMB > 0 {
		stats.LastPeakMB = observedPeakMB
	}
	if err != nil {
		stats.LastErr = err.Error()
	} else {
		stats.LastErr = ""
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
	// Prefer the latest observed ref recorded while dirty; fall back to the
	// ref this run used. dedupLoop overwrites pendingRefs[repoPath] on every
	// mid-run enqueue, so it holds the most recent branch for the follow-up.
	followUpRef := tok.ref
	if r, ok := s.pendingRefs[repoPath]; ok && r != "" {
		followUpRef = r
	}
	s.mu.Unlock()

	if followUp {
		s.logEvent("reindex_coalesced_followup", repoPath,
			"changes landed during in-flight reindex — scheduling single follow-up (#5138)")
		s.EnqueueRef(repoPath, followUpRef)
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
		s.logEvent("index_err", repoPath, err.Error())
		s.logger.Error("sched: index failed", "repo", repoPath, "err", err, "took", time.Since(t0))
		return
	}
	dur := time.Since(t0).Truncate(time.Millisecond)
	allocDiff := observedPeakMB - tok.predictedMB
	s.logEvent("index_ok", repoPath,
		dur.String()+" peak="+formatMB(observedPeakMB))
	s.logger.Info("indexer: completed", "repo", repoPath, "took", dur, "peak_heap_mb", observedPeakMB, "alloc_diff_mb", allocDiff)

	// Schedule downstream passes.
	s.scheduleAlgo(repoPath)
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
}

// eagerAlgoEnabled reports whether the post-reindex automatic algorithm pass
// is enabled. By default (S2 of #2149) the pass is suppressed; set
// GRAFEL_EAGER_ALGO=true to restore pre-S2 behaviour.
func eagerAlgoEnabled() bool {
	v := os.Getenv("GRAFEL_EAGER_ALGO")
	return v == "1" || v == "true" || v == "yes"
}

// scheduleAlgo (re)arms the per-repo algorithm pass timer. Any pending
// pass is cancelled first; a new pass starts the 30s window over.
//
// S2 (#2152): the automatic post-reindex pass is suppressed unless
// GRAFEL_EAGER_ALGO=true. Rank-sensitive MCP tools trigger the pass
// on-demand via the algo.Cache path so the post-reindex CPU cost is zero.
func (s *Scheduler) scheduleAlgo(repoPath string) {
	if s.cfg.Algorithms == nil {
		return
	}
	if !eagerAlgoEnabled() {
		// S2: cancel any pending pass (new write invalidates a previously
		// scheduled run) but do NOT schedule a new one.
		s.mu.Lock()
		s.cancelAlgoLocked(repoPath)
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelAlgoLocked(repoPath)
	s.algoPending[repoPath] = true
	s.algoTimers[repoPath] = time.AfterFunc(s.cfg.AlgoDebounce, func() {
		s.mu.Lock()
		s.algoPending[repoPath] = false
		delete(s.algoTimers, repoPath)
		// Derive the per-run cancel context from shutdownCtx (not
		// context.Background()) so that when Stop() is called, all in-flight
		// algo passes receive cancellation — matching the treatment of runIndex
		// and runLinks. Fixes issue #2493.
		ctx, cancel := context.WithCancel(s.shutdownCtx)
		s.algoCancel[repoPath] = cancel
		s.mu.Unlock()

		s.runAlgo(ctx, repoPath)

		s.mu.Lock()
		delete(s.algoCancel, repoPath)
		s.mu.Unlock()
	})
}

// cancelAlgoLocked stops any pending timer or cancels an in-flight
// algorithm pass for the given repo. MUST be called with s.mu held.
func (s *Scheduler) cancelAlgoLocked(repoPath string) {
	if t, ok := s.algoTimers[repoPath]; ok {
		t.Stop()
		delete(s.algoTimers, repoPath)
		s.algoPending[repoPath] = false
	}
	if c, ok := s.algoCancel[repoPath]; ok {
		c()
		delete(s.algoCancel, repoPath)
	}
}

func (s *Scheduler) runAlgo(ctx context.Context, repoPath string) {
	// Acquire the concurrency semaphore before starting the CPU/heap-intensive
	// algorithm pass. This enforces the AlgoCap and prevents all N repos from
	// running Louvain/PageRank/articulation simultaneously (#2141 root-cause C
	// / #2140 hyp-2). The acquire is interruptible via ctx so a cancellation
	// (new write arrives for this repo) doesn't block forever.
	cap := cap(s.algoSem)
	s.logger.Info("algo-pass: starting", "repo", repoPath, "cap", cap)
	select {
	case s.algoSem <- struct{}{}:
		// acquired
	case <-ctx.Done():
		s.logEvent("algo_cancelled", repoPath, "waiting for algo-sem slot")
		return
	case <-s.stop:
		return
	}
	defer func() { <-s.algoSem }()

	s.logEvent("algo_start", repoPath, fmt.Sprintf("cap=%d", cap))
	t0 := time.Now()
	err := s.cfg.Algorithms(ctx, repoPath)
	if err != nil {
		if ctx.Err() != nil {
			s.logEvent("algo_cancelled", repoPath, "")
			return
		}
		s.logEvent("algo_err", repoPath, err.Error())
		s.logger.Error("sched: algo failed", "repo", repoPath, "err", err)
		return
	}
	s.mu.Lock()
	stats := s.indexedRepos[repoPath]
	stats.LastAlgo = time.Now()
	stats.AlgoCount++
	s.indexedRepos[repoPath] = stats
	s.mu.Unlock()
	s.logEvent("algo_ok", repoPath, time.Since(t0).Truncate(time.Millisecond).String())
}

// Snapshot reports current scheduler state for the Status RPC.
type Snapshot struct {
	QueueLen     int
	InFlight     []InFlightJob
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
	for p := range s.algoPending {
		if s.algoPending[p] {
			out.PendingAlgo = append(out.PendingAlgo, p)
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
