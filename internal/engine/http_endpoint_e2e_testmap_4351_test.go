package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #4351 — e2e HTTP route tests (supertest route-by-string) must produce a
// TESTS edge from the test_suite to the http_endpoint_definition they exercise.
//
// These tests drive the REAL resolve pass (ResolveHTTPEndpointHandlers) over a
// merged entity table shaped like the live core-backend-v3 graph: NestJS
// controller routes synthesized as http_endpoint_definition entities (with an
// `/api/v1` mount prefix and `:id`/`{id}` template params) plus the one-per-spec
// test_suite that the Jest extractor stamps with `e2e_route_calls`.
//
// BEFORE this fix the resolve pass had no e2e-route linkage step, so a suite's
// route calls produced ZERO TESTS→endpoint edges and the endpoints looked
// untested. AFTER, each (verb, normalized-route) that uniquely matches a
// definition yields a TESTS edge.

// testsEdgesToEndpoints returns the TESTS edges whose target is an
// http_endpoint_definition stub.
func testsEdgesToEndpoints(ents []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindTests) &&
				len(r.ToID) > len(httpEndpointDefinitionKind) &&
				r.ToID[:len(httpEndpointDefinitionKind)] == httpEndpointDefinitionKind {
				out = append(out, r)
			}
		}
	}
	return out
}

func def(verb, path string) types.EntityRecord {
	return types.EntityRecord{
		Kind:       httpEndpointDefinitionKind,
		Name:       "http:" + verb + ":" + path,
		SourceFile: "src/x/x.controller.ts",
		Language:   "typescript",
		Properties: map[string]string{
			"verb":         verb,
			"path":         path,
			"framework":    "nestjs",
			"pattern_type": "http_endpoint_synthesis",
		},
	}
}

func e2eSuite(routeCalls string) types.EntityRecord {
	return types.EntityRecord{
		Kind:       testSuiteKind,
		Subtype:    "test_suite",
		Name:       "spec:alt-address.e2e:AltAddress",
		SourceFile: "test/alternate-address-write.e2e-spec.ts",
		Language:   "typescript",
		Properties: map[string]string{
			"framework":       "jest",
			"e2e_route_calls": routeCalls,
		},
	}
}

// TestIssue4351_E2ERouteTestsLinkToEndpoints is the core RED→GREEN proof. A
// suite issuing concrete supertest routes against templated, /api/v1-prefixed
// NestJS endpoints must link to exactly the endpoints it covers.
func TestIssue4351_E2ERouteTestsLinkToEndpoints(t *testing.T) {
	// Definitions as the NestJS synthesizer emits them: prefixed + templated.
	defs := []types.EntityRecord{
		def("POST", "/api/v1/alternate-addresses"),
		def("PATCH", "/api/v1/alternate-addresses/{id}"),
		def("DELETE", "/api/v1/alternate-addresses/{id}"),
		// An unrelated endpoint that must NOT be linked.
		def("GET", "/api/v1/buildings/{id}"),
	}

	// Suite route calls as the Jest extractor captures them (concrete params,
	// const-folded prefix; PATCH/DELETE hit a concrete id like "9999").
	suite := e2eSuite(
		"POST /api/v1/alternate-addresses\n" +
			"PATCH /api/v1/alternate-addresses/9999\n" +
			"DELETE /api/v1/alternate-addresses/42",
	)

	merged := append(append([]types.EntityRecord{}, defs...), suite)

	// BEFORE: no e2e_route_calls property → no edges (control).
	control := suite
	control.Properties = map[string]string{"framework": "jest"} // strip the property
	beforeMerged := append(append([]types.EntityRecord{}, defs...), control)
	beforeOut, beforeStats := ResolveHTTPEndpointHandlers(beforeMerged)
	if got := len(testsEdgesToEndpoints(beforeOut)); got != 0 {
		t.Fatalf("control (no e2e_route_calls) must emit 0 TESTS→endpoint edges, got %d", got)
	}
	if beforeStats.E2ERouteTestEdges != 0 {
		t.Fatalf("control E2ERouteTestEdges=%d, want 0", beforeStats.E2ERouteTestEdges)
	}

	// AFTER: route calls present → TESTS edges to the matched endpoints.
	out, stats := ResolveHTTPEndpointHandlers(merged)
	edges := testsEdgesToEndpoints(out)
	if stats.E2ERouteTestEdges != 3 {
		t.Fatalf("expected 3 e2e route TESTS edges, got %d (edges=%v)", stats.E2ERouteTestEdges, edges)
	}

	wantTargets := map[string]bool{
		"http_endpoint_definition:http:POST:/api/v1/alternate-addresses":        false,
		"http_endpoint_definition:http:PATCH:/api/v1/alternate-addresses/{id}":  false,
		"http_endpoint_definition:http:DELETE:/api/v1/alternate-addresses/{id}": false,
	}
	for _, e := range edges {
		if _, ok := wantTargets[e.ToID]; ok {
			wantTargets[e.ToID] = true
		} else {
			t.Errorf("unexpected TESTS edge target %q", e.ToID)
		}
	}
	for tgt, got := range wantTargets {
		if !got {
			t.Errorf("missing TESTS edge to %q; edges=%v", tgt, edges)
		}
	}

	// The unrelated /buildings endpoint must NOT be a target.
	for _, e := range edges {
		if e.ToID == "http_endpoint_definition:http:GET:/api/v1/buildings/{id}" {
			t.Error("linked an endpoint the suite never called (false TESTS edge)")
		}
	}
}

// TestIssue4351_UnprefixedTestRouteMatchesPrefixedDef proves the API/version
// prefix is tolerated on either side: a test that calls `/alternate-addresses`
// (no /api/v1) still links to the prefixed backend definition.
func TestIssue4351_UnprefixedTestRouteMatchesPrefixedDef(t *testing.T) {
	defs := []types.EntityRecord{def("POST", "/api/v1/alternate-addresses")}
	suite := e2eSuite("POST /alternate-addresses")
	out, stats := ResolveHTTPEndpointHandlers(append(defs, suite))
	if stats.E2ERouteTestEdges != 1 {
		t.Fatalf("prefix-tolerant match failed: edges=%d", stats.E2ERouteTestEdges)
	}
	_ = out
}

// TestIssue4351_AmbiguousRouteSkipped proves conservatism: when a (verb, route)
// matches MORE THAN ONE definition, no TESTS edge is fabricated.
func TestIssue4351_AmbiguousRouteSkipped(t *testing.T) {
	// Two definitions whose templates both match `/things/123`.
	defs := []types.EntityRecord{
		def("GET", "/things/{id}"),
		def("GET", "/things/{slug}"),
	}
	suite := e2eSuite("GET /things/123")
	_, stats := ResolveHTTPEndpointHandlers(append(defs, suite))
	if stats.E2ERouteTestEdges != 0 {
		t.Fatalf("ambiguous route must be skipped, got %d edges", stats.E2ERouteTestEdges)
	}
}

// TestIssue4351_NoMatchNoEdge proves an unmatched route fabricates nothing.
func TestIssue4351_NoMatchNoEdge(t *testing.T) {
	defs := []types.EntityRecord{def("GET", "/known")}
	suite := e2eSuite("GET /totally/unknown/path")
	_, stats := ResolveHTTPEndpointHandlers(append(defs, suite))
	if stats.E2ERouteTestEdges != 0 {
		t.Fatalf("unmatched route must emit 0 edges, got %d", stats.E2ERouteTestEdges)
	}
}
