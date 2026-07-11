// background.go — the silent, bounded, cancellable background enrichment
// worker (#5720, refs #5729).
//
// Historically Pass 6 (enrichment-candidate emission) ran INSIDE the index
// critical path, before graph.fb was persisted (cmd/grafel/index.go). On
// large graphs this peaked ~11GB and stalled first-index, severing MCP until
// it finished. No consumer hard-depends on candidates: every reader treats an
// absent/stale enrichment-candidates.json as "nothing pending" (nil), the
// pass is already skippable via --skip-pass=enrichment, and WriteCandidates
// errors are already swallowed (fire-and-forget). That makes it safe to move
// off the critical path entirely.
//
// Scheduler runs at most one enrichment job process-wide (never the
// multi-worker fan-out extraction uses), paced between chunks so it never
// bursts, and supersedes (cancels) any in-flight job for the SAME repo when a
// new index/rebuild starts — so a rapid edit-reindex loop never accumulates
// orphaned goroutines or races a stale write over a fresh one.
package enrichment

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
)

// InlineEntityThreshold is the size gate (#5720 requirement 6): graphs with
// at most this many entities are cheap enough to enrich synchronously, right
// after graph.fb is written, on the same goroutine that called Schedule.
// Larger graphs defer to the background worker. Deliberately generous —
// deferral is meant to be the default for the heavy path, inline execution is
// just an optimization for tiny repos/tests where spinning up a goroutine and
// waiting on it via Wait() would be pure overhead.
// Deliberately a var, not a const: tests exercise the deferred (background)
// path deterministically by temporarily lowering it rather than requiring a
// multi-thousand-entity fixture in the test corpus.
var InlineEntityThreshold = 2000

// job tracks the cancel func + completion signal for one repo's in-flight (or
// most-recently-scheduled) background enrichment run.
type job struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Scheduler runs enrichment jobs on a single background goroutine at a time,
// system-wide (the `sem` semaphore below), keyed per-repo for supersession.
type Scheduler struct {
	mu   sync.Mutex
	jobs map[string]*job

	// sem bounds system-wide enrichment concurrency to 1 — the ≤1-worker
	// requirement. It is intentionally NOT per-repo: even across multiple
	// repos/groups, at most one enrichment trickle runs at any instant, so
	// the worker is always "gentle" regardless of fleet size.
	sem chan struct{}
}

// NewScheduler constructs a Scheduler with a fresh, empty job table and a
// single-slot semaphore. Tests that want isolation from the process-wide
// DefaultScheduler should construct their own via this constructor.
func NewScheduler() *Scheduler {
	return &Scheduler{
		jobs: make(map[string]*job),
		sem:  make(chan struct{}, 1),
	}
}

// DefaultScheduler is the process-wide singleton used by cmd/grafel's
// indexer for all real (non-test) index runs.
var DefaultScheduler = NewScheduler()

// Schedule supersedes any in-flight (or previously scheduled, still-running)
// job for repoKey and starts run in a new background goroutine bound to a
// fresh, cancellable context. Schedule returns immediately — it never blocks
// the caller, so it is safe to call from the index critical path right after
// graph.fb has been persisted.
//
// run MUST check ctx.Done() between chunks/batches of work and stop promptly
// (without writing further output) when cancelled — that promptness is what
// makes supersession race-free: the new job's goroutine always waits for the
// superseded job's goroutine to fully exit (see below) before starting its
// own work, so a slow-to-cancel run could still delay (but never corrupt)
// the next run's output.
func (s *Scheduler) Schedule(repoKey string, run func(ctx context.Context)) {
	s.mu.Lock()
	var prevDone chan struct{}
	if prev, ok := s.jobs[repoKey]; ok {
		prev.cancel()
		prevDone = prev.done
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.jobs[repoKey] = &job{cancel: cancel, done: done}
	s.mu.Unlock()

	go func() {
		defer close(done)
		if prevDone != nil {
			// Wait for the superseded run to fully stop before this one does
			// anything — including acquiring the single worker slot — so a
			// stale run can never write after (and clobber) a fresh one.
			// ctx-guarded: if THIS job itself gets superseded while still
			// parked here (a rapid edit-reindex-reindex loop), bail out
			// immediately instead of blocking on an already-stale
			// predecessor — the job that superseded us will do its own
			// prevDone wait against our `done`, which we close right away.
			select {
			case <-prevDone:
			case <-ctx.Done():
				return
			}
		}
		// Acquire the system-wide single-worker slot. Blocks (silently, in
		// the background) if another repo's enrichment is currently
		// trickling; releases as soon as this run returns.
		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-s.sem }()

		// Recover any panic raised by run (e.g. an emitter panicking on a
		// pathological/large graph) so it can never crash the daemon
		// asynchronously. This must be deferred AFTER the semaphore-acquire
		// defer and BEFORE calling run, so that on panic: this recover runs
		// first (stopping the unwind), then the semaphore-release defer
		// still runs, then close(done) still runs — the daemon survives,
		// the single worker slot is freed for the next job, and a
		// superseded successor parked on this job's `done` is never stuck.
		defer func() {
			if r := recover(); r != nil {
				slog.Default().Error(
					"enrichment: background worker panicked — recovered; daemon continues in degraded state (this run's candidates trickle was abandoned)",
					"repo", repoKey, "panic", r, "stack", string(debug.Stack()))
			}
		}()

		run(ctx)
	}()
}

// Wait blocks until the most recently scheduled job for repoKey has finished
// (or immediately returns if none was ever scheduled). Test-only helper — the
// production index path never waits, by design.
func (s *Scheduler) Wait(repoKey string) {
	s.mu.Lock()
	j, ok := s.jobs[repoKey]
	s.mu.Unlock()
	if !ok {
		return
	}
	<-j.done
}
