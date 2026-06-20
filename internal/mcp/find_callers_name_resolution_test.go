package mcp

// find_callers_name_resolution_test.go — #5314.
//
// grafel_find_callers / find_callees / neighbors used to accept ONLY the opaque
// "<repo>::<hash>" entity_id and hard-errored ("entity not found") on the
// name / qualified-name forms agents naturally pass — a ~35% error rate on
// name-based calls even when the entity clearly exists. The shared
// resolveEntityArg path now resolves by exact id → unique name/qualified_name →
// disambiguation candidates when ambiguous, and only the genuine miss errors.
//
// These tests pin all five branches on a small fixture graph.

import (
	"context"
	"strings"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"

	"github.com/cajasmota/grafel/internal/graph"
)

// buildNameResDoc builds: caller --CALLS--> target, plus two homonym entities
// (DupName) so the ambiguity branch is exercisable.
//
//	core.svc.Caller         (Caller)   --CALLS--> core.svc.Target (Target)
//	core.a.DupName / core.b.DupName    (two entities, same Name "DupName")
func buildNameResDoc() *graph.Document {
	return minDoc(
		[]graph.Entity{
			{ID: "ent-target", Name: "Target", QualifiedName: "core.svc.Target", Kind: "Function", SourceFile: "svc.py", StartLine: 10},
			{ID: "ent-caller", Name: "Caller", QualifiedName: "core.svc.Caller", Kind: "Function", SourceFile: "svc.py", StartLine: 20},
			{ID: "ent-dup-a", Name: "DupName", QualifiedName: "core.a.DupName", Kind: "Function", SourceFile: "a.py", StartLine: 1},
			{ID: "ent-dup-b", Name: "DupName", QualifiedName: "core.b.DupName", Kind: "Function", SourceFile: "b.py", StartLine: 1},
		},
		[]graph.Relationship{
			{FromID: "ent-caller", ToID: "ent-target", Kind: "CALLS"},
		},
	)
}

func callersOf(t *testing.T, out map[string]any) []any {
	t.Helper()
	c, ok := out["callers"].([]any)
	if !ok {
		t.Fatalf("expected callers array, got %T (%+v)", out["callers"], out)
	}
	return c
}

// TestFindCallers_ByHexID is the regression guard: the exact local id behaves
// exactly as today and is the reference result the name-based cases must match.
func TestFindCallers_ByHexID(t *testing.T) {
	srv := newTestServer(t, buildNameResDoc())
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{"entity_id": "ent-target"})
	if out["entity_name"] != "Target" {
		t.Fatalf("entity_name = %v, want Target", out["entity_name"])
	}
	cs := callersOf(t, out)
	if len(cs) != 1 {
		t.Fatalf("want 1 caller, got %d: %+v", len(cs), cs)
	}
	if got := cs[0].(map[string]any)["name"]; got != "Caller" {
		t.Fatalf("caller name = %v, want Caller", got)
	}
}

// TestFindCallers_ByBareName: a bare Name that uniquely matches resolves and
// returns the same result as the hex id — the core ~35%-error-rate fix.
func TestFindCallers_ByBareName(t *testing.T) {
	srv := newTestServer(t, buildNameResDoc())
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{"entity_id": "Target"})
	if out["entity_name"] != "Target" {
		t.Fatalf("entity_name = %v, want Target", out["entity_name"])
	}
	cs := callersOf(t, out)
	if len(cs) != 1 || cs[0].(map[string]any)["name"] != "Caller" {
		t.Fatalf("bare-name result != hex-id result: %+v", cs)
	}
}

// TestFindCallers_ByQualifiedName: a fully qualified name resolves too.
func TestFindCallers_ByQualifiedName(t *testing.T) {
	srv := newTestServer(t, buildNameResDoc())
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{"entity_id": "core.svc.Target"})
	if out["entity_name"] != "Target" {
		t.Fatalf("entity_name = %v, want Target", out["entity_name"])
	}
	if len(callersOf(t, out)) != 1 {
		t.Fatalf("qualified-name resolution lost the caller: %+v", out)
	}
}

// TestFindCallers_AmbiguousName: a Name shared by 2 entities returns a
// disambiguation envelope (candidates), NOT a bare error.
func TestFindCallers_AmbiguousName(t *testing.T) {
	srv := newTestServer(t, buildNameResDoc())
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{"entity_id": "DupName"})
	if out["ambiguous"] != true {
		t.Fatalf("expected ambiguous=true, got %+v", out)
	}
	cands, ok := out["candidates"].([]any)
	if !ok || len(cands) != 2 {
		t.Fatalf("expected 2 candidates, got %T %+v", out["candidates"], out["candidates"])
	}
	// Candidates must carry precise entity_ids the agent can re-issue against.
	ids := map[string]bool{}
	for _, c := range cands {
		ids[c.(map[string]any)["entity_id"].(string)] = true
	}
	if !ids["repo1::ent-dup-a"] || !ids["repo1::ent-dup-b"] {
		t.Fatalf("candidate entity_ids missing precise ids: %+v", ids)
	}
}

// TestFindCallers_NonexistentName: a genuine miss still errors verbatim.
func TestFindCallers_NonexistentName(t *testing.T) {
	srv := newTestServer(t, buildNameResDoc())
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"entity_id": "NoSuchEntity"}
	res, err := srv.handleFindCallers(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected entity-not-found error result, got %+v", res)
	}
	if txt := extractResultText(t, res); !strings.Contains(txt, "entity not found") {
		t.Fatalf("expected 'entity not found', got %q", txt)
	}
}

// TestNeighbors_ByBareName: the shared resolver also lights up grafel_neighbors
// (direction=in delegates to find_callers).
func TestNeighbors_ByBareName(t *testing.T) {
	srv := newTestServer(t, buildNameResDoc())
	out := callFlowTool(t, srv.handleNeighbors, map[string]any{"entity_id": "Target", "direction": "in"})
	if len(callersOf(t, out)) != 1 {
		t.Fatalf("neighbors(in) by name lost the caller: %+v", out)
	}
}

// TestNeighbors_Both_ByQualifiedName: direction=both merges callers + callees,
// both resolved from the qualified name.
func TestNeighbors_Both_ByQualifiedName(t *testing.T) {
	srv := newTestServer(t, buildNameResDoc())
	out := callFlowTool(t, srv.handleNeighbors, map[string]any{"entity_id": "core.svc.Caller", "direction": "both"})
	// Caller CALLS Target → callees should include Target.
	callees, ok := out["callees"].([]any)
	if !ok || len(callees) != 1 {
		t.Fatalf("neighbors(both) by qualified name lost callees: %+v", out)
	}
	if callees[0].(map[string]any)["name"] != "Target" {
		t.Fatalf("callee name = %v, want Target", callees[0].(map[string]any)["name"])
	}
}

// TestFindCallees_ByBareName: find_callees gains name resolution too.
func TestFindCallees_ByBareName(t *testing.T) {
	srv := newTestServer(t, buildNameResDoc())
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{"entity_id": "Caller"})
	callees, ok := out["callees"].([]any)
	if !ok || len(callees) != 1 || callees[0].(map[string]any)["name"] != "Target" {
		t.Fatalf("find_callees by name failed: %+v", out)
	}
}
