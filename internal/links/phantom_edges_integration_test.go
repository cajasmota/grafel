package links

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/graph"
)

// TestPhantomEdges_CrossRepoBFSChain is an end-to-end integration test
// (#769) that exercises the full pipeline:
//
//  1. Two synthetic graph.Documents (fixture-b, fixture-a) connected by a
//     cross-repo HTTP CALLS link.
//  2. PromoteToPhantomEdges injects a phantom CALLS edge into fixture-b's
//     document.
//  3. engine.RunProcessFlow on fixture-b produces a cross_stack=true Process
//     entity whose chain ends at the phantom terminal (a handler in fixture-a).
//
// The test asserts:
//   - At least one phantom edge was added to fixture-b.
//   - At least one SCOPE.Process entity in fixture-b has cross_stack=true.
//   - That Process entity's cross_stack_reason mentions "phantom".
//   - That Process entity has terminal_is_phantom=true.
//   - No SCOPE.Process in fixture-b is incorrectly labeled cross_stack=true
//     for the intra-repo chain (the pure CALLS-only chain within fixture-b).
//
// This covers the "fixture-b/c → fixture-a" chain scenario described in
// issue #769 acceptance criteria.
func TestPhantomEdges_CrossRepoBFSChain(t *testing.T) {
	// ---- Fixture-a (producer) ----
	// Has a handler entity that the HTTP link targets.
	docA := &graph.Document{
		Repo: "fixture-a",
		Entities: []graph.Entity{
			{ID: "handler1", Name: "getUsersHandler", Kind: "SCOPE.Function", Language: "python", SourceFile: "api.py"},
		},
	}

	// ---- Fixture-b (consumer) ----
	// Has a consumer call chain: entry → svc → (phantom edge to fixture-a).
	// The consumer endpoint synthetic is the bridge the link pass matched on.
	docB := &graph.Document{
		Repo: "fixture-b",
		Entities: []graph.Entity{
			{ID: "entry", Name: "loadUsers", Kind: "SCOPE.Function", Language: "ts", SourceFile: "users.ts"},
			{ID: "svc", Name: "fetchFromAPI", Kind: "SCOPE.Function", Language: "ts", SourceFile: "users.ts"},
			// An intra-b chain that should NOT be cross_stack.
			{ID: "entry2", Name: "localOp", Kind: "SCOPE.Function", Language: "ts", SourceFile: "local.ts"},
			{ID: "dep1", Name: "doSomething", Kind: "SCOPE.Function", Language: "ts", SourceFile: "local.ts"},
			{ID: "dep2", Name: "helper", Kind: "SCOPE.Function", Language: "ts", SourceFile: "local.ts"},
			{ID: "dep3", Name: "finish", Kind: "SCOPE.Function", Language: "ts", SourceFile: "local.ts"},
		},
		Relationships: []graph.Relationship{
			// Consumer CALLS chain (no phantom yet).
			{ID: "r1", FromID: "entry", ToID: "svc", Kind: "CALLS"},
			// Intra-b chain (should remain cross_stack=false after phantom pass).
			{ID: "r2", FromID: "entry2", ToID: "dep1", Kind: "CALLS"},
			{ID: "r3", FromID: "dep1", ToID: "dep2", Kind: "CALLS"},
			{ID: "r4", FromID: "dep2", ToID: "dep3", Kind: "CALLS"},
		},
	}

	// ---- Cross-repo Link (as produced by the HTTP pass) ----
	// Source: fixture-b::svc (the caller in fixture-b)
	// Target: fixture-a::handler1 (the handler in fixture-a)
	crossLinks := []Link{
		{
			ID:       "http1",
			Source:   entityKey("fixture-b", "svc"),
			Target:   entityKey("fixture-a", "handler1"),
			Relation: RelationCalls,
			Method:   MethodHTTP,
		},
	}

	docs := map[string]*graph.Document{
		"fixture-a": docA,
		"fixture-b": docB,
	}

	// ---- Step 1: promote phantom edges ----
	added, err := PromoteToPhantomEdges(crossLinks, docs, "test-group")
	if err != nil {
		t.Fatalf("PromoteToPhantomEdges: %v", err)
	}
	if added != 1 {
		t.Errorf("phantom edges added = %d, want 1", added)
	}

	// Verify phantom edge is in docB.
	var phantomRel *graph.Relationship
	for i := range docB.Relationships {
		r := &docB.Relationships[i]
		if r.PropLen() > 0 && r.PropGet("cross_repo") == "true" {
			phantomRel = r
			break
		}
	}
	if phantomRel == nil {
		t.Fatal("expected phantom edge in fixture-b, found none")
	}
	if phantomRel.FromID != "svc" {
		t.Errorf("phantom edge FromID = %q, want svc", phantomRel.FromID)
	}
	if phantomRel.ToID != "handler1" {
		t.Errorf("phantom edge ToID = %q, want handler1", phantomRel.ToID)
	}
	if phantomRel.PropGet("target_repo") != "fixture-a" {
		t.Errorf("target_repo = %q, want fixture-a", phantomRel.PropGet("target_repo"))
	}

	// ---- Step 2: run process flow on fixture-b ----
	engine.RunProcessFlow(docB, engine.DefaultProcessFlowConfig())

	// Collect Process entities.
	type proc struct {
		name              string
		crossStack        bool
		crossStackReason  string
		terminalIsPhantom bool
	}
	var procs []proc
	for _, e := range docB.Entities {
		if e.Kind != engine.EntityKindProcess {
			continue
		}
		p := proc{
			name:       e.Name,
			crossStack: e.PropGet("cross_stack") == "true",
		}
		p.crossStackReason = e.PropGet("cross_stack_reason")
		p.terminalIsPhantom = e.PropGet("terminal_is_phantom") == "true"
		procs = append(procs, p)
	}
	if len(procs) == 0 {
		t.Fatal("RunProcessFlow emitted no Process entities in fixture-b")
	}

	// Sort for determinism in output.
	sort.Slice(procs, func(i, j int) bool { return procs[i].name < procs[j].name })

	// At least one cross_stack process must exist (the b→a chain).
	var crossProc *proc
	for i := range procs {
		if procs[i].crossStack {
			crossProc = &procs[i]
			break
		}
	}
	if crossProc == nil {
		var names []string
		for _, p := range procs {
			names = append(names, p.name)
		}
		t.Fatalf("no cross_stack=true Process found; processes: %s", strings.Join(names, ", "))
	}
	if crossProc.crossStackReason == "" {
		t.Errorf("cross_stack Process %q: cross_stack_reason is empty", crossProc.name)
	}
	if !strings.Contains(crossProc.crossStackReason, "phantom") {
		t.Errorf("cross_stack_reason %q should contain 'phantom'", crossProc.crossStackReason)
	}
	if !crossProc.terminalIsPhantom {
		t.Errorf("cross_stack Process %q: terminal_is_phantom should be true", crossProc.name)
	}

	// The intra-b chain (localOp→doSomething→helper→finish) must NOT be cross_stack.
	for _, p := range procs {
		if strings.Contains(p.name, "localOp") || strings.Contains(p.name, "doSomething") {
			if p.crossStack {
				t.Errorf("intra-b Process %q should have cross_stack=false, got true", p.name)
			}
		}
	}

	crossCount := 0
	for _, p := range procs {
		if p.crossStack {
			crossCount++
		}
	}
	t.Logf("fixture-b processes: %d total, cross_stack=%d", len(procs), crossCount)
	t.Logf("cross_stack reason: %q", crossProc.crossStackReason)
}
