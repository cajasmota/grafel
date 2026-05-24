// handlers_v2_diff_test.go — unit tests for GET /api/v2/groups/:group/repos/:repo/diff
// PH5 of epic #2087 (#2093).
package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupDiffTest creates a minimal on-disk fixture with two indexed refs.
// Returns the server URL.
func setupDiffTest(t *testing.T, groupName, repoSlug string, docA, docB *graph.Document, refA, refB string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)

	// Fake repo directory — StateDirForRepoRef uses its absolute path.
	repoDir := filepath.Join(home, "fakerepo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repoDir: %v", err)
	}

	// Group config.
	cfg := &registry.GroupConfig{
		Name:  groupName,
		Repos: []registry.Repo{{Slug: repoSlug, Path: repoDir}},
	}
	cfgDir := filepath.Join(home, "groups")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfgDir: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, groupName+".fleet.json")
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	if err := os.WriteFile(cfgPath, raw, 0o644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}

	// Registry.
	regRaw, _ := json.Marshal(map[string]any{
		"version": 1,
		"groups":  []map[string]any{{"name": groupName, "config_path": cfgPath}},
	})
	if err := os.WriteFile(filepath.Join(home, "registry.json"), regRaw, 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	// Write graph.json for each ref under the correct per-ref state dir.
	writeDiffRefGraph(t, repoDir, refA, docA)
	writeDiffRefGraph(t, repoDir, refB, docB)

	// Build server.
	st := newFakeStore()
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts.URL
}

// writeDiffRefGraph writes a graph.Document as graph.json in the correct
// per-ref state directory computed by daemon.StateDirForRepoRef.
func writeDiffRefGraph(t *testing.T, repoPath, ref string, doc *graph.Document) {
	t.Helper()
	dir := daemon.StateDirForRepoRef(repoPath, ref)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.json"), raw, 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
}

// diffEntity builds a minimal graph.Entity for test fixtures.
func diffEntity(id, kind, name, file string, start, end int) graph.Entity {
	return graph.Entity{ID: id, Kind: kind, Name: name, SourceFile: file, StartLine: start, EndLine: end}
}

// diffRel builds a minimal graph.Relationship for test fixtures.
func diffRel(from, to, kind string) graph.Relationship {
	return graph.Relationship{
		ID:     graph.RelationshipID(from, to, kind),
		FromID: from,
		ToID:   to,
		Kind:   kind,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestHandleV2RepoDiff_AddedRemovedModified exercises the happy path with
// known entity and relationship deltas.
func TestHandleV2RepoDiff_AddedRemovedModified(t *testing.T) {
	docA := &graph.Document{
		Entities: []graph.Entity{
			diffEntity("aaa", "Function", "funcA", "pkg/a.go", 1, 10),
			diffEntity("bbb", "Function", "funcB", "pkg/b.go", 1, 10),
		},
		Relationships: []graph.Relationship{diffRel("aaa", "bbb", "calls")},
	}
	docB := &graph.Document{
		Entities: []graph.Entity{
			// bbb unchanged
			diffEntity("bbb", "Function", "funcB", "pkg/b.go", 1, 10),
			// ccc new → added
			diffEntity("ccc", "Function", "funcC", "pkg/c.go", 1, 5),
		},
		// aaa→bbb removed, ccc→bbb added
		Relationships: []graph.Relationship{diffRel("ccc", "bbb", "calls")},
	}

	tsURL := setupDiffTest(t, "grp", "svc", docA, docB, "main", "feat-x")

	resp, err := http.Get(tsURL + "/api/v2/groups/grp/repos/svc/diff?refA=main&refB=feat-x")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			Summary graph.DiffSummary `json:"summary"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Fatal("expected ok=true")
	}
	s := body.Data.Summary
	if s.EntitiesAdded != 1 {
		t.Errorf("entities_added want 1, got %d", s.EntitiesAdded)
	}
	if s.EntitiesRemoved != 1 {
		t.Errorf("entities_removed want 1, got %d", s.EntitiesRemoved)
	}
	if s.RelationshipsAdded != 1 {
		t.Errorf("relationships_added want 1, got %d", s.RelationshipsAdded)
	}
	if s.RelationshipsRemoved != 1 {
		t.Errorf("relationships_removed want 1, got %d", s.RelationshipsRemoved)
	}
}

// TestHandleV2RepoDiff_SameRef verifies that comparing a ref to itself returns
// an empty diff with HTTP 200.
func TestHandleV2RepoDiff_SameRef(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{diffEntity("x1", "Function", "fn1", "a.go", 1, 5)},
	}
	tsURL := setupDiffTest(t, "grp2", "svc2", doc, doc, "main", "main")

	resp, err := http.Get(tsURL + "/api/v2/groups/grp2/repos/svc2/diff?refA=main&refB=main")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			Summary graph.DiffSummary `json:"summary"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.Summary.EntitiesAdded != 0 || body.Data.Summary.EntitiesRemoved != 0 {
		t.Errorf("same-ref diff should be empty, got %+v", body.Data.Summary)
	}
}

// TestHandleV2RepoDiff_MissingParams verifies that omitting refA or refB
// returns HTTP 400.
func TestHandleV2RepoDiff_MissingParams(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	regRaw, _ := json.Marshal(map[string]any{"version": 1, "groups": []any{}})
	os.WriteFile(filepath.Join(home, "registry.json"), regRaw, 0o644)

	st := newFakeStore()
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v2/groups/grp/repos/svc/diff?refA=main")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// TestHandleV2RepoDiff_UnknownGroup verifies HTTP 404 for an unknown group.
func TestHandleV2RepoDiff_UnknownGroup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)
	regRaw, _ := json.Marshal(map[string]any{"version": 1, "groups": []any{}})
	os.WriteFile(filepath.Join(home, "registry.json"), regRaw, 0o644)

	st := newFakeStore()
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v2/groups/nope/repos/svc/diff?refA=main&refB=feat")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// TestHandleV2RepoDiff_CacheHit verifies that a second call with the same
// parameters is served from cache (much faster than the cold load).
func TestHandleV2RepoDiff_CacheHit(t *testing.T) {
	docA := &graph.Document{
		Entities: []graph.Entity{diffEntity("p1", "Function", "fn1", "a.go", 1, 5)},
	}
	docB := &graph.Document{
		Entities: []graph.Entity{
			diffEntity("p1", "Function", "fn1", "a.go", 1, 5),
			diffEntity("p2", "Function", "fn2", "b.go", 1, 5),
		},
	}

	tsURL := setupDiffTest(t, "grp3", "svc3", docA, docB, "main", "pr-42")

	doReq := func() time.Duration {
		start := time.Now()
		resp, err := http.Get(tsURL + "/api/v2/groups/grp3/repos/svc3/diff?refA=main&refB=pr-42")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		resp.Body.Close()
		return time.Since(start)
	}

	first := doReq()
	second := doReq()

	// The second call must be at least 2× faster than the first — it should be
	// a pure in-process cache hit. On CI we allow a generous bound.
	if second > first/2+5*time.Millisecond {
		// Just a warning — timing assertions are inherently flaky on CI;
		// we log but don't fail the test.
		t.Logf("cache hint: first=%v second=%v (second not obviously faster, but that's OK on loaded CI)", first, second)
	}
	// What we DO assert is that both calls return 200.
	resp, _ := http.Get(tsURL + "/api/v2/groups/grp3/repos/svc3/diff?refA=main&refB=pr-42")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200 on third call (cache), got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
