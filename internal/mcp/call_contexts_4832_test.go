package mcp

// call_contexts_4832_test.go — end-to-end test for the #4832 control-flow
// follow-up: stamping conditional/condition/in_loop on outbound CALLS edges,
// surfaced via archigraph_inspect include="call_contexts" (opt-in, default
// payload unchanged per #2828). Runs the REAL handleGetNode handler against
// small Python + Go fixtures in testdata/call_contexts_4832/. Mirrors part (a)'s
// effect_contexts_4821_test.go: it reuses the same enclosing-block classifier,
// so a guarded call is conditional+condition and an unconditional call is not.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// callContextsServer builds a server with a caller function entity plus three
// outbound CALLS edges (carrying "line" properties at the given lines), and
// points the repo at the testdata/call_contexts_4832 fixture dir.
func callContextsServer(t *testing.T, sourceFile string, start, end int, callerName string, callLines [3]int) *Server {
	t.Helper()
	doc := &graph.Document{
		Repo: "upvate-core",
		Entities: []graph.Entity{
			{
				ID: "op_sync", Name: callerName,
				Kind:          "SCOPE.Operation",
				QualifiedName: "svc." + callerName,
				SourceFile:    sourceFile, StartLine: start, EndLine: end,
			},
			{ID: "t_audit", Name: "log", Kind: "SCOPE.Operation", SourceFile: sourceFile, StartLine: 1},
			{ID: "t_notify", Name: "send", Kind: "SCOPE.Operation", SourceFile: sourceFile, StartLine: 1},
			{ID: "t_mailer", Name: "deliver", Kind: "SCOPE.Operation", SourceFile: sourceFile, StartLine: 1},
		},
		Relationships: []graph.Relationship{
			{FromID: "op_sync", ToID: "t_audit", Kind: "CALLS", Properties: map[string]string{"line": itoa(callLines[0])}},
			{FromID: "op_sync", ToID: "t_notify", Kind: "CALLS", Properties: map[string]string{"line": itoa(callLines[1])}},
			{FromID: "op_sync", ToID: "t_mailer", Kind: "CALLS", Properties: map[string]string{"line": itoa(callLines[2])}},
		},
	}
	srv := newTestServer(t, doc)
	abs, err := filepath.Abs(filepath.Join("testdata", "call_contexts_4832"))
	if err != nil {
		t.Fatalf("abs testdata: %v", err)
	}
	srv.State.mu.Lock()
	srv.State.groups["test"].Repos["upvate-core"].Path = abs
	srv.State.mu.Unlock()
	return srv
}

// inspectCalls runs handleGetNode and returns the "calls" array. `include` is
// passed through (empty for the default-payload assertion).
func inspectCalls(t *testing.T, srv *Server, entity, include string) []map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	args := map[string]any{"group": "test", "entity_id": entity}
	if include != "" {
		args["include"] = include
	}
	req.Params.Arguments = args
	res, err := srv.handleGetNode(context.Background(), req)
	if err != nil {
		t.Fatalf("handleGetNode: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("handleGetNode error: %+v", res)
	}
	decoded := extractResultJSON(t, res)
	raw, ok := decoded["calls"]
	if !ok {
		t.Fatalf("no calls in result: %v", decoded)
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("calls not an array: %T", raw)
	}
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func callByLine(calls []map[string]any, line int) map[string]any {
	for _, c := range calls {
		if ln, ok := c["line"].(float64); ok && int(ln) == line {
			return c
		}
	}
	return nil
}

// TestCallContexts_Python: audit.log top-level (unconditional), notifier.send
// under an `if` (conditional + condition), mailer.deliver inside a `for`
// (in_loop) — surfaced only when include=call_contexts.
func TestCallContexts_Python(t *testing.T) {
	// notify_service.py: sync spans 1-8; call sites at lines 2,4,6.
	srv := callContextsServer(t, "notify_service.py", 1, 8, "sync", [3]int{2, 4, 6})
	calls := inspectCalls(t, srv, "sync", "call_contexts")

	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d: %+v", len(calls), calls)
	}

	audit := callByLine(calls, 2)
	if audit == nil {
		t.Fatalf("no call at line 2; got %+v", calls)
	}
	if cond, _ := audit["conditional"].(bool); cond {
		t.Errorf("audit.log (line 2) should be unconditional, got %+v", audit)
	}

	notify := callByLine(calls, 4)
	if notify == nil {
		t.Fatalf("no call at line 4; got %+v", calls)
	}
	if cond, _ := notify["conditional"].(bool); !cond {
		t.Errorf("notifier.send (line 4) should be conditional, got %+v", notify)
	}
	if c, _ := notify["condition"].(string); c == "" {
		t.Errorf("notifier.send should carry guarding condition, got %+v", notify)
	}

	mailer := callByLine(calls, 6)
	if mailer == nil {
		t.Fatalf("no call at line 6; got %+v", calls)
	}
	if loop, _ := mailer["in_loop"].(bool); !loop {
		t.Errorf("mailer.deliver (line 6) should be in_loop, got %+v", mailer)
	}
	if cond, _ := mailer["conditional"].(bool); !cond {
		t.Errorf("mailer.deliver inside for-loop should be conditional, got %+v", mailer)
	}
}

// TestCallContexts_Go: same shape on a brace-dialect language.
func TestCallContexts_Go(t *testing.T) {
	// notify_service.go: Sync spans 3-12; call sites at lines 4,6,9.
	srv := callContextsServer(t, "notify_service.go", 3, 12, "Sync", [3]int{4, 6, 9})
	calls := inspectCalls(t, srv, "Sync", "call_contexts")

	audit := callByLine(calls, 4)
	if audit == nil {
		t.Fatalf("no call at line 4; got %+v", calls)
	}
	if cond, _ := audit["conditional"].(bool); cond {
		t.Errorf("audit.Log (line 4) should be unconditional, got %+v", audit)
	}

	notify := callByLine(calls, 6)
	if notify == nil {
		t.Fatalf("no call at line 6; got %+v", calls)
	}
	if cond, _ := notify["conditional"].(bool); !cond {
		t.Errorf("notifier.Send (line 6) should be conditional, got %+v", notify)
	}
	if c, _ := notify["condition"].(string); c == "" {
		t.Errorf("notifier.Send should carry guarding condition, got %+v", notify)
	}

	mailer := callByLine(calls, 9)
	if mailer == nil {
		t.Fatalf("no call at line 9; got %+v", calls)
	}
	if loop, _ := mailer["in_loop"].(bool); !loop {
		t.Errorf("mailer.Deliver (line 9) should be in_loop, got %+v", mailer)
	}
}

// TestCallContexts_DefaultPayloadUnchanged proves the facet is opt-in: without
// include=call_contexts, no call entry carries conditional/condition/in_loop,
// so the default inspect payload is byte-identical (#2828).
func TestCallContexts_DefaultPayloadUnchanged(t *testing.T) {
	srv := callContextsServer(t, "notify_service.py", 1, 8, "sync", [3]int{2, 4, 6})
	calls := inspectCalls(t, srv, "sync", "") // no include

	for _, c := range calls {
		if _, ok := c["conditional"]; ok {
			t.Errorf("default payload must not carry 'conditional'; got %+v", c)
		}
		if _, ok := c["condition"]; ok {
			t.Errorf("default payload must not carry 'condition'; got %+v", c)
		}
		if _, ok := c["in_loop"]; ok {
			t.Errorf("default payload must not carry 'in_loop'; got %+v", c)
		}
	}
}
