package python_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/python"
)

// Issue #4369 LIVE-REPRO (extractor side).
//
// Python API test clients (DRF APIClient / Django test Client, FastAPI /
// Starlette TestClient, Flask test_client, httpx / requests in tests) call
// routes by string but produced NO link to the http_endpoint_definition they
// exercise. This mirrors the NestJS/supertest fix #4351: the pytest extractor
// now captures every `<client>.<verb>('/route')` call and stamps the
// `VERB route` pairs onto the one-per-file test_suite's `e2e_route_calls`
// property — the raw material the shared resolve pass
// (engine.linkE2ERouteTestsToEndpoints) turns into TESTS→http_endpoint_definition
// edges.
//
// This test runs the ACTUAL pytest extractor over a BYTE-COPY of a REAL DRF
// APITestCase from the upvate_core legacy Django backend
// (core/tests/test_schedule_import.py: `self.client.post("/schedule/import/")`)
// plus synthetic-but-representative FastAPI/Flask/httpx test files, and asserts
// the route calls land on the suite. Before #4369 the extractor recorded no
// route information at all, so the resolve pass had nothing to match.

func extractPytest4369(t *testing.T, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("python_pytest")
	if !ok {
		t.Fatal("custom_python_pytest not registered")
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func suiteRouteCalls4369(t *testing.T, ents []types.EntityRecord) []string {
	t.Helper()
	for _, e := range ents {
		if e.Subtype == "test_suite" {
			raw := e.Properties["e2e_route_calls"]
			if raw == "" {
				return nil
			}
			return strings.Split(raw, "\n")
		}
	}
	return nil
}

func fileInput4369(path, content string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: "python", Content: []byte(content)}
}

// TestIssue4369_RealDRFTest_RouteCaptured runs the real upvate_core DRF test
// (byte-copied) and asserts the `self.client.post("/schedule/import/")` call is
// captured onto the suite.
func TestIssue4369_RealDRFTest_RouteCaptured(t *testing.T) {
	p := filepath.Join("testdata", "issue4369", "core", "tests", "test_schedule_import.py")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read real DRF test: %v", err)
	}
	ents := extractPytest4369(t, extreg.FileInput{
		Path: "core/tests/test_schedule_import.py", Language: "python", Content: b,
	})
	calls := suiteRouteCalls4369(t, ents)
	if len(calls) == 0 {
		t.Fatal("no e2e_route_calls captured from real DRF test (#4369)")
	}
	found := false
	for _, c := range calls {
		if c == "POST /schedule/import/" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected captured route call %q from real DRF test, got=%v",
			"POST /schedule/import/", calls)
	}
}

// TestIssue4369_FrameworkCoverage exercises every required Python test client:
// DRF/Django Client, FastAPI/Starlette TestClient, Flask test_client, and
// httpx/requests, asserting each verb+route is captured and that ambiguous /
// non-path routes (f-string base URL, bare verb on a non-client receiver) are
// NOT captured.
func TestIssue4369_FrameworkCoverage(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		src     string
		want    []string
		notWant []string
	}{
		{
			name: "django_drf_client",
			path: "tests/test_django.py",
			src: `from rest_framework.test import APITestCase, APIClient
class InspectionTest(APITestCase):
    def test_create(self):
        self.client = APIClient()
        r = self.client.post('/api/v1/inspections/123/items', data={})
        r2 = self.client.get("/api/v1/inspections/123")
        # cache.get must NOT be captured as a route
        cache.get('some-key')
`,
			want:    []string{"POST /api/v1/inspections/123/items", "GET /api/v1/inspections/123"},
			notWant: []string{"GET some-key"},
		},
		{
			name: "fastapi_testclient",
			path: "tests/test_fastapi.py",
			src: `from fastapi.testclient import TestClient
def test_items():
    client = TestClient(app)
    r = client.post('/inspections/123/items', json={})
    r2 = client.get('/inspections/123')
`,
			want: []string{"POST /inspections/123/items", "GET /inspections/123"},
		},
		{
			name: "flask_test_client",
			path: "tests/test_flask.py",
			src: `def test_x(client):
    r = client.get('/x/1')
    r2 = client.post('/x', data={})
`,
			want: []string{"GET /x/1", "POST /x"},
		},
		{
			name: "httpx_requests",
			path: "tests/test_httpx.py",
			src: `import httpx, requests
def test_remote():
    r = requests.get('http://testserver/inspections/42')
    r2 = httpx.get(f'/inspections/{id}/items')
    # f-string with interpolated BASE first segment -> NOT a leading-slash path
    r3 = httpx.get(f'{BASE}/inspections/99')
`,
			want:    []string{"GET /inspections/42", "GET /inspections/{id}/items"},
			notWant: []string{"GET {BASE}/inspections/99"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := extractPytest4369(t, fileInput4369(tc.path, tc.src))
			calls := suiteRouteCalls4369(t, ents)
			joined := strings.Join(calls, "|")
			for _, w := range tc.want {
				found := false
				for _, c := range calls {
					if c == w {
						found = true
					}
				}
				if !found {
					t.Errorf("expected %q captured, got=%v", w, calls)
				}
			}
			for _, nw := range tc.notWant {
				if strings.Contains(joined, nw) {
					t.Errorf("did NOT expect %q captured, got=%v", nw, calls)
				}
			}
		})
	}
}
