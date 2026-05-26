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
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	lr := &LoadedRepo{
		Repo:      repoName,
		Path:      repoDir,
		Doc:       doc,
		ByID:      byID,
		Adjacency: buildAdjacency(doc, repoName),
		CallsAdj:  buildCallsAdjacency(doc),
	}
	return &LoadedGroup{
		Name:  groupName,
		Repos: map[string]*LoadedRepo{repoName: lr},
	}
}
