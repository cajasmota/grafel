package engine

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	// Register the browser-e2e extractor so the suite (with e2e_route_calls)
	// comes from the REAL extractor, not a hand-built fixture.
	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
)

// Issue #4399 LIVE-REPRO (resolve side, full in-pipeline).
//
// Proves end-to-end that a Playwright `request.post('/api/users')` and a Cypress
// `cy.request('GET', '/api/users')` browser-e2e API call link to the
// http_endpoint_definition they exercise via the shared
// linkE2ERouteTestsToEndpoints pass (#4351) — and that an interpolated URL
// yields NO edge. The suite is produced by the REAL custom_js_tests_route_e2e
// extractor; only the endpoint definitions are hand-built (the engine pass is
// framework-agnostic and only needs the definition index).
func TestIssue4399_BrowserE2ERouteTestsLinkToEndpoints(t *testing.T) {
	defPost := types.EntityRecord{
		Kind:       httpEndpointDefinitionKind,
		Name:       "http:POST:/api/users",
		SourceFile: "src/users/users.controller.ts",
		Language:   "typescript",
		Properties: map[string]string{
			"verb": "POST", "path": "/api/users", "framework": "nestjs",
			"pattern_type": "http_endpoint_synthesis",
		},
	}
	defGet := types.EntityRecord{
		Kind:       httpEndpointDefinitionKind,
		Name:       "http:GET:/api/users",
		SourceFile: "src/users/users.controller.ts",
		Language:   "typescript",
		Properties: map[string]string{
			"verb": "GET", "path": "/api/users", "framework": "nestjs",
			"pattern_type": "http_endpoint_synthesis",
		},
	}

	// Real Playwright spec: one literal POST + an interpolated GET that MUST be
	// dropped.
	pwSpec := "import { test } from '@playwright/test';\n" +
		"test('creates', async ({ request }) => {\n" +
		"  await request.post('/api/users', { data: {} });\n" +
		"});\n" +
		"test('built url skipped', async ({ request }) => {\n" +
		"  await request.get(`${base}/api/users`);\n" +
		"});\n"
	// Real Cypress spec: one literal GET.
	cySpec := "describe('users', () => {\n" +
		"  it('lists', () => { cy.request('GET', '/api/users'); });\n" +
		"});\n"

	ext, ok := extreg.Get("custom_js_tests_route_e2e")
	if !ok {
		t.Fatal("custom_js_tests_route_e2e not registered (dispatch-prefix regression, #4769)")
	}
	pwSuite := extractOneSuite(t, ext, "e2e/users.spec.ts", pwSpec)
	cySuite := extractOneSuite(t, ext, "cypress/e2e/users.cy.ts", cySpec)

	// The Playwright suite must carry ONLY the literal POST (interpolated GET
	// dropped — honest exclusion).
	if calls := pwSuite.Properties["e2e_route_calls"]; calls != "POST /api/users" {
		t.Fatalf("playwright suite e2e_route_calls=%q, want exactly %q (interpolated URL must be dropped)",
			calls, "POST /api/users")
	}
	if calls := cySuite.Properties["e2e_route_calls"]; calls != "GET /api/users" {
		t.Fatalf("cypress suite e2e_route_calls=%q, want %q", calls, "GET /api/users")
	}

	merged := []types.EntityRecord{defPost, defGet, *pwSuite, *cySuite}

	// AFTER: the resolve pass links each suite to exactly its endpoint.
	out, stats := ResolveHTTPEndpointHandlers(merged)
	if stats.E2ERouteTestEdges < 2 {
		t.Fatalf("expected >=2 e2e route TESTS edges (PW POST + Cy GET), got %d", stats.E2ERouteTestEdges)
	}

	gotPost, gotGet := false, false
	for _, e := range out {
		if e.Subtype != "test_suite" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != string(types.RelationshipKindTests) ||
				r.Properties["match_source"] != "e2e_supertest_route" {
				continue
			}
			switch {
			case strings.HasPrefix(e.SourceFile, "e2e/") && r.Properties["verb"] == "POST" &&
				r.Properties["route"] == "/api/users":
				gotPost = true
				if fw := r.Properties["framework"]; fw != "playwright" {
					t.Errorf("playwright suite TESTS edge framework=%q, want playwright", fw)
				}
			case strings.Contains(e.SourceFile, "cypress/") && r.Properties["verb"] == "GET" &&
				r.Properties["route"] == "/api/users":
				gotGet = true
				if fw := r.Properties["framework"]; fw != "cypress" {
					t.Errorf("cypress suite TESTS edge framework=%q, want cypress", fw)
				}
			}
		}
	}
	if !gotPost {
		t.Error("no TESTS edge from the Playwright suite to POST /api/users")
	}
	if !gotGet {
		t.Error("no TESTS edge from the Cypress suite to GET /api/users")
	}

	// The interpolated GET in the Playwright spec must NOT have produced a GET
	// edge from the e2e/ suite.
	for _, e := range out {
		if e.Subtype != "test_suite" || !strings.HasPrefix(e.SourceFile, "e2e/") {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindTests) && r.Properties["verb"] == "GET" {
				t.Fatalf("interpolated GET produced a spurious TESTS edge from the Playwright suite")
			}
		}
	}

	t.Logf("#4399 browser-e2e endpoint TESTS edges: %d", stats.E2ERouteTestEdges)
}

func extractOneSuite(t *testing.T, ext extreg.Extractor, path, src string) *types.EntityRecord {
	t.Helper()
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path: path, Language: "typescript", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	for i := range ents {
		if ents[i].Subtype == "test_suite" {
			return &ents[i]
		}
	}
	t.Fatalf("extractor emitted no test_suite for %s", path)
	return nil
}
