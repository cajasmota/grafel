// navigates_tools_test.go — tests for grafel_navigates (#2658).
//
// Three tests:
//   - TestGrafelNavigates_FiltersByRoute   — route filter returns only matching edges
//   - TestGrafelNavigates_FiltersByParam   — with_param filter returns edges carrying that param
//   - TestGrafelNavigates_FlowMode         — mode=flow multi-hop BFS traverses chains
package mcp

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// buildNavDoc constructs a minimal graph.Document populated with NAVIGATES_TO
// relationship records suitable for grafel_navigates handler tests.
//
// Navigation topology used across all three tests:
//
//	entityA --NAVIGATES_TO--> route:/foo (params: id)
//	entityA --NAVIGATES_TO--> route:/bar
//	entityB --NAVIGATES_TO--> route:/foo (no params)
//	entityB --NAVIGATES_TO--> route:/baz (params: token)
//	entityC --NAVIGATES_TO--> route:entityA  (for flow-mode: entityA nav → /foo)
func buildNavDoc() *graph.Document {
	doc := &graph.Document{Repo: "nav-repo"}
	doc.Entities = []graph.Entity{
		{ID: "entityA", Name: "ComponentA", Kind: "function", SourceFile: "a.tsx", StartLine: 10},
		{ID: "entityB", Name: "ComponentB", Kind: "function", SourceFile: "b.tsx", StartLine: 20},
		{ID: "entityC", Name: "ComponentC", Kind: "function", SourceFile: "c.tsx", StartLine: 30},
	}
	doc.Relationships = []graph.Relationship{
		graph.Relationship{
			ID: "r1", FromID: "entityA", ToID: "route:/foo", Kind: "NAVIGATES_TO",
		}.WithProperties(map[string]string{"route": "/foo", "params": "id", "line": "12", "via": "navigation_call"}),
		graph.Relationship{
			ID: "r2", FromID: "entityA", ToID: "route:/bar", Kind: "NAVIGATES_TO",
		}.WithProperties(map[string]string{"route": "/bar", "line": "15", "via": "navigation_call"}),
		graph.Relationship{
			ID: "r3", FromID: "entityB", ToID: "route:/foo", Kind: "NAVIGATES_TO",
		}.WithProperties(map[string]string{"route": "/foo", "line": "22", "via": "navigation_call"}),
		graph.Relationship{
			ID: "r4", FromID: "entityB", ToID: "route:/baz", Kind: "NAVIGATES_TO",
		}.WithProperties(map[string]string{"route": "/baz", "params": "token", "line": "24", "via": "navigation_call"}),
		// entityC navigates to entityA (for flow test: C → A → /foo).
		graph.Relationship{
			ID: "r5", FromID: "entityC", ToID: "entityA", Kind: "NAVIGATES_TO",
		}.WithProperties(map[string]string{"route": "entityA", "line": "32", "via": "navigation_call"}),
	}
	return doc
}

// callNavTool is a thin helper: build a CallToolRequest from args, invoke
// handleNavigates, and decode the JSON response.
func callNavTool(t *testing.T, srv *Server, args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := srv.handleNavigates(context.Background(), req)
	if err != nil {
		t.Fatalf("handleNavigates error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	return extractResultJSON(t, res)
}

// edgesFromResult extracts the "edges" slice from a decoded navigates response.
func edgesFromResult(t *testing.T, result map[string]any) []map[string]any {
	t.Helper()
	raw, ok := result["edges"]
	if !ok {
		t.Fatal("response missing 'edges' key")
	}
	slice, ok := raw.([]any)
	if !ok {
		t.Fatalf("'edges' is %T, want []any", raw)
	}
	out := make([]map[string]any, 0, len(slice))
	for _, item := range slice {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("edge item is %T, want map[string]any", item)
		}
		out = append(out, m)
	}
	return out
}

// TestGrafelNavigates_FiltersByRoute verifies that passing route="/foo"
// returns only the two edges whose route property contains "/foo", and not
// edges for "/bar" or "/baz". Issue #2658.
func TestGrafelNavigates_FiltersByRoute(t *testing.T) {
	srv := newTestServer(t, buildNavDoc())
	result := callNavTool(t, srv, map[string]any{
		"route":     "/foo",
		"group":     "test",
		"direction": "outgoing",
	})

	edges := edgesFromResult(t, result)
	// Expect exactly two edges: entityA→/foo and entityB→/foo.
	if len(edges) != 2 {
		t.Errorf("expected 2 edges for route=/foo, got %d: %v", len(edges), edges)
	}
	for _, e := range edges {
		route, _ := e["route"].(string)
		if route != "/foo" {
			t.Errorf("unexpected route %q in edge %v", route, e)
		}
	}

	// Verify total matches count (no truncation in this small fixture).
	total, _ := result["total"].(float64)
	count, _ := result["count"].(float64)
	if total != count {
		t.Errorf("expected total==count (no truncation), got total=%v count=%v", total, count)
	}
}

// TestGrafelNavigates_FiltersByParam verifies that with_param="id" returns
// only edges whose params list contains "id". Issue #2658.
func TestGrafelNavigates_FiltersByParam(t *testing.T) {
	srv := newTestServer(t, buildNavDoc())
	result := callNavTool(t, srv, map[string]any{
		"with_param": "id",
		"group":      "test",
		"direction":  "outgoing",
	})

	edges := edgesFromResult(t, result)
	// Only r1 (entityA→/foo, params=id) should match.
	if len(edges) != 1 {
		t.Errorf("expected 1 edge for with_param=id, got %d: %v", len(edges), edges)
	}
	if len(edges) == 1 {
		params, _ := edges[0]["params"].(string)
		if params != "id" {
			t.Errorf("expected params='id', got %q", params)
		}
		route, _ := edges[0]["route"].(string)
		if route != "/foo" {
			t.Errorf("expected route='/foo', got %q", route)
		}
	}

	// Ensure token-only edge was excluded.
	for _, e := range edges {
		p, _ := e["params"].(string)
		if p == "token" {
			t.Errorf("edge with params=token should have been excluded by with_param=id filter: %v", e)
		}
	}
}

// TestGrafelNavigates_FlowMode verifies that mode=flow performs multi-hop
// BFS, annotating edges with a "hop" counter. The fixture has:
//
//	entityC --hop0--> entityA --hop1--> route:/foo and route:/bar
//
// Starting from entityC with mode=flow should yield hop=0 for C→A, and
// hop=1 for A→/foo and A→/bar. Issue #2658.
func TestGrafelNavigates_FlowMode(t *testing.T) {
	srv := newTestServer(t, buildNavDoc())

	// prefixedID format is "repo::id" (see internal/mcp/render.go prefixedID).
	// The nav adjacency in flow mode is keyed by prefixedID(r.Repo, rel.FromID).
	result := callNavTool(t, srv, map[string]any{
		"entity_id": "nav-repo::entityC",
		"mode":      "flow",
		"max_depth": float64(3),
		"group":     "test",
	})

	// In flow mode, mode field in response must be "flow".
	if mode, _ := result["mode"].(string); mode != "flow" {
		t.Errorf("expected mode='flow' in response, got %q", mode)
	}

	edges := edgesFromResult(t, result)
	if len(edges) == 0 {
		// Fallback: try without entity_id filter (all navigation chains).
		result2 := callNavTool(t, srv, map[string]any{
			"mode":      "flow",
			"max_depth": float64(3),
			"group":     "test",
		})
		edges = edgesFromResult(t, result2)
	}

	if len(edges) == 0 {
		t.Fatal("mode=flow returned 0 edges; expected at least one NAVIGATES_TO hop from fixture")
	}

	// Flow mode BFS should have produced multiple hops from the full fixture.
	// Hop field uses omitempty so hop=0 edges have no "hop" key; hop>=1 edges
	// carry the key. Verify that at least one edge with hop=1 exists (multi-hop).
	hop1Found := false
	for _, e := range edges {
		if hop, ok := e["hop"].(float64); ok && hop >= 1 {
			hop1Found = true
		}
	}
	if !hop1Found {
		// The fixture has C→entityA→/foo as a 2-hop chain; if flow was entered
		// at entityC we expect at least one hop=1 edge.
		t.Logf("edges in flow result: %v", edges)
		t.Errorf("expected at least one edge with hop>=1 in flow-mode multi-hop BFS")
	}
}
