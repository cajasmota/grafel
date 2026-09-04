package nim_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// extendsOf returns every EXTENDS relationship embedded on the SCOPE.Component
// named `name`. It fails the test when the component itself is missing, so an
// "no edges" assertion can never be satisfied by the type having vanished.
func extendsOf(t *testing.T, ents []types.EntityRecord, name string) []types.RelationshipRecord {
	t.Helper()
	e := nimFind(ents, name, "SCOPE.Component")
	if e == nil {
		t.Fatalf("no SCOPE.Component named %q (entities: %s)", name, entityNames(ents))
	}
	var out []types.RelationshipRecord
	for _, r := range e.Relationships {
		if r.Kind == "EXTENDS" {
			out = append(out, r)
		}
	}
	return out
}

func entityNames(ents []types.EntityRecord) string {
	var b []string
	for _, e := range ents {
		b = append(b, e.Kind+":"+e.Name)
	}
	return strings.Join(b, ", ")
}

// allExtends returns every EXTENDS edge in the record set, whatever it is
// embedded on. Used by the negative controls: an edge that fired on the wrong
// entity is still an edge that fired.
func allExtends(ents []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "EXTENDS" {
				out = append(out, r)
			}
		}
	}
	return out
}

// wantOneExtends asserts exactly one EXTENDS on `owner` pointing at `to`, with
// an EMPTY FromID (so assembly anchors it on the type, not the file — #6295,
// #6298, #6367) and the given 1-based line.
func wantOneExtends(t *testing.T, ents []types.EntityRecord, owner, to string, line string) types.RelationshipRecord {
	t.Helper()
	rels := extendsOf(t, ents, owner)
	if len(rels) != 1 {
		t.Fatalf("%s: want exactly 1 EXTENDS, got %d (%+v)", owner, len(rels), rels)
	}
	r := rels[0]
	if r.ToID != to {
		t.Errorf("%s: EXTENDS ToID = %q, want %q", owner, r.ToID, to)
	}
	if r.FromID != "" {
		t.Errorf("%s: EXTENDS FromID = %q, want empty so assembly anchors it on the type", owner, r.FromID)
	}
	if got := r.Properties.Get("line"); got != line {
		t.Errorf("%s: EXTENDS line = %q, want %q", owner, got, line)
	}
	if got := r.Properties.Get("provenance"); got != "nim_object_of" {
		t.Errorf("%s: EXTENDS provenance = %q, want nim_object_of", owner, got)
	}
	// The edge's line must be the OWNING TYPE's own start line. Asserted
	// against the entity rather than only against a literal so a change that
	// moved both together (e.g. a typeRE fix) still has to keep them agreeing.
	if own := nimFind(ents, owner, "SCOPE.Component"); own != nil {
		if got, want := r.Properties.Get("line"), strconv.Itoa(own.StartLine); got != want {
			t.Errorf("%s: EXTENDS line = %q but the entity's StartLine is %s", owner, got, want)
		}
	}
	return r
}

// TestNimHierarchy_RefObjectOfEmitsExtends — varies ONLY whether the `ref
// object` declaration carries an `of Base` clause; the type name, the file and
// the surrounding source are held constant between the two halves. This is the
// primary shape: `type X = ref object of Y`.
func TestNimHierarchy_RefObjectOfEmitsExtends(t *testing.T) {
	with := runNim(t, "type Dog = ref object of Animal\n  name: string\n", "a.nim")
	r := wantOneExtends(t, with, "Dog", "Animal", "1")
	if got := r.Properties.Get("language"); got != "nim" {
		t.Errorf("EXTENDS language = %q, want nim", got)
	}
	if got := r.Properties.Get("base"); got != "" {
		t.Errorf("unqualified base should carry no `base` property, got %q", got)
	}

	without := runNim(t, "type Dog = ref object\n  name: string\n", "a.nim")
	if got := allExtends(without); len(got) != 0 {
		t.Fatalf("`ref object` with no `of` clause emitted %d EXTENDS: %+v", len(got), got)
	}
}

// TestNimHierarchy_PlainObjectOfEmitsExtends — varies ONLY the presence of the
// `ref` keyword (`object of` vs `ref object of`), holding the base and the
// clause constant. Both are inheritance in Nim and both must emit.
func TestNimHierarchy_PlainObjectOfEmitsExtends(t *testing.T) {
	ents := runNim(t, "type ApiError = object of CatchableError\n  code: int\n", "e.nim")
	wantOneExtends(t, ents, "ApiError", "CatchableError", "1")
}

// TestNimHierarchy_IndentedTypeBlockAndRootObjAndLocalBase — varies the BASE's
// identity across the three cases that matter (the universal stdlib root
// `RootObj`, a user-defined type declared in the same file, and a transitive
// second hop), holding the `type:` block layout constant. Also pins that a base
// declared in-file gets a plain bare-name ToID like any other, and that each
// type's line is its OWN line rather than the block's.
func TestNimHierarchy_IndentedTypeBlockAndRootObjAndLocalBase(t *testing.T) {
	src := `type
  Shape* = ref object of RootObj
    id*: int

  Circle* = ref object of Shape
    radius*: float

  Unit* = ref object of Circle
    scale*: float
`
	ents := runNim(t, src, "shapes.nim")
	// Shape's line is 1, not 2: typeRE's `type\s+` prefix matches across the
	// newline, so the first member of an indented `type` block is stamped with
	// the block keyword's line — for the ENTITY as well as for this edge. Pinned
	// as a divergence by
	// TestNimHierarchy_FirstMemberOfTypeBlockGetsBlockHeaderLine_KnownDivergence.
	wantOneExtends(t, ents, "Shape", "RootObj", "1")
	wantOneExtends(t, ents, "Circle", "Shape", "5")
	wantOneExtends(t, ents, "Unit", "Circle", "8")
}

// TestNimHierarchy_GenericAndQualifiedBasesAreReducedToTheBareName — varies the
// DECORATION on the base name (generic arguments, a module qualifier, both),
// holding the underlying base identity constant. ToID must be the bare name in
// every case, and the written form must be preserved in `base` rather than
// dropped.
func TestNimHierarchy_GenericAndQualifiedBasesAreReducedToTheBareName(t *testing.T) {
	cases := []struct {
		name, src, owner, toID, base string
	}{
		{"generic", "type Box = ref object of Container[int]\n", "Box", "Container", "Container[int]"},
		{"qualified", "type Boom = object of system.Exception\n", "Boom", "Exception", "system.Exception"},
		{"both", "type Both = ref object of pkg.mod.Base[T, U]\n", "Both", "Base", "pkg.mod.Base[T, U]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := runNim(t, tc.src, "g.nim")
			r := wantOneExtends(t, ents, tc.owner, tc.toID, "1")
			if got := r.Properties.Get("base"); got != tc.base {
				t.Errorf("base property = %q, want %q", got, tc.base)
			}
		})
	}
}

// TestNimHierarchy_OverloadedOfDoesNotFire — the central negative control.
//
// Varies WHERE the token `of` appears, holding constant that each source
// declares exactly one object type that has NO inheritance clause. Nim spells
// four different things `of`; only the one immediately following the `object`
// keyword on the declaration line is inheritance. Every case here must emit
// ZERO EXTENDS edges, on any entity.
//
// Note what is and is not claimed: the extractor has no comment or string
// awareness. The comment and string-literal cases are excluded because the `of`
// in them does not sit at the anchor position, NOT because anything scrubs
// them.
func TestNimHierarchy_OverloadedOfDoesNotFire(t *testing.T) {
	cases := []struct{ name, src string }{
		{"case-statement-branch", `type Token = object
  kind: int

proc render(t: Token): string =
  case t.kind
  of 0: "zero"
  of 1: "one"
  else: "many"
`},
		{"object-variant-branch", `type Node = object
  case kind: NodeKind
  of nkInt:
    intVal: int
  of nkStr:
    strVal: string
`},
		{"runtime-type-test", `type Holder = ref object
  payload: RootRef

proc check(h: Holder): bool =
  h.payload of Widget
`},
		{"trailing-line-comment", "type Plain = object # of Animal\n  a: int\n"},
		{"doc-comment", "type Plain = object ## inherits of Animal in spirit only\n  a: int\n"},
		{"string-literal", "type Plain = object\n  a: string\n\nconst note = \"of Animal\"\n"},
		{"of-on-the-next-line", "type Plain = ref object\n  of Animal\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := runNim(t, tc.src, "n.nim")
			if got := allExtends(ents); len(got) != 0 {
				t.Fatalf("emitted %d EXTENDS from a source with no inheritance: %+v", len(got), got)
			}
		})
	}
}

// TestNimHierarchy_NonObjectKindsNeverExtend — varies the type KEYWORD
// (enum/tuple/distinct vs object), holding the trailing `of Base` text
// constant. `of` is not part of those grammars, so these sources are not valid
// Nim; they exist to pin the kind restriction, which is the only thing that
// stops a token sequence with no defined meaning from minting an EXTENDS.
func TestNimHierarchy_NonObjectKindsNeverExtend(t *testing.T) {
	for _, src := range []string{
		"type Colour = enum of Base\n",
		"type Pair = tuple of Base\n",
		"type Id = distinct int of Base\n",
	} {
		ents := runNim(t, src, "k.nim")
		if got := allExtends(ents); len(got) != 0 {
			t.Errorf("%q emitted %d EXTENDS: %+v", src, len(got), got)
		}
	}
	// Positive control on the same axis: change ONLY the keyword to `object`
	// and the very same trailing text does produce an edge, so the assertions
	// above are excluding the keyword and not the source shape.
	ents := runNim(t, "type Colour = object of Base\n", "k.nim")
	wantOneExtends(t, ents, "Colour", "Base", "1")
}

// TestNimHierarchy_SelfInheritanceEmitsNoEdge — varies only whether the base
// name equals the owner. A self-edge is never information; it is the signature
// of a mis-attributed owner (#6369).
func TestNimHierarchy_SelfInheritanceEmitsNoEdge(t *testing.T) {
	ents := runNim(t, "type Loop = ref object of Loop\n", "s.nim")
	if got := allExtends(ents); len(got) != 0 {
		t.Fatalf("self-inheritance emitted %d EXTENDS: %+v", len(got), got)
	}
}

// TestNimHierarchy_NoDuplicateComponents — varies nothing; asserts the trap
// that registering a language in cross/hierarchy would spring. Emitting from
// this extractor must yield exactly ONE SCOPE.Component per declared type and
// NO synthesised component for a base, whether that base is in the file
// (Shape) or in the stdlib (RootObj).
func TestNimHierarchy_NoDuplicateComponents(t *testing.T) {
	src := `type
  Shape* = ref object of RootObj
    id*: int

  Circle* = ref object of Shape
    radius*: float
`
	ents := runNim(t, src, "shapes.nim")
	count := map[string]int{}
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			count[e.Name]++
		}
	}
	for _, n := range []string{"Shape", "Circle"} {
		if count[n] != 1 {
			t.Errorf("want exactly 1 SCOPE.Component %q, got %d", n, count[n])
		}
	}
	if count["RootObj"] != 0 {
		t.Errorf("base RootObj was materialised as %d component(s); the edge must dangle as a bare name", count["RootObj"])
	}
}

// TestNimHierarchy_PtrObjectIsInvisible_KnownDivergence — `type X = ptr object
// of Y` is valid Nim, and this extractor produces NOTHING for it: typeRE's kind
// alternation is `object|ref object|enum|tuple|distinct W`, so `ptr object` is
// not a type declaration as far as nim.go is concerned and X is not even an
// entity.
//
// This asserts the current WRONG output on purpose. Widening typeRE to accept
// `ptr object` is a reasonable future change, and without this test the person
// making it would get an entity with no EXTENDS edge and no signal that the
// hierarchy half was theirs to wire up too. When this test fails, that is the
// message: add `ptr` to isObjectKind alongside the typeRE change.
func TestNimHierarchy_PtrObjectIsInvisible_KnownDivergence(t *testing.T) {
	ents := runNim(t, "type Raw = ptr object of Animal\n  a: int\n", "p.nim")
	if e := nimFind(ents, "Raw", "SCOPE.Component"); e != nil {
		t.Fatalf("KNOWN DIVERGENCE CHANGED: `ptr object` now yields an entity. "+
			"If you widened typeRE, widen isObjectKind in hierarchy.go too and "+
			"replace this test with the positive assertion. (got %+v)", e)
	}
	if got := allExtends(ents); len(got) != 0 {
		t.Fatalf("KNOWN DIVERGENCE CHANGED: `ptr object` now emits %d EXTENDS: %+v", len(got), got)
	}
}

// TestNimHierarchy_MultipleTypesInOneFileEachKeepTheirOwnEdge — varies the
// NUMBER of inheriting types in one file, holding each declaration's shape
// constant. This is the shape an empty FromID protects: were the edge anchored
// on the file, all three would merge onto one node.
func TestNimHierarchy_MultipleTypesInOneFileEachKeepTheirOwnEdge(t *testing.T) {
	src := `type Dog = ref object of Animal
type Cat = ref object of Animal
type Rock = object
type Fish = object of Animal
`
	ents := runNim(t, src, "many.nim")
	wantOneExtends(t, ents, "Dog", "Animal", "1")
	wantOneExtends(t, ents, "Cat", "Animal", "2")
	wantOneExtends(t, ents, "Fish", "Animal", "4")
	if rels := extendsOf(t, ents, "Rock"); len(rels) != 0 {
		t.Errorf("Rock has no `of` clause but got %+v", rels)
	}
	if got := len(allExtends(ents)); got != 3 {
		t.Errorf("want 3 EXTENDS across the file, got %d", got)
	}
}

// TestNimHierarchy_FirstMemberOfTypeBlockGetsBlockHeaderLine_KnownDivergence —
// varies ONLY the position of a type within an indented `type` block (first
// member vs later member), holding the declaration shape constant.
//
// typeRE is `(?m)^[ \t]*(?:type\s+)?(Name)…` and `\s` matches a newline, so for
// the block form the match STARTS at the `type` keyword on the previous line.
// The first member's entity StartLine — and therefore its EXTENDS line — is the
// block header's line, while every later member gets its own. That is wrong,
// and it is pre-existing entity behaviour, not something the edge introduced.
//
// This asserts the current WRONG output on purpose. Whoever narrows typeRE's
// `type\s+` to `type[ \t]+` (plus a separate block-member pattern) will be told
// by this failing test that the hierarchy line stamping moves with it.
func TestNimHierarchy_FirstMemberOfTypeBlockGetsBlockHeaderLine_KnownDivergence(t *testing.T) {
	src := "type\n  First = ref object of RootObj\n    a: int\n\n  Second = ref object of First\n    b: int\n"
	ents := runNim(t, src, "blk.nim")

	first := nimFind(ents, "First", "SCOPE.Component")
	if first == nil {
		t.Fatal("no component First")
	}
	if first.StartLine != 1 {
		t.Fatalf("KNOWN DIVERGENCE CHANGED: First.StartLine = %d, was 1 (the `type` keyword's line, not First's line 2). "+
			"If you fixed typeRE, update this test and the line expectation in "+
			"TestNimHierarchy_IndentedTypeBlockAndRootObjAndLocalBase.", first.StartLine)
	}
	rels := extendsOf(t, ents, "First")
	if len(rels) != 1 || rels[0].Properties.Get("line") != "1" {
		t.Fatalf("KNOWN DIVERGENCE CHANGED: First's EXTENDS line is no longer 1: %+v", rels)
	}

	// Positive control on the same axis: the SECOND member is not affected, so
	// the divergence above is about block position and not about line stamping
	// generally.
	second := nimFind(ents, "Second", "SCOPE.Component")
	if second == nil {
		t.Fatal("no component Second")
	}
	if second.StartLine != 5 {
		t.Errorf("Second.StartLine = %d, want 5 (its own line)", second.StartLine)
	}
	if rels := extendsOf(t, ents, "Second"); len(rels) != 1 || rels[0].Properties.Get("line") != "5" {
		t.Errorf("Second's EXTENDS line: %+v, want 5", rels)
	}
}
