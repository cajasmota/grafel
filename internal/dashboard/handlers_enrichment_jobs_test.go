package dashboard

// handlers_enrichment_jobs_test.go — HTTP-level tests for enrichment job
// dispatch endpoints (#1244).
//
//   POST /api/enrichments/{group}/trigger
//   GET  /api/enrichments/{group}/jobs
//   POST /api/enrichments/{group}/jobs/{jobId}/cancel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/jobs"
)

// newJobsServer builds a minimal dashboard Server with a live job queue.
// The group "g1" is seeded into the graph cache so route guards pass.
func newJobsServer(t *testing.T) (*Server, *jobs.Queue) {
	t.Helper()
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	q := jobs.NewQueue("", 2)
	q.Start()
	t.Cleanup(q.Stop)
	srv.SetJobQueue(q)

	// Seed group "g1" into the graph cache so endpoint group-guards pass.
	srv.graphs.mu.Lock()
	srv.graphs.entries["g1"] = &cacheEntry{
		group: &DashGroup{
			Name:  "g1",
			Repos: map[string]*DashRepo{},
		},
		loadedAt: time.Now().Add(60 * time.Second),
	}
	srv.graphs.mu.Unlock()
	return srv, q
}

// doRequest fires one HTTP request against srv and returns the response.
func doRequest(t *testing.T, srv *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	return w
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/enrichments/{group}/trigger
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleEnrichmentTrigger_missingSubjectID(t *testing.T) {
	srv, _ := newJobsServer(t)
	w := doRequest(t, srv, "POST", "/api/enrichments/g1/trigger")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestHandleEnrichmentTrigger_success(t *testing.T) {
	srv, q := newJobsServer(t)
	w := doRequest(t, srv, "POST", "/api/enrichments/g1/trigger?subject_id=flow%3A%3Acheckout&kind=describe_entity")
	if w.Code != http.StatusAccepted {
		t.Errorf("want 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] == nil {
		t.Error("response missing status")
	}
	if resp["id"] == nil {
		t.Error("response missing id")
	}

	// Job must appear in queue.
	id := resp["id"].(string)
	_, ok := q.Get(id)
	if !ok {
		t.Errorf("job %s not found in queue", id)
	}
}

func TestHandleEnrichmentTrigger_noQueue(t *testing.T) {
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// jobQueue is nil — expect 503 ServiceUnavailable.
	w := doRequest(t, srv, "POST", "/api/enrichments/g1/trigger?subject_id=flow%3A%3Ax")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/enrichments/{group}/jobs
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleEnrichmentJobs_empty(t *testing.T) {
	srv, _ := newJobsServer(t)
	w := doRequest(t, srv, "GET", "/api/enrichments/g1/jobs")
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["total"].(float64) != 0 {
		t.Errorf("want total=0, got %v", resp["total"])
	}
}

func TestHandleEnrichmentJobs_afterTrigger(t *testing.T) {
	srv, q := newJobsServer(t)
	q.Enqueue("g1", "flow::checkout", "describe_entity")
	q.Enqueue("g1", "flow::payment", "describe_entity")
	q.Enqueue("g2", "flow::other", "describe_entity") // different group

	// Wait for jobs to settle.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		all := q.ListForGroup("g1")
		done := 0
		for _, j := range all {
			if j.Status == "done" || j.Status == "failed" {
				done++
			}
		}
		if done == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	w := doRequest(t, srv, "GET", "/api/enrichments/g1/jobs")
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	total := int(resp["total"].(float64))
	if total != 2 {
		t.Errorf("want total=2 for g1, got %d", total)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/enrichments/{group}/jobs/{jobId}/cancel
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleEnrichmentJobCancel_notFound(t *testing.T) {
	srv, _ := newJobsServer(t)
	w := doRequest(t, srv, "POST", "/api/enrichments/g1/jobs/job-999-1/cancel")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestHandleEnrichmentJobCancel_success(t *testing.T) {
	// Build a server with a queue that has no workers — jobs stay queued.
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// NewQueue with workerCount=0: Start() launches 0 goroutines, so jobs
	// never leave the queued state until cancelled.
	q := jobs.NewQueue("", 0)
	q.Start()
	t.Cleanup(q.Stop)
	srv.SetJobQueue(q)

	id, _ := q.Enqueue("g1", "flow::x", "describe_entity")

	target := "/api/enrichments/g1/jobs/" + id + "/cancel"
	w := doRequest(t, srv, "POST", target)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "failed" {
		t.Errorf("want status=failed after cancel, got %v", resp["status"])
	}
	errVal, _ := resp["error"].(string)
	if !strings.Contains(errVal, "cancel") {
		t.Errorf("want error containing 'cancel', got %q", errVal)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/flows/{group}/{processId}/trigger-enrichment (existing endpoint)
// now backed by queue
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleFlowTriggerEnrichment_withQueue(t *testing.T) {
	// newJobsServer already seeds g1 in the graph cache.
	srv, q := newJobsServer(t)

	w := doRequest(t, srv, "POST", "/api/flows/g1/flow%3A%3Acheckout/trigger-enrichment")
	if w.Code != http.StatusAccepted {
		t.Errorf("want 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["id"] == nil {
		t.Error("expected job id in response")
	}
	if resp["status"] == nil {
		t.Error("expected status in response")
	}

	// Verify job is tracked in queue.
	all := q.List()
	if len(all) == 0 {
		t.Error("expected at least one job in queue after trigger")
	}
}
