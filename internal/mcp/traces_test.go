package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
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
	if !strings.Contains(txt, "\"count\":2") {
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
	if !strings.Contains(txt, "\"count\":1") {
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
	if !strings.Contains(txt, "\"found\":true") {
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

// TestTraces_NoActionDefaultsList verifies that omitting the action argument
// defaults to "list" instead of returning a hard error (#archigraph_traces).
func TestTraces_NoActionDefaultsList(t *testing.T) {
	srv := setupTracesServer(t)
	// min_steps=0 so the short-flow filter doesn't hide the fixture processes.
	res := callTool(t, srv, "archigraph_traces", map[string]any{"min_steps": 0})
	if res == nil {
		t.Fatal("nil result when action is omitted")
	}
	if res.IsError {
		t.Errorf("omitting action should default to list, got tool error: %s", resultText(res))
	}
	txt := resultText(res)
	if !strings.Contains(txt, "\"count\"") {
		t.Errorf("expected list response with count, got: %s", txt)
	}
}

// ---------------------------------------------------------------------------
// #1738: token_budget enforcement for traces action=list
// ---------------------------------------------------------------------------

// build20ProcessDoc returns a document with 20 SCOPE.Process entities.
func build20ProcessDoc() *graph.Document {
	entities := make([]graph.Entity, 20)
	for i := range entities {
		entities[i] = graph.Entity{
			ID:   fmt.Sprintf("proc%02d", i),
			Name: fmt.Sprintf("Process%02d", i),
			Kind: "SCOPE.Process",
			Properties: map[string]string{
				"step_count":  "10",
				"cross_stack": "false",
				"entry_id":    fmt.Sprintf("e%02d", i),
				"entry_name":  fmt.Sprintf("Entry%02d", i),
				"terminal_id": fmt.Sprintf("t%02d", i),
			},
		}
	}
	return &graph.Document{Entities: entities}
}

// TestTracesList_DefaultLimit10 verifies that without an explicit limit,
// the list returns at most 10 items (#1738 default reduction from 25).
func TestTracesList_DefaultLimit10(t *testing.T) {
	srv := newTestServerWithDoc(t, build20ProcessDoc())
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"action":    "list",
		"min_steps": float64(0), // include all flows
		"group":     "test",
	}
	res, err := srv.handleTraces(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	out := extractResultJSON(t, res)
	count, _ := out["count"].(float64)
	if int(count) > 10 {
		t.Errorf("traces list returned %v items, want ≤10 (default limit)", count)
	}
}

// TestTracesList_TokenBudgetEnforced verifies that a tight token_budget
// caps the list and adds a truncation_note (#1738).
func TestTracesList_TokenBudgetEnforced(t *testing.T) {
	srv := newTestServerWithDoc(t, build20ProcessDoc())
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"action":       "list",
		"limit":        float64(20), // ask for all 20
		"token_budget": float64(50), // tiny budget — forces truncation
		"min_steps":    float64(0),
		"group":        "test",
	}
	res, err := srv.handleTraces(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	out := extractResultJSON(t, res)
	count, _ := out["count"].(float64)
	if int(count) >= 20 {
		t.Errorf("traces list returned %v items, want <20 (budget cap)", count)
	}
	truncNote, _ := out["truncation_note"].(string)
	if truncNote == "" {
		t.Errorf("expected truncation_note when token_budget is exceeded")
	}
}

// ---------------------------------------------------------------------------
// #1905: bridge steps in cross-repo flows carry correct repo prefix + metadata
// ---------------------------------------------------------------------------

// buildCrossRepoFlowFixture constructs a two-repo group where the seed repo
// ("frontend") holds a Process entity whose STEP_IN_PROCESS edges reference
// both frontend entities AND a backend entity from the "backend" companion
// repo. This mirrors what RunProcessFlowWithCompanions emits for cross-repo
// flows (#1893/#1905).
//
// Chain: fe_entry (frontend) → fe_caller (frontend) → be_handler (backend)
//
// All three STEP_IN_PROCESS edges live in the frontend doc (the seed repo),
// but be_handler's entity lives in the backend doc.
func buildCrossRepoFlowFixture() (frontend, backend *graph.Document) {
	frontend = &graph.Document{
		Repo: "frontend",
		Entities: []graph.Entity{
			{ID: "fe_entry", Name: "loadDashboard", Kind: "SCOPE.Function", SourceFile: "dashboard.ts", StartLine: 10},
			{ID: "fe_caller", Name: "fetchSummary", Kind: "SCOPE.Function", SourceFile: "dashboard.ts", StartLine: 20},
			// Process spanning frontend + backend via bridge at step 2.
			{ID: "proc_xr", Name: "loadDashboard → getSummary", Kind: "SCOPE.Process",
				SourceFile: "dashboard.ts", StartLine: 10,
				Properties: map[string]string{
					"entry_id":    "fe_entry",
					"entry_name":  "loadDashboard",
					"terminal_id": "be_handler",
					"step_count":  "3",
					"cross_stack": "true",
					"chain":       "fe_entry,fe_caller,be_handler",
				}},
		},
		Relationships: []graph.Relationship{
			// STEP_IN_PROCESS edges for proc_xr: steps 0+1 are frontend, step 2
			// is a bridge into the backend repo.
			{ID: "s0", FromID: "proc_xr", ToID: "fe_entry", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "0"}},
			{ID: "s1", FromID: "proc_xr", ToID: "fe_caller", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "1"}},
			// Bridge step: ToID lives in the backend doc, not in this doc.
			{ID: "s2", FromID: "proc_xr", ToID: "be_handler", Kind: "STEP_IN_PROCESS", Properties: map[string]string{"step_index": "2"}},
		},
	}
	backend = &graph.Document{
		Repo: "backend",
		Entities: []graph.Entity{
			{ID: "be_handler", Name: "OrdersController.getSummary", Kind: "SCOPE.Operation",
				SourceFile: "OrdersController.java", StartLine: 42},
		},
	}
	return frontend, backend
}

// TestTracesGet_BridgeStepMetadata_1905 asserts that when a cross-repo Process
// is fetched via archigraph_traces action=get, the bridge step (whose entity
// lives in the companion repo) is enriched with name, file, line, and repo,
// and its id carries the companion repo prefix — not the seed repo prefix.
func TestTracesGet_BridgeStepMetadata_1905(t *testing.T) {
	frontend, backend := buildCrossRepoFlowFixture()
	srv := newTestServerWithDocs(t, map[string]*graph.Document{
		"frontend": frontend,
		"backend":  backend,
	})

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"action":     "get",
		"process_id": "frontend::proc_xr",
		"group":      "test",
	}
	res, err := srv.handleTraces(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("unexpected error from traces get: %v", res)
	}
	txt := resultText(res)

	var result map[string]any
	if err := json.Unmarshal([]byte(txt), &result); err != nil {
		t.Fatalf("invalid JSON response: %v\nraw: %s", err, txt)
	}
	if result["found"] != true {
		t.Fatalf("process not found in response: %s", txt)
	}

	steps, ok := result["steps"].([]any)
	if !ok {
		t.Fatalf("steps not an array in response: %s", txt)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d: %s", len(steps), txt)
	}

	// Verify the bridge step (index 2) carries backend metadata.
	var bridgeStep map[string]any
	for _, s := range steps {
		m, _ := s.(map[string]any)
		if idx, _ := m["step_index"].(float64); int(idx) == 2 {
			bridgeStep = m
			break
		}
	}
	if bridgeStep == nil {
		t.Fatalf("bridge step (step_index=2) missing from steps: %s", txt)
	}

	// The id prefix must be the backend repo, not the frontend repo.
	id, _ := bridgeStep["id"].(string)
	if !strings.HasPrefix(id, "backend::") {
		t.Errorf("bridge step id must carry backend:: prefix, got %q", id)
	}
	if strings.HasPrefix(id, "frontend::") {
		t.Errorf("bridge step id must NOT carry frontend:: prefix, got %q", id)
	}

	// name, file, line must all be populated for the bridge step.
	name, _ := bridgeStep["name"].(string)
	if name == "" {
		t.Errorf("bridge step missing 'name' field: %v", bridgeStep)
	}
	if name != "OrdersController.getSummary" {
		t.Errorf("bridge step name = %q, want OrdersController.getSummary", name)
	}
	file, _ := bridgeStep["file"].(string)
	if file == "" {
		t.Errorf("bridge step missing 'file' field: %v", bridgeStep)
	}
	line, hasLine := bridgeStep["line"]
	if !hasLine {
		t.Errorf("bridge step missing 'line' field (StartLine=42 in fixture): %v", bridgeStep)
	}
	if line.(float64) != 42 {
		t.Errorf("bridge step line = %v, want 42", line)
	}

	// Seed-repo steps (index 0 and 1) must carry frontend:: prefix.
	for _, s := range steps {
		m, _ := s.(map[string]any)
		idx, _ := m["step_index"].(float64)
		if int(idx) == 2 {
			continue
		}
		sid, _ := m["id"].(string)
		if !strings.HasPrefix(sid, "frontend::") {
			t.Errorf("seed step (index=%d) id should carry frontend:: prefix, got %q", int(idx), sid)
		}
	}
}
