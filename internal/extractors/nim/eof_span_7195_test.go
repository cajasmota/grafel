package nim_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #7195 — the LAST declaration in a nim file reported EndLine = lineCount + 1.
//
// MECHANISM. extractIndentBody does `strings.Split(src[afterPos:], "\n")`. When
// the source ends in a newline that split yields a FINAL EMPTY ELEMENT which is
// not a line of the file at all — it is the empty remainder after the last
// newline. That element reaches the `if strings.TrimSpace(line) == ""` arm and
// is appended as a blank body line. The two callers then compute
//
//	endLine := startLine + strings.Count(body, "\n")
//
// and count the phantom. A declaration that terminates on a following sibling
// `break`s before the scan reaches EOF and never sees it, which is why every
// declaration EXCEPT the last was correct — and why every pre-existing fixture,
// each of which has something after its last declaration, was structurally
// blind to this. The rows below therefore END THE FILE ON THE DECLARATION.
//
// AXES VARIED here: (a) which call site — nim.go:150 `proc` vs nim.go:208
// `type`; (b) trailing newline present vs absent, because the phantom element
// exists ONLY in the former and a fix keyed on the wrong one passes half the
// rows; (c) declaration position — sole declaration vs last-of-two; (d) what
// sits at EOF — the body's last code line, a blank line, a whitespace-only
// line, or nothing at all; (e) whether the body's blank lines precede a SIBLING
// (mid-split) or run to EOF (end-of-split); (f) `baseIndentLen` — 0 at both the
// top-level proc and the type pass's hard-coded 0, and 2 for a NESTED proc
// (TestEOF7195NestedProcAtEOF). Axis (f) was ungraded in the first round and a
// mutant keyed on `baseIndentLen == 0` survived every other row.
//
// AXES CROSSED, not merely listed: (b) x (d). Listing two axes separately is
// not the same as crossing them, and the uncrossed cell is where this suite
// leaked once already — every whitespace-at-EOF row below originally carried a
// trailing newline, so the cell "final line is whitespace-only AND the file
// does NOT end in a newline" was unforbidden, and a guard keyed on
// `strings.TrimSpace(lines[n-1]) == ""` instead of `lines[n-1] == ""` passed
// the whole package while silently dropping that real line from the span. See
// TestEOF7195ForbiddenWhitespaceLineAtEOFNoTrailingNewline.
//
// AXES HELD CONSTANT: file path, language, indent width (two spaces), body
// content, and the declaration keyword within each pair — so a failure is
// attributable to the axis being varied and not to the surrounding source.
//
// DIRECTION, and WHICH ROW GRADES WHICH. The permissive direction is "trim
// more than the phantom", and it has TWO distinct routes that are graded by
// DIFFERENT rows. Getting this pairing wrong is not academic: the first version
// of this header named one row for the whole direction, and that row is
// measurably not the grader for the route that actually threatens the fix.
//
//   - Trim from the END OF THE SPLIT (before the collection loop). Shortens a
//     body whose blank or whitespace-only lines run to EOF. GRADED BY
//     TestEOF7195ForbiddenTrailingBlankLinesAtEOFKept, its whitespace-only
//     sibling, the type-site twin, and — for the crossed no-trailing-newline
//     cell — TestEOF7195ForbiddenWhitespaceLineAtEOFNoTrailingNewline.
//     NOT graded by TestEOF7195ForbiddenEarlierDeclUnchanged: a blank line
//     before a sibling sits in the MIDDLE of the split and no trim working from
//     the end can reach it. Two such mutants were scored and left that row
//     GREEN.
//   - Trim the COLLECTED BODY, or trim at the sibling `break`. Shortens an
//     earlier declaration whose body ends in blank lines before a sibling.
//     GRADED BY TestEOF7195ForbiddenEarlierDeclUnchanged and its type twin.
//
// Each of those rows CAN FAIL ALONE — an over-trim shrinks an EndLine, which no
// at-EOF row and not the whole-file invariant can observe, since shrinking
// never exceeds EOF.
//
// DERIVED-NOT-EXECUTED: no Nim toolchain exists on this machine, so the
// fixtures' legality is read off the Nim manual, not compiled.
// ---------------------------------------------------------------------------

const eofPath7195 = "src/domain/spans.nim"

// eofBlankBeforeSib7195: alpha's body genuinely ends in two BLANK LINES (3, 4)
// that are real lines of the file, then a sibling proc at line 5, then EOF on
// line 6. Shared by the forbidden row and its last-declaration counterpart so
// the two grade the SAME input.
const eofBlankBeforeSib7195 = "proc alpha*() =\n" + // line 1
	"  echo 1\n" + // line 2
	"\n" + // line 3 — blank, genuinely in the file
	"\n" + // line 4 — blank, genuinely in the file
	"proc beta*() =\n" + // line 5
	"  echo 2\n" // line 6, file ends here

// lineCount7195 is the number of LINES in src under editor semantics: a source
// ending in a newline has no extra empty line after it. This is the ceiling
// every emitted EndLine must respect.
func lineCount7195(src string) int {
	if src == "" {
		return 0
	}
	n := strings.Count(src, "\n")
	if !strings.HasSuffix(src, "\n") {
		n++
	}
	return n
}

func TestLineCount7195Helper(t *testing.T) {
	// The invariant's ceiling is itself a claim; pin it so a mis-derived
	// ceiling cannot silently make the invariant vacuous or over-strict.
	cases := []struct {
		src  string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
		{"a\n\n", 2},
	}
	for _, c := range cases {
		if got := lineCount7195(c.src); got != c.want {
			t.Errorf("lineCount7195(%q) = %d, want %d", c.src, got, c.want)
		}
	}
}

// --- the two-line reproducer, both trailing-newline forms, TYPE site ---------

// nim.go:208 (type pass). File ends ON the declaration's body.
func TestEOF7195TypeAtEOFTrailingNewline(t *testing.T) {
	src := "type Alpha* = object\n" + // line 1
		"  a*: int\n" // line 2, file ends here
	if got := lineCount7195(src); got != 2 {
		t.Fatalf("fixture premise: lineCount = %d, want 2", got)
	}
	ents := band7185Run(t, src, eofPath7195)
	e := band7185Get(t, ents, "Alpha", "SCOPE.Component")
	if e.StartLine != 1 || e.EndLine != 2 {
		t.Errorf("Alpha span = %d-%d, want 1-2 (EndLine must not pass EOF at line 2)", e.StartLine, e.EndLine)
	}
}

// Same shape with NO trailing newline: the phantom element does not exist here,
// so this row is the positive control that the fix is keyed to the newline and
// does not shorten a body that ends flush at EOF.
func TestEOF7195TypeAtEOFNoTrailingNewline(t *testing.T) {
	src := "type Alpha* = object\n" + // line 1
		"  a*: int" // line 2, no trailing newline
	if got := lineCount7195(src); got != 2 {
		t.Fatalf("fixture premise: lineCount = %d, want 2", got)
	}
	ents := band7185Run(t, src, eofPath7195)
	e := band7185Get(t, ents, "Alpha", "SCOPE.Component")
	if e.StartLine != 1 || e.EndLine != 2 {
		t.Errorf("Alpha span = %d-%d, want 1-2", e.StartLine, e.EndLine)
	}
}

// --- the same pair at the OTHER call site, nim.go:150 (proc pass) -----------
//
// Scored separately: a DEAD verdict at the type site says nothing about this
// one. They are twins and only one being pinned is the recurring defect here.

func TestEOF7195ProcAtEOFTrailingNewline(t *testing.T) {
	src := "proc alpha*() =\n" + // line 1
		"  echo 1\n" // line 2, file ends here
	if got := lineCount7195(src); got != 2 {
		t.Fatalf("fixture premise: lineCount = %d, want 2", got)
	}
	ents := band7185Run(t, src, eofPath7195)
	e := band7185Get(t, ents, "alpha", "SCOPE.Operation")
	if e.StartLine != 1 || e.EndLine != 2 {
		t.Errorf("alpha span = %d-%d, want 1-2 (EndLine must not pass EOF at line 2)", e.StartLine, e.EndLine)
	}
}

func TestEOF7195ProcAtEOFNoTrailingNewline(t *testing.T) {
	src := "proc alpha*() =\n" + // line 1
		"  echo 1" // line 2, no trailing newline
	if got := lineCount7195(src); got != 2 {
		t.Fatalf("fixture premise: lineCount = %d, want 2", got)
	}
	ents := band7185Run(t, src, eofPath7195)
	e := band7185Get(t, ents, "alpha", "SCOPE.Operation")
	if e.StartLine != 1 || e.EndLine != 2 {
		t.Errorf("alpha span = %d-%d, want 1-2", e.StartLine, e.EndLine)
	}
}

// --- last-of-two: the earlier sibling was already right, the last was not ----

func TestEOF7195LastOfTwoTypes(t *testing.T) {
	src := "type Alpha* = object\n" + // line 1
		"  a*: int\n" + // line 2
		"type Beta* = object\n" + // line 3
		"  b*: int\n" // line 4, file ends here
	if got := lineCount7195(src); got != 4 {
		t.Fatalf("fixture premise: lineCount = %d, want 4", got)
	}
	ents := band7185Run(t, src, eofPath7195)
	if e := band7185Get(t, ents, "Beta", "SCOPE.Component"); e.StartLine != 3 || e.EndLine != 4 {
		t.Errorf("Beta span = %d-%d, want 3-4", e.StartLine, e.EndLine)
	}
}

func TestEOF7195LastOfTwoProcs(t *testing.T) {
	src := "proc alpha*() =\n" + // line 1
		"  echo 1\n" + // line 2
		"proc beta*() =\n" + // line 3
		"  echo 2\n" // line 4, file ends here
	if got := lineCount7195(src); got != 4 {
		t.Fatalf("fixture premise: lineCount = %d, want 4", got)
	}
	ents := band7185Run(t, src, eofPath7195)
	if e := band7185Get(t, ents, "alpha", "SCOPE.Operation"); e.StartLine != 1 || e.EndLine != 2 {
		t.Errorf("alpha span = %d-%d, want 1-2", e.StartLine, e.EndLine)
	}
	if e := band7185Get(t, ents, "beta", "SCOPE.Operation"); e.StartLine != 3 || e.EndLine != 4 {
		t.Errorf("beta span = %d-%d, want 3-4", e.StartLine, e.EndLine)
	}
}

// --- FORBIDDEN ROW: the permissive direction --------------------------------
//
// A body that GENUINELY ends in blank lines, before a sibling. The phantom
// element is not involved — alpha's scan breaks on `proc beta` — so alpha's
// EndLine must be UNCHANGED at 4, covering lines 3 and 4 which really are in
// the file. A fix that trims trailing blanks off the body shortens alpha to 2
// and fails HERE ALONE: every at-EOF row above still passes, and the whole-file
// invariant still passes because shrinking a span never pushes it past EOF.
func TestEOF7195ForbiddenEarlierDeclUnchanged(t *testing.T) {
	src := eofBlankBeforeSib7195
	if got := lineCount7195(src); got != 6 {
		t.Fatalf("fixture premise: lineCount = %d, want 6", got)
	}
	ents := band7185Run(t, src, eofPath7195)
	if e := band7185Get(t, ents, "alpha", "SCOPE.Operation"); e.StartLine != 1 || e.EndLine != 4 {
		t.Errorf("alpha span = %d-%d, want 1-4 UNCHANGED: the blank lines 3-4 are real lines of the file, "+
			"and trimming them is the permissive over-fix this row forbids", e.StartLine, e.EndLine)
	}
}

// The same fixture's LAST declaration, as its own row. Kept separate on purpose:
// the forbidden row above must carry NOTHING but the unchanged-earlier
// assertion, or an over-trim's failure there would be indistinguishable from a
// must-have sibling failing on the same input.
func TestEOF7195BlankBeforeSiblingLastDecl(t *testing.T) {
	src := eofBlankBeforeSib7195
	ents := band7185Run(t, src, eofPath7195)
	if e := band7185Get(t, ents, "beta", "SCOPE.Operation"); e.StartLine != 5 || e.EndLine != 6 {
		t.Errorf("beta span = %d-%d, want 5-6", e.StartLine, e.EndLine)
	}
}

// Type-site twin of the forbidden row, scored separately.
func TestEOF7195ForbiddenEarlierTypeUnchanged(t *testing.T) {
	src := "type Alpha* = object\n" + // line 1
		"  a*: int\n" + // line 2
		"\n" + // line 3 — blank, genuinely in the file
		"proc beta*() =\n" + // line 4
		"  echo 2\n" // line 5, file ends here
	if got := lineCount7195(src); got != 5 {
		t.Fatalf("fixture premise: lineCount = %d, want 5", got)
	}
	ents := band7185Run(t, src, eofPath7195)
	if e := band7185Get(t, ents, "Alpha", "SCOPE.Component"); e.StartLine != 1 || e.EndLine != 3 {
		t.Errorf("Alpha span = %d-%d, want 1-3 UNCHANGED (line 3 is a real blank line)", e.StartLine, e.EndLine)
	}
}

// --- FORBIDDEN PAIR: trailing blank lines AT EOF are real lines -------------
//
// The first permissive mutants I wrote (trim EVERY trailing empty split
// element; trim trailing WHITESPACE-ONLY elements) came back ALIVE against the
// rows above, and they were right to. The over-trim I had guarded against
// operates on the COLLECTED BODY, where a blank line before a sibling sits in
// the MIDDLE of the split and is untouchable from the end. Trimming from the
// end of the split instead shortens a body whose blank lines are at EOF — a
// shape no row above has.
//
// The pin is a consistency argument, not a preference: the very same blank
// lines ARE inside the span when a sibling follows them
// (TestEOF7195ForbiddenEarlierDeclUnchanged — which grades the COLLECTED-BODY
// route, not this one), so EndLine must not depend on whether anything follows.
// Real lines of the file stay in the span; only the phantom empty element after
// the final newline goes.

func TestEOF7195ForbiddenTrailingBlankLinesAtEOFKept(t *testing.T) {
	src := "proc alpha*() =\n" + // line 1
		"  echo 1\n" + // line 2
		"\n" + // line 3 — blank, a real line
		"\n" // line 4 — blank, a real line; the phantom is AFTER this
	if got := lineCount7195(src); got != 4 {
		t.Fatalf("fixture premise: lineCount = %d, want 4", got)
	}
	ents := band7185Run(t, src, eofPath7195)
	if e := band7185Get(t, ents, "alpha", "SCOPE.Operation"); e.StartLine != 1 || e.EndLine != 4 {
		t.Errorf("alpha span = %d-%d, want 1-4: lines 3 and 4 are real blank lines and only the "+
			"phantom element after the final newline may be dropped", e.StartLine, e.EndLine)
	}
}

func TestEOF7195ForbiddenTrailingWhitespaceLineAtEOFKept(t *testing.T) {
	src := "proc alpha*() =\n" + // line 1
		"  echo 1\n" + // line 2
		"   \n" // line 3 — whitespace-only, still a real line
	if got := lineCount7195(src); got != 3 {
		t.Fatalf("fixture premise: lineCount = %d, want 3", got)
	}
	ents := band7185Run(t, src, eofPath7195)
	if e := band7185Get(t, ents, "alpha", "SCOPE.Operation"); e.StartLine != 1 || e.EndLine != 3 {
		t.Errorf("alpha span = %d-%d, want 1-3: line 3 is whitespace-only but real; trimming it is "+
			"the permissive over-fix this row forbids", e.StartLine, e.EndLine)
	}
}

// Type-site twin of the pair above, scored separately.
func TestEOF7195ForbiddenTrailingBlankLinesAtEOFKeptTypeSite(t *testing.T) {
	src := "type Alpha* = object\n" + // line 1
		"  a*: int\n" + // line 2
		"\n" + // line 3 — blank, a real line
		"\n" // line 4 — blank, a real line
	if got := lineCount7195(src); got != 4 {
		t.Fatalf("fixture premise: lineCount = %d, want 4", got)
	}
	ents := band7185Run(t, src, eofPath7195)
	if e := band7185Get(t, ents, "Alpha", "SCOPE.Component"); e.StartLine != 1 || e.EndLine != 4 {
		t.Errorf("Alpha span = %d-%d, want 1-4", e.StartLine, e.EndLine)
	}
}

// --- FORBIDDEN, CROSSED CELL: whitespace-only final line AND no trailing \n --
//
// The uncrossed pair. Every whitespace-at-EOF row above carries a trailing
// newline, so a guard keyed on `strings.TrimSpace(lines[n-1]) == ""` rather
// than `lines[n-1] == ""` passed the ENTIRE package — exit 0, zero failures —
// while dropping a real line from the span on every source whose final line is
// whitespace-only and which does NOT end in a newline. There is no phantom
// element to drop in that shape at all: the last split element IS the file's
// last line.
//
// This is the cell, not a variation on it: `"...\n   "` (no trailing newline)
// versus `"...\n   \n"` (trailing newline) differ ONLY in axis (b), and the
// second is already covered by
// TestEOF7195ForbiddenTrailingWhitespaceLineAtEOFKept.
func TestEOF7195ForbiddenWhitespaceLineAtEOFNoTrailingNewline(t *testing.T) {
	src := "proc alpha*() =\n" + // line 1
		"  echo 1\n" + // line 2
		"   " // line 3 — whitespace-only AND no trailing newline
	if got := lineCount7195(src); got != 3 {
		t.Fatalf("fixture premise: lineCount = %d, want 3", got)
	}
	if strings.HasSuffix(src, "\n") {
		t.Fatal("fixture premise: this row exists to cross whitespace-at-EOF with NO trailing newline")
	}
	ents := band7185Run(t, src, eofPath7195)
	if e := band7185Get(t, ents, "alpha", "SCOPE.Operation"); e.StartLine != 1 || e.EndLine != 3 {
		t.Errorf("alpha span = %d-%d, want 1-3: with no trailing newline the last split element IS "+
			"line 3, a real line. Keying the phantom drop on TrimSpace rather than an exact empty "+
			"string deletes it — the permissive over-fix this row forbids", e.StartLine, e.EndLine)
	}
}

// Type-site twin of the crossed cell, scored separately.
func TestEOF7195ForbiddenWhitespaceLineAtEOFNoTrailingNewlineTypeSite(t *testing.T) {
	src := "type Alpha* = object\n" + // line 1
		"  a*: int\n" + // line 2
		"   " // line 3 — whitespace-only AND no trailing newline
	if got := lineCount7195(src); got != 3 {
		t.Fatalf("fixture premise: lineCount = %d, want 3", got)
	}
	if strings.HasSuffix(src, "\n") {
		t.Fatal("fixture premise: this row exists to cross whitespace-at-EOF with NO trailing newline")
	}
	ents := band7185Run(t, src, eofPath7195)
	if e := band7185Get(t, ents, "Alpha", "SCOPE.Component"); e.StartLine != 1 || e.EndLine != 3 {
		t.Errorf("Alpha span = %d-%d, want 1-3", e.StartLine, e.EndLine)
	}
}

// The same crossing with a BLANK (not whitespace-only) final line and no
// trailing newline is not a reachable source: a file whose last line is empty
// and which does not end in a newline is the empty-suffix file itself. The
// nearest reachable neighbour is the trailing-newline form already covered by
// TestEOF7195ForbiddenTrailingBlankLinesAtEOFKept, so the cell is closed by
// construction rather than by a row — stated here so it is not read as an
// omission.

// --- NESTED declaration at EOF: baseIndentLen > 0 ---------------------------
//
// Every other row here has baseIndentLen == 0 (top-level proc, and the type
// pass hard-codes 0). A mutant that keyed the phantom drop on
// `baseIndentLen == 0` therefore survived them all. A nested proc is the shape
// that varies that axis: the phantom element is an artefact of the SPLIT and
// has nothing to do with the declaration's own column.
func TestEOF7195NestedProcAtEOF(t *testing.T) {
	src := "proc outer*() =\n" + //     line 1, base indent 0
		"  proc inner*() =\n" + //  line 2, base indent 2
		"    echo 1\n" //           line 3, file ends here
	if got := lineCount7195(src); got != 3 {
		t.Fatalf("fixture premise: lineCount = %d, want 3", got)
	}
	ents := band7185Run(t, src, eofPath7195)
	e := band7185Get(t, ents, "inner", "SCOPE.Operation")
	if e.StartLine != 2 || e.EndLine != 3 {
		t.Errorf("inner span = %d-%d, want 2-3 — the phantom drop must not be keyed on the "+
			"declaration's own indent (baseIndentLen was 2 here, 0 everywhere else)", e.StartLine, e.EndLine)
	}
}

// --- the whole-file invariant, added once -----------------------------------
//
// No emitted EndLine may exceed the source's line count, over EVERY entity of
// EVERY fixture below. This is what makes the defect a CLASS rather than a
// shape: it holds regardless of which declaration is last, which call site
// produced it, or whether the file ends in a newline.

// eofCorpus7195 gathers the package's fixture shapes. The `band7185*` consts
// are the #7185 indent-band fixtures reused verbatim; the rest are the shapes
// the other test files in this package exercise, plus the #7195 reproducers.
func eofCorpus7195() map[string]string {
	c := map[string]string{
		// #7185 fixtures, reused from indent_band_7185_test.go.
		"band7185/proc":          procBand7185,
		"band7185/procAbove":     procAboveBand7185,
		"band7185/type":          typeBand7185,
		"band7185/typeAbove":     typeAboveBand7195Alias,
		"band7185/widen":         widenBand7185,
		"band7185/sameLine":      sameLineBody7185,
		"7195/typeAtEOF":         "type Alpha* = object\n  a*: int\n",
		"7195/typeAtEOFNoNL":     "type Alpha* = object\n  a*: int",
		"7195/procAtEOF":         "proc alpha*() =\n  echo 1\n",
		"7195/procAtEOFNoNL":     "proc alpha*() =\n  echo 1",
		"7195/twoTypes":          "type Alpha* = object\n  a*: int\ntype Beta* = object\n  b*: int\n",
		"7195/twoProcs":          "proc alpha*() =\n  echo 1\nproc beta*() =\n  echo 2\n",
		"7195/trailingBlank":     "type Alpha* = object\n  a*: int\ntype Beta* = object\n  b*: int\n\n",
		"7195/blankBeforeSib":    eofBlankBeforeSib7195,
		"7195/blankAtEOF":        "proc alpha*() =\n  echo 1\n\n\n",
		"7195/wsLineAtEOF":       "proc alpha*() =\n  echo 1\n   \n",
		"7195/wsLineAtEOFNoNL":   "proc alpha*() =\n  echo 1\n   ",
		"7195/typeWsAtEOFNoNL":   "type Alpha* = object\n  a*: int\n   ",
		"7195/nestedWsAtEOFNoNL": "proc outer*() =\n  proc inner*() =\n    echo 1\n     ",
		"7195/typeBlankAtEOF":    "type Alpha* = object\n  a*: int\n\n\n",
		"7195/nestedProcAtEOF":   "proc outer*() =\n  proc inner*() =\n    echo 1\n",
		"7195/nestedBlankEOF":    "proc outer*() =\n  proc inner*() =\n    echo 1\n\n",
		"7195/declOnlyNoBody":    "type Alpha* = object",
		"7195/twoBlankMid":       "type Alpha* = object\n  a*: int\n\n\ntype Beta* = object\n  b*: int\n",
		// Shapes drawn from the package's other fixtures.
		"pkg/imports":     "import strutils, sequtils\n\nproc greet(name: string): string =\n  result = \"hi \" & name\n",
		"pkg/typeSection": "type\n  Animal* = object\n    name*: string\n  Dog* = ref object of Animal\n    breed*: string\n",
		"pkg/enum":        "type Color* = enum\n  red, green, blue\n",
		"pkg/tuple":       "type Point* = tuple\n  x, y: int\n",
		"pkg/distinct":    "type Meters* = distinct int\n",
		"pkg/sameLine":    "proc worker(x: int) = discard x\n",
		"pkg/method":      "type Animal = object\n  name: string\n\nmethod speak(a: Animal): string =\n  result = a.name\n",
		"pkg/recursive":   "proc fib(n: int): int =\n  if n < 2: return n\n  return fib(n - 1) + fib(n - 2)\n",
		"pkg/noTrailNL":   "import strutils\n\nproc last(): int =\n  result = 1",
	}
	return c
}

// typeAboveBand7195Alias keeps the corpus readable while making the reuse of
// indent_band_7185_test.go's const explicit.
const typeAboveBand7195Alias = typeAboveBand7185

func TestEOF7195NoEmittedSpanPastEOF(t *testing.T) {
	corpus := eofCorpus7195()
	if len(corpus) < 30 {
		t.Fatalf("corpus floor: %d fixtures, want >= 30 — a shrunken corpus makes this invariant vacuous", len(corpus))
	}
	graded := 0
	for name, src := range corpus {
		ents := band7185Run(t, src, eofPath7195)
		if len(ents) == 0 {
			t.Errorf("%s: fixture emitted NO entities — it grades nothing", name)
			continue
		}
		// The comparison itself runs through eofSpansPastEOF, so this invariant
		// and TestEOF7195InvariantHelperFires grade the SAME code path. It was
		// re-implemented inline once, which left the positive control guarding a
		// function nothing executed.
		for _, msg := range eofSpansPastEOF(src, ents) {
			t.Errorf("%s: %s", name, msg)
		}
		spanned := 0
		for _, e := range ents {
			if e.EndLine == 0 && e.StartLine == 0 {
				continue // not a spanned entity (e.g. an import placeholder)
			}
			spanned++
			graded++
			if e.StartLine > e.EndLine {
				t.Errorf("%s: %s/%s span %d-%d is inverted", name, e.Kind, e.Name, e.StartLine, e.EndLine)
			}
		}
		if spanned == 0 {
			t.Errorf("%s: no spanned entity — fixture cannot grade the invariant", name)
		}
	}
	// A floor on the number of entities actually compared, so a harness that
	// silently stops emitting spans reads RED rather than green.
	if graded < 40 {
		t.Errorf("graded only %d spanned entities, want >= 40", graded)
	}
	t.Logf("invariant graded %d spanned entities across %d fixtures", graded, len(corpus))
}

// eofSpansPastEOF returns a message per entity whose EndLine exceeds src's line
// count. Exposed as a value rather than a t.Errorf so its own detection can be
// graded (TestEOF7195InvariantHelperFires) instead of assumed — and called by
// TestEOF7195NoEmittedSpanPastEOF, so the control guards a LIVE path.
func eofSpansPastEOF(src string, ents []types.EntityRecord) []string {
	ceiling := lineCount7195(src) // not `max`: that shadows the Go 1.21 builtin
	var out []string
	for _, e := range ents {
		if e.EndLine > ceiling {
			out = append(out, fmt.Sprintf("%s/%s span %d-%d exceeds EOF (%d lines)", e.Kind, e.Name, e.StartLine, e.EndLine, ceiling))
		}
	}
	return out
}

func TestEOF7195InvariantHelperFires(t *testing.T) {
	// Positive control: a record deliberately past EOF must be detected, and a
	// record exactly AT EOF must not be, so the helper is neither a no-op nor
	// off by one for future adopters.
	src := "proc alpha*() =\n  echo 1\n" // 2 lines
	if got := eofSpansPastEOF(src, []types.EntityRecord{{Name: "x", Kind: "K", StartLine: 1, EndLine: 3}}); len(got) != 1 {
		t.Errorf("EndLine 3 in a 2-line file: got %d violations, want 1", len(got))
	}
	if got := eofSpansPastEOF(src, []types.EntityRecord{{Name: "x", Kind: "K", StartLine: 1, EndLine: 2}}); len(got) != 0 {
		t.Errorf("EndLine 2 in a 2-line file: got %v, want none", got)
	}
}
