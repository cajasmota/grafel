package nim_test

import (
	"fmt"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #7190 — in an idiomatic Nim `type` SECTION every type absorbed the ones
// declared after it, and the FIRST member's StartLine pointed at the bare
// `type` keyword instead of at its own declaration.
//
// ONE ROOT, TWO SYMPTOMS — MEASURED, NOT ASSUMED. typeRE was
//
//	(?m)^[ \t]*(?:type\s+)?([A-Z]...)\s*(?:\[...\])?\s*=\s*(object|...)
//
// and `\s` matches a NEWLINE, so for the section form the match STARTED on the
// `type` keyword's line. Both symptoms fall out of that single wrong anchor:
//
//   - StartLine is `Count(src[:m[0]], "\n") + 1`, i.e. the anchor's line — the
//     `type` keyword's line for the first member, its own line for later ones
//     (which is why only the first was ever wrong);
//   - the declaration's own indentation is whatever the anchor's line has,
//     which for the block header is "" — so nim.go's type call site could not
//     pass a real base and hard-coded `baseIndentLen = 0`. At 0 every line
//     indented at all is body, so a member at column 2 swallows its siblings.
//
// The fix narrows `type\s+` to `type[ \t]+` (the anchor can no longer leave the
// declaration's line) and CAPTURES that line's indent, which the call site then
// passes as the base. hierarchy_test.go's
// ...FirstMemberOfTypeBlock..._KnownDivergence predicted exactly this narrowing
// and is converted to the fixed expectation by this PR.
//
// WHAT THE ABSORPTION IS OBSERVABLE THROUGH — and a correction to the issue.
// Issue #7190 says "anything derived from a type's body text — calls, edges,
// subtype classification — is therefore attributed to the first type". That is
// NOT true of this code as it stands: at the type call site the collected body
// feeds `endLine` and NOTHING else (no CALLS are computed there, the EXTENDS
// edge is derived from `src` at the match end, and the subtype from the kind
// group). So absorption is observable ONLY through EndLine, and the derived
// artefact available here is the EXTENDS edge's `line` property — which grades
// the StartLine half. That is asserted in
// TestTypeSection7190_ExtendsLineIsTheDeclarationsOwnLine rather than claimed.
//
// AXES VARIED, and CROSSED rather than listed. The axes compose, so the space
// is the product, not the list:
//
//	base indent of the declaration (0 / 2 / 4)
//	  x form (single-line `type X = object` / `type` SECTION)
//	  x members per section (1 / several)
//	  x trailing newline (present / absent)
//	  x what PRECEDES the construct (start of file / a proc header / an
//	    import + a `##` comment + a blank line / a preceding section)
//
// Occupied cells and the justification for the empty ones are tabulated in the
// PR body. The axis that every fixture in the neighbouring #7185/#7195 rows
// held CONSTANT is the base — all of them are at 0, which is the one value at
// which the hard-coded 0 is right — so their green says nothing here and is not
// read as reassurance.
//
// THE PERMISSIVE DIRECTION is a base that comes out TOO LARGE: it still stops
// absorbing siblings (so every absorption row stays green) while silently
// dropping legitimate body lines. TestTypeSection7190_SectionBodyAtBandIsKept
// is the must-have row for it: its field sits at exactly base+1, which only the
// correct base collects.
//
// DERIVED-NOT-EXECUTED: there is no Nim toolchain on this machine (`javac` is
// the only compiler present), so every fixture's legality is read off the Nim
// manual ("Lexical Analysis -> Indentation": IND{>} is "MORE SPACES than the
// entry at the top of the stack", IND{=} is "the SAME number of spaces", and no
// minimum step is named) rather than compiled. FALSIFIER: a Nim program in
// which a `type` section member indented two spaces under the keyword, with a
// field one space deeper than the member, is rejected.
// ---------------------------------------------------------------------------

const sectPath7190 = "src/domain/sections.nim"

// sect7190Idiomatic — the issue's shape: keyword at column 0, three members at
// column 2, fields at column 4, trailing newline. 7 lines.
const sect7190Idiomatic = "type\n" + // 1
	"  Alpha* = object\n" + // 2
	"    a*: int\n" + // 3
	"  Beta* = object\n" + // 4
	"    b*: int\n" + // 5
	"  Gamma* = object\n" + // 6
	"    c*: int\n" // 7

// sect7190NoTrailingNL — same shape, two members, NO trailing newline. Crosses
// the section form with the trailing-newline axis that #7195 moved.
const sect7190NoTrailingNL = "type\n" + // 1
	"  Alpha* = object\n" + // 2
	"    a*: int\n" + // 3
	"  Beta* = object\n" + // 4
	"    b*: int" // 5, file ends here

// sect7190Nested — a section NESTED in a proc: keyword at column 2, members at
// column 4. base 4. Varies the base and what precedes the construct.
const sect7190Nested = "proc wrap*() =\n" + // 1
	"  type\n" + // 2
	"    Inner* = object\n" + // 3
	"      x*: int\n" + // 4
	"    Other* = object\n" + // 5
	"      y*: int\n" // 6

// sect7190Band — a member at column 2 whose FIRST field sits at column 3, i.e.
// exactly base+1 (#7185's band), then a column-4 field, then a sibling member.
// Only a base of exactly 2 both keeps the band line AND stops at the sibling.
const sect7190Band = "type\n" + // 1
	"  Deep* = object\n" + // 2
	"   tight*: int\n" + // 3, indent 3 = base+1  <- THE BAND
	"    wide*: string\n" + // 4, indent 4
	"  Next* = object\n" + // 5, indent 2         <- sibling
	"    n*: int\n" // 6

// sect7190Preceded — two sections, each with one member, preceded by an import,
// a `##` doc comment and blank lines, with a blank line inside the first
// member's body before the next section. Varies "what precedes" and "members
// per section" (1, against sect7190Idiomatic's 3).
const sect7190Preceded = "import strutils\n" + // 1
	"\n" + // 2
	"## module doc\n" + // 3
	"type\n" + // 4
	"  First* = object\n" + // 5
	"    a*: int\n" + // 6
	"\n" + // 7
	"type\n" + // 8
	"  Second* = object\n" + // 9
	"    b*: int\n" // 10

// sect7190SingleLineNested — the SINGLE-LINE form at base 2, inside a proc,
// with a proc statement after it at the same column. The single-line form at
// column 0 is covered by the pre-existing #7185/#7195 rows; this is the cell
// they cannot occupy, and it absorbs the trailing `discard` before this fix.
const sect7190SingleLineNested = "proc f*() =\n" + // 1
	"  type Local* = object\n" + // 2
	"    x*: int\n" + // 3
	"  discard\n" // 4

// sect7190Extends — the derived-artefact fixture: a section whose members carry
// EXTENDS edges, so the StartLine half is graded on an emitted EDGE and not
// only on the entity's own span.
const sect7190Extends = "type\n" + // 1
	"  Base* = ref object of RootObj\n" + // 2
	"    a*: int\n" + // 3
	"  Derived* = ref object of Base\n" + // 4
	"    b*: int\n" // 5

func sect7190Corpus() map[string]string {
	return map[string]string{
		"7190/idiomatic":        sect7190Idiomatic,
		"7190/noTrailingNL":     sect7190NoTrailingNL,
		"7190/nested":           sect7190Nested,
		"7190/band":             sect7190Band,
		"7190/preceded":         sect7190Preceded,
		"7190/singleLineNested": sect7190SingleLineNested,
		"7190/extends":          sect7190Extends,
	}
}

// --- defect 2: StartLine is the declaration's own line ----------------------

func TestTypeSection7190_FirstMemberStartsAtItsOwnDeclaration(t *testing.T) {
	ents := band7185Run(t, sect7190Idiomatic, sectPath7190)
	for _, c := range []struct {
		name string
		want int
	}{{"Alpha", 2}, {"Beta", 4}, {"Gamma", 6}} {
		if got := band7185Get(t, ents, c.name, "SCOPE.Component").StartLine; got != c.want {
			t.Errorf("%s StartLine = %d, want %d (its own declaration line)", c.name, got, c.want)
		}
	}
}

func TestTypeSection7190_NestedSectionFirstMemberStartsAtItsOwnDeclaration(t *testing.T) {
	ents := band7185Run(t, sect7190Nested, sectPath7190)
	if got := band7185Get(t, ents, "Inner", "SCOPE.Component").StartLine; got != 3 {
		t.Errorf("Inner StartLine = %d, want 3 (not the nested `type` keyword's line 2)", got)
	}
	if got := band7185Get(t, ents, "Other", "SCOPE.Component").StartLine; got != 5 {
		t.Errorf("Other StartLine = %d, want 5", got)
	}
}

// The derived artefact for the StartLine half: hierarchy.go stamps the EXTENDS
// edge with the owning type's start line, so a wrong anchor is visible on an
// EDGE and not only on the entity.
func TestTypeSection7190_ExtendsLineIsTheDeclarationsOwnLine(t *testing.T) {
	ents := band7185Run(t, sect7190Extends, sectPath7190)
	wantOneExtends(t, ents, "Base", "RootObj", "2")
	wantOneExtends(t, ents, "Derived", "Base", "4")
}

// --- defect 1: a member does not absorb its siblings ------------------------

func TestTypeSection7190_SiblingsAreNotAbsorbed(t *testing.T) {
	ents := band7185Run(t, sect7190Idiomatic, sectPath7190)
	for _, c := range []struct {
		name       string
		start, end int
	}{{"Alpha", 2, 3}, {"Beta", 4, 5}, {"Gamma", 6, 7}} {
		e := band7185Get(t, ents, c.name, "SCOPE.Component")
		if e.StartLine != c.start || e.EndLine != c.end {
			t.Errorf("%s span = %d-%d, want %d-%d", c.name, e.StartLine, e.EndLine, c.start, c.end)
		}
	}
}

func TestTypeSection7190_NoTrailingNewlineSectionSpans(t *testing.T) {
	ents := band7185Run(t, sect7190NoTrailingNL, sectPath7190)
	alpha := band7185Get(t, ents, "Alpha", "SCOPE.Component")
	beta := band7185Get(t, ents, "Beta", "SCOPE.Component")
	if alpha.StartLine != 2 || alpha.EndLine != 3 {
		t.Errorf("Alpha span = %d-%d, want 2-3", alpha.StartLine, alpha.EndLine)
	}
	if beta.StartLine != 4 || beta.EndLine != 5 {
		t.Errorf("Beta span = %d-%d, want 4-5", beta.StartLine, beta.EndLine)
	}
}

func TestTypeSection7190_NestedSectionSiblingsAreNotAbsorbed(t *testing.T) {
	ents := band7185Run(t, sect7190Nested, sectPath7190)
	inner := band7185Get(t, ents, "Inner", "SCOPE.Component")
	other := band7185Get(t, ents, "Other", "SCOPE.Component")
	if inner.EndLine != 4 {
		t.Errorf("Inner EndLine = %d, want 4 (Other at column 4 is a sibling, not body)", inner.EndLine)
	}
	if other.EndLine != 6 {
		t.Errorf("Other EndLine = %d, want 6", other.EndLine)
	}
	// The enclosing proc is unchanged: its base is computed, not hard-coded, so
	// it legitimately spans the whole section.
	if w := band7185Get(t, ents, "wrap", "SCOPE.Operation"); w.StartLine != 1 || w.EndLine != 6 {
		t.Errorf("wrap span = %d-%d, want 1-6", w.StartLine, w.EndLine)
	}
}

// The single-line form at base 2: before this fix Local's span ran to line 4
// and swallowed the proc's own `discard` statement.
func TestTypeSection7190_SingleLineFormAtBaseTwoDoesNotAbsorbSibling(t *testing.T) {
	ents := band7185Run(t, sect7190SingleLineNested, sectPath7190)
	local := band7185Get(t, ents, "Local", "SCOPE.Component")
	if local.StartLine != 2 || local.EndLine != 3 {
		t.Errorf("Local span = %d-%d, want 2-3 (the `discard` at column 2 is the proc's, not Local's body)", local.StartLine, local.EndLine)
	}
}

func TestTypeSection7190_TwoSectionsPrecededByImportAndComment(t *testing.T) {
	ents := band7185Run(t, sect7190Preceded, sectPath7190)
	first := band7185Get(t, ents, "First", "SCOPE.Component")
	second := band7185Get(t, ents, "Second", "SCOPE.Component")
	// First's body legitimately includes the blank line 7 (a blank before a
	// sibling is a real line of the file — #7195's
	// TestEOF7195ForbiddenEarlierDeclUnchanged forbids trimming it), and stops
	// at the column-0 `type` on line 8.
	if first.StartLine != 5 || first.EndLine != 7 {
		t.Errorf("First span = %d-%d, want 5-7", first.StartLine, first.EndLine)
	}
	if second.StartLine != 9 || second.EndLine != 10 {
		t.Errorf("Second span = %d-%d, want 9-10", second.StartLine, second.EndLine)
	}
}

// --- the permissive direction: a base that comes out too large --------------

// MUST-HAVE ROW. `tight*: int` sits at exactly base+1. A base of 2 keeps it
// (#7185's threshold is base+1) and stops at the column-2 sibling; a base of 3
// or 4 — the shape a "use the NAME's column" or "base+1" mis-derivation
// produces — drops it and collapses the span, while every absorption row above
// stays green.
func TestTypeSection7190_SectionBodyAtBandIsKept(t *testing.T) {
	ents := band7185Run(t, sect7190Band, sectPath7190)
	deep := band7185Get(t, ents, "Deep", "SCOPE.Component")
	if deep.StartLine != 2 || deep.EndLine != 4 {
		t.Errorf("Deep span = %d-%d, want 2-4 (the base+1 field on line 3 is body)", deep.StartLine, deep.EndLine)
	}
	next := band7185Get(t, ents, "Next", "SCOPE.Component")
	if next.StartLine != 5 || next.EndLine != 6 {
		t.Errorf("Next span = %d-%d, want 5-6", next.StartLine, next.EndLine)
	}
}

// --- forbidden row, with its own positive control ---------------------------

// sect7190Absorbed returns one message per pair of SCOPE.Component entities
// where one's span CONTAINS the other's declaration line. Returned as values,
// not t.Errorf'd, so the detection is itself graded
// (TestTypeSection7190_AbsorptionDetectorFires) instead of assumed: an absence
// assertion passes identically whether it is enforced or unreachable.
func sect7190Absorbed(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind != "SCOPE.Component" || ents[i].EndLine == 0 {
			continue
		}
		for j := range ents {
			if i == j || ents[j].Kind != "SCOPE.Component" || ents[j].StartLine == 0 {
				continue
			}
			if ents[i].StartLine < ents[j].StartLine && ents[j].StartLine <= ents[i].EndLine {
				out = append(out, fmt.Sprintf("%s (%d-%d) absorbs %s declared on line %d",
					ents[i].Name, ents[i].StartLine, ents[i].EndLine, ents[j].Name, ents[j].StartLine))
			}
		}
	}
	return out
}

func TestTypeSection7190_AbsorptionDetectorFires(t *testing.T) {
	// Positive control: the pre-fix shape (Alpha 1-6, Beta 4-7, Gamma 6-7 as
	// measured on 32bdc22b4) must be detected, and the post-fix shape must not,
	// so the forbidden row below is neither a no-op nor off by one.
	pre := []types.EntityRecord{
		{Name: "Alpha", Kind: "SCOPE.Component", StartLine: 1, EndLine: 6},
		{Name: "Beta", Kind: "SCOPE.Component", StartLine: 4, EndLine: 7},
		{Name: "Gamma", Kind: "SCOPE.Component", StartLine: 6, EndLine: 7},
	}
	if got := sect7190Absorbed(pre); len(got) != 3 {
		t.Errorf("pre-fix spans: got %d violations %v, want 3 (Alpha/Beta, Alpha/Gamma, Beta/Gamma)", len(got), got)
	}
	post := []types.EntityRecord{
		{Name: "Alpha", Kind: "SCOPE.Component", StartLine: 2, EndLine: 3},
		{Name: "Beta", Kind: "SCOPE.Component", StartLine: 4, EndLine: 5},
		{Name: "Gamma", Kind: "SCOPE.Component", StartLine: 6, EndLine: 7},
	}
	if got := sect7190Absorbed(post); len(got) != 0 {
		t.Errorf("post-fix spans: got %v, want no violations", got)
	}
	// An adjacent span that ENDS on the line before the next declaration is the
	// boundary case and must not be reported.
	adj := []types.EntityRecord{
		{Name: "A", Kind: "SCOPE.Component", StartLine: 1, EndLine: 3},
		{Name: "B", Kind: "SCOPE.Component", StartLine: 4, EndLine: 5},
	}
	if got := sect7190Absorbed(adj); len(got) != 0 {
		t.Errorf("adjacent spans: got %v, want no violations", got)
	}
}

// FORBIDDEN ROW: no type's span may contain another type's declaration line,
// across every #7190 fixture and every pre-existing section fixture in the
// package. Graded through the SAME helper the control above exercises.
func TestTypeSection7190_NoComponentAbsorbsAnother(t *testing.T) {
	corpus := sect7190Corpus()
	// Pre-existing section shapes from the package, so this row is not graded
	// only by fixtures written alongside the fix.
	corpus["pkg/typeSection"] = "type\n  Animal* = object\n    name*: string\n  Dog* = ref object of Animal\n    breed*: string\n"
	corpus["pkg/typeDiscovery"] = "type\n  Person = object\n    name: string\n    age: int\n\n  Direction = enum\n    North, South, East, West\n\n  Point = tuple\n    x, y: int\n\n  UserId = distinct int\n"
	// singleLineNested holds exactly ONE component, so it cannot exhibit a
	// component-absorbs-component pair by construction; its absorption (of the
	// proc's `discard` statement, which is not an entity) is graded by
	// TestTypeSection7190_SingleLineFormAtBaseTwoDoesNotAbsorbSibling instead.
	delete(corpus, "7190/singleLineNested")
	if len(corpus) < 8 {
		t.Fatalf("corpus floor: %d fixtures, want >= 8 — a shrunken corpus makes this row vacuous", len(corpus))
	}
	graded := 0
	for name, src := range corpus {
		ents := band7185Run(t, src, sectPath7190)
		comps := 0
		for _, e := range ents {
			if e.Kind == "SCOPE.Component" && e.EndLine != 0 {
				comps++
			}
		}
		if comps < 2 {
			// A fixture with fewer than two spanned components cannot exhibit
			// absorption at all; say so rather than counting it as graded.
			t.Errorf("%s: only %d spanned components — cannot grade absorption", name, comps)
			continue
		}
		graded += comps
		for _, msg := range sect7190Absorbed(ents) {
			t.Errorf("%s: %s", name, msg)
		}
	}
	if graded < 19 {
		t.Errorf("graded only %d spanned components, want >= 19", graded)
	}
	t.Logf("absorption row graded %d spanned components across %d fixtures", graded, len(corpus))
}

// --- #7195's whole-file invariant, applied to the new fixtures --------------

func TestTypeSection7190_FixturesRespectEOFBound(t *testing.T) {
	for name, src := range sect7190Corpus() {
		ents := band7185Run(t, src, sectPath7190)
		if len(ents) == 0 {
			t.Errorf("%s: no entities — grades nothing", name)
			continue
		}
		for _, msg := range eofSpansPastEOF(src, ents) {
			t.Errorf("%s: %s", name, msg)
		}
	}
}
