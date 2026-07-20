package main

import (
	"strings"
	"testing"
)

// TestGoTrio_EndpointSynthesis is the integration guard for issues
// #2684 (gorilla/mux), #2685 (net/http stdlib including Go 1.22
// method-prefix patterns), and #2686 (huma OpenAPI). For each
// framework the production pipeline must:
//
//  1. Emit at least 2 http_endpoint_definition entities from the
//     fixture.
//  2. After the shared resolver rebind, every emitted definition's
//     source_file lands in the handler file (handlers.go), not the
//     registration site (router.go).
//  3. Properties[registration_source_file] preserves the original
//     registration file (so the route mount-point is still
//     discoverable) and Properties[registration_start_line] preserves
//     the original line, both stashed by the resolver rebind.
//  4. StartLine is positive and refers to the handler def line in the
//     handler file.
func TestGoTrio_EndpointSynthesis(t *testing.T) {
	cases := []struct {
		name        string
		fixturePath string
		framework   string
		wantHandler string // suffix of source_file after rebind
		// Expected (verb, path) tuples we MUST find synthesized. Each
		// must end up attributed to wantHandler.
		expected []struct{ verb, path string }
	}{
		{
			name:        "gorilla_mux",
			fixturePath: "testdata/audit2678_go_trio/gorilla",
			framework:   "gorilla",
			wantHandler: "handlers.go",
			expected: []struct{ verb, path string }{
				{"GET", "/users"},
				{"GET", "/users/{id}"},
				{"HEAD", "/users/{id}"},
				{"POST", "/items"},
				{"GET", "/health"},
			},
		},
		{
			name:        "net_http_stdlib",
			fixturePath: "testdata/audit2678_go_trio/nethttp",
			framework:   "nethttp",
			wantHandler: "handlers.go",
			expected: []struct{ verb, path string }{
				{"ANY", "/legacy"},
				{"ANY", "/items"},
				{"GET", "/users/{id}"},
				{"POST", "/users"},
			},
		},
		{
			name:        "huma",
			fixturePath: "testdata/audit2678_go_trio/huma",
			framework:   "huma",
			wantHandler: "handlers.go",
			expected: []struct{ verb, path string }{
				{"GET", "/users/{id}"},
				{"POST", "/users"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := runIndexerOn(t, tc.fixturePath, "go_trio_"+tc.name, nil)
			if len(doc.Entities) == 0 {
				t.Fatalf("no entities emitted from %s fixture", tc.name)
			}

			defs := map[string]int{}
			for i, e := range doc.Entities {
				if e.Kind == "http_endpoint_definition" {
					defs[e.Name] = i
				}
			}

			// Assertion 1: at least 2 http_endpoint_definition entities.
			if len(defs) < 2 {
				t.Fatalf("%s: got %d http_endpoint_definition entities, want ≥ 2 (names: %v)",
					tc.name, len(defs), keysOf(defs))
			}

			for _, want := range tc.expected {
				id := "http:" + want.verb + ":" + want.path
				idx, ok := defs[id]
				if !ok {
					t.Errorf("%s: missing http_endpoint_definition for %s (got: %v)",
						tc.name, id, keysOf(defs))
					continue
				}
				e := doc.Entities[idx]

				// Assertion 2: source_file rebound to the handler file.
				if !strings.HasSuffix(e.SourceFile, tc.wantHandler) {
					t.Errorf("%s %s: source_file=%q does not end with %q — "+
						"resolver did not rebind to handler file",
						tc.name, id, e.SourceFile, tc.wantHandler)
				}

				// Framework property must reflect the right synthesizer.
				if got := e.PropGet("framework"); got != tc.framework {
					t.Errorf("%s %s: framework=%q want %q",
						tc.name, id, got, tc.framework)
				}

				// Assertion 3: registration_source_file + _start_line are
				// stashed by the rebind. They're only stashed when the
				// rebind actually moved the entity (handler in a different
				// file from the registration site), which is the case for
				// every fixture endpoint here by construction.
				if got := e.PropGet("registration_source_file"); got == "" {
					t.Errorf("%s %s: registration_source_file is empty — "+
						"resolver rebind did not stash the original registration site",
						tc.name, id)
				} else if !strings.HasSuffix(got, "router.go") {
					t.Errorf("%s %s: registration_source_file=%q does not end with router.go",
						tc.name, id, got)
				}
				if got := e.PropGet("registration_start_line"); got == "" || got == "0" {
					t.Errorf("%s %s: registration_start_line=%q, want a positive line number",
						tc.name, id, got)
				}

				// Assertion 4: StartLine refers to the handler def line.
				if e.StartLine <= 0 {
					t.Errorf("%s %s: start_line=%d, want positive handler-def line",
						tc.name, id, e.StartLine)
				}
			}
		})
	}
}

func keysOf(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
