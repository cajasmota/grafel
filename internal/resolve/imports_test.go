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
			Kind:       importRelKind,
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

// TestResolveImports_MonorepoTopLevelCollision asserts the suffix-strip
// in modulesForFile does NOT explode "tools.shared.helpers" into
// "shared.helpers" / "helpers" — a monorepo could have an unrelated
// top-level package "helpers" that would otherwise collide and either
// resolve to the wrong entity or be demoted to ambiguous.
//
// Setup:
//   - client/app.py does `from helpers import compute; compute()`
//   - tools/shared/helpers.py defines a function compute (NOT the
//     `helpers` package the caller meant to import)
//   - The caller imports module "helpers" which is not in the corpus,
//     so the rewrite should NOT happen.
//
// Pre-fix: modulesForFile("tools/shared/helpers.py") emitted
// ["tools.shared.helpers", "shared.helpers", "helpers"], so the
// (helpers, compute) tuple resolved to the unrelated tools entity.
// Post-fix: only the precise dotted form (and a single allowlisted
// source-root strip) is emitted, so no rewrite happens.
func TestResolveImports_MonorepoTopLevelCollision(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("client/app.py", "helpers.compute", map[string]string{
			"local_name":    "compute",
			"source_module": "helpers",
			"imported_name": "compute",
		}),
		// Unrelated entity buried under a deeper path. Only its precise
		// dotted form ("tools.shared.helpers") should be indexed.
		targetRecord("compute", "tools/shared/helpers.py", "4444444444444444"),
		callerRecord("run", "client/app.py", "compute"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 0 {
		t.Fatalf("expected 0 rewrites (unrelated monorepo entity must not collide), got %d", stats.CallsRewritten)
	}
	if got := records[2].Relationships[0].ToID; got != "compute" {
		t.Fatalf("expected target unchanged, got %q", got)
	}
}

// TestResolveImports_SrcPrefixStripped covers the conservative
// allowlisted-prefix strip kept in modulesForFile: a file at
// "src/requests/api.py" should still resolve when imported as
// "requests.api".
func TestResolveImports_SrcPrefixStripped(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("client/app.py", "requests.api.get", map[string]string{
			"local_name":    "get",
			"source_module": "requests.api",
			"imported_name": "get",
		}),
		targetRecord("get", "src/requests/api.py", "5555555555555555"),
		callerRecord("run", "client/app.py", "get"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 1 {
		t.Fatalf("expected 1 rewrite via src/ prefix strip, got %d", stats.CallsRewritten)
	}
	if got := records[2].Relationships[0].ToID; got != "5555555555555555" {
		t.Fatalf("expected target 5555555555555555, got %q", got)
	}
}

// TestResolveImports_PlainImportAmbiguous asserts deterministic
// non-resolution when two plain `import` statements both expose the
// same bare name. The pre-fix code iterated the file bucket map and
// short-circuited on the first hit — a non-deterministic pick across
// runs. The post-fix collects all candidates and drops on >1.
func TestResolveImports_PlainImportAmbiguous(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("client/app.py", "alpha", map[string]string{
			"local_name":    "alpha",
			"source_module": "alpha",
			"imported_name": "alpha",
		}),
		importerRecord("client/app.py", "beta", map[string]string{
			"local_name":    "beta",
			"source_module": "beta",
			"imported_name": "beta",
		}),
		// Both alpha and beta export a function named `tick`.
		targetRecord("tick", "alpha/__init__.py", "6666666666666666"),
		targetRecord("tick", "beta/__init__.py", "7777777777777777"),
		callerRecord("run", "client/app.py", "tick"),
	}
	// Run repeatedly — pre-fix this would flap between the two IDs
	// depending on Go's randomised map iteration order. Post-fix it
	// must always drop (rewritten==0).
	for i := 0; i < 16; i++ {
		// Reset the caller's CALLS edge each iteration so any rewrite
		// from a previous iteration doesn't mask flakiness.
		records[4].Relationships[0].ToID = "tick"
		tbl := BuildImportTable(records)
		stats := ResolveImports(records, tbl)
		if stats.CallsRewritten != 0 {
			t.Fatalf("iter %d: expected 0 rewrites under plain-import ambiguity, got %d (target=%q)",
				i, stats.CallsRewritten, records[4].Relationships[0].ToID)
		}
		if got := records[4].Relationships[0].ToID; got != "tick" {
			t.Fatalf("iter %d: expected target unchanged, got %q", i, got)
		}
	}
}

// TestResolveImports_DottedImportEdgeRewrite (issue #142) covers the
// dominant python-flask-realworld bug-resolver pattern: a project-internal
// IMPORTS edge whose ToID is the full dotted module path
// (`conduit.database.db`). The Python extractor emits ToID as the full
// dotted path, but the entity for `db` lives at conduit/database.py with
// QualifiedName="" (Python entities don't carry QualifiedName), so the
// downstream Index resolver misses byQualifiedName / byName / byKind and
// the edge ends up classified as bug-resolver.
//
// ResolveImports must rewrite the IMPORTS ToID to the underlying entity
// ID by splitting the dotted path tail-first into (module, leaf) and
// probing the per-module reverse index built in BuildImportTable.
func TestResolveImports_DottedImportEdgeRewrite(t *testing.T) {
	records := []types.EntityRecord{
		// Importing file: `from conduit.database import db`. The Python
		// extractor emits ToID = "conduit.database.db" (modPath + "." + name).
		importerRecord("app/views.py", "conduit.database.db", map[string]string{
			"local_name":    "db",
			"source_module": "conduit.database",
			"imported_name": "db",
		}),
		// Real entity for `db` lives at conduit/database.py with name "db".
		targetRecord("db", "conduit/database.py", "8888888888888888"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ImportsRewritten != 1 {
		t.Fatalf("expected 1 IMPORTS rewrite, got %d (considered=%d)", stats.ImportsRewritten, stats.ImportsConsidered)
	}
	// The IMPORTS edge on the importer marker entity should now point at
	// the real entity ID, not the dotted-path stub.
	if got := records[0].Relationships[0].ToID; got != "8888888888888888" {
		t.Fatalf("expected IMPORTS ToID rewritten to 8888888888888888, got %q", got)
	}
}

// TestResolveImports_DottedImportEdgePackageInit covers `from conduit.models
// import db` where `db` is exported from conduit/models/__init__.py.
// modulesForFile already maps __init__.py to the parent package's dotted
// form, so the (conduit.models, db) tuple resolves.
func TestResolveImports_DottedImportEdgePackageInit(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("app/views.py", "conduit.models.db", map[string]string{
			"local_name":    "db",
			"source_module": "conduit.models",
			"imported_name": "db",
		}),
		targetRecord("db", "conduit/models/__init__.py", "9999999999999999"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ImportsRewritten != 1 {
		t.Fatalf("expected 1 IMPORTS rewrite, got %d (considered=%d)", stats.ImportsRewritten, stats.ImportsConsidered)
	}
	if got := records[0].Relationships[0].ToID; got != "9999999999999999" {
		t.Fatalf("expected IMPORTS ToID rewritten to 9999999999999999, got %q", got)
	}
}

// TestResolveImports_DottedImportEdgeExternalLeftAlone covers
// `from marshmallow import Schema` where marshmallow is NOT in the
// corpus. The dotted ToID "marshmallow.Schema" must be left alone so
// the external-synthesis pass can route it to ext:marshmallow.
func TestResolveImports_DottedImportEdgeExternalLeftAlone(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("app/views.py", "marshmallow.Schema", map[string]string{
			"local_name":    "Schema",
			"source_module": "marshmallow",
			"imported_name": "Schema",
		}),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ImportsRewritten != 0 {
		t.Fatalf("expected 0 IMPORTS rewrites for external package, got %d", stats.ImportsRewritten)
	}
	if got := records[0].Relationships[0].ToID; got != "marshmallow.Schema" {
		t.Fatalf("expected IMPORTS ToID unchanged, got %q", got)
	}
}

// TestResolveImports_DottedImportPlainModule covers `import conduit.database`
// — the IMPORTS ToID is just the module path "conduit.database", with NO
// leaf symbol. The resolver should not attempt to rewrite the edge in
// this shape (there is no project-internal entity that uniquely is
// "the module" for a plain import — the marker entity itself is what
// the IMPORTS edge points at by convention).
func TestResolveImports_DottedImportPlainModule(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("app/views.py", "conduit.database", map[string]string{
			"local_name":    "conduit",
			"source_module": "conduit.database",
			"imported_name": "conduit.database",
		}),
		targetRecord("db", "conduit/database.py", "aaaa1111aaaa1111"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ImportsRewritten != 0 {
		t.Fatalf("expected 0 IMPORTS rewrites for plain module import, got %d", stats.ImportsRewritten)
	}
	if got := records[0].Relationships[0].ToID; got != "conduit.database" {
		t.Fatalf("expected IMPORTS ToID unchanged for plain module, got %q", got)
	}
}

// TestModulesForFile_Java covers the Java dispatch added in #120 —
// `src/main/java/com/foo/Bar.java` is the canonical Maven layout for
// Java package `com.foo` containing class `Bar`. The module-derivation
// must yield "com.foo" (the canonical Maven-stripped form) and may
// also yield the pre-strip "src.main.java.com.foo" alias to keep
// backward-compatible indexing.
func TestModulesForFile_Java(t *testing.T) {
	got := modulesForFile("src/main/java/com/foo/Bar.java")
	want := "com.foo"
	found := false
	for _, m := range got {
		if m == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("modulesForFile Java: expected %q in %v", want, got)
	}
	// File at repo root should return nil — caller treats that as
	// "no module".
	if got := modulesForFile("Test.java"); got != nil {
		t.Fatalf("modulesForFile root-level java: expected nil, got %v", got)
	}
}

// TestResolveImports_JavaFromImport covers Java cross-file class
// binding (issue #120). `import com.foo.Bar;` introduces local name
// "Bar" into the importing file. A bare-name CALLS target equal to
// "Bar" should rewrite to the entity ID of class Bar declared in
// src/main/java/com/foo/Bar.java.
func TestResolveImports_JavaFromImport(t *testing.T) {
	records := []types.EntityRecord{
		{
			Name:       "com.foo.Bar",
			Kind:       "SCOPE.Component",
			SourceFile: "src/main/java/x/App.java",
			Language:   "java",
			Relationships: []types.RelationshipRecord{{
				FromID: "src/main/java/x/App.java",
				ToID:   "com.foo.Bar",
				Kind:   importRelKind,
				Properties: map[string]string{
					"local_name":    "Bar",
					"source_module": "com.foo",
					"imported_name": "Bar",
				},
			}},
		},
		// Class Bar declared in com/foo/Bar.java.
		{
			ID:         "9999999999999999",
			Name:       "Bar",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/main/java/com/foo/Bar.java",
			Language:   "java",
		},
		// Caller in App.java with a bare CALLS target "Bar"
		// (e.g. `new Bar()` would normally produce the same bare
		// target post-extraction).
		{
			ID:         "1234567890abcdef",
			Name:       "App.run",
			Kind:       "SCOPE.Operation",
			SourceFile: "src/main/java/x/App.java",
			Language:   "java",
			Relationships: []types.RelationshipRecord{{
				ToID: "Bar",
				Kind: "CALLS",
			}},
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 1 {
		t.Fatalf("expected 1 java rewrite, got %d (considered=%d)",
			stats.CallsRewritten, stats.CallsConsidered)
	}
	if got := records[2].Relationships[0].ToID; got != "9999999999999999" {
		t.Fatalf("expected target rewritten to 9999999999999999, got %q", got)
	}
}

// TestResolveImports_JavaSrcMainJavaStripped confirms the canonical
// Maven layout (`src/main/java/...`) is treated equivalently to a
// repo-relative dotted path. Without the strip, an import of
// `com.foo.Bar` would not bind to `src/main/java/com/foo/Bar.java`
// because the file's dotted form would be
// `src.main.java.com.foo` and the import's source_module is plain
// `com.foo`.
func TestResolveImports_JavaSrcMainJavaStripped(t *testing.T) {
	records := []types.EntityRecord{
		{
			Name:       "com.foo.Bar",
			Kind:       "SCOPE.Component",
			SourceFile: "src/main/java/x/App.java",
			Language:   "java",
			Relationships: []types.RelationshipRecord{{
				FromID: "src/main/java/x/App.java",
				ToID:   "com.foo.Bar",
				Kind:   importRelKind,
				Properties: map[string]string{
					"local_name":    "Bar",
					"source_module": "com.foo",
					"imported_name": "Bar",
				},
			}},
		},
		{
			ID:         "abcdef0123456789",
			Name:       "Bar",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/main/java/com/foo/Bar.java",
			Language:   "java",
		},
		{
			ID:         "1111111122222222",
			Name:       "App.run",
			Kind:       "SCOPE.Operation",
			SourceFile: "src/main/java/x/App.java",
			Language:   "java",
			Relationships: []types.RelationshipRecord{{
				ToID: "Bar",
				Kind: "CALLS",
			}},
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 1 {
		t.Fatalf("expected 1 rewrite via src/main/java strip, got %d", stats.CallsRewritten)
	}
	if got := records[2].Relationships[0].ToID; got != "abcdef0123456789" {
		t.Fatalf("expected target abcdef0123456789, got %q", got)
	}
}

// TestModulesForFile_PHP covers the PHP dispatch added in #113 —
// `src/Entity/Post.php` is the canonical Symfony PSR-4 layout for a
// class living at the namespace `App\Entity\Post`. The module-derivation
// must yield `App.Entity` (PSR-4 strip + `App` re-prefix) so an IMPORTS
// edge whose ToID is `App\Entity\Post` (normalized to `App.Entity.Post`)
// resolves via the per-module reverse index. The pre-strip
// `src.Entity` form is also retained so a corpus indexed without PSR-4
// awareness still binds.
func TestModulesForFile_PHP(t *testing.T) {
	got := modulesForFile("src/Entity/Post.php")
	want := "App.Entity"
	found := false
	for _, m := range got {
		if m == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("modulesForFile PHP: expected %q in %v", want, got)
	}
	// Laravel default: app/ → App
	got = modulesForFile("app/Models/User.php")
	wantLaravel := "App.Models"
	found = false
	for _, m := range got {
		if m == wantLaravel {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("modulesForFile PHP Laravel: expected %q in %v", wantLaravel, got)
	}
	// File at repo root should return nil.
	if got := modulesForFile("Index.php"); got != nil {
		t.Fatalf("modulesForFile root-level php: expected nil, got %v", got)
	}
}

// TestResolveImports_PHPProjectLocalNamespace covers the PHP analogue
// of #93/#142 (issue #113). An IMPORTS edge whose ToID is
// `App\Entity\Post` should rewrite to the entity ID of class Post
// declared in `src/Entity/Post.php`. The backslash separator is
// normalized to dotted form before the per-module lookup.
func TestResolveImports_PHPProjectLocalNamespace(t *testing.T) {
	records := []types.EntityRecord{
		// PHP `use App\Entity\Post;` in a Form file.
		{
			Name:       "App",
			Kind:       "SCOPE.Component",
			SourceFile: "src/Form/PostType.php",
			Language:   "php",
			Relationships: []types.RelationshipRecord{{
				FromID: "src/Form/PostType.php",
				ToID:   "App\\Entity\\Post",
				Kind:   importRelKind,
				Properties: map[string]string{
					"local_name":    "Post",
					"source_module": "App.Entity",
					"imported_name": "Post",
				},
			}},
		},
		// Class Post declared in src/Entity/Post.php.
		{
			ID:         "aaaa1111aaaa1111",
			Name:       "Post",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/Entity/Post.php",
			Language:   "php",
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ImportsRewritten != 1 {
		t.Fatalf("expected 1 PHP IMPORTS rewrite, got %d (considered=%d)",
			stats.ImportsRewritten, stats.ImportsConsidered)
	}
	if got := records[0].Relationships[0].ToID; got != "aaaa1111aaaa1111" {
		t.Fatalf("expected target aaaa1111aaaa1111, got %q", got)
	}
}

// TestResolveImports_PHPSameLeafTwoNamespaces covers the disambiguation
// case: two classes both named `User` live in different project-internal
// namespaces (`App\Entity\User` vs `App\Security\User`). An importer of
// `App\Entity\User` must resolve to the Entity, not Security, version.
func TestResolveImports_PHPSameLeafTwoNamespaces(t *testing.T) {
	records := []types.EntityRecord{
		{
			Name:       "App",
			Kind:       "SCOPE.Component",
			SourceFile: "src/Controller/UserController.php",
			Language:   "php",
			Relationships: []types.RelationshipRecord{{
				FromID: "src/Controller/UserController.php",
				ToID:   "App\\Entity\\User",
				Kind:   importRelKind,
				Properties: map[string]string{
					"local_name":    "User",
					"source_module": "App.Entity",
					"imported_name": "User",
				},
			}},
		},
		{
			ID:         "1111aaaa1111aaaa",
			Name:       "User",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/Entity/User.php",
			Language:   "php",
		},
		{
			ID:         "2222bbbb2222bbbb",
			Name:       "User",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/Security/User.php",
			Language:   "php",
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ImportsRewritten != 1 {
		t.Fatalf("expected 1 PHP rewrite, got %d", stats.ImportsRewritten)
	}
	if got := records[0].Relationships[0].ToID; got != "1111aaaa1111aaaa" {
		t.Fatalf("expected Entity\\User (1111aaaa1111aaaa), got %q", got)
	}
}

// TestResolveImports_PHPExternalNamespaceLeftAlone confirms that an
// IMPORTS edge to a non-project namespace (`Symfony\Component\...`)
// misses the per-module index and is left for the external-synthesis
// pass — the resolver must not fabricate a binding to a coincidentally
// same-named project entity.
func TestResolveImports_PHPExternalNamespaceLeftAlone(t *testing.T) {
	records := []types.EntityRecord{
		{
			Name:       "Symfony",
			Kind:       "SCOPE.Component",
			SourceFile: "src/Form/PostType.php",
			Language:   "php",
			Relationships: []types.RelationshipRecord{{
				FromID: "src/Form/PostType.php",
				ToID:   "Symfony\\Component\\Form\\AbstractType",
				Kind:   importRelKind,
				Properties: map[string]string{
					"local_name":    "AbstractType",
					"source_module": "Symfony.Component.Form",
					"imported_name": "AbstractType",
				},
			}},
		},
		// A coincidentally-named project class — must NOT bind.
		{
			ID:         "ccccddddccccdddd",
			Name:       "AbstractType",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/Form/AbstractType.php",
			Language:   "php",
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ImportsRewritten != 0 {
		t.Fatalf("expected 0 rewrites for external namespace, got %d", stats.ImportsRewritten)
	}
	if got := records[0].Relationships[0].ToID; got != "Symfony\\Component\\Form\\AbstractType" {
		t.Fatalf("expected ToID preserved, got %q", got)
	}
}

// TestResolveImports_PHPFQNMethodTarget covers issue #422 — Symfony
// YAML routing strings of the form `App\Controller\BlogController::list`
// arrive on CALLS/IMPORTS edges and must rewrite to the entity ID of
// the `list` method declared in `src/Controller/BlogController.php`.
//
// The fix mirrors the dotted-import handling for #113 and #142: split
// the FQN-method shape on `::`, normalise the namespace (backslash →
// dot), resolve the class via the per-module reverse index, then bind
// the trailing method name to a method entity that lives in the class's
// SourceFile.
func TestResolveImports_PHPFQNMethodTarget(t *testing.T) {
	records := []types.EntityRecord{
		// YAML route entity (cross-extractor) carrying a CALLS edge to
		// the FQN-method shape Symfony's _controller field uses.
		{
			Name:       "blog_list",
			Kind:       "SCOPE.Operation",
			Subtype:    "route",
			SourceFile: "config/routes.yaml",
			Language:   "yaml",
			Relationships: []types.RelationshipRecord{{
				FromID: "config/routes.yaml",
				ToID:   "App\\Controller\\BlogController::list",
				Kind:   "CALLS",
			}},
		},
		// Class BlogController declared in src/Controller/BlogController.php.
		{
			ID:         "aaaa1111aaaa1111",
			Name:       "BlogController",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/Controller/BlogController.php",
			Language:   "php",
		},
		// Method `list` declared in the same file.
		{
			ID:         "bbbb2222bbbb2222",
			Name:       "list",
			Kind:       "SCOPE.Operation",
			Subtype:    "method",
			SourceFile: "src/Controller/BlogController.php",
			Language:   "php",
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.PHPFQNMethodRewritten != 1 {
		t.Fatalf("expected 1 PHP FQN-method rewrite, got %d (considered=%d)",
			stats.PHPFQNMethodRewritten, stats.PHPFQNMethodConsidered)
	}
	if got := records[0].Relationships[0].ToID; got != "bbbb2222bbbb2222" {
		t.Fatalf("expected target bbbb2222bbbb2222, got %q", got)
	}
}

// TestResolveImports_PHPFQNMethodAlreadyDotted covers the same shape
// expressed with dot separators — `App.Controller.BlogController::list`
// — which the YAML cross-extractor may emit if it normalises FQNs
// before stamping ToID.
func TestResolveImports_PHPFQNMethodAlreadyDotted(t *testing.T) {
	records := []types.EntityRecord{
		{
			Name:       "blog_list",
			Kind:       "SCOPE.Operation",
			Subtype:    "route",
			SourceFile: "config/routes.yaml",
			Language:   "yaml",
			Relationships: []types.RelationshipRecord{{
				FromID: "config/routes.yaml",
				ToID:   "App.Controller.BlogController::list",
				Kind:   "CALLS",
			}},
		},
		{
			ID:         "aaaa1111aaaa1111",
			Name:       "BlogController",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/Controller/BlogController.php",
			Language:   "php",
		},
		{
			ID:         "bbbb2222bbbb2222",
			Name:       "list",
			Kind:       "SCOPE.Operation",
			Subtype:    "method",
			SourceFile: "src/Controller/BlogController.php",
			Language:   "php",
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.PHPFQNMethodRewritten != 1 {
		t.Fatalf("expected 1 PHP FQN-method rewrite (dotted), got %d", stats.PHPFQNMethodRewritten)
	}
	if got := records[0].Relationships[0].ToID; got != "bbbb2222bbbb2222" {
		t.Fatalf("expected target bbbb2222bbbb2222, got %q", got)
	}
}

// TestResolveImports_PHPFQNMethodExternalClassLeftAlone confirms that an
// FQN-method whose class lives in an external namespace (Symfony,
// Doctrine, …) misses the per-module index and is left for the
// external-synthesis pass.
func TestResolveImports_PHPFQNMethodExternalClassLeftAlone(t *testing.T) {
	records := []types.EntityRecord{
		{
			Name:       "evt",
			Kind:       "SCOPE.Operation",
			Subtype:    "route",
			SourceFile: "config/services.yaml",
			Language:   "yaml",
			Relationships: []types.RelationshipRecord{{
				FromID: "config/services.yaml",
				ToID:   "Symfony\\Component\\HttpKernel\\HttpKernel::handle",
				Kind:   "CALLS",
			}},
		},
		// A coincidentally-named project method — must NOT bind.
		{
			ID:         "deaddeaddeaddead",
			Name:       "handle",
			Kind:       "SCOPE.Operation",
			Subtype:    "method",
			SourceFile: "src/Other/Thing.php",
			Language:   "php",
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.PHPFQNMethodRewritten != 0 {
		t.Fatalf("expected 0 rewrites for external class, got %d", stats.PHPFQNMethodRewritten)
	}
	if got := records[0].Relationships[0].ToID; got != "Symfony\\Component\\HttpKernel\\HttpKernel::handle" {
		t.Fatalf("expected ToID preserved, got %q", got)
	}
}

// TestResolveImports_PHPFQNMethodAmbiguousMethodLeftAlone covers the
// case where the resolved class file contains two methods with the same
// name (overloaded? unlikely in PHP but defensively handled). Conservative
// policy: drop the binding rather than guess.
func TestResolveImports_PHPFQNMethodAmbiguousMethodLeftAlone(t *testing.T) {
	records := []types.EntityRecord{
		{
			Name:       "blog_list",
			Kind:       "SCOPE.Operation",
			Subtype:    "route",
			SourceFile: "config/routes.yaml",
			Language:   "yaml",
			Relationships: []types.RelationshipRecord{{
				FromID: "config/routes.yaml",
				ToID:   "App\\Controller\\BlogController::list",
				Kind:   "CALLS",
			}},
		},
		{
			ID:         "aaaa1111aaaa1111",
			Name:       "BlogController",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/Controller/BlogController.php",
			Language:   "php",
		},
		{
			ID:         "bbbb2222bbbb2222",
			Name:       "list",
			Kind:       "SCOPE.Operation",
			Subtype:    "method",
			SourceFile: "src/Controller/BlogController.php",
			Language:   "php",
		},
		{
			ID:         "cccc3333cccc3333",
			Name:       "list",
			Kind:       "SCOPE.Operation",
			Subtype:    "method",
			SourceFile: "src/Controller/BlogController.php",
			Language:   "php",
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.PHPFQNMethodRewritten != 0 {
		t.Fatalf("expected 0 rewrites for ambiguous method, got %d", stats.PHPFQNMethodRewritten)
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

// TestModulesForFile_JSTS covers the JS/TS dispatch added in issue
// #421. JavaScript and TypeScript do not have a language-level package
// concept; modules are file-relative. The module-derivation strips the
// recognised extension and replaces forward slashes with dots, plus a
// conservative single-strip of the well-known `src.` source root used
// by Nest, Angular, and most npm packages.
func TestModulesForFile_JSTS(t *testing.T) {
	cases := []struct {
		path string
		want string // canonical post-strip form
	}{
		{"src/services/user.service.ts", "services.user.service"},
		{"src/users/users.controller.ts", "users.users.controller"},
		{"app/models/user.ts", "models.user"},
		{"lib/util/format.js", "util.format"},
		// .tsx / .jsx and CommonJS variants.
		{"src/components/Button.tsx", "components.Button"},
		{"src/index.mjs", "index"},
	}
	for _, tc := range cases {
		got := modulesForFile(tc.path)
		found := false
		for _, m := range got {
			if m == tc.want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("modulesForFile(%q): expected %q in %v", tc.path, tc.want, got)
		}
	}
	// File at repo root with no source-root strip — single dotted form.
	got := modulesForFile("index.ts")
	if len(got) == 0 || got[0] != "index" {
		t.Errorf("modulesForFile(index.ts): expected [index ...], got %v", got)
	}
	// Unknown extension returns nil.
	if got := modulesForFile("README.md"); got != nil {
		t.Errorf("modulesForFile(README.md): expected nil, got %v", got)
	}
}

// TestResolveImports_TypeScriptCrossFileNamedImport (issue #421) —
// a TypeScript named import `{ UserService } from "./services/user.service"`
// emits an IMPORTS edge carrying local_name=UserService,
// source_module=src.users.services.user.service (canonical post-strip
// form). A bare-name CALLS target "UserService" in the same file
// rewrites to the entity ID of the UserService class declared in
// src/users/services/user.service.ts. This is the resolver-side
// fallback path complementing the extractor-side structural-ref
// emission for the dominant `<recv>.<method>` shape.
func TestResolveImports_TypeScriptCrossFileNamedImport(t *testing.T) {
	records := []types.EntityRecord{
		// Import entity in users.controller.ts.
		{
			Name:       "./services/user.service",
			Kind:       "SCOPE.Component",
			Subtype:    "import",
			SourceFile: "src/users/users.controller.ts",
			Language:   "typescript",
			Relationships: []types.RelationshipRecord{{
				FromID: "src/users/users.controller.ts",
				ToID:   "./services/user.service",
				Kind:   importRelKind,
				Properties: map[string]string{
					"local_name":    "UserService",
					"source_module": "src.users.services.user.service",
					"imported_name": "UserService",
				},
			}},
		},
		// Class UserService in the imported file.
		{
			ID:         "deadbeefcafef00d",
			Name:       "UserService",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/users/services/user.service.ts",
			Language:   "typescript",
		},
		// Caller method in the controller — bare CALLS target
		// "UserService" (e.g. emitted by `new UserService()` shape).
		{
			ID:         "1111222233334444",
			Name:       "constructor",
			Kind:       "SCOPE.Operation",
			SourceFile: "src/users/users.controller.ts",
			Language:   "typescript",
			Relationships: []types.RelationshipRecord{{
				ToID: "UserService",
				Kind: "CALLS",
			}},
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 1 {
		t.Fatalf("expected 1 TS rewrite, got %d (considered=%d)",
			stats.CallsRewritten, stats.CallsConsidered)
	}
	if got := records[2].Relationships[0].ToID; got != "deadbeefcafef00d" {
		t.Fatalf("expected target rewritten to deadbeefcafef00d, got %q", got)
	}
}

// markdownDocRecord builds a SCOPE.Document EntityRecord for a markdown
// file, optionally carrying an IMPORTS edge to another file's path.
// Mirrors what internal/extractors/markdown emits.
func markdownDocRecord(file, importTarget, importerID string) types.EntityRecord {
	rec := types.EntityRecord{
		ID:            importerID,
		Name:          file,
		QualifiedName: file,
		Kind:          "SCOPE.Document",
		Subtype:       "markdown",
		Language:      "markdown",
		SourceFile:    file,
	}
	if importTarget != "" {
		rec.Relationships = []types.RelationshipRecord{{
			FromID: file,
			ToID:   importTarget,
			Kind:   "IMPORTS",
			Properties: map[string]string{
				"source_module": importTarget,
				"imported_name": importTarget,
				"import_kind":   "link",
				"language":      "markdown",
			},
		}}
	}
	return rec
}

// TestResolveImports_MarkdownFilePathTarget — issue #44 follow-up. A
// markdown link like `[list](./applicationset/list.yaml)` emits an
// IMPORTS edge whose ToID is the resolved file path
// "applicationset/list.yaml". The target file has a real entity in the
// graph (e.g. a SCOPE.Component emitted by the YAML extractor), and the
// resolver should bind the edge to that entity.
func TestResolveImports_MarkdownFilePathTarget(t *testing.T) {
	records := []types.EntityRecord{
		// YAML entity that the link should bind to.
		{
			ID:         "1111111111111111",
			Name:       "list",
			Kind:       "SCOPE.Component",
			SourceFile: "applicationset/list.yaml",
			Language:   "yaml",
		},
		// Markdown doc that links to the YAML.
		markdownDocRecord("applicationset/README.md", "applicationset/list.yaml", "2222222222222222"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.MarkdownFilePathRewritten != 1 {
		t.Fatalf("expected 1 markdown file-path rewrite, got %d (considered=%d)",
			stats.MarkdownFilePathRewritten, stats.MarkdownFilePathConsidered)
	}
	if got := records[1].Relationships[0].ToID; got != "1111111111111111" {
		t.Fatalf("expected ToID rewritten to 1111111111111111, got %q", got)
	}
}

// TestResolveImports_MarkdownDirTarget — issue #44 follow-up. A
// markdown link like `[kasane](./plugins/kasane)` to a bare directory
// should bind to that directory's README.md SCOPE.Document.
func TestResolveImports_MarkdownDirTarget(t *testing.T) {
	records := []types.EntityRecord{
		// The README.md Document inside the linked dir.
		markdownDocRecord("plugins/kasane/README.md", "", "3333333333333333"),
		// Top-level README.md linking to plugins/kasane.
		markdownDocRecord("README.md", "plugins/kasane", "4444444444444444"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.MarkdownFilePathRewritten != 1 {
		t.Fatalf("expected 1 markdown dir rewrite, got %d (considered=%d)",
			stats.MarkdownFilePathRewritten, stats.MarkdownFilePathConsidered)
	}
	if got := records[1].Relationships[0].ToID; got != "3333333333333333" {
		t.Fatalf("expected dir link bound to plugins/kasane/README.md, got %q", got)
	}
}

// TestResolveImports_MarkdownDocPreferredOverOther — when both a
// SCOPE.Document and another entity share a file, the Document is the
// preferred binding target (rank 1 beats rank 2).
func TestResolveImports_MarkdownDocPreferredOverOther(t *testing.T) {
	records := []types.EntityRecord{
		// Non-document entity emitted FIRST so we exercise the rank
		// override path (Document arrives later but still wins).
		{
			ID:         "5555555555555555",
			Name:       "Heading",
			Kind:       "SCOPE.Heading",
			SourceFile: "docs/guide.md",
			Language:   "markdown",
		},
		markdownDocRecord("docs/guide.md", "", "6666666666666666"),
		markdownDocRecord("README.md", "docs/guide.md", "7777777777777777"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.MarkdownFilePathRewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.MarkdownFilePathRewritten)
	}
	if got := records[2].Relationships[0].ToID; got != "6666666666666666" {
		t.Fatalf("expected Document (6666...) preferred over Heading (5555...), got %q", got)
	}
}

// TestResolveImports_MarkdownFilePathMiss — links to files that aren't
// in the indexed graph (external repo, missing file) should NOT be
// rewritten. Counted in Considered but not Rewritten.
func TestResolveImports_MarkdownFilePathMiss(t *testing.T) {
	records := []types.EntityRecord{
		markdownDocRecord("README.md", "../sibling-repo/file.md", "8888888888888888"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.MarkdownFilePathRewritten != 0 {
		t.Fatalf("expected 0 rewrites for missing file, got %d", stats.MarkdownFilePathRewritten)
	}
	if stats.MarkdownFilePathConsidered != 1 {
		t.Fatalf("expected 1 considered, got %d", stats.MarkdownFilePathConsidered)
	}
	if got := records[0].Relationships[0].ToID; got != "../sibling-repo/file.md" {
		t.Fatalf("expected ToID untouched, got %q", got)
	}
}

// scalaImporterRecord mirrors importerRecord but tags Language="scala"
// so the language-conditional dedup helpers run the scala branch.
func scalaImporterRecord(file, modulePath string, props map[string]string) types.EntityRecord {
	return types.EntityRecord{
		Name:       modulePath,
		Kind:       "SCOPE.Component",
		SourceFile: file,
		Language:   "scala",
		Relationships: []types.RelationshipRecord{{
			FromID:     file,
			ToID:       modulePath,
			Kind:       importRelKind,
			Properties: props,
		}},
	}
}

// scalaScopeTarget builds a Scala SCOPE.Component entity (the canonical
// projection emitted by the Scala extractor's class_definition path).
func scalaScopeTarget(name, file, id string) types.EntityRecord {
	return types.EntityRecord{
		ID:         id,
		Name:       name,
		Kind:       "SCOPE.Component",
		Subtype:    "class",
		SourceFile: file,
		Language:   "scala",
	}
}

// scalaFrameworkTarget builds a Pass 2.5-style framework-alias entity
// (Play YAML rules produce a Controller kind for the same class file).
func scalaFrameworkTarget(name, file, id string) types.EntityRecord {
	return types.EntityRecord{
		ID:         id,
		Name:       name,
		Kind:       "Controller",
		SourceFile: file,
		Language:   "scala",
	}
}

// TestModulesForScalaFile_PlayLayout exercises the Play `app/<pkg>` source
// root strip. `app/controllers/AsyncController.scala` should expose both
// the raw dotted form (`app.controllers`) and the post-strip alias
// (`controllers`) so a project-local `import controllers.AsyncController`
// binds to the file.
func TestModulesForScalaFile_PlayLayout(t *testing.T) {
	got := modulesForScalaFile("app/controllers/AsyncController.scala")
	want := map[string]bool{"app.controllers": true, "controllers": true}
	if len(got) == 0 {
		t.Fatalf("expected non-empty module list, got nil")
	}
	for _, m := range got {
		if !want[m] {
			t.Fatalf("unexpected module %q in %v", m, got)
		}
		delete(want, m)
	}
	if len(want) != 0 {
		t.Fatalf("missing modules: %v (got %v)", want, got)
	}
}

// TestModulesForScalaFile_SbtLayout exercises the canonical sbt /
// `src/main/scala/<pkg>` strip used by every non-Play Scala project.
// Both the raw dotted form and the post-strip alias must be returned.
func TestModulesForScalaFile_SbtLayout(t *testing.T) {
	got := modulesForScalaFile("src/main/scala/com/example/Foo.scala")
	// At minimum the raw dotted form and the canonical sbt-strip alias
	// must be present. The generic "src." top-level strip may add a
	// further `main.scala.com.example` alias — that is harmless and
	// only matters when an entity is actually indexed at the matching
	// dotted path, which is vanishingly rare in real projects.
	required := map[string]bool{
		"src.main.scala.com.example": true,
		"com.example":                true,
	}
	for _, m := range got {
		delete(required, m)
	}
	if len(required) != 0 {
		t.Fatalf("missing required modules: %v (got %v)", required, got)
	}
}

// TestModulesForScalaFile_RepoRootBail covers a `.scala` file directly at
// the repo root — there is no parent directory and therefore no package
// path. modulesForScalaFile must return nil so the caller's nil-guards
// treat the entity as unrouted.
func TestModulesForScalaFile_RepoRootBail(t *testing.T) {
	if got := modulesForScalaFile("Top.scala"); got != nil {
		t.Fatalf("expected nil for repo-root .scala, got %v", got)
	}
}

// TestResolveImports_ScalaPlayProjectLocal is the end-to-end shape that
// drove play-scala-starter from 7.75 percent bug-rate to <=3 percent.
// A test file outside `app/` imports a project-local Play controller via
// its bare package path. Pre-fix this landed in bug-extractor because
// modulesForFile had no scala arm. Post-fix the import-aware resolver
// rewrites the IMPORTS ToID to the SCOPE.Component entity ID.
func TestResolveImports_ScalaPlayProjectLocal(t *testing.T) {
	records := []types.EntityRecord{
		scalaImporterRecord("test/UnitSpec.scala", "controllers.AsyncController", map[string]string{
			"local_name":    "AsyncController",
			"source_module": "controllers",
			"imported_name": "AsyncController",
		}),
		scalaScopeTarget("AsyncController", "app/controllers/AsyncController.scala", "aaaabbbbccccdddd"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ImportsRewritten != 1 {
		t.Fatalf("expected 1 IMPORTS rewrite (project-local scala import), got %d", stats.ImportsRewritten)
	}
	if got := records[0].Relationships[0].ToID; got != "aaaabbbbccccdddd" {
		t.Fatalf("expected ToID rewritten to scope target, got %q", got)
	}
}

// TestResolveImports_ScalaPlayFrameworkProjectionNotAmbiguous covers the
// secondary play-scala-starter symptom: the Scala extractor emits a
// SCOPE.Component for AsyncController and the Play framework YAML rules
// emit a separate Controller-kind alias for the SAME file. Pre-fix the
// per-module reverse index marked (controllers, AsyncController)
// ambiguous and the IMPORTS edge from test/UnitSpec.scala fell to
// bug-extractor. Post-fix the same-file framework-projection dedup
// (shared with PHP, issue #485 follow-up) keeps the SCOPE.Component as
// the binding target.
func TestResolveImports_ScalaPlayFrameworkProjectionNotAmbiguous(t *testing.T) {
	records := []types.EntityRecord{
		scalaImporterRecord("test/UnitSpec.scala", "controllers.AsyncController", map[string]string{
			"local_name":    "AsyncController",
			"source_module": "controllers",
			"imported_name": "AsyncController",
		}),
		scalaScopeTarget("AsyncController", "app/controllers/AsyncController.scala", "1111222233334444"),
		scalaFrameworkTarget("AsyncController", "app/controllers/AsyncController.scala", "5555666677778888"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ImportsRewritten != 1 {
		t.Fatalf("expected 1 IMPORTS rewrite despite framework projection, got %d", stats.ImportsRewritten)
	}
	if got := records[0].Relationships[0].ToID; got != "1111222233334444" {
		t.Fatalf("expected SCOPE.Component (1111...) preferred over framework alias (5555...), got %q", got)
	}
}

// TestPruneImportPlaceholders_RewritesIMPORTSToFileCarrier exercises
// the PR #642 regression fix: before pruning a JS/TS import placeholder,
// every IMPORTS edge whose ToID points at the placeholder (either by
// its stamped hex ID, post-ResolveImports rewrite, or by the raw
// relative-path module string emitted by the JS extractor) must be
// rewritten to the file-level SCOPE.Component (subtype="file") carrier
// for the resolved import target. Without this rewrite the IMPORTS edge
// becomes a dangling reference the moment the placeholder is dropped
// and the entire JS/TS IMPORTS relationship category disappears from
// the graph (regression observed on typescript-react-mini: recall
// 16/16 → 10/16).
func TestPruneImportPlaceholders_RewritesIMPORTSToFileCarrier(t *testing.T) {
	// Three entities in the fixture:
	//   - App.tsx file-level carrier (importer side, hex "aaaa...")
	//   - pages/Home.tsx file-level carrier (target side, hex "bbbb...")
	//   - "./pages/Home" placeholder (subtype=import, hex "cccc...")
	// The importer carries one IMPORTS edge with ToID = the placeholder's
	// hex (resolver-rewritten case) and a second IMPORTS edge with ToID
	// = the raw module string (unresolved case). After PruneImportPlaceholders
	// both edges must point at the pages/Home.tsx carrier's hex.
	records := []types.EntityRecord{
		{
			ID:         "aaaaaaaaaaaaaaaa",
			Name:       "App.tsx",
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			SourceFile: "App.tsx",
			Relationships: []types.RelationshipRecord{
				{FromID: "aaaaaaaaaaaaaaaa", ToID: "cccccccccccccccc", Kind: "IMPORTS"},
				{FromID: "aaaaaaaaaaaaaaaa", ToID: "./pages/Home", Kind: "IMPORTS"},
			},
		},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Name:       "pages/Home.tsx",
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			SourceFile: "pages/Home.tsx",
		},
		{
			ID:         "cccccccccccccccc",
			Name:       "./pages/Home",
			Kind:       "SCOPE.Component",
			Subtype:    "import",
			SourceFile: "App.tsx",
			Properties: map[string]string{"module": "./pages/Home"},
		},
	}
	out, _, stats := PruneImportPlaceholders(records)
	if stats.Pruned != 1 {
		t.Fatalf("expected 1 placeholder pruned, got %d", stats.Pruned)
	}
	if stats.EdgeToIDRewrites != 2 {
		t.Fatalf("expected 2 edge ToID rewrites (hex + raw module), got %d", stats.EdgeToIDRewrites)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 surviving entities, got %d", len(out))
	}
	app := out[0]
	if len(app.Relationships) != 2 {
		t.Fatalf("expected 2 IMPORTS rels on App.tsx, got %d", len(app.Relationships))
	}
	for i, rel := range app.Relationships {
		if rel.ToID != "bbbbbbbbbbbbbbbb" {
			t.Fatalf("rel[%d] ToID = %q, want pages/Home.tsx carrier hex %q",
				i, rel.ToID, "bbbbbbbbbbbbbbbb")
		}
	}
}

// TestResolveRelativeImportTarget_TriesAllJSExtensions guards the
// extension-search ordering inside resolveRelativeImportTarget so a
// future change to jsExtensions doesn't silently stop resolving
// `.tsx` / `.jsx` / `.mjs` / `.cjs` imports.
func TestResolveRelativeImportTarget_TriesAllJSExtensions(t *testing.T) {
	carriers := map[string]string{
		"pages/Home.tsx":          "tsx-id",
		"hooks/useUsers.ts":       "ts-id",
		"components/Foo.jsx":      "jsx-id",
		"components/Bar.js":       "js-id",
		"esm/x.mjs":               "mjs-id",
		"common/legacy.cjs":       "cjs-id",
		"barrel/widgets/index.ts": "barrel-id",
	}
	cases := []struct {
		importer string
		module   string
		want     string
	}{
		{"App.tsx", "./pages/Home", "tsx-id"},
		{"components/UserList.tsx", "../hooks/useUsers", "ts-id"},
		{"App.tsx", "./components/Foo", "jsx-id"},
		{"App.tsx", "./components/Bar", "js-id"},
		{"App.tsx", "./esm/x", "mjs-id"},
		{"App.tsx", "./common/legacy", "cjs-id"},
		{"App.tsx", "./barrel/widgets", "barrel-id"},
	}
	for _, tc := range cases {
		id, ok := resolveRelativeImportTarget(tc.importer, tc.module, carriers)
		if !ok || id != tc.want {
			t.Errorf("resolveRelativeImportTarget(%q, %q) = (%q, %v), want (%q, true)",
				tc.importer, tc.module, id, ok, tc.want)
		}
	}
	// Non-relative specifiers must miss — those are bare-name imports
	// the dotted resolver already handled in ResolveImports.
	if _, ok := resolveRelativeImportTarget("App.tsx", "react", carriers); ok {
		t.Error("resolveRelativeImportTarget should refuse non-relative specifiers")
	}
}

// referencerRecord builds an EntityRecord representing a function whose
// body REFERENCES a same-file structural ref stub. Mirrors what
// internal/extractors/python/references.go:buildPyReferenceTargetID
// emits.
//
// stubFile is the file path embedded in the stub (the caller's file
// path); name is the bare tail. The stub shape is
// `scope:<kind>:ref:python:<stubFile>:<name>`.
func referencerRecord(callerName, callerFile string, stubKind, stubFile, stubName string) types.EntityRecord {
	return types.EntityRecord{
		ID:         "0123456789abcdef",
		Name:       callerName,
		Kind:       "SCOPE.Operation",
		Subtype:    "function",
		SourceFile: callerFile,
		Language:   "python",
		Relationships: []types.RelationshipRecord{{
			ToID: "scope:" + stubKind + ":ref:python:" + stubFile + ":" + stubName,
			Kind: "REFERENCES",
		}},
	}
}

// importerWithToID is like importerRecord but lets the test specify the
// IMPORTS edge's ToID, mirroring what
// internal/extractors/python/imports.go:resolveImportToIDs stamps
// (`ext:<root>[:<name>]` for known external roots; the dotted module
// path otherwise).
func importerWithToID(file, modulePath, toID string, props map[string]string) types.EntityRecord {
	rec := importerRecord(file, modulePath, props)
	rec.Relationships[0].ToID = toID
	return rec
}

// TestResolveImports_ReferencesCrossFileExternal covers the chain-fix
// path for an imported name whose source module is a known external
// package. The Python extractor's resolveImportToIDs has already
// rewritten the IMPORTS edge's ToID to `ext:django:Model`; the
// REFERENCES edge in the caller body is a same-file structural ref. The
// resolver must rewrite the REFERENCES ToID to the binding's `ext:` ID.
func TestResolveImports_ReferencesCrossFileExternal(t *testing.T) {
	records := []types.EntityRecord{
		importerWithToID("api/views.py", "django.db.models", "ext:django:Model", map[string]string{
			"local_name":    "Model",
			"source_module": "django.db.models",
			"imported_name": "Model",
		}),
		referencerRecord("UserView.get", "api/views.py", "component", "api/views.py", "Model"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ReferencesConsidered != 1 {
		t.Fatalf("expected 1 considered, got %d", stats.ReferencesConsidered)
	}
	if stats.ReferencesRewritten != 1 {
		t.Fatalf("expected 1 rewritten, got %d", stats.ReferencesRewritten)
	}
	if got := records[1].Relationships[0].ToID; got != "ext:django:Model" {
		t.Fatalf("expected REFERENCES rewritten to ext:django:Model, got %q", got)
	}
}

// TestResolveImports_ReferencesCrossFileInternal covers the in-project
// cross-file case: `from app.models import Post` in views.py, with
// `Post` defined in app/models.py. The IMPORTS edge's ToID is the raw
// dotted module path (no `ext:` prefix because the root is not in the
// extractor's known-external list). The resolver must look up the
// (source_module, imported_name) tuple in the per-module reverse index
// and rewrite the REFERENCES ToID to the hex entity ID of Post.
func TestResolveImports_ReferencesCrossFileInternal(t *testing.T) {
	records := []types.EntityRecord{
		importerWithToID("app/views.py", "app.models.Post", "app.models.Post", map[string]string{
			"local_name":    "Post",
			"source_module": "app.models",
			"imported_name": "Post",
		}),
		targetRecord("Post", "app/models.py", "dddddddddddddddd"),
		referencerRecord("list_posts", "app/views.py", "component", "app/views.py", "Post"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ReferencesRewritten != 1 {
		t.Fatalf("expected 1 rewritten, got %d (considered=%d)", stats.ReferencesRewritten, stats.ReferencesConsidered)
	}
	if got := records[2].Relationships[0].ToID; got != "dddddddddddddddd" {
		t.Fatalf("expected REFERENCES rewritten to dddddddddddddddd, got %q", got)
	}
}

// TestResolveImports_ReferencesFileLocalUntouched asserts that a
// structural-ref REFERENCES edge whose tail name corresponds to a
// same-file entity is left alone — the local definition shadows any
// potential import, and the same-file structural-ref pass will bind it
// via byLocation downstream. Rewriting here would skip that path.
func TestResolveImports_ReferencesFileLocalUntouched(t *testing.T) {
	original := "scope:component:ref:python:app/views.py:helper"
	records := []types.EntityRecord{
		// Same file ALSO imports a name `helper` from elsewhere — the
		// local definition still shadows the import in Python semantics.
		importerWithToID("app/views.py", "app.utils.helper", "app.utils.helper", map[string]string{
			"local_name":    "helper",
			"source_module": "app.utils",
			"imported_name": "helper",
		}),
		// File-local definition of `helper` in the same file.
		targetRecord("helper", "app/views.py", "eeeeeeeeeeeeeeee"),
		referencerRecord("entry", "app/views.py", "component", "app/views.py", "helper"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ReferencesConsidered != 0 {
		t.Fatalf("expected 0 considered (shadowed by local), got %d", stats.ReferencesConsidered)
	}
	if got := records[2].Relationships[0].ToID; got != original {
		t.Fatalf("expected REFERENCES ToID untouched, got %q", got)
	}
}

// TestResolveImports_ReferencesUnresolvedNameUntouched asserts that a
// REFERENCES edge whose tail name does NOT correspond to any IMPORTS
// binding in the source file is left alone — we must never fabricate a
// binding. The same-file structural-ref pass will mark this edge as
// unmatched (orphan) downstream; that's the correct disposition.
func TestResolveImports_ReferencesUnresolvedNameUntouched(t *testing.T) {
	original := "scope:component:ref:python:api/views.py:Unknown"
	records := []types.EntityRecord{
		// Caller's file has an import, but for a DIFFERENT name.
		importerWithToID("api/views.py", "django.db.models", "ext:django:Model", map[string]string{
			"local_name":    "Model",
			"source_module": "django.db.models",
			"imported_name": "Model",
		}),
		referencerRecord("entry", "api/views.py", "component", "api/views.py", "Unknown"),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ReferencesRewritten != 0 {
		t.Fatalf("expected 0 rewritten (no import for `Unknown`), got %d", stats.ReferencesRewritten)
	}
	if got := records[1].Relationships[0].ToID; got != original {
		t.Fatalf("expected REFERENCES ToID untouched, got %q", got)
	}
}

// TestResolveImports_ReferencesFormatBSkipped asserts that a Format B
// structural-ref REFERENCES edge (`...:<file>:<scope>#<member>`) is
// skipped — only Format A bare names participate in import-aware
// rewrite. Format B edges encode a member access that the local
// byMember path handles.
func TestResolveImports_ReferencesFormatBSkipped(t *testing.T) {
	original := "scope:component:ref:python:api/views.py:UserView#Model"
	records := []types.EntityRecord{
		importerWithToID("api/views.py", "django.db.models", "ext:django:Model", map[string]string{
			"local_name":    "Model",
			"source_module": "django.db.models",
			"imported_name": "Model",
		}),
		{
			ID:         "0123456789abcdef",
			Name:       "entry",
			Kind:       "SCOPE.Operation",
			Subtype:    "function",
			SourceFile: "api/views.py",
			Language:   "python",
			Relationships: []types.RelationshipRecord{{
				ToID: original,
				Kind: "REFERENCES",
			}},
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ReferencesConsidered != 0 {
		t.Fatalf("expected 0 considered (Format B skipped), got %d", stats.ReferencesConsidered)
	}
	if got := records[1].Relationships[0].ToID; got != original {
		t.Fatalf("expected Format B REFERENCES ToID untouched, got %q", got)
	}
}

// TestResolveImports_ReferencesHexToIDUntouched asserts that a
// REFERENCES edge already pointing at a hex entity ID (e.g. resolved by
// an earlier pass or emitted directly by a cross-extractor) is left
// alone.
// ---------------------------------------------------------------------------
// Issue #778 — Java FQCN ambiguity tie-break (canonical-file lookup)
// ---------------------------------------------------------------------------

// TestJavaCanonicalLookup_WinsOverHierarchyInferenceEntity verifies that
// lookupModuleEntityJavaCanonical prefers the entity declared in the
// canonical file (e.g. "WSException.java") over a hierarchy-inference
// entity in a peer file ("EntityNotFoundException.java") when both have
// the same Name and live in the same module bucket.
func TestJavaCanonicalLookup_WinsOverHierarchyInferenceEntity(t *testing.T) {
	const module = "com.example.errors"
	records := []types.EntityRecord{
		// Canonical class entity (correct file)
		{
			ID:         "canonical00000001",
			Name:       "MyException",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/main/java/com/example/errors/MyException.java",
			Language:   "java",
		},
		// Hierarchy-inference entity (wrong file)
		{
			ID:         "inference0000001",
			Name:       "MyException",
			Kind:       "SCOPE.Component",
			SourceFile: "src/main/java/com/example/errors/OtherException.java",
			Language:   "java",
		},
		// File with FQCN IMPORTS edge
		{
			ID:         "fileentity000001",
			Name:       "src/main/java/com/example/service/Svc.java",
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			SourceFile: "src/main/java/com/example/service/Svc.java",
			Language:   "java",
			Relationships: []types.RelationshipRecord{{
				FromID: "src/main/java/com/example/service/Svc.java",
				ToID:   module + ".MyException",
				Kind:   importRelKind,
				Properties: map[string]string{
					"local_name":    "MyException",
					"source_module": module,
					"imported_name": "MyException",
					"language":      "java",
				},
			}},
		},
	}

	tbl := BuildImportTable(records)
	// Verify ambiguity is detected
	if !tbl.ambigModuleName[module]["MyException"] {
		t.Fatal("expected (module, MyException) to be ambiguous")
	}
	// Verify canonical lookup resolves to the canonical entity
	id, ok := tbl.lookupModuleEntityJavaCanonical(module, "MyException")
	if !ok {
		t.Fatal("lookupModuleEntityJavaCanonical returned false")
	}
	if id != "canonical00000001" {
		t.Errorf("expected canonical00000001, got %q", id)
	}
	// Verify IMPORTS edge is rewritten
	stats := ResolveImports(records, tbl)
	if stats.ImportsRewritten != 1 {
		t.Fatalf("expected 1 IMPORTS rewrite, got %d", stats.ImportsRewritten)
	}
	if got := records[2].Relationships[0].ToID; got != "canonical00000001" {
		t.Errorf("IMPORTS edge: expected canonical00000001, got %q", got)
	}
}

// TestJavaCanonicalLookup_EntityFileSetForIncomingCollision verifies that
// entityFile is set for an entity that arrives second in a collision (the
// v4 fix). When the canonical SCOPE.Component arrives after the inference
// entity, its entityFile must still be recorded so the canonical suffix
// match succeeds.
func TestJavaCanonicalLookup_EntityFileSetForIncomingCollision(t *testing.T) {
	const module = "com.example.errors"
	// Deliberate order: inference entity first so canonical arrives via collision.
	records := []types.EntityRecord{
		{
			ID:         "inference0000002",
			Name:       "Widget",
			Kind:       "SCOPE.Component",
			SourceFile: "src/main/java/com/example/errors/OtherWidget.java",
			Language:   "java",
		},
		// Canonical entity — arrives second → goes through collision branch
		{
			ID:         "canonical00000002",
			Name:       "Widget",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/main/java/com/example/errors/Widget.java",
			Language:   "java",
		},
		{
			ID:         "importer000000002",
			Name:       "src/main/java/com/example/svc/Consumer.java",
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			SourceFile: "src/main/java/com/example/svc/Consumer.java",
			Language:   "java",
			Relationships: []types.RelationshipRecord{{
				FromID: "src/main/java/com/example/svc/Consumer.java",
				ToID:   module + ".Widget",
				Kind:   importRelKind,
				Properties: map[string]string{
					"local_name":    "Widget",
					"source_module": module,
					"imported_name": "Widget",
					"language":      "java",
				},
			}},
		},
	}
	tbl := BuildImportTable(records)
	id, ok := tbl.lookupModuleEntityJavaCanonical(module, "Widget")
	if !ok {
		t.Fatal("expected canonical lookup to succeed when canonical arrives second")
	}
	if id != "canonical00000002" {
		t.Errorf("expected canonical00000002, got %q", id)
	}
	stats := ResolveImports(records, tbl)
	if stats.ImportsRewritten != 1 {
		t.Fatalf("expected 1 IMPORTS rewrite, got %d", stats.ImportsRewritten)
	}
}

// TestJavaCanonicalLookup_ScopePreferredOverFrameworkProjection verifies
// that when both a SCOPE.Component and a framework projection (e.g. Service)
// live in the canonical file, lookupModuleEntityJavaCanonical returns the
// SCOPE.Component entity. This is the Java analog of the PHP/Scala same-file
// projection dedup (issue #485 / #498 follow-ups).
func TestJavaCanonicalLookup_ScopePreferredOverFrameworkProjection(t *testing.T) {
	const module = "com.example.svc"
	records := []types.EntityRecord{
		// Framework projection (e.g. from YAML synth) — arrives first
		{
			ID:         "projection0000001",
			Name:       "OrderService",
			Kind:       "Service",
			SourceFile: "src/main/java/com/example/svc/OrderService.java",
			Language:   "java",
		},
		// Canonical SCOPE.Component — arrives second (same file, diff kind)
		{
			ID:         "canonical00000003",
			Name:       "OrderService",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/main/java/com/example/svc/OrderService.java",
			Language:   "java",
		},
		// Non-canonical Dependency entity in the same module from another file
		{
			ID:         "dependency000001",
			Name:       "OrderService",
			Kind:       "Dependency",
			SourceFile: "src/main/java/com/example/svc/InvoiceService.java",
			Language:   "java",
		},
		{
			ID:         "importer000000003",
			Name:       "src/main/java/com/example/ctrl/Ctrl.java",
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			SourceFile: "src/main/java/com/example/ctrl/Ctrl.java",
			Language:   "java",
			Relationships: []types.RelationshipRecord{{
				FromID: "src/main/java/com/example/ctrl/Ctrl.java",
				ToID:   module + ".OrderService",
				Kind:   importRelKind,
				Properties: map[string]string{
					"local_name":    "OrderService",
					"source_module": module,
					"imported_name": "OrderService",
					"language":      "java",
				},
			}},
		},
	}
	tbl := BuildImportTable(records)
	id, ok := tbl.lookupModuleEntityJavaCanonical(module, "OrderService")
	if !ok {
		t.Fatal("expected canonical lookup to succeed with SCOPE preferred over Service")
	}
	if id != "canonical00000003" {
		t.Errorf("expected SCOPE.Component canonical00000003, got %q", id)
	}
}

// TestJavaCanonicalLookup_LateArrivalAfterAmbigFlag verifies the v6 fix:
// when two non-canonical entities arrive first and set the ambig flag, a
// late-arriving canonical entity is still added to the candidate list and
// its entityFile is set, so lookupModuleEntityJavaCanonical can resolve it.
func TestJavaCanonicalLookup_LateArrivalAfterAmbigFlag(t *testing.T) {
	const module = "com.example.svc"
	records := []types.EntityRecord{
		// Two dependency entities arrive first → ambig flag set
		{
			ID:         "dependency000010",
			Name:       "PayService",
			Kind:       "Dependency",
			SourceFile: "src/main/java/com/example/svc/InvoiceService.java",
			Language:   "java",
		},
		{
			ID:         "dependency000011",
			Name:       "PayService",
			Kind:       "Dependency",
			SourceFile: "src/main/java/com/example/svc/OrderService.java",
			Language:   "java",
		},
		// Canonical entity arrives AFTER ambig flag was set
		{
			ID:         "canonical00000010",
			Name:       "PayService",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/main/java/com/example/svc/PayService.java",
			Language:   "java",
		},
		{
			ID:         "importer000000010",
			Name:       "src/main/java/com/example/ctrl/CheckoutCtrl.java",
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			SourceFile: "src/main/java/com/example/ctrl/CheckoutCtrl.java",
			Language:   "java",
			Relationships: []types.RelationshipRecord{{
				FromID: "src/main/java/com/example/ctrl/CheckoutCtrl.java",
				ToID:   module + ".PayService",
				Kind:   importRelKind,
				Properties: map[string]string{
					"local_name":    "PayService",
					"source_module": module,
					"imported_name": "PayService",
					"language":      "java",
				},
			}},
		},
	}
	tbl := BuildImportTable(records)
	if !tbl.ambigModuleName[module]["PayService"] {
		t.Fatal("expected ambig flag to be set for PayService")
	}
	id, ok := tbl.lookupModuleEntityJavaCanonical(module, "PayService")
	if !ok {
		t.Fatal("expected canonical lookup to succeed for late-arriving entity")
	}
	if id != "canonical00000010" {
		t.Errorf("expected canonical00000010, got %q", id)
	}
	stats := ResolveImports(records, tbl)
	if stats.ImportsRewritten != 1 {
		t.Fatalf("expected 1 IMPORTS rewrite via canonical fallback, got %d", stats.ImportsRewritten)
	}
}

// TestJavaCanonicalLookup_BareCallsViaResolveBareCallTarget verifies that
// when a caller imports a class (e.g. `import com.example.errors.MyError`)
// and then calls it bare (`throw new MyError()`), ResolveBareCallTarget
// resolves the ambiguous bare CALLS target via lookupModuleEntityJavaCanonical.
func TestJavaCanonicalLookup_BareCallsViaResolveBareCallTarget(t *testing.T) {
	const module = "com.example.errors"
	const callerFile = "src/main/java/com/example/service/Service.java"
	records := []types.EntityRecord{
		// Canonical entity
		{
			ID:         "canonical00000020",
			Name:       "MyError",
			Kind:       "SCOPE.Component",
			Subtype:    "class",
			SourceFile: "src/main/java/com/example/errors/MyError.java",
			Language:   "java",
		},
		// Inference entity (causes ambiguity)
		{
			ID:         "inference0000020",
			Name:       "MyError",
			Kind:       "SCOPE.Component",
			SourceFile: "src/main/java/com/example/errors/OtherError.java",
			Language:   "java",
		},
		// File entity carrying IMPORTS and a bare CALLS edge
		{
			ID:         "fileentity000020",
			Name:       callerFile,
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			SourceFile: callerFile,
			Language:   "java",
			Relationships: []types.RelationshipRecord{
				// IMPORTS binding: local_name "MyError" → module+imported_name
				{
					FromID: callerFile,
					ToID:   module + ".MyError",
					Kind:   importRelKind,
					Properties: map[string]string{
						"local_name":    "MyError",
						"source_module": module,
						"imported_name": "MyError",
						"language":      "java",
					},
				},
			},
		},
		// Method entity with a bare CALLS to "MyError"
		{
			ID:         "callermethod00020",
			Name:       "Service.doWork",
			Kind:       "SCOPE.Operation",
			Subtype:    "method",
			SourceFile: callerFile,
			Language:   "java",
			Relationships: []types.RelationshipRecord{{
				ToID: "MyError",
				Kind: "CALLS",
			}},
		},
	}
	tbl := BuildImportTable(records)
	// The IMPORTS lookup should fail (ambiguous) but ResolveBareCallTarget
	// must fall through to the Java canonical tie-break.
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 1 {
		t.Fatalf("expected 1 bare CALLS rewrite via canonical fallback, got %d (considered=%d)",
			stats.CallsRewritten, stats.CallsConsidered)
	}
	if got := records[3].Relationships[0].ToID; got != "canonical00000020" {
		t.Errorf("expected CALLS rewritten to canonical00000020, got %q", got)
	}
}

// TestJavaCanonicalLookup_NoFalseBindWhenMultipleCanonicalFiles guards
// against a corner case: if two files both end with "/<name>.java" (can
// happen with inner classes or unusual package structures), the canonical
// lookup must return false rather than pick one arbitrarily.
func TestJavaCanonicalLookup_NoFalseBindWhenMultipleCanonicalFiles(t *testing.T) {
	const module = "com.example"
	records := []types.EntityRecord{
		{
			ID:         "canonical00000030",
			Name:       "Util",
			Kind:       "SCOPE.Component",
			SourceFile: "src/main/java/com/example/Util.java",
			Language:   "java",
		},
		// Another entity named "Util" in a sub-package that also maps back to
		// this module (unusual but possible in flat module derivation).
		{
			ID:         "canonical00000031",
			Name:       "Util",
			Kind:       "SCOPE.Component",
			SourceFile: "src/main/java/com/example/sub/Util.java", // different file but both end in /Util.java
			Language:   "java",
		},
		{
			ID:         "importer000000030",
			Name:       "src/main/java/com/example/App.java",
			Kind:       "SCOPE.Component",
			Subtype:    "file",
			SourceFile: "src/main/java/com/example/App.java",
			Language:   "java",
			Relationships: []types.RelationshipRecord{{
				FromID: "src/main/java/com/example/App.java",
				ToID:   "com.example.Util",
				Kind:   importRelKind,
				Properties: map[string]string{
					"local_name":    "Util",
					"source_module": "com.example",
					"imported_name": "Util",
					"language":      "java",
				},
			}},
		},
	}
	tbl := BuildImportTable(records)
	// Should NOT resolve — two entities both in "Util.java" files
	_, ok := tbl.lookupModuleEntityJavaCanonical("com.example", "Util")
	if ok {
		t.Error("expected no resolution when two entities both live in /Util.java files")
	}
}

func TestResolveImports_ReferencesHexToIDUntouched(t *testing.T) {
	records := []types.EntityRecord{
		importerWithToID("api/views.py", "django.db.models", "ext:django:Model", map[string]string{
			"local_name":    "Model",
			"source_module": "django.db.models",
			"imported_name": "Model",
		}),
		{
			ID:         "0123456789abcdef",
			Name:       "entry",
			Kind:       "SCOPE.Operation",
			Subtype:    "function",
			SourceFile: "api/views.py",
			Language:   "python",
			Relationships: []types.RelationshipRecord{{
				ToID: "ffffffffffffffff",
				Kind: "REFERENCES",
			}},
		},
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.ReferencesConsidered != 0 {
		t.Fatalf("expected 0 considered (hex skipped at outer guard), got %d", stats.ReferencesConsidered)
	}
	if got := records[1].Relationships[0].ToID; got != "ffffffffffffffff" {
		t.Fatalf("expected hex REFERENCES ToID untouched, got %q", got)
	}
}

// callerRecordWithProps builds a caller EntityRecord whose single CALLS
// edge carries the given ToID + Properties. Used by the cross-module
// CALL-target tests (issue #1694) to inject the `import_alias` /
// `call_leaf` hints the Python extractor stamps for
// `<alias>.<leaf>(...)` shapes.
func callerRecordWithProps(name, file, target string, props map[string]string) types.EntityRecord {
	return types.EntityRecord{
		ID:         "0123456789abcdef",
		Name:       name,
		Kind:       "SCOPE.Operation",
		Subtype:    "function",
		SourceFile: file,
		Language:   "python",
		Relationships: []types.RelationshipRecord{{
			ToID:       target,
			Kind:       "CALLS",
			Properties: props,
		}},
	}
}

// TestResolveImports_CrossModuleCall_FromImportSubmodule covers the
// canonical PlaceOrderSaga case: `from . import steps;
// steps.create_order()`. The extractor has resolved the relative import
// to its absolute dotted form (`services.order_saga.app`) and stamped
// import_alias="steps", call_leaf="create_order". The resolver must
// probe (source_module + "." + imported_name, call_leaf) → bind to the
// real `create_order` entity in services/order_saga/app/steps.py.
func TestResolveImports_CrossModuleCall_FromImportSubmodule(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("services/order_saga/app/orchestrator.py", "services.order_saga.app.steps", map[string]string{
			"local_name":    "steps",
			"source_module": "services.order_saga.app",
			"imported_name": "steps",
		}),
		targetRecord("create_order", "services/order_saga/app/steps.py", "abcdef0123456789"),
		callerRecordWithProps("run", "services/order_saga/app/orchestrator.py", "create_order", map[string]string{
			"import_alias": "steps",
			"call_leaf":    "create_order",
		}),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d (considered=%d)", stats.CallsRewritten, stats.CallsConsidered)
	}
	if got := records[2].Relationships[0].ToID; got != "abcdef0123456789" {
		t.Fatalf("expected ToID rewritten to abcdef0123456789, got %q", got)
	}
}

// TestResolveImports_CrossModuleCall_PlainImport covers `import billing;
// billing.charge_card()`. import_alias="billing"; source_module ==
// imported_name; the resolver probes (source_module, call_leaf) directly.
func TestResolveImports_CrossModuleCall_PlainImport(t *testing.T) {
	records := []types.EntityRecord{
		importerRecord("services/orders/checkout.py", "billing", map[string]string{
			"local_name":    "billing",
			"source_module": "billing",
			"imported_name": "billing",
		}),
		targetRecord("charge_card", "billing/__init__.py", "1111222233334444"),
		callerRecordWithProps("checkout", "services/orders/checkout.py", "charge_card", map[string]string{
			"import_alias": "billing",
			"call_leaf":    "charge_card",
		}),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.CallsRewritten)
	}
	if got := records[2].Relationships[0].ToID; got != "1111222233334444" {
		t.Fatalf("expected ToID 1111222233334444, got %q", got)
	}
}

// TestResolveImports_CrossModuleCall_UnknownAliasUnchanged confirms that
// when the import_alias hint references a name that is NOT in the file's
// import bucket (e.g. the alias was shadowed by a local variable), the
// resolver leaves the bare leaf ToID alone — it does not fall through to
// the generic bare-name resolution that would otherwise bind the leaf
// to a same-named symbol via wildcard / plain-import branches.
func TestResolveImports_CrossModuleCall_UnknownAliasUnchanged(t *testing.T) {
	// A target named `charge_card` exists in module `billing`, but the
	// caller's file imports nothing — the cross-module probe must miss
	// and the bare-name resolver must also miss (no binding).
	records := []types.EntityRecord{
		targetRecord("charge_card", "billing/__init__.py", "9999888877776666"),
		callerRecordWithProps("checkout", "services/orders/checkout.py", "charge_card", map[string]string{
			"import_alias": "billing", // alias not in bucket
			"call_leaf":    "charge_card",
		}),
	}
	tbl := BuildImportTable(records)
	stats := ResolveImports(records, tbl)
	if stats.CallsRewritten != 0 {
		t.Fatalf("expected 0 rewrites (no import binding), got %d", stats.CallsRewritten)
	}
	if got := records[1].Relationships[0].ToID; got != "charge_card" {
		t.Fatalf("expected bare ToID untouched, got %q", got)
	}
}
