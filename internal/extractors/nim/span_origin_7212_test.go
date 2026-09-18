package nim_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #7212 — THE TWO ENDS OF A SPAN WERE MEASURED FROM TWO DIFFERENT OFFSETS.
//
// Both nim.go call sites assembled the span as
//
//	startLine := strings.Count(src[:m[0]], "\n") + 1   // the match START
//	body      := extractIndentBody(src, m[1], ...)     // the match END
//	endLine   := startLine + strings.Count(body, "\n")
//
// `body` begins at m[1], so `Count(body, "\n")` is a line count measured FROM
// THE MATCH END. Adding it to a line number measured at the match START is only
// arithmetically sound while the match is confined to one line. Whenever the
// declaration itself spans lines — a `= object` clause broken after the `=` or
// after `ref`, a parameter list broken across lines — every line the match
// occupies beyond its first appears in NEITHER term, and EndLine comes out
// short by exactly that many lines. NOT an off-by-one: the deficit is the
// clause's own line count, so a patch that adds 1 is wrong at three lines.
//
// The fix gives both ends ONE origin: EndLine is anchored at the line of m[1],
// the same offset `body` is cut from. For a single-line match m[0] and m[1] are
// on the same line, so every pre-existing span is unchanged by construction —
// that is why this file adds rows rather than editing any.
//
// MEASURED ON THE BRANCH POINT 6535f8b9d, not assumed (`-run TestProbe7212`,
// since deleted):
//
//	fixture                       emitted   correct   deficit
//	span7212TwoLineClause  Alpha    2-3       2-4        1
//	span7212ThreeLineClause Alpha   2-3       2-5        2
//	span7212NestedNoNL     Alpha    3-4       3-5        1
//	span7212ProcWrapParams add      1-2       1-3        1
//	span7212ProcThreeLine  inner    2-3       2-5        2
//
// THE PROC SITE HAS THE SAME DEFECT. Issue #7212's mechanism section names the
// type site only and asks whether the proc site shares it. It does, and for a
// shape more common than the type one: procRE's parameter group is
// `(\([^)]*\))?` and `[^)]` matches a newline, so EVERY proc whose parameter
// list is wrapped — idiomatic in Nim — reports a truncated span. It is fixed
// here, and graded by its own rows and its own mutants, never by a mutant that
// edits both sites at once.
//
// AXES VARIED, crossed rather than listed:
//
//	clause line count (1 / 2 / 3)
//	  x base indent of the declaration (0 / 2 / 4)  <- non-zero is mandatory:
//	    a column-0 fixture cannot tell a correct base from the hard-coded 0
//	    that hid #7190
//	  x site (type member / proc)
//	  x what breaks the line (after `=` / after `ref` / inside a parameter list)
//	  x position of the declaration (followed by a sibling / last in the file)
//	  x trailing newline (present / absent)
//
// HELD CONSTANT, deliberately: spaces-only indentation (Nim forbids tabs —
// manual, "Lexical Analysis -> Indentation": "Indentation consists only of
// spaces; tabulators are not allowed"); no pragmas on type members (a member
// carrying one produces no entity at all — #7213, out of scope here); ASCII
// identifiers; one file per fixture.
//
// DERIVED-NOT-EXECUTED. No Nim toolchain exists on this machine, so every
// expected span below is derived from the Nim grammar plus the indentation rule
// and NEVER from what the extractor emits. The two line-break positions are
// grammatical per Nim's doc/grammar.txt:
//
//	typeDef = identWithPragma genericParamList? '=' optInd typeDefAux ...
//	primary = typeKeyw optInd typeDesc / ...          (typeKeyw includes 'ref')
//
// `optInd` is exactly "an optional indentation", i.e. the RHS may continue on
// an indented following line — which is what `Alpha* =\n    object` and
// `... =\n    ref\n      object` do. FALSIFIER: a Nim program in which a type
// definition whose RHS begins on the line after its `=`, or whose `object`
// follows `ref` on an indented next line, is rejected.
// ---------------------------------------------------------------------------

const spanPath7212 = "src/domain/spans.nim"

// span7212TwoLineClause — the issue's shape. Base 2, clause broken after `=`,
// Alpha followed by a sibling so a WIDENED span is observable as absorption.
//
// Alpha declares on line 2 at column 2; lines 3 and 4 are strictly deeper than
// column 2 so they are its body; line 5 is at column 2, a sibling. Span 2-4.
const span7212TwoLineClause = "type\n" + // 1
	"  Alpha* =\n" + // 2  <- declaration, column 2
	"    object\n" + // 3  <- clause continues
	"      a*: int\n" + // 4
	"  Beta* = object\n" + // 5  <- sibling at column 2
	"    b*: int\n" // 6

// span7212ThreeLineClause — the SAME defect at a clause of THREE lines, so a
// fix that adds a constant 1 is refuted. Base 2. The break is after `ref` as
// well as after `=`, varying what breaks the line. Alpha spans 2-5.
const span7212ThreeLineClause = "type\n" + // 1
	"  Alpha* =\n" + // 2
	"    ref\n" + // 3
	"      object\n" + // 4
	"        a*: int\n" + // 5
	"  Beta* = object\n" + // 6
	"    b*: int\n" // 7

// span7212NestedNoNL — base 4 (a section nested in a proc), two-line clause, on
// the LAST declaration of a file with NO trailing newline. This is the cell
// where #7212 meets #7195/#7196: the phantom-element drop in extractIndentBody
// does not apply (nothing follows the final '\n' because there is none) and the
// span must still land exactly on the last line, 5.
const span7212NestedNoNL = "proc wrap*() =\n" + // 1
	"  type\n" + // 2
	"    Alpha* =\n" + // 3  <- declaration, column 4
	"      object\n" + // 4
	"        a*: int" // 5, file ends here — no trailing newline

// span7212NestedTrailingNL — the trailing-newline twin of the row above. #7195
// drops the empty element after the final '\n'; if that drop and this fix
// interact, the two fixtures disagree. They must both say 3-5.
const span7212NestedTrailingNL = span7212NestedNoNL + "\n"

// span7212ProcWrapParams — the PROC site, base 0, parameter list wrapped over
// two lines. `add` declares on line 1 and its signature runs to line 2; line 3
// is its body; line 4 is a sibling at column 0. Span 1-3.
const span7212ProcWrapParams = "proc add*(a: int,\n" + // 1
	"          b: int): int =\n" + // 2
	"  result = a + b\n" + // 3
	"proc other*() =\n" + // 4
	"  discard\n" // 5

// span7212ProcThreeLine — the PROC site at base 2 (a nested proc) with a
// signature spanning THREE lines. inner declares on line 2, its signature runs
// to line 4, line 5 is its body, and line 6 at column 2 is the enclosing proc's
// own statement. Span 2-5; outer spans 1-6.
const span7212ProcThreeLine = "proc outer*() =\n" + // 1
	"  proc inner*(a: int,\n" + // 2  <- declaration, column 2
	"              b: int,\n" + // 3
	"              c: int): int =\n" + // 4
	"    result = a\n" + // 5
	"  discard\n" // 6

func span7212Corpus() map[string]string {
	return map[string]string{
		"7212/twoLineClause":    span7212TwoLineClause,
		"7212/threeLineClause":  span7212ThreeLineClause,
		"7212/nestedNoNL":       span7212NestedNoNL,
		"7212/nestedTrailingNL": span7212NestedTrailingNL,
		"7212/procWrapParams":   span7212ProcWrapParams,
		"7212/procThreeLine":    span7212ProcThreeLine,
	}
}

func span7212Want(t *testing.T, ents []types.EntityRecord, name, kind string, start, end int) {
	t.Helper()
	e := band7185Get(t, ents, name, kind)
	if e.StartLine != start || e.EndLine != end {
		t.Errorf("%s/%s span = %d-%d, want %d-%d", kind, name, e.StartLine, e.EndLine, start, end)
	}
}

// --- the TYPE site ----------------------------------------------------------

func TestSpanOrigin7212_TypeTwoLineClause(t *testing.T) {
	ents := band7185Run(t, span7212TwoLineClause, spanPath7212)
	span7212Want(t, ents, "Alpha", "SCOPE.Component", 2, 4)
	span7212Want(t, ents, "Beta", "SCOPE.Component", 5, 6)
}

// A constant +1 would make this row read 2-4 and stay wrong; only an origin
// that moves with the clause gets 2-5.
func TestSpanOrigin7212_TypeThreeLineClause(t *testing.T) {
	ents := band7185Run(t, span7212ThreeLineClause, spanPath7212)
	span7212Want(t, ents, "Alpha", "SCOPE.Component", 2, 5)
	span7212Want(t, ents, "Beta", "SCOPE.Component", 6, 7)
	// The split `ref` is still classified from the kind group, so the fix does
	// not quietly change what the entity IS while changing where it is.
	if got := band7185Get(t, ents, "Alpha", "SCOPE.Component").Subtype; got != "ref object" {
		t.Errorf("Alpha subtype = %q, want %q", got, "ref object")
	}
}

// #7195/#7196 interaction, both directions of the trailing-newline axis.
func TestSpanOrigin7212_TypeLastDeclarationAtEOFBothNewlineStates(t *testing.T) {
	for name, src := range map[string]string{
		"noTrailingNewline":   span7212NestedNoNL,
		"withTrailingNewline": span7212NestedTrailingNL,
	} {
		t.Run(name, func(t *testing.T) {
			ents := band7185Run(t, src, spanPath7212)
			span7212Want(t, ents, "Alpha", "SCOPE.Component", 3, 5)
			// The enclosing proc's base is 0 and its own match is single-line,
			// so it is untouched by this fix and still spans the section.
			span7212Want(t, ents, "wrap", "SCOPE.Operation", 1, 5)
		})
	}
}

// --- the PROC site, graded on its own ---------------------------------------

func TestSpanOrigin7212_ProcWrappedParameterList(t *testing.T) {
	ents := band7185Run(t, span7212ProcWrapParams, spanPath7212)
	span7212Want(t, ents, "add", "SCOPE.Operation", 1, 3)
	span7212Want(t, ents, "other", "SCOPE.Operation", 4, 5)
}

func TestSpanOrigin7212_ProcThreeLineSignatureAtNonZeroBase(t *testing.T) {
	ents := band7185Run(t, span7212ProcThreeLine, spanPath7212)
	span7212Want(t, ents, "inner", "SCOPE.Operation", 2, 5)
	span7212Want(t, ents, "outer", "SCOPE.Operation", 1, 6)
}

// The truncated body was not only a wrong number: CALLS edges are collected
// from the SAME `body` string, so the proc site's body must still reach the
// statement. Graded on an emitted RELATIONSHIP, not on the span, so a fix that
// moved EndLine without keeping the body intact is visible.
func TestSpanOrigin7212_ProcBodyStillYieldsCalls(t *testing.T) {
	src := "proc helper*(x: int): int =\n" + // 1
		"  result = x\n" + // 2
		"proc caller*(a: int,\n" + // 3
		"             b: int): int =\n" + // 4
		"  result = helper(a)\n" // 5
	ents := band7185Run(t, src, spanPath7212)
	span7212Want(t, ents, "caller", "SCOPE.Operation", 3, 5)
	e := band7185Get(t, ents, "caller", "SCOPE.Operation")
	found := false
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && strings.Contains(r.ToID, "helper") {
			found = true
		}
	}
	if !found {
		t.Errorf("caller: no CALLS edge to helper; got %+v", e.Relationships)
	}
}

// --- the permissive direction: a span that is too WIDE ----------------------

// span7212DeclIndent is the column the declaration on `line` (1-based) starts
// at, read from the FIXTURE SOURCE. Nesting is what makes an enclosing span
// legitimate, and nesting in Nim is indentation — so the legitimacy signal is
// taken from the source's own indentation rule, never from the spans being
// judged. Returns -1 when the line does not exist.
func span7212DeclIndent(src string, line int) int {
	lines := strings.Split(src, "\n")
	if line < 1 || line > len(lines) {
		return -1
	}
	return len(lines[line-1]) - len(strings.TrimLeft(lines[line-1], " \t"))
}

// span7212Overrun reports, for one fixture, every span that reaches past EOF and
// every span that reaches a LATER declaration's own line WITHOUT enclosing it.
// Both are the widening failure mode: an origin fix applied twice, or applied
// and then padded, produces spans that are too long.
//
// #7221 REVIEW, N1 — THE PREDICATE USED TO BE BLIND TO TOTAL ABSORPTION. It
// read `i.StartLine < j.StartLine && j.StartLine <= i.EndLine && i.EndLine <
// j.EndLine`, i.e. it fired only on a PARTIAL straddle. A span that swallowed a
// later sibling ENTIRELY satisfied none of it, and past-EOF could not catch it
// either (a span ending at the last line is not past EOF). Measured on real
// output for twoLineClause: Alpha planted 2-5 gave 1 violation, Alpha planted
// 2-6 — which fully swallows Beta 5-6 — gave 0. That is EXACTLY the #7190
// shape: restoring `extractIndentBody(src, m[1], 0)` at the type site makes the
// first member absorb the whole section, and this row would have stayed green.
//
// The `i.EndLine < j.EndLine` clause was there to spare legitimate ENCLOSURE
// (`wrap` 1-5 properly containing `Alpha` 3-5, `outer` 1-6 containing `inner`
// 2-5). But geometry alone cannot separate enclosure from absorption — both are
// containment — and neither can Kind: `outer`/`inner` are both SCOPE.Operation
// and legitimately nested. The signal that DOES separate them is the one Nim
// itself uses: a genuine parent's declaration is at a STRICTLY SHALLOWER column
// than its child's, while two siblings share a column. So containment is
// permitted only when `indent(i) < indent(j)`, read from the source; a span
// reaching a declaration at its own or a shallower column is a violation
// however far past it runs. Partial and total absorption now both fire.
func span7212Overrun(src string, ents []types.EntityRecord) []string {
	ceiling := strings.Count(strings.TrimSuffix(src, "\n"), "\n") + 1
	var out []string
	spanned := func(e types.EntityRecord) bool { return e.EndLine != 0 && e.StartLine != 0 }
	for i := range ents {
		if !spanned(ents[i]) {
			continue
		}
		if ents[i].EndLine > ceiling {
			out = append(out, fmt.Sprintf("%s/%s span %d-%d reaches past EOF (%d lines)",
				ents[i].Kind, ents[i].Name, ents[i].StartLine, ents[i].EndLine, ceiling))
		}
		for j := range ents {
			if i == j || !spanned(ents[j]) {
				continue
			}
			if ents[i].StartLine >= ents[j].StartLine || ents[j].StartLine > ents[i].EndLine {
				continue // i does not reach j's declaration line at all
			}
			ii, ji := span7212DeclIndent(src, ents[i].StartLine), span7212DeclIndent(src, ents[j].StartLine)
			if ii >= 0 && ji >= 0 && ii < ji {
				continue // genuine nesting: j is declared deeper than i
			}
			out = append(out, fmt.Sprintf("%s/%s span %d-%d absorbs %s/%s (%d-%d) declared at column %d",
				ents[i].Kind, ents[i].Name, ents[i].StartLine, ents[i].EndLine,
				ents[j].Kind, ents[j].Name, ents[j].StartLine, ents[j].EndLine, ji))
		}
	}
	return out
}

// POSITIVE CONTROL. Built from REAL extractor output rather than from hand-made
// records, then perturbed by one line each way, so the control exercises the
// detector on the same record shapes the forbidden row below feeds it.
func TestSpanOrigin7212_OverrunDetectorFires(t *testing.T) {
	// (a) past-EOF arm, on a real 6-line fixture.
	ents := band7185Run(t, span7212TwoLineClause, spanPath7212)
	if got := span7212Overrun(span7212TwoLineClause, ents); len(got) != 0 {
		t.Fatalf("baseline must be clean before planting: %v", got)
	}
	planted := append([]types.EntityRecord(nil), ents...)
	for i := range planted {
		if planted[i].Name == "Beta" {
			planted[i].EndLine = 7 // one line past the 6-line file
		}
	}
	if got := span7212Overrun(span7212TwoLineClause, planted); len(got) != 1 {
		t.Errorf("planted past-EOF span: got %d violations %v, want 1", len(got), got)
	}

	// (b) straddle arm: Alpha widened by one line reaches Beta's own line 5.
	planted2 := append([]types.EntityRecord(nil), ents...)
	for i := range planted2 {
		if planted2[i].Name == "Alpha" {
			planted2[i].EndLine = 5
		}
	}
	if got := span7212Overrun(span7212TwoLineClause, planted2); len(got) != 1 {
		t.Errorf("planted straddling span: got %d violations %v, want 1", len(got), got)
	}

	// (b2) FULL-SWALLOW arm — the #7221/N1 blind spot, now graded. Alpha pushed
	// to 2-6 contains Beta (5-6) ENTIRELY and ends exactly at EOF, so neither
	// the past-EOF arm nor the old partial-straddle predicate could see it.
	// Both declarations sit at column 2, so it is absorption, not nesting.
	planted3 := append([]types.EntityRecord(nil), ents...)
	for i := range planted3 {
		if planted3[i].Name == "Alpha" {
			planted3[i].EndLine = 6
		}
	}
	if got := span7212Overrun(span7212TwoLineClause, planted3); len(got) != 1 {
		t.Errorf("planted FULL-SWALLOW span: got %d violations %v, want 1", len(got), got)
	}

	// (c) the boundary must NOT fire, on inputs DISTINCT from the baseline
	// above (#7221/N2: the old arm (c) re-ran the baseline call verbatim and
	// added no coverage). procWrapParams holds two column-0 procs whose spans
	// are adjacent — 1-3 then 4-5 — with the second ending exactly at EOF: the
	// two boundary cases, at a fixture the baseline never touched.
	adj := band7185Run(t, span7212ProcWrapParams, spanPath7212)
	span7212Want(t, adj, "add", "SCOPE.Operation", 1, 3)
	span7212Want(t, adj, "other", "SCOPE.Operation", 4, 5)
	if got := span7212Overrun(span7212ProcWrapParams, adj); len(got) != 0 {
		t.Errorf("adjacent + ends-at-EOF spans: got %v, want none", got)
	}

	// (d) legitimate ENCLOSURE must not fire: `wrap` (1-5) contains `Alpha`
	// (3-5) and ends with it. A detector that reported this would make the
	// forbidden row fail for the wrong reason and mask real widening. Since
	// #7221/N1 this is spared because `wrap` is declared at column 0 and
	// `Alpha` at column 4 — genuine nesting — and NOT because one span contains
	// the other, which is the condition that used to silence absorption too.
	// procThreeLine is the same-Kind case: `outer` (column 0) encloses `inner`
	// (column 2), and two SCOPE.Operations nesting must be spared as well.
	nested := band7185Run(t, span7212NestedNoNL, spanPath7212)
	if got := span7212Overrun(span7212NestedNoNL, nested); len(got) != 0 {
		t.Errorf("legitimate enclosure (cross-kind): got %v, want none", got)
	}
	procs := band7185Run(t, span7212ProcThreeLine, spanPath7212)
	if got := span7212Overrun(span7212ProcThreeLine, procs); len(got) != 0 {
		t.Errorf("legitimate enclosure (same kind, outer/inner): got %v, want none", got)
	}
}

// FORBIDDEN ROW: across every #7212 fixture, no span may reach past EOF and no
// span may reach a later declaration that is NOT nested inside it — partially
// or entirely. Graded through the SAME helper the control above plants
// violations in, including the full-swallow arm added for #7221/N1.
func TestSpanOrigin7212_NoSpanOverruns(t *testing.T) {
	corpus := span7212Corpus()
	if len(corpus) < 6 {
		t.Fatalf("corpus floor: %d fixtures, want >= 6 — a shrunken corpus makes this row vacuous", len(corpus))
	}
	graded := 0
	for name, src := range corpus {
		ents := band7185Run(t, src, spanPath7212)
		n := 0
		for _, e := range ents {
			if e.StartLine != 0 && e.EndLine != 0 {
				n++
			}
		}
		if n < 2 {
			t.Errorf("%s: only %d spanned entities — cannot grade overrun", name, n)
			continue
		}
		graded += n
		for _, msg := range span7212Overrun(src, ents) {
			t.Errorf("%s: %s", name, msg)
		}
	}
	if graded < 12 {
		t.Errorf("graded only %d spanned entities, want >= 12", graded)
	}
	t.Logf("overrun row graded %d spanned entities across %d fixtures", graded, len(corpus))
}

// EVERY span must contain the whole text its own declaration occupies. This is
// the invariant the defect violated, stated over the corpus independently of
// any particular expected number: the declaration's first line is StartLine, so
// EndLine must be at least the line on which the declaration's own text ends.
// The declaration's extent is located from the fixture source, not from the
// extractor, by finding the `=` that opens the clause and the token that closes
// it.
func TestSpanOrigin7212_SpanCoversItsOwnDeclaration(t *testing.T) {
	type row struct {
		fixture, name, kind string
		declEndsOnLine      int
	}
	// Derived by reading each fixture: the line carrying the token that ends
	// the declaration's head (`object` / the `=` that opens the body).
	rows := []row{
		{"7212/twoLineClause", "Alpha", "SCOPE.Component", 3},
		{"7212/threeLineClause", "Alpha", "SCOPE.Component", 4},
		{"7212/nestedNoNL", "Alpha", "SCOPE.Component", 4},
		{"7212/nestedTrailingNL", "Alpha", "SCOPE.Component", 4},
		{"7212/procWrapParams", "add", "SCOPE.Operation", 2},
		{"7212/procThreeLine", "inner", "SCOPE.Operation", 4},
	}
	corpus := span7212Corpus()
	for _, r := range rows {
		src, ok := corpus[r.fixture]
		if !ok {
			t.Fatalf("fixture %s missing from corpus", r.fixture)
		}
		e := band7185Get(t, band7185Run(t, src, spanPath7212), r.name, r.kind)
		if e.EndLine < r.declEndsOnLine {
			t.Errorf("%s: %s span %d-%d ends BEFORE its own declaration, which runs to line %d",
				r.fixture, r.name, e.StartLine, e.EndLine, r.declEndsOnLine)
		}
	}
}

// Single-line declarations are unchanged by construction: for them m[0] and
// m[1] are on the same line, so the old and new origins coincide. Asserted
// rather than argued.
func TestSpanOrigin7212_SingleLineDeclarationsUnchanged(t *testing.T) {
	src := "type\n" + // 1
		"  Alpha* = object\n" + // 2
		"    a*: int\n" + // 3
		"  Beta* = ref object of Alpha\n" + // 4
		"    b*: int\n" + // 5
		"\n" + // 6
		"proc run*(a: Alpha) =\n" + // 7
		"  discard\n" // 8
	ents := band7185Run(t, src, spanPath7212)
	span7212Want(t, ents, "Alpha", "SCOPE.Component", 2, 3)
	span7212Want(t, ents, "Beta", "SCOPE.Component", 4, 6)
	span7212Want(t, ents, "run", "SCOPE.Operation", 7, 8)
}
