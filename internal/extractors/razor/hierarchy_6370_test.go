package razor_test

import (
	"context"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/razor" // trigger init()
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6370 (final arm) — razor emitted NO EXTENDS/IMPLEMENTS edge at all: it is
// absent from cross/hierarchy's supportedLanguages AND this extractor never
// looked at @inherits/@implements (zero occurrences of either string anywhere
// in the package, verified in #6343 Arm 0). A user asking "what extends this
// Blazor component" got an empty answer indistinguishable from "nothing does".
//
// The edges are emitted from THIS extractor rather than by registering razor in
// cross/hierarchy, because that pass mints its own SCOPE.Component per type AND
// per parent, and this package already emits exactly one component per file —
// registering there would duplicate every component and anchor the edges on the
// wrong node. TestRazorHierarchy_NoDuplicateComponents is the standing guard.

// hierTargets returns the sorted ToIDs of the `edgeKind` edges embedded on the
// component entity named `owner`, and fails if any carries a non-empty FromID
// (a file-anchored relationship — see file_anchored_rels_guard_test.go and
// #6295/#6298/#6365/#6367).
func hierTargets(t *testing.T, ents []types.EntityRecord, owner, edgeKind string) []string {
	t.Helper()
	e := componentEntity(ents, owner)
	if e == nil {
		t.Fatalf("no SCOPE.UIComponent component named %q; got %v", owner, componentNames(ents))
	}
	var out []string
	for _, r := range e.Relationships {
		if r.Kind != edgeKind {
			continue
		}
		if r.FromID != "" {
			t.Errorf("%s %s -> %s has FromID=%q, want empty so assembly anchors it on the COMPONENT",
				owner, edgeKind, r.ToID, r.FromID)
		}
		out = append(out, r.ToID)
	}
	sort.Strings(out)
	return out
}

func componentEntity(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.UIComponent" && ents[i].Subtype == "component" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func componentNames(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		if e.Kind == "SCOPE.UIComponent" && e.Subtype == "component" {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out
}

// allHierTargets returns every EXTENDS/IMPLEMENTS ToID across ALL entities, so
// an edge that escaped onto some other record is still seen.
func allHierTargets(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "EXTENDS" || r.Kind == "IMPLEMENTS" {
				out = append(out, r.Kind+":"+r.ToID)
			}
		}
	}
	sort.Strings(out)
	return out
}

func eqStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRazorHierarchy_Inherits(t *testing.T) {
	ents := extract(t, "Pages/MainLayout.razor", "@inherits LayoutComponentBase\n\n<div>@Body</div>\n")
	if got := hierTargets(t, ents, "MainLayout", "EXTENDS"); !eqStrs(got, []string{"LayoutComponentBase"}) {
		t.Fatalf("MainLayout EXTENDS = %v, want [LayoutComponentBase]", got)
	}
	if got := hierTargets(t, ents, "MainLayout", "IMPLEMENTS"); len(got) != 0 {
		t.Errorf("MainLayout IMPLEMENTS = %v, want none", got)
	}
}

// One component carries several @implements lines — each names ONE interface,
// so multiplicity is per-directive, not comma-separated as in Java-family
// syntax. A scan that read only the first match would emit one edge here.
func TestRazorHierarchy_ImplementsMultipleDirectives(t *testing.T) {
	ents := extract(t, "Shared/NavMenu.razor", "@implements IDisposable\n@implements IAsyncDisposable\n\n<nav></nav>\n")
	if got := hierTargets(t, ents, "NavMenu", "IMPLEMENTS"); !eqStrs(got, []string{"IAsyncDisposable", "IDisposable"}) {
		t.Fatalf("NavMenu IMPLEMENTS = %v, want [IAsyncDisposable IDisposable]", got)
	}
	if got := hierTargets(t, ents, "NavMenu", "EXTENDS"); len(got) != 0 {
		t.Errorf("NavMenu EXTENDS = %v, want none (an @implements directive is not inheritance)", got)
	}
}

// Both directives on one component, and the kinds must not be swapped: an
// @inherits target is never an IMPLEMENTS edge and vice versa.
func TestRazorHierarchy_InheritsAndImplements(t *testing.T) {
	ents := extract(t, "Pages/Index.razor", "@page \"/\"\n@inherits BasePage\n@implements IDisposable\n\n<h1>x</h1>\n")
	if got := hierTargets(t, ents, "Index", "EXTENDS"); !eqStrs(got, []string{"BasePage"}) {
		t.Errorf("Index EXTENDS = %v, want [BasePage]", got)
	}
	if got := hierTargets(t, ents, "Index", "IMPLEMENTS"); !eqStrs(got, []string{"IDisposable"}) {
		t.Errorf("Index IMPLEMENTS = %v, want [IDisposable]", got)
	}
}

// Generic arguments are erased from the target: `ComponentBase<TItem>` binds to
// the declared type `ComponentBase`, never to a per-instantiation name. The
// two-argument form has a SPACE inside the angle brackets, which a
// first-token-wins scan without generic erasure would truncate to `Grid<string,`.
func TestRazorHierarchy_GenericsErased(t *testing.T) {
	ents := extract(t, "Grid.razor", "@inherits GridBase<string, int>\n@implements IEnumerable<string>\n")
	if got := hierTargets(t, ents, "Grid", "EXTENDS"); !eqStrs(got, []string{"GridBase"}) {
		t.Errorf("EXTENDS = %v, want [GridBase]", got)
	}
	if got := hierTargets(t, ents, "Grid", "IMPLEMENTS"); !eqStrs(got, []string{"IEnumerable"}) {
		t.Errorf("IMPLEMENTS = %v, want [IEnumerable]", got)
	}
}

// A namespace-qualified target is emitted AS WRITTEN, and this is a recorded
// decision made against a measurement rather than an accident. See
// TestRazorHierarchy_QualifiedTargetDoesNotBind, which measures BOTH forms
// through the production resolver: as-written binds 0 of 1, the bare last
// segment binds 1 of 1. The ocaml arm's lesson (#6948) is that "no mis-binding"
// is not "sufficient" — so this is the losing form on binding, and it is chosen
// anyway. The reason, stated at the precision it was measured at:
//
//	one same-named candidate, not the intended one → the bare form BINDS, to
//	  the wrong node. This is #6369's hazard and it is the shape that justifies
//	  the decision.
//	two or more same-named candidates → the bare form DANGLES. Ambiguity drops
//	  the edge; it does not mis-point it.
//
// "A leaf name binds to any same-named type in any namespace" would therefore
// be an over-claim: only the single-candidate shape mis-binds, and razor
// components carry no namespace at all to tell the two apart. The distinction
// matters because the ambiguous shape is the one a reader assumes, and getting
// it backwards is the correction #6948 had to make publicly.
//
// All 7 corpus occurrences are unqualified, so the qualified form is widened on
// no evidence in either direction; the dangling edge is the recorded gap, of the
// same class as ocaml's deliberately-unemitted module-qualified parents (#6947).
func TestRazorHierarchy_QualifiedTargetKeptAsWritten(t *testing.T) {
	ents := extract(t, "Foo.razor", "@inherits MyApp.Components.BaseComponent\n")
	if got := hierTargets(t, ents, "Foo", "EXTENDS"); !eqStrs(got, []string{"MyApp.Components.BaseComponent"}) {
		t.Errorf("EXTENDS = %v, want [MyApp.Components.BaseComponent]", got)
	}
}

// Only the FIRST token of a directive's argument is the type name. Razor
// rejects `@inherits Foo Bar` at compile time, so this input is malformed —
// which is precisely why it needs a test: the whole-line reading and the
// first-token reading agree on EVERY well-formed input (generic arguments are
// erased before the split, and a trailing `@* … *@` is blanked to spaces that
// TrimSpace removes), so a mutant replacing directiveTarget's split with
// `strings.TrimSpace(stripGenerics(arg))` survived the entire suite and the
// golden fixture. It is graded rather than waved through as equivalent: the two
// readings differ in what they emit for a malformed line — `Foo`, which binds,
// versus `Foo Bar`, which can never bind to anything — and this pins the
// binding one.
func TestRazorHierarchy_FirstTokenOfArgumentWins(t *testing.T) {
	ents := extract(t, "Malformed.razor", "@inherits Base1 Base2\n@implements IOne ITwo\n")
	if got := hierTargets(t, ents, "Malformed", "EXTENDS"); !eqStrs(got, []string{"Base1"}) {
		t.Errorf("EXTENDS = %v, want [Base1]", got)
	}
	if got := hierTargets(t, ents, "Malformed", "IMPLEMENTS"); !eqStrs(got, []string{"IOne"}) {
		t.Errorf("IMPLEMENTS = %v, want [IOne]", got)
	}
}

// A component with no hierarchy directive must emit no hierarchy edge — a scan
// that fires on every component is as wrong as one that never fires.
func TestRazorHierarchy_PlainComponentHasNoEdges(t *testing.T) {
	ents := extract(t, "Counter.razor", "@page \"/counter\"\n@using System.Text\n@inject IJSRuntime JS\n\n<button @onclick=\"Inc\">+</button>\n\n@code {\n    private int count;\n    private void Inc() { count++; }\n}\n")
	if got := allHierTargets(ents); len(got) != 0 {
		t.Errorf("Counter emitted %v, want no hierarchy edges", got)
	}
}

// THE anchor test. `@inherits` is a DIRECTIVE and directives are line-leading;
// the same text inside markup is not one. Dropping the `^` anchor from either
// pattern makes this fail.
//
// The INDENTED case pins the second, narrower half of the anchor. Widening the
// patterns to `^[^\S\n]*@inherits…` passes the whole razor suite, the whole
// internal/quality package and the golden fixture at 6/6, 5/5, 0 forbidden, so
// the direction was distinguishable, production-reachable and observed by
// NOTHING. It is decided here rather than left to the corpus: an indented
// directive is REJECTED, matching this package's two existing anchors
// (`reUsing`, `reInject`), which are equally strict.
//
// "Zero of the 7 corpus occurrences are indented" is what motivated the choice
// and is NOT what defends it — a zero over 105 files is corpus-relative and
// says nothing about the 106th. Accepting indented directives may well be
// right; it is a widening that needs its own evidence AND a coordinated change
// to `reUsing`/`reInject`, and it must move this assertion to happen, which is
// the entire point of the assertion.
func TestRazorHierarchy_DirectiveMustBeLineLeading(t *testing.T) {
	ents := extract(t, "Markup.razor", "<p>@inherits Ghost</p>\n<span>text @implements IPhantom</span>\n")
	if got := allHierTargets(ents); len(got) != 0 {
		t.Errorf("mid-line text produced %v, want no hierarchy edges", got)
	}

	indented := extract(t, "Indented.razor", "    @inherits IndentedBase\n\t@implements IIndented\n")
	if got := allHierTargets(indented); len(got) != 0 {
		t.Errorf("indented directives produced %v, want none — an indented directive is "+
			"deliberately not recognised, as in reUsing/reInject", got)
	}
}

// The directive name must match exactly: `@inherit` (no `s`) is not a Razor
// directive, and `@inheritsFoo` is an expression, not a directive with an
// argument. Relaxing the pattern to `@inherits?` or dropping the mandatory
// separator makes this fail.
func TestRazorHierarchy_NearMissDirectiveNamesRejected(t *testing.T) {
	ents := extract(t, "Near.razor", "@inherit Base1\n@inheritsBase2\n@implement IFoo\n@implementsIBar\n")
	if got := allHierTargets(ents); len(got) != 0 {
		t.Errorf("near-miss directive names produced %v, want no hierarchy edges", got)
	}
}

// A directive inside a Razor comment is not a clause — and the real directive
// beside it still is, so this grades the scrub in both directions rather than
// only asserting an absence.
func TestRazorHierarchy_RazorCommentsIgnored(t *testing.T) {
	src := "@* @inherits Ghost *@\n" +
		"@*\n@implements IPhantom\n*@\n" +
		"@inherits RealBase\n"
	ents := extract(t, "Commented.razor", src)
	if got := hierTargets(t, ents, "Commented", "EXTENDS"); !eqStrs(got, []string{"RealBase"}) {
		t.Errorf("EXTENDS = %v, want [RealBase] (commented-out directives are not clauses)", got)
	}
	for _, tgt := range allHierTargets(ents) {
		switch tgt {
		case "EXTENDS:Ghost", "IMPLEMENTS:IPhantom":
			t.Errorf("%s came from inside a Razor comment", tgt)
		}
	}
}

// #6962: a UTF-8 BOM sits between the start of the string and the `@`, so a
// bare `(?m)^@using` misses a directive on line 1 — a live defect in this
// package's OTHER anchors, which this arm deliberately does not fix. What is
// pinned here is only that the hierarchy anchors do not inherit the hole.
//
// A fixture whose directive sits on line 2 would prove nothing about this: line
// 2 is preceded by a `\n`, which `^` matches with or without the BOM handling.
// The directive is therefore on LINE 1, immediately after the BOM.
func TestRazorHierarchy_BOMAtLineOne(t *testing.T) {
	const bom = "\uFEFF" // written as an escape: a literal BOM in a .go source file is a compile error
	ents := extract(t, "Bom.razor", bom+"@inherits LayoutComponentBase\n@implements IDisposable\n")
	if got := hierTargets(t, ents, "Bom", "EXTENDS"); !eqStrs(got, []string{"LayoutComponentBase"}) {
		t.Errorf("EXTENDS = %v, want [LayoutComponentBase] behind a BOM (#6962)", got)
	}
	if got := hierTargets(t, ents, "Bom", "IMPLEMENTS"); !eqStrs(got, []string{"IDisposable"}) {
		t.Errorf("IMPLEMENTS = %v, want [IDisposable]", got)
	}
}

// A self-edge is never information — it is what a mis-attributed owner looks
// like (#6369), and a hierarchy walk that follows one loops. The component name
// comes from the FILE PATH, so `Counter.razor` carrying `@inherits Counter` is
// exactly the shape that produces one.
func TestRazorHierarchy_SelfReferenceDropped(t *testing.T) {
	ents := extract(t, "Pages/Counter.razor", "@inherits Counter\n@implements Counter\n")
	if got := allHierTargets(ents); len(got) != 0 {
		t.Errorf("self-referential directives produced %v, want none", got)
	}
}

// Duplicate directives naming the same target emit one edge, not two.
func TestRazorHierarchy_DuplicateTargetDeduped(t *testing.T) {
	ents := extract(t, "Dup.razor", "@implements IDisposable\n@implements IDisposable\n")
	if got := hierTargets(t, ents, "Dup", "IMPLEMENTS"); !eqStrs(got, []string{"IDisposable"}) {
		t.Errorf("IMPLEMENTS = %v, want a single [IDisposable]", got)
	}
}

// The SYMPTOM of the rejected route, and only the symptom — the name says so
// now because the previous one did not. Registering razor in cross/hierarchy
// would mint a SCOPE.Component for the component AND one for each parent, on
// top of the single component this extractor already emits.
//
// This test CANNOT observe that registration: extract() calls
// extractor.Get("razor") and nothing else, so adding "razor" to
// supportedLanguages would not move it by a byte. What it observes is the shape
// that route produces — an entity minted from a hierarchy TARGET rather than
// from a file path — and it does observe it: a mutant minting a SCOPE.Component
// per target fails here, and trips the golden fixture's forbidden_entities row
// as well. An assertion whose name claims a wider population than it covers is
// the defect this board keeps re-finding, so the claim is narrowed to what runs.
//
// Exactly one component entity per file, and never one named after a base type.
func TestRazorHierarchy_NoComponentMintedForHierarchyTarget(t *testing.T) {
	ents := extract(t, "Shared/MainLayout.razor", "@inherits LayoutComponentBase\n@implements IDisposable\n\n<div>@Body</div>\n")
	if got := componentNames(ents); !eqStrs(got, []string{"MainLayout"}) {
		t.Fatalf("component entities = %v, want [MainLayout] (a base type must never become a component here)", got)
	}
	for _, e := range ents {
		if e.Name == "LayoutComponentBase" || e.Name == "IDisposable" {
			t.Errorf("base type %q was minted as a %s entity", e.Name, e.Kind)
		}
	}
}

// A file with no @code block still declares a base component. The @code scanner
// returns early (extractor.go step 4), so an implementation that collected the
// directives after that point would emit nothing for the 4 corpus files that
// carry @inherits — every one of which is a layout with no @code block.
func TestRazorHierarchy_EmittedWithoutCodeBlock(t *testing.T) {
	ents := extract(t, "Layout.razor", "@inherits LayoutComponentBase\n\n<div class=\"page\">@Body</div>\n")
	if got := hierTargets(t, ents, "Layout", "EXTENDS"); !eqStrs(got, []string{"LayoutComponentBase"}) {
		t.Fatalf("EXTENDS = %v, want [LayoutComponentBase] with no @code block present", got)
	}
}

// The directive's line is stamped, so the edge points at a place in the file.
func TestRazorHierarchy_LineStamped(t *testing.T) {
	ents := extract(t, "Lines.razor", "@page \"/x\"\n@using System\n@inherits BasePage\n")
	e := componentEntity(ents, "Lines")
	if e == nil {
		t.Fatal("no component entity")
	}
	for _, r := range e.Relationships {
		if r.Kind != "EXTENDS" {
			continue
		}
		var line string
		for _, p := range r.Properties {
			if p.K == "line" {
				line = p.V
			}
		}
		if line != "3" {
			t.Errorf("EXTENDS -> %s line = %q, want \"3\"", r.ToID, line)
		}
	}
}

// AGENTS.md ## Evidence — endpoints, not counts (#6369 is the standing example
// of edges that exist, resolve, and point at the wrong node). Three files
// pushed through the production resolver pipeline (ResolveImports →
// ReferencesEmbedded) exactly as graph assembly does.
//
// BEFORE this arm the test could not be written at all: MainLayout carried zero
// EXTENDS/IMPLEMENTS relationships, so there was no endpoint to measure.
//
// The unresolved case is asserted too, and deliberately: `IDisposable` is a BCL
// interface that is in no indexed tree, so its edge DANGLES. Per #6947 a
// dangling edge still de-orphans its source in analytics/scan.go
// (`touched[from]` is keyed unconditionally), so no orphan figure is quoted
// anywhere in this arm as evidence that an edge is real — the FROM/TO endpoints
// below are.
func TestRazorHierarchy_ResolvedEndpoints(t *testing.T) {
	const ownerPath = "Components/Layout/MainLayout.razor"
	files := map[string]string{
		ownerPath: "@inherits LayoutComponentBase\n@implements IDisposable\n\n<div>@Body</div>\n",
		"Components/Layout/LayoutComponentBase.razor": "<div>base</div>\n",
	}

	ext, ok := extractor.Get("razor")
	if !ok {
		t.Fatal("razor extractor not registered")
	}
	var recs []types.EntityRecord
	for _, p := range []string{"Components/Layout/LayoutComponentBase.razor", ownerPath} {
		ents, err := ext.Extract(context.Background(), extractor.FileInput{
			Path: p, Content: []byte(files[p]), Language: "razor",
		})
		if err != nil {
			t.Fatalf("Extract %s: %v", p, err)
		}
		recs = append(recs, ents...)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6370", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}

	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))

	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	endpoint := func(id string) string {
		if e := byID[id]; e != nil {
			return e.Name + "@" + e.SourceFile
		}
		return "<UNRESOLVED:" + id + ">"
	}

	var owner *types.EntityRecord
	for i := range recs {
		if recs[i].Name == "MainLayout" && recs[i].SourceFile == ownerPath {
			owner = &recs[i]
		}
	}
	if owner == nil {
		t.Fatal("no MainLayout entity")
	}

	got := map[string]string{}
	for _, r := range owner.Relationships {
		if r.Kind != "EXTENDS" && r.Kind != "IMPLEMENTS" {
			continue
		}
		// Replay graph assembly: the owning record's id is substituted only
		// when FromID is empty (cmd/grafel/index.go,
		// internal/extractors/incremental.go).
		from := r.FromID
		if from == "" {
			from = owner.ID
		}
		if f := endpoint(from); f != "MainLayout@"+ownerPath {
			t.Errorf("%s: FROM = %s, want MainLayout@%s (FromID=%q must be empty)",
				r.Kind, f, ownerPath, r.FromID)
		}
		got[r.Kind] = endpoint(r.ToID)
	}

	if want := "LayoutComponentBase@Components/Layout/LayoutComponentBase.razor"; got["EXTENDS"] != want {
		t.Errorf("EXTENDS TO = %s, want %s", got["EXTENDS"], want)
	}
	// IDisposable is a framework type declared in no indexed file. The edge is
	// emitted and dangles; that is the recorded state, not an accident.
	if got["IMPLEMENTS"] != "<UNRESOLVED:IDisposable>" {
		t.Errorf("IMPLEMENTS TO = %s, want <UNRESOLVED:IDisposable> (a BCL interface is in no indexed tree)", got["IMPLEMENTS"])
	}
}

// The measurement behind TestRazorHierarchy_QualifiedTargetKeptAsWritten's
// decision, kept as a test so the table cannot go stale: the same base
// component, referenced both ways, pushed through the production resolver.
//
//	toID form                          | binds to Components/BaseComponent.razor
//	-----------------------------------+----------------------------------------
//	MyApp.Components.BaseComponent     | 0 of 1  (no dotted index for razor)
//	BaseComponent                      | 1 of 1
func TestRazorHierarchy_QualifiedTargetDoesNotBind(t *testing.T) {
	for _, tc := range []struct {
		name      string
		directive string
		wantBound bool
	}{
		{"qualified", "@inherits MyApp.Components.BaseComponent\n", false},
		{"bare", "@inherits BaseComponent\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{
				"Pages/Owner.razor":              tc.directive,
				"Components/BaseComponent.razor": "<div>base</div>\n",
			}
			ext, ok := extractor.Get("razor")
			if !ok {
				t.Fatal("razor extractor not registered")
			}
			var recs []types.EntityRecord
			for _, p := range []string{"Components/BaseComponent.razor", "Pages/Owner.razor"} {
				ents, err := ext.Extract(context.Background(), extractor.FileInput{
					Path: p, Content: []byte(files[p]), Language: "razor",
				})
				if err != nil {
					t.Fatalf("Extract %s: %v", p, err)
				}
				recs = append(recs, ents...)
			}
			for i := range recs {
				recs[i].ID = graph.EntityID("issue6370", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
			}
			resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
			resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))

			byID := make(map[string]*types.EntityRecord, len(recs))
			for i := range recs {
				byID[recs[i].ID] = &recs[i]
			}
			var seen int
			for _, r := range recs {
				for _, rel := range r.Relationships {
					if rel.Kind != "EXTENDS" {
						continue
					}
					seen++
					target := byID[rel.ToID]
					if got := target != nil; got != tc.wantBound {
						t.Errorf("EXTENDS ToID %q bound=%v, want %v", rel.ToID, got, tc.wantBound)
					}
					if target != nil && target.SourceFile != "Components/BaseComponent.razor" {
						t.Errorf("EXTENDS bound to %s@%s, want Components/BaseComponent.razor",
							target.Name, target.SourceFile)
					}
				}
			}
			if seen != 1 {
				t.Fatalf("saw %d EXTENDS edges, want exactly 1", seen)
			}
		})
	}
}
