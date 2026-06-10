package javascript

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// Issue #4600 — data-driven supertest route tables.
//
// Contract / e2e specs frequently drive supertest through a COMPUTED verb member
// access over a `{ method, path }` descriptor table rather than a literal
// `request(app).get('/path')` call, e.g.
//
//	const BASE = '/api/v1/buildings';
//	const cases = [
//	  { method: 'GET',    path: `${BASE}/lite` },
//	  { method: 'POST',   path: `${BASE}/notes/create` },
//	  { method: 'DELETE', path: `${BASE}/7/notes/delete` },
//	];
//	it.each(cases)('…', ({ method, path }) => {
//	  request(app.getHttpServer())[method.toLowerCase()](path);
//	});
//
// The `.get(/.post(`-anchored verb-call regex cannot see `[method.toLowerCase()]`
// and the describe label carries no `VERB /route`, so before this fix the
// extractor stamped an EMPTY `e2e_route_calls` and the endpoints these specs
// exercise looked untested. extractRouteTableCalls recovers the (verb, route)
// pairs straight from the table, folding the `${BASE}` const.
func TestIssue4600_RouteTableExtraction(t *testing.T) {
	const src = "const BASE = '/api/v1/buildings';\n" +
		"describe('contract: building notes auth', () => {\n" +
		"  const cases = [\n" +
		"    { method: 'GET',    path: `${BASE}/lite`, action: 'lite' },\n" +
		"    { method: 'POST',   path: `${BASE}/notes/create`, action: 'create_note' },\n" +
		"    { method: 'GET',    path: `${BASE}/42/notes`, action: 'get_notes' },\n" +
		"    { method: 'DELETE', path: `${BASE}/7/notes/delete`, action: 'delete_note' },\n" +
		"  ];\n" +
		"  it.each(cases)('%s requires auth', ({ method, path }) => {\n" +
		"    const req = request(app.getHttpServer())[method.toLowerCase()](path);\n" +
		"    return req;\n" +
		"  });\n" +
		"});\n"

	e := &jestExtractor{}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     "test/contract/buildings/building-notes-auth.contract.spec.ts",
		Language: "typescript",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(ents) == 0 {
		t.Fatal("expected at least one test_suite entity")
	}

	var got string
	for _, en := range ents {
		if en.Properties != nil && en.Properties["e2e_route_calls"] != "" {
			got = en.Properties["e2e_route_calls"]
			break
		}
	}
	if got == "" {
		t.Fatal("expected e2e_route_calls to be stamped from the route table, got empty (RED before #4600)")
	}

	want := []string{
		"GET /api/v1/buildings/lite",
		"POST /api/v1/buildings/notes/create",
		"GET /api/v1/buildings/42/notes",
		"DELETE /api/v1/buildings/7/notes/delete",
	}
	for _, w := range want {
		if !strings.Contains("\n"+got+"\n", "\n"+w+"\n") {
			t.Fatalf("missing route-table call %q in e2e_route_calls:\n%s", w, got)
		}
	}
}

// TestIssue4600_RouteTableConservatism — an object that lacks a real HTTP method
// value, or whose path is not /-rooted, contributes no pair (no fabricated edge).
func TestIssue4600_RouteTableConservatism(t *testing.T) {
	const src = "describe('x', () => {\n" +
		"  const cases = [\n" +
		"    { method: 'CONNECT', path: '/api/v1/foo' },\n" + // not an indexed verb
		"    { method: 'GET', path: 'relative/no/slash' },\n" + // not /-rooted
		"    { method: 'GET', path: 'https://third-party.example/x' },\n" + // absolute URL
		"  ];\n" +
		"  it('noop', () => { expect(true).toBe(true); });\n" +
		"});\n"

	got := extractRouteTableCalls(src)
	if len(got) != 0 {
		t.Fatalf("expected no route-table pairs from non-route entries, got %v", got)
	}
}
