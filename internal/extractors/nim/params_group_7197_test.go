package nim_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #7197 — A PARENLESS PROC MAKES procRE's PARAMS GROUP NON-PARTICIPATING, AND
// NOTHING IN THE SUITE CONTAINED ONE.
//
// procRE's parameter list is an OPTIONAL capture group:
//
//	(\([^)]*\))?   // optional params
//
// FindAllStringSubmatchIndex always returns 2*(1+3) = 8 ints, so an absent
// params group is not omitted — it is reported as the pair `-1, -1`. Two loops
// in nim.go therefore wrap the read in a participation test:
//
//	if m[6] >= 0 && m[7] >= 0 { params = src[m[6]:m[7]] }
//
// Those tests are CORRECT and LOAD-BEARING: without them `src[-1:-1]` panics
// with "slice bounds out of range". They were also completely UNGRADED. Deleting
// both wrappers and indexing unconditionally left the whole package green
// (RUN=121 PASS=104 FAIL=0, identical to baseline), because every single proc
// in every fixture in the package had parentheses. Instrumenting both loops to
// report each non-participating params group across the full suite counted
// exactly ZERO occurrences. A guard whose only job is to prevent a panic
// produces no observable difference when it works, which is precisely how it
// rots: the next author to "simplify" the `if` away sees a green suite.
//
// REACHABILITY, measured against the compiled regex rather than argued:
//
//	procRE on "proc main =\n  discard\n"     -> len(m)=8, m[6]=-1, m[7]=-1
//	procRE on "template t =\n  discard\n"    -> len(m)=8, m[6]=-1, m[7]=-1
//	procRE on "proc foo() =\n  discard\n"    -> len(m)=8, m[6]=8,  m[7]=10
//
// So the -1 pair is not a theoretical shape: procRE MATCHES a parenless
// routine and mints an entity for it. Whether or not one agrees about Nim
// style, the extractor's own pattern admits the input, so the participation
// test is reachable in production on any real file containing one. Nim's
// grammar makes the parameter list optional —
//
//	routine = optInd identVis pattern? genericParamList? paramListColon
//	          pragma? ('=' COMMENT? stmt)?
//	paramListColon = paramList? (':' optInd typeDesc)?
//
// — and `proc main =` is the ordinary spelling of a no-argument routine.
//
// NOT MEASURED AGAINST A POPULATION. The local corpora contain zero .nim files,
// so the frequency of parenless routines in real Nim is NOT claimed here. The
// reachability above is from the compiled regex and is enough: one occurrence
// panics the extractor.
//
// WHAT THIS FILE GRADES. Every row below is chosen so that deleting either
// participation wrapper turns it RED with a slice-bounds panic. That is the
// whole point — the assertions are on the emitted artefact (the signature, the
// entity set, the CONTAINS edges), not on the guard, so they keep grading the
// behaviour if the guard is ever rewritten into a different shape.
//
// AXES VARIED, crossed rather than listed:
//
//	routine keyword (proc / template / iterator / method)
//	  x params (absent / present-and-empty `()` / present-and-non-empty)
//	  x export marker (present / absent)
//	  x return type annotation (absent / `: int`)
//	  x pragma block (absent / `{.inline.}`)
//	  x position relative to a type declaration (before / after, so the
//	    type-CONTAINS loop meets a parenless routine on both sides of its
//	    own match)
//
// HELD CONSTANT, deliberately: spaces-only indentation (Nim forbids tabs);
// ASCII identifiers; one file per fixture; single-line declaration heads (a
// parameter list wrapped across lines is a separate shape, untouched here);
// no generic parameters (that group is non-capturing and cannot be the -1 pair).
//
// DERIVED-NOT-EXECUTED. No Nim toolchain exists on this machine, so every
// expected signature below is derived from buildSig — `kw + " " + name`, with
// `params` appended only when non-empty — and `params` is group 3 of procRE
// and nothing else. That derivation settles every cell including `typed`:
// `proc typed*(a: string): int =` has a participating group 3 of `(a: string)`,
// and the return-type annotation is a NON-CAPTURING group that can never reach
// buildSig, so the expected value is "proc typed(a: string)".
//
// Honesty about how that cell got here: it was first written as
// "proc typed(a: string): int", which is what the DECLARATION HEAD reads, and
// the run disagreed. The value above is the re-derivation, not the observation
// copied back — the observation only showed that the first derivation had
// silently switched from "group 3" to "the text I can see". That is the exact
// failure a derived-not-executed rule exists to catch, so the cell is kept as
// the file's own worked example rather than quietly corrected.
// ---------------------------------------------------------------------------

const parens7197Path = "src/domain/routines.nim"

// parens7197Mixed — the core row. `main` and `setup` are parenless, so their
// params group does not participate; `withArgs` and `empty` are the controls
// that keep the participating path exercised in the same file. If either
// participation wrapper is deleted, extracting this source panics.
const parens7197Mixed = "proc main =\n" + // 1
	"  discard\n" + // 2
	"proc withArgs(x: int) =\n" + // 3
	"  discard\n" + // 4
	"template setup =\n" + // 5
	"  discard\n" + // 6
	"proc empty() =\n" + // 7
	"  discard\n" // 8

// parens7197Decorated — the parenless form crossed with the OTHER optional
// pieces of the same pattern: an export marker, a return-type annotation and a
// pragma block. All three are non-capturing, so none of them can supply the
// -1 pair; only the missing parameter list can. `countUp` additionally varies
// the keyword to `iterator`.
const parens7197Decorated = "proc tally*: int =\n" + // 1
	"  42\n" + // 2
	"iterator countUp {.inline.} =\n" + // 3
	"  discard\n" + // 4
	"proc typed*(a: string): int =\n" + // 5
	"  0\n" // 6

// parens7197WithType — the SECOND site. nim.go's type loop re-scans every proc
// in the file to find methods whose parameters mention the type, and that scan
// has its own copy of the participation wrapper. A parenless routine sits on
// BOTH sides of the type declaration so the scan meets one before and one
// after its own match. `attach` is the positive control: it takes a Widget and
// must still produce the CONTAINS edge, which proves the scan ran rather than
// died early.
const parens7197WithType = "proc before =\n" + // 1
	"  discard\n" + // 2
	"type\n" + // 3
	"  Widget* = object\n" + // 4
	"    id*: int\n" + // 5
	"proc attach(w: Widget) =\n" + // 6
	"  discard\n" + // 7
	"method after =\n" + // 8
	"  discard\n" // 9

func parens7197Ops(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" {
			out = append(out, ents[i].Name)
		}
	}
	sort.Strings(out)
	return out
}

func parens7197Sig(t *testing.T, ents []types.EntityRecord, name, want string) {
	t.Helper()
	e := band7185Get(t, ents, name, "SCOPE.Operation")
	if e.Signature != want {
		t.Errorf("%s: signature %q, want %q", name, e.Signature, want)
	}
}

// TestParensGroup7197_ParenlessRoutineExtractsWithEmptyParamSignature is the
// row that dies if the participation wrapper in the proc loop is removed: the
// unconditional read is src[-1:-1] on `main` and `setup`.
func TestParensGroup7197_ParenlessRoutineExtractsWithEmptyParamSignature(t *testing.T) {
	ents := band7185Run(t, parens7197Mixed, parens7197Path)

	if got, want := strings.Join(parens7197Ops(ents), ","), "empty,main,setup,withArgs"; got != want {
		t.Errorf("SCOPE.Operation set = [%s], want [%s]", got, want)
	}
	// The parenless pair: buildSig appends nothing when params is empty.
	parens7197Sig(t, ents, "main", "proc main")
	parens7197Sig(t, ents, "setup", "template setup")
	// The participating controls, in the same file, must keep their parameters.
	parens7197Sig(t, ents, "withArgs", "proc withArgs(x: int)")
	parens7197Sig(t, ents, "empty", "proc empty()")
}

// TestParensGroup7197_ParenlessCrossedWithTheOtherOptionalPieces holds the
// export marker, return type and pragma axes against the same -1 pair.
//
// NOT independent evidence for the proc-loop wrapper, and it must not be counted
// as a second kill for it. A slice-bounds panic aborts the whole test BINARY, so
// when that wrapper is deleted the Mixed row above panics first and this row
// never executes — it is masked. Its value is as a SIGNATURE pin across the
// non-capturing axes (a widening that started capturing the return type or the
// pragma shows up here and nowhere else), not as coverage of the guard. The
// guard's two copies are each killed by exactly one designated row: Mixed for
// the proc loop, WithType for the type re-scan.
func TestParensGroup7197_ParenlessCrossedWithTheOtherOptionalPieces(t *testing.T) {
	ents := band7185Run(t, parens7197Decorated, parens7197Path)

	if got, want := strings.Join(parens7197Ops(ents), ","), "countUp,tally,typed"; got != want {
		t.Errorf("SCOPE.Operation set = [%s], want [%s]", got, want)
	}
	parens7197Sig(t, ents, "tally", "proc tally")
	parens7197Sig(t, ents, "countUp", "iterator countUp")
	// NOT "proc typed(a: string): int". The return-type annotation is a
	// NON-CAPTURING group, so it never reaches `params` and buildSig never sees
	// it — the signature carries the parameter list alone. This is the cell that
	// distinguishes "the signature echoes the declaration head" from "the
	// signature echoes group 3"; see the header for why it is kept.
	parens7197Sig(t, ents, "typed", "proc typed(a: string)")
}

// TestParensGroup7197_TypeMethodScanSurvivesParenlessRoutine is the row for the
// SECOND participation wrapper — the one inside the type loop's proc re-scan.
// Scored separately from the first on purpose: a verdict on one copy of a
// duplicated guard says nothing about the other.
func TestParensGroup7197_TypeMethodScanSurvivesParenlessRoutine(t *testing.T) {
	ents := band7185Run(t, parens7197WithType, parens7197Path)

	if got, want := strings.Join(parens7197Ops(ents), ","), "after,attach,before"; got != want {
		t.Errorf("SCOPE.Operation set = [%s], want [%s]", got, want)
	}
	parens7197Sig(t, ents, "before", "proc before")
	parens7197Sig(t, ents, "after", "method after")

	// The type's method scan must have run to completion across both parenless
	// routines and still linked `attach`, the one proc that names the type.
	w := band7185Get(t, ents, "Widget", "SCOPE.Component")
	var contains []string
	for _, r := range w.Relationships {
		if r.Kind == "CONTAINS" {
			contains = append(contains, r.ToID)
		}
	}
	if len(contains) != 1 || !strings.Contains(contains[0], "attach") {
		t.Errorf("Widget CONTAINS = %v, want exactly one edge naming attach", contains)
	}
}
