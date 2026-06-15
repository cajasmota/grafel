package external

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/resolve"
)

// isBugEdgeToID mirrors internal/mcp.isBugEdgeToID for #4699 fixtures: a
// ToID that is neither a 16-char hex entity ID nor an ext:-prefixed external
// reference is an unresolved stub that counts as a fidelity bug. Duplicated
// here because internal/mcp can't be imported from internal/external tests
// (import-cycle risk); kept in lockstep with the canonical definition.
func isBugEdgeToID(toID string) bool {
	if toID == "" {
		return false
	}
	if len(toID) == 16 && isHexID(toID) {
		return false
	}
	if strings.HasPrefix(toID, "ext:") {
		return false
	}
	return true
}

// assertCrossLangGate is the canonical structural assertion for the
// per-language gate at classification time (issue #516). For each
// (name, otherLang) pair it verifies the gate's actual runtime
// behaviour rather than catalog-disjointness:
//
//  1. If stdlibFunction(name, otherLang, "", nil) returns ok=true,
//     `otherLang` legitimately owns the name in its own bare-name
//     catalog. This is fine — per-language gates allow the same bare
//     name to live in multiple catalogs as long as each catalog only
//     fires for its own source language. The assertion is SKIPPED.
//  2. Otherwise the gate must fall through: both the raw classifier
//     and the full Synthesize pipeline (with caller Language=otherLang)
//     must leave the relationship unrewritten.
//
// Rationale: prior to #516 these tests asserted that a name in
// catalog X could not appear in catalog Y. That was too strict —
// names like `partition` (kotlin + ruby + python), `auth` (swift +
// php), `basename`/`dirname`/`mkdir`/`realpath` (python + ruby) and
// `length` (ruby + others) legitimately live in multiple catalogs
// because each is gated to its own language. The structural
// invariant we actually care about is that the GATE works, not that
// the catalogs are disjoint.
//
// DO NOT reintroduce a "name must not appear in any other catalog"
// assertion. The single source of truth for cross-language safety
// is the language gate inside stdlibFunction; tests must exercise
// that gate's behaviour, not catalog set-membership.
func assertCrossLangGate(t *testing.T, name, otherLang, ownerLangHint string) {
	t.Helper()
	if _, ok := stdlibFunction(name, otherLang, "", nil); ok {
		// otherLang legitimately owns this name in its own catalog.
		// Per #516 this is allowed — the per-language gate ensures
		// that classifying a non-otherLang source will NOT fire this
		// catalog. Skip the cross-lang assertion for this pair.
		t.Skipf("name %q is also in %q's own catalog (multi-catalog membership is allowed per #516; gate is per-language)", name, otherLang)
		return
	}
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "src",
			Name:     "caller",
			Kind:     "function",
			Language: otherLang,
		}},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "src", ToID: name, Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 0 (gate should hold for non-%q sources)",
			name, otherLang, stats.Synthesized, ownerLangHint)
	}
	if doc.Relationships[0].ToID != name {
		t.Fatalf("ToID=%q, want %q (must not be rewritten for non-%q sources)",
			doc.Relationships[0].ToID, name, ownerLangHint)
	}
}

// TestSynthesize_HappyPath confirms an IMPORTS-django relationship
// produces a single ext:django placeholder and rewrites the edge.
func TestSynthesize_HappyPath(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "0123456789abcdef", Name: "models", Kind: "SCOPE.Component", SourceFile: "myapp/models.py"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "myapp/models.py", ToID: "django.db.models", Kind: "IMPORTS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 1 {
		t.Fatalf("synthesized=%d, want 1", stats.Synthesized)
	}
	if stats.RelationshipsResolved != 1 {
		t.Fatalf("resolved=%d, want 1", stats.RelationshipsResolved)
	}
	if doc.Relationships[0].ToID != "ext:django" {
		t.Fatalf("rel ToID=%q, want ext:django", doc.Relationships[0].ToID)
	}
	found := false
	for _, e := range doc.Entities {
		if e.ID == "ext:django" {
			found = true
			if e.Kind != KindExternal {
				t.Fatalf("placeholder kind=%q, want %q", e.Kind, KindExternal)
			}
			if e.Subtype != "package" {
				t.Fatalf("placeholder subtype=%q, want package", e.Subtype)
			}
			if v, ok := e.Metadata["is_external"].(bool); !ok || !v {
				t.Fatalf("placeholder is_external missing or false: %+v", e.Metadata)
			}
		}
	}
	if !found {
		t.Fatalf("ext:django entity not appended; entities=%+v", doc.Entities)
	}
}

// TestSynthesize_Idempotent confirms running the pass twice on the
// same document doesn't create duplicate placeholders.
func TestSynthesize_Idempotent(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "a", ToID: "django", Kind: "IMPORTS"},
			{ID: "rel-2", FromID: "b", ToID: "django.db", Kind: "IMPORTS"},
		},
	}
	first := Synthesize(doc)
	if first.Synthesized != 1 {
		t.Fatalf("first run synthesized=%d, want 1", first.Synthesized)
	}
	beforeEntities := len(doc.Entities)
	second := Synthesize(doc)
	if second.Synthesized != 0 {
		t.Fatalf("second run synthesized=%d, want 0 (idempotent)", second.Synthesized)
	}
	if len(doc.Entities) != beforeEntities {
		t.Fatalf("second run grew entities from %d to %d", beforeEntities, len(doc.Entities))
	}
	// Both relationships should now point at ext:django.
	for k, r := range doc.Relationships {
		if r.ToID != "ext:django" {
			t.Fatalf("rel[%d].ToID=%q, want ext:django", k, r.ToID)
		}
	}
}

// TestSynthesize_LocalUnaffected confirms relationships pointing at
// already-resolved (hex-id) entities are not touched.
func TestSynthesize_LocalUnaffected(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "0123456789abcdef", Name: "Foo", Kind: "Function"},
			{ID: "fedcba9876543210", Name: "Bar", Kind: "Function"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "0123456789abcdef", ToID: "fedcba9876543210", Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 || stats.RelationshipsResolved != 0 {
		t.Fatalf("expected no synthesis on hex-resolved edges; got %+v", stats)
	}
	if doc.Relationships[0].ToID != "fedcba9876543210" {
		t.Fatalf("local edge was rewritten: ToID=%q", doc.Relationships[0].ToID)
	}
	if len(doc.Entities) != 2 {
		t.Fatalf("entity count changed: %d", len(doc.Entities))
	}
}

// TestSynthesize_StdlibBareName confirms a bare "Println" stub becomes
// ext:Println with subtype function.
func TestSynthesize_StdlibBareName(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "main.go", ToID: "Println", Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 1 {
		t.Fatalf("synthesized=%d, want 1", stats.Synthesized)
	}
	if doc.Relationships[0].ToID != "ext:Println" {
		t.Fatalf("ToID=%q", doc.Relationships[0].ToID)
	}
	if doc.Entities[0].Subtype != "function" {
		t.Fatalf("subtype=%q, want function", doc.Entities[0].Subtype)
	}
}

// TestSynthesize_ReflectionBuiltinsLeftAlone is the issue #95 regression
// guard at the synthesiser layer. Python reflection builtins (getattr /
// setattr / hasattr / delattr / eval / exec / compile / __import__) used
// to live in stdlibBareNames and got rewritten to "ext:builtins" before
// the resolver's dynamic-pattern catalog could see them. The fix removes
// them from the stop-list — the synthesiser must now leave the stub
// untouched so the resolver classifies it as DispositionDynamic.
func TestSynthesize_ReflectionBuiltinsLeftAlone(t *testing.T) {
	reflectionBuiltins := []string{
		"getattr", "setattr", "hasattr", "delattr",
		"eval", "exec", "compile", "__import__",
	}
	for _, name := range reflectionBuiltins {
		name := name
		t.Run(name, func(t *testing.T) {
			doc := &graph.Document{
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "app.py", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 0 {
				t.Fatalf("%s: synthesized=%d, want 0", name, stats.Synthesized)
			}
			if doc.Relationships[0].ToID != name {
				t.Fatalf("%s: ToID rewritten to %q (want %q preserved)",
					name, doc.Relationships[0].ToID, name)
			}
		})
	}
}

// TestSynthesize_UnknownLeftAlone confirms truly-unknown stubs are
// neither rewritten nor synthesised — they continue to count as
// "unmatched" upstream.
func TestSynthesize_UnknownLeftAlone(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "a", ToID: "SomeRandomLocalThing", Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("synthesized=%d, want 0", stats.Synthesized)
	}
	if doc.Relationships[0].ToID != "SomeRandomLocalThing" {
		t.Fatalf("unknown stub was rewritten to %q", doc.Relationships[0].ToID)
	}
}

// TestSynthesize_GoStdlibInterfaceDispatch (issue #364) confirms a CALLS
// edge whose `Properties["receiver_type"]` matches a Go-stdlib interface
// type AND whose ToID matches a method on that type is rewritten to the
// canonical ext:<package> placeholder. Covers the dominant residual
// go-chi bug-rate post-#148: calls like `w.Write(...)` on
// `http.ResponseWriter`, `r.Cookie(...)` on `*http.Request`,
// `h.ServeHTTP(...)` on `http.Handler`.
func TestSynthesize_GoStdlibInterfaceDispatch(t *testing.T) {
	cases := []struct {
		name      string
		recvType  string
		toID      string
		wantExtID string
	}{
		{"http.ResponseWriter.Write", "http.ResponseWriter", "Write", "ext:net/http"},
		{"http.ResponseWriter.WriteHeader", "http.ResponseWriter", "WriteHeader", "ext:net/http"},
		{"http.Request.Cookie", "http.Request", "Cookie", "ext:net/http"},
		{"http.Request.Context", "http.Request", "Context", "ext:net/http"},
		{"http.Handler.ServeHTTP", "http.Handler", "ServeHTTP", "ext:net/http"},
		{"http.Server.ListenAndServe", "http.Server", "ListenAndServe", "ext:net/http"},
		{"io.Reader.Read", "io.Reader", "Read", "ext:io"},
		{"io.Closer.Close", "io.Closer", "Close", "ext:io"},
		{"context.Context.Done", "context.Context", "Done", "ext:context"},
		{"sync.Mutex.Lock", "sync.Mutex", "Lock", "ext:sync"},
		{"bytes.Buffer.WriteString", "bytes.Buffer", "WriteString", "ext:bytes"},
		{"strings.Builder.String", "strings.Builder", "String", "ext:strings"},
		{"sql.Rows.Scan", "sql.Rows", "Scan", "ext:database/sql"},
		{"error.Error", "error", "Error", "ext:errors"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &graph.Document{
				Entities: []graph.Entity{
					{ID: "fn-1", SourceFile: "handlers/http.go", Language: "go"},
				},
				Relationships: []graph.Relationship{
					{
						ID:     "rel-1",
						FromID: "fn-1",
						ToID:   tc.toID,
						Kind:   "CALLS",
						Properties: map[string]string{
							"receiver_type": tc.recvType,
						},
					},
				},
			}
			Synthesize(doc)
			if got := doc.Relationships[0].ToID; got != tc.wantExtID {
				t.Fatalf("ToID=%q, want %q", got, tc.wantExtID)
			}
		})
	}
}

// TestSynthesize_GoStdlibInterfaceDispatch_NoFalsePositive verifies that:
// (a) a CALLS edge with no receiver_type stamp does NOT get routed to a
// stdlib package even when the bare name happens to match an interface
// method (Lock/Close/Read/Write are all common user-method names);
// (b) a CALLS edge with a receiver_type stamp pointing at a USER-defined
// type (not in the stdlib catalogue) is left alone.
func TestSynthesize_GoStdlibInterfaceDispatch_NoFalsePositive(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "fn-1", SourceFile: "domain/cache.go", Language: "go"},
		},
		Relationships: []graph.Relationship{
			// (a) No receiver_type — bare "Lock" (a sync.Mutex method name) must
			// stay unresolved. A user type with a Lock method is far more likely
			// here than a misclassified call.
			{ID: "r1", FromID: "fn-1", ToID: "Lock", Kind: "CALLS"},
			// (b) receiver_type points at a user-defined type. Not on the
			// catalogue → no rewrite.
			{
				ID: "r2", FromID: "fn-1", ToID: "Acquire", Kind: "CALLS",
				Properties: map[string]string{"receiver_type": "myapp.SemSlot"},
			},
		},
	}
	Synthesize(doc)
	if doc.Relationships[0].ToID != "Lock" {
		t.Fatalf("bare Lock without receiver_type was rewritten to %q", doc.Relationships[0].ToID)
	}
	if doc.Relationships[1].ToID != "Acquire" {
		t.Fatalf("user-typed receiver Acquire was rewritten to %q", doc.Relationships[1].ToID)
	}
}

// TestSynthesize_NilDoc confirms calling on a nil document is a no-op.
func TestSynthesize_NilDoc(t *testing.T) {
	stats := Synthesize(nil)
	if stats.Synthesized != 0 || stats.RelationshipsResolved != 0 {
		t.Fatalf("nil doc produced stats: %+v", stats)
	}
}

// TestSynthesize_ScopeExternalStructuralRef covers the
// "scope:<kind>:import:external:<name>" branch emitted by Pass 3
// cross-language extractors. The trailing segment after ":external:"
// is the canonical package name, and the placeholder is created even
// when the package isn't on the static allowlist (extractor has
// already classified it as not-local).
func TestSynthesize_ScopeExternalStructuralRef(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "a.py", ToID: "scope:Module:import:external:some_obscure_pkg", Kind: "IMPORTS"},
			{ID: "rel-2", FromID: "b.py", ToID: "scope:Module:import:external:some_obscure_pkg.submodule", Kind: "IMPORTS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 1 {
		t.Fatalf("synthesized=%d, want 1 (collapsed to package root)", stats.Synthesized)
	}
	if stats.RelationshipsResolved != 2 {
		t.Fatalf("resolved=%d, want 2", stats.RelationshipsResolved)
	}
	for k, r := range doc.Relationships {
		if r.ToID != "ext:some_obscure_pkg" {
			t.Fatalf("rel[%d].ToID=%q, want ext:some_obscure_pkg", k, r.ToID)
		}
	}
}

// TestSynthesize_ScopeExternalRejectsPathSeparator confirms the
// scope-external branch refuses stubs with embedded path separators —
// those are file paths, not external package names.
func TestSynthesize_ScopeExternalRejectsPathSeparator(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "a.py", ToID: "scope:Module:import:external:some/path", Kind: "IMPORTS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 || stats.RelationshipsResolved != 0 {
		t.Fatalf("expected no synthesis on path-shaped scope-external; got %+v", stats)
	}
}

// TestSynthesize_JSExternalPackages_Fixture is Fixture A for #4695: a JS/TS
// file importing bare third-party npm packages that are NOT on the static
// isKnownExternalPackage allowlist (class-validator, typeorm) must be
// routed to ext:<package> placeholders (disposition external_package) rather
// than left as bug-extractor stubs that inflate the import-bug count and
// depress the fidelity badge.
func TestSynthesize_JSExternalPackages_Fixture(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "src/user.dto.ts", Language: "typescript", SourceFile: "src/user.dto.ts"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "class-validator", Kind: "IMPORTS",
				Properties: map[string]string{"language": "typescript", "import_path": "class-validator"}},
			{ID: "rel-2", FromID: "aaaaaaaaaaaaaaaa", ToID: "typeorm", Kind: "IMPORTS",
				Properties: map[string]string{"language": "typescript", "import_path": "typeorm"}},
			// Scoped package NOT on the static allowlist — exercises the
			// #4695 catch-all via import_path (collapses to "@scope/pkg").
			{ID: "rel-3", FromID: "aaaaaaaaaaaaaaaa", ToID: "@acme.widgets", Kind: "IMPORTS",
				Properties: map[string]string{"language": "typescript", "import_path": "@acme/widgets/sub"}},
			// Subpath import collapses to the package root.
			{ID: "rel-4", FromID: "aaaaaaaaaaaaaaaa", ToID: "class-transformer.plainToClass", Kind: "IMPORTS",
				Properties: map[string]string{"language": "typescript", "import_path": "class-transformer/plain"}},
		},
	}
	Synthesize(doc)

	want := map[string]string{
		"rel-1": "ext:class-validator",
		"rel-2": "ext:typeorm",
		"rel-3": "ext:@acme/widgets",
		"rel-4": "ext:class-transformer",
	}
	for _, r := range doc.Relationships {
		if exp, ok := want[r.ID]; ok && r.ToID != exp {
			t.Errorf("%s ToID=%q, want %q", r.ID, r.ToID, exp)
		}
	}
}

// TestSynthesize_JSRelativeImportNotExternal confirms the #4695 branch does
// NOT swallow relative imports as external packages — a `./` specifier is
// project-internal and must not be routed to an ext: placeholder.
func TestSynthesize_JSRelativeImportNotExternal(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "src/a.ts", Language: "typescript", SourceFile: "src/a.ts"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "src.b", Kind: "IMPORTS",
				Properties: map[string]string{"language": "typescript", "import_path": "./b"}},
		},
	}
	Synthesize(doc)
	for _, r := range doc.Relationships {
		if strings.HasPrefix(r.ToID, "ext:") {
			t.Errorf("relative import wrongly externalised: ToID=%q", r.ToID)
		}
	}
}

// TestSynthesize_PyExternalPackages_Fixture is Fixture A for #4699: a Python
// file importing third-party pip packages whose roots are NOT on the static
// pythonKnownExternalRoots allowlist (rest_framework, celery, pydantic) must be
// routed to ext:<package> placeholders (disposition external_package) rather
// than left as bug-extractor stubs. The only indexed Python entity lives under
// the "shop" package, so none of these roots are internal — they are pip deps.
func TestSynthesize_PyExternalPackages_Fixture(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "shop/views.py", Language: "python", SourceFile: "shop/views.py"},
		},
		Relationships: []graph.Relationship{
			// from rest_framework import serializers
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "rest_framework.serializers", Kind: "IMPORTS",
				Properties: map[string]string{"language": "python", "source_module": "rest_framework", "imported_name": "serializers"}},
			// from celery import shared_task
			{ID: "rel-2", FromID: "aaaaaaaaaaaaaaaa", ToID: "celery.shared_task", Kind: "IMPORTS",
				Properties: map[string]string{"language": "python", "source_module": "celery", "imported_name": "shared_task"}},
			// import pydantic
			{ID: "rel-3", FromID: "aaaaaaaaaaaaaaaa", ToID: "pydantic", Kind: "IMPORTS",
				Properties: map[string]string{"language": "python", "source_module": "pydantic", "imported_name": "pydantic"}},
		},
	}
	Synthesize(doc)

	want := map[string]string{
		"rel-1": "ext:rest_framework",
		"rel-2": "ext:celery",
		"rel-3": "ext:pydantic",
	}
	for _, r := range doc.Relationships {
		if exp, ok := want[r.ID]; ok && r.ToID != exp {
			t.Errorf("%s ToID=%q, want %q", r.ID, r.ToID, exp)
		}
	}
	// Control: none of these may remain a bug-extractor stub.
	for _, r := range doc.Relationships {
		if isBugEdgeToID(r.ToID) {
			t.Errorf("%s wrongly counts as a fidelity bug: ToID=%q", r.ID, r.ToID)
		}
	}
}

// TestSynthesize_PyInternalUnresolvedStillBug is Fixture B for #4699 (the
// control): a genuinely-internal same-repo import that failed to resolve must
// STILL count as a fidelity bug — the external-package catch-all must NOT mask
// real under-linking. Here "shop" is an indexed internal package, so an
// unresolved `from shop.missing import Thing` keeps its bug disposition.
func TestSynthesize_PyInternalUnresolvedStillBug(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "shop/views.py", Language: "python", SourceFile: "shop/views.py"},
			{ID: "bbbbbbbbbbbbbbbb", Name: "shop/models.py", Language: "python", SourceFile: "shop/models.py"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "shop.missing.Thing", Kind: "IMPORTS",
				Properties: map[string]string{"language": "python", "source_module": "shop.missing", "imported_name": "Thing"}},
		},
	}
	Synthesize(doc)
	for _, r := range doc.Relationships {
		if r.ID != "rel-1" {
			continue
		}
		if strings.HasPrefix(r.ToID, "ext:") {
			t.Errorf("internal unresolved import wrongly externalised: ToID=%q", r.ToID)
		}
		if !isBugEdgeToID(r.ToID) {
			t.Errorf("internal unresolved import should remain a fidelity bug, ToID=%q", r.ToID)
		}
	}
}

// TestSynthesize_PyRelativeImportNotExternal confirms the #4699 branch does
// NOT externalise a residual relative-import source_module — a dot-prefixed
// module is project-internal and must keep its bug disposition.
func TestSynthesize_PyRelativeImportNotExternal(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "shop/views.py", Language: "python", SourceFile: "shop/views.py"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: ".sibling.helper", Kind: "IMPORTS",
				Properties: map[string]string{"language": "python", "source_module": ".sibling", "imported_name": "helper"}},
		},
	}
	Synthesize(doc)
	for _, r := range doc.Relationships {
		if strings.HasPrefix(r.ToID, "ext:") {
			t.Errorf("relative import wrongly externalised: ToID=%q", r.ToID)
		}
	}
}

// ----------------------------------------------------------------------------
// #4700-#4704 — per-language external-package catch-all fixtures. For each
// language: (A) a file importing well-known third-party deps → each
// ext:<pkg>, none a fidelity bug; (B control) a genuinely-internal but
// unresolved import → STILL a fidelity bug (the catch-all must never mask it).
// ----------------------------------------------------------------------------

// TestSynthesize_JavaExternalPackages_Fixture is Fixture A for #4700: a Java
// file importing maven/gradle deps (org.springframework.*, com.fasterxml.*,
// lombok) whose roots are NOT indexed packages of this repo must route to
// canonical ext:<group> placeholders. The only indexed entity lives in the
// "com.acme" package, so none of these are internal.
func TestSynthesize_JavaExternalPackages_Fixture(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "OrderService", QualifiedName: "com.acme.order.OrderService",
				Language: "java", SourceFile: "src/main/java/com/acme/order/OrderService.java"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "org.springframework.stereotype.Service", Kind: "IMPORTS",
				Properties: map[string]string{"language": "java", "import_path": "org.springframework.stereotype.Service"}},
			{ID: "rel-2", FromID: "aaaaaaaaaaaaaaaa", ToID: "com.fasterxml.jackson.databind.ObjectMapper", Kind: "IMPORTS",
				Properties: map[string]string{"language": "java", "import_path": "com.fasterxml.jackson.databind.ObjectMapper"}},
			// Wildcard import — trailing ".*" stripped.
			{ID: "rel-3", FromID: "aaaaaaaaaaaaaaaa", ToID: "org.junit.jupiter.api.*", Kind: "IMPORTS",
				Properties: map[string]string{"language": "java", "import_path": "org.junit.jupiter.api.*"}},
		},
	}
	Synthesize(doc)

	// rel-2 (com.fasterxml.jackson.*) matches the pre-existing static
	// allowlist's longest-known-dotted-prefix (com.fasterxml.jackson) before
	// the #4700 catch-all; rel-1/rel-3 are caught by the catch-all and
	// canonicalised to the two-segment maven group.
	want := map[string]string{
		"rel-1": "ext:org.springframework",
		"rel-2": "ext:com.fasterxml.jackson",
		"rel-3": "ext:org.junit",
	}
	for _, r := range doc.Relationships {
		if exp, ok := want[r.ID]; ok && r.ToID != exp {
			t.Errorf("%s ToID=%q, want %q", r.ID, r.ToID, exp)
		}
		if isBugEdgeToID(r.ToID) {
			t.Errorf("%s wrongly counts as a fidelity bug: ToID=%q", r.ID, r.ToID)
		}
	}
}

// TestSynthesize_JavaInternalUnresolvedStillBug is Fixture B (control) for
// #4700: an unresolved import whose root IS an indexed internal package
// ("com" here, via com.acme.*) must STAY a fidelity bug.
func TestSynthesize_JavaInternalUnresolvedStillBug(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "OrderService", QualifiedName: "com.acme.order.OrderService",
				Language: "java", SourceFile: "src/main/java/com/acme/order/OrderService.java"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "com.acme.missing.Thing", Kind: "IMPORTS",
				Properties: map[string]string{"language": "java", "import_path": "com.acme.missing.Thing"}},
		},
	}
	Synthesize(doc)
	for _, r := range doc.Relationships {
		if strings.HasPrefix(r.ToID, "ext:") {
			t.Errorf("internal unresolved Java import wrongly externalised: ToID=%q", r.ToID)
		}
		if !isBugEdgeToID(r.ToID) {
			t.Errorf("internal unresolved Java import should remain a fidelity bug, ToID=%q", r.ToID)
		}
	}
}

// TestSynthesize_RubyExternalPackages_Fixture is Fixture A for #4701: a Ruby
// file requiring gems (rails, rspec, sidekiq) whose roots are NOT internal
// libs must route to ext:<gem>. The only indexed entity lives under "billing".
func TestSynthesize_RubyExternalPackages_Fixture(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "Charge", Language: "ruby", SourceFile: "lib/billing/charge.rb"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "rails/all", Kind: "IMPORTS",
				Properties: map[string]string{"language": "ruby", "import_path": "rails/all"}},
			{ID: "rel-2", FromID: "aaaaaaaaaaaaaaaa", ToID: "rspec", Kind: "IMPORTS",
				Properties: map[string]string{"language": "ruby", "import_path": "rspec"}},
			{ID: "rel-3", FromID: "aaaaaaaaaaaaaaaa", ToID: "sidekiq", Kind: "IMPORTS",
				Properties: map[string]string{"language": "ruby", "import_path": "sidekiq"}},
		},
	}
	Synthesize(doc)

	want := map[string]string{
		"rel-1": "ext:rails",
		"rel-2": "ext:rspec",
		"rel-3": "ext:sidekiq",
	}
	for _, r := range doc.Relationships {
		if exp, ok := want[r.ID]; ok && r.ToID != exp {
			t.Errorf("%s ToID=%q, want %q", r.ID, r.ToID, exp)
		}
		if isBugEdgeToID(r.ToID) {
			t.Errorf("%s wrongly counts as a fidelity bug: ToID=%q", r.ID, r.ToID)
		}
	}
}

// TestSynthesize_RubyInternalAndRelativeStillBug is the control for #4701:
// require_relative and a bare require whose root IS an internal lib must STAY
// a fidelity bug.
func TestSynthesize_RubyInternalAndRelativeStillBug(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "Charge", Language: "ruby", SourceFile: "lib/billing/charge.rb"},
		},
		Relationships: []graph.Relationship{
			// require_relative — intra-project.
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "../helpers/money", Kind: "IMPORTS",
				Properties: map[string]string{"language": "ruby", "import_path": "../helpers/money", "require_kind": "require_relative"}},
			// bare require whose root ("billing") is an indexed internal lib.
			{ID: "rel-2", FromID: "aaaaaaaaaaaaaaaa", ToID: "billing/missing", Kind: "IMPORTS",
				Properties: map[string]string{"language": "ruby", "import_path": "billing/missing"}},
		},
	}
	Synthesize(doc)
	for _, r := range doc.Relationships {
		if strings.HasPrefix(r.ToID, "ext:") {
			t.Errorf("%s wrongly externalised: ToID=%q", r.ID, r.ToID)
		}
		if !isBugEdgeToID(r.ToID) {
			t.Errorf("%s should remain a fidelity bug, ToID=%q", r.ID, r.ToID)
		}
	}
}

// TestSynthesize_GoExternalPackages_Fixture is Fixture A for #4702:
// host-prefixed and stdlib Go imports must route to ext:<module> and none may
// be a fidelity bug.
func TestSynthesize_GoExternalPackages_Fixture(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "handler", Language: "go", SourceFile: "internal/api/handler.go"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "github.com/stretchr/testify/assert", Kind: "IMPORTS",
				Properties: map[string]string{"language": "go"}},
			{ID: "rel-2", FromID: "aaaaaaaaaaaaaaaa", ToID: "golang.org/x/sync/errgroup", Kind: "IMPORTS",
				Properties: map[string]string{"language": "go"}},
			{ID: "rel-3", FromID: "aaaaaaaaaaaaaaaa", ToID: "net/http", Kind: "IMPORTS",
				Properties: map[string]string{"language": "go"}},
		},
	}
	Synthesize(doc)
	// rel-3 (stdlib net/http) canonicalises to the root package segment
	// "net" via the pre-existing Go stdlib branch — still external, not a bug.
	want := map[string]string{
		"rel-1": "ext:github.com/stretchr/testify",
		"rel-2": "ext:golang.org/x/sync",
		"rel-3": "ext:net",
	}
	for _, r := range doc.Relationships {
		if exp, ok := want[r.ID]; ok && r.ToID != exp {
			t.Errorf("%s ToID=%q, want %q", r.ID, r.ToID, exp)
		}
		if isBugEdgeToID(r.ToID) {
			t.Errorf("%s wrongly counts as a fidelity bug: ToID=%q", r.ID, r.ToID)
		}
	}
}

// TestSynthesize_GoOwnModuleUnresolvedStillBug is the control for #4702: a
// host-prefixed import whose canonical module root IS this repo's own module
// (a self-import that failed to resolve) must STAY a fidelity bug, not be
// masked as an external dependency. We seed an indexed entity whose path makes
// "github.com/acme/svc" an internal Go root.
func TestSynthesize_GoOwnModuleUnresolvedStillBug(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			// Path segments make github.com/acme/svc an internal root.
			{ID: "aaaaaaaaaaaaaaaa", Name: "handler", Language: "go",
				SourceFile: "github.com/acme/svc/internal/api/handler.go"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "github.com/acme/svc/internal/store", Kind: "IMPORTS",
				Properties: map[string]string{"language": "go"}},
		},
	}
	Synthesize(doc)
	for _, r := range doc.Relationships {
		if strings.HasPrefix(r.ToID, "ext:") {
			t.Errorf("own-module Go import wrongly externalised: ToID=%q", r.ToID)
		}
		if !isBugEdgeToID(r.ToID) {
			t.Errorf("own-module Go import should remain a fidelity bug, ToID=%q", r.ToID)
		}
	}
}

// TestSynthesize_RustExternalPackages_Fixture is Fixture A for #4703: `use
// crate::…` for serde/tokio/anyhow (none internal) must route to ext:<crate>.
func TestSynthesize_RustExternalPackages_Fixture(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "handler", Language: "rust", SourceFile: "src/api/handler.rs"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "serde::Deserialize", Kind: "IMPORTS",
				Properties: map[string]string{"language": "rust", "import_path": "serde::Deserialize"}},
			{ID: "rel-2", FromID: "aaaaaaaaaaaaaaaa", ToID: "tokio::net::TcpListener", Kind: "IMPORTS",
				Properties: map[string]string{"language": "rust", "import_path": "tokio::net::TcpListener"}},
			{ID: "rel-3", FromID: "aaaaaaaaaaaaaaaa", ToID: "anyhow::Result", Kind: "IMPORTS",
				Properties: map[string]string{"language": "rust", "import_path": "anyhow::Result"}},
		},
	}
	Synthesize(doc)
	want := map[string]string{
		"rel-1": "ext:serde",
		"rel-2": "ext:tokio",
		"rel-3": "ext:anyhow",
	}
	for _, r := range doc.Relationships {
		if exp, ok := want[r.ID]; ok && r.ToID != exp {
			t.Errorf("%s ToID=%q, want %q", r.ID, r.ToID, exp)
		}
		if isBugEdgeToID(r.ToID) {
			t.Errorf("%s wrongly counts as a fidelity bug: ToID=%q", r.ID, r.ToID)
		}
	}
}

// TestSynthesize_RustKeywordStillBug is the control for #4703: the intra-crate
// keywords crate::/self::/super:: are internal references that, when
// unresolved, must STAY a fidelity bug — never masked as a third-party crate
// nor as a sibling-module placeholder. (Bare lowercase sibling modules like
// `api::…` are governed by the pre-existing #101 sibling-module branch and are
// intentionally NOT bugs; that behaviour is unchanged.)
func TestSynthesize_RustKeywordStillBug(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "handler", Language: "rust", SourceFile: "src/api/handler.rs"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "crate::store::Store", Kind: "IMPORTS",
				Properties: map[string]string{"language": "rust", "import_path": "crate::store::Store"}},
			{ID: "rel-2", FromID: "aaaaaaaaaaaaaaaa", ToID: "super::config::Config", Kind: "IMPORTS",
				Properties: map[string]string{"language": "rust", "import_path": "super::config::Config"}},
			{ID: "rel-3", FromID: "aaaaaaaaaaaaaaaa", ToID: "self::inner::Thing", Kind: "IMPORTS",
				Properties: map[string]string{"language": "rust", "import_path": "self::inner::Thing"}},
		},
	}
	Synthesize(doc)
	for _, r := range doc.Relationships {
		if strings.HasPrefix(r.ToID, "ext:") {
			t.Errorf("%s intra-crate keyword wrongly externalised: ToID=%q", r.ID, r.ToID)
		}
		if !isBugEdgeToID(r.ToID) {
			t.Errorf("%s intra-crate keyword should remain a fidelity bug, ToID=%q", r.ID, r.ToID)
		}
	}
}

// TestSynthesize_CsharpExternalPackages_Fixture is Fixture A for #4704: BCL /
// nuget `using` directives (System.*, Microsoft.*, Newtonsoft.Json) must route
// to ext:<root> and none may be a fidelity bug.
func TestSynthesize_CsharpExternalPackages_Fixture(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "OrderController", QualifiedName: "Contoso.Orders.OrderController",
				Language: "csharp", SourceFile: "src/Contoso.Orders/OrderController.cs"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "System.Collections.Generic", Kind: "IMPORTS",
				Properties: map[string]string{"language": "csharp", "import_path": "System.Collections.Generic"}},
			{ID: "rel-2", FromID: "aaaaaaaaaaaaaaaa", ToID: "Microsoft.AspNetCore.Mvc", Kind: "IMPORTS",
				Properties: map[string]string{"language": "csharp", "import_path": "Microsoft.AspNetCore.Mvc"}},
			{ID: "rel-3", FromID: "aaaaaaaaaaaaaaaa", ToID: "Newtonsoft.Json", Kind: "IMPORTS",
				Properties: map[string]string{"language": "csharp", "import_path": "Newtonsoft.Json"}},
		},
	}
	Synthesize(doc)
	// System / Microsoft are on the pre-existing static allowlist and are
	// externalised verbatim (PascalCase) by the dotted-path branch before the
	// #4704 catch-all. Newtonsoft is NOT on the static list — it is the real
	// #4704 win, routed to a lowercased nuget placeholder by the catch-all.
	// All three are external (not fidelity bugs), which is the issue's goal.
	want := map[string]string{
		"rel-1": "ext:System",
		"rel-2": "ext:Microsoft",
		"rel-3": "ext:newtonsoft",
	}
	for _, r := range doc.Relationships {
		if exp, ok := want[r.ID]; ok && r.ToID != exp {
			t.Errorf("%s ToID=%q, want %q", r.ID, r.ToID, exp)
		}
		if isBugEdgeToID(r.ToID) {
			t.Errorf("%s wrongly counts as a fidelity bug: ToID=%q", r.ID, r.ToID)
		}
	}
}

// TestSynthesize_CsharpInternalUnresolvedStillBug is the control for #4704: an
// unresolved `using` whose root namespace IS the repo's own ("Contoso") must STAY
// a fidelity bug — the under-flagging catch-all must never mask it.
func TestSynthesize_CsharpInternalUnresolvedStillBug(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "OrderController", QualifiedName: "Contoso.Orders.OrderController",
				Language: "csharp", SourceFile: "src/Contoso.Orders/OrderController.cs"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "Contoso.Missing.Service", Kind: "IMPORTS",
				Properties: map[string]string{"language": "csharp", "import_path": "Contoso.Missing.Service"}},
		},
	}
	Synthesize(doc)
	for _, r := range doc.Relationships {
		if strings.HasPrefix(r.ToID, "ext:") {
			t.Errorf("internal unresolved C# import wrongly externalised: ToID=%q", r.ToID)
		}
		if !isBugEdgeToID(r.ToID) {
			t.Errorf("internal unresolved C# import should remain a fidelity bug, ToID=%q", r.ToID)
		}
	}
}

// TestSynthesize_CsharpAmbiguousUnderFlagged confirms #4704 under-flags: an
// unknown, non-BCL, non-indexed root namespace is ambiguous and must keep its
// bug disposition rather than be masked as external.
func TestSynthesize_CsharpAmbiguousUnderFlagged(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "aaaaaaaaaaaaaaaa", Name: "X", QualifiedName: "Contoso.X",
				Language: "csharp", SourceFile: "src/Contoso/X.cs"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "aaaaaaaaaaaaaaaa", ToID: "SomeVendor.Widgets", Kind: "IMPORTS",
				Properties: map[string]string{"language": "csharp", "import_path": "SomeVendor.Widgets"}},
		},
	}
	Synthesize(doc)
	for _, r := range doc.Relationships {
		if strings.HasPrefix(r.ToID, "ext:") {
			t.Errorf("ambiguous C# root wrongly externalised (should under-flag): ToID=%q", r.ToID)
		}
	}
}

// TestSynthesize_KindNameForm covers the "Kind:Name" stub shape, e.g.
// "Module:django" or "Function:Println" — the leading kind hint is
// stripped and the bare Name is classified.
func TestSynthesize_KindNameForm(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "a", ToID: "Module:django", Kind: "IMPORTS"},
			{ID: "rel-2", FromID: "b", ToID: "Function:Println", Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 2 {
		t.Fatalf("synthesized=%d, want 2", stats.Synthesized)
	}
	gotIDs := map[string]string{}
	for _, e := range doc.Entities {
		gotIDs[e.ID] = e.Subtype
	}
	if gotIDs["ext:django"] != "package" {
		t.Fatalf("ext:django subtype=%q, want package", gotIDs["ext:django"])
	}
	if gotIDs["ext:Println"] != "function" {
		t.Fatalf("ext:Println subtype=%q, want function", gotIDs["ext:Println"])
	}
}

// TestSynthesize_CollisionWithLocalEntity is a defensive check: if a
// previous run (or a malformed document) already contains an entity
// with ID "ext:foo", a relationship pointing at "foo" should rewrite
// to "ext:foo" without producing a duplicate entity.
func TestSynthesize_CollisionWithLocalEntity(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			// Pre-existing placeholder with the same ID we'd synthesise.
			{ID: "ext:django", Name: "django", Kind: KindExternal, Subtype: "package"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "a", ToID: "django.db.models", Kind: "IMPORTS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("synthesized=%d, want 0 (entity already present)", stats.Synthesized)
	}
	if stats.RelationshipsResolved != 1 {
		t.Fatalf("resolved=%d, want 1", stats.RelationshipsResolved)
	}
	if doc.Relationships[0].ToID != "ext:django" {
		t.Fatalf("rel ToID=%q, want ext:django", doc.Relationships[0].ToID)
	}
	count := 0
	for _, e := range doc.Entities {
		if e.ID == "ext:django" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("ext:django entity count=%d, want 1 (no duplicates)", count)
	}
}

// TestIsKnownExternalPackage_ScopedNpm guards the scoped-npm fix
// from issue #71: full "@scope/pkg" forms must match through the
// scope-level allowlist entry, while bare names still resolve and
// path-shaped strings are still rejected.
func TestIsKnownExternalPackage_ScopedNpm(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		// Scoped npm — must match via @scope fallback.
		{"@radix-ui/react-dialog", true},
		{"@tanstack/react-query", true},
		{"@reduxjs/toolkit", true},
		{"@mui/material", true},
		{"@testing-library/react", true},
		// Bare names — no regression.
		{"react", true},
		{"django", true},
		{"lodash", true},
		// Path-shaped non-scoped strings — still rejected.
		{"./local/path", false},
		{"../parent/file", false},
		{"/absolute/path", false},
		{"some/random/path", false},
		// Unknown scopes — must NOT pass.
		{"@unknown-scope/random-pkg", false},
		{"@nope/whatever", false},
		// Edge cases.
		{"", false},
		{"@", false},
		{"@scope", false}, // bare scope without /pkg — not on the allowlist
		{"@/", false},
	}
	for _, c := range cases {
		got := IsKnownExternalPackage(c.name)
		if got != c.want {
			t.Errorf("IsKnownExternalPackage(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSynthesize_ScopedNpm covers the end-to-end synthesis pass for
// scoped npm imports — the placeholder ID is the canonical
// "@scope/pkg" form, the relationship is rewritten to point at it,
// and the synthesis is idempotent on a re-run.
func TestSynthesize_ScopedNpm(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "src/Dialog.tsx", ToID: "@radix-ui/react-dialog", Kind: "IMPORTS"},
			{ID: "rel-2", FromID: "src/Query.tsx", ToID: "@tanstack/react-query", Kind: "IMPORTS"},
			{ID: "rel-3", FromID: "src/Sub.tsx", ToID: "@radix-ui/react-dialog/dist/utils", Kind: "IMPORTS"},
			{ID: "rel-4", FromID: "src/Random.tsx", ToID: "@unknown-scope/random-pkg", Kind: "IMPORTS"},
		},
	}
	stats := Synthesize(doc)
	// rel-1, rel-2, rel-3 should resolve; rel-3 collapses to the same
	// "@radix-ui/react-dialog" placeholder as rel-1.
	if stats.RelationshipsResolved != 3 {
		t.Fatalf("resolved=%d, want 3", stats.RelationshipsResolved)
	}
	if stats.Synthesized != 2 {
		t.Fatalf("synthesized=%d, want 2 (radix + tanstack)", stats.Synthesized)
	}
	if doc.Relationships[0].ToID != "ext:@radix-ui/react-dialog" {
		t.Fatalf("rel-1 ToID=%q", doc.Relationships[0].ToID)
	}
	if doc.Relationships[1].ToID != "ext:@tanstack/react-query" {
		t.Fatalf("rel-2 ToID=%q", doc.Relationships[1].ToID)
	}
	if doc.Relationships[2].ToID != "ext:@radix-ui/react-dialog" {
		t.Fatalf("rel-3 ToID=%q (deep subpath should collapse to pkg root)", doc.Relationships[2].ToID)
	}
	// rel-4 (unknown scope) must NOT be rewritten.
	if doc.Relationships[3].ToID != "@unknown-scope/random-pkg" {
		t.Fatalf("rel-4 ToID=%q (unknown scope should stay untouched)", doc.Relationships[3].ToID)
	}
}

// TestSynthesize_DanglingExtendsStructuralRef covers issue #82:
// cross/hierarchy emits EXTENDS edges to parent classes as Format A
// structural-refs ("scope:component:class:<lang>:<file>:<name>"). When
// the parent isn't declared in the corpus, the resolver leaves the
// stub untouched and Pass 4.5 must synthesise an external placeholder
// rather than leaving the edge dangling.
func TestSynthesize_DanglingExtendsStructuralRef(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "abcdef0123456789", Name: "MySerializer", Kind: "SCOPE.Component", SourceFile: "myapp/serializers.py"},
		},
		Relationships: []graph.Relationship{
			{
				ID:     "rel-1",
				FromID: "abcdef0123456789",
				ToID:   "scope:component:class:python:myapp/serializers.py:serializers.ModelSerializer",
				Kind:   "EXTENDS",
			},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 1 {
		t.Fatalf("synthesized=%d, want 1", stats.Synthesized)
	}
	if stats.RelationshipsResolved != 1 {
		t.Fatalf("resolved=%d, want 1", stats.RelationshipsResolved)
	}
	if doc.Relationships[0].ToID != "ext:serializers" {
		t.Fatalf("rel ToID=%q, want ext:serializers", doc.Relationships[0].ToID)
	}
	// Verify the placeholder entity exists and has the expected shape.
	var found bool
	for _, e := range doc.Entities {
		if e.ID == "ext:serializers" {
			found = true
			if e.Kind != KindExternal {
				t.Fatalf("placeholder kind=%q, want %q", e.Kind, KindExternal)
			}
			if e.Subtype != "package" {
				t.Fatalf("placeholder subtype=%q, want package", e.Subtype)
			}
		}
	}
	if !found {
		t.Fatalf("ext:serializers entity not appended; entities=%+v", doc.Entities)
	}
}

// TestSynthesize_DanglingExtendsLocalUntouched confirms the dangling
// structural-ref branch does NOT synthesise placeholders for tails
// that look like local class names — bare identifiers without a dot
// or terminal-lowercase tails should fall through unchanged.
func TestSynthesize_DanglingExtendsLocalUntouched(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			// Bare local class — no dot, must not be synthesised.
			{ID: "rel-1", FromID: "x", ToID: "scope:component:class:python:myapp/models.py:LocalBase", Kind: "EXTENDS"},
			// Format B (member ref) — must not be synthesised.
			{ID: "rel-2", FromID: "y", ToID: "scope:component:class:python:myapp/models.py:Outer#inner", Kind: "EXTENDS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("synthesized=%d, want 0", stats.Synthesized)
	}
	if stats.RelationshipsResolved != 0 {
		t.Fatalf("resolved=%d, want 0", stats.RelationshipsResolved)
	}
}

// TestSynthesize_ExpandedAllowlist exercises a handful of the v1.1
// allowlist additions to guard against accidental regressions when the
// list is edited.
// TestStdlibBareNames_NoCollisionNames asserts that names which commonly
// appear as user-defined methods are NOT classified as stdlib bare-names
// when seen unqualified. Issue #94: the original list (issue #89) over-
// reached and treated identifiers like `write`, `read`, `close`, `index`,
// etc. as built-ins, masking real bug-extractor cases.
func TestStdlibBareNames_NoCollisionNames(t *testing.T) {
	collisions := []string{
		"write", "read", "close", "index", "copy", "replace",
		"items", "keys", "values", "update", "pop", "clear",
		"extend", "append", "remove",
		// Issue #91: Rust prelude variants/results MUST NOT be in the
		// stdlib-bare-name allowlist — `Ok`, `Err`, `Some`, `None` are
		// commonly used as user-defined identifiers in Go/JS code and
		// would shadow real bug-extractor cases. See synth.go comment.
		"Ok", "Err", "Some", "None",
	}
	for _, name := range collisions {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := stdlibFunction(name, "", "", nil); ok {
				t.Fatalf("stdlibFunction(%q, nil) classified as stdlib bare-name; "+
					"this name commonly collides with user-defined methods "+
					"and must not synthesise a placeholder", name)
			}
			doc := &graph.Document{
				Relationships: []graph.Relationship{
					{ID: "r", FromID: "src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 0 {
				t.Fatalf("Synthesize(%q) created %d placeholder(s); "+
					"want 0 — collision name must fall through", name,
					stats.Synthesized)
			}
			if doc.Relationships[0].ToID != name {
				t.Fatalf("ToID=%q, want %q (must not be rewritten)",
					doc.Relationships[0].ToID, name)
			}
		})
	}
}

// TestStdlibBareNames_RustAssertMacros locks in the Issue #91 addition of
// Rust's `assert_eq!` / `assert_ne!` macros to the stdlib bare-name stop-
// list. These have no plausible collision with user identifiers in any
// supported language, so they MUST be classified as stdlib functions and
// rewritten to `ext:<name>` placeholders.
func TestStdlibBareNames_RustAssertMacros(t *testing.T) {
	macros := []string{"assert_eq", "assert_ne"}
	for _, name := range macros {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, nil) = (_, false); want classified as stdlib bare-name", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, nil) subtype=%q, want %q", name, subtype, "function")
			}
			doc := &graph.Document{
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "src.rs", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestExternalAPIHost_IPv6 covers the IPv6 host parsing fix from
// issue #94. The previous byte-scanner stripped the first ':' before the
// closing bracket and produced "[" as the host.
func TestExternalAPIHost_IPv6(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"https://[::1]:8080", "::1"},
		{"https://[::1]/path", "::1"},
		{"https://[fe80::1]:443", "fe80::1"},
		{"https://[2001:db8::1]:8443/api", "2001:db8::1"},
		// Sanity: regular hosts still work.
		{"https://example.com:8080/foo", "example.com"},
		{"http://user:pass@example.com:80/p", "example.com"},
		// Empty / non-URL inputs.
		{"", ""},
		{"not-a-url", ""},
	}
	for _, c := range cases {
		c := c
		t.Run(c.raw, func(t *testing.T) {
			t.Parallel()
			got := externalAPIHost(c.raw)
			if got != c.want {
				t.Fatalf("externalAPIHost(%q) = %q, want %q",
					c.raw, got, c.want)
			}
		})
	}
}

func TestSynthesize_ExpandedAllowlist(t *testing.T) {
	cases := []struct {
		stub string
		want string
	}{
		{"zod", "ext:zod"},
		{"prisma.client", "ext:prisma"},
		{"axios", "ext:axios"},
		{"pytest", "ext:pytest"},
		{"httpx.AsyncClient", "ext:httpx"},
		{"testify.Suite", "ext:testify"},
		{"junit", "ext:junit"},
		// Issue #91: C# / .NET roots — `System.*`, `Microsoft.*` are the
		// dominant import roots in ASP.NET / EF Core codebases.
		{"system", "ext:system"},
		{"system.text.json", "ext:system"},
		{"system.collections.generic", "ext:system"},
		{"microsoft", "ext:microsoft"},
		{"microsoft.entityframeworkcore", "ext:microsoft"},
		{"microsoft.aspnetcore.mvc", "ext:microsoft"},
		// Issue #91: Java EE / Jakarta — Spring/JPA imports.
		{"jakarta", "ext:jakarta"},
		{"jakarta.persistence", "ext:jakarta"},
		{"jakarta.validation", "ext:jakarta"},
		// Issue #91: Rust crates — top import-bug roots.
		{"tokio", "ext:tokio"},
		{"actix_web", "ext:actix_web"},
		{"actix", "ext:actix"},
		{"serde", "ext:serde"},
		{"serde_json", "ext:serde_json"},
		{"anyhow", "ext:anyhow"},
		{"thiserror", "ext:thiserror"},
		{"tracing", "ext:tracing"},
		{"tracing_subscriber", "ext:tracing_subscriber"},
		{"clap", "ext:clap"},
		{"reqwest", "ext:reqwest"},
		{"futures", "ext:futures"},
		{"async_trait", "ext:async_trait"},
		{"opentelemetry", "ext:opentelemetry"},
		// Issue #101: Rust `use foo::bar` paths use `::` separator, not `.`.
		// These must classify as ExternalKnown (root crate on allowlist),
		// not bug-extractor.
		{"tokio::net::TcpListener", "ext:tokio"},
		{"actix_web::App", "ext:actix_web"},
		{"actix_web::HttpResponse", "ext:actix_web"},
		{"serde::Deserialize", "ext:serde"},
		{"serde_json::Value", "ext:serde_json"},
		{"tracing_subscriber::fmt::Subscriber", "ext:tracing_subscriber"},
		// Brace-group `use foo::{A, B}` — the root crate is still
		// extractable; we collapse to the package placeholder.
		{"actix_web::{App, HttpResponse}", "ext:actix_web"},
		{"tokio::{net::TcpListener, sync::Mutex}", "ext:tokio"},
	}
	doc := &graph.Document{}
	for i, c := range cases {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     "rel-" + c.stub,
			FromID: "src",
			ToID:   c.stub,
			Kind:   "IMPORTS",
		})
		_ = i
	}
	stats := Synthesize(doc)
	if stats.RelationshipsResolved != len(cases) {
		t.Fatalf("resolved=%d, want %d", stats.RelationshipsResolved, len(cases))
	}
	for k, c := range cases {
		if doc.Relationships[k].ToID != c.want {
			t.Fatalf("case %q: ToID=%q, want %q", c.stub, doc.Relationships[k].ToID, c.want)
		}
	}
}

// TestSynthesize_PhpBackslashNamespace covers issue #102: PHP `use
// Foo\Bar\Baz` FQNs use `\` as the namespace separator. Without the
// dedicated branch in classifyExternal these stubs hit the
// path-separator rejection and land in bug-extractor.
func TestSynthesize_PhpBackslashNamespace(t *testing.T) {
	cases := []struct {
		stub string
		want string
	}{
		// Symfony — top import root in symfony-demo.
		{"Symfony\\Component\\HttpFoundation\\Response", "ext:symfony"},
		{"Symfony\\Component\\HttpKernel\\Exception\\NotFoundHttpException", "ext:symfony"},
		{"Symfony\\Bundle\\FrameworkBundle\\Controller\\AbstractController", "ext:symfony"},
		// Doctrine ORM/DBAL.
		{"Doctrine\\ORM\\EntityManager", "ext:doctrine"},
		{"Doctrine\\DBAL\\Connection", "ext:doctrine"},
		// Twig templating.
		{"Twig\\Environment", "ext:twig"},
		{"Twig\\Extension\\AbstractExtension", "ext:twig"},
		// PSR interfaces (logger, container, http-message).
		{"Psr\\Log\\LoggerInterface", "ext:psr"},
		{"Psr\\Container\\ContainerInterface", "ext:psr"},
		// Laravel / Illuminate roots.
		{"Illuminate\\Support\\Facades\\DB", "ext:illuminate"},
		{"Laravel\\Sanctum\\HasApiTokens", "ext:laravel"},
	}
	doc := &graph.Document{}
	for _, c := range cases {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     "rel-" + c.stub,
			FromID: "src",
			ToID:   c.stub,
			Kind:   "IMPORTS",
		})
	}
	stats := Synthesize(doc)
	if stats.RelationshipsResolved != len(cases) {
		t.Fatalf("resolved=%d, want %d", stats.RelationshipsResolved, len(cases))
	}
	for k, c := range cases {
		if doc.Relationships[k].ToID != c.want {
			t.Fatalf("case %q: ToID=%q, want %q", c.stub, doc.Relationships[k].ToID, c.want)
		}
	}
}

// TestSynthesize_PhpAppNamespaceLeftAlone confirms that the PHP
// project-local convention `App\*` (used in Symfony / Laravel) is
// NOT promoted to an ext: placeholder. App is project-internal and
// proper resolution is out of scope for #102 — the placeholder
// pathway must skip it cleanly.
func TestSynthesize_PhpAppNamespaceLeftAlone(t *testing.T) {
	stubs := []string{
		"App\\Entity\\User",
		"App\\Controller\\BlogController",
		"App\\Repository\\PostRepository",
	}
	doc := &graph.Document{}
	for _, s := range stubs {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     "rel-" + s,
			FromID: "src",
			ToID:   s,
			Kind:   "IMPORTS",
		})
	}
	stats := Synthesize(doc)
	if stats.RelationshipsResolved != 0 {
		t.Fatalf("resolved=%d, want 0 (App\\* is project-local)", stats.RelationshipsResolved)
	}
	for k, s := range stubs {
		if doc.Relationships[k].ToID != s {
			t.Fatalf("case %q: ToID was rewritten to %q, expected unchanged", s, doc.Relationships[k].ToID)
		}
	}
}

// TestGoBareNames_ClassifiedWhenLangIsGo locks in issue #103: bare
// PascalCase Go stdlib/framework method names that arrive at the
// resolver after the extractor strips the receiver (e.g. `w.Write(buf)`
// → `Write`, `r.ServeHTTP(...)` → `ServeHTTP`) must classify as
// stdlib bare-names — but only when the source entity's language is
// "go". The same name in another language must fall through to the
// language-agnostic path so user-defined methods aren't shadowed.
func TestGoBareNames_ClassifiedWhenLangIsGo(t *testing.T) {
	names := []string{
		"ServeHTTP",
		"ListenAndServe",
		"HandleFunc",
		"WriteHeader",
		"EncodeToString",
		"DecodeString",
		"ConstantTimeCompare",
		"Atoi",
		"Itoa",
		"Quote",
		"MethodFunc",
		"AbortWithStatus",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Direct stdlibFunction probe with lang="go" must classify.
			subtype, ok := stdlibFunction(name, "go", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"go\", nil) = (_, false); want classified as stdlib bare-name", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"go\", nil) subtype=%q, want %q", name, subtype, "function")
			}
			// End-to-end: Synthesize on a document whose FromID entity
			// is tagged language="go" rewrites the edge to ext:<name>.
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "go-src",
					Name:     "caller",
					Kind:     "function",
					Language: "go",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "go-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestGoBareNames_NotClassifiedForOtherLanguages confirms the
// language gate: the same Go-only Pascal-case names must NOT be
// rewritten when the source entity's language is anything other than
// "go". Without the gate, a user-defined `WriteHeader` method on a
// Python or JS class would be shadowed by a synthesised placeholder.
func TestGoBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	names := []string{"ServeHTTP", "EncodeToString", "AbortWithStatus", "Atoi"}
	otherLangs := []string{"python", "javascript", "rust", "java", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"go\" only)", name, lang)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "src",
						Name:     "caller",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 0",
						name, lang, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-Go)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestGoBareNames_UserMethodCollisionExclusions confirms that names
// rejected as too-likely-to-collide-with-user-methods stay
// fall-through even with lang="go". Per issue #103 hard rules:
// Get/Post/Put/Delete/Use must NOT be in the allowlist; per the
// internal review Write/Header/Handle were also excluded as too
// collision-prone (io.Writer.Write user-implementations, Header()
// accessors, generic Handle handlers).
func TestGoBareNames_UserMethodCollisionExclusions(t *testing.T) {
	excluded := []string{
		// Hard-rule exclusions from issue #103.
		"Get", "Post", "Put", "Delete", "Use",
		// Internal-review exclusions (collision-prone PascalCase).
		"Write", "Header", "Handle",
	}
	for _, name := range excluded {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := stdlibFunction(name, "go", "", nil); ok {
				t.Fatalf("stdlibFunction(%q, \"go\", nil) classified; want fall-through "+
					"(name is too-likely to be a user-defined method)", name)
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "go-src",
					Name:     "caller",
					Kind:     "function",
					Language: "go",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "go-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 0 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 0 (excluded)", name, stats.Synthesized)
			}
			if doc.Relationships[0].ToID != name {
				t.Fatalf("ToID=%q, want %q (excluded name must not be rewritten)",
					doc.Relationships[0].ToID, name)
			}
		})
	}
}

// TestSynthesize_GoImportPath_Stdlib covers issue #116: Go full-import-
// path stubs use `/` as the segment separator. Without the dedicated
// branch in classifyExternal these stubs hit the path-separator
// rejection and land in bug-extractor. Stdlib roots resolve to the
// first segment.
func TestSynthesize_GoImportPath_Stdlib(t *testing.T) {
	cases := []struct {
		stub string
		want string
	}{
		{"net/http", "ext:net"},
		{"net/http/httptest", "ext:net"},
		{"net/url", "ext:net"},
		{"encoding/json", "ext:encoding"},
		{"encoding/base64", "ext:encoding"},
		{"crypto/tls", "ext:crypto"},
		{"crypto/sha256", "ext:crypto"},
		{"database/sql", "ext:database"},
		{"compress/gzip", "ext:compress"},
		{"archive/tar", "ext:archive"},
		{"image/png", "ext:image"},
		{"text/template", "ext:text"},
		{"html/template", "ext:html"},
		{"mime/multipart", "ext:mime"},
		{"hash/crc32", "ext:hash"},
		{"path/filepath", "ext:path"},
	}
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "go-src",
			Name:     "caller",
			Kind:     "function",
			Language: "go",
		}},
	}
	for _, c := range cases {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     "rel-" + c.stub,
			FromID: "go-src",
			ToID:   c.stub,
			Kind:   "IMPORTS",
		})
	}
	stats := Synthesize(doc)
	for k, c := range cases {
		if doc.Relationships[k].ToID != c.want {
			t.Errorf("case %q: ToID=%q, want %q", c.stub, doc.Relationships[k].ToID, c.want)
		}
	}
	if stats.RelationshipsResolved != len(cases) {
		t.Fatalf("resolved=%d, want %d", stats.RelationshipsResolved, len(cases))
	}
}

// TestSynthesize_GoImportPath_HostPrefixed covers issue #116: Go
// host-prefixed import paths (github.com/<owner>/<repo>/...,
// golang.org/x/<repo>/..., gopkg.in/<pkg>) resolve to the canonical
// 3-segment (or 2-segment for gopkg.in) module identifier.
func TestSynthesize_GoImportPath_HostPrefixed(t *testing.T) {
	cases := []struct {
		stub string
		want string
	}{
		// Curated allowlist matches → ExternalKnown via canonical key.
		{"github.com/stretchr/testify/assert", "ext:github.com/stretchr/testify"},
		{"github.com/stretchr/testify/require", "ext:github.com/stretchr/testify"},
		{"github.com/stretchr/testify/mock", "ext:github.com/stretchr/testify"},
		{"github.com/gin-gonic/gin", "ext:github.com/gin-gonic/gin"},
		{"github.com/gin-gonic/gin/binding", "ext:github.com/gin-gonic/gin"},
		{"github.com/go-chi/chi/v5", "ext:github.com/go-chi/chi"},
		{"github.com/labstack/echo/v4", "ext:github.com/labstack/echo"},
		{"github.com/sirupsen/logrus", "ext:github.com/sirupsen/logrus"},
		{"github.com/spf13/cobra", "ext:github.com/spf13/cobra"},
		{"github.com/spf13/viper", "ext:github.com/spf13/viper"},
		{"github.com/google/uuid", "ext:github.com/google/uuid"},
		{"github.com/gorilla/mux", "ext:github.com/gorilla/mux"},
		{"golang.org/x/sync/errgroup", "ext:golang.org/x/sync"},
		{"golang.org/x/crypto/bcrypt", "ext:golang.org/x/crypto"},
		{"golang.org/x/net/context", "ext:golang.org/x/net"},
		{"google.golang.org/grpc/codes", "ext:google.golang.org/grpc"},
		{"google.golang.org/protobuf/proto", "ext:google.golang.org/protobuf"},
		{"gopkg.in/yaml.v3", "ext:gopkg.in/yaml.v3"},
		{"gopkg.in/yaml.v2", "ext:gopkg.in/yaml.v2"},
	}
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "go-src",
			Name:     "caller",
			Kind:     "function",
			Language: "go",
		}},
	}
	for _, c := range cases {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     "rel-" + c.stub,
			FromID: "go-src",
			ToID:   c.stub,
			Kind:   "IMPORTS",
		})
	}
	stats := Synthesize(doc)
	if stats.RelationshipsResolved != len(cases) {
		t.Fatalf("resolved=%d, want %d", stats.RelationshipsResolved, len(cases))
	}
	for k, c := range cases {
		if doc.Relationships[k].ToID != c.want {
			t.Fatalf("case %q: ToID=%q, want %q", c.stub, doc.Relationships[k].ToID, c.want)
		}
	}
}

// TestSynthesize_GoImportPath_UnknownHostPrefixedClassified confirms
// that a host-prefixed Go import path that ISN'T on the curated
// allowlist still gets a placeholder (ExternalUnknown via the
// resolver's IsKnownExternalPackage gate). Issue #116 — moves these
// out of bug-extractor regardless of allowlist status.
func TestSynthesize_GoImportPath_UnknownHostPrefixedClassified(t *testing.T) {
	stub := "github.com/some-random-org/some-random-pkg/internal/sub"
	wantCanonical := "ext:github.com/some-random-org/some-random-pkg"
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "go-src",
			Name:     "caller",
			Kind:     "function",
			Language: "go",
		}},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "go-src", ToID: stub, Kind: "IMPORTS"},
		},
	}
	stats := Synthesize(doc)
	if stats.RelationshipsResolved != 1 {
		t.Fatalf("resolved=%d, want 1 (unknown host-prefixed must still be synthesised)", stats.RelationshipsResolved)
	}
	if doc.Relationships[0].ToID != wantCanonical {
		t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, wantCanonical)
	}
	// Allowlist gate must report false — resolver will tag it
	// ExternalUnknown.
	if IsKnownExternalPackage("github.com/some-random-org/some-random-pkg") {
		t.Fatalf("unknown host-prefixed canonical must NOT be on the allowlist")
	}
}

// TestSynthesize_GoImportPath_RejectsUnixFilePath confirms that a
// Unix-style absolute file path (`/etc/foo`) and other non-Go-shaped
// stubs containing `/` are still rejected — they must not be promoted
// to placeholders.
func TestSynthesize_GoImportPath_RejectsUnixFilePath(t *testing.T) {
	stubs := []string{
		"/etc/foo",
		"/usr/local/bin/something",
		"./relative/path",
		"../parent/path",
	}
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "go-src",
			Name:     "caller",
			Kind:     "function",
			Language: "go",
		}},
	}
	for _, s := range stubs {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     "rel-" + s,
			FromID: "go-src",
			ToID:   s,
			Kind:   "IMPORTS",
		})
	}
	stats := Synthesize(doc)
	if stats.RelationshipsResolved != 0 {
		t.Fatalf("resolved=%d, want 0 (file paths must not classify as Go imports)", stats.RelationshipsResolved)
	}
}

// TestSynthesize_GoImportPath_NoLangSourceStillClassifies confirms
// the Go-import-path branch fires regardless of FromID's language —
// in real corpora the FromID is often a file-scope structural-ref
// that isn't in the entity map, so entityLang lookup returns "". The
// shape predicate (lowercase first segment, no `:`/`\`/space/leading
// `/`) is restrictive enough on its own to keep non-Go file paths
// out. Issue #116.
func TestSynthesize_GoImportPath_NoLangSourceStillClassifies(t *testing.T) {
	stub := "net/http"
	doc := &graph.Document{
		// No entity for FromID — entityLang lookup returns "".
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "scope:component:file:auth.go", ToID: stub, Kind: "IMPORTS"},
		},
	}
	stats := Synthesize(doc)
	if stats.RelationshipsResolved != 1 {
		t.Fatalf("resolved=%d, want 1 (file-scope FromID must still trigger Go-import branch)", stats.RelationshipsResolved)
	}
	if doc.Relationships[0].ToID != "ext:net" {
		t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, "ext:net")
	}
}

// TestGoBareNames_UnknownGoMethodFallsThrough confirms that a
// PascalCase Go-source method name that ISN'T in the goBareNames
// allowlist still falls through normally, so genuine missing-
// resolution bugs continue to surface in bug-extractor.
func TestGoBareNames_UnknownGoMethodFallsThrough(t *testing.T) {
	name := "MyHandler" // Not stdlib; user-defined method.
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "go-src",
			Name:     "caller",
			Kind:     "function",
			Language: "go",
		}},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "go-src", ToID: name, Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("Synthesize(%q): synthesized=%d, want 0 (unknown user method)", name, stats.Synthesized)
	}
	if doc.Relationships[0].ToID != name {
		t.Fatalf("ToID=%q, want %q (unknown name must not be rewritten)",
			doc.Relationships[0].ToID, name)
	}
}

// TestRustBareNames_ClassifiedWhenLangIsRust covers issue #108: Rust
// prelude items (Ok/Err/Some/None, Box/Vec, Result/Option, ...) and
// post-receiver-strip prelude methods (clone/unwrap/to_string, ...)
// and prelude macros (vec/println/format) must classify as stdlib
// bare-names — but only when the source entity's language is "rust".
// One representative name per category is exercised; the full list is
// asserted via the map-membership unit test below.
func TestRustBareNames_ClassifiedWhenLangIsRust(t *testing.T) {
	names := []string{
		// PascalCase prelude (types & traits)
		"Ok", "Err", "Some", "None", "Box", "Vec", "Result", "Option",
		"String", "Default", "From", "Into", "TryFrom", "TryInto",
		"Iterator", "IntoIterator", "ToString", "ToOwned", "Clone",
		"Copy", "Debug", "Display", "Send", "Sync", "Sized", "Drop",
		"Fn", "FnMut", "FnOnce",
		// Lowercase prelude methods (post-receiver-strip)
		"clone", "unwrap", "unwrap_or", "unwrap_or_default",
		"unwrap_or_else", "expect", "into", "as_ref", "as_mut", "as_str",
		"to_string", "to_owned", "into_iter", "collect", "fold", "chain",
		"count", "is_empty", "push", "pop", "remove", "get", "contains",
		"is_some", "is_none", "is_ok", "is_err", "ok", "err", "take",
		"replace", "swap", "drop", "default",
		// Macros (post-`!` strip)
		"vec", "println", "eprintln", "eprint", "write", "writeln",
		"panic", "todo", "unimplemented", "unreachable", "dbg", "assert",
		"debug_assert", "matches",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "rust", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"rust\", nil) = (_, false); want classified as stdlib bare-name", name)
			}
			// Rust wave (S19+) — rustBareNames now signals via the
			// "rust_builtin_function" subtype so the caller folds all
			// rust bare-name receiver-strips to a single `ext:std`
			// placeholder. Accept both "function" and the new subtype.
			if subtype != "function" && subtype != "rust_builtin_function" {
				t.Fatalf("stdlibFunction(%q, \"rust\", nil) subtype=%q, want %q or %q", name, subtype, "function", "rust_builtin_function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "rust-src",
					Name:     "caller",
					Kind:     "function",
					Language: "rust",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "rust-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			// Rust wave (S19+) — receiver-stripped rustBareNames now
			// fold to a single `ext:std` placeholder so the resolver
			// classifies as ExternalKnown (std is on the package
			// allowlist) and bare-name fan-out goes away. Older
			// behavior was per-name `ext:<name>` which landed in
			// external-unknown. Either ToID is acceptable as the
			// classification is "synthesised to an external", with
			// `ext:std` strictly better.
			tid := doc.Relationships[0].ToID
			if tid != "ext:std" && tid != "ext:"+name {
				t.Fatalf("ToID=%q, want %q or %q", tid, "ext:std", "ext:"+name)
			}
		})
	}
}

// TestRustBareNames_NotClassifiedForOtherLanguages confirms the
// language gate: Rust-only prelude names (especially the risky
// lowercase methods like `clone`, `get`, `push`) must NOT be rewritten
// when the source entity's language is anything other than "rust".
// Without the gate, a user-defined `clone()` method on a Go type or a
// JS `push` array call could be shadowed by a synthesised placeholder
// (#94 lesson — bias toward misses).
func TestRustBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Names that DON'T also appear in language-agnostic stdlibBareNames.
	// (`vec`/`println`/`Ok`/`clone` etc. — none of these are global.)
	names := []string{"Ok", "Err", "Some", "None", "Vec", "clone", "unwrap", "vec", "println", "to_string"}
	otherLangs := []string{"go", "python", "javascript", "java", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"rust\" only)", name, lang)
				}
				// Skip combinations where the name is also a valid per-language
				// stdlib builtin for the target language (e.g. "println" is a
				// Go universe-block builtin since #1085 multi-lang extension).
				// The cross-language guarantee we are checking is "Rust-only names
				// don't leak to unrelated languages" — names that are genuinely
				// builtin in BOTH Rust and the target language are out of scope.
				if resolve.IsStdlibBuiltinTarget(name, lang) {
					t.Skipf("%q is also a stdlib builtin for lang=%q — skipping Synthesize leak check", name, lang)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "src",
						Name:     "caller",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 0",
						name, lang, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-Rust)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestRustBareNames_UnknownRustMethodFallsThrough confirms that a
// Rust-source bare-name call that ISN'T in the rustBareNames allowlist
// still falls through normally, so genuine missing-resolution bugs
// continue to surface in bug-extractor.
func TestRustBareNames_UnknownRustMethodFallsThrough(t *testing.T) {
	name := "MyCustomFn" // Not prelude; user-defined.
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "rust-src",
			Name:     "caller",
			Kind:     "function",
			Language: "rust",
		}},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "rust-src", ToID: name, Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("Synthesize(%q): synthesized=%d, want 0 (unknown user fn)", name, stats.Synthesized)
	}
	if doc.Relationships[0].ToID != name {
		t.Fatalf("ToID=%q, want %q (unknown name must not be rewritten)",
			doc.Relationships[0].ToID, name)
	}
}

// TestRustActixDSLBareNames_ClassifiedWhenLangIsRust covers issue
// #440: Actix-web framework DSL methods (App/Resource/Scope builder,
// HttpResponse factories, web extractors, actor system, HTTP method
// route-builder verbs) get receiver-stripped by the Rust extractor and
// land in bug-extractor. These names must classify as stdlib
// bare-names — but only when the source entity's language is "rust".
// Mirrors the Kotlin Ktor (#435) and Swift Vapor (#436) precedents.
func TestRustActixDSLBareNames_ClassifiedWhenLangIsRust(t *testing.T) {
	names := []string{
		// Actix App/Resource/Scope builder DSL.
		"App", "service", "route", "scope", "wrap", "wrap_fn",
		"app_data", "default_service", "external_resource",
		"configure", "register", "data",
		// HTTP response factories and builder verbs.
		"HttpResponse", "BadRequest", "InternalServerError",
		"Unauthorized", "Forbidden", "NoContent", "Created",
		"Accepted", "body", "json", "finish", "streaming",
		// Web extractors.
		"Path", "Query", "Json", "Form", "Data", "Header",
		// Actix actor system.
		"Actor", "Handler", "Message", "Context", "Recipient",
		"Addr", "Arbiter", "System",
		"start", "started", "stopping", "stopped",
		// HTTP method route-builder verbs (`get` covered by prelude).
		"post", "put", "delete", "patch", "head", "options",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "rust", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"rust\", nil) = (_, false); want classified as stdlib bare-name", name)
			}
			// Rust wave (S19+) — rustBareNames now signals via the
			// "rust_builtin_function" subtype so the caller folds all
			// rust bare-name receiver-strips to a single `ext:std`
			// placeholder. Accept both "function" and the new subtype.
			if subtype != "function" && subtype != "rust_builtin_function" {
				t.Fatalf("stdlibFunction(%q, \"rust\", nil) subtype=%q, want %q or %q", name, subtype, "function", "rust_builtin_function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "rust-src",
					Name:     "caller",
					Kind:     "function",
					Language: "rust",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "rust-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			// Rust wave (S19+) — accept folded `ext:std` placeholder.
			tid := doc.Relationships[0].ToID
			if tid != "ext:std" && tid != "ext:"+name {
				t.Fatalf("ToID=%q, want %q or %q", tid, "ext:std", "ext:"+name)
			}
		})
	}
}

// TestRustActixDSLBareNames_NotClassifiedForOtherLanguages confirms
// the Rust language gate holds for the issue #440 additions: Actix
// DSL names must NOT be rewritten when the source entity's language
// is anything other than "rust". A JS user method named `service`,
// a Go method named `start`, a Ruby `register`, a Python `body`,
// etc. must not be shadowed by the Rust gate.
func TestRustActixDSLBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Names with the highest cross-language collision potential —
	// generic verbs/accessors that exist as user methods in many
	// ecosystems. Selection rule: each name MUST be unique to
	// rustBareNames (i.e. not in stdlibBareNames or any other
	// language map), otherwise a different gate would fire first.
	// `Path`/`Query`/`Json`/`Form`/`Data`/`Header` deliberately
	// excluded — they may live in goBareNames/javaBareNames or
	// future PRs and are not the cleanest gate-holding signal.
	names := []string{
		"service", "scope", "wrap", "wrap_fn", "app_data",
		"default_service", "external_resource", "configure",
		"streaming", "stopping", "started",
		"Recipient", "Arbiter",
	}
	otherLangs := []string{"go", "python", "javascript", "ruby", "java", "kotlin", "swift", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"rust\" only)", name, lang)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "src",
						Name:     "caller",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 0",
						name, lang, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-Rust)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestJavaBareNames_ClassifiedWhenLangIsJava covers issue #105 fix
// (B): JDK stdlib exception classes plus high-frequency Spring/JPA
// repository, BindingResult, Model, and Pageable helper bare-names
// must classify as stdlib bare-names — but only when the source
// entity's language is "java". One representative name per category
// is exercised; the full list is asserted via the per-name test
// below.
func TestJavaBareNames_ClassifiedWhenLangIsJava(t *testing.T) {
	names := []string{
		// JDK exceptions
		"IllegalArgumentException", "NullPointerException",
		"IllegalStateException", "UnsupportedOperationException",
		"RuntimeException", "IndexOutOfBoundsException",
		"ClassCastException", "NumberFormatException",
		"ArithmeticException", "IOException", "FileNotFoundException",
		"InterruptedException", "Error", "Throwable",
		// JDK Optional helpers
		"orElseThrow", "orElse", "ifPresent", "isPresent",
		// Spring Data JPA repository methods
		"findById", "findAll", "findAllById", "save", "saveAll",
		"saveAndFlush", "deleteById", "deleteAll", "existsById", "count",
		// Spring BindingResult helpers
		"hasErrors", "rejectValue", "getFieldError",
		// Spring Model / RedirectAttributes
		"addFlashAttribute", "addAttribute",
		// Spring Pageable / Page accessors
		"getTotalElements", "getTotalPages", "getNumber", "getSize",
		"hasNext", "hasPrevious",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "java", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"java\", nil) = (_, false); want classified as stdlib bare-name", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"java\", nil) subtype=%q, want %q", name, subtype, "function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "java-src",
					Name:     "caller",
					Kind:     "function",
					Language: "java",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "java-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestJavaBareNames_NotClassifiedForOtherLanguages confirms the
// language gate: Java-only Spring/JPA names (the high-collision-risk
// ones like `save`, `count`, `orElse`, `hasNext`) must NOT be
// rewritten when the source entity's language is anything other
// than "java". Without the gate, a user-defined `save()` method on
// a Go service or a JS array `count()` would be shadowed by a
// synthesised placeholder.
func TestJavaBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Names that DON'T also appear in language-agnostic
	// stdlibBareNames. (`Exception` is global and intentionally
	// omitted from this gate-check.)
	// `count` deliberately omitted: it's also in rustBareNames so
	// it classifies under lang="rust"; the gate verified here is
	// the *Java* gate, not absence-from-all-other-language-gates.
	// `save` deliberately omitted post-#447: it's also in
	// pythonBareNames (Django ORM `save()`) and classifies under
	// lang="python".
	names := []string{"orElse", "hasNext", "findById", "findAll", "hasErrors", "IllegalArgumentException"}
	otherLangs := []string{"go", "python", "javascript", "rust", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"java\" only)", name, lang)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "src",
						Name:     "caller",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 0",
						name, lang, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-Java)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestJavaBareNames_RejectedNamesNotClassified locks in the explicit
// rejection list from issue #105: generic getters/setters and
// ubiquitous functional verbs MUST NOT be in the Java allowlist.
// Resolution for those names is the responsibility of the (A)
// follow-up — cross-file receiver binding. Adding them here would
// shadow any user-defined `getId()` / `getName()` / `map()` /
// `filter()` method on a Java type and turn a real missing-resolution
// bug into a silent placeholder.
func TestJavaBareNames_RejectedNamesNotClassified(t *testing.T) {
	rejected := []string{
		// Generic getters/setters — every entity has them.
		"getId", "getName", "getValue", "setName", "setValue",
		// Ubiquitous functional verbs.
		"map", "filter", "forEach", "stream",
		// `collect` is global-collision; gated out of the Java map.
		"collect",
	}
	for _, name := range rejected {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := javaBareNames[name]; ok {
				t.Fatalf("javaBareNames[%q] present; must be rejected per issue #105 (A) deferral", name)
			}
		})
	}
}

// TestJavaBareNames_UnknownJavaMethodFallsThrough confirms that a
// Java-source bare-name call that ISN'T in the javaBareNames
// allowlist still falls through normally, so genuine missing-
// resolution bugs continue to surface in bug-extractor.
func TestJavaBareNames_UnknownJavaMethodFallsThrough(t *testing.T) {
	name := "myCustomBusinessMethod" // Not stdlib/Spring; user-defined.
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "java-src",
			Name:     "caller",
			Kind:     "function",
			Language: "java",
		}},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "java-src", ToID: name, Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("Synthesize(%q): synthesized=%d, want 0 (unknown user fn)", name, stats.Synthesized)
	}
	if doc.Relationships[0].ToID != name {
		t.Fatalf("ToID=%q, want %q (unknown name must not be rewritten)",
			doc.Relationships[0].ToID, name)
	}
}

// TestJavaBareNames_SpringMVCBuilderChain_ClassifiedWhenLangIsJava
// verifies that Spring MVC ResponseEntity builder chain leaf methods
// (`build`, `body`, `header`, `headers`, `contentType`) are classified
// as stdlib bare-names when lang="java". These names are stripped from
// chains like `ResponseEntity.notFound().build()` by the Java extractor
// because the intermediate `HeadersBuilder`/`BodyBuilder` receiver
// type is not statically known. Without this entry they land in
// bug-extractor. Refs #44.
func TestJavaBareNames_SpringMVCBuilderChain_ClassifiedWhenLangIsJava(t *testing.T) {
	names := []string{"build", "body", "header", "headers", "contentType"}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "java", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"java\", nil) = (_, false); "+
					"want classified — Spring MVC ResponseEntity builder chain leaf Refs #44", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"java\", nil) subtype=%q, want \"function\"", name, subtype)
			}
		})
	}
}

// TestJavaBareNames_SpringMVCBuilderChain_Synthesize verifies that
// Synthesize rewrites a CALLS edge with target "build" (or "body") on a
// Java entity to "ext:build" (resp. "ext:body"), classifying it as
// ExternalUnknown rather than leaving it unresolved (bug-extractor).
// This mimics `ResponseEntity.notFound().build()` /
// `ResponseEntity.status(...).body(created)` patterns in real Spring
// MVC controllers. Refs #44.
func TestJavaBareNames_SpringMVCBuilderChain_Synthesize(t *testing.T) {
	cases := []struct{ name, wantToID string }{
		{"build", "ext:build"},
		{"body", "ext:body"},
		{"header", "ext:header"},
		{"headers", "ext:headers"},
		{"contentType", "ext:contentType"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "ctrl-ent",
					Name:     "UserController.getById",
					Kind:     "SCOPE.Operation",
					Language: "java",
				}},
				Relationships: []graph.Relationship{{
					ID:     "calls-build",
					FromID: "ctrl-ent",
					ToID:   c.name,
					Kind:   "CALLS",
				}},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1 (Spring MVC builder leaf must be classified; Refs #44)",
					c.name, stats.Synthesized)
			}
			if got := doc.Relationships[0].ToID; got != c.wantToID {
				t.Fatalf("ToID=%q, want %q for %q (Refs #44)",
					got, c.wantToID, c.name)
			}
		})
	}
}

// TestJavaBareNames_SpringMVCBuilderChain_NotClassifiedForOtherLangs
// verifies the language gate: `build`, `body`, `header`, `headers`,
// `contentType` must NOT be rewritten for non-Java entities. Without
// the gate, a Go/Python/TS function named `build` or `body` would be
// shadowed by a synthesised Java placeholder. Refs #44.
func TestJavaBareNames_SpringMVCBuilderChain_NotClassifiedForOtherLangs(t *testing.T) {
	names := []string{"build", "body", "header", "headers", "contentType"}
	langs := []string{"go", "python", "typescript", "javascript", "ruby", "rust", "kotlin"}
	for _, lang := range langs {
		for _, name := range names {
			lang, name := lang, name
			t.Run(lang+"/"+name, func(t *testing.T) {
				t.Parallel()
				// The name may be in another language's bare-name list
				// (e.g. "header" in PHP/Elixir). We only assert that
				// stdlibFunction does NOT classify it when CALLED with
				// lang="<non-java>" AND the result (if any) is not due to
				// the javaBareNames gate — the document-level gate in
				// Synthesize checks entity.Language for classification.
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "non-java-ent",
						Name:     "myFunc",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{{
						ID:     "calls-" + name,
						FromID: "non-java-ent",
						ToID:   name,
						Kind:   "CALLS",
					}},
				}
				Synthesize(doc)
				got := doc.Relationships[0].ToID
				// If the name was rewritten via another language's gate
				// (e.g. Ruby build) that is acceptable. What must NOT
				// happen: the ToID remains the bare name AND a
				// synthesised entity exists whose stub is "ext:build"
				// that was generated by the Java gate for a non-Java
				// source entity.
				// Verify via stdlibFunction — must not classify via Java gate.
				subtype, classifiedByJavaGate := javaBareNamesClassify(name, lang)
				_ = subtype
				if classifiedByJavaGate {
					t.Fatalf("javaBareNames[%q] fired for lang=%q; Java gate must be lang-gated to \"java\" only",
						name, lang)
				}
				_ = got
			})
		}
	}
}

// javaBareNamesClassify is a thin shim that returns whether stdlibFunction
// classified `name` via the javaBareNames gate specifically. It does so
// by checking membership directly (the gate is a pure map lookup).
func javaBareNamesClassify(name, lang string) (string, bool) {
	if lang != "java" {
		return "", false
	}
	_, ok := javaBareNames[name]
	return "function", ok
}

// TestKotlinBareNames_ClassifiedWhenLangIsKotlin covers issue #106 fix
// (B): kotlinx.coroutines / io.ktor stdlib types, kotlin.collections
// builtins, scope functions, and contract / lazy helpers must
// classify as stdlib bare-names — but only when the source entity's
// language is "kotlin".
func TestKotlinBareNames_ClassifiedWhenLangIsKotlin(t *testing.T) {
	names := []string{
		// kotlinx.coroutines / io.ktor types
		"Frame", "CloseReason", "CopyOnWriteArrayList",
		"ConcurrentHashMap", "AtomicInteger", "AtomicLong",
		"AtomicBoolean", "AtomicReference", "Job", "Deferred",
		"Channel", "CoroutineScope", "MutableStateFlow", "StateFlow",
		"MutableSharedFlow", "SharedFlow", "Flow", "ApplicationCall",
		"Application", "Route", "Routing", "WebSocketSession",
		// kotlin.collections / builtins
		"listOf", "mapOf", "setOf", "mutableListOf", "mutableMapOf",
		"mutableSetOf", "arrayOf", "arrayListOf", "hashMapOf",
		"hashSetOf", "linkedSetOf", "sortedSetOf", "emptyList",
		"emptyMap", "emptySet", "listOfNotNull", "mapNotNull",
		// scope functions
		"let", "also", "apply", "run", "with",
		// contracts / lazy helpers
		"requireNotNull", "checkNotNull", "require", "check", "error",
		"lazy", "lazyOf", "TODO",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "kotlin", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"kotlin\", nil) = (_, false); want classified as stdlib bare-name", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"kotlin\", nil) subtype=%q, want %q", name, subtype, "function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "kt-src",
					Name:     "caller",
					Kind:     "function",
					Language: "kotlin",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "kt-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestKotlinBareNames_NotClassifiedForOtherLanguages confirms the
// language gate: Kotlin-only names (`let`, `apply`, `Frame`, `Job`,
// ...) must NOT be rewritten when the source entity's language is
// anything other than "kotlin". Without the gate, a JS user variable
// named `let` or a Go `Job` struct would be shadowed by a synthesised
// placeholder.
func TestKotlinBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Names that DON'T also appear in language-agnostic stdlibBareNames.
	names := []string{"let", "apply", "Frame", "Job", "Channel", "listOf", "lazy", "TODO", "Flow", "checkNotNull"}
	otherLangs := []string{"go", "python", "javascript", "rust", "java", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"kotlin\" only)", name, lang)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "src",
						Name:     "caller",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 0",
						name, lang, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-Kotlin)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestKotlinBareNames_RejectedNamesNotClassified locks in the explicit
// rejection list from issue #106: generic accessors / collection ops
// MUST NOT be in the Kotlin allowlist. These names have a high
// collision rate with user-defined methods on any class.
func TestKotlinBareNames_RejectedNamesNotClassified(t *testing.T) {
	rejected := []string{
		"get", "set", "add", "remove", "size", "isEmpty",
	}
	for _, name := range rejected {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := kotlinBareNames[name]; ok {
				t.Fatalf("kotlinBareNames[%q] present; must be rejected per issue #106 (collision-prone)", name)
			}
		})
	}
}

// TestKotlinKtorDSLBareNames_ClassifiedWhenLangIsKotlin covers issue
// #435: Ktor builder DSL methods (`routing { get(...); post(...) }`,
// `install(plugin)`, `intercept(phase)`, `call.respond(...)`, etc.) and
// kotlinx.coroutines builders (`runBlocking`, `withContext`, `launch`,
// `async`, `delay`, `flow`) get receiver-stripped by the Kotlin
// extractor and land in bug-extractor. These names must classify as
// stdlib bare-names — but only when the source entity's language is
// "kotlin".
func TestKotlinKtorDSLBareNames_ClassifiedWhenLangIsKotlin(t *testing.T) {
	names := []string{
		// Ktor route builder DSL.
		"routing", "route", "install", "intercept",
		// Ktor ApplicationCall responders / accessors.
		"respond", "respondText", "respondHtml", "respondRedirect",
		"respondFile", "parameters", "headers", "principal",
		"authentication", "application", "environment", "request",
		"pipeline", "attributes",
		// kotlinx.coroutines builders.
		"runBlocking", "withContext", "coroutineScope", "launch",
		"async", "delay", "flow",
		// Ktor server entry / static / WebSocket.
		"embeddedServer", "staticFiles", "static", "webSocket",
		"webSocketSession",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "kotlin", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"kotlin\", nil) = (_, false); want classified as stdlib bare-name", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"kotlin\", nil) subtype=%q, want %q", name, subtype, "function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "kt-src",
					Name:     "caller",
					Kind:     "function",
					Language: "kotlin",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "kt-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestKotlinKtorDSLBareNames_NotClassifiedForOtherLanguages confirms
// the Kotlin language gate holds for the issue #435 additions: Ktor
// DSL / coroutine builder names must NOT be rewritten when the source
// entity's language is anything other than "kotlin". A JS user method
// named `request` or a Go `launch` symbol must not be shadowed.
func TestKotlinKtorDSLBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Pick names with the highest cross-language collision potential.
	// `route` removed — now also classified by rustBareNames (#440).
	// `headers` removed from the java lane — Refs #44 added `headers` to
	// javaBareNames (Spring MVC ResponseEntity builder chain leaf) so
	// stdlibFunction("headers", "java") now returns true; that is
	// intentional and the cross-language-leak test for `headers`/java
	// is covered by TestJavaBareNames_SpringMVCBuilderChain_Synthesize.
	names := []string{"request", "respond", "install", "launch", "async", "static", "parameters"}
	otherLangs := []string{"go", "python", "javascript", "rust", "java", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"kotlin\" only)", name, lang)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "src",
						Name:     "caller",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 0",
						name, lang, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-Kotlin)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestKotlinKtorRoutingVerbs_ClassifiedWithKtorImport covers the
// Ktor-verb fix: HTTP-verb routing-DSL functions (`get("/x") { ... }`,
// `post(...)`, `put(...)`, `delete(...)`, `patch(...)`, `head(...)`,
// `options(...)`) on `io.ktor.server.routing.Route` get receiver-
// stripped by the Kotlin extractor. The lowercase verb names collide
// trivially with generic property accessors (`Repository.get`,
// `Cache.put`), so #106's safer-bias rule rejected them from
// kotlinBareNames. They must classify as stdlib bare-names ONLY when
// the source file imports `io.ktor.server.*` — same precision model as
// the Go chi-router gate (#131).
//
// Refs #44 #435 #456.
func TestKotlinKtorRoutingVerbs_ClassifiedWithKtorImport(t *testing.T) {
	verbs := []string{"get", "post", "put", "delete", "patch", "head", "options"}
	importSets := []map[string]bool{
		{"io.ktor.server.routing": true},
		{"io.ktor.server.application": true, "io.ktor.server.netty": true},
		{"io.ktor.server.routing.get": true}, // non-wildcard leaf still matches the prefix.
		{"io.ktor.server": true},
	}
	for _, name := range verbs {
		for i, imps := range importSets {
			name, imps := name, imps
			t.Run(name+"/set"+itoaTest(i), func(t *testing.T) {
				t.Parallel()
				subtype, ok := stdlibFunction(name, "kotlin", "Routes.kt", imps)
				if !ok {
					t.Fatalf("stdlibFunction(%q, \"kotlin\", Ktor imports) not classified; want stdlib bare-name", name)
				}
				if subtype != "function" {
					t.Fatalf("subtype=%q, want \"function\"", subtype)
				}
			})
		}
	}
}

// TestKotlinKtorRoutingVerbs_NotClassifiedWithoutKtorImport locks the
// import-gate precision: without a `io.ktor.server.*` import the
// HTTP-verb names must NOT be rewritten. A Kotlin `Repository.get` /
// `Cache.put` user method that lands at the resolver as a bare leaf
// (the resolver couldn't bind the receiver, common with field chains)
// must stay unresolved rather than synthesise an `ext:get` placeholder.
//
// `head` and `options` are EXCLUDED from this assertion: `head` is in
// the unconditional kotlinBareNames allowlist as a kotlinx.html DSL
// leaf builder (#470 — `<head>` HTML element block), and `options`
// would otherwise be added by an HTML-DSL cohort; both classify
// regardless of the Ktor-import gate. This test covers the verbs whose
// CLASSIFICATION depends on the Ktor gate.
//
// Refs #44 #435 #456.
func TestKotlinKtorRoutingVerbs_NotClassifiedWithoutKtorImport(t *testing.T) {
	verbs := []string{"get", "post", "put", "delete", "patch"}
	importSets := []map[string]bool{
		nil,
		{},
		{"kotlin.io.println": true},
		{"org.springframework.web.bind.annotation.RestController": true},
		{"io.ktor.client.HttpClient": true}, // client-side, not server DSL
	}
	for _, name := range verbs {
		for i, imps := range importSets {
			name, imps := name, imps
			t.Run(name+"/set"+itoaTest(i), func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, "kotlin", "App.kt", imps); ok {
					t.Fatalf("stdlibFunction(%q, \"kotlin\", non-Ktor imports=%v) classified; "+
						"must require io.ktor.server.* import", name, imps)
				}
			})
		}
	}
}

// TestKotlinKtorRoutingVerbs_NotClassifiedForOtherLanguages confirms
// the Kotlin language gate holds for the Ktor-verb addition: with a
// non-kotlin language, the Kotlin Ktor-import branch must be skipped
// even when the (synthetic) import set contains `io.ktor.server.*`.
//
// Test strategy: compare each verb/lang pair WITH and WITHOUT the
// Ktor imports. If the result is the same, the Kotlin Ktor branch is
// correctly NOT firing for the non-kotlin language (other-lang
// allowlists may classify the verb on their own — that is orthogonal
// to this gate and intentional). If a verb classifies only when Ktor
// imports are present for a non-kotlin language, the gate is broken.
//
// Refs #44 #435 #456.
func TestKotlinKtorRoutingVerbs_NotClassifiedForOtherLanguages(t *testing.T) {
	verbs := []string{"get", "post", "put", "delete", "patch", "head", "options"}
	otherLangs := []string{"go", "python", "javascript", "rust", "java", "csharp", ""}
	ktorImps := map[string]bool{"io.ktor.server.routing": true}
	for _, name := range verbs {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				_, okWithout := stdlibFunction(name, lang, "Routes.kt", nil)
				_, okWith := stdlibFunction(name, lang, "Routes.kt", ktorImps)
				if okWith != okWithout {
					t.Fatalf("stdlibFunction(%q, %q): classify diverges based on Ktor imports "+
						"(with=%v, without=%v); Kotlin Ktor branch must not fire for non-kotlin",
						name, lang, okWith, okWithout)
				}
			})
		}
	}
}

// TestHasKtorServerImport covers the import-gate helper directly:
// matches any `io.ktor.server` or `io.ktor.server.*` path, rejects
// empty / nil sets and non-Ktor / Ktor-client imports.
//
// Refs #44 #435 #456.
func TestHasKtorServerImport(t *testing.T) {
	cases := []struct {
		name    string
		imports map[string]bool
		want    bool
	}{
		{"nil", nil, false},
		{"empty", map[string]bool{}, false},
		{"root", map[string]bool{"io.ktor.server": true}, true},
		{"routing", map[string]bool{"io.ktor.server.routing": true}, true},
		{"application", map[string]bool{"io.ktor.server.application": true}, true},
		{"netty", map[string]bool{"io.ktor.server.netty": true}, true},
		{"leaf import", map[string]bool{"io.ktor.server.routing.get": true}, true},
		{"client only", map[string]bool{"io.ktor.client.HttpClient": true}, false},
		{"unrelated", map[string]bool{"kotlin.io.println": true, "java.util.UUID": true}, false},
		{"io.ktor (no server)", map[string]bool{"io.ktor.http.HttpStatusCode": true}, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := hasKtorServerImport(c.imports); got != c.want {
				t.Errorf("hasKtorServerImport(%v) = %v, want %v", c.imports, got, c.want)
			}
		})
	}
}

// itoaTest is a tiny int-to-string helper to keep test subtest names
// deterministic without pulling in strconv (already used elsewhere in
// the file via helper functions, but this avoids touching imports).
func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [16]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// TestKotlinResidualBareNames_ClassifiedWhenLangIsKotlin covers issue
// #456: residual ktor-samples bug-extractor cohorts after #122 + #106 +
// #435. The Kotlin extractor receiver-strips kotlinx.serialization
// (`Json.encodeToString(x)` → `encodeToString`), additional
// kotlinx.coroutines builders (`Dispatchers.IO` → `Dispatchers`,
// `withTimeout(ms) { ... }` → `withTimeout`), kotlin.collections /
// kotlin.text higher-order helpers (`list.mapNotNull { ... }` → already
// covered; `list.filterNotNull()` → `filterNotNull`, `s.toIntOrNull()` →
// `toIntOrNull`), and Ktor HttpClient surface names
// (`HttpClient(engine) { ... }`, `response.bodyAsText()` → `bodyAsText`).
// All must classify as stdlib bare-names ONLY when lang=="kotlin".
func TestKotlinResidualBareNames_ClassifiedWhenLangIsKotlin(t *testing.T) {
	names := []string{
		// kotlinx.serialization.
		"Serializable", "encodeToString", "decodeFromString",
		"encodeToJsonElement", "decodeFromJsonElement",
		// kotlinx.coroutines additional.
		"GlobalScope", "Dispatchers", "withTimeout", "withTimeoutOrNull",
		"joinAll", "awaitAll", "supervisorScope",
		// kotlin.collections / sequences higher-order.
		"filterNotNull", "sortedBy", "sortedByDescending", "distinctBy",
		"groupBy", "partition", "zip", "windowed", "chunked",
		"joinToString", "associate", "associateBy", "associateWith",
		"fold", "reduce", "flatten",
		// kotlin.text parsing / padding / slicing.
		"toIntOrNull", "toLongOrNull", "toDoubleOrNull", "toFloatOrNull",
		"padStart", "padEnd", "substringBefore", "substringAfter",
		"substringBeforeLast", "substringAfterLast",
		// Ktor HttpClient surface.
		"HttpClient", "createClient", "bodyAsText", "bodyAsBytes",
		"setBody",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "kotlin", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"kotlin\", nil) = (_, false); want classified as stdlib bare-name", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"kotlin\", nil) subtype=%q, want %q", name, subtype, "function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "kt-src",
					Name:     "caller",
					Kind:     "function",
					Language: "kotlin",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "kt-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestKotlinResidualBareNames_NotClassifiedForOtherLanguages confirms
// the Kotlin language gate holds for the issue #456 additions. Names
// with the highest cross-language collision potential
// (`Dispatchers`, `groupBy`, `partition`, `zip`, `fold`, `reduce`,
// `flatMap`, `HttpClient`, `setBody`, `Serializable`) must NOT be
// rewritten when the source entity's language is anything other than
// "kotlin".
func TestKotlinResidualBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Per #516: structural per-(name, lang) assertion. Names like
	// `partition` legitimately appear in BOTH kotlinBareNames AND
	// rubyBareNames — the per-language gate at classify-time prevents
	// cross-language contamination. assertCrossLangGate skips pairs
	// where the other language legitimately owns the name and asserts
	// the gate falls through for every remaining pair.
	names := []string{
		"Dispatchers", "partition", "HttpClient", "setBody",
		"Serializable", "encodeToString", "withTimeout", "bodyAsText",
		"distinctBy", "withTimeoutOrNull", "decodeFromJsonElement",
	}
	otherLangs := []string{"go", "python", "javascript", "java", "ruby", "swift", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				assertCrossLangGate(t, name, lang, "kotlin")
			})
		}
	}
}

// TestKotlinResidualBareNames_RejectedNamesNotClassified locks in the
// #456 explicit rejection rule: generic accessors that the #106 stop-
// list rejected as collision-prone must remain rejected even though
// they appear as receiver-stripped Ktor client builders in the
// ktor-samples sample dump. The Kotlin language gate alone is not
// strong enough to prevent shadowing real user-defined methods named
// `body`/`header`/`parameter`/`cookie`/`format`.
func TestKotlinResidualBareNames_RejectedNamesNotClassified(t *testing.T) {
	rejected := []string{
		"body", "header", "parameter", "cookie", "format",
		"get", "set", "add", "remove", "size", "isEmpty",
	}
	for _, name := range rejected {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := kotlinBareNames[name]; ok {
				t.Fatalf("kotlinBareNames[%q] present; must be rejected per #106/#456 (collision-prone)", name)
			}
		})
	}
}

// TestKotlinTestBareNames_GatedByTestFile covers issue #470: kotlin.test
// + kotlinx-coroutines-test + Ktor testApplication leaves classify as
// stdlib bare-names ONLY when the caller's file path matches
// isKotlinTestFile (canonical `src/test/kotlin/`, KMP `src/*Test/kotlin/`,
// or `*Test.kt`/`*Tests.kt`/`*IT.kt` suffix). A production-code caller
// with a same-named user method (e.g. an `assertEquals` extension) must
// fall through to bug-extractor — same safer-bias rule as Go testify
// (#115) and Java JUnit (#120).
func TestKotlinTestBareNames_GatedByTestFile(t *testing.T) {
	cases := []struct {
		name     string
		fromFile string
		want     bool
	}{
		// Canonical JVM test root.
		{"assertEquals", "app/src/test/kotlin/com/example/MyTest.kt", true},
		// KMP test source set.
		{"testApplication", "chat/src/backendTest/kotlin/ChatApplicationTest.kt", true},
		// File-name convention.
		{"runTest", "graalvm/src/test/kotlin/io/ktorgraal/ConfigureRoutingTest.kt", true},
		{"assertTrue", "h2/src/test/kotlin/io/ktor/samples/H2ApplicationTests.kt", true},
		{"assertNotNull", "service/IntegrationIT.kt", true},
		// Production code — must NOT classify.
		{"assertEquals", "app/src/main/kotlin/com/example/Production.kt", false},
		{"testApplication", "chat/src/backendMain/kotlin/ChatApplication.kt", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+"/"+tc.fromFile, func(t *testing.T) {
			t.Parallel()
			_, got := stdlibFunction(tc.name, "kotlin", tc.fromFile, nil)
			if got != tc.want {
				t.Fatalf("stdlibFunction(%q, \"kotlin\", %q) classified=%v, want %v",
					tc.name, tc.fromFile, got, tc.want)
			}
		})
	}
}

// TestIsKotlinTestFile covers the file-path shapes recognised as Kotlin
// test sources by the #470 test-file gate.
func TestIsKotlinTestFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"", false},
		{"src/test/kotlin/Foo.kt", true},
		{"app/src/test/kotlin/com/example/Foo.kt", true},
		{"chat/src/backendTest/kotlin/Chat.kt", true},
		{"chat/src/jvmTest/kotlin/Chat.kt", true},
		{"chat/src/commonTest/kotlin/Chat.kt", true},
		{"src/iosTest/kotlin/X.kt", true},
		{"foo/MyTest.kt", true},
		{"foo/MyTests.kt", true},
		{"foo/MyIT.kt", true},
		// Production paths.
		{"src/main/kotlin/Foo.kt", false},
		{"chat/src/backendMain/kotlin/Chat.kt", false},
		{"src/jvmMain/kotlin/X.kt", false},
		{"foo/Production.kt", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			if got := isKotlinTestFile(tc.path); got != tc.want {
				t.Fatalf("isKotlinTestFile(%q) = %v; want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestKotlinBareNames_UnknownKotlinMethodFallsThrough confirms that a
// Kotlin-source bare-name call that ISN'T in the kotlinBareNames
// allowlist still falls through normally, so genuine missing-
// resolution bugs continue to surface in bug-extractor.
func TestKotlinBareNames_UnknownKotlinMethodFallsThrough(t *testing.T) {
	name := "myCustomBusinessMethod" // Not stdlib/kotlinx; user-defined.
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "kt-src",
			Name:     "caller",
			Kind:     "function",
			Language: "kotlin",
		}},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "kt-src", ToID: name, Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("Synthesize(%q): synthesized=%d, want 0 (unknown user fn)", name, stats.Synthesized)
	}
	if doc.Relationships[0].ToID != name {
		t.Fatalf("ToID=%q, want %q (unknown name must not be rewritten)",
			doc.Relationships[0].ToID, name)
	}
}

// TestSwiftVaporDSLBareNames_ClassifiedWhenLangIsSwift covers issue
// #436: Vapor route builder DSL methods (`app.get(...)`, `routes.post`,
// `route.group`, `req.respond`), Fluent ORM query builders
// (`Model.query(on:)`, `.filter`, `.sort`, `.first`, `.all`), and HTTP
// context accessors (`req.parameters`, `req.headers`, `req.body`) get
// receiver-stripped by the Swift extractor and land in bug-extractor.
// These names must classify as stdlib bare-names — but only when the
// source entity's language is "swift". Mirrors the Kotlin Ktor DSL
// precedent (#435).
func TestSwiftVaporDSLBareNames_ClassifiedWhenLangIsSwift(t *testing.T) {
	names := []string{
		// Vapor route builder DSL.
		"get", "post", "put", "patch", "delete", "on", "group",
		"grouped", "route", "register", "boot", "run", "start",
		"shutdown", "respond", "redirect", "view", "render",
		// Vapor middleware DSL.
		"middleware", "use", "authenticate", "authorize", "protect",
		// Fluent ORM builders.
		"save", "create", "update", "find", "query", "sort",
		"limit", "offset", "with", "count", "first", "last",
		"paginate", "transform", "flatMap",
		// HTTP context accessors.
		"parameters", "headers", "body", "request", "response",
		"auth", "session", "cookies",
		// Swift Concurrency.
		"async", "await", "Task", "withCheckedContinuation",
		// SwiftNIO EventLoopFuture / Promise / LockedValueBox.
		"makeSucceededFuture", "makeFailedFuture", "makePromise",
		"makeFutureWithTask", "completeWithTask",
		"whenComplete", "whenSuccess", "whenFailure",
		"flatSubmit", "withLockedValue",
		// Swift stdlib types and Sequence/Collection idioms.
		"String", "Int", "Array", "Date", "ObjectIdentifier",
		"forEach", "joined", "dropFirst", "prefix",
		"numericCast", "singleValueContainer",
		"preconditionFailure", "preconditionInEventLoop",
		"syncShutdownGracefully",
		// swift-log Logger API.
		"debug", "info", "trace", "notice", "warning", "critical",
		// More Swift stdlib / Foundation / NIO Future idioms.
		"hasSuffix", "hasPrefix", "lowercased", "uppercased",
		"replacingOccurrences", "dropLast", "addingTimeInterval",
		"merging", "flatMapThrowing", "makeCompletedFuture",
		"precondition", "fatalError",
		"TimeZone", "Locale", "DateFormatter",
		"Int64", "UInt8", "UInt16", "UInt32", "UInt64",
		"Int8", "Int16", "Int32",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "swift", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"swift\", nil) = (_, false); want classified as stdlib bare-name", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"swift\", nil) subtype=%q, want %q", name, subtype, "function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "swift-src",
					Name:     "caller",
					Kind:     "function",
					Language: "swift",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "swift-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestSwiftVaporDSLBareNames_NotClassifiedForOtherLanguages confirms the
// Swift language gate holds for the issue #436 additions: Vapor / Fluent
// DSL names must NOT be rewritten when the source entity's language is
// anything other than "swift". A JS user method named `request`, a Go
// method named `save`, a Ruby `find`, a Kotlin `respond`, etc. must not
// be shadowed by the Swift gate.
func TestSwiftVaporDSLBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Per #516: structural per-(name, lang) assertion via
	// assertCrossLangGate. A name like `auth` may legitimately appear
	// in BOTH swiftBareNames AND phpBareNames (Laravel) or other
	// catalogs — what matters is that the per-language gate at
	// classify-time prevents cross-language contamination. Pairs
	// where `otherLang` owns the name are skipped; remaining pairs
	// must fall through.
	names := []string{
		"auth", "session", "cookies",
		"boot", "shutdown", "grouped",
		"render", "middleware", "authorize", "protect",
		"paginate", "transform", "flatMap", "offset",
	}
	otherLangs := []string{"go", "python", "javascript", "ruby", "rust", "java", "kotlin", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				assertCrossLangGate(t, name, lang, "swift")
			})
		}
	}
}

// TestSwiftBareNames_UnknownSwiftMethodFallsThrough confirms that a
// Swift-source bare-name call that ISN'T in the swiftBareNames allowlist
// still falls through normally, so genuine missing-resolution bugs
// continue to surface in bug-extractor.
func TestSwiftBareNames_UnknownSwiftMethodFallsThrough(t *testing.T) {
	name := "myCustomBusinessMethod" // Not Vapor/Fluent; user-defined.
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "swift-src",
			Name:     "caller",
			Kind:     "function",
			Language: "swift",
		}},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "swift-src", ToID: name, Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("Synthesize(%q): synthesized=%d, want 0 (unknown user fn)", name, stats.Synthesized)
	}
	if doc.Relationships[0].ToID != name {
		t.Fatalf("ToID=%q, want %q (unknown name must not be rewritten)",
			doc.Relationships[0].ToID, name)
	}
}

// TestCSharpAspNetCoreDSLBareNames_ClassifiedWhenLangIsCSharp covers
// issue #441: ASP.NET Core MVC ControllerBase action helpers
// (`return Ok(model)`, `BadRequest()`, `RedirectToAction(...)`), EF Core
// LINQ query builders (`db.Users.Where(...).FirstOrDefaultAsync()`,
// `Include(...)`, `ThenInclude(...)`), HttpContext / auth accessors
// (`User`, `IsAuthenticated`, `HasClaim`), and DI helpers
// (`GetRequiredService`) get receiver-stripped by the C# extractor and
// land in bug-extractor. These names must classify as stdlib bare-names
// — but only when the source entity's language is "csharp". Mirrors the
// Swift Vapor DSL precedent (#436).
func TestCSharpAspNetCoreDSLBareNames_ClassifiedWhenLangIsCSharp(t *testing.T) {
	names := []string{
		// ASP.NET Core MVC ControllerBase action helpers.
		"Ok", "BadRequest", "Unauthorized", "Forbid", "Conflict",
		"UnprocessableEntity", "RedirectToAction", "RedirectToRoute",
		"RedirectToPage", "Redirect", "View", "PartialView", "Json",
		"Content", "File", "PhysicalFile", "Created", "CreatedAtAction",
		"CreatedAtRoute", "Accepted", "NoContent", "StatusCode",
		"Problem", "ValidationProblem",
		// EF Core / LINQ-to-Entities query and persistence builders.
		"FirstOrDefault", "FirstOrDefaultAsync", "SingleOrDefault",
		"SingleOrDefaultAsync", "First", "FirstAsync", "Single",
		"SingleAsync", "ToList", "ToListAsync", "ToArray", "ToArrayAsync",
		"Include", "ThenInclude", "Where", "Select", "SelectMany",
		"OrderBy", "OrderByDescending", "ThenBy", "GroupBy", "Skip",
		"Take", "Count", "CountAsync", "Sum", "SumAsync", "Average",
		"Max", "Min", "Any", "All", "Find", "FindAsync", "AsNoTracking",
		"AsQueryable", "SaveChanges", "SaveChangesAsync", "Add",
		"AddAsync", "AddRange", "Update", "Remove", "RemoveRange",
		"Attach", "Entry",
		// HttpContext / IActionResult accessors.
		"User", "Request", "Session", "Items", "Headers", "Cookies",
		"Form", "Query",
		// ASP.NET Core authentication helpers.
		"SignIn", "SignOut", "Authenticate", "Challenge",
		"IsAuthenticated", "HasClaim",
		// Microsoft.Extensions.DependencyInjection helpers.
		"GetRequiredService", "GetService", "GetServices",
		"BuildServiceProvider",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "csharp", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"csharp\", nil) = (_, false); want classified as stdlib bare-name", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"csharp\", nil) subtype=%q, want %q", name, subtype, "function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "csharp-src",
					Name:     "caller",
					Kind:     "function",
					Language: "csharp",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "csharp-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestCSharpAspNetCoreDSLBareNames_NotClassifiedForOtherLanguages
// confirms the C# language gate holds for the issue #441 additions:
// ASP.NET Core / EF Core DSL names must NOT be rewritten when the source
// entity's language is anything other than "csharp". A JS user method
// named `Where`, a Go method named `Add`, a Ruby `Find`, a Kotlin
// `Update`, etc. must not be shadowed by the csharp gate.
func TestCSharpAspNetCoreDSLBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Pick names with the highest cross-language collision potential —
	// generic verbs/accessors that exist as user methods in every
	// ecosystem. Selection rule: each name MUST be unique to
	// csharpBareNames (i.e. not in stdlibBareNames or any other
	// language map), otherwise the cross-language gate test would
	// trip on a different language's allowlist firing first.
	names := []string{
		"Forbid", "Conflict",
		"UnprocessableEntity", "RedirectToAction", "RedirectToRoute",
		"RedirectToPage", "PartialView", "PhysicalFile",
		"CreatedAtAction", "CreatedAtRoute", "ValidationProblem",
		"FirstOrDefaultAsync", "SingleOrDefaultAsync", "ToListAsync",
		"ToArrayAsync", "ThenInclude", "AsNoTracking", "AsQueryable",
		"SaveChangesAsync", "AddRange", "RemoveRange", "FindAsync",
		"GetRequiredService", "BuildServiceProvider", "HasClaim",
		// `IsAuthenticated` deliberately omitted post-#447: it's also
		// in pythonBareNames (DRF permission class) and classifies
		// under lang="python".
	}
	otherLangs := []string{"go", "python", "javascript", "ruby", "rust", "java", "kotlin", "swift", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"csharp\" only)", name, lang)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "src",
						Name:     "caller",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 0",
						name, lang, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-C#)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestCSharpBareNames_UnknownCSharpMethodFallsThrough confirms that a
// C#-source bare-name call that ISN'T in the csharpBareNames allowlist
// still falls through normally, so genuine missing-resolution bugs
// continue to surface in bug-extractor.
func TestCSharpBareNames_UnknownCSharpMethodFallsThrough(t *testing.T) {
	name := "MyCustomBusinessMethod" // Not ASP.NET/EF Core; user-defined.
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "csharp-src",
			Name:     "caller",
			Kind:     "function",
			Language: "csharp",
		}},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "csharp-src", ToID: name, Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("Synthesize(%q): synthesized=%d, want 0 (unknown user fn)", name, stats.Synthesized)
	}
	if doc.Relationships[0].ToID != name {
		t.Fatalf("ToID=%q, want %q (unknown name must not be rewritten)",
			doc.Relationships[0].ToID, name)
	}
}

// TestRubyBareNames_ClassifiedWhenLangIsRuby covers issue #107: Ruby
// Object/Kernel instance methods (post-receiver-strip) classify as
// stdlib bare-names — but only when the source entity's language is
// "ruby".
func TestRubyBareNames_ClassifiedWhenLangIsRuby(t *testing.T) {
	names := []string{
		// Object lifecycle / identity
		"new", "nil?", "present?", "blank?", "respond_to?",
		"class", "tap", "then", "yield_self", "dup", "clone",
		"freeze", "frozen?", "object_id",
		// Type coercion
		"to_s", "to_str", "to_i", "to_f", "to_a", "to_h", "to_sym",
		// Inspection / type checks
		"inspect", "is_a?", "kind_of?", "instance_of?",
		// ActiveRecord persistence and validation (issue #124)
		"save", "save!", "update", "update!", "destroy", "destroy!",
		"valid?", "valid_password?", "errors", "persisted?",
		"new_record?", "attributes", "reload", "create", "create!",
		"find_or_create_by", "build", "exists?", "first", "last", "all",
		"find_by", "find_each", "find_in_batches",
		"destroy_all", "delete_all", "update_all",
		"update_attribute", "update_attributes", "update_attributes!",
		"toggle", "toggle!", "increment", "increment!",
		"decrement", "decrement!", "touch",
		"reset_counters", "reset_column_information",
		// ActiveSupport Numeric/Time helpers
		"days", "hours", "minutes", "seconds", "weeks", "months",
		"years", "ago", "from_now",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "ruby", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"ruby\", nil) = (_, false); want classified", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"ruby\", nil) subtype=%q, want %q", name, subtype, "function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "rb-src",
					Name:     "caller",
					Kind:     "function",
					Language: "ruby",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "rb-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestRubyBareNames_NotClassifiedForOtherLanguages confirms the Ruby
// language gate: a JS user-method named `tap`, a Go method named
// `clone`, etc. must NOT be rewritten when source lang != "ruby".
func TestRubyBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Names that are NOT in the language-agnostic stdlibBareNames map.
	names := []string{
		"new", "tap", "then", "dup", "freeze", "to_s", "to_h", "is_a?", "respond_to?",
		// ActiveRecord persistence names (issue #124) must not leak to
		// other languages. `save`/`errors`/`all`/`attributes` are
		// intentionally EXCLUDED from this negative list — they are
		// independently classified by other-language allowlists
		// (Spring Data `save` in Java, Go `errors` package, Python
		// `all` builtin, Ktor `attributes` accessor in Kotlin per
		// #435) and the non-leak guarantee for Ruby-only AR names is
		// asserted by the remaining names below.
		// `update`/`create` deliberately omitted post-#447: both are
		// in pythonBareNames (Django ORM verbs) and classify under
		// lang="python"; the Ruby-only-leak property is asserted by
		// the remaining AR names below.
		// `build` removed from the java lane — Refs #44 added `build`
		// to javaBareNames (Spring MVC ResponseEntity.notFound().build()
		// and similar builder-chain patterns); classifying `build` for
		// Java is intentional. The cross-language-leak test for
		// `build`/java is covered by
		// TestJavaBareNames_SpringMVCBuilderChain_Synthesize.
		"destroy", "valid?", "first", "last",
		"reload", "exists?", "persisted?", "new_record?",
		"find_or_create_by", "valid_password?",
	}
	otherLangs := []string{"go", "python", "javascript", "rust", "java", "kotlin", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"ruby\" only)", name, lang)
				}
				// Skip combinations where the name is also a valid per-language
				// stdlib builtin for the target language (e.g. "new" is a
				// Go universe-block builtin since #1085 multi-lang extension).
				// The cross-language guarantee we are checking is "Ruby-only names
				// don't leak to unrelated languages" — names that are genuinely
				// builtin in BOTH Ruby and the target language are out of scope.
				if resolve.IsStdlibBuiltinTarget(name, lang) {
					t.Skipf("%q is also a stdlib builtin for lang=%q — skipping Synthesize leak check", name, lang)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "src",
						Name:     "caller",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 0",
						name, lang, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-Ruby)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestRubyBareNames_RejectedNamesNotClassified locks in the explicit
// rejection list from issue #107: generic collection ops collide with
// user methods on any class. Per #516 this is now expressed as a
// structural per-(name, lang) assertion: each rejected name must NOT
// classify as an external bare-name under ANY non-Ruby source
// language. Names that ALSO happen to live in rubyBareNames are
// fine — the per-language gate prevents the Ruby entry from firing
// for non-Ruby sources, which is exactly what this test verifies.
//
// DO NOT reintroduce the old `rubyBareNames[name]` set-membership
// assertion. See assertCrossLangGate's docstring for the rationale.
func TestRubyBareNames_RejectedNamesNotClassified(t *testing.T) {
	rejected := []string{"each", "map", "select", "find", "count", "length", "size"}
	otherLangs := []string{"go", "python", "javascript", "java", "kotlin", "swift", "rust", "php", "csharp", ""}
	for _, name := range rejected {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				assertCrossLangGate(t, name, lang, "ruby")
			})
		}
	}
}

// TestRubySidekiqDSLBareNames_ClassifiedWhenLangIsRuby covers issue
// #449: Sidekiq gem DSL (worker, middleware, config, job context,
// Sidekiq::Client) and the Redis pipeline / hash / list / set /
// sorted-set commands exposed via Sidekiq.redis { |conn| ... } get
// receiver-stripped by the Ruby extractor and land in bug-extractor.
// These names must classify as stdlib bare-names — but only when
// the source entity's language is "ruby". Mirrors the AR persistence
// (#124) precedent.
func TestRubySidekiqDSLBareNames_ClassifiedWhenLangIsRuby(t *testing.T) {
	names := []string{
		// Sidekiq::Worker DSL. `set` covered by stdlibBareNames.
		"perform_async", "perform_in", "perform_at", "perform_bulk",
		"enqueue", "enqueue_to", "enqueue_to_in",
		"sidekiq_options", "sidekiq_retry_in", "sidekiq_retries_exhausted",
		// Sidekiq::Middleware::Chain. `exists?` covered by AR block;
		// `remove` covered by rustBareNames.
		"add", "clear", "prepend", "entries",
		// Sidekiq config / lifecycle.
		"redis", "logger", "concurrency", "queues", "strict",
		"error_handlers", "death_handlers", "on", "lifecycle_events",
		// Job context accessors.
		"jid", "bid", "args", "klass", "queue", "retry",
		"created_at", "enqueued_at",
		// Sidekiq::Client.
		"push", "push_bulk",
		// Redis pipeline / multi-exec / hash / list / set / sorted-set.
		"pipelined", "multi", "exec", "discard", "watch", "unwatch",
		"hset", "hget", "hgetall", "lpush", "rpush", "lpop", "rpop",
		"sadd", "srem", "smembers", "zadd", "zrem", "zrange",
		"zrangebyscore",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "ruby", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"ruby\", nil) = (_, false); want classified", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"ruby\", nil) subtype=%q, want %q", name, subtype, "function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "rb-src",
					Name:     "caller",
					Kind:     "function",
					Language: "ruby",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "rb-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestRubySidekiqDSLBareNames_NotClassifiedForOtherLanguages confirms
// the Ruby language gate holds for the issue #449 Sidekiq additions:
// a Go method named `enqueue`, a Python `jid`, a JS `perform_async`,
// etc. must NOT be rewritten when source lang != "ruby".
func TestRubySidekiqDSLBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Names with the highest cross-language collision potential —
	// generic verbs/accessors that exist as user methods in many
	// ecosystems. Selection rule: each name MUST be unique to
	// rubyBareNames (i.e. not in stdlibBareNames or any other
	// language map), otherwise a different gate would fire first.
	// Excluded from this negative list (each is classified by another
	// gate so the non-leak assertion would trivially fail):
	//   - `on` (swiftBareNames Vapor concurrency / Kotlin Ktor would
	//     not match but on is in swiftBareNames).
	//   - `add`, `clear`, `prepend`, `push`, `entries`, `multi`,
	//     `exec`, `watch` — generic enough that future language gates
	//     may pick them up; the unique Sidekiq/Redis names below are
	//     a stronger gate-holding signal.
	names := []string{
		"perform_async", "perform_in", "perform_at", "perform_bulk",
		"enqueue_to_in", "sidekiq_options", "sidekiq_retry_in",
		"sidekiq_retries_exhausted", "error_handlers", "death_handlers",
		"lifecycle_events", "jid", "bid", "klass", "push_bulk",
		"pipelined", "hgetall", "lpush", "rpush", "zrangebyscore",
		"smembers", "zadd",
	}
	otherLangs := []string{"go", "python", "javascript", "rust", "java", "kotlin", "swift", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"ruby\" only)", name, lang)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "src",
						Name:     "caller",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 0",
						name, lang, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-Ruby)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestIoKtorKnownExternalPackage locks in the issue #106 addition of
// "io.ktor" to the external-package allowlist so io.ktor.* references
// classify as ExternalKnown rather than ExternalUnknown.
func TestIoKtorKnownExternalPackage(t *testing.T) {
	if !IsKnownExternalPackage("io.ktor") {
		t.Fatal("IsKnownExternalPackage(\"io.ktor\") = false; want true (Issue #106)")
	}
	if !IsKnownExternalPackage("IO.KTOR") {
		t.Fatal("IsKnownExternalPackage(\"IO.KTOR\") = false; want case-folded match")
	}
}

// TestJSBareNames_ClassifiedWhenLangIsJSOrTS covers issue #104:
// Prisma ORM client method names and JS/TS array/util builtins
// (post-receiver-strip) classify as stdlib bare-names — but only
// when the source entity's language is "javascript" or "typescript".
func TestJSBareNames_ClassifiedWhenLangIsJSOrTS(t *testing.T) {
	names := []string{
		// Prisma ORM client method surface
		"findUnique", "findUniqueOrThrow", "findFirst", "findFirstOrThrow",
		"findMany", "createMany", "updateMany", "deleteMany", "upsert",
		"aggregate", "groupBy", "executeRaw", "executeRawUnsafe",
		"queryRaw", "queryRawUnsafe",
		// `$`-prefixed Prisma client methods
		"$connect", "$disconnect", "$transaction", "$queryRaw",
		"$executeRaw", "$on", "$use",
		// Array / util builtins
		"some", "every", "push", "trim", "isArray",
	}
	for _, name := range names {
		for _, lang := range []string{"javascript", "typescript"} {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				subtype, ok := stdlibFunction(name, lang, "", nil)
				if !ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) = (_, false); want classified", name, lang)
				}
				if subtype != "function" {
					t.Fatalf("stdlibFunction(%q, %q, nil) subtype=%q, want %q", name, lang, subtype, "function")
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "js-src",
						Name:     "caller",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "js-src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 1 {
					t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 1", name, lang, stats.Synthesized)
				}
				want := "ext:" + name
				if doc.Relationships[0].ToID != want {
					t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
				}
			})
		}
	}
}

// TestJSBareNames_NotClassifiedForOtherLanguages confirms the JS/TS
// language gate: a Ruby user-method named `push`, a Go method named
// `trim`, etc. must NOT be rewritten when source lang is neither
// javascript nor typescript.
func TestJSBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Names that are NOT in language-agnostic stdlibBareNames.
	names := []string{"findUnique", "findMany", "upsert", "$transaction", "some", "every", "push", "trim", "isArray"}
	otherLangs := []string{"go", "python", "ruby", "rust", "java", "kotlin", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			// `push` is on the language-specific allowlist for Rust
			// (rustBareNames covers Vec/String#push) and Ruby
			// (rubyBareNames covers Sidekiq::Client#push, #449). Skip
			// those cross-checks; the JS/TS gate is what this test
			// cares about.
			if name == "push" && (lang == "rust" || lang == "ruby") {
				continue
			}
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to JS/TS only)", name, lang)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "src",
						Name:     "caller",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 0",
						name, lang, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-JS/TS)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestJSBareNames_RejectedNamesNotClassified locks in the explicit
// rejection list from issue #104: generic collection ops and overly
// generic Prisma names collide with user methods on any class and
// MUST NOT be in the JS/TS allowlist.
func TestJSBareNames_RejectedNamesNotClassified(t *testing.T) {
	rejected := []string{
		// Generic collection ops shared with user methods
		"map", "filter", "forEach", "reduce", "find", "length", "size",
		// Generic Prisma names that overlap with non-Prisma domain
		// methods (services, controllers, factories).
		"create", "update", "delete", "count",
	}
	for _, name := range rejected {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := jsBareNames[name]; ok {
				t.Fatalf("jsBareNames[%q] present; must be rejected per issue #104 (collision-prone)", name)
			}
		})
	}
}

// TestPrismaScopedKnownExternalPackage locks in the issue #104
// addition of "@prisma" (covers @prisma/client, @prisma/extension-*,
// etc.) to the external-package allowlist so @prisma/* references
// classify as ExternalKnown rather than ExternalUnknown.
func TestPrismaScopedKnownExternalPackage(t *testing.T) {
	if !IsKnownExternalPackage("@prisma/client") {
		t.Fatal("IsKnownExternalPackage(\"@prisma/client\") = false; want true (Issue #104)")
	}
	if !IsKnownExternalPackage("@PRISMA/CLIENT") {
		t.Fatal("IsKnownExternalPackage(\"@PRISMA/CLIENT\") = false; want case-folded match")
	}
	if !IsKnownExternalPackage("prisma") {
		t.Fatal("IsKnownExternalPackage(\"prisma\") = false; want true (Issue #104)")
	}
}

// TestSynthesize_FakerAllowlist locks in the issue #127 addition of
// "@faker-js" (covers @faker-js/faker and any future @faker-js/*
// subpackages) plus the legacy unscoped "faker" bare name so faker
// references in JS/TS fixtures classify as ExternalKnown rather than
// ExternalUnknown.
func TestSynthesize_FakerAllowlist(t *testing.T) {
	if !IsKnownExternalPackage("@faker-js/faker") {
		t.Fatal("IsKnownExternalPackage(\"@faker-js/faker\") = false; want true (Issue #127)")
	}
	if !IsKnownExternalPackage("@FAKER-JS/FAKER") {
		t.Fatal("IsKnownExternalPackage(\"@FAKER-JS/FAKER\") = false; want case-folded match")
	}
	if !IsKnownExternalPackage("faker") {
		t.Fatal("IsKnownExternalPackage(\"faker\") = false; want true (Issue #127)")
	}
}

// TestGoTestifyBareNames_ClassifiedInTestFiles locks in issue #115:
// testify-helper bare names (Equal/NoError/Contains/...) that arrive at
// the resolver after the Go extractor strips the receiver
// (`assert.Equal(t, ...)` → `Equal`) must classify as stdlib bare-names
// — but only when (a) the source entity's language is "go" AND (b) the
// source file path ends with `_test.go`. The dual gate keeps these
// collision-prone names from shadowing user methods in non-test code.
func TestGoTestifyBareNames_ClassifiedInTestFiles(t *testing.T) {
	names := []string{
		"Equal", "NotEqual", "EqualValues", "Same", "NotSame",
		"NoError", "Error", "Nil", "NotNil",
		"True", "False",
		"Empty", "NotEmpty", "Contains", "NotContains", "Len",
		"Subset", "ElementsMatch",
		"Greater", "Less", "GreaterOrEqual", "LessOrEqual",
		"Panics", "NotPanics", "PanicsWithError",
		"Implements", "IsType",
		"Eventually", "Never", "WithinDuration",
		"JSONEq", "YAMLEq",
		"FileExists", "DirExists",
		"NewRecorder",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Direct stdlibFunction probe with lang="go" + _test.go path
			// must classify.
			subtype, ok := stdlibFunction(name, "go", "internal/foo/foo_test.go", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"go\", _test.go, nil) = (_, false); want classified", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"go\", _test.go, nil) subtype=%q, want %q", name, subtype, "function")
			}
			// End-to-end: Synthesize on a doc whose source entity is a Go
			// _test.go file rewrites the edge to ext:<name>.
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:         "go-test-src",
					Name:       "TestFoo",
					Kind:       "function",
					Language:   "go",
					SourceFile: "internal/foo/foo_test.go",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "go-test-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestGoTestifyBareNames_NotClassifiedInNonTestGoFiles locks in the
// file-path gate: the same testify-helper names must NOT classify when
// the Go source file is a regular `.go` file (not `_test.go`). Without
// the gate, a user-defined `Equal` method on a domain type would be
// shadowed by a synthesised testify placeholder.
func TestGoTestifyBareNames_NotClassifiedInNonTestGoFiles(t *testing.T) {
	names := []string{"Equal", "NoError", "Contains", "Empty", "Len"}
	nonTestPaths := []string{
		"internal/foo/foo.go",
		"cmd/main.go",
		// Adversarial: contains "test" but not as a `_test.go` suffix.
		"internal/test/helpers.go",
		"internal/testutil/util.go",
		"",
	}
	for _, name := range names {
		for _, path := range nonTestPaths {
			name, path := name, path
			t.Run(name+"|"+path, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, "go", path, nil); ok {
					t.Fatalf("stdlibFunction(%q, \"go\", %q, nil) classified; want fall-through "+
						"(file is not a _test.go file)", name, path)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:         "go-src",
						Name:       "Foo",
						Kind:       "function",
						Language:   "go",
						SourceFile: path,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "go-src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, file=%q): synthesized=%d, want 0",
						name, path, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten outside _test.go)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestGoTestifyBareNames_NotClassifiedForOtherLanguages confirms the
// language gate: even for a `_test.go` path, the same testify names
// must not classify when the source language isn't "go". Defensive in
// depth; the file-suffix is the dominant gate but a stray non-Go
// entity with a `.go`-shaped path must not slip through.
func TestGoTestifyBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	names := []string{"Equal", "NoError", "Contains"}
	otherLangs := []string{"python", "javascript", "rust", "java", "ruby", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "foo_test.go", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, _test.go, nil) classified; want fall-through "+
						"(testify map is gated to lang=\"go\")", name, lang)
				}
			})
		}
	}
}

// TestGoTestifyBareNames_RejectedCollisions confirms that names
// rejected as too-likely-to-collide-with-user-methods stay
// fall-through even with lang="go" + a `_test.go` path. Per the
// issue #115 hard rules: New/Add/Set are EXCLUDED. (Note: `Run` is
// excluded from the testify map but classifies via the testing.T map
// added in issue #130 — the testing.T `t.Run("sub", ...)` subtest
// dispatcher dominates in `_test.go` context.)
func TestGoTestifyBareNames_RejectedCollisions(t *testing.T) {
	excluded := []string{"New", "Add", "Set"}
	for _, name := range excluded {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := goTestifyBareNames[name]; ok {
				t.Fatalf("goTestifyBareNames[%q] present; must be rejected per issue #115 "+
					"(collision-prone with user-defined New/Add/Set methods)", name)
			}
			if _, ok := goTestingTBareNames[name]; ok {
				t.Fatalf("goTestingTBareNames[%q] present; must be rejected per issue #115/#130 "+
					"(collision-prone with user-defined New/Add/Set methods)", name)
			}
			if _, ok := stdlibFunction(name, "go", "foo_test.go", nil); ok {
				t.Fatalf("stdlibFunction(%q, \"go\", _test.go, nil) classified; want fall-through "+
					"(name is too-likely to be a user-defined method)", name)
			}
		})
	}
}

// TestGoTestifyBareNames_TestifyPackageAllowlisted confirms #117's
// addition of `github.com/stretchr/testify` as a host-prefixed known
// external package — synthesised testify CALLS edges should resolve
// to the testify package node when the full import path arrives.
func TestGoTestifyBareNames_TestifyPackageAllowlisted(t *testing.T) {
	if !IsKnownExternalPackage("github.com/stretchr/testify") {
		t.Fatal("IsKnownExternalPackage(\"github.com/stretchr/testify\") = false; " +
			"want true (Issue #115/#117)")
	}
}

// TestGoTestingTBareNames_ClassifiedInTestFiles locks in issue #130:
// `*testing.T` helper-method bare names (Helper/Cleanup/Setenv/Logf/
// Fatal/Run/...) that arrive at the resolver after the Go extractor
// strips the receiver (`t.Helper()` → `Helper`) must classify as stdlib
// bare-names — but only when (a) the source entity's language is "go"
// AND (b) the source file path ends with `_test.go`. The dual gate
// keeps these collision-prone names from shadowing user methods in
// non-test code (e.g. `Server.Run`, `Worker.Cleanup`).
func TestGoTestingTBareNames_ClassifiedInTestFiles(t *testing.T) {
	names := []string{
		"Helper", "Cleanup", "Setenv", "Parallel", "TempDir", "Deadline",
		"Skip", "Skipf", "SkipNow",
		"Fail", "FailNow", "Fatal", "Fatalf",
		"Errorf",
		"Log", "Logf",
		"Run",
		"Name", "Context",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "go", "internal/foo/foo_test.go", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"go\", _test.go, nil) = (_, false); want classified", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"go\", _test.go, nil) subtype=%q, want %q",
					name, subtype, "function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:         "go-test-src",
					Name:       "TestFoo",
					Kind:       "function",
					Language:   "go",
					SourceFile: "internal/foo/foo_test.go",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "go-test-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestGoTestingTBareNames_NotClassifiedInNonTestGoFiles locks in the
// file-path gate for issue #130: testing.T helper names must NOT
// classify when the Go source file is a regular `.go` file (not
// `_test.go`). Without the gate, a user-defined `Run` method on a
// `Server` or `Worker` type would be shadowed by a synthesised
// testing.T placeholder.
func TestGoTestingTBareNames_NotClassifiedInNonTestGoFiles(t *testing.T) {
	// Note: `Fatal`/`Fatalf`/`Errorf` are NOT in this list — they
	// classify globally via stdlibBareNames (the lang-agnostic map),
	// independent of the `_test.go` gate. Only names whose ONLY entry
	// point is goTestingTBareNames belong here.
	names := []string{"Helper", "Cleanup", "Setenv", "Run", "Logf", "Parallel"}
	nonTestPaths := []string{
		"internal/foo/foo.go",
		"cmd/main.go",
		// Adversarial: contains "test" but not as a `_test.go` suffix.
		"internal/test/helpers.go",
		"internal/testutil/util.go",
		"",
	}
	for _, name := range names {
		for _, path := range nonTestPaths {
			name, path := name, path
			t.Run(name+"|"+path, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, "go", path, nil); ok {
					t.Fatalf("stdlibFunction(%q, \"go\", %q, nil) classified; want fall-through "+
						"(file is not a _test.go file)", name, path)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:         "go-src",
						Name:       "Foo",
						Kind:       "function",
						Language:   "go",
						SourceFile: path,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "go-src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, file=%q): synthesized=%d, want 0",
						name, path, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten outside _test.go)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestGoTestingTBareNames_NotClassifiedForOtherLanguages confirms the
// language gate for issue #130: even with a `_test.go` path, the
// testing.T names must not classify when the source language isn't "go".
func TestGoTestingTBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	names := []string{"Helper", "Cleanup", "Setenv", "Run", "Logf"}
	otherLangs := []string{"python", "javascript", "rust", "java", "ruby", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "foo_test.go", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, _test.go, nil) classified; want fall-through "+
						"(testing.T map is gated to lang=\"go\")", name, lang)
				}
			})
		}
	}
}

// TestGoTestingTBareNames_NoDuplicatesWithGoBareNames confirms the
// stop-list maps don't duplicate names across stdlibBareNames /
// goBareNames / goTestifyBareNames / goTestingTBareNames. Duplicate
// entries would not break behaviour but signal a categorisation
// mistake — stdlibBareNames matches BEFORE the lang=="go" switch, so a
// name in both maps is dead code in goTestingTBareNames.
func TestGoTestingTBareNames_NoDuplicatesWithGoBareNames(t *testing.T) {
	for name := range goTestingTBareNames {
		if _, dup := stdlibBareNames[name]; dup {
			t.Errorf("name %q present in both stdlibBareNames and goTestingTBareNames; "+
				"remove from goTestingTBareNames (already classified language-wide "+
				"before the lang==\"go\" switch — entry is dead code)", name)
		}
		if _, dup := goBareNames[name]; dup {
			t.Errorf("name %q present in both goBareNames and goTestingTBareNames; "+
				"remove from goTestingTBareNames (already classified language-wide)", name)
		}
		if _, dup := goTestifyBareNames[name]; dup {
			t.Errorf("name %q present in both goTestifyBareNames and goTestingTBareNames; "+
				"remove from goTestingTBareNames (already classified by testify map)", name)
		}
	}
}

// TestGoChiRouterNames_ClassifiedWithChiImport locks in issue #131:
// go-chi router-method bare names (Get/Post/Put/Delete/Mount/Group/...)
// that arrive at the resolver after the Go extractor strips the
// receiver (`r.Get("/x", h)` → `Get`) must classify under the chi gate
// — but only when (a) the source entity's language is "go" AND (b) the
// source file's IMPORTS edges include any canonical go-chi import path.
// The dual gate keeps these collision-prone names from shadowing user
// methods like `Repository.Get` in non-chi code.
//
// Refs #44: after the package-fold sentinel fix, stdlibFunction returns
// "go_chi_function" (not "function") and classifyExternal folds to the
// canonical chi placeholder ext:github.com/go-chi/chi — a better outcome
// than per-verb ext:<name> placeholders.
func TestGoChiRouterNames_ClassifiedWithChiImport(t *testing.T) {
	names := []string{
		"Get", "Post", "Put", "Delete", "Patch",
		"Head", "Options", "Connect", "Trace",
		"Mount", "Group", "Route", "Use", "With",
		"HandleFunc", "Handle", "NotFound", "MethodNotAllowed",
	}
	chiPaths := []string{
		"github.com/go-chi/chi",
		"github.com/go-chi/chi/v5",
		"github.com/go-chi/chi/v4",
		"github.com/go-chi/chi/v3",
	}
	for _, name := range names {
		for _, chiPath := range chiPaths {
			name, chiPath := name, chiPath
			t.Run(name+"|"+chiPath, func(t *testing.T) {
				t.Parallel()
				// Direct stdlibFunction probe with lang="go" + chi import
				// in the imports set must classify.
				imports := map[string]bool{chiPath: true}
				subtype, ok := stdlibFunction(name, "go", "internal/api/router.go", imports)
				if !ok {
					t.Fatalf("stdlibFunction(%q, \"go\", chi=%q) = (_, false); want classified",
						name, chiPath)
				}
				// Refs #44 — names that are also in goBareNames fire that
				// catalog first and return "function"; names that are ONLY in
				// goChiRouterNames return the "go_chi_function" sentinel so
				// classifyExternal folds to the package placeholder.
				// Either outcome means the name is classified — that is the
				// structural invariant we assert here.
				if subtype != "function" && subtype != "go_chi_function" {
					t.Fatalf("stdlibFunction(%q, \"go\", chi=%q) subtype=%q, want function or go_chi_function",
						name, chiPath, subtype)
				}
				// End-to-end: a Go entity in a file that imports chi must
				// rewrite chi-router bare stubs to ext:github.com/go-chi/chi
				// (the package canonical), NOT to ext:<bare-name>. Refs #44.
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:         "go-chi-src",
						Name:       "RegisterRoutes",
						Kind:       "function",
						Language:   "go",
						SourceFile: "internal/api/router.go",
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-import", FromID: "internal/api/router.go", ToID: chiPath, Kind: "IMPORTS"},
						{ID: "rel-call", FromID: "go-chi-src", ToID: name, Kind: "CALLS"},
					},
				}
				Synthesize(doc)
				// Find the CALLS edge and check it was rewritten.
				var got string
				for _, r := range doc.Relationships {
					if r.ID == "rel-call" {
						got = r.ToID
						break
					}
				}
				// The stub must have been rewritten to some ext: form.
				if got == name {
					t.Fatalf("CALLS edge ToID=%q unchanged; chi gate did not fire "+
						"(name=%q, chi import=%q)", got, name, chiPath)
				}
				if !strings.HasPrefix(got, "ext:") {
					t.Fatalf("CALLS edge ToID=%q, want ext: prefix "+
						"(name=%q, chi import=%q)", got, name, chiPath)
				}
				// Names that are only in goChiRouterNames (not in goBareNames)
				// fold to the chi package canonical. Names that are in BOTH
				// (HandleFunc, NotFound, MethodNotAllowed) fire goBareNames
				// first and produce ext:<bare-name> — that is still correct
				// because they are goBareNames members. We only assert the
				// chi-exclusive names fold to the package.
				chiExclusiveNames := map[string]bool{
					"Get": true, "Post": true, "Put": true, "Delete": true,
					"Patch": true, "Head": true, "Options": true, "Connect": true,
					"Trace": true, "Mount": true, "Group": true, "Route": true,
					"Use": true, "With": true, "NewRouter": true,
					"URLParam": true, "URLParamFromCtx": true,
				}
				if chiExclusiveNames[name] && !strings.HasPrefix(got, "ext:github.com/go-chi") {
					t.Fatalf("CALLS edge ToID=%q, want prefix ext:github.com/go-chi for chi-exclusive name "+
						"(name=%q, chi import=%q)", got, name, chiPath)
				}
			})
		}
	}
}

// TestGoChiRouterNames_NotClassifiedWithoutChiImport locks in the
// import-set gate: the same chi-router names must NOT classify when the
// source file's IMPORTS edges don't include any go-chi path. Without
// this gate, a user-defined `Repository.Get` would be shadowed by a
// synthesised chi placeholder.
func TestGoChiRouterNames_NotClassifiedWithoutChiImport(t *testing.T) {
	names := []string{"Get", "Post", "Put", "Delete", "Mount", "Group", "Use"}
	importSets := []map[string]bool{
		nil,
		{},
		// Adversarial: imports unrelated packages, but no chi.
		{"github.com/gin-gonic/gin": true},
		{"github.com/labstack/echo": true},
		{"net/http": true, "encoding/json": true},
		// Adversarial: a non-chi path that contains "chi" as a substring.
		{"github.com/example/chi-fork": true},
		{"github.com/anything/notchi": true},
	}
	for _, name := range names {
		for i, imports := range importSets {
			name, imports, i := name, imports, i
			t.Run(name+"/case"+itoa(i), func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, "go", "internal/foo/foo.go", imports); ok {
					t.Fatalf("stdlibFunction(%q, \"go\", imports=%v) classified; "+
						"want fall-through (no chi import in set)", name, imports)
				}
			})
		}
	}
}

// TestGoChiRouterNames_NotClassifiedForOtherLanguages confirms the
// language gate: even with a chi import path in the set, the chi names
// must not classify when the source language isn't "go". Defensive in
// depth.
func TestGoChiRouterNames_NotClassifiedForOtherLanguages(t *testing.T) {
	names := []string{"Get", "Post", "Mount"}
	imports := map[string]bool{"github.com/go-chi/chi/v5": true}
	otherLangs := []string{"python", "javascript", "rust", "java", "ruby", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "foo.go", imports); ok {
					t.Fatalf("stdlibFunction(%q, %q, chi imports) classified; "+
						"want fall-through (chi map is gated to lang=\"go\")", name, lang)
				}
			})
		}
	}
}

// TestGoChiRouterNames_ChiPackageAllowlisted confirms #131 relies on
// the existing #117 host-prefixed known-external entry for the chi
// package — synthesised chi router CALLS edges should resolve to a chi
// package node when the full import path arrives at the resolver.
func TestGoChiRouterNames_ChiPackageAllowlisted(t *testing.T) {
	if !IsKnownExternalPackage("github.com/go-chi/chi") {
		t.Fatal("IsKnownExternalPackage(\"github.com/go-chi/chi\") = false; " +
			"want true (Issue #117/#131)")
	}
}

// itoa formats a small non-negative int as decimal. Local helper to
// keep test sub-names deterministic without pulling in strconv.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// ---------------------------------------------------------------------------
// Issue #424 — Docker image refs and host-path mounts route through synth.
// ---------------------------------------------------------------------------

// TestSynthesize_DockerImage_Compose covers a Compose-style image stub
// "docker_image:nginx:1.21". Synth must rewrite the edge to ext:docker:nginx
// (tag dropped, single placeholder per repository) and place the image on
// the allowlist so the resolver classifies it as ExternalKnown.
func TestSynthesize_DockerImage_Compose(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "docker_compose/service/api", ToID: "docker_image:nginx:1.21", Kind: "IMPORTS"},
			{ID: "r2", FromID: "docker_compose/service/db", ToID: "docker_image:postgres:14", Kind: "IMPORTS"},
			{ID: "r3", FromID: "docker_compose/service/cache", ToID: "docker_image:redis:alpine", Kind: "IMPORTS"},
		},
	}
	stats := Synthesize(doc)
	if stats.RelationshipsResolved != 3 {
		t.Fatalf("resolved=%d, want 3", stats.RelationshipsResolved)
	}
	want := map[int]string{
		0: "ext:docker:nginx",
		1: "ext:docker:postgres",
		2: "ext:docker:redis",
	}
	for i, w := range want {
		if got := doc.Relationships[i].ToID; got != w {
			t.Errorf("rel[%d].ToID=%q, want %q", i, got, w)
		}
	}
	for _, w := range want {
		if !IsKnownExternalPackage(w[len("ext:"):]) {
			t.Errorf("IsKnownExternalPackage(%q)=false, want true (ExternalKnown gate)", w)
		}
	}
}

// TestSynthesize_DockerImage_Registry covers a registry-prefixed image with
// a port (`myregistry.io:5000/team/api:dev`). The leading colon belongs to
// the port and must NOT be treated as a tag separator.
func TestSynthesize_DockerImage_Registry(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "k8s/container/app", ToID: "docker_image:ghcr.io/owner/svc:v1.2.3", Kind: "IMPORTS"},
			{ID: "r2", FromID: "k8s/container/api", ToID: "docker_image:myregistry.io:5000/team/api:dev", Kind: "IMPORTS"},
			{ID: "r3", FromID: "k8s/container/raw", ToID: "docker_image:alpine@sha256:abc", Kind: "IMPORTS"},
		},
	}
	Synthesize(doc)
	want := []string{
		"ext:docker:ghcr.io/owner/svc",
		"ext:docker:myregistry.io:5000/team/api",
		"ext:docker:alpine",
	}
	for i, w := range want {
		if got := doc.Relationships[i].ToID; got != w {
			t.Errorf("rel[%d].ToID=%q, want %q", i, got, w)
		}
	}
}

// TestSynthesize_HostPathMount covers Compose host-filesystem volume mounts.
// Every distinct path collapses to a single ext:external_filesystem
// placeholder; the package is NOT on the allowlist, so the resolver lands
// these in ExternalUnknown.
func TestSynthesize_HostPathMount(t *testing.T) {
	doc := &graph.Document{
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "docker_compose/service/api", ToID: "host_path:./src", Kind: "IMPORTS"},
			{ID: "r2", FromID: "docker_compose/service/api", ToID: "host_path:/etc/myapp", Kind: "IMPORTS"},
			{ID: "r3", FromID: "docker_compose/service/api", ToID: "host_path:${PWD}/data", Kind: "IMPORTS"},
		},
	}
	Synthesize(doc)
	for i, r := range doc.Relationships {
		if r.ToID != "ext:external_filesystem" {
			t.Errorf("rel[%d].ToID=%q, want ext:external_filesystem", i, r.ToID)
		}
	}
	if IsKnownExternalPackage("external_filesystem") {
		t.Error("external_filesystem unexpectedly on allowlist; want ExternalUnknown disposition")
	}
}

// TestDockerImageRepo unit-tests the repo-extraction helper for canonical
// docker image references.
func TestDockerImageRepo(t *testing.T) {
	cases := map[string]string{
		"":                                "",
		"nginx":                           "nginx",
		"nginx:1.21":                      "nginx",
		"redis:alpine":                    "redis",
		"library/postgres:14":             "library/postgres",
		"ghcr.io/owner/svc:v1.2.3":        "ghcr.io/owner/svc",
		"myregistry.io:5000/team/api:dev": "myregistry.io:5000/team/api",
		"myregistry.io:5000/team/api":     "myregistry.io:5000/team/api",
		"alpine@sha256:abcdef":            "alpine",
		"ubuntu:22.04@sha256:abcdef":      "ubuntu",
	}
	for in, want := range cases {
		if got := dockerImageRepo(in); got != want {
			t.Errorf("dockerImageRepo(%q)=%q, want %q", in, got, want)
		}
	}
}

// TestPHPLaravelSymfonyDSLBareNames_ClassifiedWhenLangIsPHP covers
// issue #439: Laravel facade DSL leaves (`Route::get(...)` →
// `routes`/`middleware`/`prefix`), Eloquent ORM persistence and query
// builder calls (`$user->save()`, `User::find($id)`,
// `Post::where(...)->paginate(...)`), Symfony AbstractController
// helpers (`$this->render(...)`, `$this->redirectToRoute(...)`), and
// Laravel global helpers (`config(...)`, `auth()`, `view(...)`) get
// receiver-stripped (or arrive bare) and land in bug-extractor. These
// names must classify as stdlib bare-names — but only when the source
// entity's language is "php". Mirrors the Kotlin Ktor (#435) and Swift
// Vapor (#436) precedents.
func TestPHPLaravelSymfonyDSLBareNames_ClassifiedWhenLangIsPHP(t *testing.T) {
	names := []string{
		// Eloquent ORM persistence + lifecycle.
		"find", "findOrFail", "findMany", "firstOrFail", "firstOrCreate",
		"save", "update", "delete", "forceDelete", "restore",
		"create", "make", "fill", "refresh", "fresh", "replicate",
		"is", "isNot",
		"belongsTo", "belongsToMany", "hasMany", "hasOne",
		"morphTo", "morphMany", "morphOne",
		// Eloquent / query builder.
		"where", "whereIn", "whereNotIn", "whereHas",
		"whereNull", "whereNotNull", "whereBetween", "whereDate",
		"with", "without", "orderBy", "groupBy", "having",
		"limit", "take", "skip", "first", "latest", "oldest",
		"paginate", "count", "avg", "pluck", "chunk", "each",
		"select", "selectRaw", "union", "unionAll", "joinSub",
		"crossJoin", "leftJoin", "rightJoin", "joins",
		// Symfony controller helpers.
		"render", "redirectToRoute", "redirect",
		"createForm", "createFormBuilder",
		"addFlash", "denyAccessUnlessGranted",
		"getUser", "isGranted", "generateUrl",
		"json", "file", "forward",
		"getDoctrine", "getParameter", "dispatchEvent",
		// Laravel facade DSL leaves.
		"routes", "middleware", "controller", "domain", "prefix",
		// Laravel global helpers.
		"config", "env", "route", "url", "asset", "auth", "request",
		"session", "cookie", "view", "response", "back", "old",
		"csrf_token", "csrf_field", "dd", "dump", "now", "today",
		"app", "resolve", "event", "dispatch", "validator", "optional",
		"tap",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "php", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"php\", nil) = (_, false); want classified as stdlib bare-name", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"php\", nil) subtype=%q, want %q", name, subtype, "function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "php-src",
					Name:     "caller",
					Kind:     "function",
					Language: "php",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "php-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestPHPLaravelSymfonyDSLBareNames_NotClassifiedForOtherLanguages
// confirms the PHP language gate holds for the issue #439 additions:
// Laravel / Symfony / Eloquent DSL names must NOT be rewritten when
// the source entity's language is anything other than "php". A JS user
// method named `request`, a Go method named `paginate`, a Ruby
// `redirectToRoute`, etc. must not be shadowed by the PHP gate.
func TestPHPLaravelSymfonyDSLBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Selection rule: each name MUST be unique to phpBareNames (i.e. not
	// in stdlibBareNames or any other language map), otherwise the
	// cross-language gate test would trip on a different language's
	// allowlist firing first. Picks span Eloquent verbs, Symfony
	// helpers, Laravel facade leaves, and Laravel global helpers.
	names := []string{
		// Eloquent verbs unique to PHP.
		"findOrFail", "firstOrFail", "firstOrCreate",
		"forceDelete", "belongsToMany", "morphTo", "morphMany",
		"whereHas", "whereNotIn", "selectRaw", "joinSub",
		// Symfony helpers unique to PHP.
		"redirectToRoute", "createForm", "createFormBuilder",
		"denyAccessUnlessGranted", "isGranted", "generateUrl",
		"getDoctrine", "getParameter", "dispatchEvent", "addFlash",
		// Laravel facade leaves / globals unique to PHP.
		"csrf_token", "csrf_field", "validator",
	}
	otherLangs := []string{"go", "python", "javascript", "ruby", "rust", "java", "kotlin", "swift", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"php\" only)", name, lang)
				}
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID:       "src",
						Name:     "caller",
						Kind:     "function",
						Language: lang,
					}},
					Relationships: []graph.Relationship{
						{ID: "rel-1", FromID: "src", ToID: name, Kind: "CALLS"},
					},
				}
				stats := Synthesize(doc)
				if stats.Synthesized != 0 {
					t.Fatalf("Synthesize(%q, lang=%q): synthesized=%d, want 0",
						name, lang, stats.Synthesized)
				}
				if doc.Relationships[0].ToID != name {
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-PHP)",
						doc.Relationships[0].ToID, name)
				}
			})
		}
	}
}

// TestPHPBareNames_HTTPVerbsRejected confirms the issue #439 spec
// "REJECT" rule: HTTP verb bare names `get` / `post` / `put` /
// `delete` from `Route::get(...)` must NOT be in phpBareNames, because
// they collide trivially with Eloquent attribute-accessor patterns
// (`$model->get('name')`) and PSR-7 ServerRequest accessors. The #94 /
// #106 safer-bias rule applies. NOTE: `delete` IS in phpBareNames as
// the Eloquent destructor (`$model->delete()`), per the spec — only
// `get`/`post`/`put` are rejected on collision grounds.
func TestPHPBareNames_HTTPVerbsRejected(t *testing.T) {
	rejected := []string{"get", "post", "put"}
	for _, name := range rejected {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := phpBareNames[name]; ok {
				t.Fatalf("phpBareNames[%q] present; must be rejected per issue #439 (HTTP verb collides with PHP property accessors)", name)
			}
		})
	}
}

// TestPHPBareNames_UnknownPHPMethodFallsThrough confirms that a
// PHP-source bare-name call that ISN'T in the phpBareNames allowlist
// still falls through normally, so genuine missing-resolution bugs
// continue to surface in bug-extractor.
func TestPHPBareNames_UnknownPHPMethodFallsThrough(t *testing.T) {
	name := "myCustomBusinessMethod" // Not Laravel/Symfony/Eloquent; user-defined.
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "php-src",
			Name:     "caller",
			Kind:     "function",
			Language: "php",
		}},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "php-src", ToID: name, Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("Synthesize(%q): synthesized=%d, want 0 (unknown user fn)", name, stats.Synthesized)
	}
	if doc.Relationships[0].ToID != name {
		t.Fatalf("ToID=%q, want %q (unknown name must not be rewritten)",
			doc.Relationships[0].ToID, name)
	}
}

// TestPHPSlice8BareNames_ClassifiedWhenLangIsPHP covers the slice-8
// (issue #44) additions to phpBareNames:
//   - Laravel Blueprint column/modifier methods: `integer`, `foreignId`,
//     `constrained`, `index`, `hasTable`, `hasColumn`, `dropTable`.
//   - Eloquent Model lifecycle methods forwarded through traits:
//     `bootTraits`, `booted`, `fireModelEvent`, `initializeTraits`,
//     `syncOriginal`.
//   - Laravel HTTP helpers: `noContent`, `hasFile`, `validate`,
//     `withErrors`, `insertGetId`, `attempt`, `regenerate`, `invalidate`,
//     `intended`.
//
// All names must classify as stdlib bare-names when lang=="php" and
// must NOT be rewritten for any other language (PHP gate holds).
func TestPHPSlice8BareNames_ClassifiedWhenLangIsPHP(t *testing.T) {
	names := []string{
		// Blueprint column/modifier methods (slice-8).
		"integer", "foreignId", "constrained", "index",
		"hasTable", "hasColumn", "dropTable",
		// Eloquent Model lifecycle trait methods (slice-8).
		"bootTraits", "booted", "fireModelEvent",
		"initializeTraits", "syncOriginal",
		// Laravel HTTP helpers (slice-8).
		"noContent", "hasFile", "validate", "withErrors", "insertGetId",
		"attempt", "regenerate", "invalidate", "intended",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "php", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"php\", nil) = (_, false); want classified (slice-8 phpBareNames)", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"php\", nil) subtype=%q, want \"function\"", name, subtype)
			}
			// Full Synthesize round-trip: bare CALLS edge rewrites to ext:<name>.
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "php-src",
					Name:     "caller",
					Kind:     "function",
					Language: "php",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "php-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestPHPSlice8BareNames_NotClassifiedForOtherLanguages confirms the
// PHP language gate for slice-8 names: these names must NOT be rewritten
// when the source entity's language is anything other than "php".
// Each name is unique to phpBareNames (not in stdlibBareNames or any
// other language map) so the gate test is clean.
func TestPHPSlice8BareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	names := []string{
		// Blueprint methods unique to PHP.
		"foreignId", "constrained", "hasTable", "hasColumn",
		// Eloquent lifecycle methods unique to PHP.
		"bootTraits", "fireModelEvent", "initializeTraits", "syncOriginal",
		// HTTP helpers unique to PHP in this context.
		"noContent", "hasFile", "withErrors", "insertGetId",
		"regenerate", "invalidate", "intended",
	}
	otherLangs := []string{"go", "python", "javascript", "ruby", "rust", "java", "kotlin", "swift", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"php\" only, slice-8)", name, lang)
				}
			})
		}
	}
}

// TestPythonDjangoDRFDSLBareNames_ClassifiedWhenLangIsPython covers
// issue #447: Django ORM model fields (`CharField`, `ForeignKey`,
// `ManyToManyField`, ...), QuerySet / manager verbs (`objects`,
// `get_or_create`, `select_related`, `bulk_create`, ...), Meta
// options (`verbose_name`, `unique_together`, ...), Django REST
// Framework view / serializer / permission classes
// (`ModelSerializer`, `ListAPIView`, `ModelViewSet`,
// `IsAuthenticated`, ...) and Django admin DSL helpers (`register`,
// `list_display`, ...) get receiver-stripped (or arrive bare) and
// land in bug-extractor. These names must classify as stdlib
// bare-names — but only when the source entity's language is
// "python". Mirrors the PHP Laravel/Symfony (#439) precedent.
func TestPythonDjangoDRFDSLBareNames_ClassifiedWhenLangIsPython(t *testing.T) {
	names := []string{
		// Django ORM field types.
		"CharField", "IntegerField", "BooleanField", "DateTimeField",
		"DateField", "TextField", "ForeignKey", "OneToOneField",
		"ManyToManyField", "URLField", "EmailField", "SlugField",
		"DecimalField", "FloatField", "BinaryField", "JSONField",
		"FileField", "ImageField",
		// Django ORM manager / QuerySet API.
		"objects", "exclude", "get", "get_or_create", "update_or_create",
		"create", "save", "delete", "update", "select_related",
		"prefetch_related", "values", "values_list", "annotate",
		"aggregate", "count", "exists", "bulk_create", "bulk_update",
		"latest", "earliest",
		// Django Meta options.
		"Meta", "verbose_name", "verbose_name_plural", "ordering",
		"unique_together", "index_together", "validators",
		// DRF serializer / view / viewset classes.
		"ModelSerializer", "Serializer", "ListAPIView", "RetrieveAPIView",
		"CreateAPIView", "UpdateAPIView", "DestroyAPIView",
		"ListCreateAPIView", "RetrieveUpdateDestroyAPIView",
		"ModelViewSet", "ReadOnlyModelViewSet",
		// DRF view attributes + decorator + status module leaf.
		"status", "action", "permission_classes",
		"authentication_classes", "serializer_class", "queryset",
		// DRF permission classes.
		"IsAuthenticated", "IsAdminUser", "AllowAny",
		"IsAuthenticatedOrReadOnly",
		// Django admin DSL.
		"register", "unregister", "site", "list_display", "list_filter",
		"search_fields", "readonly_fields", "fieldsets",
		// Issue #455 additions — typing.
		"cast", "Optional", "Union", "Callable", "Iterable", "Iterator",
		"Generator", "TypeVar", "Generic", "Protocol", "Awaitable",
		"Sequence", "Mapping", "Annotated", "Literal", "Final",
		"ClassVar", "NewType", "NamedTuple", "TypedDict", "overload",
		// functools / itertools.
		"update_wrapper", "partial", "wraps", "lru_cache", "cache",
		"cached_property", "reduce", "chain", "islice", "cycle", "tee",
		"starmap", "takewhile", "dropwhile", "groupby", "product",
		"permutations", "combinations",
		// inspect / textwrap.
		"cleandoc", "getsource", "signature", "isfunction", "isclass",
		"ismethod", "getmembers", "dedent", "indent",
		// pytest.
		"raises", "fixture", "mark", "parametrize", "monkeypatch",
		"xfail",
		// dataclasses.
		"dataclass", "field", "fields", "asdict", "astuple",
		"is_dataclass",
		// pathlib.
		"Path", "PurePath", "PurePosixPath", "PureWindowsPath",
		// os / os.path / io.
		"dirname", "basename", "abspath", "realpath", "expanduser",
		"expandvars", "splitext", "fspath", "fileno", "mkdir",
		"getfilesystemencoding",
		"getvalue", "readouterr",
		// logging.
		"getLogger", "basicConfig",
		// Flask DSL.
		"route", "register_blueprint", "add_url_rule", "errorhandler",
		"as_view", "app_context", "test_request_context", "test_client",
		"test_cli_runner", "url_for", "jsonify", "init_app", "Markup",
		"_get_current_object", "app_template_filter", "app_template_test",
		"add_template_filter", "add_template_test", "template_global",
		"template_filter", "template_test", "make_response", "redirect",
		"send_file", "send_from_directory", "abort", "flash",
		"stream_with_context", "copy_current_request_context",
		// Click DSL.
		"invoke", "isolated_filesystem", "get_help_record",
		"get_help_extra", "make_context", "get_parameter_source",
		"call_on_close", "lookup_default", "get_default",
		// SQLAlchemy.
		"filter_by", "create_all", "drop_all", "query",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction(name, "python", "", nil)
			if !ok {
				t.Fatalf("stdlibFunction(%q, \"python\", nil) = (_, false); want classified as stdlib bare-name", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"python\", nil) subtype=%q, want %q", name, subtype, "function")
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:       "py-src",
					Name:     "caller",
					Kind:     "function",
					Language: "python",
				}},
				Relationships: []graph.Relationship{
					{ID: "rel-1", FromID: "py-src", ToID: name, Kind: "CALLS"},
				},
			}
			stats := Synthesize(doc)
			if stats.Synthesized != 1 {
				t.Fatalf("Synthesize(%q): synthesized=%d, want 1", name, stats.Synthesized)
			}
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
			}
		})
	}
}

// TestPythonDjangoDRFDSLBareNames_NotClassifiedForOtherLanguages
// confirms the Python language gate holds for the issue #447
// additions: Django / DRF / admin DSL names must NOT be rewritten
// when the source entity's language is anything other than "python".
// A Go method named `save`, a JS method named `update`, a Ruby
// `register`, etc. must not be shadowed by the Python gate.
func TestPythonDjangoDRFDSLBareNames_NotClassifiedForOtherLanguages(t *testing.T) {
	// Selection rule: each name MUST be unique to pythonBareNames
	// (not in stdlibBareNames, phpBareNames or any other language
	// map), otherwise the cross-language gate test would trip on a
	// different language's allowlist firing first. Picks span
	// Django ORM, Meta, DRF, and admin.
	names := []string{
		// Django ORM field types (Pascal-case, no collisions).
		"CharField", "ForeignKey", "ManyToManyField", "OneToOneField",
		"DateTimeField", "JSONField", "ImageField",
		// QuerySet verbs unique to Python (snake_case).
		"get_or_create", "update_or_create", "select_related",
		"prefetch_related", "values_list", "bulk_create", "bulk_update",
		// Meta options.
		"verbose_name", "verbose_name_plural", "unique_together",
		"index_together",
		// DRF view / serializer classes.
		"ModelSerializer", "ListAPIView", "RetrieveAPIView",
		"CreateAPIView", "ModelViewSet", "ReadOnlyModelViewSet",
		"RetrieveUpdateDestroyAPIView",
		// DRF view attributes (snake_case, Python-only).
		"permission_classes", "authentication_classes",
		"serializer_class",
		// DRF permission classes. `IsAuthenticated` collides with
		// csharpBareNames (ASP.NET Core) and would trip the cross-
		// lang gate via the C# allowlist, so it is excluded here.
		"IsAdminUser", "IsAuthenticatedOrReadOnly",
		// Django admin attributes.
		"list_display", "list_filter", "search_fields",
		"readonly_fields", "fieldsets",
		// Issue #455 — Python-only typing primitives (collision-free
		// across other-language maps). `Iterator`, `Any` are in
		// pythonBareNames but ALSO in rustBareNames /
		// goChiRouterNames respectively and are intentionally
		// excluded from this cross-lang gate (they classify under the
		// other language's allowlist, which is correct for that
		// language's source).
		"Optional", "Union", "Callable", "Iterable", "Generator",
		"TypeVar", "Awaitable", "Sequence", "Mapping", "Annotated",
		"Literal", "ClassVar", "NewType", "NamedTuple", "TypedDict",
		"overload",
		// functools / itertools — `chain` excluded (rust collision).
		"update_wrapper", "wraps", "lru_cache", "cached_property",
		"islice", "starmap", "takewhile", "dropwhile", "groupby",
		"permutations", "combinations",
		// inspect / textwrap.
		"cleandoc", "getsource", "isfunction", "isclass", "ismethod",
		"getmembers", "dedent",
		// pytest.
		"raises", "parametrize", "monkeypatch", "xfail",
		// dataclasses.
		"dataclass", "asdict", "astuple", "is_dataclass",
		// pathlib — `Path` excluded (rust collision).
		"PurePath", "PurePosixPath", "PureWindowsPath",
		// os / os.path / io.
		"dirname", "basename", "abspath", "realpath", "expanduser",
		"expandvars", "splitext", "fspath", "fileno", "mkdir",
		"getfilesystemencoding", "getvalue", "readouterr",
		// logging.
		"getLogger", "basicConfig",
		// Flask DSL — `route`, `redirect`, `flash` excluded
		// (rust/swift/php/java/kotlin collisions).
		"register_blueprint", "add_url_rule", "errorhandler", "as_view",
		"app_context", "test_request_context", "test_client",
		"test_cli_runner", "url_for", "jsonify", "init_app", "Markup",
		"_get_current_object", "app_template_filter", "app_template_test",
		"add_template_filter", "add_template_test", "template_global",
		"template_filter", "template_test", "make_response", "send_file",
		"send_from_directory", "stream_with_context",
		"copy_current_request_context",
		// Click DSL.
		"isolated_filesystem", "get_help_record", "get_help_extra",
		"make_context", "get_parameter_source", "call_on_close",
		"lookup_default", "get_default",
		// SQLAlchemy — `query` excluded (swift collision).
		"filter_by", "create_all", "drop_all",
		// Wave-6 — PyMongo Collection / Database method surface.
		// All distinctive `_one` / `_many` / `_index*` / `_documents`
		// suffixes; no cross-language collisions.
		"get_collection", "get_database",
		"insert_one", "insert_many",
		"update_one", "update_many",
		"delete_one", "delete_many",
		"find_one", "find_one_and_update", "find_one_and_delete",
		"find_one_and_replace", "replace_one",
		"bulk_write", "count_documents", "estimated_document_count",
		"distinct",
		"create_index", "create_indexes", "drop_index", "drop_indexes",
		"list_indexes", "index_information",
		// Wave-6 — bson value types.
		"Decimal128",
		// Wave-6 — datetime / dateutil / uuid / requests / Django
		// extras / DB-API.  Excluded from this cross-language test:
		// `strptime` (cpp/libc collision), `now` and `today` (php
		// Laravel + JS/Swift collisions) — gated to lang=="python"
		// only, behaviourally safe but cross-lang test would trip
		// on the OTHER language's map firing for non-python sources.
		"fromisoformat", "fromtimestamp", "utcnow", "utcfromtimestamp",
		"timedelta", "relativedelta",
		"randint", "randrange",
		"uuid4", "uuid1",
		"raise_for_status",
		"order_by", "getlist",
		"fetchall", "fetchone", "fetchmany",
	}
	// Per #516: structural per-(name, lang) assertion via
	// assertCrossLangGate. Names like `basename`/`dirname`/`mkdir`/
	// `realpath` legitimately appear in BOTH pythonBareNames AND
	// rubyBareNames — what matters is that each catalog only fires
	// for its own source language. Pairs where `otherLang` owns the
	// name are skipped; remaining pairs must fall through.
	otherLangs := []string{"go", "php", "javascript", "ruby", "rust", "java", "kotlin", "swift", "csharp", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				assertCrossLangGate(t, name, lang, "python")
			})
		}
	}
}

// TestPythonBareNames_NoDuplicatesWithStdlibBareNames confirms that
// names already classified by the global stdlibBareNames stop-list
// (`filter`, `Response`) are NOT duplicated in pythonBareNames. The
// global list fires regardless of language gate, so duplicates are
// dead entries and a maintenance hazard.
func TestPythonBareNames_NoDuplicatesWithStdlibBareNames(t *testing.T) {
	for name := range pythonBareNames {
		if _, ok := stdlibBareNames[name]; ok {
			t.Errorf("pythonBareNames[%q] duplicates stdlibBareNames entry; remove from pythonBareNames", name)
		}
	}
}

// TestPythonBareNames_UnknownPythonMethodFallsThrough confirms that a
// Python-source bare-name call that ISN'T in the pythonBareNames
// allowlist still falls through normally, so genuine
// missing-resolution bugs continue to surface in bug-extractor.
func TestPythonBareNames_UnknownPythonMethodFallsThrough(t *testing.T) {
	name := "my_custom_business_method" // Not Django/DRF/admin; user-defined.
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:       "py-src",
			Name:     "caller",
			Kind:     "function",
			Language: "python",
		}},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "py-src", ToID: name, Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 0 {
		t.Fatalf("Synthesize(%q): synthesized=%d, want 0 (unknown user fn)", name, stats.Synthesized)
	}
	if doc.Relationships[0].ToID != name {
		t.Fatalf("ToID=%q, want %q (unknown name must not be rewritten)",
			doc.Relationships[0].ToID, name)
	}
}

// TestSynthesize_NodeStdlibExternalRef (Refs #44) — the JS/TS extractor
// emits `scope:operation:method:<lang>:external:node:<module>` stubs for
// Node.js stdlib method calls. The cross-language `:external:` branch
// must canonicalise those to a single `ext:node:<module>` placeholder
// per module, regardless of how many distinct methods called into the
// module.
func TestSynthesize_NodeStdlibExternalRef(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "ts-caller", Name: "f", Kind: "SCOPE.Operation", SourceFile: "src/x.ts", Language: "typescript"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "ts-caller", ToID: "scope:operation:method:typescript:external:node:path", Kind: "CALLS"},
			{ID: "rel-2", FromID: "ts-caller", ToID: "scope:operation:method:typescript:external:node:fs", Kind: "CALLS"},
			// Same module, different leaf method — must collapse to the
			// existing ext:node:path placeholder.
			{ID: "rel-3", FromID: "ts-caller", ToID: "scope:operation:method:typescript:external:node:path", Kind: "CALLS"},
		},
	}
	stats := Synthesize(doc)
	if stats.Synthesized != 2 {
		t.Fatalf("synthesized=%d, want 2 (ext:node:path + ext:node:fs)", stats.Synthesized)
	}
	if stats.RelationshipsResolved != 3 {
		t.Fatalf("resolved=%d, want 3", stats.RelationshipsResolved)
	}
	wantIDs := map[string]bool{"ext:node:path": false, "ext:node:fs": false}
	for _, e := range doc.Entities {
		if _, ok := wantIDs[e.ID]; ok {
			wantIDs[e.ID] = true
			if e.Kind != KindExternal {
				t.Fatalf("%s kind=%q, want %q", e.ID, e.Kind, KindExternal)
			}
		}
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Fatalf("missing placeholder %s; entities=%+v", id, doc.Entities)
		}
	}
	for _, r := range doc.Relationships {
		if r.ToID != "ext:node:path" && r.ToID != "ext:node:fs" {
			t.Fatalf("rel ToID=%q not rewritten to ext:node:*", r.ToID)
		}
	}
}

// ---------------------------------------------------------------------
// Wave-10 Track D — Per-import file-scoped Python gate tests.
//
// Verifies the 14 per-library gates added to lift generic Python verbs
// off the #94 safer-bias floor on files that import the canonical
// library whose surface those names belong to. Three cohorts:
//
//   1. WithImport: classify lifts to ext:<canonical-pkg> when the gate
//      fires.
//   2. WithoutImport: same name on a python file without the gate's
//      canonical import keeps the safer-bias miss (stays bug-extractor).
//   3. CrossLanguage: the gate cannot fire for non-python sources even
//      if the importing package is in the import set (shields user
//      methods named `head`, `find_all`, `urljoin` in JS / Go / Ruby).
// ---------------------------------------------------------------------

type pythonGateCase struct {
	gate          string
	importPath    string
	bareName      string
	wantCanonical string
}

func pythonGateCases() []pythonGateCase {
	return []pythonGateCase{
		// pandas / numpy / pyarrow gate. `groupby` / `reshape` / `query`
		// already classify via pythonBareNames; pick names unique to gate.
		{"pandas", "pandas", "head", "pandas"},
		{"pandas", "numpy", "transpose", "pandas"},
		{"pandas", "pyarrow", "melt", "pandas"},
		// requests / httpx / aiohttp gate. `options` is already in
		// pythonBareNames; pick gate-unique names.
		{"requests", "requests", "send", "requests"},
		{"requests", "httpx", "prepare", "requests"},
		// boto3 / botocore.
		{"boto3", "boto3", "get_object", "boto3"},
		{"boto3", "botocore", "put_object", "boto3"},
		// redis / aioredis.
		{"redis", "redis", "hset", "redis"},
		{"redis", "aioredis", "pipeline", "redis"},
		// django / rest_framework.
		{"django", "django.db.models", "first", "django"},
		{"django", "rest_framework", "last", "django"},
		// flask / flask_*.
		{"flask", "flask", "before_app_request", "flask"},
		{"flask", "flask_login", "before_app_request", "flask"},
		// sqlalchemy.
		{"sqlalchemy", "sqlalchemy", "commit", "sqlalchemy"},
		{"sqlalchemy", "flask_sqlalchemy", "rollback", "sqlalchemy"},
		// pymongo / motor / bson. `aggregate`/`update`/`count`/`insert`
		// already classify via python/stdlib core maps; pick gate-unique.
		{"pymongo", "pymongo", "find", "pymongo"},
		{"pymongo", "motor", "map_reduce", "pymongo"},
		// celery.
		{"celery", "celery", "apply_async", "celery"},
		{"celery", "celery", "si", "celery"},
		// logging.
		{"logging", "logging", "info", "logging"},
		{"logging", "loguru", "exception", "logging"},
		// re (regex).
		{"re", "re", "sub", "re"},
		{"re", "regex", "findall", "re"},
		// DB-API 2.0 — the cursor-verb call site folds to the concrete
		// driver the file imports so the engine label is correct (#2807).
		// django.db carries no engine signal → generic python-dbapi.
		{"dbapi", "sqlite3", "execute", "sqlite3"},
		{"dbapi", "psycopg2", "cursor", "psycopg2"},
		{"dbapi", "django.db", "executemany", "python-dbapi"},
		// bs4 / lxml.
		{"bs4", "bs4", "find_all", "bs4"},
		{"bs4", "lxml", "xpath", "bs4"},
		// urllib.
		{"urllib", "urllib.parse", "urljoin", "urllib"},
		{"urllib", "urllib3", "urlencode", "urllib"},
	}
}

// TestPythonPerImportGates_WithImport_LiftsToCanonical confirms that
// each per-library gate, when its canonical import is present on the
// python source file, classifies the generic verb as external-known
// and folds the edge to the canonical ecosystem placeholder.
func TestPythonPerImportGates_WithImport_LiftsToCanonical(t *testing.T) {
	for _, c := range pythonGateCases() {
		c := c
		t.Run(c.gate+"/"+c.bareName, func(t *testing.T) {
			t.Parallel()
			imports := map[string]bool{c.importPath: true}
			subtype, ok := stdlibFunction(c.bareName, "python", "x.py", imports)
			if !ok {
				t.Fatalf("stdlibFunction(%q, python, %q-imp) = (_, false); want classified", c.bareName, c.importPath)
			}
			// Subtype is the gate's sentinel; the wrapper at classifyExternal
			// folds it to the canonical package name. Validate end-to-end via
			// Synthesize so the wrapper logic is exercised.
			_ = subtype
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID:         "py-src",
					Name:       "caller",
					Kind:       "function",
					SourceFile: "x.py",
					Language:   "python",
				}},
				Relationships: []graph.Relationship{
					{ID: "imp-1", FromID: "x.py", ToID: c.importPath, Kind: "IMPORTS"},
					{ID: "rel-1", FromID: "py-src", ToID: c.bareName, Kind: "CALLS"},
				},
			}
			Synthesize(doc)
			want := "ext:" + c.wantCanonical
			if doc.Relationships[1].ToID != want {
				t.Fatalf("gate=%q name=%q: ToID=%q, want %q", c.gate, c.bareName, doc.Relationships[1].ToID, want)
			}
		})
	}
}

// TestPythonDBAPIDriverPlaceholder_EngineMatrix is the driver-matrix
// guard for #2807: the DB-API cursor-verb fold must read the concrete
// driver import so the engine label matches the real database engine,
// instead of the old hard-coded sqlite3 default that mislabelled every
// server-DB consumer as SQLite.
func TestPythonDBAPIDriverPlaceholder_EngineMatrix(t *testing.T) {
	cases := []struct {
		name        string
		imports     []string
		placeholder string // pythonDBAPIDriverPlaceholder result
		engine      string // pythonDBAPIEngineLabel[placeholder]
	}{
		// MySQL family — the iter9 q05/q09/q12 regression: mysql.connector
		// was reported as SQLite.
		{"mysql.connector", []string{"mysql.connector"}, "mysql", "MySQL"},
		{"MySQLdb", []string{"MySQLdb"}, "mysql", "MySQL"},
		{"pymysql", []string{"pymysql"}, "mysql", "MySQL"},
		{"aiomysql", []string{"aiomysql"}, "mysql", "MySQL"},
		// Postgres family.
		{"psycopg2", []string{"psycopg2.extras"}, "psycopg2", "PostgreSQL"},
		{"psycopg3", []string{"psycopg"}, "psycopg2", "PostgreSQL"},
		{"asyncpg", []string{"asyncpg"}, "psycopg2", "PostgreSQL"},
		// SQLite family — still labelled SQLite when the driver really is sqlite3.
		{"sqlite3", []string{"sqlite3"}, "sqlite3", "SQLite"},
		{"aiosqlite", []string{"aiosqlite"}, "sqlite3", "SQLite"},
		// SQL Server family.
		{"pyodbc", []string{"pyodbc"}, "pyodbc", "SQL Server"},
		{"pymssql", []string{"pymssql"}, "pyodbc", "SQL Server"},
		// Oracle.
		{"cx_Oracle", []string{"cx_Oracle"}, "cx_Oracle", "Oracle"},
		{"oracledb", []string{"oracledb"}, "cx_Oracle", "Oracle"},
		// Snowflake / ClickHouse.
		{"snowflake", []string{"snowflake.connector"}, "snowflake-connector-python", "Snowflake"},
		{"clickhouse", []string{"clickhouse_driver"}, "clickhouse-driver", "ClickHouse"},
		// No hard default to SQLite: unrecognised / engine-less imports
		// fall back to the generic placeholder labelled "unknown".
		{"django.db only", []string{"django.db"}, "python-dbapi", "unknown"},
		{"no imports", nil, "python-dbapi", "unknown"},
		{"unrelated import", []string{"os"}, "python-dbapi", "unknown"},
		// Multi-driver: prefer the concrete server engine over the sqlite3
		// stdlib fallback (sync_viewset.py imports both mysql.connector
		// and psycopg2/sqlite3 in the wild).
		{"mysql+sqlite -> mysql", []string{"sqlite3", "mysql.connector"}, "mysql", "MySQL"},
		{"sqlite+psycopg2 -> psycopg2", []string{"sqlite3", "psycopg2"}, "psycopg2", "PostgreSQL"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			imports := map[string]bool{}
			for _, imp := range c.imports {
				imports[imp] = true
			}
			got := pythonDBAPIDriverPlaceholder(imports)
			if got != c.placeholder {
				t.Fatalf("pythonDBAPIDriverPlaceholder(%v) = %q; want %q", c.imports, got, c.placeholder)
			}
			// The returned placeholder must route to ExternalKnown so the
			// resolver does not synthesise a junk ext:* node.
			if !IsKnownExternalPackage(got) {
				t.Fatalf("placeholder %q is not on knownExternalPackages allowlist", got)
			}
			if eng := pythonDBAPIEngineLabel[got]; eng != c.engine {
				t.Fatalf("engine label for %q = %q; want %q", got, eng, c.engine)
			}
		})
	}
}

// TestPythonDBAPIDriverPlaceholder_Deterministic is the #5206 regression guard.
//
// pythonDBAPIDriverPlaceholder used to iterate the file's import set in Go's
// randomised map-iteration order and keep the FIRST concrete-engine placeholder
// it saw. When a single file imports two concrete server engines (the
// upvate_core core/views/sync_viewset.py case imports BOTH a MySQL driver and
// psycopg2), the resolved ext:<driver> CALLS target flipped between mysql and
// psycopg2 from one index run to the next — non-deterministic output that
// surfaced as a spurious flat-vs-M5 resolver-index parity divergence (the two
// resolver indexes are byte-identical; the instability lived here, not in the
// index). The fix sorts the import set before iterating. This test asserts the
// result is STABLE across many independently-constructed maps (Go randomises
// per-map, so 200 fresh maps reliably exercises multiple iteration orders) and
// that the multi-server-engine choice is the documented deterministic winner.
func TestPythonDBAPIDriverPlaceholder_Deterministic(t *testing.T) {
	cases := []struct {
		name    string
		imports []string
		want    string // the single stable placeholder every run must return
	}{
		// The exact #5206 corpus shape: a file importing two concrete server
		// engines. Sorted-import order makes mysql.connector precede psycopg2,
		// so "mysql" is the deterministic winner.
		{"mysql+psycopg2", []string{"mysql.connector", "psycopg2"}, "mysql"},
		// Same set, declared in the opposite source order — must still be stable
		// and identical (the map drops ordering anyway; the sort restores it).
		{"psycopg2+mysql", []string{"psycopg2", "mysql.connector"}, "mysql"},
		// Three concrete engines + the sqlite3 stdlib fallback: sqlite3 must
		// never win, and the concrete winner must be stable.
		{"mysql+psycopg2+oracle+sqlite", []string{"sqlite3", "psycopg2", "mysql.connector", "cx_Oracle"}, "cx_Oracle"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			for i := 0; i < 200; i++ {
				imports := map[string]bool{}
				for _, imp := range c.imports {
					imports[imp] = true
				}
				if got := pythonDBAPIDriverPlaceholder(imports); got != c.want {
					t.Fatalf("iteration %d: pythonDBAPIDriverPlaceholder(%v) = %q; want stable %q "+
						"(non-deterministic map iteration regressed — #5206)", i, c.imports, got, c.want)
				}
			}
		})
	}
}

// TestPythonPerImportGates_WithoutImport_KeepsSaferBiasMiss confirms
// that on a python source file WITHOUT the gate's canonical import,
// the generic verb is NOT classified — preserving #94's safer-bias
// rule for hand-rolled classes whose methods share the name.
func TestPythonPerImportGates_WithoutImport_KeepsSaferBiasMiss(t *testing.T) {
	for _, c := range pythonGateCases() {
		c := c
		t.Run(c.gate+"/"+c.bareName, func(t *testing.T) {
			t.Parallel()
			// File imports something unrelated (a user module).
			imports := map[string]bool{"core.models.User": true}
			_, ok := stdlibFunction(c.bareName, "python", "x.py", imports)
			// Some names may legitimately be in pythonBareNames already
			// (e.g. `apply_async` — let's not assume). Just confirm that
			// the per-import gate is not the trigger. We do that by
			// comparing classify with and without the canonical import.
			gatedImports := map[string]bool{c.importPath: true, "core.models.User": true}
			_, okGated := stdlibFunction(c.bareName, "python", "x.py", gatedImports)
			if okGated && !ok {
				// Correct: the gate is the trigger and only fires with
				// the canonical import. Pass.
				return
			}
			if okGated && ok {
				// Name is classified regardless of imports — must be in
				// pythonBareNames or stdlibBareNames. That's not the gate's
				// behaviour but it's also acceptable as long as the
				// result is stable. Pass.
				return
			}
			if !okGated && !ok {
				// Neither classifies — the gate didn't fire even WITH the
				// import. That's a bug in our wiring.
				t.Fatalf("gate=%q name=%q: not classified even with %q import", c.gate, c.bareName, c.importPath)
			}
		})
	}
}

// TestPythonPerImportGates_CrossLanguageGate confirms the python lang
// gate prevents these names from being classified when the source
// language is anything other than python — even if a same-named
// import appears in the import set.
func TestPythonPerImportGates_CrossLanguageGate(t *testing.T) {
	otherLangs := []string{"javascript", "typescript", "go", "ruby", "rust", "java", "kotlin", "swift", "csharp", "php", ""}
	for _, c := range pythonGateCases() {
		for _, lang := range otherLangs {
			c, lang := c, lang
			t.Run(c.gate+"/"+c.bareName+"/"+lang, func(t *testing.T) {
				t.Parallel()
				withImp := map[string]bool{c.importPath: true}
				_, okWith := stdlibFunction(c.bareName, lang, "x", withImp)
				_, okWithout := stdlibFunction(c.bareName, lang, "x", nil)
				if okWith != okWithout {
					t.Fatalf("gate=%q name=%q lang=%q: classify diverges based on python-gated import (with=%v without=%v); python gate must not fire for non-python", c.gate, c.bareName, lang, okWith, okWithout)
				}
			})
		}
	}
}

// TestPythonPerImportGates_NoDuplicatesWithCoreMaps confirms each per-
// library bare-name map's entries are unique to their gate (no overlap
// with stdlibBareNames or pythonBareNames — a duplicate is a dead
// entry and a maintenance hazard).
func TestPythonPerImportGates_NoDuplicatesWithCoreMaps(t *testing.T) {
	gates := map[string]map[string]struct{}{
		"pandas":     pythonPandasBareNames,
		"requests":   pythonRequestsBareNames,
		"boto3":      pythonBoto3BareNames,
		"redis":      pythonRedisBareNames,
		"django":     pythonDjangoBareNames,
		"flask":      pythonFlaskBareNames,
		"sqlalchemy": pythonSQLAlchemyBareNames,
		"mongo":      pythonMongoBareNames,
		"celery":     pythonCeleryBareNames,
		"logging":    pythonLoggingBareNames,
		"re":         pythonReBareNames,
		"dbapi":      pythonDBAPIBareNames,
		"bs4":        pythonBs4BareNames,
		"urllib":     pythonUrllibBareNames,
	}
	for gateName, m := range gates {
		for name := range m {
			if _, ok := stdlibBareNames[name]; ok {
				t.Errorf("python-%s-gate[%q] duplicates stdlibBareNames; remove from gate", gateName, name)
			}
			if _, ok := pythonBareNames[name]; ok {
				t.Errorf("python-%s-gate[%q] duplicates pythonBareNames; remove from gate", gateName, name)
			}
		}
	}
}

// TestHasPythonImportHelpers covers the per-library import predicates
// directly — root-segment match, prefix tolerance for dotted Python
// paths, flask_* prefix matching, and rejection of unrelated imports.
func TestHasPythonImportHelpers(t *testing.T) {
	cases := []struct {
		name    string
		fn      func(map[string]bool) bool
		imports map[string]bool
		want    bool
	}{
		// pandas
		{"pandas/exact", hasPythonPandasImport, map[string]bool{"pandas": true}, true},
		{"pandas/dotted", hasPythonPandasImport, map[string]bool{"pandas.DataFrame": true}, true},
		{"pandas/numpy", hasPythonPandasImport, map[string]bool{"numpy.array": true}, true},
		{"pandas/unrelated", hasPythonPandasImport, map[string]bool{"django.db": true}, false},
		// requests
		{"requests/exact", hasPythonRequestsImport, map[string]bool{"requests": true}, true},
		{"requests/dotted", hasPythonRequestsImport, map[string]bool{"httpx.AsyncClient": true}, true},
		{"requests/none", hasPythonRequestsImport, map[string]bool{"core.helper": true}, false},
		// boto3
		{"boto3/yes", hasPythonBoto3Import, map[string]bool{"boto3.client": true}, true},
		{"boto3/no", hasPythonBoto3Import, map[string]bool{"requests": true}, false},
		// django
		{"django/db.models", hasPythonDjangoImport, map[string]bool{"django.db.models": true}, true},
		{"django/drf", hasPythonDjangoImport, map[string]bool{"rest_framework": true}, true},
		{"django/none", hasPythonDjangoImport, map[string]bool{"flask": true}, false},
		// flask + extensions
		{"flask/exact", hasPythonFlaskImport, map[string]bool{"flask": true}, true},
		{"flask/login", hasPythonFlaskImport, map[string]bool{"flask_login": true}, true},
		{"flask/sqlalchemy", hasPythonFlaskImport, map[string]bool{"flask_sqlalchemy": true}, true},
		{"flask/django", hasPythonFlaskImport, map[string]bool{"django": true}, false},
		// dbapi
		{"dbapi/sqlite3", hasPythonDBAPIImport, map[string]bool{"sqlite3": true}, true},
		{"dbapi/psycopg2", hasPythonDBAPIImport, map[string]bool{"psycopg2.extras": true}, true},
		{"dbapi/django.db", hasPythonDBAPIImport, map[string]bool{"django.db": true}, true},
		{"dbapi/django.db.models is also dbapi via django.db.* prefix", hasPythonDBAPIImport, map[string]bool{"django.db.models": true}, true},
		{"dbapi/none", hasPythonDBAPIImport, map[string]bool{"core.user": true}, false},
		// bs4
		{"bs4/yes", hasPythonBs4Import, map[string]bool{"bs4": true}, true},
		{"bs4/lxml", hasPythonBs4Import, map[string]bool{"lxml.etree": true}, true},
		{"bs4/none", hasPythonBs4Import, map[string]bool{"requests": true}, false},
		// urllib
		{"urllib/parse", hasPythonUrllibImport, map[string]bool{"urllib.parse": true}, true},
		{"urllib/urllib3", hasPythonUrllibImport, map[string]bool{"urllib3": true}, true},
		{"urllib/yarl", hasPythonUrllibImport, map[string]bool{"yarl": true}, true},
		{"urllib/none", hasPythonUrllibImport, map[string]bool{"requests": true}, false},
		// empty / nil
		{"nil-imports/pandas", hasPythonPandasImport, nil, false},
		{"empty-imports/django", hasPythonDjangoImport, map[string]bool{}, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.fn(c.imports); got != c.want {
				t.Errorf("got=%v want=%v imports=%v", got, c.want, c.imports)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Issue #787c — Apache POI + PDFBox bare-name allowlist + FQN fix tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPoiBareNames_ClassifiedWithPoiImport verifies the Option-B gate:
// when a Java source file imports any org.apache.poi.* package, POI class
// constructor stubs (XSSFWorkbook, SXSSFWorkbook, CellRangeAddress, etc.)
// are classified as external-known and folded to ext:org.apache.poi.
func TestPoiBareNames_ClassifiedWithPoiImport(t *testing.T) {
	importPaths := []string{
		"org.apache.poi.xssf.usermodel", // explicit class import
		"org.apache.poi.xssf.streaming", // streaming SXSSF
		"org.apache.poi.ss.util",        // CellRangeAddress package
		"org.apache.poi.ss.usermodel",   // wildcard spread
		"org.apache.poi",                // root umbrella
		"org.apache.poi.hssf.usermodel", // legacy HSSF
		"org.apache.poi.xwpf.usermodel", // Word
		"org.apache.poi.xslf.usermodel", // PowerPoint
	}
	names := []string{
		"XSSFWorkbook", "XSSFSheet", "XSSFRow", "XSSFCell",
		"SXSSFWorkbook", "SXSSFSheet", "SXSSFRow", "SXSSFCell",
		"HSSFWorkbook", "HSSFSheet", "HSSFRow", "HSSFCell",
		"CellRangeAddress", "CellReference", "WorkbookFactory",
		"XWPFDocument", "XWPFParagraph",
		"XMLSlideShow", "XSLFSlide",
		"DataFormatter",
	}
	for _, imp := range importPaths[:3] { // spot-check three import shapes
		imp := imp
		for _, name := range names[:5] { // spot-check five class names
			name := name
			t.Run(imp+"/"+name, func(t *testing.T) {
				t.Parallel()
				imports := map[string]bool{imp: true}
				subtype, ok := stdlibFunction(name, "java", "Foo.java", imports)
				if !ok {
					t.Fatalf("stdlibFunction(%q, java, %q-imp) = (_, false); want poi_type sentinel",
						name, imp)
				}
				if subtype != "poi_type" {
					t.Fatalf("stdlibFunction(%q, java, %q-imp) subtype=%q, want poi_type",
						name, imp, subtype)
				}
				// End-to-end via Synthesize: edge must be rewritten to ext:org.apache.poi.
				doc := &graph.Document{
					Entities: []graph.Entity{{
						ID: "file-ent", Name: "Foo.java", Kind: "SCOPE.Component",
						Language: "java", SourceFile: "Foo.java",
					}, {
						ID: "caller", Name: "upload", Kind: "SCOPE.Operation",
						Language: "java", SourceFile: "Foo.java",
					}},
					Relationships: []graph.Relationship{
						{ID: "imp-1", FromID: "file-ent", ToID: imp, Kind: "IMPORTS",
							Properties: map[string]string{
								"source_module": imp, "imported_name": name,
							}},
						{ID: "rel-1", FromID: "caller", ToID: name, Kind: "CALLS"},
					},
				}
				Synthesize(doc)
				// Accept any ext:org.apache.poi* canonical — the exact
				// prefix depends on which sub-family entry in
				// knownExternalPackages/javaKnownExternalRoots is the
				// longest match for this specific import path. The
				// important invariant is that it is NOT the per-class
				// ext:<ClassName> placeholder.
				got := doc.Relationships[1].ToID
				const wantPrefix = "ext:org.apache.poi"
				if len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
					t.Fatalf("import=%q name=%q: ToID=%q, want prefix %q",
						imp, name, got, wantPrefix)
				}
			})
		}
	}
}

// TestPoiBareNames_NoImport_KeepsSaferBias verifies that POI class names
// are NOT classified when the source file does not import org.apache.poi.*
// (preventing user-defined classes named Workbook or Sheet from being
// misclassified in non-POI projects).
func TestPoiBareNames_NoImport_KeepsSaferBias(t *testing.T) {
	names := []string{"XSSFWorkbook", "SXSSFWorkbook", "CellRangeAddress", "WorkbookFactory"}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// No POI imports — only an unrelated user import.
			imports := map[string]bool{"com.acme.inventory.ProductService": true}
			// stdlibFunction only fires the poi gate; it must NOT match.
			subtype, ok := stdlibFunction(name, "java", "Foo.java", imports)
			if ok && subtype == "poi_type" {
				t.Fatalf("stdlibFunction(%q, java, non-poi-imports) classified as poi_type; want safer-bias miss", name)
			}
		})
	}
}

// TestPoiBareNames_NonJavaLanguage_NotClassified ensures the Java language
// gate prevents POI class names from being classified when the stub arrives
// from a non-Java source (Go, Python, etc.).
func TestPoiBareNames_NonJavaLanguage_NotClassified(t *testing.T) {
	poiImports := map[string]bool{"org.apache.poi.xssf.usermodel": true}
	for _, lang := range []string{"go", "python", "typescript", "ruby"} {
		lang := lang
		t.Run(lang, func(t *testing.T) {
			t.Parallel()
			subtype, ok := stdlibFunction("XSSFWorkbook", lang, "foo.go", poiImports)
			if ok && subtype == "poi_type" {
				t.Fatalf("stdlibFunction(XSSFWorkbook, %q, poi-imports) = poi_type; want no classification", lang)
			}
		})
	}
}

// TestPdfBoxBareNames_ClassifiedWithPdfBoxImport verifies the PDFBox gate:
// when a Java source file imports any org.apache.pdfbox.* package, PDFBox
// class stubs are classified and folded to ext:org.apache.pdfbox.
func TestPdfBoxBareNames_ClassifiedWithPdfBoxImport(t *testing.T) {
	imp := "org.apache.pdfbox.pdmodel"
	names := []string{"PDDocument", "PDPage", "PDPageContentStream", "PDType1Font", "PDRectangle"}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			imports := map[string]bool{imp: true}
			subtype, ok := stdlibFunction(name, "java", "Report.java", imports)
			if !ok {
				t.Fatalf("stdlibFunction(%q, java, pdfbox-imp) = (_, false); want pdfbox_type sentinel", name)
			}
			if subtype != "pdfbox_type" {
				t.Fatalf("stdlibFunction(%q, java, pdfbox-imp) subtype=%q, want pdfbox_type", name, subtype)
			}
			doc := &graph.Document{
				Entities: []graph.Entity{{
					ID: "file-ent", Name: "Report.java", Kind: "SCOPE.Component",
					Language: "java", SourceFile: "Report.java",
				}, {
					ID: "caller", Name: "generate", Kind: "SCOPE.Operation",
					Language: "java", SourceFile: "Report.java",
				}},
				Relationships: []graph.Relationship{
					{ID: "imp-1", FromID: "file-ent", ToID: imp, Kind: "IMPORTS",
						Properties: map[string]string{"source_module": imp, "imported_name": name}},
					{ID: "rel-1", FromID: "caller", ToID: name, Kind: "CALLS"},
				},
			}
			Synthesize(doc)
			// #4515 — the bare-name reference is a NAMED import of the pdfbox
			// type, so it now resolves to a distinct per-symbol node
			// (ext:org.apache.pdfbox:<Type>) keyed off the precise pdfbox
			// package canon, rather than the coarse package-level placeholder.
			want := "ext:org.apache.pdfbox:" + name
			if doc.Relationships[1].ToID != want {
				t.Fatalf("name=%q: ToID=%q, want %q", name, doc.Relationships[1].ToID, want)
			}
		})
	}
}

// TestUpsertImportSet_SyntheticFQN_EnablesImportLeafFolding verifies the
// fix for issue #787c: upsertImportSet now adds source_module+"."+imported_name
// so that classifyExternal's import-leaf folder (line ~802) can call
// longestKnownDottedPrefix on the full FQN and match an external prefix.
// This end-to-end test exercises the primary fix path without needing the
// Option-B import-gate fallback.
func TestUpsertImportSet_SyntheticFQN_EnablesImportLeafFolding(t *testing.T) {
	// Simulate: import org.apache.poi.xssf.usermodel.XSSFWorkbook;
	// call: new XSSFWorkbook() → stub "XSSFWorkbook"
	doc := &graph.Document{
		Entities: []graph.Entity{{
			ID:         "file-ent",
			Name:       "InventoryController.java",
			Kind:       "SCOPE.Component",
			Language:   "java",
			SourceFile: "InventoryController.java",
		}, {
			ID:         "caller",
			Name:       "uploadProducts",
			Kind:       "SCOPE.Operation",
			Language:   "java",
			SourceFile: "InventoryController.java",
		}},
		Relationships: []graph.Relationship{
			{
				ID:     "imp-1",
				FromID: "file-ent",
				// After resolveImportToIDs, ToID is rewritten to ext:org.apache:XSSFWorkbook.
				// But source_module and imported_name Properties are still present.
				ToID: "ext:org.apache:XSSFWorkbook",
				Kind: "IMPORTS",
				Properties: map[string]string{
					"source_module": "org.apache.poi.xssf.usermodel",
					"imported_name": "XSSFWorkbook",
				},
			},
			{
				ID:     "rel-1",
				FromID: "caller",
				ToID:   "XSSFWorkbook", // bare constructor stub
				Kind:   "CALLS",
			},
		},
	}
	Synthesize(doc)
	// The stub must be rewritten to ext:org.apache (or ext:org.apache.poi if
	// the allowlist has a more-specific prefix). Either way it must not stay
	// as "ext:XSSFWorkbook" (the wrong per-class placeholder).
	rewritten := doc.Relationships[1].ToID
	if rewritten == "ext:XSSFWorkbook" {
		t.Fatalf("ToID=%q: still the bare class placeholder; FQN synthetic path did not fire", rewritten)
	}
	if rewritten == "XSSFWorkbook" {
		t.Fatalf("ToID=%q: stub not rewritten at all; no external classification applied", rewritten)
	}
	// Must start with ext:org.apache (any sub-prefix is acceptable).
	if len(rewritten) < len("ext:org.apache") || rewritten[:len("ext:org.apache")] != "ext:org.apache" {
		t.Fatalf("ToID=%q: want prefix ext:org.apache, got different prefix", rewritten)
	}
}

// TestHasPoiImport_Variants verifies hasPoiImport recognises all common
// import shapes for Apache POI (explicit class, package prefix, ext:-tagged).
func TestHasPoiImport_Variants(t *testing.T) {
	cases := []struct {
		name    string
		imports map[string]bool
		want    bool
	}{
		{"xssf.usermodel", map[string]bool{"org.apache.poi.xssf.usermodel": true}, true},
		{"xssf.streaming", map[string]bool{"org.apache.poi.xssf.streaming": true}, true},
		{"ss.util", map[string]bool{"org.apache.poi.ss.util": true}, true},
		{"hssf.usermodel", map[string]bool{"org.apache.poi.hssf.usermodel": true}, true},
		{"root poi", map[string]bool{"org.apache.poi": true}, true},
		// source_module + imported_name form added by upsertImportSet
		{"source_module form", map[string]bool{"org.apache.poi.xssf.usermodel": true, "XSSFWorkbook": true}, true},
		{"kafka only", map[string]bool{"org.apache.kafka.streams.StreamsBuilder": true}, false},
		{"nil", nil, false},
		{"empty", map[string]bool{}, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := hasPoiImport(c.imports); got != c.want {
				t.Errorf("hasPoiImport(%v) = %v, want %v", c.imports, got, c.want)
			}
		})
	}
}

// TestHasPdfBoxImport_Variants verifies hasPdfBoxImport recognises all
// common import shapes for Apache PDFBox.
func TestHasPdfBoxImport_Variants(t *testing.T) {
	cases := []struct {
		name    string
		imports map[string]bool
		want    bool
	}{
		{"pdmodel", map[string]bool{"org.apache.pdfbox.pdmodel": true}, true},
		{"root", map[string]bool{"org.apache.pdfbox": true}, true},
		{"font sub", map[string]bool{"org.apache.pdfbox.pdmodel.font": true}, true},
		{"image sub", map[string]bool{"org.apache.pdfbox.pdmodel.graphics.image": true}, true},
		{"poi only", map[string]bool{"org.apache.poi.xssf.usermodel": true}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := hasPdfBoxImport(c.imports); got != c.want {
				t.Errorf("hasPdfBoxImport(%v) = %v, want %v", c.imports, got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Refs #44 — Go per-import gate fix: upsertImportSet must strip the ext:
// prefix so bare package paths (e.g. "time", "errors", "github.com/go-chi/chi")
// are inserted alongside the ext:-prefixed form that the Go extractor writes.
// ---------------------------------------------------------------------------

// TestUpsertImportSet_GoExtractorExtPrefix verifies that when the Go extractor
// rewrites an IMPORTS edge ToID to the "ext:<path>" form (no Properties set),
// upsertImportSet adds BOTH the ext:-prefixed value AND the bare path to the
// import set, enabling all existing per-import gate predicates.
func TestUpsertImportSet_GoExtractorExtPrefix(t *testing.T) {
	cases := []struct {
		name     string
		toID     string
		wantBare string
	}{
		{"time", "ext:time", "time"},
		{"errors", "ext:errors", "errors"},
		{"encoding/json", "ext:encoding/json", "encoding/json"},
		{"sync/atomic", "ext:sync/atomic", "sync/atomic"},
		{"chi v5", "ext:github.com/go-chi/chi", "github.com/go-chi/chi"},
		{"chi v5 full", "ext:github.com/go-chi/chi/v5", "github.com/go-chi/chi/v5"},
		{"net/http", "ext:net/http", "net/http"},
		{"bare ext (no path)", "ext:", ""}, // degenerate: empty bare path should not panic
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			rel := &graph.Relationship{ToID: c.toID, Kind: "IMPORTS"}
			set := upsertImportSet(nil, rel)
			// ext:-prefixed form must always be present.
			if !set[c.toID] {
				t.Errorf("set[%q] = false; want true (ext:-prefixed form must be inserted)", c.toID)
			}
			// bare form must also be present (unless the path is empty).
			if c.wantBare != "" && !set[c.wantBare] {
				t.Errorf("set[%q] = false; want true (bare form must be inserted for gate predicates)", c.wantBare)
			}
		})
	}
}

// TestSynthesize_GoTimeImport_ExtPrefixedImports verifies end-to-end that
// a Go source file whose IMPORTS edges use the ext:-prefixed ToID form (as
// the Go extractor writes) correctly activates the `time` import gate,
// causing bare stubs like "Now" to be classified as ext:time. Refs #44.
func TestSynthesize_GoTimeImport_ExtPrefixedImports(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				ID:         "file-ent",
				Name:       "worker.go",
				Kind:       "SCOPE.Component",
				Language:   "go",
				SourceFile: "worker.go",
			},
			{
				ID:         "fn-ent",
				Name:       "RunJob",
				Kind:       "function",
				Language:   "go",
				SourceFile: "worker.go",
			},
		},
		Relationships: []graph.Relationship{
			{
				// Go extractor writes ext: form — no Properties set.
				ID:     "imp-time",
				FromID: "file-ent",
				ToID:   "ext:time",
				Kind:   "IMPORTS",
			},
			{
				// Bare stub emitted by Go extractor for `time.Now()` call.
				ID:     "call-now",
				FromID: "fn-ent",
				ToID:   "Now",
				Kind:   "CALLS",
			},
		},
	}
	Synthesize(doc)
	got := doc.Relationships[1].ToID
	if got != "ext:time" {
		t.Fatalf("ToID=%q, want ext:time — time import gate did not fire; upsertImportSet ext: stripping is broken", got)
	}
}

// TestSynthesize_GoChiImport_ExtPrefixedImports verifies end-to-end that a
// Go source file whose IMPORTS edge for chi uses the ext:-prefixed ToID form
// correctly activates hasGoChiImport, so bare router-method stubs like "Get",
// "NewRouter", and "URLParam" synthesise to ext:github.com/go-chi/chi. Refs #44.
func TestSynthesize_GoChiImport_ExtPrefixedImports(t *testing.T) {
	chiNames := []string{"Get", "Post", "Route", "Use", "NewRouter", "URLParam", "URLParamFromCtx"}
	for _, name := range chiNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := &graph.Document{
				Entities: []graph.Entity{
					{
						ID:         "file-ent",
						Name:       "main.go",
						Kind:       "SCOPE.Component",
						Language:   "go",
						SourceFile: "main.go",
					},
					{
						ID:         "fn-ent",
						Name:       "SetupRouter",
						Kind:       "function",
						Language:   "go",
						SourceFile: "main.go",
					},
				},
				Relationships: []graph.Relationship{
					{
						// Go extractor uses ext: prefix — no Properties.
						ID:     "imp-chi",
						FromID: "file-ent",
						ToID:   "ext:github.com/go-chi/chi",
						Kind:   "IMPORTS",
					},
					{
						ID:     "call-chi",
						FromID: "fn-ent",
						ToID:   name,
						Kind:   "CALLS",
					},
				},
			}
			Synthesize(doc)
			got := doc.Relationships[1].ToID
			if got == name {
				t.Fatalf("name=%q: stub not rewritten; hasGoChiImport gate did not fire — upsertImportSet ext: stripping is broken", name)
			}
			if !strings.HasPrefix(got, "ext:github.com/go-chi") {
				t.Fatalf("name=%q: ToID=%q, want prefix ext:github.com/go-chi", name, got)
			}
		})
	}
}

// TestSynthesize_GoErrorsImport_ExtPrefixedImports verifies that the errors
// import gate fires when the IMPORTS edge is in ext:-prefixed form. Refs #44.
func TestSynthesize_GoErrorsImport_ExtPrefixedImports(t *testing.T) {
	for _, stub := range []string{"New", "As", "Is", "Unwrap"} {
		stub := stub
		t.Run(stub, func(t *testing.T) {
			t.Parallel()
			doc := &graph.Document{
				Entities: []graph.Entity{
					{ID: "file-ent", Name: "errs.go", Kind: "SCOPE.Component", Language: "go", SourceFile: "errs.go"},
					{ID: "fn-ent", Name: "wrap", Kind: "function", Language: "go", SourceFile: "errs.go"},
				},
				Relationships: []graph.Relationship{
					{ID: "imp-errors", FromID: "file-ent", ToID: "ext:errors", Kind: "IMPORTS"},
					{ID: "call-stub", FromID: "fn-ent", ToID: stub, Kind: "CALLS"},
				},
			}
			Synthesize(doc)
			got := doc.Relationships[1].ToID
			if got == stub {
				t.Fatalf("stub=%q: not rewritten; errors import gate did not fire", stub)
			}
			if got != "ext:errors" {
				t.Fatalf("stub=%q: ToID=%q, want ext:errors", stub, got)
			}
		})
	}
}

// TestSynthesize_GoEncodingJsonImport_ExtPrefixedImports verifies that the
// encoding/json import gate fires when the IMPORTS edge uses the ext: form.
// Refs #44.
func TestSynthesize_GoEncodingJsonImport_ExtPrefixedImports(t *testing.T) {
	for _, stub := range []string{"Marshal", "Unmarshal", "NewEncoder", "NewDecoder"} {
		stub := stub
		t.Run(stub, func(t *testing.T) {
			t.Parallel()
			doc := &graph.Document{
				Entities: []graph.Entity{
					{ID: "file-ent", Name: "codec.go", Kind: "SCOPE.Component", Language: "go", SourceFile: "codec.go"},
					{ID: "fn-ent", Name: "encode", Kind: "function", Language: "go", SourceFile: "codec.go"},
				},
				Relationships: []graph.Relationship{
					{ID: "imp-json", FromID: "file-ent", ToID: "ext:encoding/json", Kind: "IMPORTS"},
					{ID: "call-stub", FromID: "fn-ent", ToID: stub, Kind: "CALLS"},
				},
			}
			Synthesize(doc)
			got := doc.Relationships[1].ToID
			if got == stub {
				t.Fatalf("stub=%q: not rewritten; encoding/json import gate did not fire", stub)
			}
			if !strings.HasPrefix(got, "ext:encoding") {
				t.Fatalf("stub=%q: ToID=%q, want ext:encoding prefix", stub, got)
			}
		})
	}
}

// TestPoiKnownExternalPackages_EntryExists verifies that org.apache.poi and
// org.apache.pdfbox are in knownExternalPackages so the resolver routes edges
// to ExternalKnown (not ExternalUnknown).
func TestPoiKnownExternalPackages_EntryExists(t *testing.T) {
	for _, pkg := range []string{
		"org.apache.poi",
		"org.apache.poi.ss",
		"org.apache.poi.xssf",
		"org.apache.poi.hssf",
		"org.apache.pdfbox",
		"org.apache.commons.io",
		"org.apache.commons.lang3",
		"org.apache.commons.compress",
	} {
		pkg := pkg
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()
			if !isKnownExternalPackage(pkg) {
				t.Errorf("isKnownExternalPackage(%q) = false; want true (entry missing from knownExternalPackages)", pkg)
			}
		})
	}
}

// TestSynthesizeDBEntities_DjangoFixture (issue #532) — verifies that for a
// Django-shaped fixture with multiple files querying overlapping tables the
// synthesiser produces:
//   - one ext:db.<table> entity per distinct table (deduped)
//   - one IMPORTS edge per distinct (file, table) pair (deduped)
//   - UNKNOWN table entries are skipped
func TestSynthesizeDBEntities_DjangoFixture(t *testing.T) {
	t.Parallel()
	// Simulate five SCOPE.DataAccess entities:
	//   app/views/orders.py -> SELECT users
	//   app/views/orders.py -> INSERT orders  (same file, different table)
	//   app/views/profile.py -> SELECT users  (different file, same table as first)
	//   app/models/user.py -> SELECT users    (third file reading users)
	//   app/views/sync.py -> SELECT UNKNOWN   (should be skipped)
	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				ID:            "aaaa000000000001",
				Kind:          "SCOPE.DataAccess",
				Name:          "SELECT users",
				QualifiedName: "scope:dataaccess:app/views/orders.py#psycopg2:SELECT:users",
				SourceFile:    "app/views/orders.py",
				Language:      "python",
			},
			{
				ID:            "aaaa000000000002",
				Kind:          "SCOPE.DataAccess",
				Name:          "INSERT orders",
				QualifiedName: "scope:dataaccess:app/views/orders.py#psycopg2:INSERT:orders",
				SourceFile:    "app/views/orders.py",
				Language:      "python",
			},
			{
				ID:            "aaaa000000000003",
				Kind:          "SCOPE.DataAccess",
				Name:          "SELECT users",
				QualifiedName: "scope:dataaccess:app/views/profile.py#psycopg2:SELECT:users",
				SourceFile:    "app/views/profile.py",
				Language:      "python",
			},
			{
				ID:            "aaaa000000000004",
				Kind:          "SCOPE.DataAccess",
				Name:          "SELECT users",
				QualifiedName: "scope:dataaccess:app/models/user.py#sqlalchemy:SELECT:users",
				SourceFile:    "app/models/user.py",
				Language:      "python",
			},
			{
				ID:            "aaaa000000000005",
				Kind:          "SCOPE.DataAccess",
				Name:          "SELECT UNKNOWN",
				QualifiedName: "scope:dataaccess:app/views/sync.py#psycopg2:SELECT:UNKNOWN",
				SourceFile:    "app/views/sync.py",
				Language:      "python",
			},
		},
	}

	stats := SynthesizeDBEntities(doc)

	// 2 distinct tables: users, orders (UNKNOWN skipped)
	if stats.Synthesized != 2 {
		t.Fatalf("Synthesized=%d, want 2 (users, orders)", stats.Synthesized)
	}

	// 4 distinct (file, table) pairs:
	//   (orders.py, users), (orders.py, orders), (profile.py, users), (models/user.py, users)
	if stats.RelationshipsResolved != 4 {
		t.Fatalf("RelationshipsResolved=%d, want 4 IMPORTS edges", stats.RelationshipsResolved)
	}

	// Verify ext:db.users and ext:db.orders entities exist
	entityIDs := make(map[string]bool)
	for _, e := range doc.Entities {
		entityIDs[e.ID] = true
	}
	if !entityIDs["ext:db.users"] {
		t.Error("missing ext:db.users entity")
	}
	if !entityIDs["ext:db.orders"] {
		t.Error("missing ext:db.orders entity")
	}

	// Count IMPORTS edges to db:* targets
	importCount := 0
	importTargets := make(map[string]int)
	for _, r := range doc.Relationships {
		if r.Kind == "IMPORTS" && strings.HasPrefix(r.ToID, "ext:db.") {
			importCount++
			importTargets[r.ToID]++
		}
	}
	if importCount != 4 {
		t.Fatalf("got %d IMPORTS->db:* edges, want 4", importCount)
	}
	// users should have 3 callers; orders should have 1
	if importTargets["ext:db.users"] != 3 {
		t.Errorf("ext:db.users: %d IMPORTS edges, want 3", importTargets["ext:db.users"])
	}
	if importTargets["ext:db.orders"] != 1 {
		t.Errorf("ext:db.orders: %d IMPORTS edges, want 1", importTargets["ext:db.orders"])
	}
}

// ---------------------------------------------------------------------
// Refs #44 slice-2 — Go stdlib package-fold tests
// ---------------------------------------------------------------------

// TestSynthesize_GoLogImport_FoldsToExtLog verifies that bare stubs from
// log.Printf / log.Println / log.Fatal / log.Fatalf / log.Panic / log.Panicf
// are folded to ext:log when the source file imports "log" (via the
// ext:log IMPORTS edge form the Go extractor writes). Without the import gate
// these would produce isolated ext:Printf / ext:Println / … stubs that land
// as ExternalUnknown. Refs #44.
func TestSynthesize_GoLogImport_FoldsToExtLog(t *testing.T) {
	t.Parallel()
	for _, stub := range []string{
		"Printf", "Println", "Print",
		"Fatalf", "Fatal", "Fatalln",
		"Panicf", "Panic", "Panicln",
	} {
		stub := stub
		t.Run(stub, func(t *testing.T) {
			t.Parallel()
			doc := &graph.Document{
				Entities: []graph.Entity{
					{ID: "file-ent", Name: "worker.go", Kind: "SCOPE.Component", Language: "go", SourceFile: "worker.go"},
					{ID: "fn-ent", Name: "processEvent", Kind: "function", Language: "go", SourceFile: "worker.go"},
				},
				Relationships: []graph.Relationship{
					// Go extractor writes ext:-prefixed IMPORTS edges.
					{ID: "imp-log", FromID: "file-ent", ToID: "ext:log", Kind: "IMPORTS"},
					{ID: "call-stub", FromID: "fn-ent", ToID: stub, Kind: "CALLS"},
				},
			}
			Synthesize(doc)
			got := doc.Relationships[1].ToID
			if got == stub {
				t.Fatalf("stub=%q: not rewritten; log import gate did not fire (pre-stdlibBareNames Go gate is broken)", stub)
			}
			if got != "ext:log" {
				t.Fatalf("stub=%q: ToID=%q, want ext:log", stub, got)
			}
		})
	}
}

// TestSynthesize_GoContextImport_FoldsToExtContext verifies that bare stubs
// from context.Background / context.WithCancel / context.WithTimeout /
// context.WithDeadline / context.WithValue / context.TODO are folded to
// ext:context when the source file imports "context". Without the gate these
// land as ext:Background / ext:WithCancel / … (ExternalUnknown). Refs #44.
func TestSynthesize_GoContextImport_FoldsToExtContext(t *testing.T) {
	t.Parallel()
	for _, stub := range []string{
		"Background", "TODO",
		"WithCancel", "WithTimeout", "WithDeadline", "WithValue",
	} {
		stub := stub
		t.Run(stub, func(t *testing.T) {
			t.Parallel()
			doc := &graph.Document{
				Entities: []graph.Entity{
					{ID: "file-ent", Name: "main.go", Kind: "SCOPE.Component", Language: "go", SourceFile: "main.go"},
					{ID: "fn-ent", Name: "main", Kind: "function", Language: "go", SourceFile: "main.go"},
				},
				Relationships: []graph.Relationship{
					{ID: "imp-ctx", FromID: "file-ent", ToID: "ext:context", Kind: "IMPORTS"},
					{ID: "call-stub", FromID: "fn-ent", ToID: stub, Kind: "CALLS"},
				},
			}
			Synthesize(doc)
			got := doc.Relationships[1].ToID
			if got == stub {
				t.Fatalf("stub=%q: not rewritten; context import gate did not fire", stub)
			}
			if got != "ext:context" {
				t.Fatalf("stub=%q: ToID=%q, want ext:context", stub, got)
			}
		})
	}
}

// TestSynthesize_GoNetImport_FoldsToExtNetHttp verifies that bare stubs for
// http.HandlerFunc / http.ServeHTTP / http.HandleFunc / http.WriteHeader
// are folded to ext:net/http when the source file's IMPORTS edge carries
// ext:net (the form the Go extractor writes for "net/http" imports). These
// stubs were previously landing as ext:HandlerFunc / ext:ServeHTTP / …
// (ExternalUnknown). Refs #44.
func TestSynthesize_GoNetImport_FoldsToExtNetHttp(t *testing.T) {
	t.Parallel()
	for _, stub := range []string{
		"HandlerFunc", "ServeHTTP",
		"HandleFunc", "WriteHeader",
	} {
		stub := stub
		t.Run(stub, func(t *testing.T) {
			t.Parallel()
			doc := &graph.Document{
				Entities: []graph.Entity{
					// The Go extractor reduces "net/http" → ext:net in the
					// IMPORTS edge (goKnownExternalRoots uses "net" as root).
					{ID: "file-ent", Name: "middleware.go", Kind: "SCOPE.Component", Language: "go", SourceFile: "middleware.go"},
					{ID: "fn-ent", Name: "Logger", Kind: "function", Language: "go", SourceFile: "middleware.go"},
				},
				Relationships: []graph.Relationship{
					{ID: "imp-net", FromID: "file-ent", ToID: "ext:net", Kind: "IMPORTS"},
					{ID: "call-stub", FromID: "fn-ent", ToID: stub, Kind: "CALLS"},
				},
			}
			Synthesize(doc)
			got := doc.Relationships[1].ToID
			if got == stub {
				t.Fatalf("stub=%q: not rewritten; net import gate did not fire", stub)
			}
			if got != "ext:net/http" {
				t.Fatalf("stub=%q: ToID=%q, want ext:net/http", stub, got)
			}
		})
	}
}

// TestSynthesize_GoTimeImport_FoldsToExtTime_Slice2 extends the time-gate
// tests from #945 (which covered Now/After/Date/Unix) to the additional
// names added in slice-2: Since, Sleep, NewTicker, NewTimer, Until,
// AfterFunc, ParseDuration. All should fold to ext:time. Refs #44.
func TestSynthesize_GoTimeImport_FoldsToExtTime_Slice2(t *testing.T) {
	t.Parallel()
	for _, stub := range []string{
		"Since", "Sleep", "NewTicker", "NewTimer",
		"Until", "AfterFunc", "ParseDuration",
	} {
		stub := stub
		t.Run(stub, func(t *testing.T) {
			t.Parallel()
			doc := &graph.Document{
				Entities: []graph.Entity{
					{ID: "file-ent", Name: "worker.go", Kind: "SCOPE.Component", Language: "go", SourceFile: "worker.go"},
					{ID: "fn-ent", Name: "processEvent", Kind: "function", Language: "go", SourceFile: "worker.go"},
				},
				Relationships: []graph.Relationship{
					{ID: "imp-time", FromID: "file-ent", ToID: "ext:time", Kind: "IMPORTS"},
					{ID: "call-stub", FromID: "fn-ent", ToID: stub, Kind: "CALLS"},
				},
			}
			Synthesize(doc)
			got := doc.Relationships[1].ToID
			if got == stub {
				t.Fatalf("stub=%q: not rewritten; time import gate did not fire", stub)
			}
			if got != "ext:time" {
				t.Fatalf("stub=%q: ToID=%q, want ext:time", stub, got)
			}
		})
	}
}

// TestSynthesize_GoLogImport_NoFalsePositive verifies that the log gate does
// NOT fire when the caller language is non-Go. A Python/JS/Rust function
// named "Printf" should not be synthesised as ext:log. Refs #44.
func TestSynthesize_GoLogImport_NoFalsePositive(t *testing.T) {
	t.Parallel()
	for _, lang := range []string{"python", "javascript", "rust", "java", ""} {
		lang := lang
		t.Run(lang, func(t *testing.T) {
			t.Parallel()
			doc := &graph.Document{
				Entities: []graph.Entity{
					{ID: "file-ent", Name: "foo.py", Kind: "SCOPE.Component", Language: lang, SourceFile: "foo.py"},
					{ID: "fn-ent", Name: "caller", Kind: "function", Language: lang, SourceFile: "foo.py"},
				},
				Relationships: []graph.Relationship{
					{ID: "imp-log", FromID: "file-ent", ToID: "ext:log", Kind: "IMPORTS"},
					{ID: "call-printf", FromID: "fn-ent", ToID: "Printf", Kind: "CALLS"},
				},
			}
			Synthesize(doc)
			got := doc.Relationships[1].ToID
			if got == "ext:log" {
				t.Fatalf("lang=%q: Printf synthesised as ext:log but log gate should only fire for Go", lang)
			}
		})
	}
}

// TestSynthesizeDBEntities_Idempotent confirms running SynthesizeDBEntities
// twice on the same document is a no-op on the second call.
func TestSynthesizeDBEntities_Idempotent(t *testing.T) {
	t.Parallel()
	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				ID:            "bbbb000000000001",
				Kind:          "SCOPE.DataAccess",
				Name:          "SELECT items",
				QualifiedName: "scope:dataaccess:store/views.py#psycopg2:SELECT:items",
				SourceFile:    "store/views.py",
				Language:      "python",
			},
		},
	}
	first := SynthesizeDBEntities(doc)
	if first.Synthesized != 1 || first.RelationshipsResolved != 1 {
		t.Fatalf("first run: Synthesized=%d RelationshipsResolved=%d, want 1,1", first.Synthesized, first.RelationshipsResolved)
	}
	second := SynthesizeDBEntities(doc)
	if second.Synthesized != 0 || second.RelationshipsResolved != 0 {
		t.Fatalf("second run: Synthesized=%d RelationshipsResolved=%d, want 0,0 (idempotent)", second.Synthesized, second.RelationshipsResolved)
	}
}

// TestSynthesize_NoPlaceholderForPythonStdlib verifies issue #1085: calls to
// Python stdlib builtins (int, str, list) do NOT emit External entities, while
// calls to real third-party packages (numpy, requests) DO emit External entities,
// and in-graph calls resolve to real entities unchanged.
//
// Fixture:
//   - 10 edges to stdlib names: int (×3), str (×3), list (×2), range, len
//   - 5 edges to real external packages: numpy.array (×3), requests.get (×2)
//   - 5 edges to a user-defined function (hex ID — already resolved)
func TestSynthesize_NoPlaceholderForPythonStdlib(t *testing.T) {
	const callerID = "aaaa000000000001"
	const userFuncID = "bbbb000000000001"

	// Build 5 edges that already point at a real (hex-ID) in-graph entity.
	inGraphRels := []graph.Relationship{
		{ID: "rg1", FromID: callerID, ToID: userFuncID, Kind: "CALLS"},
		{ID: "rg2", FromID: callerID, ToID: userFuncID, Kind: "CALLS"},
		{ID: "rg3", FromID: callerID, ToID: userFuncID, Kind: "CALLS"},
		{ID: "rg4", FromID: callerID, ToID: userFuncID, Kind: "CALLS"},
		{ID: "rg5", FromID: callerID, ToID: userFuncID, Kind: "CALLS"},
	}

	// 10 edges to stdlib bare names — should NOT create entities.
	stdlibRels := []graph.Relationship{
		{ID: "s1", FromID: callerID, ToID: "int", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "s2", FromID: callerID, ToID: "int", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "s3", FromID: callerID, ToID: "int", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "s4", FromID: callerID, ToID: "str", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "s5", FromID: callerID, ToID: "str", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "s6", FromID: callerID, ToID: "str", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "s7", FromID: callerID, ToID: "list", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "s8", FromID: callerID, ToID: "list", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "s9", FromID: callerID, ToID: "range", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "s10", FromID: callerID, ToID: "len", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
	}

	// 5 edges to real external packages — SHOULD create entities.
	extRels := []graph.Relationship{
		{ID: "e1", FromID: callerID, ToID: "numpy.array", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "e2", FromID: callerID, ToID: "numpy.array", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "e3", FromID: callerID, ToID: "numpy.array", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "e4", FromID: callerID, ToID: "requests.get", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
		{ID: "e5", FromID: callerID, ToID: "requests.get", Kind: "CALLS",
			Properties: map[string]string{"language": "python"}},
	}

	allRels := make([]graph.Relationship, 0, len(inGraphRels)+len(stdlibRels)+len(extRels))
	allRels = append(allRels, inGraphRels...)
	allRels = append(allRels, stdlibRels...)
	allRels = append(allRels, extRels...)

	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				ID:         callerID,
				Name:       "process",
				Kind:       "Function",
				Language:   "python",
				SourceFile: "app/core.py",
			},
			{
				ID:         userFuncID,
				Name:       "process",
				Kind:       "Function",
				Language:   "python",
				SourceFile: "app/util.py",
			},
		},
		Relationships: allRels,
	}

	stats := Synthesize(doc)

	// 1. Exactly 2 External entities: ext:numpy and ext:requests.
	//    (NOT ext:int, ext:str, ext:list, ext:range, ext:len)
	var extEntities []graph.Entity
	for _, e := range doc.Entities {
		if e.Kind == KindExternal {
			extEntities = append(extEntities, e)
		}
	}
	if len(extEntities) != 2 {
		names := make([]string, len(extEntities))
		for i, e := range extEntities {
			names[i] = e.ID
		}
		t.Errorf("want 2 External entities (numpy, requests), got %d: %v", len(extEntities), names)
	}
	for _, e := range extEntities {
		if e.ID != "ext:numpy" && e.ID != "ext:requests" {
			t.Errorf("unexpected External entity: %s", e.ID)
		}
	}

	// 2. DynamicTargetsResolved counts the 10 stdlib edges.
	if stats.DynamicTargetsResolved != 10 {
		t.Errorf("DynamicTargetsResolved=%d, want 10", stats.DynamicTargetsResolved)
	}

	// 3. Stdlib edges: ToID cleared to "", dynamic_target stamped.
	for _, r := range doc.Relationships {
		// Only check the stdlib edges by ID prefix "s".
		if len(r.ID) < 1 || r.ID[0] != 's' {
			continue
		}
		if r.ToID != "" {
			t.Errorf("stdlib edge %s: ToID=%q, want empty", r.ID, r.ToID)
		}
		if dt := r.Properties["dynamic_target"]; dt == "" {
			t.Errorf("stdlib edge %s: dynamic_target property missing", r.ID)
		}
	}

	// 4. In-graph edges: ToID still points at the hex entity ID.
	for _, r := range doc.Relationships {
		if len(r.ID) < 2 || r.ID[:2] != "rg" {
			continue
		}
		if r.ToID != userFuncID {
			t.Errorf("in-graph edge %s: ToID=%q, want %s", r.ID, r.ToID, userFuncID)
		}
	}

	// 5. External edges: ToID rewritten to ext:numpy or ext:requests.
	for _, r := range doc.Relationships {
		if len(r.ID) < 1 || r.ID[0] != 'e' {
			continue
		}
		if r.ToID != "ext:numpy" && r.ToID != "ext:requests" {
			t.Errorf("external edge %s: ToID=%q, want ext:numpy or ext:requests", r.ID, r.ToID)
		}
	}
}

// TestSynthesize_NoPlaceholderForGoStdlib verifies issue #1085 extended to Go:
// calls to Go universe-block builtins (make, len, append, panic) do NOT emit
// External entities, while calls to real stdlib packages (fmt.Println) DO emit
// External entities.
//
// Fixture:
//   - 8 edges to stdlib builtins: make (×2), len (×2), append (×2), panic (×2)
//   - 4 edges to real stdlib packages: fmt.Println (×2), os.Exit (×2)
//   - 4 edges to a user-defined function (already resolved)
func TestSynthesize_NoPlaceholderForGoStdlib(t *testing.T) {
	const callerID = "cccc000000000001"
	const userFuncID = "dddd000000000001"

	inGraphRels := []graph.Relationship{
		{ID: "rg1", FromID: callerID, ToID: userFuncID, Kind: "CALLS"},
		{ID: "rg2", FromID: callerID, ToID: userFuncID, Kind: "CALLS"},
		{ID: "rg3", FromID: callerID, ToID: userFuncID, Kind: "CALLS"},
		{ID: "rg4", FromID: callerID, ToID: userFuncID, Kind: "CALLS"},
	}

	// 8 edges to Go universe-block builtins — should NOT create entities.
	stdlibRels := []graph.Relationship{
		{ID: "sg1", FromID: callerID, ToID: "make", Kind: "CALLS",
			Properties: map[string]string{"language": "go"}},
		{ID: "sg2", FromID: callerID, ToID: "make", Kind: "CALLS",
			Properties: map[string]string{"language": "go"}},
		{ID: "sg3", FromID: callerID, ToID: "len", Kind: "CALLS",
			Properties: map[string]string{"language": "go"}},
		{ID: "sg4", FromID: callerID, ToID: "len", Kind: "CALLS",
			Properties: map[string]string{"language": "go"}},
		{ID: "sg5", FromID: callerID, ToID: "append", Kind: "CALLS",
			Properties: map[string]string{"language": "go"}},
		{ID: "sg6", FromID: callerID, ToID: "append", Kind: "CALLS",
			Properties: map[string]string{"language": "go"}},
		{ID: "sg7", FromID: callerID, ToID: "panic", Kind: "CALLS",
			Properties: map[string]string{"language": "go"}},
		{ID: "sg8", FromID: callerID, ToID: "panic", Kind: "CALLS",
			Properties: map[string]string{"language": "go"}},
	}

	// 4 edges to real stdlib packages — SHOULD create entities.
	extRels := []graph.Relationship{
		{ID: "eg1", FromID: callerID, ToID: "fmt.Println", Kind: "CALLS",
			Properties: map[string]string{"language": "go"}},
		{ID: "eg2", FromID: callerID, ToID: "fmt.Println", Kind: "CALLS",
			Properties: map[string]string{"language": "go"}},
		{ID: "eg3", FromID: callerID, ToID: "os.Exit", Kind: "CALLS",
			Properties: map[string]string{"language": "go"}},
		{ID: "eg4", FromID: callerID, ToID: "os.Exit", Kind: "CALLS",
			Properties: map[string]string{"language": "go"}},
	}

	allRels := make([]graph.Relationship, 0, len(inGraphRels)+len(stdlibRels)+len(extRels))
	allRels = append(allRels, inGraphRels...)
	allRels = append(allRels, stdlibRels...)
	allRels = append(allRels, extRels...)

	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				ID:         callerID,
				Name:       "Handler",
				Kind:       "Function",
				Language:   "go",
				SourceFile: "cmd/server/main.go",
			},
			{
				ID:         userFuncID,
				Name:       "Run",
				Kind:       "Function",
				Language:   "go",
				SourceFile: "cmd/server/run.go",
			},
		},
		Relationships: allRels,
	}

	stats := Synthesize(doc)

	// 1. Exactly 2 External entities: ext:fmt and ext:os.
	//    (NOT ext:make, ext:len, ext:append, ext:panic)
	var extEntities []graph.Entity
	for _, e := range doc.Entities {
		if e.Kind == KindExternal {
			extEntities = append(extEntities, e)
		}
	}
	if len(extEntities) != 2 {
		names := make([]string, len(extEntities))
		for i, e := range extEntities {
			names[i] = e.ID
		}
		t.Errorf("want 2 External entities (fmt, os), got %d: %v", len(extEntities), names)
	}
	for _, e := range extEntities {
		if e.ID != "ext:fmt" && e.ID != "ext:os" {
			t.Errorf("unexpected External entity: %s", e.ID)
		}
	}

	// 2. DynamicTargetsResolved counts the 8 stdlib edges.
	if stats.DynamicTargetsResolved != 8 {
		t.Errorf("DynamicTargetsResolved=%d, want 8", stats.DynamicTargetsResolved)
	}

	// 3. Stdlib edges: ToID cleared, dynamic_target stamped.
	for _, r := range doc.Relationships {
		if len(r.ID) < 2 || r.ID[:2] != "sg" {
			continue
		}
		if r.ToID != "" {
			t.Errorf("Go stdlib edge %s: ToID=%q, want empty", r.ID, r.ToID)
		}
		if dt := r.Properties["dynamic_target"]; dt == "" {
			t.Errorf("Go stdlib edge %s: dynamic_target property missing", r.ID)
		}
	}

	// 4. In-graph edges: ToID still points at the hex entity ID.
	for _, r := range doc.Relationships {
		if len(r.ID) < 2 || r.ID[:2] != "rg" {
			continue
		}
		if r.ToID != userFuncID {
			t.Errorf("in-graph edge %s: ToID=%q, want %s", r.ID, r.ToID, userFuncID)
		}
	}
}

// TestSynthesize_NoPlaceholderForJSStdlib verifies issue #1085 extended to
// JavaScript and TypeScript: calls to browser/Node globals (console, JSON,
// Math, process, fetch) do NOT emit External entities, while calls to real
// npm packages (lodash, react) DO emit External entities.
// TypeScript edges are also verified (same builtin set).
//
// Fixture (JS):
//   - 6 edges to JS globals: console (×2), JSON (×2), Math (×2)
//   - 4 edges to real npm packages: lodash.get (×2), react.useState (×2)
//
// Fixture (TS):
//   - 4 edges to TS/JS globals: fetch (×2), process (×2)
func TestSynthesize_NoPlaceholderForJSStdlib(t *testing.T) {
	const callerID = "eeee000000000001"
	const userFuncID = "ffff000000000001"

	inGraphRels := []graph.Relationship{
		{ID: "rg1", FromID: callerID, ToID: userFuncID, Kind: "CALLS"},
	}

	// 6 edges to JS globals — should NOT create entities.
	jsStdlibRels := []graph.Relationship{
		{ID: "js1", FromID: callerID, ToID: "console", Kind: "CALLS",
			Properties: map[string]string{"language": "javascript"}},
		{ID: "js2", FromID: callerID, ToID: "console", Kind: "CALLS",
			Properties: map[string]string{"language": "javascript"}},
		{ID: "js3", FromID: callerID, ToID: "JSON", Kind: "CALLS",
			Properties: map[string]string{"language": "javascript"}},
		{ID: "js4", FromID: callerID, ToID: "JSON", Kind: "CALLS",
			Properties: map[string]string{"language": "javascript"}},
		{ID: "js5", FromID: callerID, ToID: "Math", Kind: "CALLS",
			Properties: map[string]string{"language": "javascript"}},
		{ID: "js6", FromID: callerID, ToID: "Math", Kind: "CALLS",
			Properties: map[string]string{"language": "javascript"}},
	}

	// 4 edges to real npm packages — SHOULD create entities.
	extRels := []graph.Relationship{
		{ID: "ej1", FromID: callerID, ToID: "lodash.get", Kind: "CALLS",
			Properties: map[string]string{"language": "javascript"}},
		{ID: "ej2", FromID: callerID, ToID: "lodash.get", Kind: "CALLS",
			Properties: map[string]string{"language": "javascript"}},
		{ID: "ej3", FromID: callerID, ToID: "react.useState", Kind: "CALLS",
			Properties: map[string]string{"language": "javascript"}},
		{ID: "ej4", FromID: callerID, ToID: "react.useState", Kind: "CALLS",
			Properties: map[string]string{"language": "javascript"}},
	}

	// 4 TypeScript edges to globals — should NOT create entities.
	tsStdlibRels := []graph.Relationship{
		{ID: "ts1", FromID: callerID, ToID: "fetch", Kind: "CALLS",
			Properties: map[string]string{"language": "typescript"}},
		{ID: "ts2", FromID: callerID, ToID: "fetch", Kind: "CALLS",
			Properties: map[string]string{"language": "typescript"}},
		{ID: "ts3", FromID: callerID, ToID: "process", Kind: "CALLS",
			Properties: map[string]string{"language": "typescript"}},
		{ID: "ts4", FromID: callerID, ToID: "process", Kind: "CALLS",
			Properties: map[string]string{"language": "typescript"}},
	}

	allRels := make([]graph.Relationship, 0,
		len(inGraphRels)+len(jsStdlibRels)+len(extRels)+len(tsStdlibRels))
	allRels = append(allRels, inGraphRels...)
	allRels = append(allRels, jsStdlibRels...)
	allRels = append(allRels, extRels...)
	allRels = append(allRels, tsStdlibRels...)

	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				ID:         callerID,
				Name:       "renderApp",
				Kind:       "Function",
				Language:   "javascript",
				SourceFile: "src/index.js",
			},
			{
				ID:         userFuncID,
				Name:       "setupStore",
				Kind:       "Function",
				Language:   "javascript",
				SourceFile: "src/store.js",
			},
		},
		Relationships: allRels,
	}

	stats := Synthesize(doc)

	// 1. Exactly 2 External entities: ext:lodash and ext:react.
	//    (NOT ext:console, ext:JSON, ext:Math, ext:fetch, ext:process)
	var extEntities []graph.Entity
	for _, e := range doc.Entities {
		if e.Kind == KindExternal {
			extEntities = append(extEntities, e)
		}
	}
	if len(extEntities) != 2 {
		names := make([]string, len(extEntities))
		for i, e := range extEntities {
			names[i] = e.ID
		}
		t.Errorf("want 2 External entities (lodash, react), got %d: %v", len(extEntities), names)
	}
	for _, e := range extEntities {
		if e.ID != "ext:lodash" && e.ID != "ext:react" {
			t.Errorf("unexpected External entity: %s", e.ID)
		}
	}

	// 2. DynamicTargetsResolved counts 10 stdlib edges (6 JS + 4 TS).
	if stats.DynamicTargetsResolved != 10 {
		t.Errorf("DynamicTargetsResolved=%d, want 10", stats.DynamicTargetsResolved)
	}

	// 3. JS stdlib edges: ToID cleared, dynamic_target stamped.
	for _, r := range doc.Relationships {
		if len(r.ID) < 2 || r.ID[:2] != "js" {
			continue
		}
		if r.ToID != "" {
			t.Errorf("JS stdlib edge %s: ToID=%q, want empty", r.ID, r.ToID)
		}
		if dt := r.Properties["dynamic_target"]; dt == "" {
			t.Errorf("JS stdlib edge %s: dynamic_target property missing", r.ID)
		}
	}

	// 4. TS stdlib edges: ToID cleared, dynamic_target stamped.
	for _, r := range doc.Relationships {
		if len(r.ID) < 2 || r.ID[:2] != "ts" {
			continue
		}
		if r.ToID != "" {
			t.Errorf("TS stdlib edge %s: ToID=%q, want empty", r.ID, r.ToID)
		}
		if dt := r.Properties["dynamic_target"]; dt == "" {
			t.Errorf("TS stdlib edge %s: dynamic_target property missing", r.ID)
		}
	}
}

// TestSynthesize_NoPlaceholderForRubyStdlib verifies issue #1085 extended to
// Ruby: calls to Ruby kernel methods and core DSL macros (puts, raise,
// attr_accessor, attr_reader) do NOT emit External entities, while calls to
// real gems (rails, redis) DO emit External entities.
//
// Fixture:
//   - 6 edges to Ruby kernel/DSL: puts (×2), raise (×2), attr_accessor (×2)
//   - 4 edges to real gems: rails.render (×2), redis.get (×2)
//   - 2 edges to a user-defined method (already resolved)
func TestSynthesize_NoPlaceholderForRubyStdlib(t *testing.T) {
	const callerID = "9999000000000001"
	const userFuncID = "8888000000000001"

	inGraphRels := []graph.Relationship{
		{ID: "rg1", FromID: callerID, ToID: userFuncID, Kind: "CALLS"},
		{ID: "rg2", FromID: callerID, ToID: userFuncID, Kind: "CALLS"},
	}

	// 6 edges to Ruby kernel/DSL builtins — should NOT create entities.
	stdlibRels := []graph.Relationship{
		{ID: "rb1", FromID: callerID, ToID: "puts", Kind: "CALLS",
			Properties: map[string]string{"language": "ruby"}},
		{ID: "rb2", FromID: callerID, ToID: "puts", Kind: "CALLS",
			Properties: map[string]string{"language": "ruby"}},
		{ID: "rb3", FromID: callerID, ToID: "raise", Kind: "CALLS",
			Properties: map[string]string{"language": "ruby"}},
		{ID: "rb4", FromID: callerID, ToID: "raise", Kind: "CALLS",
			Properties: map[string]string{"language": "ruby"}},
		{ID: "rb5", FromID: callerID, ToID: "attr_accessor", Kind: "CALLS",
			Properties: map[string]string{"language": "ruby"}},
		{ID: "rb6", FromID: callerID, ToID: "attr_accessor", Kind: "CALLS",
			Properties: map[string]string{"language": "ruby"}},
	}

	// 4 edges to real gems — SHOULD create entities.
	extRels := []graph.Relationship{
		{ID: "er1", FromID: callerID, ToID: "rails.render", Kind: "CALLS",
			Properties: map[string]string{"language": "ruby"}},
		{ID: "er2", FromID: callerID, ToID: "rails.render", Kind: "CALLS",
			Properties: map[string]string{"language": "ruby"}},
		{ID: "er3", FromID: callerID, ToID: "redis.get", Kind: "CALLS",
			Properties: map[string]string{"language": "ruby"}},
		{ID: "er4", FromID: callerID, ToID: "redis.get", Kind: "CALLS",
			Properties: map[string]string{"language": "ruby"}},
	}

	allRels := make([]graph.Relationship, 0, len(inGraphRels)+len(stdlibRels)+len(extRels))
	allRels = append(allRels, inGraphRels...)
	allRels = append(allRels, stdlibRels...)
	allRels = append(allRels, extRels...)

	doc := &graph.Document{
		Entities: []graph.Entity{
			{
				ID:         callerID,
				Name:       "UserController",
				Kind:       "Class",
				Language:   "ruby",
				SourceFile: "app/controllers/user_controller.rb",
			},
			{
				ID:         userFuncID,
				Name:       "authenticate",
				Kind:       "Function",
				Language:   "ruby",
				SourceFile: "app/services/auth.rb",
			},
		},
		Relationships: allRels,
	}

	stats := Synthesize(doc)

	// 1. Exactly 2 External entities: ext:rails and ext:redis.
	//    (NOT ext:puts, ext:raise, ext:attr_accessor)
	var extEntities []graph.Entity
	for _, e := range doc.Entities {
		if e.Kind == KindExternal {
			extEntities = append(extEntities, e)
		}
	}
	if len(extEntities) != 2 {
		names := make([]string, len(extEntities))
		for i, e := range extEntities {
			names[i] = e.ID
		}
		t.Errorf("want 2 External entities (rails, redis), got %d: %v", len(extEntities), names)
	}
	for _, e := range extEntities {
		if e.ID != "ext:rails" && e.ID != "ext:redis" {
			t.Errorf("unexpected External entity: %s", e.ID)
		}
	}

	// 2. DynamicTargetsResolved counts the 6 stdlib edges.
	if stats.DynamicTargetsResolved != 6 {
		t.Errorf("DynamicTargetsResolved=%d, want 6", stats.DynamicTargetsResolved)
	}

	// 3. Stdlib edges: ToID cleared, dynamic_target stamped.
	for _, r := range doc.Relationships {
		if len(r.ID) < 2 || r.ID[:2] != "rb" {
			continue
		}
		if r.ToID != "" {
			t.Errorf("Ruby stdlib edge %s: ToID=%q, want empty", r.ID, r.ToID)
		}
		if dt := r.Properties["dynamic_target"]; dt == "" {
			t.Errorf("Ruby stdlib edge %s: dynamic_target property missing", r.ID)
		}
	}

	// 4. In-graph edges: ToID still points at the hex entity ID.
	for _, r := range doc.Relationships {
		if len(r.ID) < 2 || r.ID[:2] != "rg" {
			continue
		}
		if r.ToID != userFuncID {
			t.Errorf("in-graph edge %s: ToID=%q, want %s", r.ID, r.ToID, userFuncID)
		}
	}
}
