// Package jobs provides an in-memory enrichment job queue with optional
// disk persistence and a stub MCP dispatch mechanism.
//
// Design constraints (issue #1244):
//   - Stdlib-only, no external dependencies.
//   - Worker pool with configurable concurrency (default 2).
//   - Jobs persist to ~/.archigraph/jobs.jsonl for history across restarts.
//   - MCP agent invocation is stubbed: logs "would call agent X with prompt Y".
//     Real integration is a follow-up.
//   - Cancel and per-job timeout (default 5 min) are supported.
//   - Status progression: queued → running → done | failed.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Status values for a Job.
const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// DefaultWorkers is the default worker-pool concurrency limit.
const DefaultWorkers = 2

// DefaultTimeout is the per-job execution timeout.
const DefaultTimeout = 5 * time.Minute

// Job is one enrichment dispatch request.
type Job struct {
	ID         string    `json:"id"`
	SubjectID  string    `json:"subject_id"`
	Kind       string    `json:"kind"`
	Group      string    `json:"group"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	QueuedAt   time.Time `json:"queued_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// CancelFn is not serialised — it is wired by the worker at runtime.
	cancelFn func()
}

// Queue is a concurrent, persistent enrichment job queue.
//
// Usage:
//
//	q := jobs.NewQueue(historyPath, 2)
//	q.Start()
//	id, _ := q.Enqueue("my-group", "flow::checkout", "describe_entity")
//	list := q.List()
//	q.Cancel(id)
//	q.Stop()
type Queue struct {
	mu          sync.RWMutex
	jobs        map[string]*Job // keyed by Job.ID
	order       []string        // insertion order for List()
	work        chan *Job
	workerCount int
	historyPath string // absolute path to jobs.jsonl; empty = no persistence
	wg          sync.WaitGroup
	stopOnce    sync.Once
	stopCh      chan struct{}
}

// NewQueue creates a Queue but does not start it. Call Start() to activate
// the worker pool. historyPath may be empty to disable persistence.
// workerCount < 0 defaults to DefaultWorkers; 0 is valid and means no workers
// will be launched (useful for tests that control job execution manually).
func NewQueue(historyPath string, workerCount int) *Queue {
	if workerCount < 0 {
		workerCount = DefaultWorkers
	}
	return &Queue{
		jobs:        make(map[string]*Job),
		work:        make(chan *Job, 512),
		workerCount: workerCount,
		historyPath: historyPath,
		stopCh:      make(chan struct{}),
	}
}

// Start launches the worker pool. Safe to call multiple times; subsequent
// calls are no-ops after the first.
func (q *Queue) Start() {
	for i := 0; i < q.workerCount; i++ {
		q.wg.Add(1)
		go q.worker()
	}
}

// Stop drains in-flight jobs and shuts down the worker pool. Blocks until
// all workers finish.
func (q *Queue) Stop() {
	q.stopOnce.Do(func() {
		close(q.stopCh)
	})
	q.wg.Wait()
}

// Enqueue creates a new job for (group, subjectID, kind) and returns its ID.
// The job is immediately placed in the work channel; a free worker will pick
// it up as soon as capacity allows.
func (q *Queue) Enqueue(group, subjectID, kind string) (string, error) {
	id := newJobID()
	job := &Job{
		ID:        id,
		SubjectID: subjectID,
		Kind:      kind,
		Group:     group,
		Status:    StatusQueued,
		QueuedAt:  time.Now().UTC(),
	}

	q.mu.Lock()
	q.jobs[id] = job
	q.order = append(q.order, id)
	q.mu.Unlock()

	q.appendHistory(job)

	select {
	case q.work <- job:
	default:
		// Channel full — mark failed immediately so the caller gets feedback.
		q.mu.Lock()
		job.Status = StatusFailed
		job.Error = "job queue full"
		q.mu.Unlock()
		q.appendHistory(job)
		return id, fmt.Errorf("job queue full")
	}
	return id, nil
}

// Cancel requests cancellation of the job identified by id. If the job is
// still queued, it is marked failed immediately. If it is running, its
// context is cancelled. No-op for done/failed jobs.
func (q *Queue) Cancel(id string) {
	q.mu.Lock()
	job, ok := q.jobs[id]
	if !ok {
		q.mu.Unlock()
		return
	}
	switch job.Status {
	case StatusQueued:
		job.Status = StatusFailed
		job.Error = "cancelled"
		q.mu.Unlock()
		q.appendHistory(job)
	case StatusRunning:
		cancel := job.cancelFn
		q.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	default:
		q.mu.Unlock()
	}
}

// Get returns a copy of the job with the given id, or false if not found.
func (q *Queue) Get(id string) (Job, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	j, ok := q.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}

// List returns all jobs in insertion order (most-recent enqueued last).
func (q *Queue) List() []Job {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]Job, 0, len(q.order))
	for _, id := range q.order {
		if j, ok := q.jobs[id]; ok {
			out = append(out, *j)
		}
	}
	return out
}

// ListForGroup returns jobs filtered to group, newest-first.
func (q *Queue) ListForGroup(group string) []Job {
	all := q.List()
	var out []Job
	for _, j := range all {
		if j.Group == group {
			out = append(out, j)
		}
	}
	// Reverse so newest-first.
	sort.Slice(out, func(i, k int) bool {
		return out[i].QueuedAt.After(out[k].QueuedAt)
	})
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Worker internals
// ─────────────────────────────────────────────────────────────────────────────

func (q *Queue) worker() {
	defer q.wg.Done()
	for {
		select {
		case <-q.stopCh:
			return
		case job, ok := <-q.work:
			if !ok {
				return
			}
			q.execute(job)
		}
	}
}

func (q *Queue) execute(job *Job) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)

	now := time.Now().UTC()
	q.mu.Lock()
	// Guard: job may have been cancelled while queued.
	if job.Status != StatusQueued {
		q.mu.Unlock()
		cancel()
		return
	}
	job.Status = StatusRunning
	job.StartedAt = &now
	job.cancelFn = cancel
	q.mu.Unlock()

	q.appendHistory(job)

	err := dispatchAgent(ctx, job)
	cancel() // always release context

	fin := time.Now().UTC()
	q.mu.Lock()
	job.FinishedAt = &fin
	job.cancelFn = nil
	if err != nil {
		job.Status = StatusFailed
		job.Error = err.Error()
	} else {
		job.Status = StatusDone
	}
	q.mu.Unlock()

	q.appendHistory(job)
}

// dispatchAgent is the stub MCP dispatch. It logs what it would do and
// returns nil (success) after a brief simulated delay. Real agent invocation
// (via Claude Code MCP or Cursor) is wired in a follow-up issue.
func dispatchAgent(ctx context.Context, job *Job) error {
	prompt := buildEnrichmentPrompt(job)
	log.Printf("[jobs] would invoke agent for job %s: subject=%s kind=%s group=%s",
		job.ID, job.SubjectID, job.Kind, job.Group)
	log.Printf("[jobs] agent prompt (stub): %s", prompt)

	// Simulate a short async operation so the status transitions are
	// observable in tests and integration scenarios.
	select {
	case <-ctx.Done():
		return fmt.Errorf("job %s timed out or cancelled: %w", job.ID, ctx.Err())
	case <-time.After(50 * time.Millisecond):
		// Stub success — real agent call replaces this block.
		log.Printf("[jobs] job %s completed (stub — no real agent invoked)", job.ID)
		return nil
	}
}

// buildEnrichmentPrompt constructs the natural-language prompt that would be
// sent to the coding agent. Exported so tests can inspect its shape.
func buildEnrichmentPrompt(job *Job) string {
	return fmt.Sprintf(
		"Enrich entity '%s' (kind: %s) in group '%s'. "+
			"Generate or update the YAML frontmatter doc for this entity, "+
			"filling summary, preconditions, expected_outcome, steps, and gaps. "+
			"Write the result to the standard enrichment doc path for this entity.",
		job.SubjectID, job.Kind, job.Group,
	)
}

// ─────────────────────────────────────────────────────────────────────────────
// Persistence helpers
// ─────────────────────────────────────────────────────────────────────────────

// appendHistory writes one JSON line per job event to historyPath (JSONL).
// Errors are logged but never fatal — history is best-effort.
func (q *Queue) appendHistory(job *Job) {
	if q.historyPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(q.historyPath), 0o700); err != nil {
		log.Printf("[jobs] history mkdir: %v", err)
		return
	}
	q.mu.RLock()
	data, err := json.Marshal(job)
	q.mu.RUnlock()
	if err != nil {
		log.Printf("[jobs] history marshal: %v", err)
		return
	}
	f, err := os.OpenFile(q.historyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("[jobs] history open: %v", err)
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s\n", data)
}

// ─────────────────────────────────────────────────────────────────────────────
// ID generation
// ─────────────────────────────────────────────────────────────────────────────

var (
	idMu  sync.Mutex
	idSeq uint64
)

// newJobID returns a time-prefixed, monotonic, URL-safe job ID.
// Format: job-<unix-ms>-<seq>
func newJobID() string {
	idMu.Lock()
	idSeq++
	seq := idSeq
	idMu.Unlock()
	return fmt.Sprintf("job-%d-%d", time.Now().UnixMilli(), seq)
}
