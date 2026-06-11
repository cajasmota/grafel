package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"

	// Register the PHP route-hit extractor so the test_suite (carrying
	// e2e_route_calls) comes from the REAL extractor, not a hand-built fixture.
	_ "github.com/cajasmota/archigraph/internal/custom/php"
)

// Issue #4686 LIVE-REPRO (resolve side) — Laravel / Symfony / Pest feature tests.
//
// Proves end-to-end that a PHP feature test calling a route by string
// (`$this->getJson('/api/v1/...')`, `$client->request('GET', '/...')`) links to
// the http_endpoint_definition it exercises — the PHP slice of the all-language
// program (#4615), generalizing #4351 / #4369 / #4370 / #4371 / #4684 / #4685.
//
// Pipeline:
//  1. http_endpoint_definition entities for the Laravel routes (PHP route→
//     definition migration produces these at resolve; hand-built here to keep
//     the fixture focused on the e2e linkage, mirroring the #4351 NestJS shape).
//  2. The real custom_php_phpunit_pest extractor over the feature-test file →
//     the one-per-file test_suite carrying e2e_route_calls.
//  3. ResolveHTTPEndpointHandlers → the shared linkE2ERouteTestsToEndpoints pass
//     emits the TESTS→endpoint edges.

const phpFeatureTestSrc4686 = `<?php
class InspectionsTest extends TestCase {
    public function test_get_one() {
        $response = $this->getJson('/api/v1/inspections/123');
        $response->assertStatus(200);
    }

    public function test_create_item() {
        $this->postJson('/api/v1/inspections/123/items', ['name' => 'x']);
    }
}
`

func TestIssue4686_PHPFeatureE2ERouteTestsLinkToEndpoints(t *testing.T) {
	defs := []types.EntityRecord{
		def("GET", "/api/v1/inspections/{id}"),
		def("POST", "/api/v1/inspections/{id}/items"),
	}
	suite := realSuite(t, "custom_php_phpunit_pest",
		"tests/Feature/InspectionsTest.php", "php", phpFeatureTestSrc4686)

	afterOut, edges := runE2ERouteResolve(t, defs, suite)
	if edges < 2 {
		t.Fatalf("expected >=2 e2e route TESTS edges (GET + POST), got %d", edges)
	}
	targets := edgeTargets(afterOut)

	wantGet, wantPost := false, false
	for to := range targets {
		if strings.Contains(to, "GET:/api/v1/inspections/{id}") && !strings.Contains(to, "items") {
			wantGet = true
		}
		if strings.Contains(to, "POST:/api/v1/inspections/{id}/items") {
			wantPost = true
		}
	}
	if !wantGet {
		t.Errorf("expected a TESTS edge to GET /api/v1/inspections/{id}; targets=%v", targets)
	}
	if !wantPost {
		t.Errorf("expected a TESTS edge to POST /api/v1/inspections/{id}/items; targets=%v", targets)
	}
}
