package dashboard

// handlers_enrichment_batch_test.go — HTTP-level tests for the batched
// enrichment endpoint (issue #1285).
//
//	POST /api/enrichments/{group}/batch-enrich

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/jobs"
)

// doJSONRequest fires a request with a JSON body against the server routes.
func doJSONRequest(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	return w
}

// decodeBatchResp decodes the response body into a batchEnrichResponse.
func decodeBatchResp(t *testing.T, w *httptest.ResponseRecorder) batchEnrichResponse {
	t.Helper()
	var resp batchEnrichResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, w.Body.String())
	}
	return resp
}

// newBatchJobsServer creates a jobs server with a 2-worker queue (processes jobs).
func newBatchJobsServer(t *testing.T) (*Server, *jobs.Queue) {
	t.Helper()
	return newJobsServer(t) // reuse existing helper from handlers_enrichment_jobs_test.go
}

// newZeroWorkerJobsServer creates a jobs server with a 0-worker queue (jobs stay queued).
func newZeroWorkerJobsServer(t *testing.T) (*Server, *jobs.Queue) {
	t.Helper()
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	q := jobs.NewQueue("", 0) // 0 workers — jobs never leave queued state
	q.Start()
	t.Cleanup(q.Stop)
	srv.SetJobQueue(q)
	return srv, q
}

// ─────────────────────────────────────────────────────────────────────────────
// Happy-path: 30 candidates → 30 jobs accepted
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleEnrichmentBatch_happyPath(t *testing.T) {
	srv, q := newBatchJobsServer(t)

	candidates := make([]batchCandidate, 30)
	for i := range candidates {
		candidates[i] = batchCandidate{
			EntityID:        fmt.Sprintf("ep-%03d", i),
			Kind:            "http_endpoint",
			CriticalityBand: "high",
		}
	}

	w := doJSONRequest(t, srv, "POST", "/api/enrichments/g1/batch-enrich",
		batchEnrichRequest{Candidates: candidates})

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeBatchResp(t, w)
	if resp.Accepted != 30 {
		t.Errorf("want accepted=30, got %d", resp.Accepted)
	}
	if resp.Rejected != 0 {
		t.Errorf("want rejected=0, got %d (%v)", resp.Rejected, resp.RejectionReasons)
	}
	if len(resp.JobIDs) != 30 {
		t.Errorf("want 30 job IDs, got %d", len(resp.JobIDs))
	}
	if resp.BatchID == "" {
		t.Error("want non-empty batch_id")
	}

	// All created jobs must be visible in the queue.
	for _, id := range resp.JobIDs {
		if _, ok := q.Get(id); !ok {
			t.Errorf("job %s not found in queue", id)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Oversized batch → 400
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleEnrichmentBatch_oversized(t *testing.T) {
	srv, _ := newBatchJobsServer(t)

	candidates := make([]batchCandidate, maxBatchSize+1)
	for i := range candidates {
		candidates[i] = batchCandidate{EntityID: fmt.Sprintf("ep-%03d", i)}
	}

	w := doJSONRequest(t, srv, "POST", "/api/enrichments/g1/batch-enrich",
		batchEnrichRequest{Candidates: candidates})

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Empty candidates list → 400
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleEnrichmentBatch_emptyCandidates(t *testing.T) {
	srv, _ := newBatchJobsServer(t)

	w := doJSONRequest(t, srv, "POST", "/api/enrichments/g1/batch-enrich",
		batchEnrichRequest{Candidates: []batchCandidate{}})

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for empty candidates, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Idempotency: already_queued rejection
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleEnrichmentBatch_alreadyQueued(t *testing.T) {
	// Zero-worker queue: jobs stay in "queued" state indefinitely.
	srv, q := newZeroWorkerJobsServer(t)

	// Pre-enqueue "ep-001" so it shows as already_queued.
	_, _ = q.Enqueue("g1", "ep-001", "http_endpoint", "")

	candidates := []batchCandidate{
		{EntityID: "ep-001", Kind: "http_endpoint"}, // already_queued
		{EntityID: "ep-002", Kind: "http_endpoint"}, // new — should be accepted
	}

	w := doJSONRequest(t, srv, "POST", "/api/enrichments/g1/batch-enrich",
		batchEnrichRequest{Candidates: candidates})

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeBatchResp(t, w)
	if resp.Accepted != 1 {
		t.Errorf("want accepted=1, got %d", resp.Accepted)
	}
	if resp.Rejected != 1 {
		t.Errorf("want rejected=1, got %d", resp.Rejected)
	}
	if len(resp.RejectionReasons) != 1 {
		t.Fatalf("want 1 rejection reason, got %d", len(resp.RejectionReasons))
	}
	if resp.RejectionReasons[0].EntityID != "ep-001" {
		t.Errorf("want ep-001 rejected, got %s", resp.RejectionReasons[0].EntityID)
	}
	if resp.RejectionReasons[0].Reason != "already_queued" {
		t.Errorf("want reason=already_queued, got %q", resp.RejectionReasons[0].Reason)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Idempotency: already_enriched rejection
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleEnrichmentBatch_alreadyEnriched(t *testing.T) {
	// Use a 2-worker queue so jobs actually run to "done" status.
	srv, q := newBatchJobsServer(t)

	// Enqueue "ep-done" and wait for it to reach done state.
	id, _ := q.Enqueue("g1", "ep-done", "http_endpoint", "")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, ok := q.Get(id)
		if ok && j.Status == jobs.StatusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	j, _ := q.Get(id)
	if j.Status != jobs.StatusDone {
		t.Fatalf("prerequisite: ep-done job never reached done state (status=%s)", j.Status)
	}

	// Now batch-enrich with ep-done (already_enriched) + ep-new (accepted).
	candidates := []batchCandidate{
		{EntityID: "ep-done", Kind: "http_endpoint"}, // already_enriched
		{EntityID: "ep-new", Kind: "http_endpoint"},  // new
	}

	w := doJSONRequest(t, srv, "POST", "/api/enrichments/g1/batch-enrich",
		batchEnrichRequest{Candidates: candidates})

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeBatchResp(t, w)
	if resp.Accepted != 1 {
		t.Errorf("want accepted=1, got %d", resp.Accepted)
	}
	if resp.Rejected != 1 {
		t.Errorf("want rejected=1, got %d", resp.Rejected)
	}
	if len(resp.RejectionReasons) != 1 {
		t.Fatalf("want 1 rejection reason, got %d", len(resp.RejectionReasons))
	}
	if resp.RejectionReasons[0].Reason != "already_enriched" {
		t.Errorf("want reason=already_enriched, got %q", resp.RejectionReasons[0].Reason)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// No queue wired → 503
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleEnrichmentBatch_noQueue(t *testing.T) {
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Do NOT call srv.SetJobQueue — leave it nil.

	w := doJSONRequest(t, srv, "POST", "/api/enrichments/g1/batch-enrich",
		batchEnrichRequest{Candidates: []batchCandidate{{EntityID: "ep-001"}}})

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Default kind: omitting kind field defaults to "describe_entity"
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleEnrichmentBatch_defaultKind(t *testing.T) {
	srv, q := newBatchJobsServer(t)

	w := doJSONRequest(t, srv, "POST", "/api/enrichments/g1/batch-enrich",
		batchEnrichRequest{
			Candidates: []batchCandidate{
				{EntityID: "ep-no-kind"}, // Kind intentionally omitted
			},
		})

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeBatchResp(t, w)
	if resp.Accepted != 1 {
		t.Fatalf("want accepted=1, got %d", resp.Accepted)
	}

	// Verify the created job has kind "describe_entity".
	job, ok := q.Get(resp.JobIDs[0])
	if !ok {
		t.Fatal("job not found in queue")
	}
	if job.Kind != "describe_entity" {
		t.Errorf("want kind=describe_entity, got %q", job.Kind)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Invalid JSON body → 400
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleEnrichmentBatch_invalidJSON(t *testing.T) {
	srv, _ := newBatchJobsServer(t)

	req := httptest.NewRequest("POST", "/api/enrichments/g1/batch-enrich",
		bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}
