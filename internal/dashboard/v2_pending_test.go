package dashboard

// v2_pending_test.go — unit tests for the v2 Pending screen endpoints (#1442).
//
// GET  /api/v2/groups/{group}/candidates
// PUT  /api/v2/groups/{group}/candidates/{cid}/hint
//
// Tests inject a DashGroup directly into the graph cache so they don't
// depend on the registry or disk graph files.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newPendingServer creates an httptest.Server wired to a group whose single
// repo lives at repoPath.  The caller seeds candidates in the repo state dir
// before making requests.
func newPendingServer(t *testing.T, group, repoSlug, repoPath string) *httptest.Server {
	t.Helper()
	st := newFakeStore()
	st.groups[group] = GroupSummary{
		Name:       group,
		ConfigPath: "/tmp/" + group + ".json",
		Repos:      []string{repoSlug},
	}
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	grp := &DashGroup{
		Name: group,
		Repos: map[string]*DashRepo{
			repoSlug: {Slug: repoSlug, Path: repoPath},
		},
		Links: []CrossRepoLink{},
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries[group] = &cacheEntry{
		group:    grp,
		loadedAt: time.Now().Add(60 * time.Second),
	}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts
}

// seedPendingCandidates writes candidates to <repoPath>/.archigraph/enrichment-candidates.json.
func seedPendingCandidates(t *testing.T, repoPath string, cs []candidateRaw) {
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

// ---------------------------------------------------------------------------
// GET /api/v2/groups/{group}/candidates
// ---------------------------------------------------------------------------

func TestHandleV2Candidates_repairKind(t *testing.T) {
	repoPath := t.TempDir()
	seedPendingCandidates(t, repoPath, []candidateRaw{
		{
			ID:           "c1",
			Kind:         "repair_edge",
			SubjectID:    "pkg.Foo",
			Confidence:   0.9,
			DiscoveredAt: "2026-01-01T00:00:00Z",
			Context: map[string]any{
				"entity_name": "Foo",
				"entity_type": "function",
				"file":        "pkg/foo.go:10",
			},
		},
	})

	ts := newPendingServer(t, "grp", "repo1", repoPath)
	resp, err := http.Get(ts.URL + "/api/v2/groups/grp/candidates")
	if err != nil {
		t.Fatalf("GET candidates: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var env struct {
		OK   bool                 `json:"ok"`
		Data v2CandidatesResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.OK {
		t.Fatal("expected ok:true")
	}
	if len(env.Data.Repairs) != 1 {
		t.Fatalf("want 1 repair, got %d", len(env.Data.Repairs))
	}
	r := env.Data.Repairs[0]
	if r.ID != "c1" {
		t.Errorf("want id c1, got %s", r.ID)
	}
	if r.IssueType != "broken_link" {
		t.Errorf("want issueType broken_link, got %s", r.IssueType)
	}
	if r.Entity.Name != "Foo" {
		t.Errorf("want entity.name Foo, got %s", r.Entity.Name)
	}
	if r.Entity.Type != "function" {
		t.Errorf("want entity.type function, got %s", r.Entity.Type)
	}
	if r.Entity.File != "pkg/foo.go:10" {
		t.Errorf("want entity.file pkg/foo.go:10, got %s", r.Entity.File)
	}
	if r.DetectedAt == 0 {
		t.Error("want non-zero detectedAt")
	}
	if len(env.Data.Enrichments) != 0 {
		t.Errorf("want 0 enrichments, got %d", len(env.Data.Enrichments))
	}
}

func TestHandleV2Candidates_enrichmentKind(t *testing.T) {
	repoPath := t.TempDir()
	seedPendingCandidates(t, repoPath, []candidateRaw{
		{
			ID:        "e1",
			Kind:      "describe_entity",
			SubjectID: "pkg.Bar",
			Confidence: 0.75,
			Context: map[string]any{
				"entity_name": "Bar",
				"entity_type": "class",
				"file":        "pkg/bar.go:5",
			},
		},
	})

	ts := newPendingServer(t, "grp2", "repo1", repoPath)
	resp, err := http.Get(ts.URL + "/api/v2/groups/grp2/candidates")
	if err != nil {
		t.Fatalf("GET candidates: %v", err)
	}
	defer resp.Body.Close()

	var env struct {
		Data v2CandidatesResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Repairs) != 0 {
		t.Errorf("want 0 repairs, got %d", len(env.Data.Repairs))
	}
	if len(env.Data.Enrichments) != 1 {
		t.Fatalf("want 1 enrichment, got %d", len(env.Data.Enrichments))
	}
	e := env.Data.Enrichments[0]
	if e.EnrichmentType != "summary" {
		t.Errorf("want enrichmentType summary, got %s", e.EnrichmentType)
	}
	if e.Entity.Name != "Bar" {
		t.Errorf("want entity.name Bar, got %s", e.Entity.Name)
	}
}

func TestHandleV2Candidates_tabRepairsFilter(t *testing.T) {
	repoPath := t.TempDir()
	seedPendingCandidates(t, repoPath, []candidateRaw{
		{ID: "r1", Kind: "repair_edge", SubjectID: "A", Confidence: 0.8},
		{ID: "e1", Kind: "describe_entity", SubjectID: "B", Confidence: 0.8},
	})

	ts := newPendingServer(t, "grp3", "repo1", repoPath)
	resp, err := http.Get(ts.URL + "/api/v2/groups/grp3/candidates?tab=repairs")
	if err != nil {
		t.Fatalf("GET candidates: %v", err)
	}
	defer resp.Body.Close()

	var env struct {
		Data v2CandidatesResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Repairs) != 1 {
		t.Errorf("tab=repairs: want 1 repair, got %d", len(env.Data.Repairs))
	}
	if len(env.Data.Enrichments) != 0 {
		t.Errorf("tab=repairs: want 0 enrichments, got %d", len(env.Data.Enrichments))
	}
}

func TestHandleV2Candidates_tabEnrichmentsFilter(t *testing.T) {
	repoPath := t.TempDir()
	seedPendingCandidates(t, repoPath, []candidateRaw{
		{ID: "r1", Kind: "repair_edge", SubjectID: "A", Confidence: 0.8},
		{ID: "e1", Kind: "describe_entity", SubjectID: "B", Confidence: 0.8},
	})

	ts := newPendingServer(t, "grp4", "repo1", repoPath)
	resp, err := http.Get(ts.URL + "/api/v2/groups/grp4/candidates?tab=enrichments")
	if err != nil {
		t.Fatalf("GET candidates: %v", err)
	}
	defer resp.Body.Close()

	var env struct {
		Data v2CandidatesResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Repairs) != 0 {
		t.Errorf("tab=enrichments: want 0 repairs, got %d", len(env.Data.Repairs))
	}
	if len(env.Data.Enrichments) != 1 {
		t.Errorf("tab=enrichments: want 1 enrichment, got %d", len(env.Data.Enrichments))
	}
}

func TestHandleV2Candidates_emptyLists(t *testing.T) {
	// A repo with no candidates should return empty arrays (not null).
	repoPath := t.TempDir()
	// Do NOT seed any candidates file.

	ts := newPendingServer(t, "grp5", "repo1", repoPath)
	resp, err := http.Get(ts.URL + "/api/v2/groups/grp5/candidates")
	if err != nil {
		t.Fatalf("GET candidates: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode outer: %v", err)
	}
	var data v2CandidatesResponse
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Repairs == nil {
		t.Error("repairs should be [] not null")
	}
	if data.Enrichments == nil {
		t.Error("enrichments should be [] not null")
	}
}

func TestHandleV2Candidates_groupNotFound(t *testing.T) {
	repoPath := t.TempDir()
	ts := newPendingServer(t, "grp6", "repo1", repoPath)

	resp, err := http.Get(ts.URL + "/api/v2/groups/does-not-exist/candidates")
	if err != nil {
		t.Fatalf("GET candidates: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestHandleV2Candidates_communityNamingExcluded(t *testing.T) {
	// name_community candidates should appear in NEITHER tab.
	repoPath := t.TempDir()
	seedPendingCandidates(t, repoPath, []candidateRaw{
		{ID: "nc1", Kind: "name_community", SubjectID: "C1", Confidence: 0.9},
		{ID: "r1", Kind: "repair_edge", SubjectID: "X", Confidence: 0.9},
	})

	ts := newPendingServer(t, "grp7", "repo1", repoPath)
	resp, err := http.Get(ts.URL + "/api/v2/groups/grp7/candidates")
	if err != nil {
		t.Fatalf("GET candidates: %v", err)
	}
	defer resp.Body.Close()

	var env struct {
		Data v2CandidatesResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Only the repair_edge should appear; name_community is excluded.
	if len(env.Data.Repairs) != 1 {
		t.Errorf("want 1 repair, got %d", len(env.Data.Repairs))
	}
	if len(env.Data.Enrichments) != 0 {
		t.Errorf("want 0 enrichments (name_community excluded), got %d", len(env.Data.Enrichments))
	}
}

// ---------------------------------------------------------------------------
// PUT /api/v2/groups/{group}/candidates/{cid}/hint  (cid = entity ID, #1518)
// ---------------------------------------------------------------------------

// TestHandleV2CandidateHint_ok verifies that a hint is stored in the
// entity-hints store keyed by SubjectID (entity ID) and that a subsequent
// GET /candidates returns the hint attached to the candidate.
func TestHandleV2CandidateHint_ok(t *testing.T) {
	repoPath := t.TempDir()
	seedPendingCandidates(t, repoPath, []candidateRaw{
		// ID "c99" is the ephemeral candidate ID; "entity:X" is the stable entity ID.
		{ID: "c99", Kind: "repair_edge", SubjectID: "entity:X", Confidence: 0.9},
	})

	ts := newPendingServer(t, "grpH", "repo1", repoPath)
	body := bytes.NewBufferString(`{"hint":"check the migration guide"}`)
	// URL now uses entity ID, not candidate ID.
	req, _ := http.NewRequest("PUT",
		ts.URL+"/api/v2/groups/grpH/candidates/entity:X/hint", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT hint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var env struct {
		OK   bool              `json:"ok"`
		Data map[string]string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.OK {
		t.Fatal("want ok:true")
	}
	if env.Data["hint"] != "check the migration guide" {
		t.Errorf("want hint in response, got %v", env.Data)
	}
	if env.Data["entityId"] != "entity:X" {
		t.Errorf("want entityId in response, got %v", env.Data)
	}

	// Verify hint is in the entity-hints store, NOT mutated onto the candidate.
	hints := readEntityHints(repoPath)
	if hints["entity:X"] != "check the migration guide" {
		t.Errorf("hint not in entity-hints store; got %q", hints["entity:X"])
	}
	// The raw candidate's Hint field should NOT have been modified.
	raw := readAllCandidates(repoPath)
	if len(raw) == 0 {
		t.Fatal("no candidates on disk")
	}
	if raw[0].Hint != "" {
		t.Errorf("raw candidate Hint should be untouched; got %q", raw[0].Hint)
	}

	// GET /candidates should return the hint attached to the repair candidate.
	gresp, err := http.Get(ts.URL + "/api/v2/groups/grpH/candidates")
	if err != nil {
		t.Fatalf("GET candidates: %v", err)
	}
	defer gresp.Body.Close()
	var genv struct {
		Data v2CandidatesResponse `json:"data"`
	}
	if err := json.NewDecoder(gresp.Body).Decode(&genv); err != nil {
		t.Fatalf("decode candidates: %v", err)
	}
	if len(genv.Data.Repairs) != 1 {
		t.Fatalf("want 1 repair, got %d", len(genv.Data.Repairs))
	}
	if genv.Data.Repairs[0].Hint != "check the migration guide" {
		t.Errorf("hint not re-attached on GET; got %q", genv.Data.Repairs[0].Hint)
	}
	if genv.Data.Repairs[0].EntityID != "entity:X" {
		t.Errorf("want entityId entity:X, got %q", genv.Data.Repairs[0].EntityID)
	}
}

// TestHandleV2CandidateHint_survivesReindex verifies that the hint survives
// a simulated re-index that replaces the candidate ID while keeping the same
// entity ID (SubjectID).  This is the core regression tested by #1518.
func TestHandleV2CandidateHint_survivesReindex(t *testing.T) {
	repoPath := t.TempDir()
	// First index: candidate ID "old-cid", entity "entity:Y".
	seedPendingCandidates(t, repoPath, []candidateRaw{
		{ID: "old-cid", Kind: "repair_edge", SubjectID: "entity:Y", Confidence: 0.9},
	})

	ts := newPendingServer(t, "grpReindex", "repo1", repoPath)

	// Save hint by entity ID.
	body := bytes.NewBufferString(`{"hint":"important context"}`)
	req, _ := http.NewRequest("PUT",
		ts.URL+"/api/v2/groups/grpReindex/candidates/entity:Y/hint", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT hint: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT want 200, got %d", resp.StatusCode)
	}

	// Simulate re-index: same SubjectID but new candidate ID "new-cid".
	seedPendingCandidates(t, repoPath, []candidateRaw{
		{ID: "new-cid", Kind: "repair_edge", SubjectID: "entity:Y", Confidence: 0.9},
	})

	// GET candidates — hint should still be present.
	gresp, err := http.Get(ts.URL + "/api/v2/groups/grpReindex/candidates")
	if err != nil {
		t.Fatalf("GET candidates: %v", err)
	}
	defer gresp.Body.Close()
	var genv struct {
		Data v2CandidatesResponse `json:"data"`
	}
	if err := json.NewDecoder(gresp.Body).Decode(&genv); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(genv.Data.Repairs) != 1 {
		t.Fatalf("want 1 repair after re-index, got %d", len(genv.Data.Repairs))
	}
	rc := genv.Data.Repairs[0]
	if rc.ID != "new-cid" {
		t.Errorf("want new-cid after re-index, got %q", rc.ID)
	}
	if rc.Hint != "important context" {
		t.Errorf("hint lost after re-index; got %q", rc.Hint)
	}
}

// TestHandleV2CandidateHint_backwardCompatLegacyHintField verifies that
// candidates whose Hint field is still populated in enrichment-candidates.json
// (legacy format before #1518) still have their hint returned via GET.
func TestHandleV2CandidateHint_backwardCompatLegacyHintField(t *testing.T) {
	repoPath := t.TempDir()
	seedPendingCandidates(t, repoPath, []candidateRaw{
		// Legacy: hint baked into the candidate JSON (old format).
		{ID: "legacy-cid", Kind: "repair_edge", SubjectID: "entity:Z", Confidence: 0.9, Hint: "legacy hint"},
	})

	ts := newPendingServer(t, "grpLegacy", "repo1", repoPath)
	gresp, err := http.Get(ts.URL + "/api/v2/groups/grpLegacy/candidates")
	if err != nil {
		t.Fatalf("GET candidates: %v", err)
	}
	defer gresp.Body.Close()
	var genv struct {
		Data v2CandidatesResponse `json:"data"`
	}
	if err := json.NewDecoder(gresp.Body).Decode(&genv); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(genv.Data.Repairs) != 1 {
		t.Fatalf("want 1 repair, got %d", len(genv.Data.Repairs))
	}
	if genv.Data.Repairs[0].Hint != "legacy hint" {
		t.Errorf("legacy hint not returned; got %q", genv.Data.Repairs[0].Hint)
	}
}

func TestHandleV2CandidateHint_clearHint(t *testing.T) {
	repoPath := t.TempDir()
	seedPendingCandidates(t, repoPath, []candidateRaw{
		{ID: "c1", Kind: "repair_edge", SubjectID: "entity:ClearMe", Confidence: 0.9},
	})
	// Pre-seed a hint in the entity store.
	if err := os.WriteFile(entityHintsFile(repoPath), []byte(`{"entity:ClearMe":"old hint"}`), 0o644); err != nil {
		t.Fatalf("seed entity hints: %v", err)
	}

	ts := newPendingServer(t, "grpH2", "repo1", repoPath)
	body := bytes.NewBufferString(`{"hint":""}`)
	req, _ := http.NewRequest("PUT",
		ts.URL+"/api/v2/groups/grpH2/candidates/entity:ClearMe/hint", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT hint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	// Hint should be gone from the entity store.
	hints := readEntityHints(repoPath)
	if h, ok := hints["entity:ClearMe"]; ok {
		t.Errorf("hint should be removed; got %q", h)
	}
}

func TestHandleV2CandidateHint_notFound(t *testing.T) {
	repoPath := t.TempDir()
	seedPendingCandidates(t, repoPath, []candidateRaw{
		{ID: "c1", Kind: "repair_edge", SubjectID: "entity:Known", Confidence: 0.9},
	})

	ts := newPendingServer(t, "grpH3", "repo1", repoPath)
	body := bytes.NewBufferString(`{"hint":"irrelevant"}`)
	// Use an entity ID that doesn't exist in candidates.
	req, _ := http.NewRequest("PUT",
		ts.URL+"/api/v2/groups/grpH3/candidates/entity:NoSuchEntity/hint", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT hint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestHandleV2CandidateHint_badJSON(t *testing.T) {
	repoPath := t.TempDir()
	ts := newPendingServer(t, "grpH4", "repo1", repoPath)

	body := bytes.NewBufferString(`not json`)
	req, _ := http.NewRequest("PUT",
		ts.URL+"/api/v2/groups/grpH4/candidates/c1/hint", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT hint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}
