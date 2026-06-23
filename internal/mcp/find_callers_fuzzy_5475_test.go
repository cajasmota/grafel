package mcp

// find_callers_fuzzy_5475_test.go — #5475.
//
// Two distinct bugs in grafel_find_callers, shared root cause (incomplete
// cross-repo / unresolved-id handling):
//
//	BUG 1 (error rate): resolveEntityArg only resolved an exact hex id or an
//	  EXACT unique Name/QualifiedName. References that grafel_find resolves
//	  fuzzily (different case, a qualified-name suffix, a substring) hard-errored
//	  with "entity not found". The shared fuzzyMatchEntities fall-through (reused
//	  from grafel_find / handleSearchEntities) now rescues them.
//
//	BUG 2 (dropped callers): when a caller edge's FromID was an unresolved id
//	  (byID[id]==nil — the cross-repo linker rewrite gap), only file-ref and
//	  semantic edge kinds got a synthetic-caller fallback; CALLS-class edges were
//	  silently dropped, so find_callers returned N-1 callers. The fallback now
//	  covers every real inbound caller kind.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// --- BUG 1: fuzzy input resolution -----------------------------------------

// buildFuzzyResDoc: Caller --CALLS--> Target. Target's name is "Target" with
// qualified name "core.svc.Target".
func buildFuzzyResDoc() *graph.Document {
	return minDoc(
		[]graph.Entity{
			{ID: "ent-target", Name: "Target", QualifiedName: "core.svc.Target", Kind: "Function", SourceFile: "svc.py", StartLine: 10},
			{ID: "ent-caller", Name: "Caller", QualifiedName: "core.svc.Caller", Kind: "Function", SourceFile: "svc.py", StartLine: 20},
		},
		[]graph.Relationship{
			{FromID: "ent-caller", ToID: "ent-target", Kind: "CALLS"},
		},
	)
}

// TestFindCallers_ByDifferentCaseName: a name with different case ("target")
// is not an exact match but grafel_find resolves it; find_callers now does too.
func TestFindCallers_ByDifferentCaseName(t *testing.T) {
	srv := newTestServer(t, buildFuzzyResDoc())
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{"entity_id": "target"})
	if out["entity_name"] != "Target" {
		t.Fatalf("entity_name = %v, want Target (case-insensitive resolution)", out["entity_name"])
	}
	cs := callersOf(t, out)
	if len(cs) != 1 || cs[0].(map[string]any)["name"] != "Caller" {
		t.Fatalf("case-insensitive resolution lost the caller: %+v", cs)
	}
}

// TestFindCallers_ByQualifiedNameSuffix: a partial qualified name ("svc.Target")
// is not an exact Name/QualifiedName but is a substring grafel_find resolves.
func TestFindCallers_ByQualifiedNameSuffix(t *testing.T) {
	srv := newTestServer(t, buildFuzzyResDoc())
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{"entity_id": "svc.Target"})
	if out["entity_name"] != "Target" {
		t.Fatalf("entity_name = %v, want Target (qualified-name suffix resolution)", out["entity_name"])
	}
	if len(callersOf(t, out)) != 1 {
		t.Fatalf("qualified-name suffix resolution lost the caller: %+v", out)
	}
}

// TestFindCallers_FuzzyAmbiguous: a substring matching >1 entity returns a
// disambiguation envelope, NOT a bare error and NOT an arbitrary pick.
func TestFindCallers_FuzzyAmbiguous(t *testing.T) {
	doc := minDoc(
		[]graph.Entity{
			{ID: "ent-pa", Name: "PaymentService", QualifiedName: "core.pay.PaymentService", Kind: "Class", SourceFile: "pay.py", StartLine: 1},
			{ID: "ent-pb", Name: "PaymentGateway", QualifiedName: "core.pay.PaymentGateway", Kind: "Class", SourceFile: "pay.py", StartLine: 5},
		},
		[]graph.Relationship{},
	)
	srv := newTestServer(t, doc)
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{"entity_id": "Payment"})
	if out["ambiguous"] != true {
		t.Fatalf("expected ambiguous=true for fuzzy multi-match, got %+v", out)
	}
	cands, ok := out["candidates"].([]any)
	if !ok || len(cands) != 2 {
		t.Fatalf("expected 2 fuzzy candidates, got %T %+v", out["candidates"], out["candidates"])
	}
}

// TestFindCallers_FuzzyNonexistent: a probe matching nothing even fuzzily still
// errors verbatim (genuine not-found case preserved).
func TestFindCallers_FuzzyNonexistent(t *testing.T) {
	srv := newTestServer(t, buildFuzzyResDoc())
	got := callFlowToolError(t, srv.handleFindCallers, map[string]any{"entity_id": "zzzznope"})
	if got == "" {
		t.Fatalf("expected an error result for a genuine miss")
	}
}

// TestFindCallees_FuzzyNotRegressed: the shared resolver also powers
// find_callees; a fuzzy name there resolves too (no regression).
func TestFindCallees_FuzzyNotRegressed(t *testing.T) {
	srv := newTestServer(t, buildFuzzyResDoc())
	out := callFlowTool(t, srv.handleFindCallees, map[string]any{"entity_id": "caller"})
	callees, ok := out["callees"].([]any)
	if !ok || len(callees) != 1 || callees[0].(map[string]any)["name"] != "Target" {
		t.Fatalf("find_callees fuzzy resolution failed/regressed: %+v", out)
	}
}

// --- BUG 2: dropped callers on unresolved CALLS FromID ----------------------

// buildDroppedCallerDoc: Target has TWO callers — RealCaller (resolved) and an
// unresolved FromID "phantom/lib.py::caller" on a CALLS edge that the cross-repo
// linker never rewrote to a stamped entity id. Pre-fix find_callers returned
// only RealCaller (N-1).
func buildDroppedCallerDoc() *graph.Document {
	return minDoc(
		[]graph.Entity{
			{ID: "ent-target", Name: "Target", QualifiedName: "core.svc.Target", Kind: "Function", SourceFile: "svc.py", StartLine: 10},
			{ID: "ent-real", Name: "RealCaller", QualifiedName: "core.svc.RealCaller", Kind: "Function", SourceFile: "svc.py", StartLine: 20},
		},
		[]graph.Relationship{
			{FromID: "ent-real", ToID: "ent-target", Kind: "CALLS"},
			// Unresolved FromID — the source entity was never stamped (linker gap).
			{FromID: "phantom/lib/PhantomCaller", ToID: "ent-target", Kind: "CALLS"},
		},
	)
}

// TestFindCallers_UnresolvedCallsFromIDNotDropped: the unresolved-FromID CALLS
// caller now appears as a synthetic caller (#5475) instead of being silently
// dropped, so the count is the full 2.
func TestFindCallers_UnresolvedCallsFromIDNotDropped(t *testing.T) {
	srv := newTestServer(t, buildDroppedCallerDoc())
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{"entity_id": "ent-target"})
	cs := callersOf(t, out)
	if len(cs) != 2 {
		t.Fatalf("want 2 callers (resolved + synthetic unresolved-FromID), got %d: %+v", len(cs), cs)
	}
	names := map[string]bool{}
	for _, c := range cs {
		names[c.(map[string]any)["name"].(string)] = true
	}
	if !names["RealCaller"] {
		t.Fatalf("lost the resolved caller: %+v", names)
	}
	// Synthetic caller is named from the trailing path/id segment.
	if !names["PhantomCaller"] {
		t.Fatalf("unresolved-FromID CALLS caller was dropped (BUG 2 not fixed): %+v", names)
	}
}
