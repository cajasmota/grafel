package dashboard

// handlers_enrichment_progress_test.go — HTTP-level tests for
// GET /api/enrichments/{group}/progress (#1286).

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/jobs"
)

// TestHandleEnrichmentProgress_noQueue returns 200 with empty tiers when no
// job queue is wired.
func TestHandleEnrichmentProgress_noQueue(t *testing.T) {
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Seed group so the route guard passes.
	srv.graphs.mu.Lock()
	srv.graphs.entries["g1"] = &cacheEntry{
		group:    &DashGroup{Name: "g1", Repos: map[string]*DashRepo{}},
		loadedAt: time.Now().Add(60 * time.Second),
	}
	srv.graphs.mu.Unlock()

	w := doRequest(t, srv, "GET", "/api/enrichments/g1/progress")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp["overall_total"].(float64) != 0 {
		t.Errorf("want overall_total=0, got %v", resp["overall_total"])
	}
	tiers, ok := resp["tiers"].([]any)
	if !ok || len(tiers) != 4 {
		t.Errorf("want 4 tiers, got %v", resp["tiers"])
	}
}

// TestHandleEnrichmentProgress_withJobs checks that done/running/queued
// counts are bucketed correctly by criticality_band.
func TestHandleEnrichmentProgress_withJobs(t *testing.T) {
	srv, q := newJobsServer(t)

	// Enqueue some jobs with explicit bands.
	// Use a 0-worker sub-queue trick: we enqueue directly to inspect counts.
	// The main queue (workerCount=2) will pick these up; we wait for them to
	// complete before querying the progress endpoint.

	// 2 critical jobs.
	id1, _ := q.Enqueue("g1", "entity::alpha", "describe_entity", "critical")
	id2, _ := q.Enqueue("g1", "entity::beta", "describe_entity", "critical")
	// 1 high job.
	id3, _ := q.Enqueue("g1", "entity::gamma", "describe_entity", "high")

	// Wait for all to finish (stub completes in 50ms each).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ids := []string{id1, id2, id3}
		done := 0
		for _, id := range ids {
			j, _ := q.Get(id)
			if j.Status == jobs.StatusDone || j.Status == jobs.StatusFailed {
				done++
			}
		}
		if done == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	w := doRequest(t, srv, "GET", "/api/enrichments/g1/progress")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Tiers []struct {
			Band    string  `json:"band"`
			Total   float64 `json:"total"`
			Done    float64 `json:"done"`
			Queued  float64 `json:"queued"`
			Running float64 `json:"running"`
		} `json:"tiers"`
		OverallDone  float64 `json:"overall_done"`
		OverallTotal float64 `json:"overall_total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.OverallTotal != 3 {
		t.Errorf("want overall_total=3, got %v", resp.OverallTotal)
	}
	if resp.OverallDone != 3 {
		t.Errorf("want overall_done=3, got %v", resp.OverallDone)
	}

	// Find the critical tier.
	var criticalTier *struct {
		Band    string
		Total   float64
		Done    float64
		Queued  float64
		Running float64
	}
	for i := range resp.Tiers {
		if resp.Tiers[i].Band == "critical" {
			tier := resp.Tiers[i]
			criticalTier = &struct {
				Band    string
				Total   float64
				Done    float64
				Queued  float64
				Running float64
			}{tier.Band, tier.Total, tier.Done, tier.Queued, tier.Running}
			break
		}
	}
	if criticalTier == nil {
		t.Fatal("critical tier not found in response")
	}
	if criticalTier.Total != 2 {
		t.Errorf("critical.total want 2, got %v", criticalTier.Total)
	}
	if criticalTier.Done != 2 {
		t.Errorf("critical.done want 2, got %v", criticalTier.Done)
	}
}

// TestHandleEnrichmentProgress_bandFallback checks that a job with an empty
// criticality_band is counted in the "low" bucket.
func TestHandleEnrichmentProgress_bandFallback(t *testing.T) {
	srv, q := newJobsServer(t)

	id, _ := q.Enqueue("g1", "entity::x", "describe_entity", "") // empty band → low

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := q.Get(id)
		if j.Status == jobs.StatusDone || j.Status == jobs.StatusFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	w := doRequest(t, srv, "GET", "/api/enrichments/g1/progress")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	var resp struct {
		Tiers []struct {
			Band  string  `json:"band"`
			Total float64 `json:"total"`
		} `json:"tiers"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	for _, tier := range resp.Tiers {
		if tier.Band == "low" && tier.Total != 1 {
			t.Errorf("low.total want 1, got %v", tier.Total)
		}
		if tier.Band != "low" && tier.Total != 0 {
			t.Errorf("%s.total want 0, got %v", tier.Band, tier.Total)
		}
	}
}

// TestHandleEnrichmentProgress_unknownGroup returns 200 with empty tiers for an
// unknown group (no jobs → all-zero counts). The progress endpoint intentionally
// does not 404 on unknown groups: the graph cache may not be warmed yet but the
// job queue is always queryable.
func TestHandleEnrichmentProgress_unknownGroup(t *testing.T) {
	srv, _ := newJobsServer(t)
	w := doRequest(t, srv, "GET", "/api/enrichments/nonexistent/progress")
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	var resp struct {
		OverallTotal float64 `json:"overall_total"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.OverallTotal != 0 {
		t.Errorf("want overall_total=0 for unknown group, got %v", resp.OverallTotal)
	}
}
