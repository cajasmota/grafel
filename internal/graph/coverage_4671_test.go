package graph

import "testing"

// TestComputeCoverage_4671_UnitSpecCreditsEndpoint mirrors the RESOLVED graph
// shape that the JS/TS extractor (after issue #4671) + resolver produce for a
// NestJS controller UNIT spec:
//
//	x.controller.ts:    class XController { @Get('get_counts') getCounts(y){...} }
//	x.controller.spec.ts:
//	    const controller = new XController(mockSvc);
//	    describe('XController', () => { it('counts', () => controller.getCounts('2025')) })
//
// After #4671 the spec's it() block is a test Operation that CALLS the handler
// (the structural-ref the extractor emits is bound by the resolver to the
// handler entity ID before ComputeCoverage runs). ComputeCoverage must then:
//   - credit the handler via the synthetic test→CALLS→handler edge (phase 2),
//   - credit the http_endpoint_definition the handler backs (phase 4).
//
// This is the live-proven path that was missing (graph 18.2% vs real ~80%).
func TestComputeCoverage_4671_UnitSpecCreditsEndpoint(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		// Production: handler method + the endpoint definition it backs.
		{ID: "handler", Name: "getCounts", Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "src/x.controller.ts"},
		{ID: "endpoint", Name: "GET /v1/proposals/get_counts", Kind: "http_endpoint_definition", SourceFile: "src/x.controller.ts"},
		// Test: the it() block Operation emitted by the #4671 pass.
		{ID: "spec_it", Name: "it:counts@2", Kind: "SCOPE.Operation", Subtype: "test", SourceFile: "src/x.controller.spec.ts"},
	}
	rels := []Relationship{
		// handler backs the endpoint (resolver-emitted IMPLEMENTS).
		{ID: "impl", FromID: "handler", ToID: "endpoint", Kind: "IMPLEMENTS"},
		// the #4671 test→handler CALLS edge (resolved to the handler entity ID).
		{ID: "calls", FromID: "spec_it", ToID: "handler", Kind: "CALLS"},
	}
	report := ComputeCoverage(makeDoc(entities, rels))

	covered := map[string]bool{}
	for _, e := range entities {
		covered[e.ID] = true
	}
	for _, u := range report.UncoveredEntities {
		covered[u.EntityID] = false
	}
	if !covered["handler"] {
		t.Error("handler getCounts should be covered via test→CALLS→handler (phase 2)")
	}
	if !covered["endpoint"] {
		t.Error("endpoint should be credited via handler (phase 4)")
	}
	if report.TotalProduction != 2 {
		t.Errorf("TotalProduction want 2 (handler + endpoint), got %d", report.TotalProduction)
	}
	if report.CoveredProduction != 2 {
		t.Errorf("CoveredProduction want 2, got %d", report.CoveredProduction)
	}
}

// TestComputeCoverage_4671_OfflineContractSpecNotCredited is the honest-
// exclusion guard: an OFFLINE contract spec asserts constants and never
// invokes the handler, so it emits NO CALLS edge to the handler. Such a spec
// must NOT credit the endpoint (the 279 upvate-v3 contract specs correctly
// stay uncredited). Ref #4662.
func TestComputeCoverage_4671_OfflineContractSpecNotCredited(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "handler", Name: "getCounts", Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "src/x.controller.ts"},
		{ID: "endpoint", Name: "GET /v1/proposals/get_counts", Kind: "http_endpoint_definition", SourceFile: "src/x.controller.ts"},
		// Contract spec: a test Operation that calls only an assertion helper —
		// no CALLS edge to the handler.
		{ID: "contract_it", Name: "it:contract@2", Kind: "SCOPE.Operation", Subtype: "test", SourceFile: "src/x.contract.spec.ts"},
	}
	rels := []Relationship{
		{ID: "impl", FromID: "handler", ToID: "endpoint", Kind: "IMPLEMENTS"},
		// No CALLS edge from contract_it to handler.
	}
	report := ComputeCoverage(makeDoc(entities, rels))
	for _, u := range report.UncoveredEntities {
		if u.EntityID == "handler" {
			// good — handler stays uncovered.
		}
	}
	if report.CoveredProduction != 0 {
		t.Errorf("CoveredProduction want 0 (offline contract spec credits nothing), got %d", report.CoveredProduction)
	}
}
