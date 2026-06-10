package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #4628 — END-TO-END coverage repro for the upvate-v3 (Django/DRF + pytest)
// stack. The symptom: every http_endpoint reads UNCOVERED and overall coverage is
// stuck at ~18%, even though the spec→endpoint linkage (#4351/#4487/#4620) and
// handler-credit (#4559) passes are all merged.
//
// Unlike the existing #4351 fixtures — which keep the SAME `/api/v1` prefix on
// BOTH the endpoint-definition path AND the test route — this fixture reproduces
// the LIVE asymmetry of the upvate stack:
//
//   - DRF synthesizes the endpoint definition with path "/v1/inspections/get_counts"
//     (the `api` segment dropped during synthesis — the live ID is
//     `http:GET:/v1/inspections/get_counts`).
//   - The pytest APITestCase drives the route through the FULL mount path
//     `self.client.get("/api/v1/inspections/get_counts")`, so the pytest
//     extractor stamps `e2e_route_calls = "GET /api/v1/inspections/get_counts"`.
//   - The pytest suite is emitted with Kind "SCOPE.Pattern" (NOT "SCOPE.Operation"
//     as the Jest extractor uses).
//
// The test drives the WHOLE pipeline the live indexer runs:
//
//	ResolveHTTPEndpointHandlers  (emits IMPLEMENTS + e2e-route TESTS edges as stubs)
//	  → assign EntityIDs → resolve.BuildIndex → resolve.ReferencesEmbedded
//	    (rewrites the Kind:Name edge stubs to 16-char hash IDs)
//	      → graph bridge → graph.ComputeCoverage
//
// and asserts the endpoint is credited covered. If the endpoint reads uncovered
// the linkage broke somewhere in that chain — which is the #4628 RED state.

// drfEndpoint builds an http_endpoint synthetic exactly as the DRF synthesis
// pass emits it: Kind "http_endpoint" (migrated to definition inside the resolve
// pass), a `source_handler` pointing at the ViewSet method, and a `path` that has
// already had its `api` mount segment dropped (live shape).
func drfEndpoint(verb, path, viewMethod string) types.EntityRecord {
	return types.EntityRecord{
		Kind:       httpEndpointKind,
		Name:       "http:" + verb + ":" + path,
		SourceFile: "core/views/inspection_viewset.py",
		Language:   "python",
		Properties: map[string]string{
			"verb":           verb,
			"path":           path,
			"framework":      "django-rest-framework",
			"pattern_type":   "http_endpoint_synthesis",
			"source_handler": "SCOPE.Operation:" + viewMethod,
		},
	}
}

// drfHandler builds the ViewSet method entity the endpoint's source_handler
// resolves to (e.g. "InspectionViewSet.get_counts").
func drfHandler(viewMethod string) types.EntityRecord {
	return types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Name:       viewMethod,
		SourceFile: "core/views/inspection_viewset.py",
		Language:   "python",
		StartLine:  2836,
	}
}

// pytestSuite builds the one-per-file pytest test_suite as the pytest extractor
// emits it: Kind "SCOPE.Pattern" (Subtype "test_suite") in a test file, carrying
// the `e2e_route_calls` property with the FULL `/api/v1` route the client hits.
func pytestSuite(routeCalls string) types.EntityRecord {
	return types.EntityRecord{
		Kind:       "SCOPE.Pattern",
		Subtype:    "test_suite",
		Name:       "pytest_suite:test_inspection_counts",
		SourceFile: "core/tests/test_inspection_counts.py",
		Language:   "python",
		Properties: map[string]string{
			"framework":       "pytest",
			"pattern_type":    "test_suite",
			"e2e_route_calls": routeCalls,
		},
	}
}

// runFullCoveragePipeline mirrors the live indexer: resolve endpoints, assign
// IDs, build the ref index, rewrite the embedded edge stubs to hash IDs, bridge
// to a graph.Document, and compute coverage.
func runFullCoveragePipeline(merged []types.EntityRecord) (*graph.CoverageReport, []types.EntityRecord) {
	resolved, _ := ResolveHTTPEndpointHandlers(merged)

	// Assign stable IDs (the live indexer does this before BuildIndex).
	for i := range resolved {
		resolved[i].ID = resolved[i].ComputeID()
	}

	// Rewrite the embedded Kind:Name relationship stubs to 16-char hash IDs.
	idx := resolve.BuildIndex(resolved)
	resolve.ReferencesEmbedded(resolved, idx)

	// Bridge EntityRecords → graph.Document (entities + flattened relationships),
	// exactly as the read layer presents them to ComputeCoverage.
	doc := &graph.Document{}
	for i := range resolved {
		e := &resolved[i]
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:         e.ID,
			Name:       e.Name,
			Kind:       e.Kind,
			Subtype:    e.Subtype,
			SourceFile: e.SourceFile,
			StartLine:  e.StartLine,
			Language:   e.Language,
			Properties: e.Properties,
		})
		for _, r := range e.Relationships {
			// The read layer flattens embedded relationships with FromID = the
			// parent entity's ID when the edge itself didn't carry one (an
			// embedded edge is anchored on its parent). Mirror that here.
			from := r.FromID
			if from == "" {
				from = e.ID
			}
			doc.Relationships = append(doc.Relationships, graph.Relationship{
				FromID:     from,
				ToID:       r.ToID,
				Kind:       r.Kind,
				Properties: r.Properties,
			})
		}
	}
	return graph.ComputeCoverage(doc), resolved
}

// TestIssue4628_DRFEndpointCreditedViaE2ERouteTest is the core RED→GREEN proof.
// A pytest APITestCase hits `/api/v1/inspections/get_counts`; the DRF endpoint is
// synthesized at `/v1/inspections/get_counts`. The full pipeline must credit the
// endpoint covered (via the direct e2e-route TESTS edge the testmap emits).
func TestIssue4628_DRFEndpointCreditedViaE2ERouteTest(t *testing.T) {
	merged := []types.EntityRecord{
		drfEndpoint("GET", "/v1/inspections/get_counts", "InspectionViewSet.get_counts"),
		drfHandler("InspectionViewSet.get_counts"),
		// pytest client hits the FULL /api/v1 mount path.
		pytestSuite("GET /api/v1/inspections/get_counts"),
	}

	report, resolved := runFullCoveragePipeline(merged)

	// Locate the endpoint's hash ID.
	var epID string
	for i := range resolved {
		if resolved[i].Kind == httpEndpointDefinitionKind {
			epID = resolved[i].ID
		}
	}
	if epID == "" {
		t.Fatalf("no http_endpoint_definition found after resolve")
	}

	for _, u := range report.UncoveredEntities {
		if u.EntityID == epID {
			t.Errorf("#4628 RED: DRF endpoint /v1/inspections/get_counts reads UNCOVERED "+
				"despite a pytest APITestCase hitting /api/v1/inspections/get_counts "+
				"(covered=%d/%d, %.1f%%)", report.CoveredProduction, report.TotalProduction, report.CoveragePct)
		}
	}
}

// TestIssue4628_DRFEndpointCreditedViaHandlerHop is the complementary proof for
// the handler-credit hop (#4559): a pytest suite whose name-affinity TESTS edge
// reaches the ViewSet METHOD must credit the http_endpoint that method backs,
// via the IMPLEMENTS edge the resolver emits from source_handler.
func TestIssue4628_DRFEndpointCreditedViaHandlerHop(t *testing.T) {
	// A pytest suite that links to the handler METHOD directly (the name-affinity
	// / CALLS path the pytest extractor produces), with NO e2e_route_calls — so the
	// ONLY way the endpoint gets credited is the IMPLEMENTS hop from the covered
	// handler.
	suite := types.EntityRecord{
		Kind:       "SCOPE.Pattern",
		Subtype:    "test_suite",
		Name:       "pytest_suite:test_inspection_counts",
		SourceFile: "core/tests/test_inspection_counts.py",
		Language:   "python",
		Properties: map[string]string{"framework": "pytest", "pattern_type": "test_suite"},
		Relationships: []types.RelationshipRecord{
			{
				ToID: "SCOPE.Operation:InspectionViewSet.get_counts",
				Kind: string(types.RelationshipKindTests),
				Properties: map[string]string{
					"framework":    "pytest",
					"match_source": "python_test_name_affinity",
				},
			},
		},
	}

	merged := []types.EntityRecord{
		drfEndpoint("GET", "/v1/inspections/get_counts", "InspectionViewSet.get_counts"),
		drfHandler("InspectionViewSet.get_counts"),
		suite,
	}

	report, resolved := runFullCoveragePipeline(merged)

	var epID, handlerID string
	for i := range resolved {
		switch {
		case resolved[i].Kind == httpEndpointDefinitionKind:
			epID = resolved[i].ID
		case resolved[i].Name == "InspectionViewSet.get_counts":
			handlerID = resolved[i].ID
		}
	}
	if epID == "" || handlerID == "" {
		t.Fatalf("endpoint or handler missing after resolve (ep=%q handler=%q)", epID, handlerID)
	}

	for _, u := range report.UncoveredEntities {
		if u.EntityID == handlerID {
			t.Errorf("#4628 RED: ViewSet handler get_counts reads UNCOVERED despite a pytest " +
				"name-affinity TESTS edge (pytest suite Kind=SCOPE.Pattern not credited?)")
		}
		if u.EntityID == epID {
			t.Errorf("#4628 RED: DRF endpoint reads UNCOVERED despite its handler being tested " +
				"(IMPLEMENTS handler-hop / #4559 not firing)")
		}
	}
}

// TestIssue4628_PYTEST_CallsPathFromScopePattern checks whether a pytest suite
// (Kind SCOPE.Pattern) that reaches a handler via a CALLS edge credits the
// handler. ComputeCoverage phase-2 only collects CALLS from entities classified
// as TEST entities (testEntityKinds = SCOPE.Operation/Function/Method) — and
// SCOPE.Pattern is NOT in that set, so a pytest suite's CALLS edges are dropped.
func TestIssue4628_PYTEST_CallsPathFromScopePattern(t *testing.T) {
	entities := []graph.Entity{
		{ID: "handler", Name: "InspectionViewSet.get_counts", Kind: "SCOPE.Operation",
			SourceFile: "core/views/inspection_viewset.py", StartLine: 2836},
		{ID: "suite", Name: "pytest_suite:test_x", Kind: "SCOPE.Pattern", Subtype: "test_suite",
			SourceFile: "core/tests/test_x.py"},
	}
	rels := []graph.Relationship{
		{FromID: "suite", ToID: "handler", Kind: "CALLS"},
	}
	doc := &graph.Document{Entities: entities, Relationships: rels}
	report := graph.ComputeCoverage(doc)
	uncov := false
	for _, u := range report.UncoveredEntities {
		if u.EntityID == "handler" {
			uncov = true
		}
	}
	if uncov {
		t.Errorf("#4628 PYTEST RED: pytest suite (SCOPE.Pattern) CALLS handler but handler reads UNCOVERED — SCOPE.Pattern excluded from testEntityKinds")
	}
	if report.TotalTests == 0 {
		t.Errorf("#4628 PYTEST RED: pytest suite (SCOPE.Pattern test_suite) not counted in TotalTests")
	}
}

// TestIssue4628_FullChain_PytestCallsToEndpoint is the end-to-end lever proof:
// a pytest suite (SCOPE.Pattern) CALLS the DRF ViewSet handler, the handler
// IMPLEMENTS the http_endpoint — so coverage must propagate
// suite(CALLS)→handler(covered)→endpoint(handler-hop). This is the dominant
// shape across the upvate-v3 Python/DRF surface and the reason coverage was
// pinned at ~18% (every endpoint uncovered) before the SCOPE.Pattern fix.
func TestIssue4628_FullChain_PytestCallsToEndpoint(t *testing.T) {
	merged := []types.EntityRecord{
		drfEndpoint("GET", "/v1/inspections/get_counts", "InspectionViewSet.get_counts"),
		drfHandler("InspectionViewSet.get_counts"),
		// pytest suite reaches the handler method via CALLS (no e2e_route_calls,
		// no direct TESTS-to-endpoint), so the ONLY route to crediting the
		// endpoint is: SCOPE.Pattern counted as test → CALLS credits handler →
		// IMPLEMENTS hop credits endpoint.
		{
			Kind:       "SCOPE.Pattern",
			Subtype:    "test_suite",
			Name:       "pytest_suite:test_inspection_counts",
			SourceFile: "core/tests/test_inspection_counts.py",
			Language:   "python",
			Properties: map[string]string{"framework": "pytest", "pattern_type": "test_suite"},
			Relationships: []types.RelationshipRecord{
				{
					ToID: "SCOPE.Operation:InspectionViewSet.get_counts",
					Kind: "CALLS",
				},
			},
		},
	}

	report, resolved := runFullCoveragePipeline(merged)

	var epID, handlerID string
	for i := range resolved {
		switch {
		case resolved[i].Kind == httpEndpointDefinitionKind:
			epID = resolved[i].ID
		case resolved[i].Name == "InspectionViewSet.get_counts":
			handlerID = resolved[i].ID
		}
	}
	if epID == "" || handlerID == "" {
		t.Fatalf("endpoint or handler missing after resolve (ep=%q handler=%q)", epID, handlerID)
	}
	for _, u := range report.UncoveredEntities {
		if u.EntityID == handlerID {
			t.Errorf("#4628 RED: handler uncovered despite pytest CALLS edge")
		}
		if u.EntityID == epID {
			t.Errorf("#4628 RED: endpoint uncovered despite pytest CALLS→handler→IMPLEMENTS chain")
		}
	}
}
