// Issue #2690 — Starlette / Tornado / Pyramid endpoint synthesis.
//
// This is the production-pipeline guard for the three Python web
// frameworks that PR #2681 left as NOT IMPLEMENTED in the #2678 audit.
// Each subtest runs the full Indexer.Run on a tiny fixture and asserts:
//
//   - the expected http_endpoint_definition entities exist (verb + path),
//   - source_file points at the handler module,
//   - source_line points at the `def <handler>` line (or the class verb
//     method's def line for Tornado), never the decorator or the
//     Application(...) call line, and never the default 0.
//
// The synthesizers also forward a source_handler reference shaped as
// `SCOPE.Operation:<name>` (Starlette / Pyramid) or `SCOPE.Class:<name>`
// (Tornado). When the handler module is split from the routes module the
// resolver rebind (#2680) retargets source_file/start_line to the
// handler entity; this fixture exercises the same-file path because the
// rebind is already covered by the Laravel + DRF tests.
package main

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// findEndpointDef returns the http_endpoint_definition entity whose
// synthetic ID matches id, or nil when absent. The Indexer emits exactly
// one definition per (verb, path) on the producer side; this is a
// strict-match lookup intended to fail loudly when synthesis regresses.
func findEndpointDef(entities []graph.Entity, id string) *graph.Entity {
	for i := range entities {
		e := &entities[i]
		if e.Kind != "http_endpoint_definition" {
			continue
		}
		if e.ID == id || e.Name == id {
			return e
		}
	}
	return nil
}

// assertEndpointAt asserts that the endpoint exists, its source_file has
// the given suffix (path-portable across machines), and its source_line
// equals wantLine. Mirrors the helper in issue2678_audit_python_test.go so
// the assertion shapes are uniform across the #2678 audit + #2690
// follow-up.
func assertEndpointAt(t *testing.T, entities []graph.Entity, id, suffix string, wantLine int) {
	t.Helper()
	e := findEndpointDef(entities, id)
	if e == nil {
		t.Errorf("issue #2690: no http_endpoint_definition with ID %q (entities=%d)", id, len(entities))
		return
	}
	if !strings.HasSuffix(e.SourceFile, suffix) {
		t.Errorf("issue #2690: endpoint %s source_file = %q, want suffix %q", id, e.SourceFile, suffix)
	}
	if e.StartLine != wantLine {
		t.Errorf("issue #2690: endpoint %s source_line = %d, want %d (the handler def line)", id, e.StartLine, wantLine)
	}
}

// TestPythonTrioEndpointAttribution_Starlette guards the Starlette branch
// of issue #2690. Three Route(...) entries each carry an explicit methods=
// list and a same-file endpoint= handler; the synthesizer must stamp each
// at the corresponding `def <handler>` line in app.py.
func TestPythonTrioEndpointAttribution_Starlette(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2678_python_trio/starlette", "audit2690_starlette", nil)

	// `def health` is on L16, `def get_user` on L20, `async def
	// create_item` on L24. Keep in sync with the fixture.
	cases := []struct {
		id       string
		wantLine int
	}{
		{"http:GET:/health", 16},
		{"http:GET:/users/{user_id}", 20},
		{"http:POST:/items", 24},
	}
	for _, tc := range cases {
		assertEndpointAt(t, doc.Entities, tc.id, "app.py", tc.wantLine)
	}
}

// TestPythonTrioEndpointAttribution_Tornado guards the Tornado branch of
// issue #2690. Application(...) registers two handler classes; the
// synthesizer must expand UsersHandler's GET + POST methods and stamp
// each endpoint at the method's `def` line, not the Application(...)
// call line and not the class declaration line.
func TestPythonTrioEndpointAttribution_Tornado(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2678_python_trio/tornado", "audit2690_tornado", nil)

	// HealthHandler.get is on L16; UsersHandler.get is on L21;
	// UsersHandler.post is on L24. Keep in sync with the fixture.
	cases := []struct {
		id       string
		wantLine int
	}{
		{"http:GET:/health", 16},
		{"http:GET:/users/{user_id}", 21},
		{"http:GET:/users", 21},
		{"http:POST:/users/{user_id}", 24},
		{"http:POST:/users", 24},
	}
	for _, tc := range cases {
		assertEndpointAt(t, doc.Entities, tc.id, "app.py", tc.wantLine)
	}
}

// TestPythonTrioEndpointAttribution_Pyramid guards the Pyramid branch of
// issue #2690. Three @view_config decorators pair with same-file
// add_route declarations; the synthesizer must resolve each route_name
// to its path and stamp the endpoint at the handler def line.
func TestPythonTrioEndpointAttribution_Pyramid(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2678_python_trio/pyramid", "audit2690_pyramid", nil)

	// `def health` is on L18, `def get_user` on L23, `def create_user`
	// on L28. Keep in sync with the fixture.
	cases := []struct {
		id       string
		wantLine int
	}{
		{"http:GET:/health", 18},
		{"http:GET:/users/{user_id}", 23},
		{"http:POST:/users", 28},
	}
	for _, tc := range cases {
		assertEndpointAt(t, doc.Entities, tc.id, "app.py", tc.wantLine)
	}
}
