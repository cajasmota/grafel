// issue2665_nav_surface_test.go — integration tests for the navigation
// surface added to grafel_endpoints and grafel_find_callers (#2665).
//
// Covers:
//   - grafel_endpoints(kind=navigation) returns aggregated NAVIGATES_TO routes
//   - grafel_endpoints(action=definitions, include_navigation=true) adds the
//     "navigation_routes" key alongside HTTP definitions
//   - grafel_find_callers("/route/path") resolves a route literal via
//     reverse NAVIGATES_TO traversal and returns call-site entities with
//     file:line + params_keys
package mcp

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// buildNavSurfaceDoc builds a small fixture with three call-sites navigating
// to two routes plus one HTTP endpoint definition for the include_navigation
// merge test.
//
//	pushA --NAVIGATES_TO--> route:/users/[id]  (params_keys=["id","mode"])
//	pushB --NAVIGATES_TO--> route:/users/[id]  (params_keys=["id"])
//	pushC --NAVIGATES_TO--> route:/dashboard   (no params)
//
// HTTP definition: GET /api/users (so include_navigation merging is observable).
func buildNavSurfaceDoc() *graph.Document {
	doc := &graph.Document{Repo: "ns-repo"}
	doc.Entities = []graph.Entity{
		{ID: "pushA", Name: "pushA", Kind: "function", SourceFile: "screens/a.tsx", StartLine: 10},
		{ID: "pushB", Name: "pushB", Kind: "function", SourceFile: "screens/b.tsx", StartLine: 20},
		{ID: "pushC", Name: "pushC", Kind: "function", SourceFile: "screens/c.tsx", StartLine: 30},
		graph.Entity{
			ID: "ep1", Name: "GET /api/users", Kind: "http_endpoint_definition",
			SourceFile: "api/users.go", StartLine: 5,
		}.WithProperties(map[string]string{"path": "/api/users", "verb": "GET"}),
	}
	doc.Relationships = []graph.Relationship{
		graph.Relationship{
			ID: "n1", FromID: "pushA", ToID: "route:/users/[id]", Kind: "NAVIGATES_TO",
		}.WithProperties(map[string]string{
			"route":       "/users/[id]",
			"params":      "id, mode",
			"params_keys": `["id","mode"]`,
			"line":        "12",
			"via":         "navigation_call",
		},
		),
		graph.Relationship{
			ID: "n2", FromID: "pushB", ToID: "route:/users/[id]", Kind: "NAVIGATES_TO",
		}.WithProperties(map[string]string{
			"route":       "/users/[id]",
			"params":      "id",
			"params_keys": `["id"]`,
			"line":        "22",
			"via":         "navigation_call",
		},
		),
		graph.Relationship{
			ID: "n3", FromID: "pushC", ToID: "route:/dashboard", Kind: "NAVIGATES_TO",
		}.WithProperties(map[string]string{
			"route": "/dashboard",
			"line":  "32",
			"via":   "navigation_call",
		},
		),
	}
	return doc
}

func callEndpointsTool(t *testing.T, srv *Server, args map[string]any) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := srv.handleEndpoints(context.Background(), req)
	if err != nil {
		t.Fatalf("handleEndpoints error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("endpoints tool error: %v", res)
	}
	return extractResultJSON(t, res)
}

// TestEndpoints_KindNavigation verifies that kind=navigation surfaces the
// distinct NAVIGATES_TO routes with aggregated params_keys and call-site
// counts. #2665.
func TestEndpoints_KindNavigation(t *testing.T) {
	srv := newTestServer(t, buildNavSurfaceDoc())
	out := callEndpointsTool(t, srv, map[string]any{
		"action": "definitions", // ignored — kind=navigation short-circuits
		"kind":   "navigation",
		"group":  "test",
	})

	if k, _ := out["kind"].(string); k != "navigation" {
		t.Fatalf("expected kind='navigation', got %q", k)
	}
	routesRaw, ok := out["routes"].([]any)
	if !ok {
		t.Fatalf("response missing 'routes' slice: %v", out)
	}
	if len(routesRaw) != 2 {
		t.Fatalf("expected 2 distinct routes (users/dashboard), got %d: %v", len(routesRaw), routesRaw)
	}

	// Find the /users/[id] route entry and verify aggregated params_keys
	// merged from both call-sites ("id" + "mode").
	var users map[string]any
	for _, r := range routesRaw {
		m, _ := r.(map[string]any)
		if rt, _ := m["route"].(string); rt == "/users/[id]" {
			users = m
			break
		}
	}
	if users == nil {
		t.Fatalf("did not find /users/[id] route in output: %v", routesRaw)
	}
	cs, _ := users["call_sites"].(float64)
	if cs != 2 {
		t.Errorf("expected call_sites=2 for /users/[id], got %v", cs)
	}
	pk, _ := users["params_keys"].(string)
	if pk != `["id","mode"]` {
		t.Errorf("expected merged params_keys=[\"id\",\"mode\"], got %q", pk)
	}
}

// TestEndpoints_IncludeNavigation verifies that action=definitions with
// include_navigation=true returns the standard HTTP definitions payload AND
// a "navigation_routes" key with the aggregated NAVIGATES_TO routes. #2665.
func TestEndpoints_IncludeNavigation(t *testing.T) {
	srv := newTestServer(t, buildNavSurfaceDoc())
	out := callEndpointsTool(t, srv, map[string]any{
		"action":             "definitions",
		"include_navigation": true,
		"group":              "test",
	})

	// HTTP definitions should still be present (lines, count, total fields).
	if _, ok := out["total"]; !ok {
		t.Errorf("expected HTTP-definitions envelope keys to be present; got %v", out)
	}
	// Navigation routes should be merged in.
	nr, ok := out["navigation_routes"].([]any)
	if !ok {
		t.Fatalf("expected 'navigation_routes' slice in merged response: %v", out)
	}
	if len(nr) != 2 {
		t.Errorf("expected 2 nav routes, got %d", len(nr))
	}
	navCount, _ := out["navigation_count"].(float64)
	if navCount != 2 {
		t.Errorf("expected navigation_count=2, got %v", navCount)
	}
	if v, _ := out["include_navigation"].(bool); !v {
		t.Errorf("expected include_navigation=true in response")
	}
}

// TestFindCallers_RouteLiteralResolves verifies that passing a route literal
// (string starting with "/") to grafel_find_callers resolves it via the
// NAVIGATES_TO reverse traversal and returns push-site callers carrying
// file:line + params_keys. #2665.
func TestFindCallers_RouteLiteralResolves(t *testing.T) {
	srv := newTestServer(t, buildNavSurfaceDoc())

	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"entity_id": "/users/[id]",
		"group":     "test",
	}
	res, err := srv.handleFindCallers(context.Background(), req)
	if err != nil {
		t.Fatalf("handleFindCallers error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("tool returned error: %v", res)
	}
	out := extractResultJSON(t, res)

	if r, _ := out["resolved_as"].(string); r != "navigation_route" {
		t.Errorf("expected resolved_as='navigation_route', got %q", r)
	}
	callersRaw, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("response missing 'callers' slice: %v", out)
	}
	if len(callersRaw) != 2 {
		t.Fatalf("expected 2 callers for /users/[id], got %d: %v", len(callersRaw), callersRaw)
	}
	// Each caller must carry file, line, route, params_keys.
	sawIDOnly := false
	sawIDAndMode := false
	for _, c := range callersRaw {
		m, _ := c.(map[string]any)
		if f, _ := m["file"].(string); f == "" {
			t.Errorf("caller missing 'file': %v", m)
		}
		if line, _ := m["line"].(float64); line == 0 {
			t.Errorf("caller missing 'line': %v", m)
		}
		pk, _ := m["params_keys"].(string)
		switch pk {
		case `["id"]`:
			sawIDOnly = true
		case `["id","mode"]`:
			sawIDAndMode = true
		}
	}
	if !sawIDOnly || !sawIDAndMode {
		t.Errorf("expected one caller with params_keys=[\"id\"] and one with [\"id\",\"mode\"]; sawIDOnly=%v sawIDAndMode=%v", sawIDOnly, sawIDAndMode)
	}
}

// TestFindCallers_NonRouteEntityFallsThrough verifies that an entity_id that
// happens to start with "/" but matches no NAVIGATES_TO edge falls through
// to the standard "entity not found" error path rather than silently
// returning the route-resolve empty case.
func TestFindCallers_NonRouteEntityFallsThrough(t *testing.T) {
	srv := newTestServer(t, buildNavSurfaceDoc())
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"entity_id": "/does/not/exist",
		"group":     "test",
	}
	res, err := srv.handleFindCallers(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if !res.IsError {
		t.Errorf("expected IsError=true for unknown route literal; got %v", res)
	}
}
