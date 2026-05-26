// ux_1650_test.go — end-to-end demonstration tests for the #1650 MCP UX
// overhaul. Each test corresponds to a verification scenario from the issue:
//
//	(a) find_callers / find_callees / expand return ids in items
//	(b) get_source accepts a label/qualified_name (clarifier on ambiguity)
//	(c) endpoints path_contains + method filter, terse one-line render
//	(d) endpoint stats: cross-repo links fold into resolved counts
//	(e) find_paths walks across repos via cross-repo links
//	(f) cwd-based group inference resolves group without explicit group=
//	(g) every JSON tool response carries elapsed_ms (covered by wrap test)
package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// scenario_a — flow handlers carry entity_id on every result item.
func TestUX1650_FlowToolsReturnIDs(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "a", Name: "A", Kind: "Function", SourceFile: "a.go", StartLine: 1},
			{ID: "b", Name: "B", Kind: "Function", SourceFile: "b.go", StartLine: 1},
			{ID: "c", Name: "C", Kind: "Function", SourceFile: "c.go", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			{FromID: "a", ToID: "b", Kind: "CALLS"},
			{FromID: "b", ToID: "c", Kind: "CALLS"},
		},
	}
	srv := newTestServer(t, doc)
	res := callEndpointTool(t, srv.handleFindCallers, map[string]any{"group": "test", "entity_id": "b"})
	callers := getSlice(t, res, "callers")
	if len(callers) == 0 {
		t.Fatal("expected callers")
	}
	for _, c := range callers {
		obj := c.(map[string]any)
		// #1739: narrow default shape renamed entity_id → id.
		if eid, _ := obj["id"].(string); eid == "" {
			t.Errorf("caller missing id: %v", obj)
		}
	}
	res = callEndpointTool(t, srv.handleFindCallees, map[string]any{"group": "test", "entity_id": "b"})
	callees := getSlice(t, res, "callees")
	for _, c := range callees {
		obj := c.(map[string]any)
		// #1739: narrow default shape renamed entity_id → id.
		if eid, _ := obj["id"].(string); eid == "" {
			t.Errorf("callee missing id: %v", obj)
		}
	}
}

// scenario_b — get_source accepts a label and returns a clarifier list when
// multiple entities share the same label.
func TestUX1650_GetSourceAcceptsLabelWithClarifier(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "h1", Name: "handler", QualifiedName: "a.handler", Kind: "Function", SourceFile: "a.go", StartLine: 10, EndLine: 12},
			{ID: "h2", Name: "handler", QualifiedName: "b.handler", Kind: "Function", SourceFile: "b.go", StartLine: 20, EndLine: 22},
		},
	}
	srv := newTestServer(t, doc)

	// label is ambiguous → clarifier.
	res := callEndpointTool(t, srv.handleGetNodeSource, map[string]any{"group": "test", "node_id": "handler"})
	if amb, _ := res["ambiguous"].(bool); !amb {
		t.Fatalf("expected ambiguous=true clarifier, got %v", res)
	}
	matches, _ := res["matches"].([]any)
	if len(matches) != 2 {
		t.Errorf("expected 2 clarifier matches, got %d", len(matches))
	}

	// qualified_name uniquely resolves; the open will fail because the
	// fixture file doesn't exist on disk, but the error must NOT be the
	// "node not found" branch — we just want to prove the lookup matched.
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "node_id": "a.handler"}
	res2, err := srv.handleGetNodeSource(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res2 == nil {
		t.Fatal("nil result")
	}
	// Read the error text — qualified_name resolved past the not-found check.
	if res2.IsError {
		text := extractResultText(t, res2)
		if strings.Contains(text, "node not found") {
			t.Errorf("qualified_name lookup failed at the resolution stage: %s", text)
		}
	}
}

// scenario_c — endpoints path_contains filter narrows a 1000-row corpus to
// a handful of lines in terse mode.
func TestUX1650_EndpointsFilterAndTerse(t *testing.T) {
	ents := []graph.Entity{}
	for i := 0; i < 100; i++ {
		ents = append(ents, graph.Entity{
			ID:         "ep_" + strconvI(i),
			Name:       "GET /api/v1/widget/" + strconvI(i),
			Kind:       "http_endpoint_definition",
			SourceFile: "routes.go", StartLine: i + 1,
			Properties: map[string]string{"verb": "GET", "path": "/api/v1/widget/" + strconvI(i)},
		})
	}
	ents = append(ents, graph.Entity{
		ID:         "ep_proposal",
		Name:       "GET /api/v1/proposals/get_counts",
		Kind:       "http_endpoint_definition",
		SourceFile: "proposal_view.py", StartLine: 100,
		Properties: map[string]string{"verb": "GET", "path": "/api/v1/proposals/get_counts"},
	})
	doc := &graph.Document{Entities: ents}
	srv := newTestServer(t, doc)

	// path_contains="proposal" → 1 line, not 5,780.
	// #2288: terse default omits `definitions` — assert `count` and `lines`
	// reflect the filter. A second full-mode call verifies the struct array
	// is also size-1.
	res := callEndpointTool(t, srv.handleEndpoints, map[string]any{
		"group":         "test",
		"action":        "definitions",
		"path_contains": "proposal",
	})
	if _, has := res["definitions"]; has {
		t.Error("terse default should omit `definitions` (#2288)")
	}
	if count, _ := res["count"].(float64); count != 1 {
		t.Errorf("path_contains=proposal: want count=1, got %v", count)
	}
	lines, _ := res["lines"].([]any)
	if len(lines) != 1 {
		t.Errorf("expected 1 terse line, got %d", len(lines))
	}
	resFull := callEndpointTool(t, srv.handleEndpoints, map[string]any{
		"group":         "test",
		"action":        "definitions",
		"path_contains": "proposal",
		"format":        "full",
	})
	defs := getSlice(t, resFull, "definitions")
	if len(defs) != 1 {
		t.Errorf("path_contains=proposal (full): want 1, got %d", len(defs))
	}
	// One-line shape contains METHOD PATH file:line.
	if line, _ := lines[0].(string); !strings.Contains(line, "GET") || !strings.Contains(line, "/proposals/get_counts") {
		t.Errorf("terse line shape wrong: %q", line)
	}
}

// scenario_d — endpoint_stats folds cross-repo links into the orphan count.
func TestUX1650_EndpointStatsCrossRepoLinks(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "caller", Name: "fetchSomething", Kind: "http_endpoint_call"},
		},
		Relationships: []graph.Relationship{
			// caller fetches an entity that doesn't exist intra-repo.
			{FromID: "caller", ToID: "ext_unknown", Kind: "FETCHES"},
		},
	}
	srv := newTestServer(t, doc)
	// Inject a cross-repo link covering the caller.
	lg := srv.State.Group("test")
	if lg == nil {
		t.Fatal("no test group")
	}
	lg.Links = []CrossRepoLink{
		{Source: prefixedID("repo1", "caller"), Target: "other_repo::ext_unknown", Kind: "CALLS", Confidence: 1.0},
	}

	res := callEndpointTool(t, srv.handleEndpointStats, map[string]any{"group": "test"})
	totals := res["totals"].(map[string]any)
	if orphans := getFloat(t, totals, "orphan_calls"); orphans != 0 {
		t.Errorf("orphan_calls: want 0 (link resolves it), got %v", orphans)
	}
	if cross := getFloat(t, totals, "cross_repo_resolved"); cross != 1 {
		t.Errorf("cross_repo_resolved: want 1, got %v", cross)
	}
}

// scenario_e — find_paths walks from a mobile-call entity in repo A to a
// backend handler in repo B via a cross-repo link.
func TestUX1650_FindPathsCrossRepo(t *testing.T) {
	// Build a multi-repo group: repoA (mobile) and repoB (backend).
	docA := &graph.Document{
		Repo: "repoA",
		Entities: []graph.Entity{
			{ID: "mobile_call", Name: "useProposalCounts", Kind: "Function", SourceFile: "App.jsx"},
		},
	}
	docB := &graph.Document{
		Repo: "repoB",
		Entities: []graph.Entity{
			{ID: "backend_handler", Name: "get_counts", Kind: "Function", SourceFile: "views.py"},
		},
	}
	srv := newTestServer(t, docA, docB)
	lg := srv.State.Group("test")
	lg.Links = []CrossRepoLink{
		{
			Source: prefixedID("repoA", "mobile_call"),
			Target: prefixedID("repoB", "backend_handler"),
			Kind:   "FETCHES",
		},
	}

	res := callEndpointTool(t, srv.handleFindPaths, map[string]any{
		"group": "test",
		"from":  prefixedID("repoA", "mobile_call"),
		"to":    prefixedID("repoB", "backend_handler"),
	})
	if found, _ := res["found"].(bool); !found {
		t.Fatalf("expected cross-repo path, got: %v", res)
	}
	if crosses, _ := res["crosses_repos"].(bool); !crosses {
		t.Errorf("expected crosses_repos=true")
	}
	if hop, _ := res["hop_count"].(float64); hop != 1 {
		t.Errorf("expected 1 hop, got %v", hop)
	}
}

// scenario_g — every tool response carries elapsed_ms (added by wrap).
// We exercise wrap directly because the handler-level tests in this file
// call the handlers without the wrap middleware.
func TestUX1650_WrapInjectsElapsedMS(t *testing.T) {
	srv := newTestServer(t, &graph.Document{
		Entities: []graph.Entity{{ID: "x", Name: "x", Kind: "Function"}},
	})
	wrapped := srv.wrap("test_tool", srv.handleWhoami)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test"}
	res, err := wrapped(context.Background(), req)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	payload := extractResultJSON(t, res)
	if _, ok := payload["elapsed_ms"]; !ok {
		t.Errorf("expected elapsed_ms in tool payload, got keys: %v", keysOf(payload))
	}
}

// scenario_h — #1687: error responses also carry elapsed_ms so callers can
// measure latency even when the tool returns a validation or lookup error.
func TestUX1687_WrapInjectsElapsedMSOnError(t *testing.T) {
	srv := newTestServer(t, &graph.Document{
		Entities: []graph.Entity{{ID: "x", Name: "x", Kind: "Function"}},
	})
	// handleFindCallers requires entity_id; call with the wrong key so it
	// returns IsError=true immediately (before any graph lookup).
	wrapped := srv.wrap("archigraph_find_callers", srv.handleFindCallers)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "test", "node_id": "x"} // wrong key
	res, err := wrapped(context.Background(), req)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true for missing entity_id, got success")
	}
	// Error responses use a plain text trailer format, not JSON — use extractResultText.
	text := extractResultText(t, res)
	if !strings.Contains(text, "elapsed_ms=") {
		t.Errorf("expected elapsed_ms= trailer in error response, got: %q", text)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func callEndpointToolText(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) string {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res == nil || res.IsError {
		return ""
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func strconvI(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	n := 0
	for i > 0 {
		buf[n] = byte('0' + i%10)
		i /= 10
		n++
	}
	out := make([]byte, n)
	for k := 0; k < n; k++ {
		out[k] = buf[n-1-k]
	}
	return string(out)
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
