package main

import (
	"strings"
	"testing"
)

// TestAudit2678Go_GinEchoAttribution is the integration-test guard for
// the Go gin/echo half of #2678: every http_endpoint_definition emitted
// from a Gin or Echo route registration MUST attribute its source_file
// to the file where the handler body lives (not the router.go where
// the .GET / .POST registration call sits).
//
// Before the fix, every endpoint's source_file was the registration
// site (router.go / echo_router.go). After the fix it should be the
// handler file (handlers_gin.go / handlers_echo.go).
func TestAudit2678Go_GinEchoAttribution(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2678_go/gin_echo", "audit2678_go_ginecho", nil)
	if len(doc.Entities) == 0 {
		t.Fatalf("no entities emitted from gin/echo fixture")
	}

	// Build a (name → entity) lookup over http_endpoint_definition entities.
	// Names are the canonical synthetic ID (e.g. "http:GET:/users").
	defs := map[string]int{}
	for i, e := range doc.Entities {
		if e.Kind == "http_endpoint_definition" {
			defs[e.Name] = i
		}
	}

	cases := []struct {
		endpointName string // synthetic ID
		wantFile     string // handler file the source_file MUST land in
		framework    string
	}{
		{"http:GET:/users", "handlers_gin.go", "gin"},
		{"http:POST:/users", "handlers_gin.go", "gin"},
		{"http:GET:/items", "handlers_echo.go", "echo"},
		{"http:POST:/items", "handlers_echo.go", "echo"},
	}

	for _, tc := range cases {
		idx, ok := defs[tc.endpointName]
		if !ok {
			t.Errorf("missing http_endpoint_definition for %s (got: %v)",
				tc.endpointName, mapKeys(defs))
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
			t.Errorf("%s: attribution=%q want %q — re-attribution pass did not run",
				tc.endpointName, got, "handler_resolved")
		}
		// registration_source_file property must preserve the original router file
		// so the mount-point is still discoverable.
		if got := e.Properties["registration_source_file"]; got == "" {
			t.Errorf("%s: registration_source_file is empty — original registration site lost",
				tc.endpointName)
		}
		if e.StartLine <= 0 {
			t.Errorf("%s: start_line=%d, expected a positive handler-body line",
				tc.endpointName, e.StartLine)
		}
	}
}

func mapKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
