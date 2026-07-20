// process_flow_xrepo_1893_test.go — Wave-1 fix for issue #1893.
//
// Owner observation: a cross-stack Process flow titled "frontendFunc →
// repo-d:<cross-repo>" had ALL its steps in the frontend repo and ZERO
// steps in the backend repo. The flow terminated at the HTTP CALL SITE
// (a phantom cross-repo CALLS edge target) instead of continuing into
// the backend handler that resolves the route.
//
// Why this happened: RunProcessFlow walked ONE document at a time. The
// phantom edge target ID lives in the OTHER repo's Entities slice; with
// no companion adjacency the BFS could not follow into the backend.
//
// Fix (#1893): RunProcessFlowWithCompanions unifies adjacency + entity
// index across all provided docs. The BFS now traverses the phantom
// edge AND continues into the backend handler's CALLS chain.
//
// Tests in this file assert the cross-repo extension by construction
// (no live graph required):
//
//   - TestProcessFlow_1893_CrossRepoExtension_Extends: with companions,
//     the flow extends past the phantom edge into the backend handler.
//   - TestProcessFlow_1893_NoCompanions_StillTerminates: without
//     companions, the flow still terminates at the phantom target
//     (pre-#1893 behaviour preserved).
//   - TestProcessFlow_1893_BridgeStepIndex: the
//     cross_stack_bridge_at_step property is stamped with the chain
//     index of the first phantom edge target.
package engine

import (
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// makeXRepoFixture builds a two-repo fixture mirroring the #1893
// evidence: a frontend chain that fetches a backend route, where the
// backend has its own multi-step handler chain.
//
//	Frontend (repo "fe"):
//	  fe_entry --CALLS--> fe_loadData --FETCHES--> http_call ...
//	  http_call --CALLS(phantom, cross_repo)--> be_handler  (lives in "be")
//
//	Backend (repo "be"):
//	  be_handler --IMPLEMENTS--> http_endpoint_definition
//	  be_handler --CALLS--> be_service --CALLS--> be_repo
//
// After buildCallsAdjacencyMulti reverses the IMPLEMENTS edge, the
// chain through fe_handler also flows definition → be_handler (the
// #1639 handler-continuation reverse). Combined with the phantom edge
// fe_loadData reaches be_handler via the phantom and continues into
// the backend handler's own CALLS chain.
func makeXRepoFixture() (fe, be *graph.Document) {
	fe = &graph.Document{
		Repo: "fe",
		Entities: []graph.Entity{
			{ID: "fe_entry", Name: "loadDashboard", Kind: "SCOPE.Function", Language: "ts", SourceFile: "dashboard.ts"},
			{ID: "fe_loadData", Name: "fetchSummary", Kind: "SCOPE.Function", Language: "ts", SourceFile: "dashboard.ts"},
		},
		Relationships: []graph.Relationship{
			{ID: "fe_r1", FromID: "fe_entry", ToID: "fe_loadData", Kind: "CALLS"},
			// Phantom cross-repo CALLS edge pointing at the backend
			// handler. This is what links.PromoteToPhantomEdges injects.
			graph.Relationship{
				ID: "fe_phantom", FromID: "fe_loadData", ToID: "be_handler", Kind: "CALLS",
			}.WithProperties(map[string]string{
				"cross_repo":  "true",
				"target_repo": "be",
				"link_method": "http",
			},
			),
		},
	}
	be = &graph.Document{
		Repo: "be",
		Entities: []graph.Entity{
			{ID: "be_handler", Name: "OrdersController.getSummary", Kind: "SCOPE.Operation", Language: "java", SourceFile: "OrdersController.java"},
			{ID: "be_service", Name: "OrderService.summarize", Kind: "SCOPE.Operation", Language: "java", SourceFile: "OrderService.java"},
			{ID: "be_repo", Name: "OrderRepository.fetchAll", Kind: "SCOPE.Operation", Language: "java", SourceFile: "OrderRepository.java"},
		},
		Relationships: []graph.Relationship{
			{ID: "be_r1", FromID: "be_handler", ToID: "be_service", Kind: "CALLS"},
			{ID: "be_r2", FromID: "be_service", ToID: "be_repo", Kind: "CALLS"},
		},
	}
	return fe, be
}

// findProcessByEntry returns the first SCOPE.Process whose entry_id matches.
func findProcessByEntry(doc *graph.Document, entryID string) *graph.Entity {
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind != EntityKindProcess {
			continue
		}
		if e.PropGet("entry_id") == entryID {
			return e
		}
	}
	return nil
}

func TestProcessFlow_1893_CrossRepoExtension_Extends(t *testing.T) {
	fe, be := makeXRepoFixture()
	cfg := DefaultProcessFlowConfig()
	cfg.MinSteps = 2 // allow short chains for the test fixture

	stats := RunProcessFlowWithCompanions(fe, []*graph.Document{be}, cfg)
	if stats.Processes == 0 {
		t.Fatalf("expected ≥1 Process entity, got 0 (stats=%+v)", stats)
	}

	p := findProcessByEntry(fe, "fe_entry")
	if p == nil {
		t.Fatalf("no Process found with entry_id=fe_entry; got: %+v", fe.Entities)
	}

	chain := strings.Split(p.PropGet("chain"), ",")
	// Pre-#1893: chain would be [fe_entry, fe_loadData, be_handler] (3 steps,
	// terminating at the phantom target with no continuation).
	// Post-#1893: chain extends through be_service / be_repo.
	if len(chain) < 4 {
		t.Fatalf("expected chain to extend into backend (≥4 steps), got %d: %v", len(chain), chain)
	}
	// Assert backend handler + at least one of its downstream steps is in the chain.
	have := map[string]bool{}
	for _, id := range chain {
		have[id] = true
	}
	if !have["be_handler"] {
		t.Errorf("chain missing be_handler: %v", chain)
	}
	if !have["be_service"] && !have["be_repo"] {
		t.Errorf("chain didn't continue past be_handler into backend CALLS chain: %v", chain)
	}

	if p.PropGet("cross_stack") != "true" {
		t.Errorf("Process should be cross_stack=true, got %q", p.PropGet("cross_stack"))
	}
}

func TestProcessFlow_1893_NoCompanions_StillTerminates(t *testing.T) {
	fe, _ := makeXRepoFixture()
	cfg := DefaultProcessFlowConfig()
	cfg.MinSteps = 2

	// No companions — pre-#1893 behaviour: chain dead-ends at the phantom target.
	RunProcessFlow(fe, cfg)
	p := findProcessByEntry(fe, "fe_entry")
	if p == nil {
		t.Fatalf("no Process found with entry_id=fe_entry")
	}
	chain := strings.Split(p.PropGet("chain"), ",")
	// Without companions the chain must NOT continue into the backend.
	for _, id := range chain {
		if id == "be_service" || id == "be_repo" {
			t.Errorf("single-doc flow leaked backend entity %q into chain %v", id, chain)
		}
	}
	if chain[len(chain)-1] != "be_handler" {
		t.Errorf("expected chain to terminate at be_handler (phantom target); got terminal=%q chain=%v",
			chain[len(chain)-1], chain)
	}
	if p.PropGet("cross_stack") != "true" {
		t.Errorf("Process should still be cross_stack=true via phantom edge; got %q",
			p.PropGet("cross_stack"))
	}
}

func TestProcessFlow_1893_BridgeStepIndex(t *testing.T) {
	fe, be := makeXRepoFixture()
	cfg := DefaultProcessFlowConfig()
	cfg.MinSteps = 2

	RunProcessFlowWithCompanions(fe, []*graph.Document{be}, cfg)
	p := findProcessByEntry(fe, "fe_entry")
	if p == nil {
		t.Fatalf("no Process found")
	}
	raw, ok := p.PropLookup("cross_stack_bridge_at_step")
	if !ok {
		t.Fatalf("Process missing cross_stack_bridge_at_step property; props=%v", p.PropsSnapshot())
	}
	idx, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("cross_stack_bridge_at_step not an integer: %q (%v)", raw, err)
	}
	// In the fixture the phantom edge target be_handler is the 3rd step
	// (index 2: fe_entry[0] -> fe_loadData[1] -> be_handler[2]).
	if idx != 2 {
		t.Errorf("cross_stack_bridge_at_step = %d, want 2", idx)
	}
}
