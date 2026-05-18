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
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"rust\", nil) subtype=%q, want %q", name, subtype, "function")
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
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
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
			if subtype != "function" {
				t.Fatalf("stdlibFunction(%q, \"rust\", nil) subtype=%q, want %q", name, subtype, "function")
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
			want := "ext:" + name
			if doc.Relationships[0].ToID != want {
				t.Fatalf("ToID=%q, want %q", doc.Relationships[0].ToID, want)
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
	names := []string{"request", "respond", "install", "launch", "async", "static", "headers", "parameters"}
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
	// Pick names with the highest cross-language collision potential —
	// generic verbs/accessors that exist as user methods in every
	// ecosystem. Selection rule: each name MUST be unique to
	// swiftBareNames (i.e. not in stdlibBareNames or any other
	// language map), otherwise the cross-language gate test would
	// trip on a different language's allowlist firing first.
	// `body` and `register` removed — now also classified by
	// rustBareNames (Actix-web DSL, #440).
	names := []string{
		"query", "auth", "session", "cookies",
		"boot", "shutdown", "grouped", "redirect",
		"render", "middleware", "authorize", "protect",
		"paginate", "transform", "flatMap", "offset",
	}
	otherLangs := []string{"go", "python", "javascript", "ruby", "rust", "java", "kotlin", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"swift\" only)", name, lang)
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
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-Swift)",
						doc.Relationships[0].ToID, name)
				}
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
		"destroy", "valid?", "build", "first", "last",
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
// user methods on any class and MUST NOT be in the Ruby allowlist.
func TestRubyBareNames_RejectedNamesNotClassified(t *testing.T) {
	rejected := []string{"each", "map", "select", "find", "count", "length", "size"}
	for _, name := range rejected {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := rubyBareNames[name]; ok {
				t.Fatalf("rubyBareNames[%q] present; must be rejected per issue #107 (collision-prone)", name)
			}
		})
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
// receiver (`r.Get("/x", h)` → `Get`) must classify as stdlib bare-names
// — but only when (a) the source entity's language is "go" AND (b) the
// source file's IMPORTS edges include any canonical go-chi import path.
// The dual gate keeps these collision-prone names from shadowing user
// methods like `Repository.Get` in non-chi code.
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
				if subtype != "function" {
					t.Fatalf("stdlibFunction(%q, \"go\", chi=%q) subtype=%q, want %q",
						name, chiPath, subtype, "function")
				}
				// End-to-end: a Go entity in a file that emits an IMPORTS
				// edge to the chi package must rewrite a CALLS edge with a
				// chi-router method name to ext:<name>.
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
				want := "ext:" + name
				// Find the CALLS edge and check it was rewritten.
				var got string
				for _, r := range doc.Relationships {
					if r.ID == "rel-call" {
						got = r.ToID
						break
					}
				}
				if got != want {
					t.Fatalf("CALLS edge ToID=%q, want %q "+
						"(name=%q, chi import=%q)", got, want, name, chiPath)
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
	}
	otherLangs := []string{"go", "php", "javascript", "ruby", "rust", "java", "kotlin", "swift", "csharp", ""}
	for _, name := range names {
		for _, lang := range otherLangs {
			name, lang := name, lang
			t.Run(name+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if _, ok := stdlibFunction(name, lang, "", nil); ok {
					t.Fatalf("stdlibFunction(%q, %q, nil) classified; want fall-through "+
						"(name is gated to lang=\"python\" only)", name, lang)
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
					t.Fatalf("ToID=%q, want %q (must not be rewritten for non-Python)",
						doc.Relationships[0].ToID, name)
				}
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
