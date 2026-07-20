// process_flow_xrepo_4316_test.go — fix for issue #4316.
//
// Symptom (measured on `acme`): 307 cross-repo HTTP links RESOLVED but
// only 5 cross-repo FLOWS existed, with many flows dead-ending either AT
// the http-call node (B1) or AT the backend entry handler (B2).
//
// Root cause (B1): a frontend caller has two outgoing edges in the
// unified adjacency — a FETCHES edge to its dead-end consumer
// http_endpoint synthetic, and a phantom cross-repo CALLS edge to the
// resolved backend handler. primaryPath() picked whichever child sorted
// first by entity ID; when the synthetic sorted first the persisted chain
// dead-ended at the synthetic and the real end-to-end path was buried in
// a non-primary DAG branch. primaryPathCrossRepo() now prefers the branch
// that continues across the resolved cross-repo boundary.
//
// B2 (handler → service continuation) already worked once the chain
// reaches the handler via companion adjacency; the test below pins it so
// it cannot regress.
//
// Guard tests assert NO over-chaining: an UNRESOLVED http-call synthetic
// (no phantom edge) stays terminal, and a UI-state setter chain is
// unchanged.
package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// b1Docs builds the B1 shape: a frontend caller that both FETCHES a
// consumer http_endpoint synthetic (dead-end, ID sorts FIRST) and carries
// a phantom cross-repo CALLS edge to the resolved backend handler, which
// itself calls a service. The synthetic ID is chosen to sort BEFORE the
// handler ID so the pre-fix leftmost walk would dead-end at it.
func b1Docs() (fe, be *graph.Document) {
	fe = &graph.Document{
		Repo: "fe",
		Entities: []graph.Entity{
			{ID: "fe_entry", Name: "Details", Kind: "SCOPE.Function", Language: "ts", SourceFile: "Details.tsx"},
			graph.Entity{ID: "aaa_synth", Name: "http:GET:/group-building-settings", Kind: "http_endpoint",
				Language: "ts", SourceFile: "Details.tsx",
			}.WithProperties(map[string]string{"pattern_type": "http_endpoint_client_synthesis"}),
		},
		Relationships: []graph.Relationship{
			{ID: "fe_f1", FromID: "fe_entry", ToID: "aaa_synth", Kind: "FETCHES"},
			graph.Relationship{ID: "fe_ph", FromID: "fe_entry", ToID: "zzz_handler", Kind: "CALLS"}.WithProperties(map[string]string{"cross_repo": "true", "target_repo": "be", "link_method": "http"}),
		},
	}
	be = &graph.Document{
		Repo: "be",
		Entities: []graph.Entity{
			{ID: "zzz_handler", Name: "GroupBuildingSettingsViewSet.retrieve", Kind: "SCOPE.Operation",
				Language: "python", SourceFile: "views.py"},
			{ID: "zzz_service", Name: "SettingsService.get", Kind: "SCOPE.Operation",
				Language: "python", SourceFile: "service.py"},
			{ID: "zzz_repo", Name: "SettingsRepository.fetch", Kind: "SCOPE.Operation",
				Language: "python", SourceFile: "repo.py"},
		},
		Relationships: []graph.Relationship{
			{ID: "be_r1", FromID: "zzz_handler", ToID: "zzz_service", Kind: "CALLS"},
			{ID: "be_r2", FromID: "zzz_service", ToID: "zzz_repo", Kind: "CALLS"},
		},
	}
	return
}

func processChain(doc *graph.Document, entryID string) []string {
	p := findProcessByEntry(doc, entryID)
	if p == nil {
		return nil
	}
	return strings.Split(p.PropGet("chain"), ",")
}

// B1: a flow reaching an http-call node WITH a resolved cross-repo link
// must continue past it into the backend handler (and onward), NOT
// dead-end at the consumer synthetic.
func TestProcessFlow_4316_B1_ContinuesPastHTTPCall(t *testing.T) {
	fe, be := b1Docs()
	cfg := DefaultProcessFlowConfig()
	cfg.MinSteps = 2

	RunProcessFlowWithCompanions(fe, []*graph.Document{be}, cfg)

	chain := processChain(fe, "fe_entry")
	if len(chain) == 0 {
		t.Fatalf("no Process emitted for fe_entry")
	}
	have := map[string]bool{}
	for _, id := range chain {
		have[id] = true
	}
	// Must NOT dead-end at the synthetic.
	if chain[len(chain)-1] == "aaa_synth" {
		t.Fatalf("B1 not fixed: chain dead-ends at consumer synthetic: %v", chain)
	}
	if !have["zzz_handler"] {
		t.Fatalf("B1: chain did not cross into backend handler: %v", chain)
	}
	if !have["zzz_service"] {
		t.Fatalf("B1: chain reached handler but did not continue into service: %v", chain)
	}
	p := findProcessByEntry(fe, "fe_entry")
	if p.PropGet("cross_stack") != "true" {
		t.Errorf("expected cross_stack=true, got %q", p.PropGet("cross_stack"))
	}
}

// B2: once at a backend handler reached via the cross-repo link, the flow
// must continue into the handler's own callees (service → repository),
// i.e. end-to-end rather than stopping at the entry handler.
func TestProcessFlow_4316_B2_ChainsHandlerToService(t *testing.T) {
	fe, be := b1Docs()
	cfg := DefaultProcessFlowConfig()
	cfg.MinSteps = 2

	RunProcessFlowWithCompanions(fe, []*graph.Document{be}, cfg)

	chain := processChain(fe, "fe_entry")
	last := chain[len(chain)-1]
	// The terminal must be the deepest backend node, not the entry handler.
	if last == "zzz_handler" {
		t.Fatalf("B2 not fixed: chain terminates at entry handler: %v", chain)
	}
	if last != "zzz_repo" {
		t.Fatalf("B2: chain did not reach the deepest backend callee (zzz_repo); terminal=%s chain=%v", last, chain)
	}
}

// Guard 1 — an UNRESOLVED http-call (consumer synthetic with NO phantom
// cross-repo edge) must STAY a terminal. No over-chaining: the flow ends
// at the synthetic because no resolved link exists to traverse.
func TestProcessFlow_4316_Guard_UnresolvedHTTPCallStaysTerminal(t *testing.T) {
	fe := &graph.Document{
		Repo: "fe",
		Entities: []graph.Entity{
			{ID: "fe_a", Name: "loadThing", Kind: "SCOPE.Function", Language: "ts", SourceFile: "a.tsx"},
			{ID: "fe_b", Name: "callApi", Kind: "SCOPE.Function", Language: "ts", SourceFile: "a.tsx"},
			graph.Entity{ID: "fe_synth", Name: "http:GET:/orphan", Kind: "http_endpoint", Language: "ts", SourceFile: "a.tsx"}.WithProperties(map[string]string{"pattern_type": "http_endpoint_client_synthesis"}),
		},
		Relationships: []graph.Relationship{
			{ID: "fe_r1", FromID: "fe_a", ToID: "fe_b", Kind: "CALLS"},
			{ID: "fe_f1", FromID: "fe_b", ToID: "fe_synth", Kind: "FETCHES"},
			// No phantom edge: the link never resolved to a backend handler.
		},
	}
	cfg := DefaultProcessFlowConfig()
	cfg.MinSteps = 2

	RunProcessFlow(fe, cfg) // no companions; nothing to cross into anyway

	chain := processChain(fe, "fe_a")
	if len(chain) == 0 {
		t.Fatalf("no Process emitted for fe_a")
	}
	// The synthetic is a legitimate terminal — there is nothing resolved to
	// chain into. It must remain the terminal and the chain must not invent
	// downstream steps.
	if chain[len(chain)-1] != "fe_synth" {
		t.Fatalf("unresolved http-call should stay terminal; got chain=%v", chain)
	}
}

// Guard 2 — a UI-state setter chain (getState/setIsLoading/dispatch) must
// stay exactly as long as before: the cross-repo chooser only diverges
// from leftmost when a sibling crosses a repo boundary, which never
// happens here.
func TestProcessFlow_4316_Guard_UIStateSetterUnchanged(t *testing.T) {
	mk := func(id, name string) graph.Entity {
		return graph.Entity{ID: id, Name: name, Kind: "SCOPE.Function", Language: "ts", SourceFile: "ui.tsx"}
	}
	fe := &graph.Document{
		Repo: "fe",
		Entities: []graph.Entity{
			mk("h", "useThing"),
			mk("cb", "handleClick"),
			mk("set", "setIsLoading"),
			mk("disp", "dispatch"),
			mk("gs", "getState"),
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "h", ToID: "cb", Kind: "CALLS"},
			{ID: "r2", FromID: "cb", ToID: "set", Kind: "CALLS"},
			{ID: "r3", FromID: "cb", ToID: "disp", Kind: "CALLS"},
			{ID: "r4", FromID: "disp", ToID: "gs", Kind: "CALLS"},
		},
	}
	feCopy := *fe

	cfg := DefaultProcessFlowConfig()
	cfg.MinSteps = 2

	// Cross-repo chooser path (with empty companions).
	RunProcessFlowWithCompanions(fe, nil, cfg)
	gotX := strings.Join(processChain(fe, "h"), ",")

	// Reference leftmost path.
	RunProcessFlow(&feCopy, cfg)
	gotRef := strings.Join(processChain(&feCopy, "h"), ",")

	if gotX != gotRef {
		t.Fatalf("UI-state setter chain changed under cross-repo chooser:\n  xrepo = %q\n  ref   = %q", gotX, gotRef)
	}
	// Sanity: the chain must not have grown beyond the real call depth.
	if n := len(strings.Split(gotX, ",")); n > 4 {
		t.Fatalf("UI-state chain over-chained to %d steps: %q", n, gotX)
	}
}
