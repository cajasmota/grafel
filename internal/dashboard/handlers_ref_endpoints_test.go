// handlers_ref_endpoints_test.go — integration tests for the ?ref= endpoints
// added by issue #2220.
//
// Endpoints under test:
//
//	GET /api/groups/:g/refs
//	GET /api/groups/:g/stats?ref=
//	GET /api/groups/:g/repos/:r/entities?ref=
//	GET /api/groups/:g/repos/:r/relationships?ref=
//	GET /api/groups/:g/repos/:r/cross-repo-edges?ref=
//	GET /api/groups/:g/repos/:r/orphans?ref=
//	GET /api/groups/:g/repos/:r/patterns?ref=
package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// ── test fixture builder ─────────────────────────────────────────────────────

// refEndpointFixture builds a minimal on-disk layout for the ref-endpoint tests:
//
//   - One group with one repo.
//   - Two indexed refs: "main" and "feat/foo".
//   - Each ref has a graph.fb (empty sentinel) + graph-stats.json.
//
// Returns the httptest.Server and a cleanup function.
func buildRefEndpointFixture(t *testing.T) (ts *httptest.Server, groupName, repoSlug string) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	// Use daemon-root for deterministic store paths.
	storeRoot := filepath.Join(home, "store")
	t.Setenv("GRAFEL_DAEMON_ROOT", storeRoot)

	groupName = "testgroup"
	repoSlug = "testrepo"
	repoPath := filepath.Join(home, "myrepo")
	_ = os.MkdirAll(repoPath, 0o755)

	// Write group config + registry.
	cfg := &registry.GroupConfig{
		Name:  groupName,
		Repos: []registry.Repo{{Slug: repoSlug, Path: repoPath}},
	}
	cfgDir := filepath.Join(home, "groups")
	_ = os.MkdirAll(cfgDir, 0o755)
	cfgPath := filepath.Join(cfgDir, groupName+".fleet.json")
	raw, _ := json.Marshal(cfg)
	_ = os.WriteFile(cfgPath, raw, 0o644)

	regRaw, _ := json.Marshal(map[string]any{
		"version": 1,
		"groups":  []map[string]any{{"name": groupName, "config_path": cfgPath}},
	})
	_ = os.WriteFile(filepath.Join(home, "registry.json"), regRaw, 0o644)

	// Write graph.fb + graph-stats.json for each ref.
	// Also write a graph for the _unknown sentinel so that the "no ?ref="
	// default path (which reads the current HEAD via gitmeta and falls back
	// to _unknown when the temp dir has no git repo) finds a valid graph.
	for _, ref := range []string{"", "main", "feat/foo"} {
		stateDir := daemon.StateDirForRepoRef(repoPath, ref)
		_ = os.MkdirAll(stateDir, 0o755)

		// Write a minimal FlatBuffers graph file (just needs to exist on disk
		// for the refs endpoint; the graphstate loader handles empty files by
		// falling back gracefully). We write a real graph.json instead so
		// LoadGraphFromDir can parse it.
		doc := &graph.Document{
			Entities: []graph.Entity{
				{
					ID:   "e1",
					Name: "EntityOne",
					Kind: "Function",
				},
				{
					ID:   "e2",
					Name: "EntityTwo",
					Kind: "Function",
				},
			},
			Relationships: []graph.Relationship{
				{ID: "r1", FromID: "e1", ToID: "e2", Kind: "calls"},
			},
		}
		docBytes, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal doc: %v", err)
		}
		_ = os.WriteFile(filepath.Join(stateDir, "graph.json"), docBytes, 0o644)

		// Write graph-stats.json sidecar.
		stats := graph.GraphStatsSidecar{TotalEntities: 2}
		statsBytes, _ := json.Marshal(stats)
		_ = os.WriteFile(filepath.Join(stateDir, "graph-stats.json"), statsBytes, 0o644)
	}

	st := newFakeStore()
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Park the deferred Pass-4 sweep and release cache mmap readers before the
	// t.TempDir RemoveAll teardown so a background graph-algo.json write can't
	// leave refs/_unknown busy on Windows (v0.1.9 3-OS acceptance). See
	// quiesceGraphCache.
	quiesceGraphCache(t, srv)
	ts = httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts, groupName, repoSlug
}

// ── GET /api/groups/:g/refs ───────────────────────────────────────────────────

func TestHandleGroupRefs_OK(t *testing.T) {
	ts, group, _ := buildRefEndpointFixture(t)

	resp, err := http.Get(ts.URL + "/api/groups/" + group + "/refs")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		Group string `json:"group"`
		Refs  []struct {
			Name        string `json:"name"`
			IsCanonical bool   `json:"is_canonical"`
			EntityCount int    `json:"entity_count"`
			IsHot       bool   `json:"is_hot"`
		} `json:"refs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Group != group {
		t.Errorf("group: want %q, got %q", group, body.Group)
	}
	if len(body.Refs) < 1 {
		t.Errorf("want ≥1 ref, got %d", len(body.Refs))
	}
	// Check that "main" is listed.
	found := false
	for _, r := range body.Refs {
		if r.Name == "main" {
			found = true
			if !r.IsCanonical {
				t.Error("main should be canonical")
			}
		}
	}
	if !found {
		t.Error("main ref not found in response")
	}
}

func TestHandleGroupRefs_NotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	reg := map[string]any{"version": 1, "groups": []any{}}
	raw, _ := json.Marshal(reg)
	_ = os.WriteFile(filepath.Join(home, "registry.json"), raw, 0o644)

	st := newFakeStore()
	srv, _ := NewServer(DefaultConfig(), st)
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/groups/does-not-exist/refs")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// ── GET /api/groups/:g/stats ─────────────────────────────────────────────────

func TestHandleGroupStats_NoRef(t *testing.T) {
	ts, group, _ := buildRefEndpointFixture(t)

	resp, err := http.Get(ts.URL + "/api/groups/" + group + "/stats")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["group"] != group {
		t.Errorf("group field: want %q, got %v", group, body["group"])
	}
}

func TestHandleGroupStats_NamedRef(t *testing.T) {
	ts, group, _ := buildRefEndpointFixture(t)

	resp, err := http.Get(ts.URL + "/api/groups/" + group + "/stats?ref=main")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["ref"] != "main" {
		t.Errorf("ref: want main, got %v", body["ref"])
	}
}

func TestHandleGroupStats_AtAll(t *testing.T) {
	ts, group, _ := buildRefEndpointFixture(t)

	resp, err := http.Get(ts.URL + "/api/groups/" + group + "/stats?ref=@all")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["ref"] != "@all" {
		t.Errorf("ref: want @all, got %v", body["ref"])
	}
}

func TestHandleGroupStats_InvalidRef(t *testing.T) {
	ts, group, _ := buildRefEndpointFixture(t)

	resp, err := http.Get(ts.URL + "/api/groups/" + group + "/stats?ref=nonexistent-branch")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "invalid ref" {
		t.Errorf("error field: want 'invalid ref', got %v", body["error"])
	}
	if body["available"] == nil {
		t.Error("response should include 'available' refs list")
	}
}

// ── GET /api/groups/:g/repos/:r/entities ─────────────────────────────────────

func TestHandleRepoEntities_NoRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/entities", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["group"] != group {
		t.Errorf("group field mismatch")
	}
	if body["repo"] != repo {
		t.Errorf("repo field mismatch")
	}
}

func TestHandleRepoEntities_NamedRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/entities?ref=main", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["ref"] != "main" {
		t.Errorf("ref: want main, got %v", body["ref"])
	}
}

func TestHandleRepoEntities_AtAll(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/entities?ref=@all", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["ref"] != "@all" {
		t.Errorf("ref: want @all, got %v", body["ref"])
	}
}

func TestHandleRepoEntities_InvalidRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/entities?ref=missing-branch", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "invalid ref" {
		t.Errorf("error field: want 'invalid ref', got %v", body["error"])
	}
}

// ── GET /api/groups/:g/repos/:r/relationships ─────────────────────────────────

func TestHandleRepoRelationships_NoRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/relationships", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["relationships"] == nil {
		t.Error("missing relationships field")
	}
}

func TestHandleRepoRelationships_InvalidRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/relationships?ref=bad-ref", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

// ── GET /api/groups/:g/repos/:r/cross-repo-edges ─────────────────────────────

func TestHandleRepoCrossRepoEdges_NoRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/cross-repo-edges", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	// edges should be present (may be an empty array when no cross-repo links exist)
	if _, ok := body["count"]; !ok {
		t.Error("missing count field in response")
	}
}

func TestHandleRepoCrossRepoEdges_AtAll(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/cross-repo-edges?ref=@all", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["ref"] != "@all" {
		t.Errorf("ref: want @all, got %v", body["ref"])
	}
}

func TestHandleRepoCrossRepoEdges_InvalidRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/cross-repo-edges?ref=bad-ref", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

// ── GET /api/groups/:g/repos/:r/orphans ──────────────────────────────────────

func TestHandleRepoOrphans_NoRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/orphans", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["orphans"] == nil {
		t.Error("missing orphans field")
	}
}

func TestHandleRepoOrphans_NamedRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/orphans?ref=main", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["ref"] != "main" {
		t.Errorf("ref: want main, got %v", body["ref"])
	}
}

func TestHandleRepoOrphans_InvalidRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/orphans?ref=ghost-branch", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

// ── GET /api/groups/:g/repos/:r/patterns ─────────────────────────────────────

func TestHandleRepoPatterns_NoRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/patterns", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["patterns"] == nil {
		t.Error("missing patterns field")
	}
}

func TestHandleRepoPatterns_NamedRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/patterns?ref=main", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["ref"] != "main" {
		t.Errorf("ref: want main, got %v", body["ref"])
	}
}

func TestHandleRepoPatterns_InvalidRef(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	resp, err := http.Get(fmt.Sprintf("%s/api/groups/%s/repos/%s/patterns?ref=ghost", ts.URL, group, repo))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

// ── CORS: verify no regression ───────────────────────────────────────────────

func TestRefEndpoints_CORSNotRegressed(t *testing.T) {
	ts, group, repo := buildRefEndpointFixture(t)

	endpoints := []string{
		"/api/groups/" + group + "/refs",
		"/api/groups/" + group + "/stats",
		fmt.Sprintf("/api/groups/%s/repos/%s/entities", group, repo),
		fmt.Sprintf("/api/groups/%s/repos/%s/relationships", group, repo),
		fmt.Sprintf("/api/groups/%s/repos/%s/cross-repo-edges", group, repo),
		fmt.Sprintf("/api/groups/%s/repos/%s/orphans", group, repo),
		fmt.Sprintf("/api/groups/%s/repos/%s/patterns", group, repo),
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodOptions, ts.URL+ep, nil)
			req.Header.Set("Origin", "http://localhost:3000")
			req.Header.Set("Access-Control-Request-Method", "GET")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("OPTIONS: %v", err)
			}
			resp.Body.Close()
			// The server should handle OPTIONS without a 405.
			// 200 or 204 is fine; 405 indicates a CORS regression.
			if resp.StatusCode == http.StatusMethodNotAllowed {
				t.Errorf("CORS regression: OPTIONS %s returned 405", ep)
			}
		})
	}
}
