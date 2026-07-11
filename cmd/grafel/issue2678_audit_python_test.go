// Issue #2678 — production-pipeline guard for Python web-framework
// endpoint attribution.
//
// PR #2677 confirmed that DRF/Django endpoints were misattributed to the
// routing/registration file instead of the handler method file. #2678 is
// the systemic audit: every web-framework extractor that emits HTTP
// endpoint entities must stamp source_file at the handler def and
// source_line at the `def <handler>` line — never the decorator line and
// never the default 0.
//
// This file is the Python side of that audit. It runs the full production
// Indexer.Run on small fixtures and asserts the attribution invariants
// for the two implemented decorator-based frameworks (Flask + FastAPI).
// Tornado, Pyramid and Starlette have no endpoint extractor today and
// are intentionally out of scope here — recorded as NOT IMPLEMENTED in
// the audit report.
package main

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// endpointDef looks up the http_endpoint_definition entity with the given
// synthetic ID (`http:<METHOD>:<path>`) in the indexed graph. The Indexer
// emits exactly one definition per (verb, path) per side; this helper
// returns the first match or fails the test.
func endpointDef(t *testing.T, entities []graph.Entity, id string) graph.Entity {
	t.Helper()
	for _, e := range entities {
		if e.Kind != "http_endpoint_definition" {
			continue
		}
		if e.ID == id || e.Name == id {
			return e
		}
	}
	t.Fatalf("issue #2678: no http_endpoint_definition with ID %q in the graph (entities=%d)", id, len(entities))
	return graph.Entity{}
}

// assertEndpointAttributedTo asserts that the endpoint's source_file ends
// with handlerSuffix (so the test is path-portable) and that source_line
// equals wantLine — the line of the handler `def`, not the decorator.
func assertEndpointAttributedTo(t *testing.T, e graph.Entity, handlerSuffix string, wantLine int) {
	t.Helper()
	if !strings.HasSuffix(e.SourceFile, handlerSuffix) {
		t.Errorf("issue #2678: endpoint %s source_file = %q, want suffix %q (attribution should point at the handler file)", e.ID, e.SourceFile, handlerSuffix)
	}
	if e.StartLine != wantLine {
		t.Errorf("issue #2678: endpoint %s source_line = %d, want %d (the `def <handler>` line, not the decorator line and not 0)", e.ID, e.StartLine, wantLine)
	}
}

// TestPythonEndpointAttribution_Flask runs the production index pipeline
// against the Flask fixture and asserts every endpoint is attributed to
// app.py at the `def <handler>` line. Guards #2678 for Flask.
func TestPythonEndpointAttribution_Flask(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2678_python/flask_app", "audit2678_flask", nil)

	// `def health` is on L16, `def get_user` on L21, `def create_user` on L26.
	// Keep these in sync with cmd/grafel/testdata/audit2678_python/flask_app/app.py.
	cases := []struct {
		id       string
		wantLine int
	}{
		{"http:GET:/health", 16},
		{"http:GET:/users/{user_id}", 21},
		{"http:POST:/users", 26},
	}
	for _, tc := range cases {
		e := endpointDef(t, doc.Entities, tc.id)
		assertEndpointAttributedTo(t, e, "app.py", tc.wantLine)
	}
}

// TestPythonEndpointAttribution_FastAPI runs the production index pipeline
// against the FastAPI fixture and asserts every endpoint is attributed to
// main.py at the `def <handler>` line — including the `async def` handler,
// which previously also had source_line=0. Guards #2678 for FastAPI.
func TestPythonEndpointAttribution_FastAPI(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2678_python/fastapi_app", "audit2678_fastapi", nil)

	// `def health` is on L16, `async def create_item` on L21, `def
	// delete_user` on L26. Keep these in sync with the fixture file.
	// create_item is registered on `router = APIRouter(prefix="/v1")`, so
	// its route path is folded to /v1/items (see #5688).
	cases := []struct {
		id       string
		wantLine int
	}{
		{"http:GET:/health", 16},
		{"http:POST:/v1/items", 21},
		{"http:DELETE:/users/{user_id}", 26},
	}
	for _, tc := range cases {
		e := endpointDef(t, doc.Entities, tc.id)
		assertEndpointAttributedTo(t, e, "main.py", tc.wantLine)
	}
}
