package engine

import (
	"strings"
	"testing"
)

// TestPythonMaskInertRegions_EscapedNewlineContinuesLiteral_6637 pins the third
// escape shape in the SINGLE-LINE literal scan of pythonMaskInertRegions: a
// backslash that escapes a NEWLINE, i.e. a Python line continuation inside a
// string.
//
// WHICH BRANCH. There are two byte-identical escape skips in this function and
// the issue's diff does not say which one it means:
//
//	http_endpoint_synthesis.go:3202-3205  the TRIPLE-QUOTED branch's skip
//	http_endpoint_synthesis.go:3226-3229  the SINGLE-LINE literal scan's skip  <- THIS ROW
//
// (line numbers derived from `git show 1552ec034:internal/engine/http_endpoint_synthesis.go`).
// The one under test here is the single-line one, and c2 below is what makes
// that a proof rather than an intention: the triple-quoted branch is entered
// only on three consecutive identical quote bytes, so a fixture that contains
// no `"""` and no `”'` anywhere CANNOT reach :3202 on any dispatch. The
// #6653 terminator break at :3230-3232 and the clamps at :3238-3240 /
// :3245-3247 are deliberately untouched.
//
// WHY THE BRANCH DEMONSTRABLY RUNS. The fixture's literal opens on line 1 and
// closes on line 2. The scan breaks on the opening quote character or on `\n`,
// so the ONLY way it can reach the closing quote on line 2 is by the escape
// skip stepping over the `\n`. If the skip does not run — or runs but does not
// consume the newline — the literal is unterminated at end of line and #6614
// blanks its span, so the byte-exact identity asserted below fails. The
// assertion is therefore itself the execution witness, not a separate claim.
//
// THE DEFECT, measured before this row was written. Mutant M1:
//
//   - if b[j] == '\\' {
//   - if b[j] == '\\' && j+1 < len(b) && (b[j+1] == c || b[j+1] == '\\') {
//
// i.e. honour the escape only for the two shapes #6624 already pins.
//
//	in       `x = "a\` + LF + `b" and app.include_router(r, prefix=p)`
//	pristine unchanged
//	M1       "x =    \nb                                     "
//
// Under M1 the literal is read as unterminated at the newline, its tail is
// blanked (#6614), and then the `"` on line 2 opens a NEW literal that blanks
// the rest of that line — losing the include_router mount entirely. That is
// the OVER-masking direction: a false negative, and a repo missing an endpoint
// looks exactly like a repo that never had one.
//
// MUTANT FAMILY, scored separately against the package suite at 1552ec034
// BEFORE this file existed (go vet 0 for each; `go test -count=1
// ./internal/engine/`, full package):
//
//	M1  escape only when it escapes the quote or a backslash   ALIVE  -> killed here
//	M2  escape branch deleted outright                         already DEAD (#6624)
//	M3  `j += 1`                                               already DEAD (#6624)
//	M4  escape unless the escaped byte is `\n`                 ALIVE  -> killed here
//	M5  `j += 3`                                               already DEAD (#6624)
//
// Three of the five predicted mutants were already dead, all three in a
// direction #6624 pinned.
//
// WHAT THIS ROW DOES NOT PIN — read this before adding to the family above.
// M1..M5 all NARROW what counts as an escape, and an earlier draft of this
// comment claimed that was the whole family, on the grounds that the pristine
// predicate is "unconditional". That claim was FALSE and is recorded here
// because the next reader will otherwise inherit it: `b[j] == '\\'` is
// unconditional in its OPERAND, not in its CHARACTER SET, and
// `b[j] == '\\' || b[j] == X` is strictly more permissive for every X. Three
// such mutants are measured SURVIVORS against the full package including this
// row (go vet 0 each):
//
//	W1  `if b[j] == '\\' || b[j] == '/' {`   ALIVE
//	W2  `if b[j] == '\\' || b[j] == ' ' {`   ALIVE
//	W3  `if b[j] == '\\' || b[j] == '=' {`   ALIVE
//
// W1's witness is a canonical FastAPI line and its failure is the SAME
// over-masking false negative this row exists to pin, reached from the other
// side:
//
//	in       `app.include_router(r, prefix="/")`
//	pristine `app.include_router(r, prefix="/")`
//	W1       `app.include_router(r, prefix=    `
//
// The `/` before the closing quote is eaten as an escape, the real terminator
// is skipped, the literal reads as unterminated, #6614 blanks the tail, the
// mount is gone. Tracked as #6680; no fixture is manufactured for it here,
// because what the escape SET should be is a decision and not a defect to be
// patched into a passing test.
//
// The sweep below did not find them, and widening its alphabet would not have
// helped: `' '` and `'='` are ALREADY in it, and 149802 and 150152 of its own
// inputs respectively distinguish W2 and W3 from pristine. A sweep varies the
// INPUT against a FIXED mutant, so exhausting the input space says nothing
// about the mutant space (#6659). Vary both, and score the WIDENING direction
// as its own axis rather than assuming the narrowing family is all of it.
//
// BRANCH ATTRIBUTION rests on c2's CONSTRUCTION, not on a measurement. The
// triple-quoted branch is entered only when three consecutive identical quote
// bytes appear, so a fixture containing neither `"""` nor `”'` cannot execute
// the twin's escape skip on ANY dispatch — that is a property of the fixture,
// checkable by reading it, and it is what c2 enforces.
//
// CORROBORATION ONLY, explicitly de-claimed as evidence: applying M1's exact
// conditional to the twin at :3202-3205 instead leaves the full package green,
// this row included. That reproduces, but it proves nothing on its own — an
// independent implementation could not distinguish that twin mutant from
// pristine on any input tried, so its survival is consistent with the mutant
// being equivalent rather than with this row being well targeted. The
// construction argument above is what carries the claim.
//
// RECORDED EQUIVALENT — do not re-score as DEAD. Adding a bounds guard alone,
// `if b[j] == '\\' && j+1 < len(b)`, is equivalent for ALL inputs, by
// construction rather than by sweep count. The two differ only on a trailing
// backslash at end of buffer. There pristine sets j = len(b)+1; the mutant
// falls through to the terminator test, where b[j] is `\\` — neither c nor
// `\n` — so it takes `j++` and reaches len(b). Both then exit the loop with
// j >= len(b), both fail `j < len(b) && b[j] == c`, both clamp j to len(b),
// and both blank exactly i..len(b). No input distinguishes them, so no fixture
// can kill it and none should be built.
//
// FIXTURE and GUARD, brute forced rather than hand-picked: every string of
// length <= 7 over {x, ' ', '=', '"', '\”, '#', '\\', 'a', '\n'} (5,380,840
// inputs) was scored for guard-pass x survival of M1 and M4. 8336 inputs
// satisfy the guard below and NONE of them leaves either mutant alive, so no
// edit to this fixture that still satisfies its own premise can make the row
// vacuous. (That statement is about M1 and M4 only — see W1..W3 above for the
// direction this sweep could not see at all.)
//
// The per-clause annotations name TWO DIFFERENT jobs, because the drop
// analysis showed the clauses do not all do the same one. The metric is
// mutant-output == pristine-output, split by whether pristine itself still
// satisfies the identity assertion:
//
//   - VACUITY GUARD. Dropping it admits fixtures that leave this row GREEN
//     while the mutant survives — it passes and observes nothing. c5 is the
//     ONLY clause of this kind: 66642 such fixtures.
//   - LOUD-FAILURE GUARD. Dropping it admits fixtures on which the mutant
//     survives but pristine ALSO fails the identity assertion, so the row goes
//     red on correct code. c0 (4166), c4 (1516) and c6 (1120) are these, and
//     each admits ZERO vacuous fixtures. They buy a comprehensible failure
//     rather than coverage, and "N survivors admitted" would overstate them.
//
// c1, c2 and c7 admit nothing in either category. c2 is branch identity, above,
// and is not claimed to do kill work.
//
// Refs #6637; siblings #6624 (escaped quote, escaped backslash), #6418
// (terminated literals stay intact), #6614 (unterminated tails are blanked),
// #6649/#6650 (blank() preserves `\n` and `\r`). Out of scope and separately
// tracked: #6653, #6654, #6655.
func TestPythonMaskInertRegions_EscapedNewlineContinuesLiteral_6637(t *testing.T) {
	// `x = "a\` NEWLINE `b" and app.include_router(r, prefix=p)` — a real
	// Python line continuation inside a single-line string literal.
	const src = "x = \"a\\\nb\" and app.include_router(r, prefix=p)"

	// c0 LOUD-FAILURE GUARD (4166 admitted if dropped, none of them vacuous —
	// pristine fails the identity assertion on every one). The scanner
	// dispatches on the first `#`, `"` or `'`, and a `#` goes to the COMMENT
	// branch, which eats the line without ever reaching the escape skip.
	q := strings.IndexAny(src, "#\"'")
	if q < 0 || src[q] == '#' {
		t.Fatalf("#6637 premise broken: the first quote-or-comment character in %q must be a QUOTE — "+
			"a `#` dispatches to the comment branch and the single-line escape skip never RUNS. "+
			"This row is now VACUOUS.", src)
	}
	quote := src[q]

	// c1 DECORATIVE: admits nothing in either category (c2 and c6 already
	// reject every such fixture in the sweep space). Kept because it names the
	// branch under test — the single-line scan runs only when the triple-quote
	// probe fails.
	if q+1 >= len(src) || src[q+1] == quote {
		t.Fatalf("#6637 premise broken: the byte after the opening quote must exist and DIFFER from it, "+
			"so the triple-quote probe fails and the SINGLE-LINE branch is the one that runs; got %q. "+
			"This row is now VACUOUS.", src)
	}

	// c2 BRANCH IDENTITY, not a kill guard (admits nothing in either category —
	// it is not doing kill work and is not claimed to). The triple-quoted
	// branch at :3202 is entered only on three consecutive identical quote
	// bytes, so a fixture containing neither `"""` nor `'''` cannot execute
	// its escape skip on ANY dispatch. Without this clause the row could be
	// killing the twin at :3202 and reporting it as :3226.
	if strings.Contains(src, `"""`) || strings.Contains(src, "'''") {
		t.Fatalf("#6637 premise broken: fixture %q contains a triple-quote run, so the identical escape "+
			"skip in the TRIPLE-QUOTED branch (:3202-3205) becomes reachable and this row can no longer "+
			"say which branch it observed. This row is now VACUOUS.", src)
	}
	body := src[q+1:]

	// c3 is a precondition rather than an independently droppable check: with
	// no backslash there is no escape offset to derive, so the clauses below
	// are undefined and the branch does not execute at all.
	e := strings.IndexByte(body, '\\')
	if e < 0 {
		t.Fatalf("#6637 premise broken: fixture %q contains no backslash inside the literal, so the "+
			"escape skip — the whole subject of this row — never executes. This row is VACUOUS.", src)
	}

	// c4 LOUD-FAILURE GUARD (1516 admitted, none vacuous). Containing a
	// backslash is not enough: the scan
	// must REACH it. The scan breaks on the quote character or on a newline,
	// so either one standing between the opener and the backslash ends the
	// literal first and the escape skip is never evaluated.
	if bad := strings.IndexAny(body[:e], string(quote)+"\n"); bad >= 0 {
		t.Fatalf("#6637 premise broken: no %q and no newline may stand between the opening quote and the "+
			"backslash, or the scan BREAKS before the escape skip is evaluated; found %q at body offset "+
			"%d in %q. This row is now VACUOUS.", quote, body[bad], bad, src)
	}

	// c5 VACUITY GUARD — the ONLY clause here that is one (66642 silently-green
	// fixtures admitted if dropped), and the clause that distinguishes this row from
	// both #6624 rows: the escaped byte must be a NEWLINE. M1 handles an
	// escaped quote and an escaped backslash exactly like pristine, and an
	// escaped ordinary byte is indistinguishable too (a one-byte and a
	// two-byte skip converge on it, since a backslash is neither the quote nor
	// a newline). The newline is the ONLY byte on which M1 and M4 differ from
	// pristine.
	if e+1 >= len(body) || body[e+1] != '\n' {
		t.Fatalf("#6637 premise broken: the backslash must escape a NEWLINE — escaping the quote or a "+
			"backslash is #6624's shape, on which these mutants behave exactly like pristine, and "+
			"escaping an ordinary byte observes nothing; fixture %q. This row is now VACUOUS.", src)
	}
	rest := body[e+2:]

	// c6 LOUD-FAILURE GUARD (1120 admitted, none vacuous). The literal must
	// TERMINATE on the continuation
	// line, with nothing between the continuation and the closing quote that
	// could end the scan first. Drop this and a fixture whose second line ends
	// in another newline is admitted: pristine finds THAT newline unterminated
	// and blanks the span, the mutants blank from the first newline, and both
	// reach the same output. The row does not go quiet there — it goes RED,
	// because pristine no longer satisfies the identity assertion either. An
	// earlier draft of this file had exactly that hole and described it as
	// vacuity; the drop analysis says otherwise, and 1120 such fixtures fail
	// loudly on correct code rather than passing on broken code. This clause is
	// also what makes the literal terminated, so #6418 says the pristine output
	// is the input unchanged.
	k := strings.IndexByte(rest, quote)
	if k < 0 || strings.IndexAny(rest[:k], "\\\n") >= 0 {
		t.Fatalf("#6637 premise broken: the continued literal must CLOSE on the continuation line with no "+
			"further backslash or newline before its closing %q — otherwise pristine treats it as "+
			"unterminated too and blanks the same span the mutants do; fixture %q. "+
			"This row is now VACUOUS.", quote, src)
	}

	// c7 DECORATIVE: admits nothing in either category. Nothing after the closing quote may
	// open another inert region and no newline may precede the opener, so the
	// only inert region in the fixture is the literal under test — which is
	// what licenses the byte-exact identity assertion below rather than a
	// substring check.
	if strings.ContainsAny(rest[k+1:], "#\"'") || strings.Contains(src[:q], "\n") {
		t.Fatalf("#6637 premise broken: nothing after the closing quote may open another literal or "+
			"comment, and no newline may precede the opening quote, or the identity assertion below "+
			"would be observing that other region instead: %q. This row is now VACUOUS.", src)
	}

	// A backslash-escaped newline is a line CONTINUATION: the literal is still
	// open on line 2 and closes there, so it is TERMINATED and #6418 leaves it
	// intact byte for byte — including the `include_router` mount that follows
	// the closing quote.
	if got := pythonMaskInertRegions(src); got != src {
		t.Fatalf("#6637: a backslash-escaped newline inside a single-line literal changed the masked "+
			"copy: got %q, want %q. The escape skip in the SINGLE-LINE branch "+
			"(http_endpoint_synthesis.go:3226-3229, NOT the triple-quoted twin at :3202-3205) must step "+
			"over the byte it escapes UNCONDITIONALLY (`j += 2`). An escape honoured only for the quote "+
			"and the backslash — the two shapes #6624 pins — leaves the newline to BREAK the scan: the "+
			"literal reads as unterminated, #6614 blanks its tail, and the closing quote on the next "+
			"line then opens a fresh unterminated literal that blanks the rest of that line, dropping a "+
			"live app.include_router mount. That is the OVER-masking direction, a false negative, and it "+
			"is invisible downstream because a missing endpoint looks like an endpoint that never "+
			"existed.", got, src)
	}
}
