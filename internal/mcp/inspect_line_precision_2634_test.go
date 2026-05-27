package mcp

// inspect_line_precision_2634_test.go — line-precise CALLS / called_by edges
// on archigraph_inspect (#2634).
//
// Four test cases:
//  1. TestInspect_IncludesLinePrecision_OutboundCalls   — calls[].line
//  2. TestInspect_IncludesLinePrecision_InboundCallers  — called_by[].line
//  3. TestInspect_ContextSnippet_Reasonable             — called_by[].context (disk read)
//  4. TestInspect_BackwardCompat                        — legacy fields still present

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// callInspect is a thin wrapper that invokes handleGetNode and returns the
// decoded JSON map.  It fails the test on any handler or JSON error.
func callInspect(t *testing.T, srv *Server, entityID string) map[string]any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"group":     "test",
		"entity_id": entityID,
	}
	res, err := srv.handleGetNode(context.Background(), req)
	if err != nil {
		t.Fatalf("handleGetNode error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}
	return extractResultJSON(t, res)
}

// lineOfCall extracts calls[i].line (float64 via JSON round-trip) from out.
func lineOfCall(t *testing.T, out map[string]any, idx int) int {
	t.Helper()
	calls, ok := out["calls"]
	if !ok {
		t.Fatalf("missing 'calls' key in result; got %v", out)
	}
	arr, ok := calls.([]any)
	if !ok {
		t.Fatalf("'calls' is %T, want []any", calls)
	}
	if idx >= len(arr) {
		t.Fatalf("calls[%d] out of range (len=%d)", idx, len(arr))
	}
	m := arr[idx].(map[string]any)
	v, ok := m["line"]
	if !ok {
		t.Fatalf("calls[%d] missing 'line' field: %v", idx, m)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("calls[%d].line is %T, want float64", idx, v)
	}
	return int(f)
}

// lineOfCalledBy extracts called_by[i].line from out.
func lineOfCalledBy(t *testing.T, out map[string]any, idx int) int {
	t.Helper()
	calledBy, ok := out["called_by"]
	if !ok {
		t.Fatalf("missing 'called_by' key in result; got keys: %v", mapKeys(out))
	}
	arr, ok := calledBy.([]any)
	if !ok {
		t.Fatalf("'called_by' is %T, want []any", calledBy)
	}
	if idx >= len(arr) {
		t.Fatalf("called_by[%d] out of range (len=%d)", idx, len(arr))
	}
	m := arr[idx].(map[string]any)
	v, ok := m["line"]
	if !ok {
		t.Fatalf("called_by[%d] missing 'line' field: %v", idx, m)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("called_by[%d].line is %T, want float64", idx, v)
	}
	return int(f)
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestInspect_IncludesLinePrecision_OutboundCalls verifies that inspecting an
// entity with 2 outbound CALLS edges tagged with Properties["line"] surfaces
// those line numbers in calls[].line.
func TestInspect_IncludesLinePrecision_OutboundCalls(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "caller1", Name: "Caller", Kind: "SCOPE.Operation", SourceFile: "a.go", StartLine: 1},
			{ID: "callee1", Name: "TargetA", Kind: "SCOPE.Operation", SourceFile: "b.go", StartLine: 10},
			{ID: "callee2", Name: "TargetB", Kind: "SCOPE.Operation", SourceFile: "c.go", StartLine: 20},
		},
		Relationships: []graph.Relationship{
			{
				ID: "r1", FromID: "caller1", ToID: "callee1", Kind: "CALLS",
				Properties: map[string]string{"line": "5"},
			},
			{
				ID: "r2", FromID: "caller1", ToID: "callee2", Kind: "CALLS",
				Properties: map[string]string{"line": "12"},
			},
		},
	}
	srv := newTestServer(t, doc)
	out := callInspect(t, srv, "caller1")

	// Two outbound calls — lines 5 and 12.
	calls, ok := out["calls"]
	if !ok {
		t.Fatalf("inspect result missing 'calls' key; got keys: %v", mapKeys(out))
	}
	arr, ok := calls.([]any)
	if !ok {
		t.Fatalf("'calls' is %T, want []any", calls)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 calls entries, got %d: %v", len(arr), arr)
	}

	// Build a set of observed lines so order doesn't matter.
	got := map[int]bool{}
	for i := range arr {
		m := arr[i].(map[string]any)
		if v, ok := m["line"].(float64); ok {
			got[int(v)] = true
		}
	}
	for _, want := range []int{5, 12} {
		if !got[want] {
			t.Errorf("expected line %d in calls, got lines: %v", want, got)
		}
	}
}

// TestInspect_IncludesLinePrecision_InboundCallers verifies that inspecting
// entity X, which is called by A at line 7 and B at line 22, returns
// called_by entries with those line numbers.
func TestInspect_IncludesLinePrecision_InboundCallers(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "x", Name: "Target", Kind: "SCOPE.Operation", SourceFile: "x.go", StartLine: 1},
			{ID: "a", Name: "CallerA", Kind: "SCOPE.Operation", SourceFile: "a.go", StartLine: 1},
			{ID: "b", Name: "CallerB", Kind: "SCOPE.Operation", SourceFile: "b.go", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			{
				ID: "r1", FromID: "a", ToID: "x", Kind: "CALLS",
				Properties: map[string]string{"line": "7"},
			},
			{
				ID: "r2", FromID: "b", ToID: "x", Kind: "CALLS",
				Properties: map[string]string{"line": "22"},
			},
		},
	}
	srv := newTestServer(t, doc)
	out := callInspect(t, srv, "x")

	calledBy, ok := out["called_by"]
	if !ok {
		t.Fatalf("inspect result missing 'called_by' key; got keys: %v", mapKeys(out))
	}
	arr, ok := calledBy.([]any)
	if !ok {
		t.Fatalf("'called_by' is %T, want []any", calledBy)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 called_by entries, got %d: %v", len(arr), arr)
	}

	got := map[int]bool{}
	for i := range arr {
		m := arr[i].(map[string]any)
		if v, ok := m["line"].(float64); ok {
			got[int(v)] = true
		}
	}
	for _, want := range []int{7, 22} {
		if !got[want] {
			t.Errorf("expected line %d in called_by, got lines: %v", want, got)
		}
	}
}

// TestInspect_ContextSnippet_Reasonable writes a real source file to a temp
// dir so the context-snippet disk read has something to read, then verifies
// that called_by[].context is a non-empty substring of the call-site line.
func TestInspect_ContextSnippet_Reasonable(t *testing.T) {
	// Build a temp dir that acts as the repo root.
	repoRoot := t.TempDir()

	// Write the caller's source file (3 lines; call site is line 2).
	callerSrc := filepath.Join(repoRoot, "caller.go")
	srcLines := "func CallerFn() {\n\tclient.post(\"/api/v1/foo\")\n}\n"
	if err := os.WriteFile(callerSrc, []byte(srcLines), 0o644); err != nil {
		t.Fatalf("write caller source: %v", err)
	}

	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "target1", Name: "Target", Kind: "SCOPE.Operation", SourceFile: "target.go", StartLine: 1},
			{ID: "caller1", Name: "CallerFn", Kind: "SCOPE.Operation", SourceFile: "caller.go", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			{
				ID: "r1", FromID: "caller1", ToID: "target1", Kind: "CALLS",
				// line 2 is `\tclient.post("/api/v1/foo")`
				Properties: map[string]string{"line": "2"},
			},
		},
	}
	srv := newTestServer(t, doc)

	// Patch the LoadedRepo.Path so the context reader knows the repo root.
	srv.State.mu.Lock()
	lr := srv.State.groups["test"].Repos["repo1"]
	lr.Path = repoRoot
	srv.State.mu.Unlock()

	out := callInspect(t, srv, "target1")

	calledBy, ok := out["called_by"]
	if !ok {
		t.Fatalf("missing 'called_by'; keys: %v", mapKeys(out))
	}
	arr := calledBy.([]any)
	if len(arr) == 0 {
		t.Fatal("expected at least one called_by entry")
	}
	m := arr[0].(map[string]any)
	ctx, _ := m["context"].(string)
	if ctx == "" {
		t.Fatalf("context snippet is empty; expected substring of call-site line")
	}
	// The line is `\tclient.post("/api/v1/foo")` — trimmed and capped at 40 chars.
	// We just check it contains something identifiable from that line.
	if !containsAny(ctx, "client", "post", "api") {
		t.Errorf("context snippet %q does not contain expected call-site text", ctx)
	}
	// Verify it doesn't exceed 40 chars.
	if len(ctx) > 40 {
		t.Errorf("context snippet too long (%d chars, max 40): %q", len(ctx), ctx)
	}
	fmt.Printf("TestInspect_ContextSnippet_Reasonable: context=%q\n", ctx)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// TestInspect_BackwardCompat verifies that the new calls/called_by fields are
// purely additive: the legacy fields (target, target_path on calls and source,
// source_path on called_by) are still present, so consumers that only read
// those fields continue to work.
func TestInspect_BackwardCompat(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "entity1", Name: "MyFunc", Kind: "SCOPE.Operation", SourceFile: "x.go", StartLine: 3},
			{ID: "dep1", Name: "HelperA", Kind: "SCOPE.Operation", SourceFile: "helper.go", StartLine: 1},
			{ID: "caller1", Name: "Parent", Kind: "SCOPE.Operation", SourceFile: "p.go", StartLine: 1},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "entity1", ToID: "dep1", Kind: "CALLS"},
			{ID: "r2", FromID: "caller1", ToID: "entity1", Kind: "CALLS"},
		},
	}
	srv := newTestServer(t, doc)
	out := callInspect(t, srv, "entity1")

	// Legacy top-level entity fields must be present.
	for _, key := range []string{"id", "name", "kind", "file", "line"} {
		if _, ok := out[key]; !ok {
			t.Errorf("backward-compat: missing top-level field %q", key)
		}
	}

	// calls[] must have target and target_path.
	if calls, ok := out["calls"]; ok {
		arr := calls.([]any)
		for i, raw := range arr {
			m := raw.(map[string]any)
			if _, ok := m["target"]; !ok {
				t.Errorf("calls[%d] missing 'target'", i)
			}
			if _, ok := m["target_path"]; !ok {
				t.Errorf("calls[%d] missing 'target_path'", i)
			}
			// new field must also be present (even if zero).
			if _, ok := m["line"]; !ok {
				t.Errorf("calls[%d] missing new 'line' field", i)
			}
		}
	}

	// called_by[] must have source and source_path.
	if calledBy, ok := out["called_by"]; ok {
		arr := calledBy.([]any)
		for i, raw := range arr {
			m := raw.(map[string]any)
			if _, ok := m["source"]; !ok {
				t.Errorf("called_by[%d] missing 'source'", i)
			}
			if _, ok := m["source_path"]; !ok {
				t.Errorf("called_by[%d] missing 'source_path'", i)
			}
			// new fields must also be present.
			if _, ok := m["line"]; !ok {
				t.Errorf("called_by[%d] missing new 'line' field", i)
			}
			if _, ok := m["context"]; !ok {
				t.Errorf("called_by[%d] missing new 'context' field", i)
			}
		}
	}
}
