package external

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #6337 — external synthesis for VB.NET EXTENDS / IMPLEMENTS targets.
//
// Before this change every arm of classifyExternal was gated on a relKind of
// IMPORTS / CALLS or a structural-ref stub prefix, so a hierarchy target fell
// through to the generic dotted-root fallback. That fallback did two wrong
// things and one useless one:
//
//   - bare `Form` / `IDisposable` matched nothing and stayed a raw stub, which
//     internal/feedback counts as bug-extractor (resolve.IsBugEdgeToID);
//   - dotted `System.Windows.Forms.Form` collapsed to `ext:System`, so every
//     BCL type in the graph shared ONE node — the grouping is real but it
//     groups the wrong thing;
//   - dotted `Windows.Forms.TextBox` resolved to `ext:Windows` on the
//     authority of the RUST `windows` crate entry in knownExternalPackages.
//
// The tests below pin the exact ext IDs, not merely "it resolved". Asserting
// resolve.IsResolvedToID alone is vacuous here: the pre-change dotted forms
// ALREADY satisfied it via ext:System, and a mutant returning a bare
// `"System"` instead of `"dotnet:System..."` would keep every such assertion
// green while silently reverting the ecosystem tag (#6337's named mutant).

// vbDoc builds a one-relationship document with a single vbnet caller plus any
// extra in-tree entities the mask-guard cases need.
func vbDoc(kind, toID string, extra ...graph.Entity) *graph.Document {
	ents := []graph.Entity{{
		ID:         "0123456789abcdef",
		Name:       "Widget",
		Kind:       "SCOPE.Component",
		SourceFile: "Widget.vb",
		Language:   "vbnet",
	}}
	ents = append(ents, extra...)
	return &graph.Document{
		Entities: ents,
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "0123456789abcdef", ToID: toID, Kind: kind},
		},
	}
}

func TestVBNetHierarchyExternalSynthesis_6337(t *testing.T) {
	cases := []struct {
		name    string
		relKind string
		toID    string
		extra   []graph.Entity
		want    string // expected ToID after Synthesize
		subtype string // expected placeholder subtype ("" when unresolved)
	}{
		{
			name:    "bare BCL leaf EXTENDS",
			relKind: "EXTENDS",
			toID:    "Form",
			want:    "ext:dotnet:Form",
			subtype: "type",
		},
		{
			name:    "bare BCL leaf IMPLEMENTS",
			relKind: "IMPLEMENTS",
			toID:    "IDisposable",
			want:    "ext:dotnet:IDisposable",
			subtype: "type",
		},
		{
			// The dotted form used to fold to ext:System. Type identity is the
			// whole point of the node — every Form subclass under one node,
			// not every BCL type under one node.
			name:    "dotted System base keeps type identity",
			relKind: "EXTENDS",
			toID:    "System.Windows.Forms.Form",
			want:    "ext:dotnet:System.Windows.Forms.Form",
			subtype: "type",
		},
		{
			// The eight-way collision the issue names. `Windows` is a Rust
			// crate in knownExternalPackages; without the dotnet arm running
			// FIRST this resolves to ext:Windows on that authority.
			name:    "Windows root does not resolve through the Rust crate",
			relKind: "EXTENDS",
			toID:    "Windows.Forms.TextBox",
			want:    "ext:dotnet:Windows.Forms.TextBox",
			subtype: "type",
		},
		{
			// Member-level `Implements IDisposable.Dispose`. isVBExternalBaseType
			// alone answers false here (root `IDisposable` is not a framework
			// root namespace); the type/member split is what recovers it.
			name:    "member-level external clause",
			relKind: "IMPLEMENTS",
			toID:    "IDisposable.Dispose",
			want:    "ext:dotnet:IDisposable:Dispose",
			subtype: "member",
		},
		{
			// THE MASK GUARD. A WinForms app that declares its own partial
			// `Panel` must keep the unresolved edge visible — that ambiguity
			// is the partial-class defect, not an external base.
			name:    "in-tree type of the same name is NOT masked",
			relKind: "EXTENDS",
			toID:    "Panel",
			extra: []graph.Entity{{
				ID: "fedcba9876543210", Name: "Panel", Kind: "SCOPE.Component",
				SourceFile: "Panel.vb", Language: "vbnet",
			}},
			want: "Panel",
		},
		{
			// The 16 measured in-tree member misses (IFrameServer.*) are a
			// resolver-routing defect. They must stay dangling.
			name:    "in-tree member target is NOT masked",
			relKind: "IMPLEMENTS",
			toID:    "IFrameServer.GetFrame",
			extra: []graph.Entity{{
				ID: "fedcba9876543210", Name: "IFrameServer", Kind: "SCOPE.Component",
				SourceFile: "FrameServer.vb", Language: "vbnet",
			}},
			want: "IFrameServer.GetFrame",
		},
		{
			// The typePart half of the mask guard, which the case above does
			// NOT reach (`IFrameServer` is rejected by the allowlist before the
			// guard is consulted). Here the type IS on the allowlist and IS
			// declared in-tree — a repo that ships its own `IDisposable` — so
			// the member clause must stay visible too.
			name:    "in-tree type behind a member target is NOT masked",
			relKind: "IMPLEMENTS",
			toID:    "IDisposable.Dispose",
			extra: []graph.Entity{{
				ID: "fedcba9876543210", Name: "IDisposable", Kind: "SCOPE.Component",
				SourceFile: "IDisposable.vb", Language: "vbnet",
			}},
			want: "IDisposable.Dispose",
		},
		{
			// A typo is not a framework base. It must remain a bug edge so the
			// extractor defect it represents is still reported.
			name:    "unknown bare name stays a bug edge",
			relKind: "EXTENDS",
			toID:    "Fomr",
			want:    "Fomr",
		},
		{
			// relKind gate — the arm must not swallow ambiguous CALLS targets.
			name:    "CALLS is not a hierarchy edge",
			relKind: "CALLS",
			toID:    "IDisposable",
			want:    "IDisposable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := vbDoc(tc.relKind, tc.toID, tc.extra...)
			Synthesize(doc)
			got := doc.Relationships[0].ToID
			if got != tc.want {
				t.Fatalf("ToID = %q, want %q", got, tc.want)
			}
			if tc.subtype == "" {
				return
			}
			var found bool
			for _, e := range doc.Entities {
				if e.ID != tc.want {
					continue
				}
				found = true
				if e.Kind != KindExternal {
					t.Errorf("placeholder kind = %q, want %q", e.Kind, KindExternal)
				}
				if e.Subtype != tc.subtype {
					t.Errorf("placeholder subtype = %q, want %q", e.Subtype, tc.subtype)
				}
				if e.SourceFile != "" {
					t.Errorf("placeholder SourceFile = %q, want empty", e.SourceFile)
				}
			}
			if !found {
				t.Fatalf("no placeholder entity with ID %q", tc.want)
			}
		})
	}
}

// TestVBNetHierarchyIsLanguageGated_6337 pins the #94 safer-bias rule: the same
// bare name from a non-vbnet caller must not be routed to the dotnet canon.
func TestVBNetHierarchyIsLanguageGated_6337(t *testing.T) {
	for _, lang := range []string{"csharp", "java", "go", ""} {
		doc := vbDoc("EXTENDS", "IDisposable")
		doc.Entities[0].Language = lang
		Synthesize(doc)
		if got := doc.Relationships[0].ToID; got == "ext:dotnet:IDisposable" {
			t.Errorf("lang=%q: ToID = %q, want the vbnet gate to hold", lang, got)
		}
	}
}

// TestVBNetHierarchyMaskGuardIgnoresExternalNodes_6337 pins the one exclusion
// in buildInTreeNameSet. A previously-synthesised external placeholder is not
// an in-tree declaration, and the #4515 per-symbol path names such nodes after
// the bare imported symbol — so without the exclusion an `ext:` node named
// `Form` would suppress the very edge the arm exists to resolve, and a second
// Synthesize run would disagree with the first.
func TestVBNetHierarchyMaskGuardIgnoresExternalNodes_6337(t *testing.T) {
	doc := vbDoc("EXTENDS", "Form", graph.Entity{
		ID: "ext:somepkg:Form", Name: "Form", Kind: KindExternal, Subtype: "symbol",
	})
	Synthesize(doc)
	if got := doc.Relationships[0].ToID; got != "ext:dotnet:Form" {
		t.Fatalf("ToID = %q, want ext:dotnet:Form (an ext: node is not an in-tree declaration)", got)
	}
}

// TestVBNetHierarchySynthesizeIsIdempotent_6337 — Synthesize must be re-runnable
// on its own output. The second pass sees an already-`ext:` ToID and skips it;
// this asserts no duplicate placeholder and no ToID churn.
func TestVBNetHierarchySynthesizeIsIdempotent_6337(t *testing.T) {
	doc := vbDoc("IMPLEMENTS", "IDisposable.Dispose")
	Synthesize(doc)
	first := doc.Relationships[0].ToID
	n := len(doc.Entities)
	Synthesize(doc)
	if got := doc.Relationships[0].ToID; got != first {
		t.Fatalf("ToID churned on re-run: %q -> %q", first, got)
	}
	if len(doc.Entities) != n {
		t.Fatalf("entities grew on re-run: %d -> %d", n, len(doc.Entities))
	}
}

// TestVBNetHierarchyDoesNotBorrowGoIO_6337 is the ecosystem control the issue
// asks for by name. `IO` is a .NET root namespace AND `io` is a Go stdlib
// package sitting in knownExternalPackages. The dotnet canon must be a
// DIFFERENT node from the Go one, and the Go edge must be untouched.
//
// This is the assertion the named mutant fails: an arm that returns the bare
// root (`"IO"` / `"System"`) instead of the ecosystem-tagged canon keeps every
// IsResolvedToID assertion green and collides the two ecosystems here.
func TestVBNetHierarchyDoesNotBorrowGoIO_6337(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "0123456789abcdef", Name: "Writer", Kind: "SCOPE.Component",
				SourceFile: "Writer.vb", Language: "vbnet"},
			{ID: "fedcba9876543210", Name: "gofile", Kind: "SCOPE.Component",
				SourceFile: "main.go", Language: "go"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-1", FromID: "0123456789abcdef", ToID: "System.IO.StringWriter", Kind: "EXTENDS"},
			{ID: "rel-2", FromID: "fedcba9876543210", ToID: "io", Kind: "IMPORTS"},
		},
	}
	Synthesize(doc)

	const wantVB = "ext:dotnet:System.IO.StringWriter"
	if got := doc.Relationships[0].ToID; got != wantVB {
		t.Fatalf("vbnet EXTENDS ToID = %q, want %q", got, wantVB)
	}
	if got := doc.Relationships[1].ToID; got != "ext:io" {
		t.Fatalf("go IMPORTS ToID = %q, want ext:io", got)
	}
	if doc.Relationships[0].ToID == doc.Relationships[1].ToID {
		t.Fatal("the .NET IO namespace and the Go io package collapsed to one node")
	}
}
