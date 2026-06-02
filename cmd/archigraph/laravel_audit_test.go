// Audit-2678 integration guard: every Laravel http_endpoint_definition must
// point at the controller method's source file + line, NOT the routes file
// where Route::verb('/path', [Controller::class, 'method']) was registered.
//
// Mirrors the DRF acceptance test in #2677: source_file ends in the
// controller path, source_line matches the method def line.
package main

import (
	"strings"
	"testing"
)

func TestAudit2678_Laravel_EndpointAttribution(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2678_other/laravel", "laravel_audit", nil)

	type ep struct {
		Name string
		File string
		Line int
	}
	var got []ep
	for _, e := range doc.Entities {
		if e.Kind != "http_endpoint_definition" && e.Kind != "http_endpoint" {
			continue
		}
		got = append(got, ep{Name: e.Name, File: e.SourceFile, Line: e.StartLine})
	}
	if len(got) < 3 {
		t.Fatalf("Laravel fixture: expected >= 3 endpoint definitions, got %d", len(got))
	}

	// Every endpoint must land on the controller file, not the routes file.
	for _, g := range got {
		if !strings.Contains(g.File, "ThingController.php") {
			t.Errorf("endpoint %s: source_file=%s, want path containing ThingController.php (was attributed to routes registration site)",
				g.Name, g.File)
		}
		if g.Line <= 0 {
			t.Errorf("endpoint %s: start_line=%d, want > 0 (method def line)", g.Name, g.Line)
		}
	}

	// Sanity-check the exact method lines from the fixture controller:
	//   index → line 9
	//   store → line 14
	//   show  → line 19
	want := map[string]int{
		"http:GET:/things":      9,
		"http:POST:/things":     14,
		"http:GET:/things/{id}": 19,
	}
	for _, g := range got {
		w, ok := want[g.Name]
		if !ok {
			continue
		}
		if g.Line != w {
			t.Errorf("endpoint %s: start_line=%d, want %d", g.Name, g.Line, w)
		}
	}
}
