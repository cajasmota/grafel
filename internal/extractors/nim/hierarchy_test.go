package nim_test

import (
	"fmt"
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

// requireComponents fails unless every named type is present as a
// SCOPE.Component.
//
// Every "emits no EXTENDS" assertion below MUST call this first. allExtends
// counts edges across the whole record set and says nothing about whether the
// declaring type still exists, so without this a negative test passes when the
// type stops being extracted at all — the type vanishing and the edge being
// correctly withheld look identical to it. extendsOf has this guard built in;
// the allExtends callers did not, which is the hole this closes.
func requireComponents(t *testing.T, ents []types.EntityRecord, names ...string) {
	t.Helper()
	for _, n := range names {
		if nimFind(ents, n, "SCOPE.Component") == nil {
			t.Fatalf("vacuous negative: no SCOPE.Component named %q, so \"no EXTENDS\" "+
				"is satisfied by the type having vanished (entities: %s)", n, entityNames(ents))
		}
	}
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
// `ref` keyword. Everything else is byte-identical between the two halves: the
// same owner name, the same base, the same body, the same file, produced from
// one template string. Both forms are inheritance in Nim and both must emit the
// same edge.
//
// Written as a template rather than as two hand-written sources on purpose. The
// first version of this test asserted a controlled comparison in its doc
// comment while its body held a single case and left the `ref` half to a
// different test with a different owner, base and file — nothing was held
// constant, so the comment claimed a comparison the body never made.
func TestNimHierarchy_PlainObjectOfEmitsExtends(t *testing.T) {
	const tmpl = "type ApiError = %sobject of CatchableError\n  code: int\n"
	for _, tc := range []struct{ name, refKw string }{
		{"without ref", ""},
		{"with ref", "ref "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ents := runNim(t, fmt.Sprintf(tmpl, tc.refKw), "e.nim")
			wantOneExtends(t, ents, "ApiError", "CatchableError", "1")
		})
	}
}

// TestNimHierarchy_RefObjectSeparatorIsNotJustOneSpace — varies ONLY the
// whitespace between `ref` and `object`, holding the owner, base, body and file
// constant. typeRE's kind group is `ref\s+object`, so the kind string reaching
// isObjectKind can be "ref object", "ref\tobject" or "ref  object"; a check
// written as `kind == "ref object"` handles the first and silently drops the
// others. That mutant was ALIVE against the rest of this package.
func TestNimHierarchy_RefObjectSeparatorIsNotJustOneSpace(t *testing.T) {
	const tmpl = "type Dog = ref%sobject of Animal\n  name: string\n"
	for _, tc := range []struct{ name, sep string }{
		{"single space", " "},
		{"tab", "\t"},
		{"two spaces", "  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ents := runNim(t, fmt.Sprintf(tmpl, tc.sep), "d.nim")
			wantOneExtends(t, ents, "Dog", "Animal", "1")
		})
	}
}

// TestNimHierarchy_OfMustBeFollowedByWhitespace — varies ONLY whether a space
// separates `of` from the base name, holding everything else constant.
// `ofAnimal` is one identifier, not the keyword `of` applied to `Animal`, so it
// must yield nothing. The separator is `[ \t]+` and not `[ \t]*` for exactly
// this reason; the `*` mutant was ALIVE against the rest of this package and
// against the golden fixture.
func TestNimHierarchy_OfMustBeFollowedByWhitespace(t *testing.T) {
	glued := runNim(t, "type Dog = ref object ofAnimal\n  name: string\n", "d.nim")
	requireComponents(t, glued, "Dog")
	if got := allExtends(glued); len(got) != 0 {
		t.Fatalf("`object ofAnimal` is one identifier, not inheritance, but got %+v", got)
	}
	// Positive control on the same axis: insert the space and only the space.
	spaced := runNim(t, "type Dog = ref object of Animal\n  name: string\n", "d.nim")
	wantOneExtends(t, spaced, "Dog", "Animal", "1")
}

// TestNimHierarchy_UnterminatedGenericDoesNotSwallowLaterLines — varies ONLY
// whether the generic argument list on the declaration line is closed, holding
// the owner, base and file constant. nimObjectOfRE's argument class is
// `[^\]\n]*`, and the `\n` is what stops an unclosed `[` from consuming the rest
// of the file up to some unrelated `]`. Dropping the `\n` from that class was an
// ALIVE mutant: ToID is unaffected, so only the `base` property can observe it.
func TestNimHierarchy_UnterminatedGenericDoesNotSwallowLaterLines(t *testing.T) {
	src := "type Box = ref object of Container[int\nproc f(xs: seq[int]) = discard\n"
	ents := runNim(t, src, "g.nim")
	r := wantOneExtends(t, ents, "Box", "Container", "1")
	if got := r.Properties.Get("base"); got != "" {
		t.Errorf("unclosed generic on the declaration line must record no written "+
			"form, got base=%q — the argument class swallowed past the newline", got)
	}

	// Positive control on the same axis: close the bracket on that same line and
	// the written form IS recorded, so the assertion above is about the newline
	// and not about `base` never being set.
	closed := runNim(t, "type Box = ref object of Container[int]\nproc f(xs: seq[int]) = discard\n", "g.nim")
	if got := wantOneExtends(t, closed, "Box", "Container", "1").Properties.Get("base"); got != "Container[int]" {
		t.Errorf("closed generic: base=%q, want Container[int]", got)
	}
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
	cases := []struct{ name, owner, src string }{
		{"case-statement-branch", "Token", `type Token = object
  kind: int

proc render(t: Token): string =
  case t.kind
  of 0: "zero"
  of 1: "one"
  else: "many"
`},
		{"object-variant-branch", "Node", `type Node = object
  case kind: NodeKind
  of nkInt:
    intVal: int
  of nkStr:
    strVal: string
`},
		{"runtime-type-test", "Holder", `type Holder = ref object
  payload: RootRef

proc check(h: Holder): bool =
  h.payload of Widget
`},
		{"trailing-line-comment", "Plain", "type Plain = object # of Animal\n  a: int\n"},
		{"doc-comment", "Plain", "type Plain = object ## inherits of Animal in spirit only\n  a: int\n"},
		{"string-literal", "Plain", "type Plain = object\n  a: string\n\nconst note = \"of Animal\"\n"},
		{"of-on-the-next-line", "Plain", "type Plain = ref object\n  of Animal\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := runNim(t, tc.src, "n.nim")
			// Without this the case passes when the declaring type is not
			// extracted at all — see requireComponents.
			requireComponents(t, ents, tc.owner)
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
	for _, tc := range []struct{ owner, src string }{
		{"Colour", "type Colour = enum of Base\n"},
		{"Pair", "type Pair = tuple of Base\n"},
		{"Id", "type Id = distinct int of Base\n"},
	} {
		ents := runNim(t, tc.src, "k.nim")
		// The kind restriction is only observable while the type is still an
		// entity; without this the row passes if typeRE stops matching it.
		requireComponents(t, ents, tc.owner)
		if got := allExtends(ents); len(got) != 0 {
			t.Errorf("%q emitted %d EXTENDS: %+v", tc.src, len(got), got)
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
	requireComponents(t, ents, "Loop")
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

// TestNimHierarchy_InsideTripleQuotedStringOverFires_KnownDivergence — a whole
// type declaration sitting inside a `"""…"""` string literal is extracted as a
// real declaration, entity and EXTENDS edge alike.
//
// This extractor has no string awareness. The package header says the anchor is
// the whole guard, and the anchor only decides which `of` belongs to a
// declaration — it has no opinion on whether the DECLARATION is real. Inside a
// triple-quoted block the `type` line is line-initial, so typeRE matches it and
// the `of` sits exactly where inheritance sits.
//
// The entity half is pre-existing; the EXTENDS half is new with #6370, so the
// edge half is this change's to own. Asserted as the current WRONG output on
// purpose: whoever adds string scrubbing is told by a failing test that they
// have also fixed this, rather than discovering it.
//
// The `#`-comment case is the positive control and is NOT a divergence: typeRE
// is anchored `(?m)^[ \t]*(?:type\s+)?[A-Z]`, so a `#` before `type` makes the
// line structurally unmatchable. That is why the golden fixture's commented-out
// `Ghost` decoy cannot fire for THIS extractor and its forbidden_entities rows
// say so, while this shape can.
func TestNimHierarchy_InsideTripleQuotedStringOverFires_KnownDivergence(t *testing.T) {
	src := "const doc = \"\"\"\ntype Ghost = ref object of Widget\n\"\"\"\n\n" +
		"type Real* = ref object of RootObj\n  a: int\n"
	ents := runNim(t, src, "s.nim")

	if nimFind(ents, "Ghost", "SCOPE.Component") == nil {
		t.Fatalf("KNOWN DIVERGENCE CHANGED: a type declared inside a \"\"\"…\"\"\" string is " +
			"no longer extracted as an entity. If you added string scrubbing, delete this " +
			"test and add the positive assertion, and promote the golden fixture's Ghost/Widget " +
			"forbidden_entities rows to a reachable decoy.")
	}
	if !nimHasRel(ents, "Ghost", "SCOPE.Component", "EXTENDS", "Widget") {
		t.Fatalf("KNOWN DIVERGENCE CHANGED: the string-literal declaration no longer emits " +
			"EXTENDS -> Widget. See the message above.")
	}

	// Positive control: the real declaration outside the string is unaffected,
	// so the divergence is about the string and not about the file being
	// mis-parsed wholesale.
	wantOneExtends(t, ents, "Real", "RootObj", "5")

	// Negative control on the neighbouring axis: the same declaration behind a
	// `#` is excluded, and by typeRE's line anchor rather than by any string or
	// comment awareness.
	commented := runNim(t, "# type Ghost = ref object of Widget\ntype Real* = ref object of RootObj\n", "c.nim")
	requireComponents(t, commented, "Real")
	if e := nimFind(commented, "Ghost", "SCOPE.Component"); e != nil {
		t.Errorf("a commented-out declaration became an entity: %+v", e)
	}
	if got := len(allExtends(commented)); got != 1 {
		t.Errorf("commented-out form: want exactly 1 EXTENDS (Real -> RootObj), got %d", got)
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
