package mcp

// security_finding_promotion_test.go — proves the Phase-2 promotion pipeline
// for the grafel-security-audit skill (#2810).
//
// The skill's Phase 2 confirms a taint finding and then PROMOTES it into the
// group memory store as a first-class, queryable record via
// grafel_save_finding(type="security_finding", nodes=[entity_id], ...).
// This test injects a SYNTHETIC confirmed finding (request.body -> os.remove
// path-traversal), saves it through the real handler, and then verifies it is
// queryable in isolation via grafel_list_findings(type="security_finding")
// — without dragging along co-resident note findings.
//
// It is the unit-test stand-in for the manual "inject a real taint into a
// scratch fixture" check: it exercises save -> list -> type-filter end to end
// against the live handlers, with the memory dir pointed at a t.TempDir() so
// nothing touches the user's ~/.grafel store.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// callRawTool invokes a handler and returns the raw JSON text (handles both
// object and array result bodies — list_findings returns a JSON array).
func callRawTool(t *testing.T, fn func(context.Context, mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error), args map[string]any) string {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = args
	res, err := fn(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("handler returned nil result")
	}
	if res.IsError {
		t.Fatalf("handler returned tool error: %v", res.Content)
	}
	return extractResultText(t, res)
}

func TestSecurityFindingPromotionRoundtrip(t *testing.T) {
	// A tiny graph with the entity the synthetic finding points at, so the
	// nodes reference is a real ID the skill could resolve later.
	doc := minDoc([]graph.Entity{
		{ID: "ent_send_proposals", Name: "send_proposals", Kind: "function"},
	}, nil)
	srv := newTestServer(t, doc)

	// Point the memory store at a temp dir so we never touch ~/.grafel.
	memDir := t.TempDir()
	// #6114: State.Group returns an immutable per-call VIEW, so writing through
	// it would be silently discarded. Mutate the live group under s.mu.
	srv.State.mu.Lock()
	srv.State.groups["test"].MemoryDir = memDir
	srv.State.mu.Unlock()

	// 1) Save a benign note first — it must NOT show up in the security query.
	noteOut := callRawTool(t, srv.handleSaveResult, map[string]any{
		"group":    "test",
		"question": "Why does the inspector card fail to navigate?",
		"answer":   "Stale buildingId from a persisted-store migration.",
		// no type => defaults to "note"
	})
	if !json.Valid([]byte(noteOut)) {
		t.Fatalf("save note: invalid JSON result: %s", noteOut)
	}

	// 2) PROMOTE a confirmed taint finding as a first-class security_finding.
	//    This is exactly the call the skill's Phase-2 prompt should make.
	secOut := callRawTool(t, srv.handleSaveResult, map[string]any{
		"group":    "test",
		"question": "path_traversal on send_proposals",
		"answer":   "SEVERITY=high. Tainted input from request.body reaches os.remove without a sanitizer.",
		"type":     "security_finding",
		"nodes":    []any{"ent_send_proposals"},
	})
	var saved map[string]any
	if err := json.Unmarshal([]byte(secOut), &saved); err != nil {
		t.Fatalf("save security_finding: unmarshal failed: %v\nraw: %s", err, secOut)
	}
	if saved["path"] == nil || saved["path"] == "" {
		t.Fatalf("save security_finding: expected a non-empty path, got %v", saved["path"])
	}

	// 3) Query ONLY the security findings. The note must be excluded.
	listOut := callRawTool(t, srv.handleListFindings, map[string]any{
		"group": "test",
		"type":  "security_finding",
	})
	var items []map[string]any
	if err := json.Unmarshal([]byte(listOut), &items); err != nil {
		t.Fatalf("list_findings(type=security_finding): unmarshal failed: %v\nraw: %s", err, listOut)
	}
	if len(items) != 1 {
		t.Fatalf("type-filtered list: got %d findings, want exactly 1 (the promoted finding)\nraw: %s", len(items), listOut)
	}
	got := items[0]
	if got["type"] != "security_finding" {
		t.Errorf("promoted finding type: got %v, want security_finding", got["type"])
	}
	if got["question"] != "path_traversal on send_proposals" {
		t.Errorf("promoted finding question: got %v", got["question"])
	}
	// The entity reference must survive the roundtrip so the finding is
	// graph-queryable by entity (grafel_list_findings entity_id=...).
	nodes, ok := got["nodes"].([]any)
	if !ok || len(nodes) != 1 || nodes[0] != "ent_send_proposals" {
		t.Errorf("promoted finding nodes: got %v, want [ent_send_proposals]", got["nodes"])
	}

	// 4) Sanity: the unfiltered list contains BOTH records, proving the type
	//    filter is what isolated the security finding (not an empty store).
	allOut := callRawTool(t, srv.handleListFindings, map[string]any{"group": "test"})
	var all []map[string]any
	if err := json.Unmarshal([]byte(allOut), &all); err != nil {
		t.Fatalf("list_findings(all): unmarshal failed: %v\nraw: %s", err, allOut)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered list: got %d findings, want 2 (note + security_finding)", len(all))
	}

	// 5) The promoted finding is also reachable by entity_id — the acceptance
	//    criterion "queryable via grafel_list_findings".
	byEntOut := callRawTool(t, srv.handleListFindings, map[string]any{
		"group":     "test",
		"entity_id": "ent_send_proposals",
	})
	var byEnt []map[string]any
	if err := json.Unmarshal([]byte(byEntOut), &byEnt); err != nil {
		t.Fatalf("list_findings(entity_id): unmarshal failed: %v\nraw: %s", err, byEntOut)
	}
	if len(byEnt) != 1 || byEnt[0]["question"] != "path_traversal on send_proposals" {
		t.Fatalf("entity_id query: got %v, want the promoted security finding", byEnt)
	}
}

// TestFindingsOfTypeNoteFallback documents the normalisation: a stored finding
// with an empty Type is treated as "note", so legacy un-typed findings remain
// queryable via list_findings(type="note") and never leak into a
// security_finding query.
func TestFindingsOfTypeNoteFallback(t *testing.T) {
	in := []Finding{
		{Question: "legacy", Type: ""},
		{Question: "explicit-note", Type: "note"},
		{Question: "sec", Type: "security_finding"},
	}
	notes := findingsOfType(in, "note")
	if len(notes) != 2 {
		t.Fatalf("findingsOfType(note): got %d, want 2 (empty-type normalises to note)", len(notes))
	}
	sec := findingsOfType(in, "security_finding")
	if len(sec) != 1 || sec[0].Question != "sec" {
		t.Fatalf("findingsOfType(security_finding): got %v, want the one sec finding", sec)
	}
	if got := findingsOfType(in, ""); len(got) != 3 {
		t.Fatalf("findingsOfType(empty): got %d, want all 3 (no-op)", len(got))
	}
}
