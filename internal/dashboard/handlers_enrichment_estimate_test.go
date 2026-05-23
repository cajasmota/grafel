package dashboard

// handlers_enrichment_estimate_test.go — HTTP-level tests for
// GET /api/enrichments/{group}/estimate (#1287).

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
)

// seedCandidates writes a minimal enrichment-candidates.json under the
// per-repo state dir determined by ARCHIGRAPH_DAEMON_ROOT, using the same
// path logic as StateDirForRepo so readAllCandidates picks them up.
func seedCandidates(t *testing.T, repoPath string, candidates []map[string]any) {
	t.Helper()
	stateDir := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll state dir: %v", err)
	}
	data, err := json.Marshal(candidates)
	if err != nil {
		t.Fatalf("marshal candidates: %v", err)
	}
	path := filepath.Join(stateDir, "enrichment-candidates.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write candidates: %v", err)
	}
}

// newEstimateServer creates a test server with group "g1" and a repo at repoPath.
// ARCHIGRAPH_DAEMON_ROOT must be set before calling so StateDirForRepo is hermetic.
func newEstimateServer(t *testing.T, repoPath string) *Server {
	t.Helper()
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["g1"] = &cacheEntry{
		group: &DashGroup{
			Name: "g1",
			Repos: map[string]*DashRepo{
				"repo-a": {Path: repoPath},
			},
		},
		loadedAt: time.Now().Add(60 * time.Second),
	}
	srv.graphs.mu.Unlock()
	return srv
}

// TestHandleEnrichmentEstimate_noGroup returns 400 when group is empty.
func TestHandleEnrichmentEstimate_noGroup(t *testing.T) {
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// The route pattern requires {group} so we hit the missing-group guard
	// by using an invalid path that maps to a non-existent group.
	w := doRequest(t, srv, "GET", "/api/enrichments/nonexistent/estimate")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestHandleEnrichmentEstimate_emptyCandidates returns 200 with zero totals
// when there are no pending candidates.
func TestHandleEnrichmentEstimate_emptyCandidates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	repoPath := filepath.Join(tmp, "repo-a")
	srv := newEstimateServer(t, repoPath)
	// No candidate file seeded.

	w := doRequest(t, srv, "GET", "/api/enrichments/g1/estimate")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp enrichmentEstimateResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TotalEstTokens != 0 {
		t.Errorf("want 0 total tokens, got %d", resp.TotalEstTokens)
	}
	if resp.TotalEstUSD != 0 {
		t.Errorf("want 0 total USD, got %f", resp.TotalEstUSD)
	}
	if len(resp.Tiers) != 4 {
		t.Errorf("want 4 tiers, got %d", len(resp.Tiers))
	}
}

// TestHandleEnrichmentEstimate_withCandidates validates token/USD breakdown.
func TestHandleEnrichmentEstimate_withCandidates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	repoPath := filepath.Join(tmp, "repo-a")
	srv := newEstimateServer(t, repoPath)

	// Seed 2 critical (http_endpoint → 900 tok+200 overhead = 1100) and
	// 3 high (Service → 600+200 = 800) candidates.
	candidates := []map[string]any{
		{"id": "c1", "kind": "http_endpoint", "subject_id": "ep-001", "criticality_band": "critical"},
		{"id": "c2", "kind": "http_endpoint", "subject_id": "ep-002", "criticality_band": "critical"},
		{"id": "c3", "kind": "Service", "subject_id": "svc-001", "criticality_band": "high"},
		{"id": "c4", "kind": "Service", "subject_id": "svc-002", "criticality_band": "high"},
		{"id": "c5", "kind": "Service", "subject_id": "svc-003", "criticality_band": "high"},
	}
	seedCandidates(t, repoPath, candidates)

	w := doRequest(t, srv, "GET", "/api/enrichments/g1/estimate")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp enrichmentEstimateResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Locate tiers by band.
	tierMap := make(map[string]estimateTier)
	for _, tier := range resp.Tiers {
		tierMap[tier.Band] = tier
	}

	// Critical: 2 entities × 1100 tokens = 2200 tokens.
	crit := tierMap["critical"]
	if crit.Count != 2 {
		t.Errorf("critical: want count=2, got %d", crit.Count)
	}
	if crit.EstTokens != 2200 {
		t.Errorf("critical: want est_tokens=2200, got %d", crit.EstTokens)
	}
	if crit.Model != "sonnet" {
		t.Errorf("critical: want model=sonnet, got %q", crit.Model)
	}

	// High: 3 entities × 800 tokens = 2400 tokens.
	high := tierMap["high"]
	if high.Count != 3 {
		t.Errorf("high: want count=3, got %d", high.Count)
	}
	if high.EstTokens != 2400 {
		t.Errorf("high: want est_tokens=2400, got %d", high.EstTokens)
	}
	if high.Model != "haiku" {
		t.Errorf("high: want model=haiku, got %q", high.Model)
	}

	// Total tokens = 2200 + 2400 = 4600.
	if resp.TotalEstTokens != 4600 {
		t.Errorf("want total_est_tokens=4600, got %d", resp.TotalEstTokens)
	}
	if resp.TotalEstUSD <= 0 {
		t.Errorf("want positive total_est_usd, got %f", resp.TotalEstUSD)
	}
	if resp.EstMinutes < 0 {
		t.Errorf("want non-negative est_minutes, got %f", resp.EstMinutes)
	}
}

// TestHandleEnrichmentEstimate_excludesAlreadyEnriched checks that entities
// with completed jobs are excluded from the estimate.
func TestHandleEnrichmentEstimate_excludesAlreadyEnriched(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(daemon.EnvRoot, tmp)

	repoPath := filepath.Join(tmp, "repo-a")
	srv, q := newJobsServer(t)

	// Wire the repo into the server's group.
	srv.graphs.mu.Lock()
	srv.graphs.entries["g1"].group.Repos["repo-a"] = &DashRepo{Path: repoPath}
	srv.graphs.mu.Unlock()

	// Seed 3 candidates — two are "critical", one "medium".
	candidates := []map[string]any{
		{"id": "c1", "kind": "http_endpoint", "subject_id": "ep-001", "criticality_band": "critical"},
		{"id": "c2", "kind": "http_endpoint", "subject_id": "ep-002", "criticality_band": "critical"},
		{"id": "c3", "kind": "Service", "subject_id": "svc-001", "criticality_band": "medium"},
	}
	seedCandidates(t, repoPath, candidates)

	// Enqueue and complete a job for ep-001 so it counts as "already enriched".
	id, err := q.Enqueue("g1", "ep-001", "describe_entity", "critical")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// Wait for the stub worker to finish the job.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := q.Get(id)
		if j.Status == "done" || j.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	w := doRequest(t, srv, "GET", "/api/enrichments/g1/estimate")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp enrichmentEstimateResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// 1 entity already enriched — excluded.
	if resp.AlreadyEnriched != 1 {
		t.Errorf("want already_enriched=1, got %d", resp.AlreadyEnriched)
	}

	// Only 2 candidates remain (ep-002 critical + svc-001 medium).
	total := 0
	for _, tier := range resp.Tiers {
		total += tier.Count
	}
	if total != 2 {
		t.Errorf("want 2 total pending candidates, got %d", total)
	}
}
