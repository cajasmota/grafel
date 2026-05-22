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

// TestFindCallers_NoEdgeSignal verifies that when an entity is found but has no
// callers, the response carries the explicit no-edge signal (#1618).
func TestFindCallers_NoEdgeSignal(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(1),
	})
	// "result" field must be present and set to "no_incoming_edges".
	res, ok := out["result"].(string)
	if !ok {
		t.Fatalf("expected string 'result' field in no-caller response, got %T: %v", out["result"], out["result"])
	}
	if res != "no_incoming_edges" {
		t.Errorf("expected result=no_incoming_edges, got %q", res)
	}
	// "note" field must be present and non-empty.
	note, ok := out["note"].(string)
	if !ok || note == "" {
		t.Errorf("expected non-empty 'note' field in no-caller response")
	}
}

// TestFindCallers_WithCallersNoSignal verifies that the no-edge signal is NOT
// present when callers are found (#1618 regression guard).
func TestFindCallers_WithCallersNoSignal(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	// FuncB has 1 caller (FuncA) — result field must NOT be set.
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "ent-b",
		"depth":     float64(1),
	})
	if _, hasResult := out["result"]; hasResult {
		t.Errorf("expected no 'result' field when callers are present, got %v", out["result"])
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

// TestFindCallees_NoEdgeSignal verifies that when an entity is found but has no
// callees, the response carries the explicit no-edge signal (#1618).
func TestFindCallees_NoEdgeSignal(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	// FuncC is a leaf — no outbound edges.
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{
		"entity_id": "ent-c",
		"depth":     float64(1),
	})
	// "result" field must be present and set to "no_outgoing_edges".
	res, ok := out["result"].(string)
	if !ok {
		t.Fatalf("expected string 'result' field in no-callee response, got %T: %v", out["result"], out["result"])
	}
	if res != "no_outgoing_edges" {
		t.Errorf("expected result=no_outgoing_edges, got %q", res)
	}
	// "note" field must be present and non-empty.
	note, ok := out["note"].(string)
	if !ok || note == "" {
		t.Errorf("expected non-empty 'note' field in no-callee response")
	}
}

// TestFindCallees_WithCalleesNoSignal verifies that the no-edge signal is NOT
// present when callees are found (#1618 regression guard).
func TestFindCallees_WithCalleesNoSignal(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	// FuncA calls FuncB — result field must NOT be set.
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{
		"entity_id": "ent-a",
		"depth":     float64(1),
	})
	if _, hasResult := out["result"]; hasResult {
		t.Errorf("expected no 'result' field when callees are present, got %v", out["result"])
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

// buildPublicAPIDeadCodeDoc models the polyglot-platform fixture's dead-code
// scenario: a shared module exporting several public operations where most are
// either called in-repo, imported by another repo, or legitimate API, and only
// the marker-tagged unreferenced ones are genuinely dead.
//
//	verifyToken      — exported, imported cross-repo (IMPORTS imported_name)   → LIVE
//	hasRole          — exported, unreferenced, NO dead marker (legit API)      → LIVE
//	legacySignToken  — exported, unreferenced, "legacy" marker                 → DEAD
//	postHandler      — route handler (GET /x), unreferenced                    → LIVE
//	DeadReindexAll   — exported, unreferenced, "Dead" marker                   → DEAD
func buildPublicAPIDeadCodeDoc() *graph.Document {
	return minDoc(
		[]graph.Entity{
			{ID: "mod", Name: "src", Kind: "Module", SourceFile: "src/auth.ts"},
			{ID: "verify", Name: "verifyToken", Kind: "SCOPE.Operation", SourceFile: "src/auth.ts", Language: "typescript"},
			{ID: "role", Name: "hasRole", Kind: "SCOPE.Operation", SourceFile: "src/auth.ts", Language: "typescript"},
			{ID: "legacy", Name: "legacySignToken", Kind: "SCOPE.Operation", SourceFile: "src/auth.ts", Language: "typescript"},
			{ID: "route", Name: "GET /products", Kind: "SCOPE.Operation", SourceFile: "src/index.ts", Language: "typescript"},
			{ID: "dead2", Name: "DeadReindexAll", Kind: "SCOPE.Operation", SourceFile: "internal/server.go", Language: "go"},
			// External import marker carrying imported_name=verifyToken.
			{ID: "ext", Name: "@shared/js", Kind: "SCOPE.External", SourceFile: ""},
			// A shared helper each operation calls — gives them an outbound
			// edge so they are not "fully isolated" (Class 1), exercising the
			// Class-2 unreferenced-public-operation path instead.
			{ID: "helper", Name: "jwtSign", Kind: "SCOPE.Operation", SourceFile: "src/jwt.ts", Language: "typescript"},
		},
		[]graph.Relationship{
			{FromID: "mod", ToID: "verify", Kind: "CONTAINS"},
			{FromID: "mod", ToID: "role", Kind: "CONTAINS"},
			{FromID: "mod", ToID: "legacy", Kind: "CONTAINS"},
			{FromID: "mod", ToID: "route", Kind: "CONTAINS"},
			{FromID: "mod", ToID: "dead2", Kind: "CONTAINS"},
			{FromID: "mod", ToID: "helper", Kind: "CONTAINS"},
			// Each public op calls the shared helper (outbound edge).
			{FromID: "verify", ToID: "helper", Kind: "CALLS"},
			{FromID: "role", ToID: "helper", Kind: "CALLS"},
			{FromID: "legacy", ToID: "helper", Kind: "CALLS"},
			{FromID: "route", ToID: "helper", Kind: "CALLS"},
			{FromID: "dead2", ToID: "helper", Kind: "CALLS"},
			// verifyToken is imported (consumed) cross-repo.
			{FromID: "mod", ToID: "ext", Kind: "IMPORTS", Properties: map[string]string{"imported_name": "verifyToken"}},
		},
	)
}

func TestFindDeadCode_PrecisionOnPublicAPI(t *testing.T) {
	srv := newTestServerWithDoc(t, buildPublicAPIDeadCodeDoc())
	out := callFlowTool(t, srv.handleFindDeadCode, map[string]any{})
	dead := out["dead_code"].([]any)

	flagged := map[string]bool{}
	for _, item := range dead {
		flagged[item.(map[string]any)["name"].(string)] = true
	}

	wantDead := []string{"legacySignToken", "DeadReindexAll"}
	wantLive := []string{"verifyToken", "hasRole", "GET /products"}

	for _, n := range wantDead {
		if !flagged[n] {
			t.Errorf("expected %q to be flagged as dead code", n)
		}
	}
	for _, n := range wantLive {
		if flagged[n] {
			t.Errorf("false positive: %q should NOT be flagged (live/legit API)", n)
		}
	}
	if len(flagged) != len(wantDead) {
		t.Errorf("expected exactly %d dead entities, got %d: %v", len(wantDead), len(flagged), flagged)
	}
}

func TestFindDeadCode_ImportedNotFlagged(t *testing.T) {
	srv := newTestServerWithDoc(t, buildPublicAPIDeadCodeDoc())
	out := callFlowTool(t, srv.handleFindDeadCode, map[string]any{})
	for _, item := range out["dead_code"].([]any) {
		if item.(map[string]any)["name"] == "verifyToken" {
			t.Fatal("verifyToken is imported cross-repo and must not be flagged")
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

// ---------------------------------------------------------------------------
// TestExpand_NoEdgeSignal (#1618)
// ---------------------------------------------------------------------------

// buildIsolatedDoc builds a single entity with no edges.
func buildIsolatedDoc() *graph.Document {
	return minDoc(
		[]graph.Entity{
			{ID: "iso-1", Name: "IsolatedFunc", Kind: "Function", SourceFile: "iso.go", StartLine: 1},
		},
		[]graph.Relationship{},
	)
}

// TestExpand_NoEdgeSignal verifies that archigraph_expand returns the explicit
// no-edge signal when the entity is found but has zero neighbours (#1618).
func TestExpand_NoEdgeSignal(t *testing.T) {
	doc := buildIsolatedDoc()
	srv := newTestServerWithDoc(t, doc)

	out := callFlowTool(t, srv.handleGetNeighbors, map[string]any{
		"node": "iso-1",
	})
	if out == nil {
		t.Fatal("expected JSON response, got nil")
	}
	res, ok := out["result"].(string)
	if !ok {
		t.Fatalf("expected string 'result' field in no-edge response, got %T: %v", out["result"], out["result"])
	}
	if res != "no_edges" {
		t.Errorf("expected result=no_edges, got %q", res)
	}
	note, ok := out["note"].(string)
	if !ok || note == "" {
		t.Errorf("expected non-empty 'note' field in no-edge response")
	}
	count, ok := out["count"].(float64)
	if !ok || count != 0 {
		t.Errorf("expected count=0, got %v", out["count"])
	}
}

// TestExpand_WithEdgesNoSignal verifies that the no-edge signal is NOT present
// when the entity has neighbours (#1618 regression guard).
func TestExpand_WithEdgesNoSignal(t *testing.T) {
	doc := buildChainDoc()
	srv := newTestServerWithDoc(t, doc)

	// FuncA has an outbound edge to FuncB — no no-edge signal expected.
	// handleGetNeighbors returns a flat array for the non-empty case, which
	// does not decode to map[string]any. We just verify it's not an error and
	// does not contain result=no_edges.
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"node": "ent-a"}
	res, err := srv.handleGetNeighbors(nil, req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %v", res.Content)
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			if strings.Contains(tc.Text, `"result"`) && strings.Contains(tc.Text, `"no_edges"`) {
				t.Errorf("no_edges signal must not appear when edges exist: %s", tc.Text)
			}
		}
	}
}
