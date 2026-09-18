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
// present and correct while the outer type ALSO gained it. So both halves have
// to be graded separately — a fix that suppresses the clause inside the nested
// span entirely would turn a wrong edge into a missing one and would still look
// like progress under any count assertion. Every test below therefore pairs a
// MUST-HAVE row on the nested owner with a FORBIDDEN row on the outer owner.
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
