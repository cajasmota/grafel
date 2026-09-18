package fsharp_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7187 — collectHierarchyEdges regex-scanned a type's WHOLE body for
// `inherit` / `interface ... with` with no notion of a nested declaration, so a
// nested type's clause was attributed to the OUTER type as well.
//
// The defect is a SPURIOUS edge, not a missing one: on the reproducer measured
// on 248e89025 (and unchanged on 4d88f6372) the nested type's own edge was
// present and correct while the outer type ALSO gained it. A fix that
// suppressed the clause inside the nested span entirely would turn a wrong edge
// into a missing one and would still look like progress under any count
// assertion, so the MUST-HAVE direction is what has to be graded.
//
// WHICH ROWS ACTUALLY GRADE, stated honestly because it is easy to miscount
// (reviewed on PR #7188). Where a type declares a clause of its own, its row is
// `eqStrs(got, []string{own})`, and the paired "does not contain T" row is
// DOMINATED BY IT on every input, not merely on the mutants scored: a forbidden
// row fires iff T is in got, and T != own, so got != [own] and the eqStrs row
// fires too. Those forbidden rows are DIAGNOSTICS — they name the leaked target
// in the failure message — and they are not a second graded axis. The
// forbidden-direction rows that genuinely carry weight are the `len(got) != 0`
// rows on an owner that declares NOTHING (ShallowHost, WrapHost, DeepHost,
// CollideHost), because those owners have no eqStrs row to dominate them.
//
// The forbidden target name is UNIQUE in each fixture (#7144 / #7152: F# dedup
// keys are name-keyed, so a name shared with another entity merges silently and
// is invisible to a presence check and to a count).
//
// DERIVED-NOT-EXECUTED: no F# toolchain exists on this machine (dotnet, fsc,
// fsharpc, fsi, mono all absent), so no fixture here was compiled. The shallow
// form in the first test is the exact text measured on the issue; the
// conventional form in the second is F#'s ordinary nested-type layout.

// fsHierTargets returns the targets of `kind` carried by the named type, and
// fails if the type has no entity at all (a fix that deleted the entity would
// otherwise pass a "does not have the edge" assertion vacuously).
func fsHierTargets(t *testing.T, ents []types.EntityRecord, name, kind string) []string {
	t.Helper()
	rec := fsFind(ents, name, "SCOPE.Component")
	if rec == nil {
		t.Fatalf("no SCOPE.Component entity named %q — the forbidden-row assertions below would be vacuous without it", name)
	}
	return fsRelTargets(rec, kind)
}

// TestFSharp_NestedType7187_ShallowNesting is the issue's own reproducer, with
// every name made unique so no dedup merge can hide a row.
//
// Axes VARIED: which owner is asserted (outer vs nested), and the direction of
// the assertion (must-have vs forbidden).
// Axes HELD CONSTANT: the nested header's indent (1), the clause's indent (5),
// the outer declaration's column (0), the clause keyword (`inherit`), the file
// path, and the fact that the outer type declares NO inheritance of its own.
func TestFSharp_NestedType7187_ShallowNesting(t *testing.T) {
	const src = "module M\n" + // 1
		"\n" + // 2
		"type SeventySevenBase() =\n" + // 3
		"    class end\n" + // 4
		"\n" + // 5
		"type ShallowHost =\n" + // 6   column 0, inherits NOTHING
		" type ShallowNested =\n" + // 7   column 1 — inside ShallowHost's body
		"     inherit SeventySevenBase()\n" // 8

	ents := runFSharp(t, src, "src/Shallow.fs")

	// MUST-HAVE: the nested type keeps its own edge.
	if got := fsHierTargets(t, ents, "ShallowNested", "EXTENDS"); !eqStrs(got, []string{"SeventySevenBase"}) {
		t.Errorf("shallow: ShallowNested EXTENDS = %v, want [SeventySevenBase] — the nested type's OWN edge must survive", got)
	}

	// FORBIDDEN: the outer type must not absorb it.
	for _, tgt := range fsHierTargets(t, ents, "ShallowHost", "EXTENDS") {
		if tgt == "SeventySevenBase" {
			t.Errorf("shallow: ShallowHost EXTENDS contains SeventySevenBase — the nested type's inherit clause leaked to the OUTER owner")
		}
	}
	if got := fsHierTargets(t, ents, "ShallowHost", "EXTENDS"); len(got) != 0 {
		t.Errorf("shallow: ShallowHost EXTENDS = %v, want none — ShallowHost declares no inheritance of its own", got)
	}
}

// TestFSharp_NestedType7187_ConventionalNesting holds the outer type's OWN
// clauses fixed while a nested type carries clauses of its own. This is the
// test that distinguishes a nesting-aware scan from a blanket suppression: the
// outer type must keep exactly its own two edges and gain neither of the
// nested type's.
//
// Axes VARIED: clause kind (EXTENDS via `inherit`, IMPLEMENTS via
// `interface ... with`) and owner (outer vs nested).
// Axes HELD CONSTANT: the nesting depth (one level), the nested header's indent
// (4, the conventional member column), the file path, and the presence of a
// real member inside each `interface ... with` block.
func TestFSharp_NestedType7187_ConventionalNesting(t *testing.T) {
	const src = "module M\n" + // 1
		"\n" + // 2
		"type HostOnlyBase() =\n" + // 3
		"    class end\n" + // 4
		"\n" + // 5
		"type NestOnlyBase() =\n" + // 6
		"    class end\n" + // 7
		"\n" + // 8
		"type IHostOnly =\n" + // 9
		"    abstract member Ping : unit -> unit\n" + // 10
		"\n" + // 11
		"type INestOnly =\n" + // 12
		"    abstract member Pong : unit -> unit\n" + // 13
		"\n" + // 14
		"type ConvHost() =\n" + // 15
		"    inherit HostOnlyBase()\n" + // 16
		"    interface IHostOnly with\n" + // 17
		"        member _.Ping () = ()\n" + // 18
		"    type ConvNested() =\n" + // 19
		"        inherit NestOnlyBase()\n" + // 20
		"        interface INestOnly with\n" + // 21
		"            member _.Pong () = ()\n" // 22

	ents := runFSharp(t, src, "src/Conv.fs")

	// MUST-HAVE, outer: its own clauses are untouched by the nesting fix.
	if got := fsHierTargets(t, ents, "ConvHost", "EXTENDS"); !eqStrs(got, []string{"HostOnlyBase"}) {
		t.Errorf("conventional: ConvHost EXTENDS = %v, want exactly [HostOnlyBase] — the outer type's own inherit must survive and the nested one must not appear", got)
	}
	if got := fsHierTargets(t, ents, "ConvHost", "IMPLEMENTS"); !eqStrs(got, []string{"IHostOnly"}) {
		t.Errorf("conventional: ConvHost IMPLEMENTS = %v, want exactly [IHostOnly] — the outer type's own interface clause must survive and the nested one must not appear", got)
	}

	// MUST-HAVE, nested: its own clauses are emitted on it.
	if got := fsHierTargets(t, ents, "ConvNested", "EXTENDS"); !eqStrs(got, []string{"NestOnlyBase"}) {
		t.Errorf("conventional: ConvNested EXTENDS = %v, want [NestOnlyBase] — the nested type's OWN inherit must survive", got)
	}
	if got := fsHierTargets(t, ents, "ConvNested", "IMPLEMENTS"); !eqStrs(got, []string{"INestOnly"}) {
		t.Errorf("conventional: ConvNested IMPLEMENTS = %v, want [INestOnly] — the nested type's OWN interface clause must survive", got)
	}

	// FORBIDDEN, stated separately from the must-have rows above so a change
	// that empties the outer's slice is not read as a pass of the must-have.
	for _, tgt := range fsHierTargets(t, ents, "ConvHost", "EXTENDS") {
		if tgt == "NestOnlyBase" {
			t.Errorf("conventional: ConvHost EXTENDS contains NestOnlyBase — the nested type's inherit clause leaked to the OUTER owner")
		}
	}
	for _, tgt := range fsHierTargets(t, ents, "ConvHost", "IMPLEMENTS") {
		if tgt == "INestOnly" {
			t.Errorf("conventional: ConvHost IMPLEMENTS contains INestOnly — the nested type's interface clause leaked to the OUTER owner")
		}
	}
}

// TestFSharp_NestedType7187_SiblingAfterNestedResumes pins the RESUMPTION
// boundary: a clause that sits back at the outer type's member column AFTER a
// nested type's body has closed still belongs to the outer type. Masking that
// ran to the end of the outer body — rather than to the end of the nested
// type's own indentation block — would silently drop it.
//
// Axis VARIED: the position of the outer's own clause (after the nested block
// rather than before it).
// Axis HELD CONSTANT: everything else in the conventional fixture's shape.
func TestFSharp_NestedType7187_SiblingAfterNestedResumes(t *testing.T) {
	const src = "module M\n" + // 1
		"\n" + // 2
		"type ResumeHostBase() =\n" + // 3
		"    class end\n" + // 4
		"\n" + // 5
		"type ResumeNestBase() =\n" + // 6
		"    class end\n" + // 7
		"\n" + // 8
		"type ResumeHost() =\n" + // 9
		"    type ResumeNested() =\n" + // 10
		"        inherit ResumeNestBase()\n" + // 11
		"    inherit ResumeHostBase()\n" // 12  back at the member column

	ents := runFSharp(t, src, "src/Resume.fs")

	if got := fsHierTargets(t, ents, "ResumeHost", "EXTENDS"); !eqStrs(got, []string{"ResumeHostBase"}) {
		t.Errorf("resume: ResumeHost EXTENDS = %v, want exactly [ResumeHostBase] — the outer clause AFTER the nested block must still be attributed to the outer type", got)
	}
	if got := fsHierTargets(t, ents, "ResumeNested", "EXTENDS"); !eqStrs(got, []string{"ResumeNestBase"}) {
		t.Errorf("resume: ResumeNested EXTENDS = %v, want [ResumeNestBase]", got)
	}
}

// TestFSharp_NestedType7187_MultiLineNestedHeader covers a nested declaration
// whose HEADER spans lines. typeRE's `\s+`, `(?:<[^>]*>)?` and
// `(?:\([^)]*\))?` all cross newlines, so the header's continuation line is not
// the first body line — and if the masker starts walking at the newline after
// the match START rather than at its END, that continuation is mistaken for a
// body line and terminates the block early whenever it is indented at or
// shallower than the `type` column. The nested clause then stays in the outer
// body and #7187 survives verbatim. Reported on PR #7188 (fixture P4a).
//
// Axis VARIED: the continuation line's indent relative to the nested `type`
// column — 2 (shallower, the leaking case) and 8 (deeper, the control).
// Axes HELD CONSTANT: the nested `type` column (4), the clause's indent (8),
// the clause keyword (`inherit`), the outer type's own lack of inheritance,
// the file path, and the wrapped construct (a parenthesised parameter list).
//
// DERIVED-NOT-EXECUTED: a continuation indented shallower than its own `type`
// keyword is very likely an offside violation in real F#, so this row grades
// the masker's agreement with extractIndentBody rather than a shape a compiler
// would accept. That agreement is the property the doc comment claims.
func TestFSharp_NestedType7187_MultiLineNestedHeader(t *testing.T) {
	const shallowCont = "module M\n" + // 1
		"\n" + // 2
		"type WrapBase() =\n" + // 3
		"    class end\n" + // 4
		"\n" + // 5
		"type WrapHost() =\n" + // 6
		"    type WrapNested(\n" + // 7   header opens here, column 4
		"  x: int) =\n" + // 8   continuation at column 2 — shallower
		"        inherit WrapBase()\n" // 9

	ents := runFSharp(t, shallowCont, "src/Wrap.fs")
	if got := fsHierTargets(t, ents, "WrapNested", "EXTENDS"); !eqStrs(got, []string{"WrapBase"}) {
		t.Errorf("shallow continuation: WrapNested EXTENDS = %v, want [WrapBase] — the nested type's OWN edge must survive", got)
	}
	if got := fsHierTargets(t, ents, "WrapHost", "EXTENDS"); len(got) != 0 {
		t.Errorf("shallow continuation: WrapHost EXTENDS = %v, want none — the multi-line nested header must be masked from the END of the matched header, not from the newline after its start", got)
	}

	// Control: the same shape with the continuation indented DEEPER than the
	// nested `type` column. This one was already clean before the fix to the
	// scan start, so it grades the absence of a regression, not the fix.
	const deepCont = "module M\n" + // 1
		"\n" + // 2
		"type DeepBase() =\n" + // 3
		"    class end\n" + // 4
		"\n" + // 5
		"type DeepHost() =\n" + // 6
		"    type DeepNested(\n" + // 7
		"        x: int) =\n" + // 8   column 8 — deeper
		"        inherit DeepBase()\n" // 9

	ents = runFSharp(t, deepCont, "src/Deep.fs")
	if got := fsHierTargets(t, ents, "DeepNested", "EXTENDS"); !eqStrs(got, []string{"DeepBase"}) {
		t.Errorf("deep continuation control: DeepNested EXTENDS = %v, want [DeepBase]", got)
	}
	if got := fsHierTargets(t, ents, "DeepHost", "EXTENDS"); len(got) != 0 {
		t.Errorf("deep continuation control: DeepHost EXTENDS = %v, want none", got)
	}

	// THREE-line header. This arm exists because a two-line header alone leaves
	// a hole: a masker that starts at the newline after the match START but then
	// delegates to extractIndentBody still passes the two-line case, since
	// extractIndentBody appends its FIRST line unconditionally when it is
	// non-blank (the same-line-body case) and so absorbs one shallow
	// continuation. It takes a SECOND shallow continuation for that variant to
	// terminate early and leak. Scored as mutant M4 on PR #7188, which was ALIVE
	// at 0 --- FAIL until this arm existed.
	const twoShallowConts = "module M\n" + // 1
		"\n" + // 2
		"type ThreeBase() =\n" + // 3
		"    class end\n" + // 4
		"\n" + // 5
		"type ThreeHost() =\n" + // 6
		"    type ThreeNested(\n" + // 7   header opens, column 4
		"  x: int,\n" + // 8   continuation 1, column 2
		"  y: int) =\n" + // 9   continuation 2, column 2
		"        inherit ThreeBase()\n" // 10

	ents = runFSharp(t, twoShallowConts, "src/Three.fs")
	if got := fsHierTargets(t, ents, "ThreeNested", "EXTENDS"); !eqStrs(got, []string{"ThreeBase"}) {
		t.Errorf("three-line header: ThreeNested EXTENDS = %v, want [ThreeBase]", got)
	}
	if got := fsHierTargets(t, ents, "ThreeHost", "EXTENDS"); len(got) != 0 {
		t.Errorf("three-line header: ThreeHost EXTENDS = %v, want none — two shallow continuation lines must not terminate the masked block", got)
	}
}

// TestFSharp_NestedType7187_NameCollisionDropsTheEdge pins a RECALL LOSS this
// change introduces, so that it is graded rather than merely disclosed.
//
// The masking predicate is typeRE over the outer BODY; the re-attribution
// predicate is typeRE over `src` AND the file-level `typeSeen` name filter. The
// regex is shared, the COMPOSED predicate is not, and typeSeen sits between
// them: a nested type whose name collides with an earlier type produces no
// entity, so its clause is excised from the outer type and has nowhere to land.
//
// BEFORE this change the clause landed on the OUTER type (wrong owner, edge
// present). AFTER it, no entity in the file carries the edge. That is a
// wrong-edge → missing-edge conversion — narrow, but new, and the same failure
// mode mutant M3 is rejected for. It is pinned here as the accepted trade
// rather than left unobserved: attributing an edge to a type that does not
// declare it is the error #6326 was built to avoid, and unwinding typeSeen is a
// change to entity identity across the whole extractor, not to this scan.
//
// Axis VARIED: whether the nested type's name collides with an earlier
// top-level type.
// Axes HELD CONSTANT: the nesting shape, indents, clause keyword, base name,
// and the file path — the non-colliding arm is the same fixture with the nested
// type renamed.
func TestFSharp_NestedType7187_NameCollisionDropsTheEdge(t *testing.T) {
	const collide = "module M\n" + // 1
		"\n" + // 2
		"type CollideBase() =\n" + // 3
		"    class end\n" + // 4
		"\n" + // 5
		"type Twin() =\n" + // 6   top-level, claims the name first
		"    class end\n" + // 7
		"\n" + // 8
		"type CollideHost() =\n" + // 9
		"    type Twin() =\n" + // 10  same name — typeSeen drops this entity
		"        inherit CollideBase()\n" // 11

	ents := runFSharp(t, collide, "src/Collide.fs")

	// The outer type must NOT absorb it — this is what fails pre-fix.
	if got := fsHierTargets(t, ents, "CollideHost", "EXTENDS"); len(got) != 0 {
		t.Errorf("collision: CollideHost EXTENDS = %v, want none — the nested clause must not be attributed to the outer type", got)
	}
	// And the accepted consequence: NOTHING carries it. Stated as an assertion
	// so a later change that restores the edge has to come here and say so.
	if got := fsHierTargets(t, ents, "Twin", "EXTENDS"); len(got) != 0 {
		t.Errorf("collision: Twin EXTENDS = %v, want none — the surviving Twin entity is the TOP-LEVEL one, which declares no inheritance; if this ever becomes [CollideBase] the nested type is being merged into the top-level one, which is a different defect", got)
	}

	// Control: rename the nested type and the edge lands where it belongs, so
	// the loss above is attributable to the NAME COLLISION and nothing else.
	const distinct = "module M\n" +
		"\n" +
		"type CollideBase() =\n" +
		"    class end\n" +
		"\n" +
		"type Twin() =\n" +
		"    class end\n" +
		"\n" +
		"type CollideHost() =\n" +
		"    type Triplet() =\n" +
		"        inherit CollideBase()\n"

	ents = runFSharp(t, distinct, "src/Collide.fs")
	if got := fsHierTargets(t, ents, "Triplet", "EXTENDS"); !eqStrs(got, []string{"CollideBase"}) {
		t.Errorf("distinct-name control: Triplet EXTENDS = %v, want [CollideBase] — only the name collision may cost the edge", got)
	}
	if got := fsHierTargets(t, ents, "CollideHost", "EXTENDS"); len(got) != 0 {
		t.Errorf("distinct-name control: CollideHost EXTENDS = %v, want none", got)
	}
}

// TestFSharp_NestedType7187_BraceGuardReadsTheUnmaskedBody grades the CHOICE of
// input to the object-expression brace guard (#6326's `insideBraces`), which
// this change had to make and which nothing observed until PR #7188's EC-1
// mutant — swapping both guard call sites to consult the MASKED text — came
// back ALIVE at 0 `--- FAIL`.
//
// The two inputs can differ, and the shape required is narrow. Masking replaces
// bytes with spaces and preserves length, so offsets are identical and only
// content differs; `insideBraces` asks `count("{") > count("}")` over the
// prefix. A verdict flip therefore needs a masked region holding MORE `}` than
// `{` — i.e. a brace region that OPENS outside a nested type's block and CLOSES
// inside it. Masking then eats the closer, the prefix looks permanently open,
// and every later clause is suppressed. The opposite imbalance cannot flip the
// verdict, because `>` reads a negative depth the same as zero.
//
// Below: the object expression opens on line 11 at column 8, the nested header
// is on line 13 at column 4, and the closing `}` on line 14 is inside the
// nested block. `BraceHost`'s own `inherit` on line 15 sits AFTER the object
// expression has closed, so it is genuinely its own clause and must be emitted.
// Measured: emitted with the guard on `scrubbed`; SILENTLY DROPPED with the
// guard on `masked`.
//
// Axis VARIED: nothing — this is a single constructed witness. Its role is to
// make the guard's input choice observable, not to sweep a space.
// Axes HELD CONSTANT: one nesting level, one object expression, the outer
// clause after the nested block, the file path.
//
// HONEST SCOPE, because the doc comment on maskNestedTypeBodies must not
// overstate it: a nested `type` header inside an unclosed object-expression
// brace is almost certainly NOT legal F#, and DERIVED-NOT-EXECUTED applies (no
// toolchain here). What this row proves is that the two inputs are NOT
// interchangeable in the extractor's own alphabet, so the choice is a real one
// and `scrubbed` is the conservative side of it. It does not claim the shape
// occurs in real code, and no corpus incidence has been counted.
func TestFSharp_NestedType7187_BraceGuardReadsTheUnmaskedBody(t *testing.T) {
	const src = "module M\n" + // 1
		"\n" + // 2
		"type BraceBase() =\n" + // 3
		"    class end\n" + // 4
		"\n" + // 5
		"type IThing =\n" + // 6
		"    abstract member Ping : unit -> unit\n" + // 7
		"\n" + // 8
		"type BraceHost() =\n" + // 9
		"    member _.Make () =\n" + // 10
		"        { new IThing with\n" + // 11  '{' OUTSIDE any nested block
		"            member _.Ping () = ()\n" + // 12
		"    type BraceNested() =\n" + // 13  nested header, column 4
		"        member _.Q = 0 }\n" + // 14  '}' INSIDE the nested block
		"    inherit BraceBase()\n" // 15  outer's own clause, after the close

	ents := runFSharp(t, src, "src/Brace.fs")
	if got := fsHierTargets(t, ents, "BraceHost", "EXTENDS"); !eqStrs(got, []string{"BraceBase"}) {
		t.Errorf("brace guard: BraceHost EXTENDS = %v, want [BraceBase] — the object expression closes on line 14, so line 15's inherit is BraceHost's own; a guard reading the MASKED body loses the closing brace with the nested block and suppresses it", got)
	}
}
