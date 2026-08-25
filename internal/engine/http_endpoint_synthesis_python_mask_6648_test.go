package engine

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #6648 — pythonMaskInertRegions' `#`-comment loop ran to `\n` ONLY, so a bare
// carriage return was treated as ordinary comment content. Python disagrees:
// under universal newlines a lone `\r` IS a line terminator, so the bytes after
// it are live code on the NEXT line. We blanked them, and a real mount was
// silently masked away before the scan ever saw it.
//
// That makes this a FALSE NEGATIVE, and the fix moves in the direction of LESS
// masking — which can only stop discarding real code, never mint an endpoint
// that does not exist. That asymmetry is why the comment branch is decidable on
// its own while its neighbours are not.
//
// Scope, deliberately: the comment branch's terminator test and nothing else.
// Explicitly untouched, each separately tracked —
//
//   - the TRIPLE-QUOTED branch: a lone `\r` inside a triple-quoted literal
//     genuinely IS content, so having no terminator test there is already
//     correct (#6655 owns its blanked range);
//   - `blank()`: preserving `\n` AND `\r` is the contract that keeps byte
//     offsets meaningful, pinned by #6649/#6650 (#6654 owns its preserve set);
//   - #6653, the single-line literal scan. Same byte, MIXED direction: making
//     it `\r`-aware would make some TERMINATED literals count as unterminated
//     (more masking) while shortening unterminated spans (less), which moves
//     the deliberate #6418/#6614 boundary. It needs its own decision.
//
// Reachability is not hypothetical: nothing on the Python path normalises CR.
// Bytes are read raw and forwarded verbatim into the synthesis pass, so an
// old-Mac-ending file, or a CR introduced mid-line by a generator or a bad
// merge, hits this in production.
//
// ---------------------------------------------------------------------------
// MUTANT FAMILIES, scored separately (`go vet` 0, then the FULL package with
// `go test -count=1 ./internal/engine/`, scored on the exit code). A sweep
// varies the INPUT against a FIXED mutant and says nothing about whether the
// fixture pins the FAMILY — the lesson #6625 paid for — so the family is what
// is varied here.
//
//	M1  stop at `\r` ONLY   `for ; i < len(b) && b[i] != '\r'; i++`
//	M2  stop at NEITHER     `for ; i < len(b); i++`
//	M3  CRLF-only           stop at `\r` only when it is followed by `\n`,
//	                        i.e. `\r\n` ends the comment, a LONE `\r` stays
//	                        content. On a lone CR this is behaviourally the
//	                        PRISTINE code, which is why it is the family that
//	                        matters here.
//	M4  any control byte    `for ; i < len(b) && b[i] >= 0x20; i++` — the
//	                        PERMISSIVE direction, and the only one here that
//	                        MINTS rather than hides. It reads naturally as
//	                        "stop at a line terminator" but also stops at a
//	                        TAB, ending a comment early and handing the rest
//	                        of a commented-out line to the mount scan.
//
// Measured on 65c5bef10, `go vet` 0 throughout, scored on the exit code of the
// FULL package (no `-run` filter):
//
//	family     before this file (mutant on the pristine terminator)   after
//	--------   ---------------------------------------------------   -----
//	M1         DEAD                                                  DEAD
//	M2         DEAD                                                  DEAD
//	M3         SURVIVOR                                              DEAD
//	M4         SURVIVOR                                              DEAD
//
// The prediction that M1 would be a survivor was WRONG and is recorded here
// rather than quietly corrected: M1 and M2 both let the comment run past a
// `\n`, which is the OVER-masking direction, and that was already dead under
// TestSynth_FastAPI_MountedRouter_AbsentWhenUnresolvable_6414. M3 and M4 are
// the families this file kills at package level. The other thing this file
// changes is not a mutant at all: the pristine terminator itself now fails
// here, which is the bug.
//
// M3 and M4 are the two HALVES of the claim, and they were not found together.
// M3 is "a `\r` DOES end the comment" — the false negative this issue reports.
// M4 is "and nothing else does", the half the first round of this file left
// entirely unobserved: the doc comment asserted the terminator set was exactly
// {`\n`, `\r`} while every fixture here only ever exercised the first half.
// M4 is the strictly worse direction — it MINTS a mount from a commented-out
// line rather than dropping a real one — which is the recurring shape #6659
// records: prose asserts what no test observes, and the permissive direction
// is the one to mutate first. Found by independent review of PR #6667.
//
// #6649's three blank() families (blankcr, blanknl, blankboth) were re-scored
// after the fix and all three are still DEAD — see the note at the end of this
// comment about what the fix took away from them.
//
// PER-CLAUSE DROP ANALYSIS, measured. Guard-dropping cannot be run directly
// here (dropping C1 leaves `h == -1` and the next line panics), so the drops
// were measured with a claim-only probe carrying the degraded fixture and no
// premise guards at all, against each family in turn. SURV means the claim
// still passed, i.e. that family is invisible to that fixture shape:
//
//	fixture degradation                        pristine  M1     M2     M3     fixed
//	----------------------------------------   --------  -----  -----  -----  -----
//	(control) the fixture below                kills     kills  kills  kills  PASS
//	C1 dropped: no `#` at all                  SURV      SURV   SURV   SURV   PASS
//	C2 dropped: the `#` inside a triple quote  kills     kills  kills  kills  FAIL
//	C3 dropped: CRLF instead of a lone `\r`    SURV      kills  kills  SURV   PASS
//	C4 dropped: nothing live after the `\r`    SURV      kills  kills  SURV   PASS
//	C5 dropped: no second `\n`-ended comment   kills     SURV   kills  kills  PASS
//
// C6, added after review, is scored on its own: dropping the tab from the
// `/ghost` line leaves M4 a SURVIVOR (exit 0) while every other family is
// unaffected, since no other family stops at a sub-0x20 byte.
//
// Read off that matrix, each verdict naming the family it is scored against
// because an unqualified "load-bearing" is an incomplete claim (#6632):
//
//	C1  load-bearing against ALL FOUR — with no `#` the comment branch never
//	    runs and the terminator is unobservable.
//	C2  load-bearing against NONE. Its degradation admits no survivor; it makes
//	    the test fail on CORRECT code instead. So it is load-bearing for the
//	    assertion's own correctness, exactly the honest de-claim #6629's C4 made.
//	C3  load-bearing against the PRISTINE terminator and M3 only. M1 and M2 die
//	    on a CRLF fixture too, since they run past the `\n`.
//	C4  same verdict as C3, and for the same reason.
//	C5  load-bearing against M1 only — and only for this test IN ISOLATION. At
//	    package level M1 was already dead via #6414, so C5 adds no kill there.
//	    It is kept because a fixture whose only comment ends in `\r` cannot tell
//	    a comment loop that stops at `\r` from one that stops at both.
//	C6  load-bearing against M4 only, and the only clause here guarding the
//	    permissive direction. It is also the only one whose kill runs through
//	    the direction check (an unexpected mount) rather than a missing one.
//
// Well-formed CRLF is identical under every family above, because the `\r` is
// immediately followed by the `\n` the loop already stopped on. A `\r\n`
// fixture therefore observes NOTHING about this change — the C3 row measures
// exactly that — and it is carried below only as a control asserting the common
// Windows case did not move.
//
// WHAT THE FIX TOOK AWAY, recorded because it is a real cost. The comment loop
// now halts ON a `\r`, so blank() is never handed a carriage return from that
// call site any more. #6649's two caller-A rows (commentCRLF, commentLoneCR)
// could previously observe blank()'s `\r` clause and no longer can; their
// `kills` fields were restated there rather than left overstating. blankcr
// stays DEAD through callers B and C (docstringCRLF, unterminatedCR), verified
// by re-running all three blank() families after this change.
//
// Refs #6648, #6629, #6623, #6652, #6653, #6418, #6614.

// crFixture is the mount file under test. Two mounts, because M1 and M3 are
// killed by different ones:
//
//	/network sits after a LONE `\r` inside a comment — masked away pristine
//	         and under M3, live under the fix. Kills pristine and M3.
//	/people  sits after a comment terminated by a plain `\n` — live pristine
//	         and under M3, masked away under M1 and M2, whose comment loops
//	         run past the `\n` and eat the rest of the file. Kills M1 and M2.
//	/ghost   is COMMENTED OUT behind a TAB, and must never be synthesised at
//	         all. It is the permissive direction: a loop that stops at any
//	         byte below 0x20 — a plausible reading of "stop at a line
//	         terminator" — ends the comment at the tab and mints a mount that
//	         does not exist. Kills M4, via the direction check at the end of
//	         the test rather than via a mount-present assertion.
const crFixture = "from fastapi import FastAPI\n" +
	"from app.api import markets, users\n" +
	"\n" +
	"app = FastAPI()\n" +
	"# mount the market router\rapp.include_router(markets.router, prefix=\"/network\")\n" +
	"# and the people one\n" +
	"app.include_router(users.router, prefix=\"/people\")\n" +
	"# TODO\tapp.include_router(ghost.router, prefix=\"/ghost\")\n"

// crlfControl is crFixture with the lone `\r` replaced by a well-formed `\r\n`.
// Both mounts are live under EVERY behaviour scored above except M2, so this
// row distinguishes nothing on its own — that is the point. It exists to pin
// that the common Windows shape did not move.
var crlfControl = strings.Replace(crFixture, "router\rapp", "router\r\napp", 1)

// TestPythonMaskInertRegions_LoneCREndsComment_6648 is the false negative
// itself, observed end to end: the mount after a lone `\r` must reach the scan.
func TestPythonMaskInertRegions_LoneCREndsComment_6648(t *testing.T) {
	src := crFixture

	// --- premise -----------------------------------------------------------
	// The claim is about the COMMENT branch, so the premise must establish
	// that the branch RUNS on the byte we care about — not merely that the
	// fixture contains a `#`. This function has been pinned by vacuous
	// fixtures repeatedly (#6611, #6615, #6620, #6623), so each clause below
	// names the mutant family it is load-bearing against (#6632).

	// C1, LOAD-BEARING against ALL FOUR families (pristine, M1, M2, M3) —
	// measured: with the `#` replaced by a triple-quoted region every one of
	// them SURVIVES. No `#`, no comment branch, and the terminator under test
	// is simply never reached.
	h := strings.IndexByte(src, '#')
	if h < 0 {
		t.Fatalf("#6648 premise broken: fixture %q contains no `#`, so the comment branch "+
			"never runs and this test is VACUOUS.", src)
	}

	// C2, load-bearing against NO family — measured, and worth stating plainly
	// rather than labelling it a kill it does not make. Putting the `#` inside
	// a triple-quoted region admits no survivor: it makes the test fail on
	// CORRECT code, because the mounts are blanked by the string branch. So
	// this clause guards the ASSERTION's correctness against a later fixture
	// edit, the same honest de-claim #6629's C4 made. It is still the trap that
	// has
	// caught this function most often: a quote EARLIER in the source consumes
	// the `#` in the string branch, so the comment branch is never entered at
	// all and the region is blanked (or skipped) by a different branch under
	// every mutant. The scanner dispatches on the first quote-or-`#` byte, so
	// requiring no quote before `h` is what proves `#` is that byte.
	if strings.ContainsAny(src[:h], "\"'") {
		t.Fatalf("#6648 premise broken: the source before the `#` at %d must contain no quote, "+
			"or the string branch consumes the `#` and the COMMENT branch never runs; got %q. "+
			"This test is now VACUOUS.", h, src[:h])
	}

	// C3, LOAD-BEARING against the PRISTINE terminator and M3 ONLY — measured:
	// with a `\r\n` here both of those SURVIVE, while M1 and M2 still die
	// (they run past the `\n` regardless). The
	// comment must be ended by a LONE `\r`. With a well-formed `\r\n` there,
	// the pristine loop, M3 and the fix all stop at the same byte and the
	// fixture observes nothing whatsoever (see crlfControl below).
	cr := strings.IndexByte(src[h:], '\r')
	if cr < 0 {
		t.Fatalf("#6648 premise broken: the comment starting at %d contains no `\\r`, so the "+
			"pristine loop and the `\\r`-aware loop stop at the same byte. VACUOUS.", h)
	}
	cr += h
	if cr+1 < len(src) && src[cr+1] == '\n' {
		t.Fatalf("#6648 premise broken: the `\\r` at %d is followed by `\\n`, i.e. a well-formed "+
			"CRLF. Every behaviour under test agrees there — the comment ends at that byte "+
			"either way — so this test is VACUOUS. It needs a LONE `\\r`.", cr)
	}
	if nl := strings.IndexByte(src[h:], '\n'); nl >= 0 && nl+h < cr {
		t.Fatalf("#6648 premise broken: a `\\n` at %d ends the comment before the `\\r` at %d, "+
			"so the pristine loop never reaches the `\\r`. VACUOUS.", nl+h, cr)
	}

	// C4, LOAD-BEARING against the PRISTINE terminator and M3 ONLY, same
	// verdict and same reason as C3 — measured with a bare `\r` that ends the
	// line: both SURVIVE, M1 and M2 still die. There must be
	// real code AFTER the lone `\r` on that same physical line — that is the
	// live code being masked away. A trailing `\r` at end of line masks
	// nothing and leaves both behaviours identical.
	tailEnd := strings.IndexByte(src[cr:], '\n')
	if tailEnd < 0 {
		tailEnd = len(src)
	} else {
		tailEnd += cr
	}
	if !strings.Contains(src[cr:tailEnd], "include_router") {
		t.Fatalf("#6648 premise broken: no `include_router` between the lone `\\r` at %d and the "+
			"next `\\n`; got %q. Nothing live is being masked away and this test is VACUOUS.",
			cr, src[cr:tailEnd])
	}

	// C5, LOAD-BEARING against M1 ONLY, and only for this test IN ISOLATION —
	// measured: removing the second comment leaves M1 a SURVIVOR while
	// pristine, M2 and M3 still die. M1's comment loop stops at `\r` and
	// nothing else, so on the lone-`\r` row alone it is indistinguishable from
	// the fix; it needs a comment terminated by a plain `\n` with live code
	// after it to run past. At PACKAGE level M1 (and M2) were already dead via
	// TestSynth_FastAPI_MountedRouter_AbsentWhenUnresolvable_6414, so this
	// clause adds no kill there — it is kept so the fixture cannot silently
	// degrade into one that cannot tell the two loops apart.
	rest := src[tailEnd:]
	h2 := strings.IndexByte(rest, '#')
	if h2 < 0 {
		t.Fatalf("#6648 premise broken: no second `#` comment after the lone-`\\r` line. The " +
			"mutant that stops ONLY at `\\r` behaves exactly like the fix on that line and " +
			"SURVIVES. This test is VACUOUS for M1.")
	}
	nl2 := strings.IndexByte(rest[h2:], '\n')
	if nl2 < 0 {
		t.Fatalf("#6648 premise broken: the second comment is not terminated by `\\n`; got %q.",
			rest[h2:])
	}
	if strings.ContainsAny(rest[h2:h2+nl2], "\r") {
		t.Fatalf("#6648 premise broken: the second comment %q contains a `\\r`, so the `\\r`-only "+
			"mutant stops there too and SURVIVES. VACUOUS for M1.", rest[h2:h2+nl2])
	}
	if !strings.Contains(rest[h2+nl2:], "include_router") {
		t.Fatalf("#6648 premise broken: nothing live after the second comment, so a comment loop " +
			"that runs past `\\n` swallows nothing observable. VACUOUS for M1 and M2.")
	}

	// C6, LOAD-BEARING against M4 — the PERMISSIVE direction, and the only
	// clause here guarding against a mutant that MINTS rather than hides. The
	// doc comment on pythonMaskInertRegions claims the comment terminator set
	// is exactly {`\n`, `\r`}; nothing observed the "and nothing else" half
	// until this clause. A loop written `b[i] >= 0x20` reads naturally as
	// "stop at a line terminator" and also stops at a TAB, ending the comment
	// early and handing the rest of a commented-out line to the mount scan.
	// That is strictly worse than the false negative this issue fixes: a
	// phantom endpoint is asserted, not merely a real one dropped.
	ctrl := -1
	for k := 0; k < len(src); k++ {
		if src[k] < 0x20 && src[k] != '\n' && src[k] != '\r' {
			ctrl = k
			break
		}
	}
	if ctrl < 0 {
		t.Fatalf("#6648 premise broken: the fixture holds no sub-0x20 byte other than `\n` " +
			"and `\r`, so a comment loop that stops at ANY control byte behaves exactly like " +
			"the correct one here and SURVIVES. This test is VACUOUS for M4.")
	}
	ctrlEOL := strings.IndexByte(src[ctrl:], '\n')
	if ctrlEOL < 0 {
		ctrlEOL = len(src)
	} else {
		ctrlEOL += ctrl
	}
	if !strings.Contains(src[ctrl:ctrlEOL], "include_router") {
		t.Fatalf("#6648 premise broken: nothing that looks like a mount follows the control "+
			"byte at %d (%q), so a comment loop stopping there unmasks nothing observable "+
			"and M4 SURVIVES.", ctrl, src[ctrl:ctrlEOL])
	}

	// --- claim, at the masking layer ---------------------------------------
	masked := pythonMaskInertRegions(src)

	if len(masked) != len(src) {
		t.Fatalf("#6648: pythonMaskInertRegions changed length: got %d, want %d", len(masked), len(src))
	}

	// BRANCH-RAN assertion, not a premise: the comment TEXT between `#` and the
	// lone `\r` must be blank. If it is not, the comment branch did not execute
	// on this `#` and everything below observes a different mechanism.
	for k := h; k < cr; k++ {
		if masked[k] != ' ' {
			t.Fatalf("#6648: byte %d of the comment survived masking as %q — the COMMENT BRANCH "+
				"did not run on the `#` at %d, so this test is observing some other mechanism. "+
				"masked %q", k, masked[k], h, masked)
		}
	}
	if masked[cr] != '\r' {
		t.Fatalf("#6648: the `\\r` at %d came out as %q — blank() must preserve it (#6649).",
			cr, masked[cr])
	}

	// The live code after the lone `\r` must SURVIVE masking byte for byte.
	if got, want := masked[cr:tailEnd], src[cr:tailEnd]; got != want {
		t.Errorf("#6648: the code after the lone `\\r` was masked away.\n got  %q\n want %q\n"+
			"A bare carriage return TERMINATES a Python comment under universal newlines, so "+
			"these bytes are live code on the next line, not comment content.", got, want)
	}

	// --- claim, end to end -------------------------------------------------
	// The masking layer feeds the mount scan, so the user-visible consequence
	// is the mount going missing. Assert that directly rather than trusting the
	// intermediate buffer.
	_, res := runDetect(t, "python", "app/main.py", src)
	mounts := fastapiMountSynths(res)

	if _, ok := mounts["/network"]; !ok {
		t.Errorf("#6648: no url_mount_point synthetic for %q — the mount that sits after a lone "+
			"`\\r` inside a comment was masked away and never reached the scan. This is the "+
			"false negative the issue reports: a mount that EXISTS is not reported. got %v",
			"/network", keysOfMounts(mounts))
	}
	if _, ok := mounts["/people"]; !ok {
		t.Errorf("#6648: no url_mount_point synthetic for %q — a comment terminated by a plain "+
			"`\\n` must still END at that `\\n`. If it does not, the comment loop runs on and "+
			"eats the rest of the file (mutants M1 and M2). got %v",
			"/people", keysOfMounts(mounts))
	}

	// Direction check, and the assertion that kills M4. The fix removes
	// masking, so it can only stop discarding real code — it must never MINT a
	// mount. `/ghost` is commented out behind a tab and exists precisely to be
	// absent: a comment loop that stops at any sub-0x20 byte ends the comment
	// at that tab and synthesises it.
	for prefix := range mounts {
		if prefix != "/network" && prefix != "/people" {
			t.Errorf("#6648: unexpected mount %q synthesised. The comment terminator set is "+
				"exactly {`\\n`, `\\r`} and NOTHING else may end a comment — a loop that also "+
				"stops at a tab or another sub-0x20 byte hands a commented-out mount to the "+
				"scan and MINTS an endpoint that does not exist. Less masking must never "+
				"invent one; that asymmetry is why this branch was decidable on its own.", prefix)
		}
	}
}

// TestPythonMaskInertRegions_WellFormedCRLFControl_6648 is the CONTROL. A
// well-formed `\r\n` ends the comment under the pristine terminator and under
// the fix alike, so this row must read IDENTICALLY before and after. It pins
// that the common Windows case did not move; it distinguishes nothing, and
// saying so plainly is better than a label implying a kill it does not make.
func TestPythonMaskInertRegions_WellFormedCRLFControl_6648(t *testing.T) {
	src := crlfControl

	if !strings.Contains(src, "\r\n") {
		t.Fatalf("#6648 control broken: fixture carries no CRLF; it is not a control for the " +
			"Windows shape.")
	}
	if strings.Contains(strings.ReplaceAll(src, "\r\n", ""), "\r") {
		t.Fatalf("#6648 control broken: fixture carries a LONE `\\r`, so it is the divergent " +
			"case and not a control at all.")
	}

	masked := pythonMaskInertRegions(src)

	// The exact masked buffer, byte for byte: comment text blanked, both
	// terminators preserved, every mount left live. This literal is the pin —
	// it must be unchanged by this issue's fix.
	want := "from fastapi import FastAPI\n" +
		"from app.api import markets, users\n" +
		"\n" +
		"app = FastAPI()\n" +
		strings.Repeat(" ", len("# mount the market router")) +
		"\r\napp.include_router(markets.router, prefix=\"/network\")\n" +
		strings.Repeat(" ", len("# and the people one")) +
		"\napp.include_router(users.router, prefix=\"/people\")\n" +
		strings.Repeat(" ", len("# TODO\tapp.include_router(ghost.router, prefix=\"/ghost\")")) +
		"\n"
	if masked != want {
		t.Errorf("#6648 control: the well-formed CRLF shape MOVED.\n got  %q\n want %q\n"+
			"A `\\r` immediately followed by `\\n` ends the comment under both the old and the "+
			"new terminator test, so this buffer must be identical before and after the fix.",
			masked, want)
	}

	_, res := runDetect(t, "python", "app/main.py", src)
	mounts := fastapiMountSynths(res)
	for _, prefix := range []string{"/network", "/people"} {
		if _, ok := mounts[prefix]; !ok {
			t.Errorf("#6648 control: mount %q absent under well-formed CRLF; got %v",
				prefix, keysOfMounts(mounts))
		}
	}
}

// keysOfMounts renders the mount prefixes actually synthesised, so a failure
// says what WAS emitted rather than only what was missing.
func keysOfMounts(m map[string]types.EntityRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
