package engine

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	// Register the Go + Ruby custom extractors so the test_suite (carrying
	// e2e_route_calls) comes from the REAL extractors, not a hand-built fixture.
	_ "github.com/cajasmota/archigraph/internal/custom/golang"
	_ "github.com/cajasmota/archigraph/internal/custom/ruby"
)

// Issue #4371 LIVE-REPRO (resolve side, full in-pipeline) — Go + Rails.
//
// Proves end-to-end that a Go httptest / Rails request spec calling a route by
// string links to the http_endpoint_definition it exercises, the finer-grained
// endpoint-level TESTS edge that complements the SUT-class edge — mirroring the
// NestJS/supertest (#4351), Python (#4369), and Java/Spring (#4370) work.
//
// Pipeline (all REAL passes, faithful fixtures):
//  1. applyHTTPEndpointSynthesis over the real router/handler source (Go
//     net/http mux registration; Rails config/routes.rb) → http_endpoint_*.
//  2. The real custom extractor over the test file → the one-per-file
//     test_suite carrying e2e_route_calls.
//  3. ResolveHTTPEndpointHandlers over the merged set → migrates the endpoints
//     and runs the shared linkE2ERouteTestsToEndpoints pass.
//
// BEFORE #4371 the suite carried no e2e_route_calls, so no TESTS→endpoint edge
// existed. AFTER, the suite links to the matching endpoint definitions.

// synthEndpoints drives the REAL per-language endpoint synthesis over one
// production source file and returns the synthesized endpoint entities.
func synthEndpoints(t *testing.T, lang, path, src string) []types.EntityRecord {
	t.Helper()
	res := applyHTTPEndpointSynthesis(DetectorPassArgs{
		Ctx:     context.Background(),
		Lang:    lang,
		Path:    path,
		Content: []byte(src),
	})
	var defs []types.EntityRecord
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind || e.Kind == httpEndpointDefinitionKind {
			defs = append(defs, e)
		}
	}
	return defs
}

// realSuite runs the named custom extractor over a test file and returns the
// emitted test_suite (with e2e_route_calls), failing if none is produced.
func realSuite(t *testing.T, extractorName, path, lang, src string) types.EntityRecord {
	t.Helper()
	ex, ok := extreg.Get(extractorName)
	if !ok {
		t.Fatalf("%s not registered", extractorName)
	}
	ents, err := ex.Extract(context.Background(), extreg.FileInput{
		Path: path, Language: lang, Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("%s extract: %v", extractorName, err)
	}
	for _, e := range ents {
		if e.Subtype == "test_suite" && e.Properties["e2e_route_calls"] != "" {
			return e
		}
	}
	t.Fatalf("%s emitted no test_suite with e2e_route_calls", extractorName)
	return types.EntityRecord{}
}

// runE2ERouteResolve runs the full RED→GREEN check: BEFORE (e2e_route_calls
// stripped) must yield 0 endpoint-TESTS edges; AFTER yields >=1. Returns the
// resolved AFTER entities and the after-stats edge count.
func runE2ERouteResolve(t *testing.T, defs []types.EntityRecord, suite types.EntityRecord) ([]types.EntityRecord, int) {
	t.Helper()
	if len(defs) == 0 {
		t.Fatal("no endpoints synthesized — fixture/route-synthesis regressed")
	}
	merged := make([]types.EntityRecord, 0, len(defs)+1)
	merged = append(merged, defs...)
	merged = append(merged, suite)

	// BEFORE control: strip e2e_route_calls → no TESTS→endpoint edges.
	before := make([]types.EntityRecord, len(merged))
	copy(before, merged)
	beforeSuite := suite
	bp := map[string]string{}
	for k, v := range suite.Properties {
		if k != "e2e_route_calls" {
			bp[k] = v
		}
	}
	beforeSuite.Properties = bp
	before[len(before)-1] = beforeSuite
	beforeOut, beforeStats := ResolveHTTPEndpointHandlers(before)
	if beforeStats.E2ERouteTestEdges != 0 {
		t.Fatalf("control E2ERouteTestEdges=%d, want 0", beforeStats.E2ERouteTestEdges)
	}
	if got := countSuiteEndpointTestsEdges(beforeOut); got != 0 {
		t.Fatalf("control must emit 0 suite→endpoint TESTS edges, got %d", got)
	}

	afterOut, afterStats := ResolveHTTPEndpointHandlers(merged)
	if afterStats.E2ERouteTestEdges == 0 {
		t.Fatalf("expected >=1 e2e route TESTS edge, got 0")
	}
	return afterOut, afterStats.E2ERouteTestEdges
}

// edgeTargets returns the ToIDs of every e2e route-test TESTS edge from a suite.
func edgeTargets(ents []types.EntityRecord) map[string]bool {
	out := map[string]bool{}
	for _, e := range ents {
		if e.Subtype != "test_suite" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindTests) &&
				r.Properties["match_source"] == "e2e_supertest_route" {
				out[r.ToID] = true
			}
		}
	}
	return out
}

// ── Go httptest ────────────────────────────────────────────────────────────

const goRouterSrc4371 = `package web

import "net/http"

func NewRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /inspections/{id}/items", createItem)
	mux.HandleFunc("GET /inspections/{id}", getOne)
	return mux
}

func createItem(w http.ResponseWriter, r *http.Request) {}
func getOne(w http.ResponseWriter, r *http.Request)     {}
`

const goHTTPTestSrc4371Pipeline = `package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateItem(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/inspections/123/items", nil)
	rec := httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, req)
}

func TestGetOne(t *testing.T) {
	req, _ := http.NewRequest("GET", "/inspections/123", nil)
	w := httptest.NewRecorder()
	NewRouter().ServeHTTP(w, req)
}
`

func TestIssue4371_GoHTTPTestE2ERouteTestsLinkToEndpoints(t *testing.T) {
	defs := synthEndpoints(t, "go", "internal/web/router.go", goRouterSrc4371)
	suite := realSuite(t, "custom_go_httptest_e2e",
		"internal/web/handler_test.go", "go", goHTTPTestSrc4371Pipeline)

	afterOut, edges := runE2ERouteResolve(t, defs, suite)
	targets := edgeTargets(afterOut)

	wantPost, wantGet := false, false
	for to := range targets {
		if strings.Contains(to, "POST:/inspections/{id}/items") {
			wantPost = true
		}
		if strings.Contains(to, "GET:/inspections/{id}") && !strings.Contains(to, "items") {
			wantGet = true
		}
	}
	if !wantPost {
		t.Errorf("no TESTS edge to POST /inspections/{id}/items (targets=%v)", targets)
	}
	if !wantGet {
		t.Errorf("no TESTS edge to GET /inspections/{id} (targets=%v)", targets)
	}
	t.Logf("#4371 Go httptest endpoint-level TESTS edges: before=0 after=%d", edges)
}

// ── Rails request specs ─────────────────────────────────────────────────────

const railsRoutesSrc4371 = `Rails.application.routes.draw do
  get    '/inspections/:id',       to: 'inspections#show'
  post   '/inspections/:id/items', to: 'items#create'
  patch  '/inspections/:id',       to: 'inspections#update'
end
`

const railsRequestSpecSrc4371Pipeline = `require 'rails_helper'

RSpec.describe 'Inspections API', type: :request do
  it 'shows one' do
    get '/inspections/123'
    expect(response).to have_http_status(:ok)
  end

  it 'creates an item' do
    post '/inspections/123/items', params: { name: 'x' }
    expect(response).to have_http_status(:created)
  end
end
`

func TestIssue4371_RailsRequestSpecE2ERouteTestsLinkToEndpoints(t *testing.T) {
	defs := synthEndpoints(t, "ruby", "config/routes.rb", railsRoutesSrc4371)
	suite := realSuite(t, "custom_ruby_rspec",
		"spec/requests/inspections_spec.rb", "ruby", railsRequestSpecSrc4371Pipeline)

	afterOut, edges := runE2ERouteResolve(t, defs, suite)
	targets := edgeTargets(afterOut)

	wantShow, wantCreate := false, false
	for to := range targets {
		// Rails synthesizes :id-style template segments; the resolver wildcards
		// them, so the concrete /123 test routes match the templates.
		if strings.Contains(to, "GET:/inspections/") && !strings.Contains(to, "items") {
			wantShow = true
		}
		if strings.Contains(to, "POST:/inspections/") && strings.Contains(to, "items") {
			wantCreate = true
		}
	}
	if !wantShow {
		t.Errorf("no TESTS edge to GET /inspections/:id (targets=%v)", targets)
	}
	if !wantCreate {
		t.Errorf("no TESTS edge to POST /inspections/:id/items (targets=%v)", targets)
	}
	t.Logf("#4371 Rails request-spec endpoint-level TESTS edges: before=0 after=%d", edges)
}
