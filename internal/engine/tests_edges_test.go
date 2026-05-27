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
		// test_ prefix convention
		"test_foo.py",
		"test_schedule_import.py",
		// _test suffix convention
		"foo_test.py",
		"bar_test.py",
		// tests/ directory convention (no prefix required)
		"tests/test_foo.py",
		"tests/conftest.py",
		"tests/__init__.py",
		"app/tests/test_schedule_import.py",
		"core/tests/models.py",
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

// ---------------------------------------------------------------------------
// TestSynthesiseRoutesToFromEndpoints (#2570)
//
// Verifies that SynthesiseRoutesToFromEndpoints converts http_endpoint entities
// (emitted by ApplyDjangoDRFRoutes) into ROUTES_TO RelationshipRecords that
// buildRoutesToIndex can consume, enabling Pass 2.8 to produce TESTS edges
// on repositories like upvate that keep router.register() and include() in
// separate files.
// ---------------------------------------------------------------------------

func TestSynthesiseRoutesToFromEndpoints_DRFPattern(t *testing.T) {
	// Simulate entities from ApplyDjangoDRFRoutes for a ScheduleViewset.
	entities := []types.EntityRecord{
		{
			Kind: "http_endpoint_synthesis",
			Properties: map[string]string{
				"verb":            "POST",
				"path":            "/api/v1/schedule",
				"source_handler":  "SCOPE.Operation:ScheduleViewset.create",
				"drf_view_method": "ScheduleViewset.create",
				"pattern_type":    "drf_router_expanded",
				"framework":       "django",
			},
		},
		{
			Kind: "http_endpoint_synthesis",
			Properties: map[string]string{
				"verb":            "GET",
				"path":            "/api/v1/schedule",
				"source_handler":  "SCOPE.Operation:ScheduleViewset.list",
				"drf_view_method": "ScheduleViewset.list",
				"pattern_type":    "drf_router_expanded",
				"framework":       "django",
			},
		},
		// ANY catch-all — no source_handler; must be skipped.
		{
			Kind: "http_endpoint_synthesis",
			Properties: map[string]string{
				"verb":         "ANY",
				"path":         "/api/v1/schedule/{id}",
				"pattern_type": "drf_router_expanded",
				"framework":    "django",
			},
		},
		// Non-endpoint entity — must be ignored.
		{
			Kind: "SCOPE.Operation",
			Properties: map[string]string{
				"path": "/api/v1/schedule",
			},
		},
	}

	rels := SynthesiseRoutesToFromEndpoints(entities)

	if len(rels) != 2 {
		t.Fatalf("expected 2 ROUTES_TO records (POST + GET), got %d: %+v", len(rels), rels)
	}
	for _, r := range rels {
		if r.Kind != "ROUTES_TO" {
			t.Errorf("expected Kind=ROUTES_TO, got %q", r.Kind)
		}
	}

	// Verify the POST record.
	var foundPost bool
	for _, r := range rels {
		if r.FromID == "http:POST:/api/v1/schedule" && r.ToID == "SCOPE.Operation:ScheduleViewset.create" {
			foundPost = true
		}
	}
	if !foundPost {
		t.Errorf("expected POST ROUTES_TO for ScheduleViewset.create; got %+v", rels)
	}
}

// TestSynthesiseRoutesToFromEndpoints_Dedup verifies that duplicate (fromID, toID)
// pairs are deduplicated even when the same endpoint entity appears twice
// (e.g. from both pass2Records and pass3Records being merged via concatRecords).
func TestSynthesiseRoutesToFromEndpoints_Dedup(t *testing.T) {
	e := types.EntityRecord{
		Kind: "http_endpoint_synthesis",
		Properties: map[string]string{
			"verb":           "POST",
			"path":           "/api/v1/import",
			"source_handler": "SCOPE.Operation:ImportViewSet.create",
			"pattern_type":   "drf_router_expanded",
		},
	}
	rels := SynthesiseRoutesToFromEndpoints([]types.EntityRecord{e, e, e})
	if len(rels) != 1 {
		t.Errorf("expected exactly 1 deduplicated ROUTES_TO record, got %d: %+v", len(rels), rels)
	}
}

// TestSynthesiseRoutesToFromEndpoints_EmptyInput ensures the function is a
// no-op when given an empty or nil slice.
func TestSynthesiseRoutesToFromEndpoints_EmptyInput(t *testing.T) {
	if rels := SynthesiseRoutesToFromEndpoints(nil); len(rels) != 0 {
		t.Errorf("nil input must return empty; got %d records", len(rels))
	}
	if rels := SynthesiseRoutesToFromEndpoints([]types.EntityRecord{}); len(rels) != 0 {
		t.Errorf("empty input must return empty; got %d records", len(rels))
	}
}

// TestMultiHop_UpvatePattern is an integration-style test that simulates the
// full Pass 2.8 flow for a repository like upvate: router.register() and
// include() in separate files, so pass2Rels has zero ROUTES_TO but
// pass3Records contains http_endpoint entities from ApplyDjangoDRFRoutes.
//
// The test verifies that when SynthesiseRoutesToFromEndpoints output is merged
// into the routesToRels argument, ApplyTestsMultiHopViaHTTP produces TESTS
// edges for a Django TestCase class method calling self.client.post(...).
func TestMultiHop_UpvatePattern(t *testing.T) {
	// Simulate the http_endpoint entity emitted by ApplyDjangoDRFRoutes.
	drfEndpointEntities := []types.EntityRecord{
		{
			Kind: "http_endpoint_synthesis",
			Properties: map[string]string{
				"verb":            "POST",
				"path":            "/api/v1/import",
				"source_handler":  "SCOPE.Operation:ImportViewSet.create",
				"drf_view_method": "ImportViewSet.create",
				"pattern_type":    "drf_router_expanded",
				"framework":       "django",
			},
		},
	}

	// Simulate a Django TestCase test method calling self.client.post('/api/v1/import').
	const upvateTestSrc = `import io
from rest_framework.test import APIClient, APITestCase

class ImportCSVTest(APITestCase):
    def setUp(self):
        self.client = APIClient()
        self.client.force_authenticate(user=self.user)

    def test_import_csv_returns_200(self):
        response = self.client.post('/api/v1/import', data={}, format='json')
        self.assertEqual(response.status_code, 200)
`

	paths := []string{"core/tests/test_schedule_import.py"}
	content := map[string][]byte{
		"core/tests/test_schedule_import.py": []byte(upvateTestSrc),
	}
	reader := func(p string) []byte { return content[p] }

	// Simulate what index.go Pass 2.8 now does: synthesise ROUTES_TO from
	// endpoint entities and merge with pass2Rels (empty for upvate pattern).
	synthesised := SynthesiseRoutesToFromEndpoints(drfEndpointEntities)
	if len(synthesised) == 0 {
		t.Fatal("SynthesiseRoutesToFromEndpoints returned empty — test setup is wrong")
	}

	edges := ApplyTestsMultiHopViaHTTP(paths, reader, synthesised)

	var found bool
	for _, e := range edges {
		if e.ToID == "SCOPE.Operation:ImportViewSet.create" &&
			e.Properties["test_function"] == "test_import_csv_returns_200" &&
			e.Properties["via"] == "http_router" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected TESTS edge from test_import_csv_returns_200 to ImportViewSet.create; got %+v", edges)
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
