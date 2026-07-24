package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// TestVapor_BasicRoute covers the common Vapor controller shape:
// app.get("todos") / routes.post("todos") — one literal path component.
func TestVapor_BasicRoute(t *testing.T) {
	src := `
import Vapor

func routes(_ app: Application) throws {
    app.get("todos") { req in "[]" }
    app.post("todos") { req -> Todo in Todo() }
}
`
	ids, _ := runDetect(t, "swift", "routes.swift", src)
	requireContains(t, ids, []string{
		"http:GET:/todos",
		"http:POST:/todos",
	}, "vapor-basic-route")
}

// TestVapor_PathParam covers the variadic component form with a `:param`
// dynamic component: app.get("todos", ":todoID") → GET /todos/{todoID}.
func TestVapor_PathParam(t *testing.T) {
	src := `
import Vapor

struct TodoController: RouteCollection {
    func boot(routes: RoutesBuilder) throws {
        routes.get("todos", ":todoID") { req in Todo() }
        routes.delete("todos", ":todoID") { req in HTTPStatus.ok }
    }
}
`
	ids, _ := runDetect(t, "swift", "TodoController.swift", src)
	requireContains(t, ids, []string{
		"http:GET:/todos/{todoID}",
		"http:DELETE:/todos/{todoID}",
	}, "vapor-path-param")
}

// TestVapor_OnForm covers the explicit `.on(.VERB, components...)` registration.
func TestVapor_OnForm(t *testing.T) {
	src := `
import Vapor

func configure(_ app: Application) throws {
    app.on(.GET, "health") { req in "ok" }
}
`
	ids, _ := runDetect(t, "swift", "configure.swift", src)
	requireContains(t, ids, []string{
		"http:GET:/health",
	}, "vapor-on-form")
}

// TestVapor_NonRouteReceiverIgnored is the negative guard: a `.get(`/`.post(`
// on a NON-routes receiver (e.g. a dictionary) must not forge an endpoint.
func TestVapor_NonRouteReceiverIgnored(t *testing.T) {
	src := `
import Foundation

func lookup(_ cache: [String: String]) -> String? {
    return cache.get("key")
}
`
	ids, _ := runDetect(t, "swift", "cache.swift", src)
	for _, id := range ids {
		if id == "http:GET:/key" {
			t.Fatalf("negative: cache.get(\"key\") must not synthesize a Vapor endpoint; got %v", ids)
		}
	}
}

// TestVapor_E2ERouteTestLinkage is the end-to-end RED→GREEN proof (#4749
// validation B). A Vapor route GET /todos is synthesized into an
// http_endpoint_definition; an XCTVapor test_suite carrying
// `e2e_route_calls = "GET /todos"` must yield a TESTS edge from the suite to
// that endpoint via the shared linkE2ERouteTestsToEndpoints pass.
func TestVapor_E2ERouteTestLinkage(t *testing.T) {
	def := types.EntityRecord{
		Kind:       httpEndpointDefinitionKind,
		Name:       "http:GET:/todos",
		SourceFile: "Sources/App/Controllers/TodoController.swift",
		Language:   "swift",
		Properties: map[string]string{
			"verb":         "GET",
			"path":         "/todos",
			"framework":    "vapor",
			"pattern_type": "http_endpoint_synthesis",
		},
	}
	suite := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		Name:       "TodoControllerTests",
		SourceFile: "Tests/AppTests/TodoControllerTests.swift",
		Language:   "swift",
		Properties: map[string]string{
			"framework":       "xctvapor",
			"e2e_route_calls": "GET /todos",
		},
	}

	merged := []types.EntityRecord{def, suite}
	resolved, stats := ResolveHTTPEndpointHandlers(merged)

	if stats.E2ERouteTestEdges < 1 {
		t.Fatalf("expected >=1 e2e route-test edge for Vapor GET /todos; got %d", stats.E2ERouteTestEdges)
	}

	// Confirm the TESTS edge lands on the suite and targets the endpoint.
	found := false
	for i := range resolved {
		if resolved[i].Name != "TodoControllerTests" {
			continue
		}
		for _, r := range resolved[i].Relationships {
			if r.Kind == string(types.RelationshipKindTests) &&
				r.Properties.Get("match_source") == "e2e_supertest_route" &&
				r.Properties.Get("route") == "/todos" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected TESTS edge from TodoControllerTests suite to GET /todos endpoint")
	}
}
