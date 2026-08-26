package engine

import (
	"fmt"
	"strings"
	"testing"
)

// pythonMaskInertRegions' doc comment promises a copy of src of the SAME BYTE
// LENGTH, with THE SAME NEWLINE POSITIONS, so that a byte offset in the masked
// copy addresses exactly the same source position as in the original. The only
// thing that delivers the second half of that promise is one predicate inside
// the writer, blank():
//
//	if b[i] != '\n' && b[i] != '\r' {
//		b[i] = ' '
//	}
//
// Neither clause was observed on d7eec1d33. Dropping either one left `go vet`
// at 0 and the full engine package at exit 0:
//
//	if b[i] != '\n' {   // "blankcr" — SURVIVOR, the CR is destroyed
//	if b[i] != '\r' {   // "blanknl" — SURVIVOR, the LF is destroyed
//	if true {           // "both"    — SURVIVOR, line structure gone entirely
//
// blanknl is the damaging one. Inside a multi-line triple-quoted docstring it
// rewrites a newline as a space, so two source lines MERGE in the masked copy
// while every later offset still lines up byte-for-byte. Anything downstream
// that reads the masked copy per line, or maps an offset back to a line
// number, then silently reads a collapsed line — wrong quietly, which this
// project ranks above breaking loudly. The witness from #6649:
//
//	src       "\"\"\"doc\nline2\"\"\"\napp..."
//	pristine  "      \n        \napp..."
//	mutant    "               \napp..."
//
// Why the whole nine-item pythonMaskInertRegions cluster missed it: every
// earlier item was about the SCANNER — which branch is entered, how far it
// runs, where it stops. blank() is the WRITER, and no fixture had ever
// asserted anything about it beyond "the span became spaces", an assertion
// that cannot see a preserved byte at all. #6629's C4 is the only place it is
// even mentioned, and only to explain why a fixture may NOT contain a `\r` —
// which is exactly the shape needed to observe the CR clause. So the guard
// that kept the old assertions honest is what kept this hole open, and the
// fix is to stop asserting the SPELLING of the masked copy and assert the
// CONTRACT instead.
//
// The contract, stated once, kills both clauses at once:
//
//	len(got) == len(src), AND
//	the ORDERED offsets of '\n' in got == those in src, AND
//	the ORDERED offsets of '\r' in got == those in src.
//
// This also sidesteps the trap that forced #6629's C4: an all-spaces
// assertion cannot be used naively on a fixture containing `\r`, precisely
// because blank() preserves it. Asserting the preserved-byte POSITIONS needs
// no such guard — a `\r` in the region is the point, not a hazard.
//
// blank() has THREE call sites, and a fixture that exercises one pins one
// caller, not the helper. All three are covered here, and they do not have
// equal power, because two of them can never see a '\n' at all:
//
//	CALLER A — the `#`-comment loop, `for ; i < len(b) && b[i] != '\n'; i++`.
//	  Halts ON the newline, so blank() is never handed a '\n' from here. It
//	  CAN be handed a '\r' (the CR of a CRLF, or a lone CR mid-comment).
//	  Pins blankcr and both; CANNOT pin blanknl.
//	CALLER B — the triple-quote span, `for k := i; k < j; k++`. The only site
//	  whose span crosses line boundaries, so the only one that can hand
//	  blank() a '\n'. Pins blankcr, blanknl and both.
//	CALLER C — the UNTERMINATED single-line literal span (#6614), same loop
//	  shape but j stops at the quote or the '\n'. Like caller A it never sees
//	  a '\n', and like caller A it can see a '\r'.
//	  Pins blankcr and both; CANNOT pin blanknl.
//
// So blanknl is killable ONLY through caller B, and the two docstring
// fixtures below are what do it. That asymmetry is the reason the fixtures are
// labelled by caller rather than by shape.
//
// Three mutant FAMILIES were scored separately, not one family against varied
// input — the distinction #6625 paid for. Measured on d7eec1d33, full package
// `go test -count=1 ./internal/engine/`, `go vet` 0 throughout:
//
//	                  before this file    after
//	blankcr           exit 0 (SURVIVOR)   exit 1 DEAD
//	blanknl           exit 0 (SURVIVOR)   exit 1 DEAD
//	both              exit 0 (SURVIVOR)   exit 1 DEAD
//
// Per-fixture drop analysis. The subtests are independent, so the drop table
// is DERIVED from the measured per-subtest failure matrix rather than guessed
// — dropping a fixture removes exactly its row. Measured with each mutant
// applied and the full package run; a bullet means that subtest FAILED:
//
//	                       blankcr   blanknl   both
//	commentCRLF        A      X                  X
//	commentLoneCR      A      X                  X
//	docstringMultiline B                X        X
//	docstringCRLF      B      X        X         X
//	unterminatedCR     C      X                  X
//
// Each verdict names the MUTANT it is load-bearing against, because an
// unqualified "load-bearing" is an incomplete claim (#6632, #6647):
//
//	drop commentCRLF        no survivor — blankcr/both still die on 3 others
//	drop commentLoneCR      no survivor — same
//	drop docstringMultiline no survivor — blanknl still dies on docstringCRLF
//	drop docstringCRLF      no survivor — blanknl still dies on docstringMultiline
//	drop unterminatedCR     no survivor — same
//	drop BOTH caller-B      blanknl SURVIVES (measured, exit 0); blankcr and
//	                        both still die on callers A and C
//	drop callers A and C    no survivor (measured, exit 1 for all three) —
//	                        caller B alone kills the whole table
//
// Two honest negative results follow, recorded rather than dressed up:
//
//  1. NO SINGLE fixture is load-bearing against any mutant. Only the caller-B
//     PAIR is, and only against blanknl. The two docstring fixtures are
//     mutually redundant as kills; they are kept because docstringMultiline
//     is #6649's own witness and docstringCRLF is the one blanknl/blankcr
//     kill that #6648 cannot disturb.
//  2. The caller-A and caller-C fixtures kill NOTHING that caller B does not
//     already kill. They are load-bearing for CALLER COVERAGE only — they are
//     what makes this a statement about blank() rather than about the
//     triple-quote branch. If the mutant table were the sole goal they could
//     all be deleted, and that is exactly the reasoning that would have left
//     two of blank()'s three call sites unobserved.
//
// Out of scope and separately tracked: #6648 — the comment loop's TERMINATOR
// treating a lone `\r` as content. That is the SCANNER's stop condition; this
// is the WRITER's predicate. They touch the same byte and are not the same
// bug, and this file does not fix or pin #6648. Caller B's fixtures are
// deliberately immune to it, so the blanknl and blankcr kills stand whichever
// way #6648 is resolved. Also out of scope: #6418, a real Python tokenizer,
// recorded as the eventual fix for this whole family; and the recorded
// equivalences at the single-line clamp and the mixed-quote terminator.
//
// #6654 tightened the premise guard below, and — the more useful half —
// measured that the preserve set's OTHER direction needs nothing here at all.
// Both are recorded because both change what THIS file claims:
//
//  1. The guard's region used to be `tc.src[:at+len(tc.mustVanish)]` — a prefix
//     from BYTE 0. It therefore accepted a '\n' belonging to live code BEFORE
//     the inert region, so a fixture could satisfy every premise and the
//     contract assertion while observing nothing. It is now derived from what
//     masking actually blanked around the marker (blankedRunAround6649), and
//     each row carries observesPreserveSet, checked in BOTH directions.
//     The tightened guard is load-bearing, DEMONSTRATED rather than asserted.
//     #6654's own witness row was added temporarily and the full package run
//     three times on 1b343351c, `go vet` 0 throughout:
//
//     {name: "guardHole6650", caller: "A: `#`-comment loop",
//     src: "import os\n# app.include_router(r)\napp = FastAPI()\n",
//     mustVanish: "include_router"}
//
//     - OLD guard, pristine source: exit 0. The row PASSES.
//     - OLD guard, blankcr applied (`if b[i] != '\n' {`): exit 1, and the
//     failures are docstringCRLF and unterminatedCR ONLY. guardHole6650
//     passes — it observes NOTHING while looking like a fifth observation.
//     That is the vacuity the guard exists to stop, measured.
//     - NEW guard, pristine source: exit 1 on guardHole6650 alone, reporting
//     the blanked run as "app.include_router(r)" (offsets 12..32) with no
//     terminator in it. The row is rejected as VACUOUS, which is correct:
//     its only '\n' belongs to the live code line before the comment and is
//     never handed to blank().
//
//     The run is deliberately CONSERVATIVE at its edges: an unchanged byte
//     that masking happened to leave alone — a source ' ' inside the blanked
//     span — stops the outward walk. It can therefore under-report the
//     region, never over-report it, which is the safe direction for a premise.
//
//  2. The preserve set's OTHER direction — a byte JOINING the set — is NOT
//     pinned here and needs no fixture anywhere. #6654 was filed on a measured
//     survivor, `&& b[i] != '\t'`. Re-scored on 1b343351c, `go vet` 0, full
//     package `go test -count=1 ./internal/engine/`, each mutant applied and
//     scored separately, that survivor is DEAD and so is the whole family:
//
//     && b[i] != '\t' (0x09)  exit 1 DEAD   #6683 sweep, #6648 CRLF control
//     && b[i] != '\v' (0x0B)  exit 1 DEAD   #6683 sweep byte_0x0b
//     && b[i] != 0x00         exit 1 DEAD   #6683 sweep byte_0x00
//     && b[i] != '\\' (0x5C)  exit 1 DEAD   #6629 backslash-inside-a-comment
//     && b[i] != ' '  (0x20)  exit 0        EQUIVALENT, see below
//
//     The family is CLOSED, byte for byte, with no gap to fill:
//
//     - #6683's escape-set sweep enumerates 253 of the 256 byte values (all
//     but `\`, `\n` and `\r`) and its EXECUTION WITNESS asserts a #6614
//     blanked tail byte-for-byte. A preserve-set widening on byte X leaves
//     X live in that probe, so the row fails — as a PREMISE failure
//     ("#6680 ... This row is now VACUOUS"), which names the wrong
//     predicate but does kill the mutant.
//     - `\` is the one byte that sweep excludes, by its own g3, and #6629's
//     backslash-inside-a-comment fixture carries it.
//     - `\n` and `\r` are the set itself: the REMOVAL direction, pinned by
//     the rows below.
//     - `' '` is an EQUIVALENT mutant, recorded and never scored as DEAD.
//     The only thing blank() does to a non-preserved byte is write `' '`,
//     so on input `' '` pristine and mutant leave the same byte at the same
//     offset. Measured as a survivor, which is what an equivalence looks
//     like. No fixture was manufactured to force it.
//
//     A 253-byte sweep of the widening direction WAS written and then cut: it
//     re-killed what #6683 already kills, at the cost of a second place that
//     has to agree with the first about one predicate. What it would have
//     bought is attribution — a widening currently fails as #6680's premise,
//     on the escape set at :3242, not on the writer's preserve set at :3196.
//     That is a diagnosis cost on a mutant that CANNOT reach main, not
//     missing coverage, and this note is the cheaper fix. If #6683's sweep is
//     ever narrowed, or #6629's backslash fixture dropped, the widening family
//     goes dark again — a label change is a coverage claim (#6659).
//
// WHICH AXES THIS FIXTURE SET VARIES, AND WHICH IT HOLDS CONSTANT. Four PRs in
// this cluster pinned one axis and left its neighbour open, so the constants are
// stated and justified rather than left to be discovered by the next mutant:
//
//	axis                         varied?   why
//	CALLER (A / B / C)           VARIED    blank() has three call sites and two
//	                                       of them can never see a '\n'; a
//	                                       single-caller table would be a
//	                                       statement about the triple-quote
//	                                       branch, not about blank().
//	TERMINATOR KIND (LF/CRLF/    VARIED    the removal direction splits on it:
//	lone CR)                               blanknl is killable only through a
//	                                       '\n', blankcr only through a '\r'.
//	MARKER ON A LIVE CODE LINE   VARIED    the shipped rows all have the marker
//	                                       inside an inert region; the #6654 pin
//	                                       supplies the other value of this axis
//	                                       — a marker whose only terminator is on
//	                                       LIVE code — and asserts it is
//	                                       REJECTED. Before #6654 this axis had
//	                                       one value and the guard was unpinned.
//	PRESERVE-SET BYTE            CONSTANT  every row's blanked region contains
//	(which byte is added)                  only '\n'/'\r' as preserved bytes, so
//	                                       no row can observe a WIDENING. That is
//	                                       deliberate: the widening family is
//	                                       closed by #6683 and #6629 (item 2
//	                                       above), so varying it here would
//	                                       duplicate coverage, and a row carrying
//	                                       both axes would attribute neither.
//	SCANNER BRANCH SHAPE         CONSTANT  fixtures are minimal, well-formed
//	                                       Python; the scanner's stop conditions
//	                                       are #6648/#6653/#6637's axis and a
//	                                       fixture varying both would fail for
//	                                       reasons it could not name — #6659
//	                                       records a #6652 fixture silently
//	                                       intercepted by #6648.
//
// Refs #6654, #6649, #6648, #6683, #6680, #6629, #6614, #6418.
// preserveSetCase6649 is one fixture of the table below. It is a NAMED type
// so the guard that runs over it can be exercised from a second test without
// duplicating it — see TestPythonMaskInertRegions_PremiseGuardRejectsLiveCodeTerminator_6654.
type preserveSetCase6649 struct {
	name string
	// caller records WHICH call site of blank() the fixture drives, so a
	// later edit cannot quietly turn this into three tests of the
	// triple-quote branch.
	caller string
	src    string
	// mustVanish is a substring of src that the masking MUST turn into
	// spaces. It is the premise guard: it proves the blanking path
	// actually ran over the region under test, rather than the fixture
	// being copied through untouched (in which case the '\n' and '\r'
	// offsets would match trivially and every mutant would survive).
	// It never contains a '\n' or a '\r' — see mustVanish's own guard.
	mustVanish string
	// kills names the mutant families this fixture is load-bearing
	// against, per the measured drop analysis above.
	kills string
	// observesPreserveSet records whether this row can see blank()'s
	// preserve set AT ALL, i.e. whether the region that actually came out
	// blanked contains a byte blank() must preserve. It is not
	// documentation: the premise below checks the claim in BOTH
	// directions, so a false label fails as loudly as a broken fixture
	// (#6654). Since #6648 the two caller-A rows are honestly false — the
	// comment loop halts ON a terminator of either kind, so caller A can
	// never hand blank() one.
	observesPreserveSet bool
}

// preserveSetCases6649 is the shipped fixture table. It is a function rather
// than a literal inside the test so the #6654 pin can run the SAME rows through
// the SAME guard as its positive control — a control built from a copy would
// drift away from what actually ships.
func preserveSetCases6649() []preserveSetCase6649 {
	return []preserveSetCase6649{
		{
			// #6648 changed what this row can see. The comment loop now halts
			// ON a `\r` as well as a `\n`, so caller A never hands blank() a
			// line terminator of EITHER kind any more: the CR of a CRLF is the
			// byte it stops at. This row is therefore caller-A coverage of the
			// blanking path only — it can no longer observe blank()'s preserve
			// set. It still fails under the INVERTED predicate (blank only the
			// terminators), but every row here does, so it is load-bearing
			// against nothing. blankcr stays dead through caller C.
			name:                "commentCRLF",
			caller:              "A: `#`-comment loop",
			src:                 "import os\r\n# app.include_router(r, prefix='/ghost')\r\napp = FastAPI()\r\n",
			mustVanish:          "include_router",
			kills:               "cannot observe the preserve set since #6648; dies only under the inverted predicate, which every row here kills — load-bearing against nothing",
			observesPreserveSet: false,
		},
		{
			// The lone-CR shape, kept because it is the one #6648 turned. The
			// marker had to move INSIDE the comment: the bytes after the bare
			// `\r` are live code on the next line now, so they are no longer
			// blanked and the old marker made the premise guard fire. What the
			// row still asserts is the whole-buffer claim — the lone `\r`
			// survives at its own offset — which is exactly the byte-offset
			// contract #6648 must not have disturbed.
			name:                "commentLoneCR",
			caller:              "A: `#`-comment loop",
			src:                 "import os\n# first half\rapp = FastAPI()\n",
			mustVanish:          "first half",
			kills:               "cannot observe the preserve set since #6648; dies only under the inverted predicate, which every row here kills — load-bearing against nothing",
			observesPreserveSet: false,
		},
		{
			// The #6649 witness, and the ONLY shape in this table that can
			// hand blank() a '\n'.
			name:                "docstringMultiline",
			caller:              "B: triple-quoted span",
			src:                 "\"\"\"doc\nline2\"\"\"\napp = FastAPI()\n",
			mustVanish:          "line2",
			kills:               "fails under blanknl and both; the only caller that can see a '\\n'",
			observesPreserveSet: true,
		},
		{
			// Same caller with CRLF endings: proves the CR clause on a span
			// that #6648 cannot reach, so this kill is stable whichever way
			// the comment terminator is resolved.
			name:                "docstringCRLF",
			caller:              "B: triple-quoted span",
			src:                 "\"\"\"doc\r\nline2\r\n\"\"\"\r\napp = FastAPI()\r\n",
			mustVanish:          "line2",
			kills:               "fails under blankcr, blanknl and both — the widest fixture here",
			observesPreserveSet: true,
		},
		{
			name:   "unterminatedCR",
			caller: "C: unterminated single-line literal (#6614)",
			src:    "x = \"abc\rdef\napp = FastAPI()\n",
			// The marker sits AFTER the CR, so the premise guard below sees a
			// preserved byte inside the blanked prefix.
			mustVanish:          "def",
			kills:               "fails under blankcr and both; caller coverage only, redundant as a kill",
			observesPreserveSet: true,
		},
	}
}

func TestPythonMaskInertRegions_PreservesLineStructure_6649(t *testing.T) {
	cases := preserveSetCases6649()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkPreserveSetRow6649(t, tc)
		})
	}
}

// fataler6649 is the slice of *testing.T that checkPreserveSetRow6649 uses. It
// exists so the SAME guard code that runs under `go test` can be run against a
// deliberately broken fixture and its rejection OBSERVED, rather than the
// rejection being described in a comment (#6654). *testing.T satisfies it.
type fataler6649 interface {
	Helper()
	Fatalf(format string, args ...any)
}

// checkPreserveSetRow6649 is the whole body of one row: premise guards first,
// then the #6649 contract claim. Every t.Fatalf below is a guard whose wording
// says which of the two it is.
func checkPreserveSetRow6649(t fataler6649, tc preserveSetCase6649) {
	t.Helper()
	// --- premise ---------------------------------------------------
	// The claim is about a byte that blank() PRESERVES, so the premise
	// has to establish that blank() ran over a region containing one.

	if strings.ContainsAny(tc.mustVanish, "\n\r") {
		t.Fatalf("#6649 premise broken: mustVanish %q contains a line terminator, "+
			"which blank() PRESERVES — the all-spaces check below would fail on "+
			"correct code. Pick a marker without one.", tc.mustVanish)
	}
	at := strings.Index(tc.src, tc.mustVanish)
	if at < 0 {
		t.Fatalf("#6649 premise broken: marker %q is not in fixture %q, so nothing "+
			"establishes that the blanking path ran. This subtest is VACUOUS.",
			tc.mustVanish, tc.src)
	}
	got := pythonMaskInertRegions(tc.src)

	// The blanking path ran: this is what makes the contract check
	// below a statement about blank() and not about a pass-through.
	for k := at; k < at+len(tc.mustVanish); k++ {
		if got[k] != ' ' {
			t.Fatalf("#6649 premise broken (caller %s): byte %d of the inert region "+
				"survived masking as %q — the fixture is not being blanked at all, so "+
				"nothing here exercises blank(). src %q, masked %q",
				tc.caller, k, got[k], tc.src, got)
		}
	}

	// The fixture must carry a byte the predicate under test is
	// supposed to preserve INSIDE a region that actually came out
	// BLANKED. #6654: this used to be measured over tc.src[:at+len],
	// a prefix from BYTE 0, so a '\n' belonging to live code BEFORE
	// the inert region satisfied it and a fixture could observe
	// nothing while passing. The region is now derived from got: the
	// run of bytes around the marker that masking actually changed,
	// with preserved terminators admitted only when they are FLANKED
	// by changed bytes (a terminator at the edge of the run is
	// untouched live code, not evidence that blank() saw one).
	lo, hi := blankedRunAround6649(tc.src, got, at, len(tc.mustVanish))
	region := ""
	if lo <= hi {
		region = tc.src[lo : hi+1]
	}
	sawPreserved := strings.ContainsAny(region, "\n\r")
	if tc.observesPreserveSet && !sawPreserved {
		t.Fatalf("#6649 premise broken (caller %s): the region masking actually blanked "+
			"around marker %q is %q (offsets %d..%d of %q), and it contains no '\\n' or "+
			"'\\r'. blank() is therefore never handed a byte it must preserve, pristine "+
			"and every preserve-set mutant produce the same output, and the contract "+
			"assertion below observes NOTHING. This subtest is VACUOUS.",
			tc.caller, tc.mustVanish, region, lo, hi, tc.src)
	}
	if !tc.observesPreserveSet && sawPreserved {
		t.Fatalf("#6649 premise broken (caller %s): this row is labelled as UNABLE to "+
			"observe blank()'s preserve set, but the blanked run %q (offsets %d..%d of "+
			"%q) does contain a preserved byte. A `kills` label is a coverage claim "+
			"(#6659): relabel the row and re-score what it now kills, rather than "+
			"leaving a real observation recorded as none.",
			tc.caller, region, lo, hi, tc.src)
	}

	// --- claim -----------------------------------------------------
	// pythonMaskInertRegions' doc comment: same byte length, same
	// newline positions, so offsets address identical source
	// positions. Asserted as stated, not as a fixture spelling.
	if len(got) != len(tc.src) {
		t.Fatalf("#6649 (caller %s, kills %s): pythonMaskInertRegions changed length: "+
			"got %d (%q), want %d (%q). Offsets into the masked copy no longer address "+
			"the same source positions.",
			tc.caller, tc.kills, len(got), got, len(tc.src), tc.src)
	}
	for _, nl := range []byte{'\n', '\r'} {
		want := byteOffsets6649(tc.src, nl)
		have := byteOffsets6649(got, nl)
		if !sameOffsets6649(want, have) {
			t.Fatalf("#6649 (caller %s, kills %s): the %q offsets of the masked copy "+
				"are %v, but the source's are %v. blank() must preserve every line "+
				"terminator, or a line boundary inside the masked region VANISHES: "+
				"two source lines merge, and every consumer that reads the masked copy "+
				"per line, or maps an offset back to a line number, silently reads a "+
				"collapsed line. src %q, masked %q",
				tc.caller, tc.kills, nl, have, want, tc.src, got)
		}
	}
}

// fatalRecorder6649 captures the FIRST t.Fatalf a row produces instead of
// failing the run, so a fixture that MUST be rejected can be asserted on. Fatalf
// has to abort the way *testing.T's does — the code after a broken premise
// indexes into a region that guard just proved wrong — so it panics with a
// sentinel that recordRow6649 recovers.
type fatalRecorder6649 struct{ msgs []string }

type fatalSentinel6649 struct{}

func (r *fatalRecorder6649) Helper() {}

func (r *fatalRecorder6649) Fatalf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
	panic(fatalSentinel6649{})
}

// recordRow6649 runs one row through the real guard and returns the message it
// was rejected with, or "" if the row passed. Any panic that is NOT the
// sentinel is re-raised: a nil deref in the guard must not read as "accepted".
func recordRow6649(tc preserveSetCase6649) (msg string) {
	r := &fatalRecorder6649{}
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(fatalSentinel6649); !ok {
				panic(p)
			}
		}
		if len(r.msgs) > 0 {
			msg = r.msgs[0]
		}
	}()
	checkPreserveSetRow6649(r, tc)
	return ""
}

// TestPythonMaskInertRegions_PremiseGuardRejectsLiveCodeTerminator_6654 is the
// REGRESSION PIN for #6654's second half. The fix it pins is the region the
// premise guard measures over, and without this test that fix is unobserved:
// reverting blankedRunAround6649 to
//
//	lo, hi := 0, len(src)-1
//
// — the exact byte-0 prefix #6654 describes — leaves `go vet` at 0 and the full
// engine package at exit 0. The guard's own defect class, one level up: the PR
// subject is a premise that passes vacuously, and its fix was documented in a
// comment rather than observed.
//
// The witness is #6654's guardHole6650 fixture. It cannot be a row of the table
// above, because a correct guard REJECTS it — a pin must pass on correct code
// and fail on the defect, so the assertion is inverted here: the guard must
// reject it, with the run it actually measured named in the message.
//
// Under the byte-0 region the fixture is accepted and observes NOTHING: its
// only terminator is the '\n' of the live `import os` line before the comment,
// which the `#`-comment loop never hands to blank(). Measured (#6654): with the
// old guard and this fixture added as a table row, `blankcr` produced exit 1
// through docstringCRLF and unterminatedCR ONLY — guardHole6650 passed, looking
// like a fifth observation while making none.
//
// The POSITIVE CONTROL is the second half and is not optional: a harness that
// rejected everything would satisfy the first assertion on its own. Every real
// row must come back accepted.
//
// Refs #6654, #6649, #6659.
func TestPythonMaskInertRegions_PremiseGuardRejectsLiveCodeTerminator_6654(t *testing.T) {
	hole := preserveSetCase6649{
		name:       "guardHole6650",
		caller:     "A: `#`-comment loop",
		src:        "import os\n# app.include_router(r)\napp = FastAPI()\n",
		mustVanish: "include_router",
		kills:      "#6654 NEGATIVE fixture: exists to be REJECTED, never added to the table",
		// The label the fixture would carry if someone wrote it in good faith:
		// it LOOKS like a caller-A observation of the preserve set, and the
		// point is that it is not one.
		observesPreserveSet: true,
	}

	msg := recordRow6649(hole)
	if msg == "" {
		t.Fatalf("#6654: the premise guard ACCEPTED %q. Its only line terminator is the '\\n' of "+
			"the live `import os` line BEFORE the inert region, and the `#`-comment loop never "+
			"hands blank() a terminator (#6648), so this fixture observes NOTHING about the "+
			"preserve set while passing every assertion. The guard's region must be derived from "+
			"what masking actually BLANKED around the marker, not from a prefix starting at byte "+
			"0 — that prefix is the #6654 defect and this row is what sees it.", hole.src)
	}
	for _, want := range []string{"VACUOUS", "app.include_router(r)", "offsets 12..32"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("#6654: the guard rejected the fixture but not for the region it should have "+
				"measured: the message must name %q so the failure is self-explanatory and so a "+
				"guard that rejects for some OTHER reason cannot pass as this pin. Got: %s", want, msg)
		}
	}

	// POSITIVE CONTROL — a guard that rejects everything would pass the above.
	// Scoped to PREMISE failures only: a row can also fail on the #6649 CONTRACT
	// (that is what blankcr does to docstringCRLF and unterminatedCR), and that
	// is a statement about blank(), attributed by the table above. Folding it in
	// here would make this test fail for a reason it does not name — the
	// misattribution this cluster keeps paying for (#6659).
	for _, tc := range preserveSetCases6649() {
		if m := recordRow6649(tc); strings.Contains(m, "premise broken") {
			t.Fatalf("#6654 control broken: a PREMISE guard rejected the shipped row %q (caller %s), "+
				"which must be accepted. Without this half, a harness that rejects every fixture "+
				"would satisfy the assertion above and this test would prove nothing. Rejection: %s",
				tc.name, tc.caller, m)
		}
	}
}

// blankedRunAround6649 returns the inclusive bounds of the run of bytes that
// masking actually changed around src[at:at+n], which is the region a premise
// about blank()'s preserve set may legitimately be measured over (#6654).
//
// A byte belongs to the run if masking CHANGED it, or if it is a line
// terminator — terminators are the bytes blank() preserves, so got == src there
// even in the middle of a blanked span and a change test alone would split the
// run at exactly the byte of interest. Admitting them unconditionally would
// re-open the hole this replaces, since the terminator ENDING a blanked span
// belongs to live code; so the run is then TRIMMED at both ends back to a
// changed byte. What survives is exactly "terminators flanked by blanked
// bytes", i.e. terminators blank() was really handed.
func blankedRunAround6649(src, got string, at, n int) (int, int) {
	inRun := func(k int) bool {
		return got[k] != src[k] || src[k] == '\n' || src[k] == '\r'
	}
	lo, hi := at, at+n-1
	for lo > 0 && inRun(lo-1) {
		lo--
	}
	for hi < len(src)-1 && inRun(hi+1) {
		hi++
	}
	for lo <= hi && got[lo] == src[lo] {
		lo++
	}
	for hi >= lo && got[hi] == src[hi] {
		hi--
	}
	return lo, hi
}

// byteOffsets6649 returns the ordered offsets of c in s.
func byteOffsets6649(s string, c byte) []int {
	var out []int
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			out = append(out, i)
		}
	}
	return out
}

func sameOffsets6649(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
