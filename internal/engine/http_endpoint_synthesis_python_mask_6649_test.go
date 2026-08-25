package engine

import (
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
// Refs #6649, #6648, #6629, #6614, #6418.
func TestPythonMaskInertRegions_PreservesLineStructure_6649(t *testing.T) {
	cases := []struct {
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
	}{
		{
			// #6648 changed what this row can see. The comment loop now halts
			// ON a `\r` as well as a `\n`, so caller A never hands blank() a
			// line terminator of EITHER kind any more: the CR of a CRLF is the
			// byte it stops at. This row is therefore caller-A coverage of the
			// blanking path only — it can no longer observe blank()'s preserve
			// set. It still fails under the INVERTED predicate (blank only the
			// terminators), but every row here does, so it is load-bearing
			// against nothing. blankcr stays dead through caller C.
			name:       "commentCRLF",
			caller:     "A: `#`-comment loop",
			src:        "import os\r\n# app.include_router(r, prefix='/ghost')\r\napp = FastAPI()\r\n",
			mustVanish: "include_router",
			kills:      "cannot observe the preserve set since #6648; dies only under the inverted predicate, which every row here kills — load-bearing against nothing",
		},
		{
			// The lone-CR shape, kept because it is the one #6648 turned. The
			// marker had to move INSIDE the comment: the bytes after the bare
			// `\r` are live code on the next line now, so they are no longer
			// blanked and the old marker made the premise guard fire. What the
			// row still asserts is the whole-buffer claim — the lone `\r`
			// survives at its own offset — which is exactly the byte-offset
			// contract #6648 must not have disturbed.
			name:       "commentLoneCR",
			caller:     "A: `#`-comment loop",
			src:        "import os\n# first half\rapp = FastAPI()\n",
			mustVanish: "first half",
			kills:      "cannot observe the preserve set since #6648; dies only under the inverted predicate, which every row here kills — load-bearing against nothing",
		},
		{
			// The #6649 witness, and the ONLY shape in this table that can
			// hand blank() a '\n'.
			name:       "docstringMultiline",
			caller:     "B: triple-quoted span",
			src:        "\"\"\"doc\nline2\"\"\"\napp = FastAPI()\n",
			mustVanish: "line2",
			kills:      "fails under blanknl and both; the only caller that can see a '\\n'",
		},
		{
			// Same caller with CRLF endings: proves the CR clause on a span
			// that #6648 cannot reach, so this kill is stable whichever way
			// the comment terminator is resolved.
			name:       "docstringCRLF",
			caller:     "B: triple-quoted span",
			src:        "\"\"\"doc\r\nline2\r\n\"\"\"\r\napp = FastAPI()\r\n",
			mustVanish: "line2",
			kills:      "fails under blankcr, blanknl and both — the widest fixture here",
		},
		{
			name:   "unterminatedCR",
			caller: "C: unterminated single-line literal (#6614)",
			src:    "x = \"abc\rdef\napp = FastAPI()\n",
			// The marker sits AFTER the CR, so the premise guard below sees a
			// preserved byte inside the blanked prefix.
			mustVanish: "def",
			kills:      "fails under blankcr and both; caller coverage only, redundant as a kill",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
			// The fixture must carry at least one byte that the predicate
			// under test is supposed to preserve, INSIDE a region that gets
			// blanked. Without that, pristine and every mutant produce the
			// same output and the contract assertion observes nothing.
			region := tc.src[:at+len(tc.mustVanish)]
			if !strings.ContainsAny(region, "\n\r") {
				t.Fatalf("#6649 premise broken: fixture %q has no '\\n' or '\\r' at or before "+
					"the blanked marker %q, so blank() is never handed a byte it must "+
					"preserve and every mutant survives. This subtest is VACUOUS.",
					tc.src, tc.mustVanish)
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
		})
	}
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
