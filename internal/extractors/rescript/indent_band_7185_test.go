package rescript_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/rescript"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #7185 — extractIndentBody's DEAD BAND in the rescript extractor.
//
// `minBodyIndent := baseIndentLen + 2` paired with an `indent <= baseIndentLen`
// terminator leaves a line at exactly `baseIndentLen+1` satisfying NEITHER
// branch. It is not appended and it does not stop the scan, so the loop drops
// it and keeps going.
//
// THE RESCRIPT DECISION IS NOT THE F# DECISION, AND IT IS NOT THE NIM ONE.
// PR #7184 sourced fsharp's +1 to the F# 4.1 offside rule; #7185's nim arm
// sources +1 to the Nim manual's IND{>} pseudo-terminal. NEITHER argument is
// available here, because ReScript HAS NO INDENTATION RULE TO CITE:
//
//	ReScript Language Manual v11, "Let Binding" -> Block Scope:
//	  "Bindings can be scoped through `{}`."
//	  "The value of the last line of a scope is implicitly returned."
//
// Scope boundaries are BRACES. The manual assigns no meaning to a line's
// column, and the section on block scope says nothing about indentation at
// all. So in ReScript's own terms:
//
//	(a) ANY column > 0 is a legal indentation for a body line, and
//	(b) ANY column is a legal indentation for a top-level `let` or `type`.
//
// Consequently NO threshold — +0, +1, +2, or a terminate-at-base+1 rule — can
// be CORRECT for ReScript. The whole indent heuristic is an approximation of
// brace matching, which this extractor does not do. That is the honest answer
// to "what should a base+1 line do": the language does not say.
//
// THE DEAD BAND IS A DEFECT ANYWAY, INDEPENDENT OF THAT ANSWER, because it does
// not make the loop pick a different threshold — it makes the loop NON-TOTAL.
// A base+1 line is neither body nor terminator, so the scan steps over it and
// keeps collecting the lines BELOW it. The emitted body then has a HOLE: the
// base+1 line is missing while base+2 lines that follow it are present. No
// threshold makes "skip this line and keep going" right; see
// TestIndentBand7185_ReScript_NoHoleInTheBody below, which measures exactly
// that hole on a ReScript program that is legal under the brace rule quoted
// above.
//
// OF THE TWO WAYS TO MAKE THE LOOP TOTAL — append at base+1, or terminate at
// base+1 — APPEND IS CHOSEN, on this ReScript-specific ground: because braces
// (not columns) delimit a body, a body line may legally sit at ANY column
// greater than the declaration's, including base+1, and terminating there
// would DROP true CALLS/RENDERS edges from such a body (measured in
// TestIndentBand7185_ReScript_LetSite_BandLineIsBody, where `stepOne` is lost
// under both `+2` and a terminate-at-base+1 rule). The opposite case — a
// top-level declaration hand-indented by exactly one column, which append then
// absorbs into the previous body — is NOT a regression: under `+2` that
// declaration's own deeper body lines are ALREADY absorbed today, with its
// header line silently missing.
//
// DERIVED-NOT-EXECUTED: there is no ReScript toolchain on this machine
// (`rescript`, `bsc`, `bsb`, `res` all absent; `javac` is the only compiler
// present), so the brace rule is read off the manual rather than compiled.
//
// FALSIFIER, and it is a different KIND of falsifier from the F# and Nim ones:
// it is NOT "a ReScript program where base+1 is a sibling" — those exist and
// are legal, precisely because indentation is free. It is a CORPUS claim: real
// ReScript in which top-level declarations hand-indented by exactly one column
// are MORE common than block bodies indented by exactly one column. If that
// held, terminate-at-base+1 would be the better approximation. No ReScript
// corpus was measured here; the claim rests on `rescript format` being the
// single canonical style (top level at column 0, bodies at column 2), which
// makes the band empty in conforming source either way.
//
// Each test asserts the EMITTED ARTEFACT — EndLine, CALLS, RENDERS — never an
// internal counter, and the two call sites are graded separately:
// extractor.go:182 (the `let` pass) and extractor.go:220 (the `type` pass).
// ---------------------------------------------------------------------------

func band7185Run(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("rescript")
	if !ok {
		t.Fatal("rescript extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "rescript",
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

func band7185Edges(e types.EntityRecord, kind string) []string {
	var out []string
	for _, r := range e.Relationships {
		if r.Kind == kind {
			out = append(out, r.ToID)
		}
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// CALL SITE 1 — extractor.go:182, the `let` pass.
// Artefacts: EndLine, CALLS, RENDERS.
// ---------------------------------------------------------------------------

const letBand7185 = "let alpha = () => {\n" + // line 1, base indent 0
	" stepOne()\n" + //                        line 2, indent 1  <- THE BAND
	"  stepTwo()\n" + //                       line 3, indent 2
	"}\n" + //                                 line 4, indent 0  <- terminator
	"\n" +
	"let beta = () => {\n" + //                line 6
	"  other()\n" +
	"}"

func TestIndentBand7185_ReScript_LetSite_BandLineIsBody(t *testing.T) {
	ents := band7185Run(t, letBand7185, "band.res")
	alpha := band7185Get(t, ents, "alpha", "SCOPE.Operation")

	// Pre-fix (+2): body = " () => {\n  stepTwo()" (1 NL) -> EndLine 2, and the
	// CALLS set was [stepTwo] only. A terminate-at-base+1 rule would give the
	// same loss, which is why append is the choice — see the header block.
	if alpha.EndLine != 3 {
		t.Errorf("alpha EndLine = %d, want 3 (the base+1 line is body under the brace rule)", alpha.EndLine)
	}
	got := band7185Edges(alpha, "CALLS")
	want := []string{"stepOne", "stepTwo"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("alpha CALLS = %v, want %v (stepOne sits at base+1 and was silently dropped by the dead band)", got, want)
	}
}

func TestIndentBand7185_ReScript_LetSite_SiblingNotAbsorbed(t *testing.T) {
	ents := band7185Run(t, letBand7185, "band.res")
	alpha := band7185Get(t, ents, "alpha", "SCOPE.Operation")
	beta := band7185Get(t, ents, "beta", "SCOPE.Operation")

	// CONTROL AT BASE (indent 0): the closing `}` at column 0 ends alpha's body.
	// This is the arm an off-by-one in the OTHER direction (+0, or `<=` -> `<`)
	// breaks — under +0 nothing ever terminates and alpha swallows beta.
	for _, c := range band7185Edges(alpha, "CALLS") {
		if c == "other" {
			t.Errorf("alpha CALLS contains %q — a base-indent sibling was absorbed into the previous body", c)
		}
	}
	if beta.StartLine != 6 {
		t.Errorf("beta StartLine = %d, want 6", beta.StartLine)
	}
	if beta.EndLine != 7 {
		t.Errorf("beta EndLine = %d, want 7", beta.EndLine)
	}
}

// letAboveBand7185 is the base+2 CONTROL: body at indent 2, which every
// candidate threshold (+0, +1, +2) treats as body.
const letAboveBand7185 = "let alpha = () => {\n" + // line 1
	"  stepTwo()\n" + //                            line 2, indent 2
	"}\n" + //                                      line 3
	"\n" +
	"let beta = () => {\n" +
	"  other()\n" +
	"}"

func TestIndentBand7185_ReScript_LetSite_AboveBandControl(t *testing.T) {
	ents := band7185Run(t, letAboveBand7185, "above.res")
	alpha := band7185Get(t, ents, "alpha", "SCOPE.Operation")
	if alpha.EndLine != 2 {
		t.Errorf("alpha EndLine = %d, want 2", alpha.EndLine)
	}
	if got, want := strings.Join(band7185Edges(alpha, "CALLS"), ","), "stepTwo"; got != want {
		t.Errorf("alpha CALLS = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// CALL SITE 2 — extractor.go:220, the `type` pass. Different base (the
// declaration's own indent, group 1 of typeRE), different Kind
// (SCOPE.Component), no CALLS computed. A verdict at the `let` site says
// nothing here.
// ---------------------------------------------------------------------------

const typeBand7185 = "type alpha = {\n" + // line 1, base indent 0
	" a: int,\n" + //                     line 2, indent 1  <- THE BAND
	"  b: string,\n" + //                 line 3, indent 2
	"}\n" + //                            line 4, indent 0  <- terminator
	"\n" +
	"type beta = {\n" + //                line 6
	"  c: int,\n" +
	"}"

func TestIndentBand7185_ReScript_TypeSite_BandLineIsBody(t *testing.T) {
	ents := band7185Run(t, typeBand7185, "band_type.res")
	alpha := band7185Get(t, ents, "alpha", "SCOPE.Component")

	// Pre-fix (+2): body = " {\n  b: string," (1 NL) -> EndLine 2.
	if alpha.StartLine != 1 {
		t.Errorf("alpha StartLine = %d, want 1", alpha.StartLine)
	}
	if alpha.EndLine != 3 {
		t.Errorf("alpha EndLine = %d, want 3 (the base+1 field line is body)", alpha.EndLine)
	}
}

func TestIndentBand7185_ReScript_TypeSite_SiblingTerminates(t *testing.T) {
	ents := band7185Run(t, typeBand7185, "band_type.res")
	alpha := band7185Get(t, ents, "alpha", "SCOPE.Component")
	beta := band7185Get(t, ents, "beta", "SCOPE.Component")

	// CONTROL AT BASE.
	if alpha.EndLine >= beta.StartLine {
		t.Errorf("alpha EndLine = %d must be < beta StartLine = %d — a base-indent sibling was absorbed", alpha.EndLine, beta.StartLine)
	}
	if beta.EndLine != 7 {
		t.Errorf("beta EndLine = %d, want 7", beta.EndLine)
	}
}

// typeAboveBand7185 is the base+2 CONTROL for the type site.
const typeAboveBand7185 = "type alpha = {\n" + // line 1
	"  b: string,\n" + //                      line 2, indent 2
	"}\n" + //                                 line 3
	"\n" +
	"type beta = {\n" +
	"  c: int,\n" +
	"}"

func TestIndentBand7185_ReScript_TypeSite_AboveBandControl(t *testing.T) {
	ents := band7185Run(t, typeAboveBand7185, "above_type.res")
	alpha := band7185Get(t, ents, "alpha", "SCOPE.Component")
	if alpha.EndLine != 2 {
		t.Errorf("alpha EndLine = %d, want 2", alpha.EndLine)
	}
}

// ---------------------------------------------------------------------------
// THE HOLE — the ReScript-specific argument, measured rather than asserted.
//
// This is the case the header block rests on. `gamma` is a declaration
// hand-indented by exactly one column; ReScript permits it, since braces and
// not columns delimit scope. Under the old `+2` the scan SKIPPED gamma's header
// line and then CARRIED ON collecting gamma's own body at indent 3, so alpha's
// body contained `inner()` while the declaration that owns it was invisible —
// a non-contiguous body. That is the state no threshold justifies. After the
// fix alpha's body is contiguous through gamma's block.
//
// This test also records the acknowledged COST of choosing append: alpha's span
// now covers gamma. Under `+2` it covered gamma's body anyway, so this is a
// change of EndLine, not a new mis-attribution.
// ---------------------------------------------------------------------------

const holeBand7185 = "let alpha = 1\n" + // line 1, base indent 0
	" let gamma = () => {\n" + //         line 2, indent 1  <- THE BAND
	"   inner()\n" + //                   line 3, indent 3
	" }" //                               line 4, indent 1  <- also the band

func TestIndentBand7185_ReScript_NoHoleInTheBody(t *testing.T) {
	ents := band7185Run(t, holeBand7185, "hole.res")
	alpha := band7185Get(t, ents, "alpha", "SCOPE.Operation")
	gamma := band7185Get(t, ents, "gamma", "SCOPE.Operation")

	// Pre-fix: alpha.EndLine == 2 — one newline, from the skipped-then-resumed
	// scan that kept `   inner()` but dropped both base+1 lines around it.
	if alpha.EndLine != 4 {
		t.Errorf("alpha EndLine = %d, want 4 — the body must be CONTIGUOUS; a base+1 line may not be skipped while lines below it are kept", alpha.EndLine)
	}
	// Pre-fix AND post-fix alike, `inner()` is inside alpha's body: the append
	// does not create this mis-attribution, it only stops hiding it.
	if got := band7185Edges(alpha, "CALLS"); strings.Join(got, ",") != "inner" {
		t.Errorf("alpha CALLS = %v, want [inner] — unchanged by the fix; recorded so the cost of appending is pinned, not assumed", got)
	}
	// No entity is lost either way.
	if gamma.StartLine != 2 {
		t.Errorf("gamma StartLine = %d, want 2", gamma.StartLine)
	}
}

// ---------------------------------------------------------------------------
// THE WIDENING DIRECTION.
//
// +2 -> +1 makes every body LARGER, so every body consumer sees more text.
// #7152 recorded that the F# scanners read RAW `src`. MEASURED HERE, rescript's
// do NOT: both `collectCalls` (extractor.go:390) and `collectRenders`
// (extractor.go:443) run `stripStringsAndComments(body)` first. These forbidden
// rows pin that, so a regression that drops the scrub — or routes the widened
// body through an unscrubbed path — mints an edge and fails here.
// ---------------------------------------------------------------------------

const widenBand7185 = "let alpha = () => {\n" + // line 1
	" // notACall() and <NotAComponent />\n" + // line 2, indent 1, comment
	" let s = \"alsoNotACall()\"\n" + //          line 3, indent 1, string
	"  realCall()\n" + //                        line 4, indent 2
	"  <RealComponent />\n" + //                 line 5, indent 2
	"}"

func TestIndentBand7185_ReScript_WideningMintsNothingFromCommentsOrStrings(t *testing.T) {
	ents := band7185Run(t, widenBand7185, "widen.res")
	alpha := band7185Get(t, ents, "alpha", "SCOPE.Operation")

	// MUST-HAVE: the widening really reached the base+1 lines, or the forbidden
	// half below is vacuous.
	if alpha.EndLine != 5 {
		t.Fatalf("alpha EndLine = %d, want 5 — the base+1 lines must be inside the body for the forbidden check to mean anything (pre-fix it was 3)", alpha.EndLine)
	}
	// FORBIDDEN.
	for _, c := range band7185Edges(alpha, "CALLS") {
		if c == "notACall" || c == "alsoNotACall" {
			t.Errorf("alpha CALLS contains %q — the widened body minted an edge from a comment or a string literal", c)
		}
	}
	for _, r := range band7185Edges(alpha, "RENDERS") {
		if r == "NotAComponent" {
			t.Errorf("alpha RENDERS contains %q — the widened body minted a RENDERS edge from a comment", r)
		}
	}
	// MUST-HAVE: the real ones survive.
	if got := band7185Edges(alpha, "CALLS"); strings.Join(got, ",") != "realCall" {
		t.Errorf("alpha CALLS = %v, want [realCall]", got)
	}
	if got := band7185Edges(alpha, "RENDERS"); strings.Join(got, ",") != "RealComponent" {
		t.Errorf("alpha RENDERS = %v, want [RealComponent]", got)
	}
}
