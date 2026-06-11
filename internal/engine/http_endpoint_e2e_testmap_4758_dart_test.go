package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"

	// Register the Dart route-hit extractor so the test_suite (carrying
	// e2e_route_calls) comes from the REAL extractor, not a hand-built fixture.
	_ "github.com/cajasmota/archigraph/internal/custom/dart"
)

// Issue #4758 LIVE-REPRO (resolve side) — Dart package:test route-hit tests.
// Proves end-to-end that a Dart test calling a shelf route by string
// (`handler(Request('GET', Uri.parse('/todos')))`,
// `Request('POST', Uri.parse('/todos'))`) links to the
// http_endpoint_definition it exercises — closing the #4757 N/A. The shared
// linkE2ERouteTestsToEndpoints pass is language-agnostic; only the Dart route
// capture is new.

const dartTestHTTPSrc4758 = `
import 'package:test/test.dart';
import 'package:shelf/shelf.dart';

void main() {
  test('lists todos', () async {
    final res = await handler(Request('GET', Uri.parse('http://localhost:8080/todos')));
    expect(res.statusCode, 200);
  });

  test('creates a todo', () async {
    final res = await handler(Request('POST', Uri.parse('http://localhost:8080/todos')));
    expect(res.statusCode, 201);
  });
}
`

func TestIssue4758_DartTestE2ERouteTestsLinkToEndpoints(t *testing.T) {
	defs := []types.EntityRecord{
		def("GET", "/todos"),
		def("POST", "/todos"),
	}
	suite := realSuite(t, "custom_dart_tests_route_e2e",
		"test/todos_test.dart", "dart", dartTestHTTPSrc4758)

	afterOut, edges := runE2ERouteResolve(t, defs, suite)
	if edges < 2 {
		t.Fatalf("expected >=2 e2e route TESTS edges (GET + POST), got %d", edges)
	}
	assertDartRouteEdges(t, edgeTargets(afterOut))
}

func assertDartRouteEdges(t *testing.T, targets map[string]bool) {
	t.Helper()
	wantGet, wantPost := false, false
	for to := range targets {
		if strings.Contains(to, "GET:/todos") {
			wantGet = true
		}
		if strings.Contains(to, "POST:/todos") {
			wantPost = true
		}
	}
	if !wantGet {
		t.Errorf("expected a TESTS edge to GET /todos; targets=%v", targets)
	}
	if !wantPost {
		t.Errorf("expected a TESTS edge to POST /todos; targets=%v", targets)
	}
}
