package javascript_test

import (
	"testing"
)

// TestExtract_NodeStdlibNamespaceImport_RoutesToExtNodePackage (Refs #44).
// `import * as path from "path"` followed by `path.join(...)` should emit a
// CALLS edge keyed on the cross-language `:external:node:path` stub so the
// synth pass collapses it to `ext:node:path` instead of falling through to
// the bare-name "join" which lands in the bug-extractor.
func TestExtract_NodeStdlibNamespaceImport_RoutesToExtNodePackage(t *testing.T) {
	src := `import * as path from "path";

function f() {
  return path.join("a", "b");
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/util/paths.ts")

	want := "scope:operation:method:typescript:external:node:path"
	if !hasRelEdge(ents, "f", "CALLS", want) {
		caller := findByNameRel(ents, "f")
		if caller == nil {
			t.Fatalf("f entity missing; ents=%+v", ents)
		}
		t.Fatalf("expected CALLS f -> %q; got %+v", want, caller.Relationships)
	}
}

// TestExtract_NodeStdlibDefaultImport_NodePrefix (Refs #44). The `node:`
// prefix form (`import fs from "node:fs"`) and the bare form (`import fs
// from "fs"`) both canonicalise to the same `node:fs` placeholder.
func TestExtract_NodeStdlibDefaultImport_NodePrefix(t *testing.T) {
	src := `import fs from "node:fs";

function read() {
  return fs.readFileSync("x");
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/io.ts")

	want := "scope:operation:method:typescript:external:node:fs"
	if !hasRelEdge(ents, "read", "CALLS", want) {
		caller := findByNameRel(ents, "read")
		t.Fatalf("expected CALLS read -> %q; got %+v", want, caller.Relationships)
	}
}

// TestExtract_NodeStdlibBareNamedImport (Refs #44). `import { join } from
// "path"` followed by `join("a","b")` — the call is a bare identifier
// (no member_expression) but the import binding still resolves it to the
// Node stdlib module.
func TestExtract_NodeStdlibBareNamedImport(t *testing.T) {
	src := `import { join } from "path";

function combine() {
  return join("a", "b");
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/combine.ts")

	want := "scope:operation:method:typescript:external:node:path"
	if !hasRelEdge(ents, "combine", "CALLS", want) {
		caller := findByNameRel(ents, "combine")
		t.Fatalf("expected CALLS combine -> %q; got %+v", want, caller.Relationships)
	}
}

// TestExtract_NodeStdlibSubPathCollapsesToRoot (Refs #44). `import { pipeline
// } from "stream/promises"` collapses to `ext:node:stream`. Sub-paths
// would otherwise be rejected by synth's `/`-containing-string check on
// the `:external:` branch.
func TestExtract_NodeStdlibSubPathCollapsesToRoot(t *testing.T) {
	src := `import { readFile } from "fs/promises";

async function load() {
  return readFile("x");
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/load.ts")

	want := "scope:operation:method:typescript:external:node:fs"
	if !hasRelEdge(ents, "load", "CALLS", want) {
		caller := findByNameRel(ents, "load")
		t.Fatalf("expected CALLS load -> %q; got %+v", want, caller.Relationships)
	}
}

// TestExtract_NodeStdlibThirdPartyImportFallsBack (Refs #44). A receiver
// imported from a third-party package (`import { join } from "lodash"`)
// must NOT be classified as Node stdlib — the bare-name fallback applies.
// This guards the collision-prone bias the task description called out.
func TestExtract_NodeStdlibThirdPartyImportFallsBack(t *testing.T) {
	src := `import { join } from "lodash";

function f() {
  return join(["a", "b"], ",");
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/lodash-join.ts")

	caller := findByNameRel(ents, "f")
	if caller == nil {
		t.Fatalf("f entity missing")
	}
	// Should NOT contain the node:path stub.
	bad := "scope:operation:method:typescript:external:node:path"
	for _, r := range caller.Relationships {
		if r.ToID == bad {
			t.Fatalf("unexpected node:path classification for lodash import; rels=%+v", caller.Relationships)
		}
	}
}

// TestExtract_NodeStdlibUserMethodNotShadowed (Refs #44). If the receiver
// is NOT an import binding (just a local variable), the Node stdlib path
// must not trigger — preserves bare-name behaviour for user code where
// `path.join(...)` could be calling a user `path` object's `join` method.
func TestExtract_NodeStdlibUserMethodNotShadowed(t *testing.T) {
	src := `function f(path) {
  return path.join("a", "b");
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/userjoin.ts")

	caller := findByNameRel(ents, "f")
	if caller == nil {
		t.Fatalf("f entity missing")
	}
	bad := "scope:operation:method:typescript:external:node:path"
	for _, r := range caller.Relationships {
		if r.ToID == bad {
			t.Fatalf("unexpected node:path classification for user param; rels=%+v", caller.Relationships)
		}
	}
}
