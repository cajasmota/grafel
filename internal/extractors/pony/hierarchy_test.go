package pony_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// implementsOf returns the sorted ToIDs of every IMPLEMENTS edge embedded on
// the SCOPE.Component named `owner`, or nil when no such component exists.
// Reading through the public Extract output rather than calling provideEdges
// directly is deliberate: every assertion below is then a claim about the
// EXTRACTOR, so a mutant that fixes the helper and skips the call site is not
// hidden (the "pin behaviour, not the helper" rule).
func implementsOf(ents []types.EntityRecord, owner string) []string {
	var out []string
	for i := range ents {
		e := &ents[i]
		if e.Kind != "SCOPE.Component" || e.Name != owner {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "IMPLEMENTS" {
				out = append(out, r.ToID)
			}
		}
	}
	sort.Strings(out)
	return out
}

// allImplements returns every IMPLEMENTS edge in the file as "Owner->Target",
// sorted. Used by the over-firing tests, where the point is that NOTHING was
// emitted anywhere — an assertion scoped to one owner would pass while the edge
// landed on its neighbour.
func allImplements(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		e := &ents[i]
		for _, r := range e.Relationships {
			if r.Kind == "IMPLEMENTS" {
				out = append(out, e.Name+"->"+r.ToID)
			}
		}
	}
	sort.Strings(out)
	return out
}

func eqStrings(a, b []string) bool {
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

// TestPonyHierarchy_EveryDeclarationKeywordConforms varies ONE axis — the
// declaration keyword — and holds everything else constant: every declaration
// below names exactly one trait, with no generics, no parentheses and no
// members. It asserts only that the keyword does not decide whether the clause
// is read. It says nothing about multi-type lists or about non-conforming
// types; those are TestPonyHierarchy_IntersectionListEmitsOnePerTrait and
// TestPonyHierarchy_NoIsClauseEmitsNothing.
func TestPonyHierarchy_EveryDeclarationKeywordConforms(t *testing.T) {
	src := `trait Named
actor Worker is Named
class Circle is Named
primitive Zero is Named
struct Point is Named
interface Sized is Named
trait Loud is Named
`
	ents := runPony(t, src, "src/all.pony")
	for _, owner := range []string{"Worker", "Circle", "Zero", "Point", "Sized", "Loud"} {
		got := implementsOf(ents, owner)
		if !eqStrings(got, []string{"Named"}) {
			t.Errorf("%s: IMPLEMENTS = %v, want [Named]", owner, got)
		}
	}
	// `trait Named` itself declares no clause, and is the in-source control
	// that the six above are not simply "every component gets an edge".
	if got := implementsOf(ents, "Named"); len(got) != 0 {
		t.Errorf("Named: IMPLEMENTS = %v, want none", got)
	}
}

// TestPonyHierarchy_IntersectionListEmitsOnePerTrait varies the SHAPE of the
// provides list and holds the owner keyword (`class`) and the trait names
// constant. Each case states the exact full edge set, so an extra edge fails
// just as loudly as a missing one.
func TestPonyHierarchy_IntersectionListEmitsOnePerTrait(t *testing.T) {
	cases := []struct {
		name string
		decl string
		want []string
	}{
		{"single", "class C is A", []string{"A"}},
		{"parenthesised single", "class C is (A)", []string{"A"}},
		{"two in parens", "class C is (A & B)", []string{"A", "B"}},
		{"three in parens", "class C is (A & B & D)", []string{"A", "B", "D"}},
		{"unparenthesised pair", "class C is A & B", []string{"A", "B"}},
		{"nested parens", "class C is (A & (B & D))", []string{"A", "B", "D"}},
		{"tight spacing", "class C is (A&B)", []string{"A", "B"}},
		{"repeated name states one fact", "class C is (A & A)", []string{"A"}},
		{"trailing line comment", "class C is (A & B) // note", []string{"A", "B"}},
		{"trailing block comment", "class C is A /* note */", []string{"A"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := runPony(t, tc.decl+"\n", "src/x.pony")
			if got := implementsOf(ents, "C"); !eqStrings(got, tc.want) {
				t.Fatalf("%q: IMPLEMENTS = %v, want %v", tc.decl, got, tc.want)
			}
		})
	}
}

// TestPonyHierarchy_UnionListIsRejectedWhole varies whether a provides leaf is
// a plain named type and holds the clause position constant. A `|` union is not
// an intersection: `is (A | B)` would state "conforms to A OR B", which no
// single IMPLEMENTS edge can express, so emitting the first name would assert a
// conformance the source does not. ponyProvidedTypeRE is anchored at BOTH ends
// precisely so such a leaf is rejected whole rather than half-matched.
//
// The second case is the positive control: a rejected leaf must not poison its
// siblings, so the neighbouring `Named` still produces its edge. Without it,
// this test would pass on a producer that emitted nothing at all.
func TestPonyHierarchy_UnionListIsRejectedWhole(t *testing.T) {
	cases := []struct {
		decl string
		want []string
	}{
		{"class C is (A | B)", nil},
		{"class C is ((A | B) & Named)", []string{"Named"}},
	}
	for _, tc := range cases {
		t.Run(tc.decl, func(t *testing.T) {
			ents := runPony(t, tc.decl+"\n", "src/x.pony")
			if got := implementsOf(ents, "C"); !eqStrings(got, tc.want) {
				t.Fatalf("%q: IMPLEMENTS = %v, want %v", tc.decl, got, tc.want)
			}
		})
	}
}

// TestPonyHierarchy_GenericParamsAreCrossedNotSearched pins skipGenericParams
// through the extractor. It varies the generic parameter list — absent, simple,
// constrained, NESTED — and holds the conformance clause constant, so a
// producer that finds `]` by scanning for the first one fails only on the
// nested case, which is why the nested case is here.
func TestPonyHierarchy_GenericParamsAreCrossedNotSearched(t *testing.T) {
	cases := []struct {
		name string
		decl string
		want []string
	}{
		{"no params", "class Box is Sized", []string{"Sized"}},
		{"simple param", "class Box[A] is Sized", []string{"Sized"}},
		{"constrained param", "class Box[A: Any val] is Sized", []string{"Sized"}},
		{"nested bracket in constraint", "class Box[A: Seq[U8] val] is Sized", []string{"Sized"}},
		{"generic target", "class Box is Comparable[Box]", []string{"Comparable"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := runPony(t, tc.decl+"\n", "src/x.pony")
			if got := implementsOf(ents, "Box"); !eqStrings(got, tc.want) {
				t.Fatalf("%q: IMPLEMENTS = %v, want %v", tc.decl, got, tc.want)
			}
		})
	}
}

// TestPonyHierarchy_UnterminatedGenericListEmitsNothing pins the -1 branch of
// skipGenericParams: a `[` that never closes on its own line must not let the
// anchor drift onto a later line and adopt that line's `is`.
func TestPonyHierarchy_UnterminatedGenericListEmitsNothing(t *testing.T) {
	src := "class Box[A: Any\n  is Sized\n"
	if got := allImplements(runPony(t, src, "src/x.pony")); len(got) != 0 {
		t.Fatalf("IMPLEMENTS = %v, want none — the anchor crossed a newline", got)
	}
}

// TestPonyHierarchy_MultiLineGenericListIsInvisible_KnownDivergence pins
// today's CONSERVATIVE output on purpose. A generic parameter list wrapped
// across lines is legal Pony, but skipGenericParams stops at the newline, so
// the anchor is left sitting on the `[` and no edge is emitted. That is
// deliberate — the provides list is read to end-of-LINE everywhere else in this
// file, and a bracket scan that crosses newlines would, on an UNTERMINATED
// list, run to the next unbalanced `]` anywhere in the file and anchor there.
// It is still a miss, and this test is what tells whoever decides the trade is
// worth taking that the clause is theirs.
//
// The second declaration is the positive control: the single-line form on the
// very next line still conforms, so a producer that emitted nothing at all
// fails here rather than passing this test for the wrong reason.
//
// Verified as a real tripwire by mutating TOWARD the fix (deleting the newline
// case from skipGenericParams): this test fires and nothing else in the package
// moves.
func TestPonyHierarchy_MultiLineGenericListIsInvisible_KnownDivergence(t *testing.T) {
	src := `class Wrapped[
  A: Any val] is Sized
class Inline[A: Any val] is Sized
`
	ents := runPony(t, src, "src/x.pony")
	if got := implementsOf(ents, "Wrapped"); len(got) != 0 {
		t.Fatalf("Wrapped: IMPLEMENTS = %v, want none — skipGenericParams now crosses "+
			"newlines: delete this known-divergence test and assert Wrapped IMPLEMENTS Sized", got)
	}
	if got := implementsOf(ents, "Inline"); !eqStrings(got, []string{"Sized"}) {
		t.Fatalf("Inline: IMPLEMENTS = %v, want [Sized] — the control failed, so the "+
			"assertion above proved nothing", got)
	}
}

// TestPonyHierarchy_NoIsClauseEmitsNothing is the required negative control: a
// declaration with no clause, in each of the three positions a following `is`
// could be stolen from (end of file, before a member, before another
// declaration that HAS a clause). The last case doubles as a positive control
// that the neighbour's own edge is still emitted, so "nothing fired at all"
// cannot pass this test.
func TestPonyHierarchy_NoIsClauseEmitsNothing(t *testing.T) {
	src := `trait Named
class Plain
class WithMember
  let x: U8 = 0
class Conformer is Named
`
	ents := runPony(t, src, "src/x.pony")
	if got := allImplements(ents); !eqStrings(got, []string{"Conformer->Named"}) {
		t.Fatalf("IMPLEMENTS = %v, want exactly [Conformer->Named]", got)
	}
}

// TestPonyHierarchy_OverloadedIsDoesNotFire is the over-firing control, and the
// reason the anchor exists. It enumerates every OTHER thing Pony spells `is`,
// each placed where a forward-searching producer would attach it to the
// preceding declaration, and asserts the whole-file edge set is empty. Recall
// cannot detect over-firing, so this is the direction the golden fixture's
// forbidden rows also grade — deliberately doubled, at both levels.
func TestPonyHierarchy_OverloadedIsDoesNotFire(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"identity operator in a method body", `class Plain
  fun eq(o: Plain box): Bool =>
    this is o
`},
		{"identity operator in a behaviour body", `actor Runner
  be go(o: Runner tag) =>
    if this is o then None end
`},
		{"is inside a string literal", `class Plain
  fun doc(): String => "class Spectre is Phantom"
`},
		{"is inside a line comment in a body", `class Plain
  // class Spectre is Phantom
  fun n(): U8 => 0
`},
		{"whole declaration commented out", `// class Spectre is Phantom
`},
		{"whole declaration commented out, indented", `  // class Spectre is Phantom
`},
		{"is on the NEXT line", `class Plain
  is Named
`},
		{"CRLF declaration with no clause", "class Plain\r\nclass Other\r\n"},
		{"name is a prefix of another with a clause", `class Plainis
`},
		{"isnt operator", `class Plain
  fun ne(o: Plain box): Bool =>
    this isnt o
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := allImplements(runPony(t, tc.src, "src/x.pony")); len(got) != 0 {
				t.Fatalf("IMPLEMENTS = %v, want none", got)
			}
		})
	}
}

// TestPonyHierarchy_CRLFDeclarationStillConforms is the positive control for
// the CRLF case above: the \r must be trimmed from the provides line, not
// carried into the target name. Without it, the CRLF row in the over-firing
// table would pass for the wrong reason on a producer that emits nothing at all
// under CRLF.
func TestPonyHierarchy_CRLFDeclarationStillConforms(t *testing.T) {
	ents := runPony(t, "trait Named\r\nclass C is Named\r\n", "src/x.pony")
	if got := implementsOf(ents, "C"); !eqStrings(got, []string{"Named"}) {
		t.Fatalf("IMPLEMENTS = %v, want [Named]", got)
	}
}

// TestPonyHierarchy_TypeAliasIsProducesNoEdge pins the call-site exclusion
// described in the package header: `type Alias is X` is an alias, not
// conformance, and its right-hand side is commonly a `|` union which no single
// IMPLEMENTS edge could describe. The alias entity itself must still exist —
// that is the positive half, without which this test would pass on an extractor
// that dropped type aliases entirely.
func TestPonyHierarchy_TypeAliasIsProducesNoEdge(t *testing.T) {
	src := `trait Named
type Shape is (Circle | Square)
type Solo is Named
`
	ents := runPony(t, src, "src/x.pony")
	if got := allImplements(ents); len(got) != 0 {
		t.Fatalf("IMPLEMENTS = %v, want none — type aliases are not conformance", got)
	}
	for _, n := range []string{"Shape", "Solo"} {
		if ponyFind(ents, n, "SCOPE.Component") == nil {
			t.Fatalf("alias entity %s missing — the negative above proved nothing", n)
		}
	}
}

// TestPonyHierarchy_SelfConformanceIsDropped: `class C is C` is not information
// and is the signature of a mis-attributed owner (#6369). The second
// declaration is the positive control — the guard must drop the self pair only,
// not the whole clause it appears in.
func TestPonyHierarchy_SelfConformanceIsDropped(t *testing.T) {
	src := `class C is C
class D is (D & Named)
`
	ents := runPony(t, src, "src/x.pony")
	if got := implementsOf(ents, "C"); len(got) != 0 {
		t.Errorf("C: IMPLEMENTS = %v, want none", got)
	}
	if got := implementsOf(ents, "D"); !eqStrings(got, []string{"Named"}) {
		t.Errorf("D: IMPLEMENTS = %v, want [Named]", got)
	}
}

// TestPonyHierarchy_QualifiedAndGenericTargetsRecordTheWrittenForm asserts BOTH
// halves of the ToID convention on one declaration each: the target is the BARE
// name (so an out-of-tree stdlib trait can bind by name) AND the written form
// survives in the `base` property (so the qualifier/arguments are not lost).
// The third case is the control that `base` is ABSENT when nothing was erased —
// without it, a producer that always stamped `base` would pass.
func TestPonyHierarchy_QualifiedAndGenericTargetsRecordTheWrittenForm(t *testing.T) {
	cases := []struct {
		decl     string
		wantTo   string
		wantBase string // "" means the property must be absent
	}{
		{"class C is col.Hashable", "Hashable", "col.Hashable"},
		{"class C is Comparable[C]", "Comparable", "Comparable[C]"},
		{"class C is Stringable", "Stringable", ""},
	}
	for _, tc := range cases {
		t.Run(tc.decl, func(t *testing.T) {
			ents := runPony(t, tc.decl+"\n", "src/x.pony")
			comp := ponyFind(ents, "C", "SCOPE.Component")
			if comp == nil {
				t.Fatal("component C missing")
			}
			var rel *types.RelationshipRecord
			for i := range comp.Relationships {
				if comp.Relationships[i].Kind == "IMPLEMENTS" {
					rel = &comp.Relationships[i]
				}
			}
			if rel == nil {
				t.Fatalf("no IMPLEMENTS edge for %q", tc.decl)
			}
			if rel.ToID != tc.wantTo {
				t.Errorf("ToID = %q, want %q", rel.ToID, tc.wantTo)
			}
			// Props is key-SORTED and Get binary-searches it (#6802): a
			// literal-built Props in the wrong key order reads a present key as
			// absent. Reading every key back is what observes that.
			if got := rel.Properties.Get("base"); got != tc.wantBase {
				t.Errorf("base = %q, want %q", got, tc.wantBase)
			}
			if got := rel.Properties.Get("provenance"); got != "pony_is_clause" {
				t.Errorf("provenance = %q, want pony_is_clause", got)
			}
			if got := rel.Properties.Get("line"); got != "1" {
				t.Errorf("line = %q, want 1", got)
			}
		})
	}
}

// TestPonyHierarchy_EdgeIsAnchoredOnTheTypeNotTheFile: FromID must be EMPTY so
// the assembly loop stamps the owning record's own id. A non-empty non-hex
// FromID is rewritten onto the FILE entity, merging every type in the file onto
// one node (#6295, #6298). The file entity must carry no IMPLEMENTS of its own.
func TestPonyHierarchy_EdgeIsAnchoredOnTheTypeNotTheFile(t *testing.T) {
	ents := runPony(t, "trait Named\nclass A is Named\nclass B is Named\n", "src/x.pony")
	n := 0
	for i := range ents {
		e := &ents[i]
		for _, r := range e.Relationships {
			if r.Kind != "IMPLEMENTS" {
				continue
			}
			n++
			if r.FromID != "" {
				t.Errorf("%s: FromID = %q, want empty", e.Name, r.FromID)
			}
			if strings.HasPrefix(e.Kind, "SCOPE.File") || e.Kind == "SCOPE.File" {
				t.Errorf("IMPLEMENTS edge landed on the FILE entity %s", e.Name)
			}
		}
	}
	if n != 2 {
		t.Fatalf("emitted %d IMPLEMENTS edges, want 2", n)
	}
}

// TestPonyHierarchy_NoDuplicateComponents is the trap groovy's equivalent test
// exists for: emitting from cross/hierarchy would mint a SECOND SCOPE.Component
// per type, plus one for each named trait. Both halves are asserted — one
// component per declared type, and NO component synthesised for the
// out-of-tree trait named only in a clause.
func TestPonyHierarchy_NoDuplicateComponents(t *testing.T) {
	src := `trait Named
class A is (Named & Stringable)
class B is Named
`
	ents := runPony(t, src, "src/x.pony")
	count := map[string]int{}
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" {
			count[ents[i].Name]++
		}
	}
	for _, n := range []string{"Named", "A", "B"} {
		if count[n] != 1 {
			t.Errorf("component %s appears %d times, want 1", n, count[n])
		}
	}
	if count["Stringable"] != 0 {
		t.Errorf("Stringable was synthesised as a component %d times, want 0", count["Stringable"])
	}
}

// TestPonyHierarchy_CapabilityBeforeNameIsInvisible_KnownDivergence pins
// today's WRONG output on purpose. `class val Foo is Bar` is real Pony —
// a reference-capability may precede the name — but typeDeclarationRE is
// `^(class|…)\s+([A-Za-z_]\w*)`, so it binds the CAPABILITY as the type name
// and the anchor then lands on ` Foo is Bar`, which is not `is`. The entity
// half of this defect predates #6370; the edge half is inherited from it.
// Whoever widens typeDeclarationRE gets a failing test telling them the clause
// is theirs too.
//
// Verified as a real tripwire by mutating TOWARD the fix (allowing an optional
// capability token before the name in typeDeclarationRE): this test fires and
// TestPonyHierarchy_EveryDeclarationKeywordConforms stays green.
func TestPonyHierarchy_CapabilityBeforeNameIsInvisible_KnownDivergence(t *testing.T) {
	ents := runPony(t, "trait Bar\nclass val Foo is Bar\n", "src/x.pony")
	if ponyFind(ents, "Foo", "SCOPE.Component") != nil {
		t.Fatal("`class val Foo` now yields a Foo component — typeDeclarationRE was widened; " +
			"delete this known-divergence test and assert Foo IMPLEMENTS Bar instead")
	}
	if got := allImplements(ents); len(got) != 0 {
		t.Fatalf("IMPLEMENTS = %v; the divergence changed shape — re-derive it", got)
	}
	if ponyFind(ents, "val", "SCOPE.Component") == nil {
		t.Fatal("the capability token is no longer mis-bound as the type name; " +
			"this divergence is fixed, update the test")
	}
}

// TestPonyHierarchy_DeclarationInsideBlockCommentOverFires_KnownDivergence pins
// the limit the package header states: the anchor decides which `is` belongs to
// a declaration and has no opinion on whether the DECLARATION is real. A
// column-0 `class … is …` inside a `/* … */` block comment or a `"""…"""`
// docstring is line-initial, so typeDeclarationRE matches it and the edge is
// emitted. This asserts today's wrong output so it is a fact on record rather
// than a sentence in a comment.
//
// Verified as a real tripwire by mutating TOWARD the fix (blanking `/*…*/` and
// `"""…"""` regions before extraction): this test fires and nothing else in the
// package moves.
func TestPonyHierarchy_DeclarationInsideBlockCommentOverFires_KnownDivergence(t *testing.T) {
	src := `/*
class Ghost is Phantom
*/
class Real is Named
`
	ents := runPony(t, src, "src/x.pony")
	got := allImplements(ents)
	want := []string{"Ghost->Phantom", "Real->Named"}
	if !eqStrings(got, want) {
		t.Fatalf("IMPLEMENTS = %v, want %v — if Ghost->Phantom is gone the extractor "+
			"gained comment awareness: delete this known-divergence test", got, want)
	}
}
