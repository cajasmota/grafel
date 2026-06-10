package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #4600 — data-driven supertest route tables credit coverage end-to-end.
//
// A contract spec drives supertest through a computed verb over a
// `{ method, path }` table; the Jest extractor (after #4600) folds those into
// `e2e_route_calls`. This engine test proves the EXISTING resolve pass then
// links each table-derived route to the synthesized NestJS endpoint, so the
// endpoint's Tests panel is no longer (0). Routes carry the concrete `42`/`7`
// path params and the `/api/v1` mount prefix; the definitions are templated
// (`{buildingId}`) — the concrete-vs-template segment matcher must align them.
func TestIssue4600_RouteTableLinksToEndpoints(t *testing.T) {
	defs := []types.EntityRecord{
		def("GET", "/api/v1/buildings/lite"),
		def("POST", "/api/v1/buildings/notes/create"),
		def("GET", "/api/v1/buildings/{buildingId}/notes"),
		def("DELETE", "/api/v1/buildings/{buildingId}/notes/delete"),
		// Unrelated endpoint that must NOT be linked.
		def("GET", "/api/v1/proposals/get_counts"),
	}

	// e2e_route_calls exactly as the Jest extractor now stamps them from the
	// route table (concrete IDs from `${BASE}/42/notes`, `${BASE}/7/notes/...`).
	suite := e2eSuite("GET /api/v1/buildings/lite\n" +
		"POST /api/v1/buildings/notes/create\n" +
		"GET /api/v1/buildings/42/notes\n" +
		"DELETE /api/v1/buildings/7/notes/delete")

	out, stats := ResolveHTTPEndpointHandlers(append(append([]types.EntityRecord{}, defs...), suite))
	edges := testsEdgesToEndpoints(out)
	if stats.E2ERouteTestEdges != 4 {
		t.Fatalf("expected 4 route-table TESTS edges, got %d (edges=%v)", stats.E2ERouteTestEdges, edges)
	}

	linked := map[string]bool{}
	for _, e := range edges {
		linked[e.ToID] = true
	}
	want := []string{
		"http_endpoint_definition:http:GET:/api/v1/buildings/lite",
		"http_endpoint_definition:http:POST:/api/v1/buildings/notes/create",
		"http_endpoint_definition:http:GET:/api/v1/buildings/{buildingId}/notes",
		"http_endpoint_definition:http:DELETE:/api/v1/buildings/{buildingId}/notes/delete",
	}
	for _, w := range want {
		if !linked[w] {
			t.Fatalf("missing TESTS edge to %q (got %v)", w, linked)
		}
	}
	// The unrelated proposals endpoint must stay untested.
	if linked["http_endpoint_definition:http:GET:/api/v1/proposals/get_counts"] {
		t.Fatal("proposals endpoint must not be linked by the buildings route table")
	}
}
