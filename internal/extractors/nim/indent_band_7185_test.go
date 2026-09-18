package nim_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/nim"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #7185 — extractIndentBody's DEAD BAND in the nim extractor.
//
// `minBodyIndent := baseIndentLen + 2` paired with a `indent <= baseIndentLen`
// terminator leaves a line at exactly `baseIndentLen+1` satisfying NEITHER
// branch: it is not appended to the body and it does not stop the scan. The
// loop silently drops it and keeps going, so the emitted body has a HOLE —
// the base+1 line is gone while deeper lines *after* it are still collected.
//
// THE BOUNDARY IS SOURCED TO NIM, NOT TO F#. PR #7184 chose +1 for fsharp from
// the F# 4.1 offside rule; that argument does not transfer. Nim's own rule,
// from the Nim manual, Lexical Analysis -> Indentation:
//
//	"Nim's standard grammar describes an indentation sensitive language. This
//	 means that all the control structures are recognized by indentation.
//	 Indentation consists only of spaces; tabulators are not allowed."
//
// and, for the grammar pseudo-terminals used throughout that grammar:
//
//	IND{>}  "denotes an indentation that consists of MORE SPACES than the entry
//	         at the top of the stack"
//	IND{=}  "an indentation that has the SAME number of spaces"
//
// An indented statement list is introduced by IND{>} — strictly more spaces
// than the enclosing entry — and a sibling at the same level is IND{=}, i.e.
// EXACTLY the enclosing column. The manual states no minimum step: "more
// spaces" is satisfied by one. So base+1 is body, and only base-or-less can be
// a sibling. Threshold = baseIndentLen + 1.
//
// DERIVED-NOT-EXECUTED: there is no Nim toolchain on this machine (`nim`,
// `nimble` absent; `javac` is the only compiler present), so this is read off
// the manual rather than compiled.
//
// FALSIFIER: a Nim program in which a statement indented exactly one space
// deeper than its enclosing declaration is rejected by the compiler, or parses
// as a SIBLING of that declaration rather than as its body. Nothing in the
// manual's IND{>} / IND{=} pair admits such a program, and the manual names no
// minimum indentation step — but it was not compiled here.
//
// Each test below asserts the EMITTED ARTEFACT (EndLine, CALLS edges), never an
// internal counter, and each call site is graded separately: nim.go:149 (the
// proc/func/method pass) and nim.go:207 (the type pass). A verdict at one says
// nothing about the other.
// ---------------------------------------------------------------------------

func band7185Run(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("nim")
	if !ok {
		t.Fatal("nim extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "nim",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func band7185Get(t *testing.T, ents []types.EntityRecord, name, kind string) types.EntityRecord {
	t.Helper()
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return ents[i]
		}
	}
	var names []string
	for i := range ents {
		names = append(names, ents[i].Kind+"/"+ents[i].Name)
	}
	t.Fatalf("entity %s/%s not emitted; got %v", kind, name, names)
	return types.EntityRecord{}
}

func band7185Calls(e types.EntityRecord) []string {
	var out []string
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" {
			out = append(out, r.ToID)
		}
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// CALL SITE 1 — nim.go:149, the proc/func/method/template/macro/iterator pass.
// Artefacts: EndLine and the CALLS edge set.
// ---------------------------------------------------------------------------

// procBand7185 puts `stepOne()` at exactly base+1 (one space), between a blank
// same-line remainder and a base+2 line, with a base-0 sibling terminating.
const procBand7185 = "proc alpha() =\n" + // line 1, base indent 0
	" stepOne()\n" + //                     line 2, indent 1  <- THE BAND
	"  stepTwo()\n" + //                    line 3, indent 2
	"\n" + //                               line 4
	"proc beta() =\n" + //                  line 5, indent 0  <- sibling
	"  other()" //                        line 6

func TestIndentBand7185_Nim_ProcSite_BandLineIsBody(t *testing.T) {
	ents := band7185Run(t, procBand7185, "band.nim")
	alpha := band7185Get(t, ents, "alpha", "SCOPE.Operation")

	// Pre-fix (+2): the band line vanished, body = "\n  stepTwo()\n" (2 NLs)
	// so EndLine was 3 and CALLS was [stepTwo] only.
	if alpha.EndLine != 4 {
		t.Errorf("alpha EndLine = %d, want 4 (the base+1 line is body, so the span reaches the blank line before the sibling)", alpha.EndLine)
	}
	got := band7185Calls(alpha)
	want := []string{"stepOne", "stepTwo"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("alpha CALLS = %v, want %v (stepOne sits at base+1 and was silently dropped by the dead band)", got, want)
	}
}

func TestIndentBand7185_Nim_ProcSite_SiblingNotAbsorbed(t *testing.T) {
	ents := band7185Run(t, procBand7185, "band.nim")
	alpha := band7185Get(t, ents, "alpha", "SCOPE.Operation")
	beta := band7185Get(t, ents, "beta", "SCOPE.Operation")

	// CONTROL AT BASE (indent 0): `proc beta` terminates alpha's body. This is
	// the arm an off-by-one in the OTHER direction (+0, or `<=` -> `<`) breaks.
	for _, c := range band7185Calls(alpha) {
		if c == "other" {
			t.Errorf("alpha CALLS contains %q — a base-indent sibling was absorbed into the previous body", c)
		}
	}
	if beta.StartLine != 5 {
		t.Errorf("beta StartLine = %d, want 5", beta.StartLine)
	}
	if beta.EndLine != 6 {
		t.Errorf("beta EndLine = %d, want 6", beta.EndLine)
	}
}

// procAboveBand7185 is the base+2 CONTROL: the only interesting line sits at
// indent 2, which is body under every candidate threshold (+0, +1, +2).
const procAboveBand7185 = "proc alpha() =\n" + // line 1
	"  stepTwo()\n" + //                        line 2, indent 2
	"\n" + //                                   line 3
	"proc beta() =\n" + //                      line 4
	"  other()" //                            line 5

func TestIndentBand7185_Nim_ProcSite_AboveBandControl(t *testing.T) {
	ents := band7185Run(t, procAboveBand7185, "above.nim")
	alpha := band7185Get(t, ents, "alpha", "SCOPE.Operation")
	if alpha.EndLine != 3 {
		t.Errorf("alpha EndLine = %d, want 3", alpha.EndLine)
	}
	if got, want := strings.Join(band7185Calls(alpha), ","), "stepTwo"; got != want {
		t.Errorf("alpha CALLS = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// CALL SITE 2 — nim.go:207, the type pass. baseIndentLen is hard-coded 0 there,
// so the band is at exactly one space. Artefact: EndLine.
// A verdict at the proc site says nothing here: this site emits a different
// Kind (SCOPE.Component), computes no CALLS, and passes a different base.
//
// SCOPE LIMIT OF THIS ROW: it grades the boundary at base 0, which is the right
// base only because the fixture puts `type Alpha = object` at column 0.
// Idiomatic Nim writes a `type` SECTION with the declarations indented, and
// nim.go:207 hard-codes 0 regardless, so there every type absorbs the ones after
// it. That is #7190 — it reproduces identically at +2 and at +1, so it is
// neither caused nor cured here, and this row is structurally unable to see it.
// ---------------------------------------------------------------------------

const typeBand7185 = "type Alpha = object\n" + // line 1, base 0 (hard-coded)
	" fieldA: int\n" + //                       line 2, indent 1  <- THE BAND
	"  fieldB: string\n" + //                   line 3, indent 2
	"\n" + //                                   line 4
	"type Beta = object\n" + //                 line 5, indent 0  <- sibling
	"  fieldC: int" //                        line 6

func TestIndentBand7185_Nim_TypeSite_BandLineIsBody(t *testing.T) {
	ents := band7185Run(t, typeBand7185, "band_type.nim")
	alpha := band7185Get(t, ents, "Alpha", "SCOPE.Component")

	// Pre-fix (+2): body = "\n  fieldB: string\n" (2 NLs) -> EndLine 3.
	if alpha.EndLine != 4 {
		t.Errorf("Alpha EndLine = %d, want 4 (the base+1 field line is body)", alpha.EndLine)
	}
	if alpha.StartLine != 1 {
		t.Errorf("Alpha StartLine = %d, want 1", alpha.StartLine)
	}
}

func TestIndentBand7185_Nim_TypeSite_SiblingTerminates(t *testing.T) {
	ents := band7185Run(t, typeBand7185, "band_type.nim")
	beta := band7185Get(t, ents, "Beta", "SCOPE.Component")
	alpha := band7185Get(t, ents, "Alpha", "SCOPE.Component")

	// CONTROL AT BASE: `type Beta` at column 0 must end Alpha's span. Under an
	// off-by-one the other way (+0) Alpha would swallow the rest of the file.
	if alpha.EndLine >= beta.StartLine {
		t.Errorf("Alpha EndLine = %d must be < Beta StartLine = %d — a base-indent sibling was absorbed", alpha.EndLine, beta.StartLine)
	}
	if beta.EndLine != 6 {
		t.Errorf("Beta EndLine = %d, want 6", beta.EndLine)
	}
}

// typeAboveBand7185 is the base+2 CONTROL for the type site.
const typeAboveBand7185 = "type Alpha = object\n" + // line 1
	"  fieldB: string\n" + //                       line 2, indent 2
	"\n" + //                                       line 3
	"type Beta = object\n" + //                     line 4
	"  fieldC: int" //                            line 5

func TestIndentBand7185_Nim_TypeSite_AboveBandControl(t *testing.T) {
	ents := band7185Run(t, typeAboveBand7185, "above_type.nim")
	alpha := band7185Get(t, ents, "Alpha", "SCOPE.Component")
	if alpha.EndLine != 3 {
		t.Errorf("Alpha EndLine = %d, want 3", alpha.EndLine)
	}
}

// ---------------------------------------------------------------------------
// THE WIDENING DIRECTION — the half a recall-style assertion is blind to.
//
// Moving the threshold from +2 to +1 makes every body LARGER, so every body
// consumer sees more text. #7152 recorded that the F# scanners read RAW `src`;
// MEASURED HERE, nim's `collectCalls` does NOT — it runs
// `stripStringsAndComments(body)` first (nim.go:435). These forbidden rows pin
// that: if the scrub is ever dropped, or the band line is admitted to an
// unscrubbed path, a comment or string literal at base+1 mints a CALLS edge.
// ---------------------------------------------------------------------------

const widenBand7185 = "proc alpha() =\n" + // line 1
	" # notACall()\n" + //                  line 2, indent 1, comment at the band
	" discard \"alsoNotACall()\"\n" + //     line 3, indent 1, string at the band
	"  realCall()\n" + //                   line 4, indent 2
	"\n" + //                               line 5
	"proc beta() =\n" + //                  line 6
	"  other()" //                        line 7

func TestIndentBand7185_Nim_WideningMintsNothingFromCommentsOrStrings(t *testing.T) {
	ents := band7185Run(t, widenBand7185, "widen.nim")
	alpha := band7185Get(t, ents, "alpha", "SCOPE.Operation")

	// MUST-HAVE: the widening really did reach these lines.
	if alpha.EndLine != 5 {
		t.Fatalf("alpha EndLine = %d, want 5 — the base+1 lines must be inside the body for this forbidden check to mean anything", alpha.EndLine)
	}
	// FORBIDDEN: nothing minted from the comment or the string literal.
	for _, c := range band7185Calls(alpha) {
		if c == "notACall" || c == "alsoNotACall" || c == "other" {
			t.Errorf("alpha CALLS contains %q — the widened body minted an edge from a comment, a string literal, or a sibling", c)
		}
	}
	if got, want := strings.Join(band7185Calls(alpha), ","), "realCall"; got != want {
		t.Errorf("alpha CALLS = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// THE FIRST-LINE SPECIAL CASE — the one branch that BYPASSES the threshold this
// PR moves, and which the nim suite did not observe at all.
//
// Both copies of extractIndentBody open with
//
//	if i == 0 && strings.TrimSpace(line) != "" { append; continue }
//
// whose nim comment names the exact construct it exists to serve: `Same-line
// body: "proc foo() = result"`. Disabling it (`if false && i == 0 && …`) killed
// exactly ONE test across both packages — a rescript synthetic fixture — and
// NOTHING in nim. Prose asserting what no test observes, in the copy whose own
// comment names the construct.
//
// It interacts DIRECTLY with the boundary this PR moves, which is why the row
// belongs here rather than in a generic gap issue. The remainder after the
// regex match begins with the space before the body, so its indent is 1. At
// base 0 that clears minBodyIndent under +1 anyway and the branch is invisible.
// At base 2 — an INDENTED declaration — 1 fails `>= 3`, satisfies `<= 2`, and
// the loop terminates on the very first line with an EMPTY body. The first-line
// branch is the only thing that carries a same-line body past the threshold.
//
// THE INDENTED SAME-LINE FORM IS LEGAL NIM, derived from the manual's own
// grammar (Nim manual, "The Nim grammar"):
//
//	routine = optInd identVis pattern? genericParamList?
//	  paramListColon pragma? ('=' COMMENT? stmt)? indAndComment
//
// The body is `('=' COMMENT? stmt)?` — a `stmt` directly after the `=`, with NO
// IND{>} interposed, which IS the same-line form; and the production is led by
// `optInd`, so a routine may itself be indented, i.e. nested. So `proc inner():
// int = compute()` at column 2 inside another proc is grammatical.
// DERIVED-NOT-EXECUTED: no Nim toolchain here. FALSIFIER: a Nim program where a
// nested routine with its body on the declaration line is rejected.
// ---------------------------------------------------------------------------

const sameLineBody7185 = "proc outer() =\n" + //          line 1, base 0
	"  proc inner(): int = compute()\n" + //             line 2, base 2, same-line body
	"\n" + //                                            line 3
	"  discard inner()" //                               line 4, indent 2 -> terminates

func TestIndentBand7185_Nim_IndentedSameLineBody(t *testing.T) {
	ents := band7185Run(t, sameLineBody7185, "sameline.nim")
	inner := band7185Get(t, ents, "inner", "SCOPE.Operation")

	// With the first-line branch DISABLED, the remainder " compute()" has indent
	// 1, which fails `1 >= 3` and satisfies `1 <= 2`: the loop breaks at once,
	// the body is empty, EndLine collapses to 2 and CALLS is empty.
	if got := strings.Join(band7185Calls(inner), ","); got != "compute" {
		t.Errorf("inner CALLS = %q, want \"compute\" — the same-line body after `=` is the whole body of an indented routine; without the i==0 branch it never reaches the threshold", got)
	}
	if inner.EndLine != 3 {
		t.Errorf("inner EndLine = %d, want 3 (the same-line body plus the trailing blank line; without the i==0 branch the span collapses to 2)", inner.EndLine)
	}
	if inner.StartLine != 2 {
		t.Errorf("inner StartLine = %d, want 2", inner.StartLine)
	}
}
