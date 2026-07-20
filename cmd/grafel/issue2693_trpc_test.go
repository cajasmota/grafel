package main

import (
	"strings"
	"testing"
)

// TestTRPC_ProcedureSynthesis is the integration guard for #2693
// (follow-up to the #2678 / #2687 audit). The production pipeline must:
//
//  1. Emit one http_endpoint_definition per leaf procedure reachable from
//     a router defined in the same file. The fixture composes
//     `userRouter` + `postsRouter` into `appRouter` and expects five
//     dotted-path endpoints: users.list, users.byId, users.create,
//     posts.list, posts.create.
//  2. Map the tRPC builder method to the right verb: .query → GET,
//     .mutation → POST.
//  3. Stamp source_file = the fixture's server.ts and source_line = the
//     1-based line of the `.query(` / `.mutation(` call (the resolver's
//     def line for the inline arrow procedure).
//  4. Use the dotted path as the canonical path component of the
//     synthetic ID (`http:GET:users.list`).
func TestTRPC_ProcedureSynthesis(t *testing.T) {
	doc := runIndexerOn(t, "testdata/audit2678_jsts_trpc", "trpc_2693", nil)
	if len(doc.Entities) == 0 {
		t.Fatalf("no entities emitted from trpc fixture")
	}

	// Collect tRPC procedure definitions by name (synthetic ID).
	defs := map[string]int{}
	for i, e := range doc.Entities {
		if e.Kind != "http_endpoint_definition" {
			continue
		}
		if e.PropLen() == 0 || e.PropGet("framework") != "trpc" {
			continue
		}
		defs[e.Name] = i
	}

	expected := []struct {
		verb, path string
		line       int
	}{
		{"GET", "users.list", 19},
		{"GET", "users.byId", 24},
		{"POST", "users.create", 29},
		{"GET", "posts.list", 35},
		{"POST", "posts.create", 40},
	}

	// Assertion 1: procedure count matches the fixture.
	if got, want := len(defs), len(expected); got != want {
		names := make([]string, 0, len(defs))
		for k := range defs {
			names = append(names, k)
		}
		t.Fatalf("trpc: got %d procedure endpoints, want %d (names: %v)",
			got, want, names)
	}

	for _, want := range expected {
		id := "http:" + want.verb + ":" + want.path
		idx, ok := defs[id]
		if !ok {
			t.Errorf("missing http_endpoint_definition for %s", id)
			continue
		}
		e := doc.Entities[idx]

		// Assertion 2: verb mapping.
		if got := e.PropGet("verb"); got != want.verb {
			t.Errorf("%s: verb=%q want %q", id, got, want.verb)
		}

		// Assertion 3 (a): canonical path is the dotted form, NOT a URL.
		// Specifically: no leading slash, segments joined by `.`.
		if got := e.PropGet("path"); got != want.path {
			t.Errorf("%s: path=%q want %q (dotted form, no leading slash)",
				id, got, want.path)
		}
		if strings.HasPrefix(e.PropGet("path"), "/") {
			t.Errorf("%s: path %q must not be canonicalised to a URL (no leading slash)",
				id, e.PropGet("path"))
		}

		// Assertion 3 (b): source_file is the fixture's server.ts.
		if !strings.HasSuffix(e.SourceFile, "server.ts") {
			t.Errorf("%s: source_file=%q does not end with server.ts",
				id, e.SourceFile)
		}

		// Assertion 3 (c): source_line is the .query(...) / .mutation(...)
		// call line — the inline arrow function's def line. This is the
		// precision contract of #2693.
		if e.StartLine != want.line {
			t.Errorf("%s: start_line=%d want %d (the .%s(...) call line)",
				id, e.StartLine, want.line, strings.ToLower(want.verb))
		}
	}
}
