package engine

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	// Register the Java custom extractor so the test_suite (with e2e_route_calls)
	// comes from the REAL extractor, not a hand-built fixture.
	_ "github.com/cajasmota/archigraph/internal/custom/java"
)

// Issue #4370 LIVE-REPRO (resolve side, full in-pipeline).
//
// Proves end-to-end that a Spring integration test calling a route by string
// (MockMvc / WebTestClient / TestRestTemplate / REST Assured) links to the
// http_endpoint_definition it exercises — the finer-grained endpoint-level
// TESTS edge that complements the controller-class TESTS edge ExtractJUnit5
// (#4359) already produces, mirroring the NestJS/supertest (#4351) and Python
// (#4369) distinctions.
//
// Pipeline (all REAL passes, faithful Spring fixtures):
//  1. ApplyJavaAnnotationRoutes over a real @RestController with a
//     class-level @RequestMapping("/inspections") base + @PostMapping("/{id}/items")
//     / @GetMapping("/{id}") methods → the real http_endpoint entities.
//  2. The real Java custom extractor over a @SpringBootTest MockMvc test class
//     → the one-per-file test_suite carrying e2e_route_calls.
//  3. ResolveHTTPEndpointHandlers over the merged set → migrates http_endpoint
//     to http_endpoint_definition, builds the endpoint index, and runs the
//     shared linkE2ERouteTestsToEndpoints pass.
//
// BEFORE #4370 the suite carried no e2e_route_calls, so no TESTS→endpoint edge
// existed. AFTER, the suite links to the matching endpoint definitions, with the
// servlet base-path prefix and {id}-template segments resolved.

const springControllerSrc4370 = `package com.example.web;

import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestBody;

@RestController
@RequestMapping("/api/v1/inspections")
public class InspectionController {

    @PostMapping("/{id}/items")
    public Item createItem(@PathVariable Long id, @RequestBody Item body) {
        return body;
    }

    @GetMapping("/{id}")
    public Inspection getOne(@PathVariable Long id) {
        return null;
    }
}
`

const springMockMvcTestSrc4370 = `package com.example.web;

import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.WebMvcTest;
import org.springframework.test.web.servlet.MockMvc;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;

@WebMvcTest(InspectionController.class)
class InspectionControllerTest {

    @Autowired
    MockMvc mockMvc;

    @Test
    void createsItem() throws Exception {
        mockMvc.perform(post("/api/v1/inspections/123/items").content("{}"))
               .andExpect(status().isCreated());
    }

    @Test
    void getsOne() throws Exception {
        mockMvc.perform(get("/api/v1/inspections/123"))
               .andExpect(status().isOk());
    }
}
`

func TestIssue4370_SpringE2ERouteTestsLinkToEndpoints(t *testing.T) {
	// 1. Real Spring route synthesis from the @RestController.
	files := map[string]string{
		"src/main/java/com/example/web/InspectionController.java": springControllerSrc4370,
	}
	reader := func(p string) []byte {
		if s, ok := files[p]; ok {
			return []byte(s)
		}
		return nil
	}
	defs := ApplyJavaAnnotationRoutes(
		[]string{"src/main/java/com/example/web/InspectionController.java"}, reader)
	if len(defs) == 0 {
		t.Fatal("Spring annotation route synthesis produced no endpoints")
	}

	// 2. Real Java custom extractor over the MockMvc test → the suite.
	je, ok := extreg.Get("custom_java_patterns")
	if !ok {
		t.Fatal("custom_java_patterns not registered")
	}
	suiteEnts, err := je.Extract(context.Background(), extreg.FileInput{
		Path:     "src/test/java/com/example/web/InspectionControllerTest.java",
		Language: "java",
		Content:  []byte(springMockMvcTestSrc4370),
	})
	if err != nil {
		t.Fatalf("java extract: %v", err)
	}
	var suite *types.EntityRecord
	for i := range suiteEnts {
		if suiteEnts[i].Subtype == "test_suite" {
			suite = &suiteEnts[i]
			break
		}
	}
	if suite == nil {
		t.Fatal("Java extractor emitted no test_suite")
	}
	if suite.Properties["e2e_route_calls"] == "" {
		t.Fatal("suite carries no e2e_route_calls — extractor side regressed (#4370)")
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

	// AFTER: the route calls drive TESTS edges to the matching endpoints.
	afterOut, afterStats := ResolveHTTPEndpointHandlers(merged)
	if afterStats.E2ERouteTestEdges == 0 {
		t.Fatalf("expected >=1 e2e route TESTS edge from the Spring suite, got 0")
	}

	// The edges must target POST /api/v1/inspections/{id}/items and
	// GET /api/v1/inspections/{id} (concrete /123 in the test → {id} template).
	wantPost, wantGet := false, false
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
			to := r.ToID
			if strings.Contains(to, "POST:/api/v1/inspections/{id}/items") {
				wantPost = true
			}
			if strings.Contains(to, "GET:/api/v1/inspections/{id}") &&
				!strings.Contains(to, "items") {
				wantGet = true
			}
			// Framework attributed from the JUnit suite, not hard-coded jest.
			if fw := r.Properties["framework"]; fw == "jest" {
				t.Errorf("Spring suite TESTS edge mislabeled framework=%q", fw)
			}
		}
	}
	if !wantPost {
		t.Errorf("no TESTS edge from the Spring suite to POST /api/v1/inspections/{id}/items")
	}
	if !wantGet {
		t.Errorf("no TESTS edge from the Spring suite to GET /api/v1/inspections/{id}")
	}

	t.Logf("#4370 endpoint-level TESTS edges from real Spring MockMvc suite: before=0 after=%d",
		afterStats.E2ERouteTestEdges)
}
