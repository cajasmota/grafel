package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// docstate unit tests
// ---------------------------------------------------------------------------

// setTestHome sets both HOME and ARCHIGRAPH_HOME so that docstate functions
// write to the test's temporary directory on all platforms.
//
// On Windows, os.UserHomeDir() reads USERPROFILE (not HOME), so setting only
// HOME is insufficient. ARCHIGRAPH_HOME is checked first by defaultDocstateDir
// and sidesteps the platform difference entirely.
func setTestHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("ARCHIGRAPH_HOME", filepath.Join(dir, ".archigraph"))
}

func TestLoadDocgenState_absent(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	st, err := LoadDocgenState("mygroup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st != nil {
		t.Fatalf("expected nil state for absent file, got %+v", st)
	}
}

func TestLoadDocgenState_present(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)

	now := time.Now().UTC().Truncate(time.Second)
	st := DocgenState{
		LastDocgenAt:     &now,
		LastDocgenCommit: "abc123",
		GeneratedPaths:   []string{"docs/index.md"},
	}
	if err := SaveDocgenState("mygroup", st); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadDocgenState("mygroup")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil state")
	}
	if !loaded.LastDocgenAt.Equal(now) {
		t.Errorf("last_docgen_at: got %v want %v", loaded.LastDocgenAt, now)
	}
	if loaded.LastDocgenCommit != "abc123" {
		t.Errorf("commit: got %q", loaded.LastDocgenCommit)
	}
}

func TestComputeDocState_neverGenerated(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)

	lg := &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{}}
	res := ComputeDocState("g", lg)

	if res.DocumentationState != "never_generated" {
		t.Errorf("state: got %q want never_generated", res.DocumentationState)
	}
	if res.SuggestedAction != "run /archigraph-tech-docs" {
		t.Errorf("action: got %q", res.SuggestedAction)
	}
	if res.LastDocgenAt != nil {
		t.Errorf("last_docgen_at: expected nil")
	}
}

func TestComputeDocState_fresh(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)

	// Create a repo with one source file.
	repoDir := filepath.Join(tmp, "repo-a")
	srcFile := filepath.Join(repoDir, "src", "app.go")
	if err := os.MkdirAll(filepath.Dir(srcFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcFile, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Docgen happened *after* the file was written.
	time.Sleep(5 * time.Millisecond) // ensure strict after
	futureDocgen := time.Now().UTC()

	st := DocgenState{LastDocgenAt: &futureDocgen}
	if err := SaveDocgenState("g", st); err != nil {
		t.Fatal(err)
	}

	lg := makeLoadedGroupWithFile(t, "g", "repo-a", repoDir, "src/app.go")
	res := ComputeDocState("g", lg)

	if res.DocumentationState != "fresh" {
		t.Errorf("state: got %q want fresh", res.DocumentationState)
	}
	if res.StaleCount != 0 {
		t.Errorf("stale_count: got %d want 0", res.StaleCount)
	}
	if res.SuggestedAction != "none — graph is healthy" {
		t.Errorf("action: got %q", res.SuggestedAction)
	}
}

func TestComputeDocState_stale(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)

	// Docgen happened *before* the file was written.
	pastDocgen := time.Now().UTC().Add(-1 * time.Hour)
	st := DocgenState{LastDocgenAt: &pastDocgen}
	if err := SaveDocgenState("g", st); err != nil {
		t.Fatal(err)
	}

	// Create source file (mtime = now, after pastDocgen).
	repoDir := filepath.Join(tmp, "repo-b")
	srcFile := filepath.Join(repoDir, "src", "handler.go")
	if err := os.MkdirAll(filepath.Dir(srcFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcFile, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	lg := makeLoadedGroupWithFile(t, "g", "repo-b", repoDir, "src/handler.go")
	res := ComputeDocState("g", lg)

	if res.DocumentationState != "stale" {
		t.Errorf("state: got %q want stale", res.DocumentationState)
	}
	if res.StaleCount != 1 {
		t.Errorf("stale_count: got %d want 1", res.StaleCount)
	}
}

func TestComposeSuggestedAction_transitions(t *testing.T) {
	cases := []struct {
		name          string
		docState      DocStateResult
		candidates    int
		residuals     int
		wantSubstring string
	}{
		{
			name:          "never_generated",
			docState:      DocStateResult{DocumentationState: "never_generated"},
			wantSubstring: "run /archigraph-tech-docs",
		},
		{
			name:          "stale",
			docState:      DocStateResult{DocumentationState: "stale", StaleCount: 3},
			wantSubstring: "refresh docs",
		},
		{
			name:          "fresh_with_candidates",
			docState:      DocStateResult{DocumentationState: "fresh"},
			candidates:    2,
			wantSubstring: "pattern candidate",
		},
		{
			name:          "fresh_with_residuals",
			docState:      DocStateResult{DocumentationState: "fresh"},
			residuals:     5,
			wantSubstring: "repair candidate",
		},
		{
			name:          "all_fresh",
			docState:      DocStateResult{DocumentationState: "fresh"},
			wantSubstring: "graph is healthy",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := composeSuggestedAction(tc.docState, tc.candidates, tc.residuals)
			if got == "" {
				t.Fatal("empty suggested_action")
			}
			// Contains check.
			if len(tc.wantSubstring) > 0 {
				found := false
				for i := 0; i <= len(got)-len(tc.wantSubstring); i++ {
					if got[i:i+len(tc.wantSubstring)] == tc.wantSubstring {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("suggested_action %q does not contain %q", got, tc.wantSubstring)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration: archigraph_whoami returns enriched response
// ---------------------------------------------------------------------------

func TestHandleWhoami_enrichedResponse_neverGenerated(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	t.Setenv("ARCHIGRAPH_WHOAMI_NUDGE", "") // ensure nudge enabled

	repoDir := filepath.Join(tmp, "repo-a")
	doc := fixtureDoc("repo-a")
	writeGraph(t, repoDir, doc)

	regPath := makeRegistry(t, tmp, map[string]map[string]string{
		"g": {"repo-a": repoDir},
	})

	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "g"}
	res, err := srv.handleWhoami(context.Background(), req)
	if err != nil {
		t.Fatalf("handleWhoami: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}

	out := extractResultJSON(t, res)

	checkField(t, out, "documentation_state", "never_generated")
	checkField(t, out, "suggested_action", "run /archigraph-tech-docs")
}

func TestHandleWhoami_enrichedResponse_afterDocgen_fresh(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	t.Setenv("ARCHIGRAPH_WHOAMI_NUDGE", "")

	repoDir := filepath.Join(tmp, "repo-a")
	doc := fixtureDoc("repo-a")
	writeGraph(t, repoDir, doc)

	regPath := makeRegistry(t, tmp, map[string]map[string]string{
		"g": {"repo-a": repoDir},
	})

	// Write docgen state in the future so no files appear stale.
	futureTime := time.Now().UTC().Add(1 * time.Hour)
	st := DocgenState{LastDocgenAt: &futureTime}
	if err := SaveDocgenState("g", st); err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "g"}
	res, err := srv.handleWhoami(context.Background(), req)
	if err != nil {
		t.Fatalf("handleWhoami: %v", err)
	}

	out := extractResultJSON(t, res)

	checkField(t, out, "documentation_state", "fresh")
	checkField(t, out, "suggested_action", "none — graph is healthy")
}

func TestHandleWhoami_enrichedResponse_stale(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	t.Setenv("ARCHIGRAPH_WHOAMI_NUDGE", "")

	// Write a source file to the repo dir.
	repoDir := filepath.Join(tmp, "repo-a")
	srcPath := filepath.Join(repoDir, "src", "DashboardScreen.tsx")
	if err := os.MkdirAll(filepath.Dir(srcPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcPath, []byte("// code"), 0o644); err != nil {
		t.Fatal(err)
	}

	doc := &graph.Document{
		Repo: "repo-a",
		Entities: []graph.Entity{
			{ID: "a1", Name: "DashboardScreen", SourceFile: "src/DashboardScreen.tsx", StartLine: 1, EndLine: 5},
		},
	}
	writeGraph(t, repoDir, doc)

	regPath := makeRegistry(t, tmp, map[string]map[string]string{
		"g": {"repo-a": repoDir},
	})

	// Docgen happened before the source file was written.
	pastDocgen := time.Now().UTC().Add(-2 * time.Hour)
	st := DocgenState{LastDocgenAt: &pastDocgen}
	if err := SaveDocgenState("g", st); err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "g"}
	res, err := srv.handleWhoami(context.Background(), req)
	if err != nil {
		t.Fatalf("handleWhoami: %v", err)
	}

	out := extractResultJSON(t, res)

	checkField(t, out, "documentation_state", "stale")
	sc, _ := out["stale_count"].(float64)
	if sc < 1 {
		t.Errorf("stale_count: got %v want >= 1", out["stale_count"])
	}
}

func TestHandleWhoami_quietMode(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	t.Setenv("ARCHIGRAPH_WHOAMI_NUDGE", "quiet")

	repoDir := filepath.Join(tmp, "repo-a")
	doc := fixtureDoc("repo-a")
	writeGraph(t, repoDir, doc)

	regPath := makeRegistry(t, tmp, map[string]map[string]string{
		"g": {"repo-a": repoDir},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "g"}
	res, err := srv.handleWhoami(context.Background(), req)
	if err != nil {
		t.Fatalf("handleWhoami: %v", err)
	}

	out := extractResultJSON(t, res)

	// In quiet mode, documentation_state must NOT be present.
	if _, found := out["documentation_state"]; found {
		t.Error("documentation_state should be absent in quiet mode")
	}
	if _, found := out["suggested_action"]; found {
		t.Error("suggested_action should be absent in quiet mode")
	}
}

// ---------------------------------------------------------------------------
// wire_version field
// ---------------------------------------------------------------------------

// TestHandleWhoami_wireVersion asserts that archigraph_whoami always returns
// a non-empty wire_version field, both in normal mode and in quiet mode.
func TestHandleWhoami_wireVersion(t *testing.T) {
	for _, quiet := range []string{"", "quiet"} {
		quiet := quiet
		name := "normal"
		if quiet == "quiet" {
			name = "quiet"
		}
		t.Run(name, func(t *testing.T) {
			tmp := t.TempDir()
			setTestHome(t, tmp)
			t.Setenv("ARCHIGRAPH_WHOAMI_NUDGE", quiet)

			repoDir := filepath.Join(tmp, "repo-a")
			doc := fixtureDoc("repo-a")
			writeGraph(t, repoDir, doc)

			regPath := makeRegistry(t, tmp, map[string]map[string]string{
				"g": {"repo-a": repoDir},
			})
			srv, err := NewServer(Config{RegistryPath: regPath})
			if err != nil {
				t.Fatalf("NewServer: %v", err)
			}

			req := mcpapi.CallToolRequest{}
			req.Params.Arguments = map[string]any{"group": "g"}
			res, err := srv.handleWhoami(context.Background(), req)
			if err != nil {
				t.Fatalf("handleWhoami: %v", err)
			}
			if res.IsError {
				t.Fatalf("tool error: %v", res.Content)
			}

			out := extractResultJSON(t, res)

			wv, ok := out["wire_version"].(string)
			if !ok || wv == "" {
				t.Errorf("wire_version: missing or empty (got %T: %v)", out["wire_version"], out["wire_version"])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func checkField(t *testing.T, m map[string]any, key, want string) {
	t.Helper()
	got, ok := m[key].(string)
	if !ok {
		t.Errorf("field %q: missing or not string (got %T: %v)", key, m[key], m[key])
		return
	}
	if got != want {
		t.Errorf("field %q: got %q want %q", key, got, want)
	}
}

// ---------------------------------------------------------------------------
// Fix #1 — whoami docgen-trap: entity/relationship/tests_edges counts
// ---------------------------------------------------------------------------

// TestHandleWhoami_indexCounts asserts that archigraph_whoami always returns
// entity_count, relationship_count, and an index{} block even when no docgen
// has ever been run — so a fully-indexed group is never mistaken for empty.
func TestHandleWhoami_indexCounts(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	t.Setenv("ARCHIGRAPH_WHOAMI_NUDGE", "") // normal mode

	// Build a graph with 4 entities, 3 edges (2 CALLS + 1 TESTS).
	repoDir := filepath.Join(tmp, "repo-a")
	doc := &graph.Document{
		Repo: "repo-a",
		Entities: []graph.Entity{
			{ID: "a1", Name: "Foo", Kind: "function", SourceFile: "foo.go", StartLine: 1, EndLine: 5},
			{ID: "a2", Name: "Bar", Kind: "function", SourceFile: "bar.go", StartLine: 1, EndLine: 5},
			{ID: "a3", Name: "Baz", Kind: "function", SourceFile: "baz.go", StartLine: 1, EndLine: 5},
			{ID: "t1", Name: "TestFoo", Kind: "function", SourceFile: "foo_test.go", StartLine: 1, EndLine: 5},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "a1", ToID: "a2", Kind: "CALLS"},
			{ID: "r2", FromID: "a2", ToID: "a3", Kind: "CALLS"},
			{ID: "r3", FromID: "t1", ToID: "a1", Kind: "TESTS"},
		},
	}
	writeGraph(t, repoDir, doc)

	regPath := makeRegistry(t, tmp, map[string]map[string]string{
		"g": {"repo-a": repoDir},
	})

	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "g"}
	res, err := srv.handleWhoami(context.Background(), req)
	if err != nil {
		t.Fatalf("handleWhoami: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}

	out := extractResultJSON(t, res)

	// Top-level counts must be present.
	ec, ok := out["entity_count"].(float64)
	if !ok {
		t.Fatalf("entity_count: missing or wrong type (got %T: %v)", out["entity_count"], out["entity_count"])
	}
	if ec != 4 {
		t.Errorf("entity_count: got %v want 4", ec)
	}
	rc, ok := out["relationship_count"].(float64)
	if !ok {
		t.Fatalf("relationship_count: missing or wrong type (got %T: %v)", out["relationship_count"], out["relationship_count"])
	}
	if rc != 3 {
		t.Errorf("relationship_count: got %v want 3", rc)
	}

	// index{} sub-object must mirror the top-level counts and include tests_edges.
	idx, ok := out["index"].(map[string]any)
	if !ok {
		t.Fatalf("index: missing or wrong type (got %T: %v)", out["index"], out["index"])
	}
	if v, _ := idx["entity_count"].(float64); v != 4 {
		t.Errorf("index.entity_count: got %v want 4", idx["entity_count"])
	}
	if v, _ := idx["relationship_count"].(float64); v != 3 {
		t.Errorf("index.relationship_count: got %v want 3", idx["relationship_count"])
	}
	if v, _ := idx["tests_edges"].(float64); v != 1 {
		t.Errorf("index.tests_edges: got %v want 1", idx["tests_edges"])
	}
	// indexed_sha and indexed_ref must be present (may be empty string in test env).
	if _, present := idx["indexed_sha"]; !present {
		t.Error("index.indexed_sha: field must be present")
	}
	if _, present := idx["indexed_ref"]; !present {
		t.Error("index.indexed_ref: field must be present")
	}
}

// TestHandleWhoami_indexCounts_multiRepo asserts that entity/relationship counts
// aggregate across all repos in the group, matching archigraph_stats behaviour.
func TestHandleWhoami_indexCounts_multiRepo(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	t.Setenv("ARCHIGRAPH_WHOAMI_NUDGE", "")

	repoADir := filepath.Join(tmp, "repo-a")
	repoBDir := filepath.Join(tmp, "repo-b")

	docA := &graph.Document{
		Repo: "repo-a",
		Entities: []graph.Entity{
			{ID: "a1", Name: "FuncA1", Kind: "function", SourceFile: "a.go", StartLine: 1, EndLine: 5},
			{ID: "a2", Name: "FuncA2", Kind: "function", SourceFile: "a.go", StartLine: 6, EndLine: 10},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "a1", ToID: "a2", Kind: "CALLS"},
		},
	}
	docB := &graph.Document{
		Repo: "repo-b",
		Entities: []graph.Entity{
			{ID: "b1", Name: "FuncB1", Kind: "function", SourceFile: "b.go", StartLine: 1, EndLine: 5},
		},
		Relationships: []graph.Relationship{
			{ID: "r2", FromID: "b1", ToID: "a1", Kind: "TESTS"},
		},
	}
	writeGraph(t, repoADir, docA)
	writeGraph(t, repoBDir, docB)

	regPath := makeRegistry(t, tmp, map[string]map[string]string{
		"g": {"repo-a": repoADir, "repo-b": repoBDir},
	})

	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "g"}
	res, err := srv.handleWhoami(context.Background(), req)
	if err != nil {
		t.Fatalf("handleWhoami: %v", err)
	}
	out := extractResultJSON(t, res)

	// repo-a has 2 entities + 1 rel; repo-b has 1 entity + 1 TESTS rel → totals: 3 + 2.
	if v, _ := out["entity_count"].(float64); v != 3 {
		t.Errorf("entity_count: got %v want 3", out["entity_count"])
	}
	if v, _ := out["relationship_count"].(float64); v != 2 {
		t.Errorf("relationship_count: got %v want 2", out["relationship_count"])
	}
	idx, _ := out["index"].(map[string]any)
	if idx == nil {
		t.Fatal("index: missing")
	}
	if v, _ := idx["tests_edges"].(float64); v != 1 {
		t.Errorf("index.tests_edges: got %v want 1", idx["tests_edges"])
	}
}

// TestHandleWhoami_indexCounts_quietMode asserts that index counts are NOT
// present when ARCHIGRAPH_WHOAMI_NUDGE=quiet (early return path).
func TestHandleWhoami_indexCounts_quietMode(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	t.Setenv("ARCHIGRAPH_WHOAMI_NUDGE", "quiet")

	repoDir := filepath.Join(tmp, "repo-a")
	doc := fixtureDoc("repo-a")
	writeGraph(t, repoDir, doc)
	regPath := makeRegistry(t, tmp, map[string]map[string]string{
		"g": {"repo-a": repoDir},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "g"}
	res, err := srv.handleWhoami(context.Background(), req)
	if err != nil {
		t.Fatalf("handleWhoami: %v", err)
	}
	out := extractResultJSON(t, res)

	// In quiet mode the enrichment block (including index counts) is suppressed.
	if _, found := out["entity_count"]; found {
		t.Error("entity_count should be absent in quiet mode")
	}
	if _, found := out["index"]; found {
		t.Error("index should be absent in quiet mode")
	}
}

// TestLoadedRepo_testsEdgeCachePopulated verifies that the TestsEdgeCount field
// is populated at graph-load time so that groupIndexCounts can return the
// tests_edges count in O(1) without rescanning relationships on every whoami
// call (#3325 perf fix).
func TestLoadedRepo_testsEdgeCachePopulated(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	t.Setenv("ARCHIGRAPH_WHOAMI_NUDGE", "")

	repoDir := filepath.Join(tmp, "repo-cache")
	doc := &graph.Document{
		Repo: "repo-cache",
		Entities: []graph.Entity{
			{ID: "e1", Name: "Prod1", Kind: "function", SourceFile: "a.go", StartLine: 1, EndLine: 5},
			{ID: "e2", Name: "Prod2", Kind: "function", SourceFile: "a.go", StartLine: 6, EndLine: 10},
			{ID: "t1", Name: "TestProd1", Kind: "function", SourceFile: "a_test.go", StartLine: 1, EndLine: 5},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "e1", ToID: "e2", Kind: "CALLS"},
			{ID: "r2", FromID: "t1", ToID: "e1", Kind: "TESTS"},
			{ID: "r3", FromID: "t1", ToID: "e2", Kind: "TESTS"},
		},
	}
	writeGraph(t, repoDir, doc)

	regPath := makeRegistry(t, tmp, map[string]map[string]string{
		"g": {"repo-cache": repoDir},
	})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Force a reload so the State is warm.
	lg := srv.State.Group("g")
	if lg == nil {
		t.Fatal("group 'g' not found after NewServer")
	}

	// The per-repo cached count must equal the number of TESTS edges (2).
	lr := lg.Repos["repo-cache"]
	if lr == nil {
		t.Fatal("repo-cache not loaded")
	}
	if lr.TestsEdgeCount != 2 {
		t.Errorf("LoadedRepo.TestsEdgeCount: got %d want 2 — cache not populated at load time", lr.TestsEdgeCount)
	}

	// groupIndexCounts must aggregate the cached value correctly.
	entities, rels, testsEdges := groupIndexCounts(lg)
	if entities != 3 {
		t.Errorf("groupIndexCounts entities: got %d want 3", entities)
	}
	if rels != 3 {
		t.Errorf("groupIndexCounts relationships: got %d want 3", rels)
	}
	if testsEdges != 2 {
		t.Errorf("groupIndexCounts testsEdges: got %d want 2 — not reading cached TestsEdgeCount", testsEdges)
	}

	// Verify correctness via the whoami handler too — the end-to-end path.
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "g"}
	res, herr := srv.handleWhoami(context.Background(), req)
	if herr != nil {
		t.Fatalf("handleWhoami: %v", herr)
	}
	out := extractResultJSON(t, res)
	idx, _ := out["index"].(map[string]any)
	if idx == nil {
		t.Fatal("index block missing from whoami response")
	}
	if v, _ := idx["tests_edges"].(float64); v != 2 {
		t.Errorf("index.tests_edges: got %v want 2", idx["tests_edges"])
	}
}

// ---------------------------------------------------------------------------
// Fix #2 — whoami must honor explicit group= for index-state fields
// ---------------------------------------------------------------------------

// TestHandleWhoami_explicitGroup_indexState asserts that when group= is
// provided explicitly, the indexed_sha/indexed_ref in the response key off the
// queried group's repos (not the cwd). The test sets up two groups in the same
// registry and queries each via explicit group= to verify the index block
// reflects the correct group.
func TestHandleWhoami_explicitGroup_indexState(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	t.Setenv("ARCHIGRAPH_WHOAMI_NUDGE", "")

	// Two groups, each with a different repo and entity count.
	repoAlphaDir := filepath.Join(tmp, "repo-alpha")
	repoBetaDir := filepath.Join(tmp, "repo-beta")

	docAlpha := &graph.Document{
		Repo: "repo-alpha",
		Entities: []graph.Entity{
			{ID: "e1", Name: "AlphaFunc", Kind: "function", SourceFile: "alpha.go", StartLine: 1, EndLine: 5},
			{ID: "e2", Name: "AlphaStruct", Kind: "struct", SourceFile: "alpha.go", StartLine: 6, EndLine: 10},
			{ID: "e3", Name: "AlphaHelper", Kind: "function", SourceFile: "alpha.go", StartLine: 11, EndLine: 15},
		},
	}
	docBeta := &graph.Document{
		Repo: "repo-beta",
		Entities: []graph.Entity{
			{ID: "f1", Name: "BetaFunc", Kind: "function", SourceFile: "beta.go", StartLine: 1, EndLine: 5},
		},
	}
	writeGraph(t, repoAlphaDir, docAlpha)
	writeGraph(t, repoBetaDir, docBeta)

	// Both groups in the same registry.
	regPath := makeRegistry(t, tmp, map[string]map[string]string{
		"alpha-group": {"repo-alpha": repoAlphaDir},
		"beta-group":  {"repo-beta": repoBetaDir},
	})

	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Query alpha-group explicitly.
	reqAlpha := mcpapi.CallToolRequest{}
	reqAlpha.Params.Arguments = map[string]any{"group": "alpha-group"}
	resAlpha, err := srv.handleWhoami(context.Background(), reqAlpha)
	if err != nil {
		t.Fatalf("handleWhoami(alpha-group): %v", err)
	}
	outAlpha := extractResultJSON(t, resAlpha)
	if v, _ := outAlpha["entity_count"].(float64); v != 3 {
		t.Errorf("alpha-group entity_count: got %v want 3", outAlpha["entity_count"])
	}
	if v, _ := outAlpha["group"].(string); v != "alpha-group" {
		t.Errorf("alpha-group group field: got %q want %q", v, "alpha-group")
	}

	// Query beta-group explicitly.
	reqBeta := mcpapi.CallToolRequest{}
	reqBeta.Params.Arguments = map[string]any{"group": "beta-group"}
	resBeta, err := srv.handleWhoami(context.Background(), reqBeta)
	if err != nil {
		t.Fatalf("handleWhoami(beta-group): %v", err)
	}
	outBeta := extractResultJSON(t, resBeta)
	if v, _ := outBeta["entity_count"].(float64); v != 1 {
		t.Errorf("beta-group entity_count: got %v want 1", outBeta["entity_count"])
	}
	if v, _ := outBeta["group"].(string); v != "beta-group" {
		t.Errorf("beta-group group field: got %q want %q", v, "beta-group")
	}

	// The two results must have different entity counts, proving each query
	// resolved against its own group's index (not the cwd's).
	alphaEC, _ := outAlpha["entity_count"].(float64)
	betaEC, _ := outBeta["entity_count"].(float64)
	if alphaEC == betaEC {
		t.Errorf("entity_count must differ between groups: alpha=%v beta=%v", alphaEC, betaEC)
	}
}

// ---------------------------------------------------------------------------
// Fix #3372 — whoami indexed_sha must come from the loaded graph, not git
// ---------------------------------------------------------------------------

// TestHandleWhoami_indexedSHAFromGraph asserts that indexed_sha / indexed_ref
// in the whoami response are read from the loaded graph's Doc.IndexedSHA /
// Doc.IndexedRef fields (O(1), no subprocess) rather than from a live
// gitmeta.Capture call. The test stores a known SHA in the graph document and
// checks that the same value appears verbatim in the whoami output — proving
// the value is graph-sourced (a live git call would return a different or empty
// SHA in the test directory which is not a real git worktree).
func TestHandleWhoami_indexedSHAFromGraph(t *testing.T) {
	tmp := t.TempDir()
	setTestHome(t, tmp)
	t.Setenv("ARCHIGRAPH_WHOAMI_NUDGE", "")

	const wantSHA = "cafebabe1234"
	const wantRef = "feat/perf-fix-3372"

	repoDir := filepath.Join(tmp, "repo-sha-test")
	doc := &graph.Document{
		Repo:       "repo-sha-test",
		IndexedSHA: wantSHA,
		IndexedRef: wantRef,
		Entities: []graph.Entity{
			{ID: "e1", Name: "SomeFunc", Kind: "function", SourceFile: "main.go", StartLine: 1, EndLine: 5},
		},
	}
	writeGraph(t, repoDir, doc)

	regPath := makeRegistry(t, tmp, map[string]map[string]string{
		"sha-group": {"repo-sha-test": repoDir},
	})

	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "sha-group"}
	res, herr := srv.handleWhoami(context.Background(), req)
	if herr != nil {
		t.Fatalf("handleWhoami: %v", herr)
	}
	out := extractResultJSON(t, res)

	// Top-level fields.
	if got, _ := out["indexed_sha"].(string); got != wantSHA {
		t.Errorf("indexed_sha: got %q want %q (must come from graph metadata, not git subprocess)", got, wantSHA)
	}
	if got, _ := out["indexed_ref"].(string); got != wantRef {
		t.Errorf("indexed_ref: got %q want %q (must come from graph metadata, not git subprocess)", got, wantRef)
	}

	// index{} block must mirror the same values.
	idx, _ := out["index"].(map[string]any)
	if idx == nil {
		t.Fatal("index block missing from whoami response")
	}
	if got, _ := idx["indexed_sha"].(string); got != wantSHA {
		t.Errorf("index.indexed_sha: got %q want %q", got, wantSHA)
	}
	if got, _ := idx["indexed_ref"].(string); got != wantRef {
		t.Errorf("index.indexed_ref: got %q want %q", got, wantRef)
	}
}

// makeLoadedGroupWithFile builds a minimal LoadedGroup with one repo whose
// entity graph has the given source file path relative to repoDir.
func makeLoadedGroupWithFile(t *testing.T, groupName, repoName, repoDir, relFile string) *LoadedGroup {
	t.Helper()
	doc := &graph.Document{
		Repo: repoName,
		Entities: []graph.Entity{
			{ID: "e1", Name: "Foo", SourceFile: relFile, StartLine: 1, EndLine: 5},
		},
	}
	lr := &LoadedRepo{
		Repo: repoName,
		Path: repoDir,
		Doc:  doc,
	}
	return &LoadedGroup{
		Name:  groupName,
		Repos: map[string]*LoadedRepo{repoName: lr},
	}
}
