package main

import (
	"strings"
	"testing"
)

// TestAudit2678Go_ChiFiberSynthesis is the integration-test guard for
// the chi + fiber half of #2678. Title-case verb routers were not being
// synthesized into http_endpoint_definition entities at all (only the
// upper-case Gin/Echo regex was wired up). This test asserts that:
//
//  1. Every chi `r.Get/.Post/...` registration produces a
//     http_endpoint_definition with framework=chi.
//  2. Every fiber `app.Get/.Post/...` registration produces one with
//     framework=fiber.
//  3. Source attribution lands in the handler-def file, not the router
//     setup file (the #2678 systemic bug).
func TestAudit2678Go_ChiFiberSynthesis(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2678_go/chi_fiber", "audit2678_go_chifiber", nil)
	if len(doc.Entities) == 0 {
		t.Fatalf("no entities emitted from chi/fiber fixture")
	}

	defs := map[string]int{}
	for i, e := range doc.Entities {
		if e.Kind == "http_endpoint_definition" {
			defs[e.Name] = i
		}
	}

	cases := []struct {
		endpointName string
		wantFile     string
		framework    string
	}{
		{"http:GET:/orders", "handlers_chi.go", "chi"},
		{"http:POST:/orders", "handlers_chi.go", "chi"},
		{"http:GET:/widgets", "handlers_fiber.go", "fiber"},
		{"http:POST:/widgets", "handlers_fiber.go", "fiber"},
	}

	for _, tc := range cases {
		idx, ok := defs[tc.endpointName]
		if !ok {
			t.Errorf("missing http_endpoint_definition for %s (got: %v)",
				tc.endpointName, defsMapKeys(defs))
			continue
		}
		e := doc.Entities[idx]
		if !strings.HasSuffix(e.SourceFile, tc.wantFile) {
			t.Errorf("%s: source_file=%q does not end with %q — endpoint is "+
				"still attributed to the router-registration file (#2678 regression)",
				tc.endpointName, e.SourceFile, tc.wantFile)
		}
		if got := e.Properties["framework"]; got != tc.framework {
			t.Errorf("%s: framework=%q want %q", tc.endpointName, got, tc.framework)
		}
		if got := e.Properties["attribution"]; got != "handler_resolved" {
			t.Errorf("%s: attribution=%q want %q",
				tc.endpointName, got, "handler_resolved")
		}
		if e.StartLine <= 0 {
			t.Errorf("%s: start_line=%d, expected positive handler-body line",
				tc.endpointName, e.StartLine)
		}
	}
}

func defsMapKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
