package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// callFlowTool invokes a handler directly (expects JSON result).
func callFlowTool(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	var out map[string]any
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			if err := json.Unmarshal([]byte(tc.Text), &out); err != nil {
				// May be markdown (summarize_subgraph) — return nil.
				return nil
			}
			return out
		}
	}
	t.Fatal("no text content")
	return nil
}

// callFlowToolText returns the raw text result (for summarize_subgraph markdown).
func callFlowToolText(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) string {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("no text content")
	return ""
}

// callFlowToolError expects the handler to return a tool error; returns the error text.
func callFlowToolError(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) string {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		return err.Error()
	}
	if res != nil && res.IsError {
		for _, c := range res.Content {
			if tc, ok := c.(mcpapi.TextContent); ok {
				return tc.Text
			}
		}
	}
	t.Fatal("expected error result but got success")
	return ""
}

// buildChainDoc builds: A --CALLS--> B --CALLS--> C
func buildChainDoc() *graph.Document {
	return minDoc(
		[]graph.Entity{
			{ID: "ent-a", Name: "FuncA", Kind: "Function", SourceFile: "a.go", StartLine: 10},
			{ID: "ent-b", Name: "FuncB", Kind: "Function", SourceFile: "b.go", StartLine: 20},
			{ID: "ent-c", Name: "FuncC", Kind: "Function", SourceFile: "c.go", StartLine: 30},
		},
		[]graph.Relationship{
			{FromID: "ent-a", ToID: "ent-b", Kind: "CALLS"},
			{FromID: "ent-b", ToID: "ent-c", Kind: "CALLS"},
		},
	)
}

// buildDeadCodeDoc builds: A --CALLS--> B, and C (isolated).
func buildDeadCodeDoc() *graph.Document {
	return minDoc(
		[]graph.Entity{
			{ID: "ent-a", Name: "FuncA", Kind: "Function", SourceFile: "a.go"},
			{ID: "ent-b", Name: "FuncB", Kind: "Function", SourceFile: "b.go"},
			{ID: "ent-c", Name: "DeadFunc", Kind: "Function", SourceFile: "dead.go"},
			{ID: "ent-ext", Name: "fmt.Println", Kind: "stdlib.Function", SourceFile: ""},
		},
		[]graph.Relationship{
			{FromID: "ent-a", ToID: "ent-b", Kind: "CALLS"},
		},
	)
}

// ---------------------------------------------------------------------------
// TestFindCallers
// ---------------------------------------------------------------------------

func TestFindCallers_DirectCaller(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	// FuncB has 1 direct caller: FuncA.
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-b",
		"depth":     float64(1),
	})

	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	if len(callers) != 1 {
		t.Fatalf("expected 1 caller, got %d", len(callers))
	}
	first := callers[0].(map[string]any)
	if first["name"] != "FuncA" {
		t.Errorf("expected caller=FuncA, got %v", first["name"])
	}
	if first["hop_count"].(float64) != 1 {
		t.Errorf("expected hop_count=1, got %v", first["hop_count"])
	}
}

func TestFindCallers_Transitive(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	// FuncC has 1 direct caller (FuncB) and 1 transitive caller (FuncA at depth 2).
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-c",
		"depth":     float64(2),
	})

	callers, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T", out["callers"])
	}
	if len(callers) != 2 {
		t.Fatalf("expected 2 callers (direct + transitive), got %d", len(callers))
	}
}

func TestFindCallers_NotFound(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)
	errMsg := callFlowToolError(t, srv.handleFindCallers, map[string]any{
		"entity_id": "no-such-entity",
	})
	if errMsg == "" {
		t.Fatal("expected error for unknown entity")
	}
}

func TestFindCallers_NoCallers(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	// FuncA has no callers.
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(1),
	})
	callers := out["callers"].([]any)
	if len(callers) != 0 {
		t.Errorf("expected 0 callers for root, got %d", len(callers))
	}
}

// ---------------------------------------------------------------------------
// TestFindCallees
// ---------------------------------------------------------------------------

func TestFindCallees_Direct(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	// FuncA calls FuncB directly.
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(1),
	})
	callees, ok := out["callees"].([]any)
	if !ok {
		t.Fatalf("expected callees array, got %T", out["callees"])
	}
	if len(callees) != 1 {
		t.Fatalf("expected 1 callee, got %d", len(callees))
	}
	first := callees[0].(map[string]any)
	if first["name"] != "FuncB" {
		t.Errorf("expected callee=FuncB, got %v", first["name"])
	}
}

func TestFindCallees_Transitive(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	// FuncA calls FuncB (hop 1) and transitively FuncC (hop 2).
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(2),
	})
	callees := out["callees"].([]any)
	if len(callees) != 2 {
		t.Fatalf("expected 2 callees, got %d", len(callees))
	}
}

func TestFindCallees_LeafReturnsEmpty(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	// FuncC is a leaf — no outbound edges.
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{
		"entity_id": "ent-c",
		"depth":     float64(1),
	})
	callees := out["callees"].([]any)
	if len(callees) != 0 {
		t.Errorf("expected 0 callees for leaf, got %d", len(callees))
	}
}

// ---------------------------------------------------------------------------
// TestImpactRadius
// ---------------------------------------------------------------------------

func TestImpactRadius_RootChanges(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	// Changing FuncB affects FuncA (its caller).
	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id": "ent-b",
		"hops":      float64(1),
	})
	affected, ok := out["affected"].([]any)
	if !ok {
		t.Fatalf("expected affected array, got %T", out["affected"])
	}
	if len(affected) == 0 {
		t.Fatal("expected at least 1 affected entity")
	}
	// First result is highest-risk (FuncA is the only caller).
	first := affected[0].(map[string]any)
	if first["name"] != "FuncA" {
		t.Errorf("expected FuncA in impact, got %v", first["name"])
	}
	// risk_score must be in [0, 1].
	rs, ok := first["risk_score"].(float64)
	if !ok {
		t.Fatalf("expected numeric risk_score, got %T", first["risk_score"])
	}
	if rs < 0 || rs > 1 {
		t.Errorf("risk_score out of [0,1]: %v", rs)
	}
}

func TestImpactRadius_RootHasNoUpstreamImpact(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	// FuncA is the root of the chain (no inbound callers), so changing it
	// affects nobody above it. impact_radius walks inbound, so count = 0.
	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id": "ent-a",
		"hops":      float64(1),
	})
	affected := out["affected"].([]any)
	if len(affected) != 0 {
		t.Errorf("expected 0 affected for root (no callers), got %d", len(affected))
	}
}

// ---------------------------------------------------------------------------
// TestSummarizeSubgraph
// ---------------------------------------------------------------------------

func TestSummarizeSubgraph_MarkdownContainsName(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	text := callFlowToolText(t, srv.handleSummarizeSubgraph, map[string]any{
		"entity_id": "ent-b",
		"depth":     float64(1),
	})

	if !strings.Contains(text, "FuncB") {
		t.Errorf("summary should contain entity name FuncB; got:\n%s", text)
	}
	if !strings.Contains(text, "Called by") {
		t.Errorf("summary should have 'Called by' section; got:\n%s", text)
	}
	if !strings.Contains(text, "Calls") {
		t.Errorf("summary should have 'Calls' section; got:\n%s", text)
	}
}

func TestSummarizeSubgraph_RootNoCallers(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	text := callFlowToolText(t, srv.handleSummarizeSubgraph, map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(1),
	})
	if !strings.Contains(text, "No callers") {
		t.Errorf("FuncA has no callers; summary should say so:\n%s", text)
	}
}

// ---------------------------------------------------------------------------
// TestFindDeadCode
// ---------------------------------------------------------------------------

func TestFindDeadCode_IsolatedEntity(t *testing.T) {
	doc := buildDeadCodeDoc()
	srv := newTestServerWithDoc(t, doc)

	out := callFlowTool(t, srv.handleFindDeadCode, map[string]any{})
	dead, ok := out["dead_code"].([]any)
	if !ok {
		t.Fatalf("expected dead_code array, got %T", out["dead_code"])
	}
	// DeadFunc (ent-c) should appear; FuncA→FuncB are connected; ent-ext is stdlib.
	found := false
	for _, item := range dead {
		m := item.(map[string]any)
		if m["name"] == "DeadFunc" {
			found = true
		}
		// FuncA and FuncB must NOT appear (they have edges between them).
		if m["name"] == "FuncA" || m["name"] == "FuncB" {
			t.Errorf("FuncA/FuncB should not be dead code, but appeared: %v", m["name"])
		}
		// stdlib entities must not appear.
		if m["name"] == "fmt.Println" {
			t.Errorf("stdlib entity should not appear in dead code results")
		}
	}
	if !found {
		t.Error("DeadFunc should be listed as dead code")
	}
}

func TestFindDeadCode_KindFilter(t *testing.T) {
	doc := buildDeadCodeDoc()
	srv := newTestServerWithDoc(t, doc)

	// Filter to "Class" — no entities match, expect empty.
	out := callFlowTool(t, srv.handleFindDeadCode, map[string]any{
		"kind_filter": "Class",
	})
	dead := out["dead_code"].([]any)
	if len(dead) != 0 {
		t.Errorf("expected 0 dead Class entities, got %d", len(dead))
	}
}

func TestFindDeadCode_StdlibExcluded(t *testing.T) {
	doc := buildDeadCodeDoc()
	srv := newTestServerWithDoc(t, doc)

	out := callFlowTool(t, srv.handleFindDeadCode, map[string]any{})
	dead := out["dead_code"].([]any)
	for _, item := range dead {
		m := item.(map[string]any)
		if name, _ := m["name"].(string); name == "fmt.Println" {
			t.Error("stdlib entity fmt.Println must not appear in dead code")
		}
	}
}

// ---------------------------------------------------------------------------
// TestImpactRiskScore unit tests
// ---------------------------------------------------------------------------

func TestImpactRiskScore_HighInDegree(t *testing.T) {
	e := &graph.Entity{Kind: "Function", Properties: map[string]string{}}
	score := impactRiskScore(e, 50)
	if score <= 0 {
		t.Errorf("high in-degree should produce score > 0, got %v", score)
	}
}

func TestImpactRiskScore_APIBoundary(t *testing.T) {
	e := &graph.Entity{Kind: "http_endpoint_definition", Properties: map[string]string{}}
	score := impactRiskScore(e, 0)
	if score < 0.25 {
		t.Errorf("API boundary entity should score >= 0.25, got %v", score)
	}
}

func TestImpactRiskScore_WithCoverage(t *testing.T) {
	eCovered := &graph.Entity{Kind: "Function", Properties: map[string]string{"test_coverage": "85"}}
	eUncovered := &graph.Entity{Kind: "Function", Properties: map[string]string{}}
	scoreCovered := impactRiskScore(eCovered, 0)
	scoreUncovered := impactRiskScore(eUncovered, 0)
	if scoreCovered >= scoreUncovered {
		t.Errorf("covered entity (%v) should score lower than uncovered (%v)", scoreCovered, scoreUncovered)
	}
}
