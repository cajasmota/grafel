package external

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
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
		{
			// #6337 round 2 — GROUPING. A generic instantiation must land on
			// the open type's node. Returning the raw spelling gave
			// `ext:dotnet:List(Of Machine)` its own node, one per
			// instantiation, which is the opposite of what the arm is for.
			name:    "generic instantiation folds to the open type",
			relKind: "EXTENDS",
			toID:    "List(Of Machine)",
			want:    "ext:dotnet:List",
			subtype: "type",
		},
		{
			name:    "a second instantiation shares that node",
			relKind: "IMPLEMENTS",
			toID:    "IComparable(Of Profile)",
			want:    "ext:dotnet:IComparable",
			subtype: "type",
		},
		{
			name:    "generic member-level clause folds to the open type",
			relKind: "IMPLEMENTS",
			toID:    "IComparable(Of Profile).CompareTo",
			want:    "ext:dotnet:IComparable:CompareTo",
			subtype: "member",
		},
		{
			name:    "dotted generic instantiation strips too",
			relKind: "EXTENDS",
			toID:    "System.Collections.Generic.List(Of Machine)",
			want:    "ext:dotnet:System.Collections.Generic.List",
			subtype: "type",
		},
		{
			// #6337 round 2 — MALFORMED SPELLINGS. The dotted half is a
			// three-entry ROOT set, not a curated type list, so before the
			// well-formedness check any `System.`-prefixed string synthesised.
			// A clause the parser truncated must stay a bug edge: scoring a
			// parse failure as a resolution is exactly the masking this arm
			// claims to avoid.
			name:    "truncated dotted clause stays a bug edge",
			relKind: "EXTENDS",
			toID:    "System.",
			want:    "System.",
		},
		{
			name:    "empty interior segment stays a bug edge",
			relKind: "EXTENDS",
			toID:    "System..Forms.Form",
			want:    "System..Forms.Form",
		},
		{
			name:    "non-identifier segment stays a bug edge",
			relKind: "EXTENDS",
			toID:    "System.Forms.Form>",
			want:    "System.Forms.Form>",
		},
		{
			// The other direction of the same pin: tightening must not have
			// cost a well-formed deep dotted name.
			name:    "well-formed deep dotted name still synthesises",
			relKind: "EXTENDS",
			toID:    "Microsoft.Win32.SafeHandles.SafeHandleZeroOrMinusOneIsInvalid",
			want:    "ext:dotnet:Microsoft.Win32.SafeHandles.SafeHandleZeroOrMinusOneIsInvalid",
			subtype: "type",
		},
		{
			// #6337 round 2 — the mask guard must BLOCK, not merely decline.
			// Declining hands the target to the generic dotted-root fallback,
			// which returns ext:System — a resolved node, so the ambiguity
			// leaves the bug-edge count and the guard achieves nothing.
			//
			// This case is also what kills review mutant W6 ("skip gate 3
			// whenever typePart is dotted"), and its scope is worth stating
			// exactly, because the mutant survived until round 2 added it.
			// WHAT IT PINS is gate 3's contract: the lookup is on the whole
			// typePart, whatever produced the name. WHAT IT DOES NOT CLAIM is
			// that a VB.NET source file can reach it — it cannot. The vbnet
			// extractor stamps the enclosing namespace as a PROPERTY
			// (`vbnet_namespace`, extractors/vbnet/extractor.go:151,201), never
			// as part of Entity.Name, so an in-tree `Namespace Windows` /
			// `Class Foo` is indexed as `Foo` and inTreeNames never holds
			// `Windows.Foo`. The dotted arm of gate 3 is therefore a no-op for
			// anything the vbnet extractor emits, and the residual it does not
			// cover is documented as UNGUARDED at vbFrameworkRootNamespaces in
			// resolve/refs.go rather than claimed as covered.
			//
			// It is kept, not deleted, because the set is language-AGNOSTIC by
			// design (see buildInTreeNameSet) and dropping dotted names from it
			// would mean ADDING a special case that makes the guard weaker on a
			// polyglot document — the mutation itself.
			name:    "masked dotted target does not fall through to ext:System",
			relKind: "EXTENDS",
			toID:    "System.Windows.Forms.Form",
			extra: []graph.Entity{{
				ID: "fedcba9876543210", Name: "System.Windows.Forms.Form",
				Kind: "SCOPE.Component", SourceFile: "Form.vb", Language: "vbnet",
			}},
			want: "System.Windows.Forms.Form",
		},
		{
			// #6337 round 2 — `Exception`. See the position note in
			// synth_vbnet_hierarchy_6337.go. Below the stdlib stop-list this
			// produced an untagged `ext:Exception` typed "function" and shared
			// with Python / Java / JS.
			name:    "Exception gets the dotnet canon, not the shared stdlib node",
			relKind: "EXTENDS",
			toID:    "Exception",
			want:    "ext:dotnet:Exception",
			subtype: "type",
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

// TestVBNetHierarchyExceptionDoesNotStealStdlibBareName_6337 is the other
// direction of the `Exception` fix, and the reason the arm was moved above the
// stdlib stop-list rather than the stop-list being language-gated.
//
// Moving an arm earlier can steal edges from the arm it jumped over. This pins
// that it did not: `Exception` reaching classifyExternal from any language but
// vbnet, or from vbnet on a non-hierarchy edge, must still fold to the shared
// `ext:Exception` function node it always did. Only vbnet EXTENDS / IMPLEMENTS
// changed.
func TestVBNetHierarchyExceptionDoesNotStealStdlibBareName_6337(t *testing.T) {
	// python is not "ext:Exception": `Exception` is a Python BUILTIN, and a
	// separate pre-pass in Synthesize drops the edge entirely rather than
	// synthesising a placeholder for it. That is pre-existing behaviour and is
	// pinned here as-is — the point of this test is that NOTHING outside vbnet
	// hierarchy changed when the arm moved.
	for lang, want := range map[string]string{
		"python":     "",
		"java":       "ext:Exception",
		"javascript": "ext:Exception",
		"go":         "ext:Exception",
		"":           "ext:Exception",
	} {
		doc := vbDoc("EXTENDS", "Exception")
		doc.Entities[0].Language = lang
		doc.Entities[0].SourceFile = "a.py"
		Synthesize(doc)
		if got := doc.Relationships[0].ToID; got != want {
			t.Errorf("lang=%q: ToID = %q, want %q (the vbnet arm must not claim it)", lang, got, want)
		}
	}
	// vbnet, but a CALLS edge — still the stdlib arm's.
	doc := vbDoc("CALLS", "Exception")
	Synthesize(doc)
	if got := doc.Relationships[0].ToID; got != "ext:Exception" {
		t.Errorf("vbnet CALLS: ToID = %q, want ext:Exception", got)
	}
}

// TestVBNetHierarchyArmStealsNothingElseFromStdlib_6337 generalises the case
// above across both tables. The arm now runs before stdlibFunction, so every
// name claimed by BOTH the language-agnostic stdlibBareNames stop-list and the
// vbnet bare-leaf table changes hands: for vbnet EXTENDS / IMPLEMENTS it stops
// producing a shared, untagged `ext:<name>` node typed "function" and produces
// an `ext:dotnet:<name>` type node instead.
//
// The measured answer today is that `Exception` is the only such name. This
// test fails if a future entry on either table silently adds another, which may
// well be the right call — Exception was — but is a decision that needs its own
// pinned case, not a side effect of the arm's position.
func TestVBNetHierarchyArmStealsNothingElseFromStdlib_6337(t *testing.T) {
	var both []string
	for name := range stdlibBareNames {
		if _, member, ok := resolve.VBNetExternalHierarchyTarget(name); ok && member == "" {
			both = append(both, name)
		}
	}
	sort.Strings(both)
	want := []string{"Exception"}
	if !reflect.DeepEqual(both, want) {
		t.Fatalf("names claimed by BOTH stdlibBareNames and the vbnet base-type table = %v, want %v.\n"+
			"Each one is a name the #6337 arm takes off the shared ext:<name> stdlib node\n"+
			"for vbnet hierarchy edges. Add a case pinning its canon and subtype in\n"+
			"TestVBNetHierarchyExternalSynthesis_6337, and a both-directions case like\n"+
			"TestVBNetHierarchyExceptionDoesNotStealStdlibBareName_6337, then update this list.", both, want)
	}
}

// TestVBNetHierarchyRelKindGateIsHierarchyOnly_6337 pins the relKind gate in the
// WIDENING direction (#6337 round 3, review mutant W3).
//
// The existing "CALLS is not a hierarchy edge" case pins one kind on one name,
// and a mutant that widened the gate to `REFERENCES` / `USES` survived the whole
// suite. That is the expensive direction to get wrong: EXTENDS + IMPLEMENTS are
// 498 edges on the 302-file corpus, while REFERENCES / USES are the bulk of the
// graph and are ORDINARY USE SITES. An `Inherits Form` says the type is a base
// class; a `USES Form` says only that the name was mentioned, and half the
// reasons a mention stays unresolved are in-tree resolver misses. Synthesising
// an `ext:dotnet:` node for those would move a large population out of the
// bug-edge count without resolving any of it — the arm's whole justification is
// that a hierarchy target has nowhere else to go, and a use site does.
//
// The gate is asserted on the `dotnet:` PREFIX, not on IsResolvedToID: a
// non-hierarchy edge may legitimately be claimed by some other arm (a dotted
// name still reaches the generic dotted-root fallback and comes back
// `ext:System`), and pinning "unresolved" would be pinning the wrong property
// and would break the moment an unrelated arm changed.
func TestVBNetHierarchyRelKindGateIsHierarchyOnly_6337(t *testing.T) {
	for _, relKind := range []string{"REFERENCES", "USES", "CALLS", "IMPORTS", "CONTAINS", "DEPENDS_ON", ""} {
		for _, toID := range []string{"Form", "IDisposable", "System.Windows.Forms.Form", "IDisposable.Dispose"} {
			doc := vbDoc(relKind, toID)
			Synthesize(doc)
			if got := doc.Relationships[0].ToID; strings.HasPrefix(got, "ext:"+vbnetExtNamespace) {
				t.Errorf("relKind=%q toID=%q: ToID = %q — the #6337 arm is for EXTENDS / "+
					"IMPLEMENTS only. A use site is not a hierarchy target: it has other "+
					"arms available and its unresolved half is dominated by in-tree "+
					"resolver misses, which this arm would hide", relKind, toID, got)
			}
		}
	}
	// The other direction, so an always-decline mutant cannot satisfy the loop
	// above: the two kinds the arm IS for still reach it.
	for _, relKind := range []string{"EXTENDS", "IMPLEMENTS"} {
		doc := vbDoc(relKind, "Form")
		Synthesize(doc)
		if got := doc.Relationships[0].ToID; got != "ext:dotnet:Form" {
			t.Errorf("relKind=%q: ToID = %q, want ext:dotnet:Form — narrowing the gate "+
				"past EXTENDS / IMPLEMENTS removes the arm entirely", relKind, got)
		}
	}
}

// TestVBNetHierarchyMaskGuardFoldsVBCaseOnly_6337 pins gate 3's case rule in
// BOTH directions, and pins the language boundary that makes it safe
// (#6337 round 3, review mutant W5).
//
// The choice was previously unpinned in both directions at once: gate 3 was a
// plain map lookup, round 2 wrote its case-sensitivity down as a known
// limitation, and a mutant that made it case-INSENSITIVE passed the whole suite
// untouched. So the hole could have been closed for free with no test noticing,
// and reopened just as quietly.
//
// THE DECISION IS: fold case, but only for VB.NET declarations. VB.NET is
// case-insensitive — `Class panel` and `Inherits Panel` are the same type to
// the compiler — so a case-sensitive gate 3 gave a semantically wrong answer
// and shadowed a real in-tree type behind an `ext:dotnet:` placeholder. No
// other language in the set is case-insensitive, so folding the whole
// language-agnostic set would make a Go `list` or a Python `form` block a VB
// hierarchy target in languages where those spellings are genuinely distinct
// types. Measured on the 302-file corpus before choosing: 0 targets newly
// blocked by the fold, 0 in-tree names differing from an allowlisted base type
// only by case. See inTreeNameSet.
//
// Each case below kills a different mutant, which is the point of the table:
// the VB cases kill "revert to case-sensitive", and the non-VB lowercase case
// kills "fold the whole set with strings.EqualFold" — the mutant as written.
func TestVBNetHierarchyMaskGuardFoldsVBCaseOnly_6337(t *testing.T) {
	const goFile, vbFile = "list.go", "Panel.vb"
	cases := []struct {
		name    string
		toID    string
		relKind string
		inTree  graph.Entity
		want    string
		why     string
	}{
		{
			name:    "VB in-tree lowercase blocks the allowlisted spelling",
			toID:    "Panel",
			relKind: "EXTENDS",
			inTree: graph.Entity{ID: "fedcba9876543210", Name: "panel", Kind: "SCOPE.Component",
				SourceFile: vbFile, Language: "vbnet"},
			want: "Panel",
			why: "VB.NET is case-insensitive: `Class panel` IS the type `Inherits Panel` " +
				"names, so this is the partial-class ambiguity the guard exists to keep visible",
		},
		{
			name:    "VB in-tree uppercase blocks it too",
			toID:    "Panel",
			relKind: "EXTENDS",
			inTree: graph.Entity{ID: "fedcba9876543210", Name: "PANEL", Kind: "SCOPE.Component",
				SourceFile: vbFile, Language: "vbnet"},
			want: "Panel",
			why:  "the fold must be symmetric, not a one-way ToLower on the target only",
		},
		{
			name:    "VB fold reaches the TYPE part of a member clause",
			toID:    "IDisposable.Dispose",
			relKind: "IMPLEMENTS",
			inTree: graph.Entity{ID: "fedcba9876543210", Name: "idisposable", Kind: "SCOPE.Component",
				SourceFile: vbFile, Language: "vbnet"},
			want: "IDisposable.Dispose",
			why:  "gate 3 is keyed on typePart, so the fold must apply there as well",
		},
		{
			// THE MUTANT'S CASE. A case-insensitive scan over the whole
			// language-agnostic set blocks this; the VB-only fold does not.
			name:    "a non-VB lowercase name does NOT block",
			toID:    "Form",
			relKind: "EXTENDS",
			inTree: graph.Entity{ID: "fedcba9876543210", Name: "form", Kind: "SCOPE.Component",
				SourceFile: goFile, Language: "go"},
			want: "ext:dotnet:Form",
			why: "Go is case-sensitive: `form` and `Form` are different types, and " +
				"suppressing a real external base on that collision is a recall loss " +
				"with no VB.NET semantics behind it",
		},
		{
			name:    "a non-VB EXACT name still blocks",
			toID:    "Form",
			relKind: "EXTENDS",
			inTree: graph.Entity{ID: "fedcba9876543210", Name: "Form", Kind: "SCOPE.Component",
				SourceFile: goFile, Language: "go"},
			want: "Form",
			why: "the exact half of the set stays language-AGNOSTIC, mirroring " +
				"resolve.Index.nameExists — the fold narrows nothing that already matched",
		},
		{
			name:    "an EXTERNAL vbnet-tagged node does not block by fold either",
			toID:    "Form",
			relKind: "EXTENDS",
			inTree: graph.Entity{ID: "ext:somepkg:form", Name: "form", Kind: KindExternal,
				Subtype: "symbol", Language: "vbnet"},
			want: "ext:dotnet:Form",
			why: "the Kind exclusion must be applied BEFORE the fold index is built, or " +
				"a re-run of Synthesize would reject what the first run accepted",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := vbDoc(tc.relKind, tc.toID, tc.inTree)
			Synthesize(doc)
			if got := doc.Relationships[0].ToID; got != tc.want {
				t.Errorf("in-tree %q (lang=%q) + %s %q: ToID = %q, want %q\n  %s",
					tc.inTree.Name, tc.inTree.Language, tc.relKind, tc.toID, got, tc.want, tc.why)
			}
		})
	}
}

// TestVBNetHierarchyFoldIndexIsVBOnly_6337 pins the same rule one level down,
// on the set builder, so the table above cannot be satisfied by a Synthesize
// that reaches the right answers through some other arm.
func TestVBNetHierarchyFoldIndexIsVBOnly_6337(t *testing.T) {
	set := buildInTreeNameSet(&graph.Document{Entities: []graph.Entity{
		{ID: "1", Name: "panel", Kind: "SCOPE.Component", SourceFile: "a.vb", Language: "vbnet"},
		{ID: "2", Name: "form", Kind: "SCOPE.Component", SourceFile: "b.go", Language: "go"},
		{ID: "3", Name: "Label", Kind: "SCOPE.Component", SourceFile: "c.py", Language: "python"},
		{ID: "ext:x:list", Name: "list", Kind: KindExternal, Language: "vbnet"},
	}})

	// The exact half is language-agnostic and keeps every real name verbatim.
	for _, n := range []string{"panel", "form", "Label"} {
		if !set.exact[n] {
			t.Errorf("exact[%q] = false; the exact half must stay language-agnostic", n)
		}
	}
	if set.exact["list"] {
		t.Error(`exact["list"] = true for an ext: node; external placeholders are not in-tree declarations`)
	}

	// The fold half is VB-only, and excludes external nodes before folding.
	if !set.blocks("Panel") {
		t.Error(`blocks("Panel") = false; a VB in-tree "panel" must block it`)
	}
	if set.blocks("Form") {
		t.Error(`blocks("Form") = true; only a Go "form" declares that spelling, and Go is case-sensitive`)
	}
	if set.blocks("label") {
		t.Error(`blocks("label") = true; only a Python "Label" declares that spelling, and Python is case-sensitive`)
	}
	if set.blocks("List") || set.blocks("list") {
		t.Error(`blocks("List"/"list") = true; the only "list" is an ext: node`)
	}
	if len(set.vbFold) != 1 {
		t.Errorf("vbFold = %v, want exactly the one VB.NET entity's lowercased name", set.vbFold)
	}
}
