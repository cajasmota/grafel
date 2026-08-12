// v2_jobs.go — async action-job registry for WebUI v2 (#1512).
//
// The v2 Operations + Settings screens trigger long-running actions
// (rebuild, reset). Per the backend-v2 async convention these handlers MUST
// NOT block the HTTP request on the work itself: the daemon must keep serving
// reads while a rebuild runs (consistent with the #1487 serving-mutex fix).
//
// Design:
//
//   - POST .../rebuild | .../reset enqueues an actionJob, kicks off the work
//     in a background goroutine, and returns 202 immediately with a job id.
//   - GET  /api/v2/jobs/{id}        polls the job status/progress.
//   - GET  /api/v2/jobs/{id}/stream is the SSE feed of status transitions.
//
// The actual indexing is NOT performed in this process. We dial the daemon
// and fire the existing `Rebuild` RPC (the same path the CLI `grafel
// rebuild` command and the v1 /api/groups/{group}/rebuild handler use). The
// goroutine simply tracks the RPC lifecycle so the job surfaces a clean
// status. Live indexing progress is still available via the existing
// /api/index-progress/{group} SSE stream, keyed on the progress token we
// embed in the job.
//
// This registry is in-memory + bounded; jobs are pruned after a TTL so a
// long-lived daemon does not accumulate unbounded state (memory-safe per the
// repo's memory discipline).

package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Action-job status values.
const (
	actionJobQueued  = "queued"
	actionJobRunning = "running"
	actionJobDone    = "done"
	actionJobFailed  = "failed"
)

// actionJobTTL is how long a finished job is retained for polling before it is
// pruned from the registry.
const actionJobTTL = 30 * time.Minute

// jobStreamHeartbeat is the SSE keepalive cadence for the job-status stream.
// `job` events already fire on every status transition; this is only the
// idle-keepalive. It was 15s, long enough that a fast job could begin and
// finish between two ticks and leave the stream looking dead (#1527). 1s is
// sub-second-perceptible without being chatty.
const jobStreamHeartbeat = 1 * time.Second

// actionJob is one async action (rebuild/reset) tracked by the daemon.
type actionJob struct {
	ID            string `json:"id"`
	Op            string `json:"op"` // "rebuild" | "reset"
	Group         string `json:"group"`
	Repo          string `json:"repo,omitempty"`
	Status        string `json:"status"`
	Progress      int    `json:"progress"` // 0..100, coarse
	Message       string `json:"message,omitempty"`
	Error         string `json:"error,omitempty"`
	ProgressToken string `json:"progress_token"`
	QueuedAt      int64  `json:"queued_at"` // unix-ms
	StartedAt     *int64 `json:"started_at,omitempty"`
	FinishedAt    *int64 `json:"finished_at,omitempty"`
}

// jobSub holds one SSE subscriber's channel plus the guard that orders a
// fan-out send against the close performed by that subscriber's cancel func.
//
// #6299 — this registry is the fourth copy of the progress broker's fan-out
// shape, and it was the worst of them: update snapshotted the subscriber
// channels under r.mu, released it, and sent outside it, while cancel closed the
// channel entirely OUTSIDE r.mu (the three brokers at least closed under the
// write lock). A tab closing on GET /api/v2/jobs/{id}/stream while runRebuildJob
// pushes a status transition therefore raced, and could raise
// `panic: send on closed channel` on a net/http goroutine — a daemon crash.
//
// mu restores the ordering per subscriber rather than per registry: publishers
// take it for reading, the single cancel path takes it for writing. The guarded
// region contains nothing that can block — a bool test and one non-blocking
// send — so a slow SSE client still cannot hold up a worker.
type jobSub struct {
	mu     sync.RWMutex
	closed bool
	ch     chan actionJob
}

// send delivers j unless the subscriber has been cancelled or its buffer is
// full. It never blocks: dropping on a slow consumer is deliberate, so a stalled
// SSE client cannot stall the rebuild worker.
func (s *jobSub) send(j actionJob) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		// Cancelled — the consumer has gone away. Dropping is correct; the job
		// state itself lives in the registry and is readable via GET /jobs/{id}.
		return
	}
	select {
	case s.ch <- j:
	default:
	}
}

// close closes the subscriber's channel so handleV2JobStream's `j, open := <-ch`
// arm fires. It is idempotent — unlike the pre-#6299 cancel, which had neither a
// sync.Once nor a closed guard and panicked with `close of closed channel` on a
// second call — and it excludes any concurrent send.
func (s *jobSub) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

// actionJobRegistry is a concurrent, TTL-pruned store of action jobs plus a
// per-job subscriber set for SSE streaming.
type actionJobRegistry struct {
	mu   sync.RWMutex
	jobs map[string]*actionJob
	subs map[string]map[*jobSub]struct{}
}

func newActionJobRegistry() *actionJobRegistry {
	return &actionJobRegistry{
		jobs: make(map[string]*actionJob),
		subs: make(map[string]map[*jobSub]struct{}),
	}
}

// create inserts a new queued job and returns a copy.
func (r *actionJobRegistry) create(op, group, repo, token string) actionJob {
	id := fmt.Sprintf("aj-%d", time.Now().UnixNano())
	j := &actionJob{
		ID:            id,
		Op:            op,
		Group:         group,
		Repo:          repo,
		Status:        actionJobQueued,
		ProgressToken: token,
		QueuedAt:      time.Now().UnixMilli(),
	}
	r.mu.Lock()
	r.pruneLocked()
	r.jobs[id] = j
	r.mu.Unlock()
	return *j
}

// update applies a mutation to the job under lock, then notifies subscribers
// with a copy. mutate runs while holding the lock; keep it cheap.
func (r *actionJobRegistry) update(id string, mutate func(*actionJob)) {
	r.mu.Lock()
	j, ok := r.jobs[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	mutate(j)
	snapshot := *j
	subs := r.subs[id]
	targets := make([]*jobSub, 0, len(subs))
	for s := range subs {
		targets = append(targets, s)
	}
	r.mu.Unlock()

	// The registry lock is released before the fan-out so a slow subscriber never
	// delays the worker; each send takes only that subscriber's own guard, held
	// for a non-blocking operation (see jobSub.send, #6299).
	for _, s := range targets {
		s.send(snapshot)
	}
}

// get returns a copy of the job, or false.
func (r *actionJobRegistry) get(id string) (actionJob, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	j, ok := r.jobs[id]
	if !ok {
		return actionJob{}, false
	}
	return *j, true
}

// subscribe registers a channel for job updates. The returned cancel func
// removes and closes it, and is safe to call more than once. The current
// snapshot is delivered immediately.
func (r *actionJobRegistry) subscribe(id string) (<-chan actionJob, func(), bool) {
	r.mu.Lock()
	j, ok := r.jobs[id]
	if !ok {
		r.mu.Unlock()
		return nil, nil, false
	}
	s := &jobSub{ch: make(chan actionJob, 8)}
	if r.subs[id] == nil {
		r.subs[id] = make(map[*jobSub]struct{})
	}
	r.subs[id][s] = struct{}{}
	snapshot := *j
	r.mu.Unlock()

	// Deliver the current state first so a late subscriber is not stuck. Routed
	// through the guard for uniformity; the channel is freshly made with capacity
	// 8 and no one else holds it yet, so this always lands.
	s.send(snapshot)

	cancel := func() {
		r.mu.Lock()
		if set, ok := r.subs[id]; ok {
			delete(set, s)
			if len(set) == 0 {
				delete(r.subs, id)
			}
		}
		r.mu.Unlock()
		// Ordered against any in-flight fan-out send, and idempotent (#6299).
		// Lock order is r.mu -> jobSub.mu and never the reverse; the close is
		// outside r.mu, which is safe now that jobSub.mu is what orders it.
		s.close()
	}
	return s.ch, cancel, true
}

// pruneLocked drops finished jobs older than the TTL. Caller holds r.mu.
func (r *actionJobRegistry) pruneLocked() {
	cutoff := time.Now().Add(-actionJobTTL).UnixMilli()
	for id, j := range r.jobs {
		if j.FinishedAt != nil && *j.FinishedAt < cutoff {
			if _, streaming := r.subs[id]; !streaming {
				delete(r.jobs, id)
			}
		}
	}
}

// terminal returns true once a job has stopped transitioning.
func (j actionJob) terminal() bool {
	return j.Status == actionJobDone || j.Status == actionJobFailed
}

// ─────────────────────────────────────────────────────────────────────────────
// Job query handlers
// ─────────────────────────────────────────────────────────────────────────────

// handleV2JobGet — GET /api/v2/jobs/{id}
func (s *Server) handleV2JobGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j, ok := s.actionJobs.get(id)
	if !ok {
		writeV2Err(w, http.StatusNotFound, "not_found", fmt.Sprintf("job %q not found", id))
		return
	}
	writeV2JSON(w, http.StatusOK, v2OK(j))
}

// handleV2JobStream — GET /api/v2/jobs/{id}/stream (SSE).
//
// Emits the standard v2 SSE lifecycle: a `connected` event, one `job` event
// per status transition (and immediately for the current state), `heartbeat`
// every jobStreamHeartbeat (~1s), and a final `close` once the job reaches a
// terminal state.
func (s *Server) handleV2JobStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", "streaming not supported")
		return
	}

	ch, cancel, ok := s.actionJobs.subscribe(id)
	if !ok {
		writeV2Err(w, http.StatusNotFound, "not_found", fmt.Sprintf("job %q not found", id))
		return
	}
	defer cancel()

	setV2SSEHeaders(w)
	w.WriteHeader(http.StatusOK)

	connected, _ := json.Marshal(map[string]int64{"subscribed_at": time.Now().UnixMilli()})
	writeV2SSEEvent(w, "connected", string(connected))
	flusher.Flush()

	heartbeat := time.NewTicker(jobStreamHeartbeat)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			writeV2SSEEvent(w, "heartbeat", "{}")
			flusher.Flush()
		case j, open := <-ch:
			if !open {
				writeV2SSEEvent(w, "close", "{}")
				flusher.Flush()
				return
			}
			data, _ := json.Marshal(j)
			writeV2SSEEvent(w, "job", string(data))
			flusher.Flush()
			if j.terminal() {
				writeV2SSEEvent(w, "close", "{}")
				flusher.Flush()
				return
			}
		}
	}
}
