// Tests for ApplyTestsMultiHopViaHTTP — #2549.
//
// Verifies that the multi-hop TESTS propagation pass synthesises TESTS edges
// from test functions through HTTP client call sites → ROUTES_TO → ViewSet
// method, and that it does NOT synthesise spurious edges when no matching
// route exists.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// testFooViewsSrc is a minimal views.py that defines FooView.create. In a
// real index this file would be processed by the DRF extractor and generate
// a ViewSet entity; here we only need the ROUTES_TO relationship below.
const testFooViewsSrc = `from rest_framework.viewsets import ModelViewSet

class FooView(ModelViewSet):
    """A simple viewset."""

    def create(self, request, *args, **kwargs):
        return super().create(request, *args, **kwargs)
`

// testFooTestSrc is the Django REST test file that exercises FooView via the
// test client at the registered URL prefix /api/v1/foo.
const testFooTestSrc = `import pytest
from rest_framework.test import APITestCase

class TestFooCreate(APITestCase):
    def test_create_foo(self):
        response = self.client.post('/api/v1/foo', {'name': 'bar'}, format='json')
        self.assertEqual(response.status_code, 201)

    def test_list_foo(self):
        response = self.client.get('/api/v1/foo')
        self.assertEqual(response.status_code, 200)
`

// routesToFoo is a minimal ROUTES_TO relationship emitted by the DRF router
// pass.  FromID uses the "Route:" prefix convention; ToID is the ViewSet stub
// in "View:" prefix convention.
var routesToFoo = []types.RelationshipRecord{
	{
		FromID: "Route:/api/v1/foo",
		ToID:   "View:FooView",
		Kind:   "ROUTES_TO",
		Properties: map[string]string{
			"pattern_type": "ast_driven",
		},
	},
}

// ---------------------------------------------------------------------------
// TestTestsEdges_MultiHopViaHttpClient
//
// Happy-path: test file calls self.client.post('/api/v1/foo'), ROUTES_TO
// maps /api/v1/foo → View:FooView.  Expect at least one TESTS edge from the
// enclosing test function to View:FooView tagged with via=http_router.
// ---------------------------------------------------------------------------

func TestTestsEdges_MultiHopViaHttpClient(t *testing.T) {
	paths := []string{
		"app/views.py",
		"app/tests/test_foo.py",
	}
	content := map[string][]byte{
		"app/views.py":          []byte(testFooViewsSrc),
		"app/tests/test_foo.py": []byte(testFooTestSrc),
	}
	reader := func(p string) []byte { return content[p] }

	edges := ApplyTestsMultiHopViaHTTP(paths, reader, routesToFoo)

	if len(edges) == 0 {
		t.Fatal("expected at least one synthesised TESTS edge, got none")
	}

	// Verify at least one edge targets View:FooView with the right metadata.
	var found bool
	for _, e := range edges {
		if e.Kind != "TESTS" {
			t.Errorf("unexpected edge Kind %q (want TESTS)", e.Kind)
			continue
		}
		if e.ToID != "View:FooView" {
			continue
		}
		if e.Properties["via"] != "http_router" {
			t.Errorf("expected via=http_router, got %q", e.Properties["via"])
		}
		if e.Properties["pattern_type"] != "tests_multi_hop_http_router" {
			t.Errorf("expected pattern_type=tests_multi_hop_http_router, got %q", e.Properties["pattern_type"])
		}
		if e.Properties["confidence"] != "high" {
			t.Errorf("expected confidence=high, got %q", e.Properties["confidence"])
		}
		found = true
	}

	if !found {
		t.Errorf("no TESTS edge targeting View:FooView found; edges emitted: %+v", edges)
	}

	// Verify that both test functions (POST and GET) each contributed an edge.
	testFuncs := map[string]bool{}
	for _, e := range edges {
		if e.ToID == "View:FooView" {
			testFuncs[e.Properties["test_function"]] = true
		}
	}
	if !testFuncs["test_create_foo"] {
		t.Errorf("expected TESTS edge from test_create_foo, got test_funcs=%v", testFuncs)
	}
	if !testFuncs["test_list_foo"] {
		t.Errorf("expected TESTS edge from test_list_foo, got test_funcs=%v", testFuncs)
	}
}

// ---------------------------------------------------------------------------
// TestTestsEdges_NoHopWhenRouteAbsent
//
// Negative case: test calls a path for which NO ROUTES_TO edge exists.
// The pass must emit zero edges (no phantom nodes, no dangling refs).
// ---------------------------------------------------------------------------

const testBarTestSrc = `import pytest
from rest_framework.test import APITestCase

class TestBarEndpoints(APITestCase):
    def test_call_unknown_endpoint(self):
        # /api/v1/nonexistent has no registered viewset.
        response = self.client.post('/api/v1/nonexistent', {})
        self.assertEqual(response.status_code, 404)
`

func TestTestsEdges_NoHopWhenRouteAbsent(t *testing.T) {
	paths := []string{
		"app/tests/test_bar.py",
	}
	content := map[string][]byte{
		"app/tests/test_bar.py": []byte(testBarTestSrc),
	}
	reader := func(p string) []byte { return content[p] }

	// routesToFoo maps /api/v1/foo, NOT /api/v1/nonexistent.
	edges := ApplyTestsMultiHopViaHTTP(paths, reader, routesToFoo)

	if len(edges) != 0 {
		t.Errorf("expected 0 synthesised edges when route is absent, got %d: %+v", len(edges), edges)
	}
}

// ---------------------------------------------------------------------------
// Additional unit tests for helpers
// ---------------------------------------------------------------------------

func TestNormaliseHTTPPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/api/v1/foo/", "/api/v1/foo"},
		{"/api/v1/foo", "/api/v1/foo"},
		{"//api//v1//foo//", "/api/v1/foo"},
		{"/", "/"},
		{"", "/"},
		{"/API/V1/Foo", "/api/v1/foo"},
	}
	for _, tc := range cases {
		got := normaliseHTTPPath(tc.in)
		if got != tc.want {
			t.Errorf("normaliseHTTPPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsTestFilePath(t *testing.T) {
	yes := []string{
		"tests/test_foo.py",
		"app/tests/test_schedule_import.py",
		"foo_test.py",
		"bar_test.py",
	}
	no := []string{
		"views.py",
		"models.py",
		"urls.py",
		"test_results.json",
	}
	for _, p := range yes {
		if !isTestFilePath(p) {
			t.Errorf("isTestFilePath(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if isTestFilePath(p) {
			t.Errorf("isTestFilePath(%q) = true, want false", p)
		}
	}
}

func TestBuildRoutesToIndex(t *testing.T) {
	rels := []types.RelationshipRecord{
		{Kind: "ROUTES_TO", FromID: "Route:/api/v1/users", ToID: "View:UserViewSet"},
		{Kind: "ROUTES_TO", FromID: "Route:/api/v1/orders/", ToID: "View:OrderViewSet"},
		{Kind: "CALLS", FromID: "Foo", ToID: "Bar"}, // must be ignored
		{Kind: "ROUTES_TO", FromID: "http:POST:/api/v1/items", ToID: "View:ItemViewSet"},
	}
	idx := buildRoutesToIndex(rels)

	if ids, ok := idx["/api/v1/users"]; !ok || len(ids) == 0 || ids[0] != "View:UserViewSet" {
		t.Errorf("Route: prefix not indexed correctly; got %v", idx)
	}
	// Trailing-slash path is normalised.
	if ids, ok := idx["/api/v1/orders"]; !ok || len(ids) == 0 || ids[0] != "View:OrderViewSet" {
		t.Errorf("trailing-slash path not normalised; got %v", idx)
	}
	// http: prefix extraction.
	if ids, ok := idx["/api/v1/items"]; !ok || len(ids) == 0 || ids[0] != "View:ItemViewSet" {
		t.Errorf("http: prefix not indexed correctly; got %v", idx)
	}
	// CALLS edge must not appear.
	if _, ok := idx["Bar"]; ok {
		t.Errorf("non-ROUTES_TO edge was indexed")
	}
}

func TestEnclosingPyTestFunc(t *testing.T) {
	src := `import pytest

class FooTest:
    def test_alpha(self):
        self.client.post('/foo')
        self.client.get('/bar')

    def test_beta(self):
        self.client.delete('/baz')
`
	// Position inside test_alpha body (after "self.client.post('/foo')").
	posAlpha := len("import pytest\n\nclass FooTest:\n    def test_alpha(self):\n        self.client.post('/foo')\n")
	got := enclosingPyTestFunc(src, posAlpha-1)
	if got != "test_alpha" {
		t.Errorf("enclosingPyTestFunc for posAlpha=%d: got %q, want test_alpha", posAlpha, got)
	}

	// Position inside test_beta body.
	posBeta := len(src) - len("        self.client.delete('/baz')\n") - 5
	got = enclosingPyTestFunc(src, posBeta)
	if got != "test_beta" {
		t.Errorf("enclosingPyTestFunc for posBeta=%d: got %q, want test_beta", posBeta, got)
	}
}

func TestApplyTestsMultiHop_NilFileReader(t *testing.T) {
	edges := ApplyTestsMultiHopViaHTTP([]string{"test_foo.py"}, nil, routesToFoo)
	if len(edges) != 0 {
		t.Errorf("nil reader must return empty; got %d edges", len(edges))
	}
}

func TestApplyTestsMultiHop_EmptyRoutes(t *testing.T) {
	reader := func(p string) []byte { return []byte(testFooTestSrc) }
	edges := ApplyTestsMultiHopViaHTTP([]string{"app/tests/test_foo.py"}, reader, nil)
	if len(edges) != 0 {
		t.Errorf("empty routes must return empty; got %d edges", len(edges))
	}
}

// TestTestsEdges_PathPrefixFallback verifies that a test calling a detail URL
// (/api/v1/foo/42) is matched against the collection route (/api/v1/foo) when
// no exact entry exists in the ROUTES_TO index.
func TestTestsEdges_PathPrefixFallback(t *testing.T) {
	detailTestSrc := `from rest_framework.test import APITestCase

class TestFooDetail(APITestCase):
    def test_retrieve_foo(self):
        response = self.client.get('/api/v1/foo/42')
        self.assertEqual(response.status_code, 200)
`
	paths := []string{"app/tests/test_detail.py"}
	content := map[string][]byte{
		"app/tests/test_detail.py": []byte(detailTestSrc),
	}
	reader := func(p string) []byte { return content[p] }

	edges := ApplyTestsMultiHopViaHTTP(paths, reader, routesToFoo)

	var found bool
	for _, e := range edges {
		if e.ToID == "View:FooView" && e.Properties["test_function"] == "test_retrieve_foo" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected TESTS edge via prefix fallback /api/v1/foo for detail URL /api/v1/foo/42; got %+v", edges)
	}
}
