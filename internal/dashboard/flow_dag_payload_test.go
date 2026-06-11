// flow_dag_payload_test.go — tests for the server-side FlowDag payload (#4363).
//
// These assert the backend build produces a well-formed v2DownstreamDAGResponse
// equivalent to what the retired client-side flowToDagPayload adapter produced
// for the same inputs: stable "flow-step-<index>" node ids, the real branch
// edges from branches_dag (not just the linear chain), first-class roles,
// terminal sinks, and the fan-out truncation flag.
package dashboard

import (
	"encoding/json"
	"testing"
)

func mkStep(idx int, name, kind, entityKind, edgeKind string) AnnotatedStep {
	return AnnotatedStep{
		EntityID:   "repo::e" + itoa(idx),
		Name:       name,
		Label:      name,
		Repo:       "repo",
		StepIndex:  idx,
		EdgeKind:   edgeKind,
		EntityKind: entityKind,
		StepKind:   kind,
	}
}

func TestBuildFlowDagPayload_NoSteps_ReturnsNil(t *testing.T) {
	if p := buildFlowDagPayload(nil, "", "label", "internal"); p != nil {
		t.Fatalf("expected nil for empty steps, got %+v", p)
	}
}

func TestBuildFlowDagPayload_LinearChain(t *testing.T) {
	steps := []AnnotatedStep{
		mkStep(0, "handler", StepKindFunctionCall, "SCOPE.Function", ""),
		mkStep(1, "service", StepKindFunctionCall, "SCOPE.Operation", "CALLS"),
		mkStep(2, "saveUser", StepKindDBWrite, "SCOPE.Operation", "PUBLISHES_TO"),
	}
	p := buildFlowDagPayload(steps, "", "GET /users", "http_handler")
	if p == nil {
		t.Fatal("expected payload, got nil")
	}
	if p.RootID != "flow-step-0" {
		t.Fatalf("root_id = %q, want flow-step-0", p.RootID)
	}
	if len(p.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(p.Nodes))
	}
	if len(p.Edges) != 2 {
		t.Fatalf("edges = %d, want 2 (linear)", len(p.Edges))
	}
	// Entry HTTP handler → endpoint role.
	if p.Nodes[0].Role != "endpoint" {
		t.Fatalf("entry role = %q, want endpoint", p.Nodes[0].Role)
	}
	// db_write sink → collection role + terminal.
	last := p.Nodes[2]
	if last.Role != "collection" {
		t.Fatalf("sink role = %q, want collection", last.Role)
	}
	if !last.Terminal {
		t.Fatal("sink should be terminal")
	}
	// db_write is an observable effect → effects badge.
	if len(last.Effects) != 1 || last.Effects[0] != StepKindDBWrite {
		t.Fatalf("sink effects = %v, want [db_write]", last.Effects)
	}
	// Edge into the QUERIES sink maps to JOINS_COLLECTION.
	var joins bool
	for _, e := range p.Edges {
		if e.To == "flow-step-2" && e.Kind == "JOINS_COLLECTION" {
			joins = true
		}
	}
	if !joins {
		t.Fatalf("expected JOINS_COLLECTION edge into sink, edges=%+v", p.Edges)
	}
}

func TestBuildFlowDagPayload_BranchingDag(t *testing.T) {
	// 0 → {1, 2}; 1 → 3. The linear chain would emit 0→1→2→3; the branches_dag
	// must instead carry the real fan-out (0→1, 0→2, 1→3).
	steps := []AnnotatedStep{
		mkStep(0, "root", StepKindFunctionCall, "SCOPE.Function", ""),
		mkStep(1, "armA", StepKindFunctionCall, "SCOPE.Operation", "CALLS"),
		mkStep(2, "armB", StepKindFunctionCall, "SCOPE.Operation", "CALLS"),
		mkStep(3, "leaf", StepKindDBQuery, "SCOPE.Operation", "QUERIES"),
	}
	dag := map[string]any{
		"step_index": 0, "entity_id": "repo::e0",
		"branches": []map[string]any{
			{"step_index": 1, "entity_id": "repo::e1",
				"branches": []map[string]any{
					{"step_index": 3, "entity_id": "repo::e3"},
				}},
			{"step_index": 2, "entity_id": "repo::e2"},
		},
	}
	raw, _ := json.Marshal(dag)
	p := buildFlowDagPayload(steps, string(raw), "branchy", "internal")
	if p == nil {
		t.Fatal("expected payload")
	}
	want := map[string]bool{
		"flow-step-0|flow-step-1": false,
		"flow-step-0|flow-step-2": false,
		"flow-step-1|flow-step-3": false,
	}
	if len(p.Edges) != len(want) {
		t.Fatalf("edges = %d, want %d: %+v", len(p.Edges), len(want), p.Edges)
	}
	for _, e := range p.Edges {
		k := e.From + "|" + e.To
		if _, ok := want[k]; !ok {
			t.Fatalf("unexpected edge %q (edges=%+v)", k, p.Edges)
		}
		want[k] = true
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("missing expected edge %q", k)
		}
	}
	// One fan-out point (node 0 has out-degree 2).
	if p.BranchCount != 1 {
		t.Fatalf("branch_count = %d, want 1", p.BranchCount)
	}
	// Non-entry, non-sink internal entry step → handler role.
	if p.Nodes[0].Role != "handler" {
		t.Fatalf("internal entry role = %q, want handler", p.Nodes[0].Role)
	}
}

func TestBuildFlowDagPayload_FanoutCapFlag(t *testing.T) {
	steps := []AnnotatedStep{
		mkStep(0, "root", StepKindFunctionCall, "SCOPE.Function", ""),
		mkStep(1, "armA", StepKindFunctionCall, "SCOPE.Operation", "CALLS"),
	}
	dag := map[string]any{
		"step_index": 0, "entity_id": "repo::e0",
		"branches": []map[string]any{
			{"step_index": 1, "entity_id": "repo::e1"},
			{"step_index": 99, "entity_id": "__overflow__", "reason": "fanout_cap"},
		},
	}
	raw, _ := json.Marshal(dag)
	p := buildFlowDagPayload(steps, string(raw), "capped", "internal")
	if !p.Truncation.FanoutTruncated {
		t.Fatal("expected fanout_truncated = true when a fanout_cap sentinel is present")
	}
	// The sentinel carries no real step → it must NOT produce an edge.
	if len(p.Edges) != 1 {
		t.Fatalf("edges = %d, want 1 (sentinel skipped)", len(p.Edges))
	}
}

func TestBuildFlowDagPayload_InconsistentDagFallsBackToLinear(t *testing.T) {
	// branches_dag references a step index (7) the steps slice doesn't carry →
	// the build must fall back to the linear chain, never emit a dangling edge.
	steps := []AnnotatedStep{
		mkStep(0, "root", StepKindFunctionCall, "SCOPE.Function", ""),
		mkStep(1, "next", StepKindFunctionCall, "SCOPE.Operation", "CALLS"),
	}
	dag := map[string]any{
		"step_index": 0, "entity_id": "repo::e0",
		"branches": []map[string]any{
			{"step_index": 7, "entity_id": "repo::e7"},
		},
	}
	raw, _ := json.Marshal(dag)
	p := buildFlowDagPayload(steps, string(raw), "bad", "internal")
	if len(p.Edges) != 1 || p.Edges[0].From != "flow-step-0" || p.Edges[0].To != "flow-step-1" {
		t.Fatalf("expected linear fallback edge 0→1, got %+v", p.Edges)
	}
}
