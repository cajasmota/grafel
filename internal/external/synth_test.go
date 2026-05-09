package external

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

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
			if _, ok := stdlibFunction(name); ok {
				t.Fatalf("stdlibFunction(%q) classified as stdlib bare-name; "+
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
			subtype, ok := stdlibFunction(name)
			if !ok {
				t.Fatalf("stdlibFunction(%q) = (_, false); want classified as stdlib bare-name", name)
			}
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q) subtype=%q, want %q", name, subtype, "function")
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
