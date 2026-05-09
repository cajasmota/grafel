package resolve

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// importerRecord builds an EntityRecord for the IMPORTING file's marker
// entity carrying a single IMPORTS relationship. Mirrors what
// internal/extractors/python/extractor.go:importRecord emits.
func importerRecord(file, modulePath string, props map[string]string) types.EntityRecord {
	return types.EntityRecord{
		Name:       modulePath,
		Kind:       "SCOPE.Component",
		Subtype:    "module",
		SourceFile: file,
		Language:   "python",
		Relationships: []types.RelationshipRecord{{
			FromID:     file,
			ToID:       modulePath,
			Kind:       ImportRelKind,
			Properties: props,
		}},
	}
}

// targetRecord builds the entity that a CALLS edge should bind to after
// the import-aware resolver runs (e.g. the real `get` defined in
// requests/api.py). The ID field is what ResolveImports rewrites the
// CALLS target to; we set it to a synthetic 16-char hex value so the
// downstream isHexID check accepts it.
func targetRecord(name, file, id string) types.EntityRecord {
	return types.EntityRecord{
		ID:         id,
		Name:       name,
		Kind:       "SCOPE.Operation",
		Subtype:    "function",
		SourceFile: file,
		Language:   "python",
	}
}

// callerRecord builds an EntityRecord representing a function that
// makes a single bare-name CALL. The CALLS edge's FromID is left empty
// (matching the Pass 1 emission convention); SourceFile pins the
// caller's file so ResolveImports can find the import table entry.
func callerRecord(name, file, target string) types.EntityRecord {
	return types.EntityRecord{
		ID:         "0123456789abcdef",
		Name:       name,
		Kind:       "SCOPE.Operation",
		Subtype:    "function",
		SourceFile: file,
		Language:   "python",
		Relationships: []types.RelationshipRecord{{
			ToID: target,
			Kind: "CALLS",
		}},
	}
}

// TestResolveImports_PlainImport covers `import x; x.foo()` — the
// extractor emits ToID="foo" and a binding with local_name="x",
// source_module="x", imported_name="x". The resolver should rewrite
// "foo" to the entity id of the `foo` defined in module x.
func TestResolveImports_PlainImport(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("client/app.py", "remote", map[string]string{
			"local_name":    "remote",
			"source_module": "remote",
			"imported_name": "remote",
		}),
		targetRecord("dispatch", "remote/__init__.py", "aaaaaaaaaaaaaaaa"),
		callerRecord("run", "client/app.py", "dispatch"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d (considered=%d)", stats.CallsRewritten, stats.CallsConsidered)
	}
	caller := records[2]
	if got := caller.Relationships[0].ToID; got != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("expected CALLS target rewritten to aaaaaaaaaaaaaaaa, got %q", got)
	}
}

// TestResolveImports_FromImport covers `from foo import bar; bar()`.
func TestResolveImports_FromImport(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("client/app.py", "foo.bar", map[string]string{
			"local_name":    "bar",
			"source_module": "foo",
			"imported_name": "bar",
		}),
		targetRecord("bar", "foo/__init__.py", "bbbbbbbbbbbbbbbb"),
		callerRecord("run", "client/app.py", "bar"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.CallsRewritten)
	}
	if got := records[2].Relationships[0].ToID; got != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("expected target bbbbbbbbbbbbbbbb, got %q", got)
	}
}

// TestResolveImports_FromImportAlias covers
// `from foo import bar as baz; baz()` — the local name "baz" must
// rewrite to the entity for `bar` defined in module foo.
func TestResolveImports_FromImportAlias(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("client/app.py", "foo.bar", map[string]string{
			"local_name":    "baz",
			"source_module": "foo",
			"imported_name": "bar",
		}),
		targetRecord("bar", "foo/__init__.py", "cccccccccccccccc"),
		callerRecord("run", "client/app.py", "baz"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.CallsRewritten)
	}
	if got := records[2].Relationships[0].ToID; got != "cccccccccccccccc" {
		t.Fatalf("expected target cccccccccccccccc, got %q", got)
	}
}

// TestResolveImports_BareNameNotImported leaves a bare CALLS target
// alone when no import binding matches.
func TestResolveImports_BareNameNotImported(t *testing.T) {
	records := []types.EntityRecord{
		callerRecord("run", "client/app.py", "mystery"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 0 {
		t.Fatalf("expected 0 rewrites, got %d", stats.CallsRewritten)
	}
	if got := records[0].Relationships[0].ToID; got != "mystery" {
		t.Fatalf("expected target unchanged, got %q", got)
	}
}

// TestResolveImports_ExternalImportNoEntity covers
// `import os; os.getcwd()` — when `os` is not part of the corpus the
// import-aware pass leaves the CALLS target alone (the downstream
// classifier will tag it ExternalKnown via the allowlist).
func TestResolveImports_ExternalImportNoEntity(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("client/app.py", "os", map[string]string{
			"local_name":    "os",
			"source_module": "os",
			"imported_name": "os",
		}),
		callerRecord("run", "client/app.py", "getcwd"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 0 {
		t.Fatalf("expected 0 rewrites (os not in corpus), got %d", stats.CallsRewritten)
	}
	if got := records[1].Relationships[0].ToID; got != "getcwd" {
		t.Fatalf("expected target unchanged for external symbol, got %q", got)
	}
}

// TestResolveImports_DottedTargetSkipped — receiver-typed dotted
// targets ("Class.method") are handled by the base resolver via
// byMember and must not be touched here.
func TestResolveImports_DottedTargetSkipped(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("client/app.py", "foo.bar", map[string]string{
			"local_name":    "bar",
			"source_module": "foo",
			"imported_name": "bar",
		}),
		targetRecord("bar", "foo/__init__.py", "dddddddddddddddd"),
		{
			ID:         "1234567890abcdef",
			Name:       "Driver.run",
			Kind:       "SCOPE.Operation",
			SourceFile: "client/app.py",
			Language:   "python",
			Relationships: []types.RelationshipRecord{{
				ToID: "Helper.run",
				Kind: "CALLS",
			}},
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsConsidered != 0 {
		t.Fatalf("expected dotted target to be skipped, considered=%d", stats.CallsConsidered)
	}
	if got := records[2].Relationships[0].ToID; got != "Helper.run" {
		t.Fatalf("expected dotted target unchanged, got %q", got)
	}
}

// TestResolveImports_Wildcard covers `from foo import *; bar()`.
// Best-effort: when foo exports a single entity named `bar`, the
// CALLS target is rewritten.
func TestResolveImports_Wildcard(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("client/app.py", "foo", map[string]string{
			"source_module": "foo",
			"wildcard":      "1",
		}),
		targetRecord("bar", "foo/__init__.py", "eeeeeeeeeeeeeeee"),
		callerRecord("run", "client/app.py", "bar"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 1 {
		t.Fatalf("expected 1 wildcard rewrite, got %d", stats.CallsRewritten)
	}
	if got := records[2].Relationships[0].ToID; got != "eeeeeeeeeeeeeeee" {
		t.Fatalf("expected wildcard target eeeeeeeeeeeeeeee, got %q", got)
	}
}

// TestResolveImports_AmbiguousModuleEntity covers the case where a
// (module, name) tuple resolves to two distinct entities. The
// resolver must leave the CALLS target alone rather than guess.
func TestResolveImports_AmbiguousModuleEntity(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("client/app.py", "foo.bar", map[string]string{
			"local_name":    "bar",
			"source_module": "foo",
			"imported_name": "bar",
		}),
		// Two entities with name "bar" both in foo/__init__.py — the
		// (foo, bar) tuple is ambiguous, so the resolver must skip.
		targetRecord("bar", "foo/__init__.py", "ffffffffffffffff"),
		{
			ID:         "1111111111111111",
			Name:       "bar",
			Kind:       "SCOPE.Operation",
			Subtype:    "function",
			SourceFile: "foo/__init__.py",
			Language:   "python",
		},
		callerRecord("run", "client/app.py", "bar"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	// Module "foo" has two `bar` entities — the lookup is ambiguous
	// for the (foo, bar) tuple. The (foo.bar, bar) tuple is unique
	// (only foo/__init__.py serves it under "foo.bar"); the actual
	// extractor emits source_module="foo" so the lookup hits the
	// ambiguous tuple and skips. We assert no rewrite under the
	// ambiguous condition.
	if stats.CallsRewritten != 0 {
		t.Fatalf("expected 0 rewrites under ambiguity, got %d", stats.CallsRewritten)
	}
}

// TestResolveImports_FileLocalCollisionDropsBinding covers the case
// where the same file imports two different symbols under the same
// local name (e.g. shadowing). The conservative behaviour is to drop
// both bindings and leave the CALLS stub alone.
func TestResolveImports_FileLocalCollisionDropsBinding(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("client/app.py", "foo.bar", map[string]string{
			"local_name":    "bar",
			"source_module": "foo",
			"imported_name": "bar",
		}),
		importerRecord("client/app.py", "qux.bar", map[string]string{
			"local_name":    "bar",
			"source_module": "qux",
			"imported_name": "bar",
		}),
		targetRecord("bar", "foo/__init__.py", "2222222222222222"),
		targetRecord("bar", "qux/__init__.py", "3333333333333333"),
		callerRecord("run", "client/app.py", "bar"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 0 {
		t.Fatalf("expected 0 rewrites under local-name collision, got %d", stats.CallsRewritten)
	}
}
