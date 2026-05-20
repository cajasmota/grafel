package dashboard

// handlers_action_test.go — unit tests for POST /api/{enrichments|repairs}/{group}/action
// (#1016). Uses a temp-dir-backed repo so reads/writes land in <t.TempDir()>/.archigraph/.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newActionServer creates a test HTTP server with a DashGroup whose "svc"
// repo points to repoPath.  The caller is responsible for creating the
// .archigraph directory and seeding any candidate files before calling
// this helper.
func newActionServer(t *testing.T, repoPath string) *httptest.Server {
	t.Helper()
	st := newFakeStore()
	st.groups["testgroup"] = GroupSummary{
		Name:       "testgroup",
		ConfigPath: "/tmp/testgroup.json",
		Repos:      []string{"svc"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	grp := &DashGroup{
		Name: "testgroup",
		Repos: map[string]*DashRepo{
			"svc": {Slug: "svc", Path: repoPath},
		},
		Links: []CrossRepoLink{},
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["testgroup"] = &cacheEntry{
		group:    grp,
		loadedAt: time.Now().Add(60 * time.Second), // never expire during test
	}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts
}

// seedEnrichmentCandidates writes an enrichment-candidates.json with the
// given candidates under <repoPath>/.archigraph/.
func seedEnrichmentCandidates(t *testing.T, repoPath string, cs []candidateRaw) {
	t.Helper()
	archDir := filepath.Join(repoPath, ".archigraph")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		t.Fatalf("marshal candidates: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archDir, "enrichment-candidates.json"), data, 0o644); err != nil {
		t.Fatalf("write candidates: %v", err)
	}
}

// postAction sends a POST to url with the given body and returns the status
// code plus parsed JSON body.
func postAction(t *testing.T, url string, body any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw)) //nolint:noctx
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// ---------------------------------------------------------------------------
// Validation tests
// ---------------------------------------------------------------------------

func TestCandidateAction_MissingCandidateID(t *testing.T) {
	repoPath := t.TempDir()
	ts := newActionServer(t, repoPath)
	code, body := postAction(t, ts.URL+"/api/enrichments/testgroup/action", map[string]any{
		"action": "reject",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%v", code, body)
	}
	if !strings.Contains(body["error"].(string), "candidate_id") {
		t.Fatalf("error should mention candidate_id, got %v", body)
	}
}

func TestCandidateAction_InvalidAction(t *testing.T) {
	repoPath := t.TempDir()
	ts := newActionServer(t, repoPath)
	code, body := postAction(t, ts.URL+"/api/enrichments/testgroup/action", map[string]any{
		"candidate_id": "ec:abc",
		"action":       "invalid",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%v", code, body)
	}
}

func TestCandidateAction_UnknownCandidate_404(t *testing.T) {
	repoPath := t.TempDir()
	ts := newActionServer(t, repoPath)
	code, _ := postAction(t, ts.URL+"/api/enrichments/testgroup/action", map[string]any{
		"candidate_id": "ec:notexist",
		"action":       "reject",
	})
	if code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

func TestCandidateAction_BadGroup_404(t *testing.T) {
	repoPath := t.TempDir()
	ts := newActionServer(t, repoPath)
	code, _ := postAction(t, ts.URL+"/api/enrichments/nosuchgroup/action", map[string]any{
		"candidate_id": "ec:abc",
		"action":       "reject",
	})
	if code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

// ---------------------------------------------------------------------------
// Reject enrichment candidate
// ---------------------------------------------------------------------------

func TestCandidateAction_Reject_Enrichment(t *testing.T) {
	repoPath := t.TempDir()
	const candidateID = "ec:aabbccddeeff0011"
	candidates := []candidateRaw{
		{
			ID:           candidateID,
			Kind:         "describe_entity",
			SubjectID:    "entity:e1",
			Confidence:   0.7,
			DiscoveredAt: "2024-01-01T00:00:00Z",
			Context:      map[string]any{"name": "UserService"},
		},
		{
			ID:           "ec:other",
			Kind:         "describe_entity",
			SubjectID:    "entity:e2",
			Confidence:   0.6,
			DiscoveredAt: "2024-01-01T00:00:00Z",
		},
	}
	seedEnrichmentCandidates(t, repoPath, candidates)

	ts := newActionServer(t, repoPath)
	code, body := postAction(t, ts.URL+"/api/enrichments/testgroup/action", map[string]any{
		"candidate_id": candidateID,
		"action":       "reject",
		"reason":       "not needed",
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%v", code, body)
	}
	if body["success"] != true {
		t.Fatalf("expected success=true, got %v", body)
	}
	if body["updated_candidate_id"] != candidateID {
		t.Fatalf("wrong updated_candidate_id: %v", body)
	}

	// Verify rejection was written.
	rejPath := filepath.Join(repoPath, ".archigraph", "enrichment-rejections.json")
	rejData, err := os.ReadFile(rejPath)
	if err != nil {
		t.Fatalf("rejection file not created: %v", err)
	}
	var rejs []map[string]any
	if err := json.Unmarshal(rejData, &rejs); err != nil {
		t.Fatalf("parse rejections: %v", err)
	}
	if len(rejs) == 0 {
		t.Fatal("expected at least one rejection entry")
	}

	// Verify candidate was removed.
	archDir := filepath.Join(repoPath, ".archigraph")
	remaining := readAllCandidates(repoPath)
	// The candidates file is under repoPath (StateDirForRepo uses repoPath/.archigraph)
	_ = archDir
	for _, c := range remaining {
		if c.ID == candidateID {
			t.Fatalf("candidate %s was not removed from candidates file", candidateID)
		}
	}
}

// ---------------------------------------------------------------------------
// Apply enrichment candidate
// ---------------------------------------------------------------------------

func TestCandidateAction_Apply_Enrichment(t *testing.T) {
	repoPath := t.TempDir()
	const candidateID = "ec:112233445566"
	candidates := []candidateRaw{
		{
			ID:           candidateID,
			Kind:         "describe_entity",
			SubjectID:    "entity:e3",
			Confidence:   0.9,
			DiscoveredAt: "2024-01-01T00:00:00Z",
		},
	}
	seedEnrichmentCandidates(t, repoPath, candidates)

	ts := newActionServer(t, repoPath)
	code, body := postAction(t, ts.URL+"/api/enrichments/testgroup/action", map[string]any{
		"candidate_id": candidateID,
		"action":       "apply",
		"value":        "Handles user authentication and session management.",
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%v", code, body)
	}
	if body["success"] != true {
		t.Fatalf("expected success=true, got %v", body)
	}

	// Verify resolution was written.
	resPath := filepath.Join(repoPath, ".archigraph", "enrichment-resolutions.json")
	resData, err := os.ReadFile(resPath)
	if err != nil {
		t.Fatalf("resolution file not created: %v", err)
	}
	var ress []map[string]any
	if err := json.Unmarshal(resData, &ress); err != nil {
		t.Fatalf("parse resolutions: %v", err)
	}
	if len(ress) == 0 {
		t.Fatal("expected at least one resolution entry")
	}
	res := ress[0]
	if res["kind"] != "describe_entity" {
		t.Fatalf("wrong kind: %v", res["kind"])
	}
	if res["value"] != "Handles user authentication and session management." {
		t.Fatalf("wrong value: %v", res["value"])
	}

	// Verify candidate was removed.
	remaining := readAllCandidates(repoPath)
	for _, c := range remaining {
		if c.ID == candidateID {
			t.Fatalf("candidate %s still present after apply", candidateID)
		}
	}
}

// ---------------------------------------------------------------------------
// Repair endpoint enforces repair-kind partition
// ---------------------------------------------------------------------------

func TestCandidateAction_RepairEndpoint_RefusesEnrichmentKind(t *testing.T) {
	repoPath := t.TempDir()
	const candidateID = "ec:aabb1122"
	// Seed a non-repair candidate (describe_entity).
	candidates := []candidateRaw{
		{
			ID:           candidateID,
			Kind:         "describe_entity",
			SubjectID:    "entity:e1",
			Confidence:   0.7,
			DiscoveredAt: "2024-01-01T00:00:00Z",
		},
	}
	seedEnrichmentCandidates(t, repoPath, candidates)

	ts := newActionServer(t, repoPath)
	// POST to /api/repairs endpoint — should NOT find the enrichment candidate.
	code, _ := postAction(t, ts.URL+"/api/repairs/testgroup/action", map[string]any{
		"candidate_id": candidateID,
		"action":       "reject",
	})
	if code != http.StatusNotFound {
		t.Fatalf("repairs endpoint must not match enrichment candidates; got %d", code)
	}
}

func TestCandidateAction_EnrichmentEndpoint_RefusesRepairKind(t *testing.T) {
	repoPath := t.TempDir()
	const candidateID = "ec:repaircandidate"
	// Seed a repair_edge candidate.
	candidates := []candidateRaw{
		{
			ID:           candidateID,
			Kind:         "repair_edge",
			SubjectID:    "entity:e1",
			Confidence:   0.8,
			DiscoveredAt: "2024-01-01T00:00:00Z",
		},
	}
	seedEnrichmentCandidates(t, repoPath, candidates)

	ts := newActionServer(t, repoPath)
	// POST to /api/enrichments endpoint — should NOT find the repair candidate.
	code, _ := postAction(t, ts.URL+"/api/enrichments/testgroup/action", map[string]any{
		"candidate_id": candidateID,
		"action":       "reject",
	})
	if code != http.StatusNotFound {
		t.Fatalf("enrichments endpoint must not match repair candidates; got %d", code)
	}
}

// ---------------------------------------------------------------------------
// Repair candidate apply/reject via repairs endpoint
// ---------------------------------------------------------------------------

func TestCandidateAction_Apply_Repair(t *testing.T) {
	repoPath := t.TempDir()
	const candidateID = "ec:repairtest"
	candidates := []candidateRaw{
		{
			ID:           candidateID,
			Kind:         "repair_edge",
			SubjectID:    "entity:e5",
			Confidence:   0.95,
			DiscoveredAt: "2024-01-01T00:00:00Z",
			Context:      map[string]any{"edge_id": "someedgehash"},
		},
	}
	seedEnrichmentCandidates(t, repoPath, candidates)

	ts := newActionServer(t, repoPath)
	code, body := postAction(t, ts.URL+"/api/repairs/testgroup/action", map[string]any{
		"candidate_id": candidateID,
		"action":       "apply",
		"value":        "bind_to_entity",
		"reason":       "Manually confirmed binding.",
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%v", code, body)
	}
	if body["success"] != true {
		t.Fatalf("expected success=true, got %v", body)
	}

	// Verify resolution written.
	resPath := filepath.Join(repoPath, ".archigraph", "enrichment-resolutions.json")
	if _, err := os.Stat(resPath); os.IsNotExist(err) {
		t.Fatal("resolution file not created for repair candidate")
	}
}
