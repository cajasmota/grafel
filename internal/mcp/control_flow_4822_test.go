package mcp

// control_flow_4822_test.go — end-to-end test for the #4822 archigraph_control_flow
// tool (control-flow epic #4820 part b): an on-demand, not-persisted per-function
// CFG (nodes + edges + conditions + effects) for the flowchart view (#4819).
// Reuses the same small Python (Django-shaped) and TS (NestJS-shaped) fixtures as
// the part-(a) effect_contexts test (testdata/effect_contexts_4821/).

import (
	"encoding/json"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

func callControlFlow(t *testing.T, srv *Server, entity, detail string) map[string]any {
	t.Helper()
	args := map[string]any{"entity_id": entity, "group": "test"}
	if detail != "" {
		args["detail"] = detail
	}
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := srv.handleControlFlow(nil, req)
	if err != nil {
		t.Fatalf("handleControlFlow(%s, detail=%q): %v", entity, detail, err)
	}
	if res.IsError {
		t.Fatalf("handleControlFlow(%s) tool error: %+v", entity, res.Content)
	}
	var txt string
	for _, c := range res.Content {
		if tc, ok := c.(mcpapi.TextContent); ok {
			txt = tc.Text
		}
	}
	if txt == "" {
		t.Fatalf("handleControlFlow(%s) empty content", entity)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(txt), &out); err != nil {
		t.Fatalf("unmarshal control_flow result: %v\n%s", err, txt)
	}
	return out
}

func cfgNodeList(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	raw, ok := out["nodes"].([]any)
	if !ok {
		t.Fatalf("no nodes array: %v", out)
	}
	res := make([]map[string]any, 0, len(raw))
	for _, n := range raw {
		if m, ok := n.(map[string]any); ok {
			res = append(res, m)
		}
	}
	return res
}

func cfgEdgeKinds(t *testing.T, out map[string]any) map[string]bool {
	t.Helper()
	raw, ok := out["edges"].([]any)
	if !ok {
		t.Fatalf("no edges array: %v", out)
	}
	kinds := map[string]bool{}
	for _, e := range raw {
		if m, ok := e.(map[string]any); ok {
			if k, ok := m["kind"].(string); ok {
				kinds[k] = true
			}
		}
	}
	return kinds
}

func cfgShapes(nodes []map[string]any) map[string]int {
	out := map[string]int{}
	for _, n := range nodes {
		if s, ok := n["shape"].(string); ok {
			out[s]++
		}
	}
	return out
}

// TestControlFlow_Python: the on-demand CFG of the Django-shaped fixture has a
// decision node with a condition, a loop header + back-edge, a return terminal
// wired to exit, and an effect-annotated process node at detail=data.
func TestControlFlow_Python(t *testing.T) {
	srv := effectContextsTestServer(t, "sync_service.py", 1, 7)
	out := callControlFlow(t, srv, "sync", "data")

	if sup, _ := out["supported"].(bool); !sup {
		t.Fatalf("python CFG should be supported; got %v", out["supported"])
	}
	if cx, _ := out["cyclomatic_complexity"].(float64); cx < 3 {
		t.Errorf("cyclomatic_complexity = %v; want >= 3", out["cyclomatic_complexity"])
	}

	nodes := cfgNodeList(t, out)
	shapes := cfgShapes(nodes)
	if shapes["decision"] == 0 {
		t.Errorf("expected a decision node; shapes=%v", shapes)
	}
	if shapes["loop"] == 0 {
		t.Errorf("expected a loop node; shapes=%v", shapes)
	}
	if shapes["return"] == 0 {
		t.Errorf("expected a return terminal; shapes=%v", shapes)
	}
	if shapes["start"] != 1 || shapes["end"] != 1 {
		t.Errorf("want exactly one start+end; shapes=%v", shapes)
	}

	// Condition text on a decision node.
	sawCond := false
	for _, n := range nodes {
		if n["shape"] == "decision" {
			if c, _ := n["condition"].(string); c != "" {
				sawCond = true
			}
		}
	}
	if !sawCond {
		t.Errorf("decision node missing condition; nodes=%v", nodes)
	}

	// Effect annotation present at detail=data.
	sawEffect := false
	for _, n := range nodes {
		if _, ok := n["effects"]; ok {
			sawEffect = true
		}
	}
	if !sawEffect {
		t.Errorf("expected effect annotation at detail=data; nodes=%v", nodes)
	}

	kinds := cfgEdgeKinds(t, out)
	if !kinds["loop_back"] {
		t.Errorf("expected a loop_back edge; kinds=%v", kinds)
	}
	if !kinds["exit"] {
		t.Errorf("expected an exit edge from terminal; kinds=%v", kinds)
	}
}

// TestControlFlow_TS: same shape on the NestJS-flavoured TS fixture.
func TestControlFlow_TS(t *testing.T) {
	srv := effectContextsTestServer(t, "sync.service.ts", 2, 11)
	out := callControlFlow(t, srv, "sync", "data")

	if sup, _ := out["supported"].(bool); !sup {
		t.Fatalf("jsts CFG should be supported; got %v", out["supported"])
	}
	nodes := cfgNodeList(t, out)
	shapes := cfgShapes(nodes)
	if shapes["decision"] == 0 || shapes["loop"] == 0 || shapes["return"] == 0 {
		t.Errorf("expected decision+loop+return nodes; shapes=%v", shapes)
	}
	kinds := cfgEdgeKinds(t, out)
	if !kinds["loop_back"] {
		t.Errorf("expected a loop_back edge; kinds=%v", kinds)
	}
}

// TestControlFlow_DetailLevels: outline omits conditions+effects+labels; full
// includes labels. Proves the #2828 token-control parameter works.
func TestControlFlow_DetailLevels(t *testing.T) {
	srv := effectContextsTestServer(t, "sync_service.py", 1, 7)

	outline := cfgNodeList(t, callControlFlow(t, srv, "sync", "outline"))
	for _, n := range outline {
		if _, ok := n["condition"]; ok {
			t.Errorf("outline must omit condition; node=%v", n)
		}
		if _, ok := n["effects"]; ok {
			t.Errorf("outline must omit effects; node=%v", n)
		}
		if _, ok := n["label"]; ok {
			t.Errorf("outline must omit label; node=%v", n)
		}
	}

	full := cfgNodeList(t, callControlFlow(t, srv, "sync", "full"))
	sawLabel := false
	for _, n := range full {
		if l, _ := n["label"].(string); l != "" {
			sawLabel = true
		}
	}
	if !sawLabel {
		t.Errorf("full detail should carry node labels; nodes=%v", full)
	}
}

// TestControlFlow_NotFound: a bogus entity id returns a tool error.
func TestControlFlow_NotFound(t *testing.T) {
	srv := effectContextsTestServer(t, "sync_service.py", 1, 7)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"entity_id": "does_not_exist", "group": "test"}
	res, err := srv.handleControlFlow(nil, req)
	if err != nil {
		t.Fatalf("handleControlFlow err: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected a tool error for unknown entity, got %+v", res.Content)
	}
}
