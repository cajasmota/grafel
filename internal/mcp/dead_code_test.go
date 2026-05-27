package mcp

import (
	"strings"
	"testing"
)

// TestDeadCodeLiveRecompute exercises the live-recompute fallback
// path (sidecar absent). Validates the wire shape — including the new
// reachability summary fields — even when the fixture has no obvious
// entry-points.
func TestDeadCodeLiveRecompute(t *testing.T) {
	doc := buildDeadCodeDoc()
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleDeadCode, map[string]any{
		"limit": float64(100),
	})
	if _, ok := out["dead_code"]; !ok {
		t.Fatalf("response missing dead_code key: %v", out)
	}
	if _, ok := out["total_entities"]; !ok {
		t.Errorf("response missing total_entities")
	}
	if _, ok := out["reachable"]; !ok {
		t.Errorf("response missing reachable")
	}
	if _, ok := out["entry_points"]; !ok {
		t.Errorf("response missing entry_points")
	}
	src, _ := out["source"].(string)
	// Sidecar is unlikely to exist for the in-test group name, but
	// either path is valid output.
	if src != "live_recompute" && src != "sidecar" {
		t.Errorf("source should be live_recompute or sidecar, got %q", src)
	}
}

// TestDeadCodeFromEntry exercises the per-entry recompute path. A
// nonexistent entity should still return a valid (empty-ish)
// response, not a tool error.
func TestDeadCodeFromEntry(t *testing.T) {
	doc := buildDeadCodeDoc()
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleDeadCode, map[string]any{
		"from": "nonexistent-entity-id",
	})
	if src, _ := out["source"].(string); src != "from_entry" {
		t.Errorf("source should be from_entry, got %q", src)
	}
}

// TestDeadCodeKindFilter validates that the bare-kind filter
// case-insensitively restricts the dead list.
func TestDeadCodeKindFilter(t *testing.T) {
	doc := buildDeadCodeDoc()
	srv := newTestServer(t, doc)

	out := callFlowTool(t, srv.handleDeadCode, map[string]any{
		"kind_filter": "function",
	})
	dead, ok := out["dead_code"].([]any)
	if !ok {
		t.Fatalf("dead_code not an array: %T", out["dead_code"])
	}
	for _, item := range dead {
		m := item.(map[string]any)
		kind, _ := m["kind"].(string)
		if kind != "" && strings.ToLower(kind) != "function" {
			t.Errorf("kind_filter=function returned non-function: kind=%q", kind)
		}
	}
}

// TestMatchesBareKind exercises the bare-kind helper directly.
func TestMatchesBareKind(t *testing.T) {
	if !matchesBareKind("SCOPE.Function", "function") {
		t.Errorf("SCOPE.Function should match 'function'")
	}
	if matchesBareKind("SCOPE.Class", "function") {
		t.Errorf("SCOPE.Class should not match 'function'")
	}
	if !matchesBareKind("anything", "") {
		t.Errorf("empty filter should accept any kind")
	}
}

// TestIsLiveCodeKind verifies the framework-managed / non-callable
// kinds are excluded from dead-code analysis.
func TestIsLiveCodeKind(t *testing.T) {
	for _, kind := range []string{"SCOPE.Function", "SCOPE.Operation", "SCOPE.Class"} {
		if !isLiveCodeKind(kind) {
			t.Errorf("%q should be considered live-code", kind)
		}
	}
	for _, kind := range []string{"File", "Migration", "SCOPE.Stylesheet", "SCOPE.Schema", "SCOPE.DataAccess"} {
		if isLiveCodeKind(kind) {
			t.Errorf("%q should be excluded from dead-code analysis", kind)
		}
	}
}
