package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// processFixtureDoc builds a graph with two pre-computed Process entities
// plus their STEP_IN_PROCESS / ENTRY_POINT_OF edges — mirroring what
// engine.RunProcessFlow emits at index time.
func processFixtureDoc(repo string) *graph.Document {
	doc := &graph.Document{
		Version:     1,
		GeneratedAt: time.Now(),
		Repo:        repo,
		Entities: []graph.Entity{
			{ID: "f1", Name: "handleSubmit", Kind: "SCOPE.Function", SourceFile: "src/form.ts", StartLine: 10, EndLine: 30, Language: "ts"},
			{ID: "f2", Name: "validateForm", Kind: "SCOPE.Function", SourceFile: "src/form.ts", StartLine: 40, EndLine: 55, Language: "ts"},
			{ID: "f3", Name: "submitOrder", Kind: "SCOPE.Function", SourceFile: "src/api.ts", StartLine: 5, EndLine: 25, Language: "ts"},
			{ID: "ep", Name: "http:POST:/api/orders", Kind: "http_endpoint", SourceFile: "src/api.ts", StartLine: 30, EndLine: 60, Language: "ts"},
			// Process entity 1: ordinary 3-step chain.
			{ID: "p1", Name: "handleSubmit → validateForm", Kind: "SCOPE.Process", SourceFile: "src/form.ts", StartLine: 10, EndLine: 30, Language: "ts",
				Properties: map[string]string{
					"entry_id": "f1", "entry_name": "handleSubmit",
					"terminal_id": "f2", "step_count": "3",
					"cross_stack":  "false",
					"chain":        "f1,f3,f2",
					"chain_labels": "handleSubmit → submitOrder → validateForm",
				}},
			// Process entity 2: cross-stack (traverses http_endpoint).
			{ID: "p2", Name: "handleSubmit → http:POST:/api/orders", Kind: "SCOPE.Process", SourceFile: "src/form.ts", StartLine: 10, EndLine: 30, Language: "ts",
				Properties: map[string]string{
					"entry_id": "f1", "entry_name": "handleSubmit",
					"terminal_id": "ep", "step_count": "3",
					"cross_stack":  "true",
					"chain":        "f1,f3,ep",
					"chain_labels": "handleSubmit → submitOrder → http:POST:/api/orders",
				}},
		},
		Relationships: []graph.Relationship{
			{ID: "c1", FromID: "f1", ToID: "f3", Kind: "CALLS"},
			{ID: "c2", FromID: "f3", ToID: "f2", Kind: "CALLS"},
			{ID: "c3", FromID: "f3", ToID: "ep", Kind: "CALLS"},
			// STEP_IN_PROCESS for p1.
			{ID: "s1", FromID: "p1", ToID: "f1", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "0"}},
			{ID: "s2", FromID: "p1", ToID: "f3", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "1"}},
			{ID: "s3", FromID: "p1", ToID: "f2", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "2"}},
			// STEP_IN_PROCESS for p2.
			{ID: "s4", FromID: "p2", ToID: "f1", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "0"}},
			{ID: "s5", FromID: "p2", ToID: "f3", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "1"}},
			{ID: "s6", FromID: "p2", ToID: "ep", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "2"}},
			// ENTRY_POINT_OF for both.
			{ID: "e1", FromID: "f1", ToID: "p1", Kind: "ENTRY_POINT_OF"},
			{ID: "e2", FromID: "f1", ToID: "p2", Kind: "ENTRY_POINT_OF"},
		},
	}
	return doc
}

func setupTracesServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGraph(t, repo, processFixtureDoc("r1"))
	reg := Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{"r1": {Path: repo}}},
	}}
	regPath := filepath.Join(dir, "registry.json")
	d, _ := json.MarshalIndent(reg, "", "  ")
	_ = os.WriteFile(regPath, d, 0o644)
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestTraces_ListReturnsAllProcesses(t *testing.T) {
	srv := setupTracesServer(t)
	// min_steps=0 disables the short-flow filter (#1639) — these fixtures
	// have 3-step chains and the test asserts list completeness, not filtering.
	res := callTool(t, srv, "archigraph_traces", map[string]any{"action": "list", "min_steps": 0})
	txt := resultText(res)
	if !strings.Contains(txt, "\"count\": 2") {
		t.Errorf("expected count=2, got: %s", txt)
	}
	if !strings.Contains(txt, "handleSubmit") {
		t.Errorf("expected handleSubmit in list, got: %s", txt)
	}
}

func TestTraces_ListCrossStackOnly(t *testing.T) {
	srv := setupTracesServer(t)
	res := callTool(t, srv, "archigraph_traces", map[string]any{
		"action":           "list",
		"cross_stack_only": true,
		"min_steps":        0,
	})
	txt := resultText(res)
	if !strings.Contains(txt, "\"count\": 1") {
		t.Errorf("expected count=1 cross-stack process, got: %s", txt)
	}
	if !strings.Contains(txt, "/api/orders") {
		t.Errorf("expected http endpoint in cross-stack process, got: %s", txt)
	}
}

func TestTraces_GetReturnsFullChain(t *testing.T) {
	srv := setupTracesServer(t)
	res := callTool(t, srv, "archigraph_traces", map[string]any{
		"action":     "get",
		"process_id": "p1",
	})
	txt := resultText(res)
	if !strings.Contains(txt, "\"found\": true") {
		t.Errorf("expected found=true, got: %s", txt)
	}
	if !strings.Contains(txt, "validateForm") || !strings.Contains(txt, "submitOrder") {
		t.Errorf("expected both steps in chain, got: %s", txt)
	}
}

func TestTraces_FollowAdHocBFS(t *testing.T) {
	srv := setupTracesServer(t)
	res := callTool(t, srv, "archigraph_traces", map[string]any{
		"action":         "follow",
		"entry_point_id": "f1",
		"max_depth":      5,
	})
	txt := resultText(res)
	if !strings.Contains(txt, "handleSubmit") {
		t.Errorf("expected handleSubmit in follow result, got: %s", txt)
	}
	// Should reach both terminals (f2 and ep) from f1.
	if !strings.Contains(txt, "validateForm") {
		t.Errorf("expected validateForm in follow result")
	}
}

func TestTraces_InvalidActionReturnsError(t *testing.T) {
	srv := setupTracesServer(t)
	res := callTool(t, srv, "archigraph_traces", map[string]any{"action": "bogus"})
	if res == nil || !res.IsError {
		t.Errorf("expected tool error for bogus action")
	}
}
