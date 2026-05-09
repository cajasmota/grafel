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
