package nim_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #7231 — the type pass deduped by NAME ALONE, per file, first match wins.
//
// READ THIS FIRST: THE RECORD COUNTS BELOW ARE NOT GRAPH RECALL. #7231 was
// filed and initially fixed as a recall bug and it is not one.
// `graph.EntityID(repo, kind, name, sourceFile)` (internal/graph/graph.go:259)
// does not hash StartLine, so two records sharing Kind+Name+SourceFile derive
// the same id, and EVERY consumer of what extractNim returns folds them:
//
//   - cmd/grafel/index.go:6303 — `if !seenEntity[id]`, first kept, rest dropped;
//   - internal/extractors/incremental.go's convertExtractedRecords — the same
//     derivation via entityRecordToGraphEntity (:2046); its own doc calls the
//     collision "EXPECTED, not erroneous";
//   - internal/daemon/extract/subproc.go:358 is a TRANSPORT, not a terminal
//     consumer: its envelopes decode into extract.Coordinate's Result.Entities,
//     which cmd/grafel/index.go:1824 assigns to pass1Records and hands to the
//     same assembly loop as the first bullet.
//
// (internal/extractors/cross/consumes_api calls only its own client/endpoint
// extractors and never reaches a language extractor, so it is not in the set.)
//
// So the extra records DO NOT REACH THE GRAPH, the type-entity count stays at
// 6572, and this change is a NO-OP at the graph layer for entities. What it is
// worth is the written-down and graded fold rule, the tests, and the removal of
// an unreachable map. Whether entity identity should carry a line component is
// a separate, higher-blast-radius question, filed on its own.
//
// WHAT THE EXTRACTOR USED TO DROP. UNPINNED MEASUREMENT, 2026-09-18, over a
// 4431-file Nim population (nim-lang/Nim, nimbus-eth2, pixie, nitter, jester).
// `archigraph-corpora` contains zero `.nim` files, so NOTHING IN THIS TREE
// ASSERTS THESE FIGURES and they will drift: 415 of 6987 type RECORDS were
// discarded. 221 of the 255 duplicated (file,name) groups — 86.7% — are
// declarations sitting in DIFFERENT routine bodies (proc/template/macro/block/
// static), i.e. genuinely distinct coexisting types in disjoint scopes; 17 are
// `when`-branch conditional compilation, of which exactly 1 differs in Subtype.
// The strongest figure this file actually PINS is a whole-file edge total on a
// hand-written fixture; everything else above is prose.
//
// WHAT THE FIX IS, IN TWO HALVES THAT MUST BE GRADED SEPARATELY.
//
//  1. The name dedup is GONE, so every declaration becomes a record.
//
//  2. Every edge the type loop owns — EXTENDS AND CONTAINS — is emitted only
//     from the FIRST (lowest byte offset) declaration of a name in the file.
//
// HALF 2 IS THE ONLY PART THAT CHANGES THE GRAPH, AND ONLY VIA EXTENDS.
// Neither assembly seam puts its RELATIONSHIP loop inside the entity fold:
// index.go:6446 walks `r.Relationships` outside the `!seenEntity[id]` block and
// incremental.go's carries an explicit comment saying it is "NOT inside the
// else", both defaulting a blank FromID to the derived id. So a DROPPED
// duplicate's edges are unioned onto the SURVIVOR. Ungated, two declarations of
// one name with different bases make the single surviving node assert
// `EXTENDS BaseA` AND `EXTENDS BaseB` — an edge no declaration states.
//
// CONTAINS escapes that only by accident, and the "773 duplicate edges"
// justification #7231 was re-scoped on is therefore WRONG AT THE GRAPH LAYER: a
// duplicate's CONTAINS shares FromID, ToID and Kind with the survivor's, so
// index.go:6453's `seenRel[relID]` folds it and half 1 alone would have added
// ZERO CONTAINS edges to the graph, not 773. The 773 are extractor-level. The
// CONTAINS half of the gate is belt-and-braces (it does still skip a whole-file
// rescan per duplicate); the EXTENDS half is the live fix, because EXTENDS
// duplicates do NOT share a ToID and nothing downstream collapses them.
//
// NOT AN OPTION, AND MEASURED RATHER THAN ARGUED: scoping the member scan to
// the declaration's own span. In Nim a "method" is a free-standing proc taking
// the type as its FIRST PARAMETER, declared OUTSIDE the type body. Over all
// 9370 (type-declaration, matching-proc) pairs in the population, pairs with
// the proc inside the declaration's span = 0 and outside = 9370. That change
// takes CONTAINS from 8597 to 0.
//
// THE DIRECTION THIS PACKAGE WAS STRUCTURALLY BLIND TO, and why the fixtures
// are shaped the way they are. The whole nim suite is GREEN both before and
// after this change, so nothing here graded either half. The dangerous mutation
// is not the gate failing to fire — a duplicate edge is visible in any count —
// it is the gate firing TOO BROADLY: suppressing an edge for a name the file
// declares only ONCE. That loses real edges while the duplicate count stays at
// zero and every aggregate in this package stays green. A fixture containing
// only duplicated names cannot see it at all, because in such a fixture "first
// declaration of this name" and "first declaration in this file" are the same
// set.
//
// BOTH EDGE KINDS GET THAT TREATMENT, separately. The first round of this work
// gated CONTAINS only and shipped an ALIVE mutant: nothing in this package had
// an `of Base` clause on a duplicated name in EITHER direction, so gating
// `baseOfEdge` — or failing to — was invisible. `nimExtendsDupFixture` exists
// for exactly that row and is not a variation on the CONTAINS one.
//
// So `nimDupFixture` below declares, in one file:
//
//	Anchor  — declared ONCE, BEFORE any duplicate   (has a method)
//	Widget  — declared TWICE, in two proc bodies,
//	          with DIFFERENT subtypes (object / ref object) (has a method)
//	Gadget  — declared ONCE, AFTER the duplicates    (has a method)
//
// Anchor and Gadget straddle the duplicated group on both sides, so a guard
// keyed file-wide rather than per-name breaks at least one of them whichever
// end it latches onto. Widget's two declarations differ in Subtype so a guard
// keyed on name+subtype (which would let both emit) is separated from one keyed
// on name. And the two Widget declarations are asserted individually — the
// LOWER-line one carries the edge, the higher-line one carries none — so
// reversing "first" to "last" is a distinct failure from losing the guard.
//
// The mutant table with RUN/PASS/FAIL per row is in the PR body.
// ---------------------------------------------------------------------------

// nimDupFixture is one file declaring a name twice inside two different proc
// bodies, straddled by two singly-declared types that each have a method.
//
// LEGALITY. `doc/grammar.txt` gives `typeDef = identVisDot genericParamList?
// pragma? ('=' optInd typeDefValue)?` and `section(RULE) = COMMENT? RULE /
// (COMMENT? optInd (RULE / COMMENT)^+i dedent)`, and a `type` section is a
// `complexOrSimpleStmt`, which `simpleStmt / complexOrSimpleStmt` admits inside
// any routine body — a routine body is `stmt`, not a restricted form. So a
// `type` section nested in a proc body is grammatical, and that is the shape
// 86.7% of the population actually has. There is no Nim toolchain on this
// machine; the derivation above is the whole of the legality claim.
const nimDupFixture = `
import strutils

type Anchor = object
  id: int

proc firstUser() =
  type Widget = object
    a: int
  discard

proc secondUser() =
  type Widget = ref object
    b: int
  discard

type Gadget = object
  c: int

proc label*(a: Anchor): string = discard
proc render*(w: Widget): string = discard
proc paint*(g: Gadget) = discard
`

// lineOf returns the 1-based line of the first line whose trimmed text equals
// want. Expected StartLines are DERIVED from the fixture text rather than
// written as literals, so the assertion says "this entity's StartLine is the
// line its own declaration is written on" and cannot be satisfied by copying
// back whatever the code happened to produce.
func lineOf(t *testing.T, src, want string) int {
	t.Helper()
	for i, ln := range strings.Split(src, "\n") {
		if strings.TrimSpace(ln) == want {
			return i + 1
		}
	}
	t.Fatalf("fixture has no line %q — the fixture and the test have drifted apart", want)
	return 0
}

// nimComponents returns every SCOPE.Component with the given name, excluding
// the `import` stubs buildImportEntities mints (which are also
// SCOPE.Component).
func nimComponents(ents []types.EntityRecord, name string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Name == name && e.Subtype != "import" {
			out = append(out, e)
		}
	}
	return out
}

// nimContainsToIDs returns the ToIDs of one record's CONTAINS edges.
func nimContainsToIDs(e types.EntityRecord) []string {
	var out []string
	for _, r := range e.Relationships {
		if r.Kind == "CONTAINS" {
			out = append(out, r.ToID)
		}
	}
	return out
}

// TestFirstDecl7231_DuplicateNameYieldsTwoEntities grades HALF 1 on its own:
// the declaration that first-wins used to discard is now an entity, with its
// own line and its own subtype.
func TestFirstDecl7231_DuplicateNameYieldsTwoEntities(t *testing.T) {
	ents := runNim(t, nimDupFixture, "dup.nim")

	widgets := nimComponents(ents, "Widget")
	if len(widgets) != 2 {
		t.Fatalf("Widget entities = %d, want 2 (the second declaration was discarded by a name-keyed dedup)", len(widgets))
	}

	wantFirst := lineOf(t, nimDupFixture, "type Widget = object")
	wantSecond := lineOf(t, nimDupFixture, "type Widget = ref object")

	got := map[int]string{}
	for _, w := range widgets {
		got[w.StartLine] = w.Subtype
	}
	if sub, ok := got[wantFirst]; !ok || sub != "object" {
		t.Errorf("declaration at line %d: subtype %q present=%v, want \"object\" present=true (have %v)", wantFirst, sub, ok, got)
	}
	if sub, ok := got[wantSecond]; !ok || sub != "ref object" {
		t.Errorf("declaration at line %d: subtype %q present=%v, want \"ref object\" present=true (have %v)", wantSecond, sub, ok, got)
	}
}

// TestFirstDecl7231_ContainsOnlyFromLowestLineDeclaration grades HALF 2's
// restrictive direction AND its ordering: the duplicated name's edge set is
// emitted exactly once, and from the declaration with the LOWER line.
//
// "First" is lowest byte offset. It is read off the iteration order of
// typeRE's FindAllStringSubmatchIndex — successive non-overlapping matches,
// left to right — and off no map. Line order is a faithful stand-in here
// because typeRE is `(?m)^`-anchored, so every match starts at a line start
// and no two matches can share one.
func TestFirstDecl7231_ContainsOnlyFromLowestLineDeclaration(t *testing.T) {
	ents := runNim(t, nimDupFixture, "dup.nim")

	widgets := nimComponents(ents, "Widget")
	if len(widgets) != 2 {
		t.Fatalf("Widget entities = %d, want 2", len(widgets))
	}
	lo, hi := widgets[0], widgets[1]
	if lo.StartLine > hi.StartLine {
		lo, hi = hi, lo
	}

	loEdges := nimContainsToIDs(lo)
	hiEdges := nimContainsToIDs(hi)

	if len(loEdges) != 1 {
		t.Errorf("Widget@%d (first declaration) CONTAINS = %v, want exactly 1 edge (to render)", lo.StartLine, loEdges)
	} else if !strings.Contains(loEdges[0], "render") {
		t.Errorf("Widget@%d CONTAINS ToID = %q, want the ref for proc render", lo.StartLine, loEdges[0])
	}
	if len(hiEdges) != 0 {
		t.Errorf("Widget@%d (second declaration) CONTAINS = %v, want none — every declaration of a name "+
			"produces a byte-identical edge set, so a second copy is pure duplication", hi.StartLine, hiEdges)
	}
}

// TestFirstDecl7231_SingleDeclaredTypesKeepTheirContains is the PERMISSIVE
// direction and the row this package had nothing on. Anchor is declared once
// BEFORE the duplicated group and Gadget once AFTER it; a guard that suppresses
// on anything coarser than the name — a file-wide "already emitted" flag, a
// per-file key, a counter — silences one of them while every duplicate count in
// this package stays at zero.
func TestFirstDecl7231_SingleDeclaredTypesKeepTheirContains(t *testing.T) {
	ents := runNim(t, nimDupFixture, "dup.nim")

	for _, tc := range []struct {
		typeName string
		method   string
		where    string
	}{
		{"Anchor", "label", "declared once, BEFORE the duplicated group"},
		{"Gadget", "paint", "declared once, AFTER the duplicated group"},
	} {
		t.Run(tc.typeName, func(t *testing.T) {
			got := nimComponents(ents, tc.typeName)
			if len(got) != 1 {
				t.Fatalf("%s entities = %d, want 1 (%s)", tc.typeName, len(got), tc.where)
			}
			edges := nimContainsToIDs(got[0])
			if len(edges) != 1 {
				t.Fatalf("%s (%s) CONTAINS = %v, want exactly 1 edge to proc %s — "+
					"the first-declaration guard must key on the NAME, so it can never fire for a name "+
					"this file declares only once", tc.typeName, tc.where, edges, tc.method)
			}
			if !strings.Contains(edges[0], tc.method) {
				t.Errorf("%s CONTAINS ToID = %q, want the ref for proc %s", tc.typeName, edges[0], tc.method)
			}
		})
	}
}

// TestFirstDecl7231_TotalContainsUnchangedByDuplication is the population-scale
// non-regression in miniature: the edge set this file produces is EXACTLY the
// one the pre-#7231 extractor produced, because the pre-#7231 extractor emitted
// one entity per name and this one emits the same edges from the first of each
// name. Three types, three methods, three edges — no more and no fewer.
//
// This is deliberately a whole-file total rather than a per-type check: it is
// the shape of the corpus assertion (8597 before, 8597 after, byte for byte)
// and it fails in BOTH directions — a lost guard adds a fourth edge, an
// over-broad guard drops to two.
func TestFirstDecl7231_TotalContainsUnchangedByDuplication(t *testing.T) {
	ents := runNim(t, nimDupFixture, "dup.nim")

	var all []string
	for _, e := range ents {
		if e.Kind != "SCOPE.Component" || e.Subtype == "import" {
			continue
		}
		for _, id := range nimContainsToIDs(e) {
			all = append(all, fmt.Sprintf("%s@%d -> %s", e.Name, e.StartLine, id))
		}
	}
	if len(all) != 3 {
		t.Fatalf("CONTAINS edges in the file = %d, want 3 (Anchor->label, Widget->render, Gadget->paint); got:\n  %s",
			len(all), strings.Join(all, "\n  "))
	}
}

// nimWhenFixture is the `when`-branch shape — 17 of the 255 duplicated groups,
// and the one the issue was originally filed on (`AtomicFlag` in
// Nim/lib/pure/concurrency/atomics.nim, `distinct int8` in one branch and
// `object` in the other). The branches are mutually exclusive at compile time,
// so unlike the routine-body case the two declarations really are competing
// descriptions of one type; the rule here is stated rather than accidental —
// BOTH are emitted, and the LOWER-line one carries the edges.
//
// LEGALITY: `doc/grammar.txt` gives `whenStmt = 'when' expr colcom stmt
// (IND{=} 'elif' expr colcom stmt)* (IND{=} 'else' colcom stmt)?`, and `stmt`
// admits a `type` section, so a `type` section under a `when` branch is
// grammatical.
const nimWhenFixture = `
when defined(windows):
  type AtomicFlag = distinct int8
else:
  type AtomicFlag = object
    v: bool

proc clear*(f: var AtomicFlag) = discard
`

func TestFirstDecl7231_WhenBranchesBothSurvive(t *testing.T) {
	ents := runNim(t, nimWhenFixture, "atomics.nim")

	flags := nimComponents(ents, "AtomicFlag")
	if len(flags) != 2 {
		t.Fatalf("AtomicFlag entities = %d, want 2 — both `when` branches are declarations and "+
			"the extractor has no basis for preferring either", len(flags))
	}

	bySub := map[string]types.EntityRecord{}
	for _, f := range flags {
		bySub[f.Subtype] = f
	}
	for _, want := range []string{"distinct", "object"} {
		if _, ok := bySub[want]; !ok {
			t.Errorf("no AtomicFlag with subtype %q; got %v", want, bySub)
		}
	}

	lo, hi := flags[0], flags[1]
	if lo.StartLine > hi.StartLine {
		lo, hi = hi, lo
	}
	if gotLo := nimContainsToIDs(lo); len(gotLo) != 1 {
		t.Errorf("AtomicFlag@%d (first branch) CONTAINS = %v, want 1 edge to proc clear", lo.StartLine, gotLo)
	}
	if gotHi := nimContainsToIDs(hi); len(gotHi) != 0 {
		t.Errorf("AtomicFlag@%d (second branch) CONTAINS = %v, want none", hi.StartLine, gotHi)
	}
}

// nimExtendsDupFixture is the row the first round of #7231 shipped ALIVE: an
// `of Base` clause on BOTH declarations of a duplicated name, alongside a
// singly-declared type that also has one.
//
// This is the only fixture shape that can observe the gate on `baseOfEdge`.
// Without it, gating EXTENDS and not gating it produce identical output for
// every source in this package, so the mutation is undetectable — which is
// exactly what happened.
//
// WHY THE UNGATED BEHAVIOUR IS A REAL GRAPH DEFECT AND NOT A COSMETIC ONE.
// `Dual`'s two records fold to ONE node (graph.EntityID omits StartLine), but
// the assembly seams run their relationship loop OUTSIDE that fold and default
// a blank FromID to the derived id — so ungated, the surviving `Dual` node
// asserts `EXTENDS BaseA` and `EXTENDS BaseB` at once. Those two edges have
// different ToIDs, so `seenRel` cannot collapse them the way it silently
// collapses duplicate CONTAINS. `Solo` is the control: a singly-declared type
// whose EXTENDS must survive any gate keyed on the name.
//
// LEGALITY: `doc/grammar.txt` gives `objectDecl = 'object' pragma? ('of'
// typeDesc)? COMMENT? objectPart`, so `ref object of Base` is the inheritance
// form; the `type`-section-inside-a-routine-body derivation is the one
// nimDupFixture already relies on.
const nimExtendsDupFixture = `
type Solo = ref object of SoloBase
  x: int

proc firstUser() =
  type Dual = ref object of BaseA
    a: int
  discard

proc secondUser() =
  type Dual = ref object of BaseB
    b: int
  discard
`

// nimExtendsToIDs returns the ToIDs of one record's EXTENDS edges.
func nimExtendsToIDs(e types.EntityRecord) []string {
	var out []string
	for _, r := range e.Relationships {
		if r.Kind == "EXTENDS" {
			out = append(out, r.ToID)
		}
	}
	return out
}

// TestFirstDecl7231_ExtendsOnlyFromFirstDeclaration is the restrictive
// direction of the EXTENDS gate: the second declaration of a duplicated name
// contributes no inheritance edge, so the node the two records fold onto
// asserts exactly the base its own surviving declaration writes.
func TestFirstDecl7231_ExtendsOnlyFromFirstDeclaration(t *testing.T) {
	ents := runNim(t, nimExtendsDupFixture, "dualbase.nim")

	duals := nimComponents(ents, "Dual")
	if len(duals) != 2 {
		t.Fatalf("Dual records = %d, want 2", len(duals))
	}
	lo, hi := duals[0], duals[1]
	if lo.StartLine > hi.StartLine {
		lo, hi = hi, lo
	}

	loBases := nimExtendsToIDs(lo)
	if len(loBases) != 1 || loBases[0] != "BaseA" {
		t.Errorf("Dual@%d (first declaration) EXTENDS = %v, want exactly [BaseA]", lo.StartLine, loBases)
	}
	if hiBases := nimExtendsToIDs(hi); len(hiBases) != 0 {
		t.Errorf("Dual@%d (second declaration) EXTENDS = %v, want none — both records fold onto one "+
			"graph node whose relationship loop runs OUTSIDE the fold, so this edge is unioned onto "+
			"the survivor and makes it claim a base no surviving declaration states", hi.StartLine, hiBases)
	}
}

// TestFirstDecl7231_SingleDeclaredTypeKeepsItsExtends is the PERMISSIVE
// direction for EXTENDS — the twin of the CONTAINS row above, scored
// separately because the gate can be over-broad for one edge kind and not the
// other: they are two different call sites.
func TestFirstDecl7231_SingleDeclaredTypeKeepsItsExtends(t *testing.T) {
	ents := runNim(t, nimExtendsDupFixture, "dualbase.nim")

	solos := nimComponents(ents, "Solo")
	if len(solos) != 1 {
		t.Fatalf("Solo records = %d, want 1", len(solos))
	}
	bases := nimExtendsToIDs(solos[0])
	if len(bases) != 1 || bases[0] != "SoloBase" {
		t.Fatalf("Solo EXTENDS = %v, want exactly [SoloBase] — Solo is declared ONCE, so a gate keyed "+
			"on the name can never suppress its inheritance edge", bases)
	}
}

// TestFirstDecl7231_TotalExtendsUnchangedByDuplication is the EXTENDS twin of
// the CONTAINS total: a whole-file count that fails in BOTH directions — an
// ungated `baseOfEdge` makes it 3, an over-broad gate makes it 1.
func TestFirstDecl7231_TotalExtendsUnchangedByDuplication(t *testing.T) {
	ents := runNim(t, nimExtendsDupFixture, "dualbase.nim")

	var all []string
	for _, e := range ents {
		if e.Kind != "SCOPE.Component" || e.Subtype == "import" {
			continue
		}
		for _, id := range nimExtendsToIDs(e) {
			all = append(all, fmt.Sprintf("%s@%d -> %s", e.Name, e.StartLine, id))
		}
	}
	if len(all) != 2 {
		t.Fatalf("EXTENDS edges in the file = %d, want 2 (Solo->SoloBase, Dual->BaseA); got:\n  %s",
			len(all), strings.Join(all, "\n  "))
	}
}
