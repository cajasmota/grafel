package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	// Register the pytest extractor so the suite (with e2e_route_calls) comes
	// from the REAL extractor, not a hand-built fixture.
	_ "github.com/cajasmota/archigraph/internal/custom/python"
)

// Issue #4369 LIVE-REPRO (resolve side, full in-pipeline).
//
// Proves end-to-end that a Python DRF/Django API test calling a route by string
// links to the http_endpoint_definition it exercises — the finer-grained
// endpoint-level TESTS edge that complements the ViewSet-class TESTS edge
// ApplyTestsMultiHopViaHTTP (#2549) already produces, exactly mirroring the
// NestJS/supertest distinction in #4351.
//
// Pipeline (all REAL passes, byte-copies of upvate_core sources):
//  1. ApplyDjangoDRFRoutes over byte-copied core/routers.py + the ScheduleViewset
//     trim → the real http_endpoint entities (incl. POST /schedule/import).
//  2. The real pytest extractor over byte-copied core/tests/test_schedule_import.py
//     → the one-per-file test_suite carrying e2e_route_calls
//     (POST /schedule/import/).
//  3. ResolveHTTPEndpointHandlers over the merged set → migrates http_endpoint to
//     http_endpoint_definition, builds the endpoint index, and runs the shared
//     linkE2ERouteTestsToEndpoints pass.
//
// BEFORE #4369 the suite carried no e2e_route_calls, so no TESTS→endpoint edge
// existed. AFTER, the suite links to exactly POST /schedule/import.
func TestIssue4369_PythonE2ERouteTestsLinkToEndpoints(t *testing.T) {
	root := filepath.Join("..", "custom", "python", "testdata", "issue4369")
	reader := func(p string) []byte {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			return nil
		}
		return b
	}

	// 1. Real DRF route synthesis.
	endpointFiles := []string{"core/routers.py", "core/views/schedule_viewset.py"}
	defs := ApplyDjangoDRFRoutes(endpointFiles, reader)
	if len(defs) == 0 {
		t.Fatal("DRF synthesis produced no endpoints")
	}

	// 2. Real pytest extractor over the real DRF test file → the suite.
	pe, ok := extreg.Get("python_pytest")
	if !ok {
		t.Fatal("python_pytest not registered")
	}
	testSrc := reader("core/tests/test_schedule_import.py")
	if len(testSrc) == 0 {
		t.Fatal("could not read byte-copied DRF test file")
	}
	suiteEnts, err := pe.Extract(context.Background(), extreg.FileInput{
		Path: "core/tests/test_schedule_import.py", Language: "python", Content: testSrc,
	})
	if err != nil {
		t.Fatalf("pytest extract: %v", err)
	}
	var suite *types.EntityRecord
	for i := range suiteEnts {
		if suiteEnts[i].Subtype == "test_suite" {
			suite = &suiteEnts[i]
			break
		}
	}
	if suite == nil {
		t.Fatal("pytest extractor emitted no test_suite")
	}
	if suite.Properties["e2e_route_calls"] == "" {
		t.Fatal("suite carries no e2e_route_calls — extractor side regressed (#4369)")
	}

	// Build the merged set: every real endpoint + the real suite.
	merged := make([]types.EntityRecord, 0, len(defs)+1)
	merged = append(merged, defs...)
	merged = append(merged, *suite)

	// BEFORE control: strip e2e_route_calls — no TESTS→endpoint edges.
	before := make([]types.EntityRecord, len(merged))
	copy(before, merged)
	beforeSuite := *suite
	beforeProps := map[string]string{}
	for k, v := range suite.Properties {
		if k != "e2e_route_calls" {
			beforeProps[k] = v
		}
	}
	beforeSuite.Properties = beforeProps
	before[len(before)-1] = beforeSuite
	beforeOut, beforeStats := ResolveHTTPEndpointHandlers(before)
	if beforeStats.E2ERouteTestEdges != 0 {
		t.Fatalf("control (no e2e_route_calls) E2ERouteTestEdges=%d, want 0", beforeStats.E2ERouteTestEdges)
	}
	if got := countSuiteEndpointTestsEdges(beforeOut); got != 0 {
		t.Fatalf("control must emit 0 suite→endpoint TESTS edges, got %d", got)
	}

	// AFTER: the route calls drive a TESTS edge to POST /schedule/import.
	afterOut, afterStats := ResolveHTTPEndpointHandlers(merged)
	if afterStats.E2ERouteTestEdges == 0 {
		t.Fatalf("expected >=1 e2e route TESTS edge from the Python suite, got 0")
	}

	// The edge must target the POST /schedule/import endpoint definition.
	wantTarget := false
	for _, e := range afterOut {
		if e.Subtype != "test_suite" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != string(types.RelationshipKindTests) {
				continue
			}
			if r.Properties["match_source"] != "e2e_supertest_route" {
				continue
			}
			// ToID is "<defKind>:<defName>"; defName is the SyntheticID
			// "http:POST:/schedule/import".
			if strings.Contains(r.ToID, "POST:/schedule/import") {
				wantTarget = true
			}
			// Framework attributed from the suite, not hard-coded jest.
			if fw := r.Properties["framework"]; fw == "jest" {
				t.Errorf("Python suite TESTS edge mislabeled framework=%q", fw)
			}
		}
	}
	if !wantTarget {
		t.Fatalf("no TESTS edge from the Python suite to POST /schedule/import (edges=%d)",
			afterStats.E2ERouteTestEdges)
	}

	t.Logf("#4369 endpoint-level TESTS edges from real DRF suite: before=0 after=%d",
		afterStats.E2ERouteTestEdges)
}

// countSuiteEndpointTestsEdges counts e2e route-test TESTS edges (match_source
// e2e_supertest_route) emitted from any test_suite to an endpoint definition.
func countSuiteEndpointTestsEdges(ents []types.EntityRecord) int {
	n := 0
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindTests) &&
				r.Properties["match_source"] == "e2e_supertest_route" {
				n++
			}
		}
	}
	return n
}
