package engine

import (
	"strings"
	"testing"
)

// TestPythonMaskInertRegions_EscapedQuoteDoesNotTerminateLiteral_6624 pins the
// backslash-escape skip in the SINGLE-LINE literal branch of
// pythonMaskInertRegions. `j += 2` steps over the byte a backslash escapes;
// nothing in the suite observed how far it steps, so `j += 1` (and, exactly
// equivalently, deleting the escape branch outright) passed `go vet` at 0 and
// the full package at exit 0.
//
// The defect it hides is not a panic — it is a masking difference, measured
// before this test was written:
//
//	in       `x = "a\"b" and app.include_router(r, prefix="/x")`
//	pristine `x = "a\"b" and app.include_router(r, prefix="/x")`   (intact, #6418)
//	j += 1   `x = "a\"b" and app.include_router(r, prefix="/x  `
//
// With a one-byte skip the escaped quote is read as a TERMINATOR. The literal
// closes early, the scanner re-enters the quote branch on the real closing
// quote, that one is now unterminated, and the #6614 blanking path eats the
// rest of the line — including the `prefix=` value the mount scan has to read.
// So an under-skip corrupts well-formed code, and an over-skip (`j += 3`)
// steps past a terminator that immediately follows an escaped quote and blanks
// a literal that was closed. Both directions are pinned below.
//
// FIXTURE. `x = "\""` — a Python assignment of one double-quote character.
// Chosen by brute force, not by hand: every string of length <= 7 over
// {x, ' ', '=', '"', '\”, '#', '\\', 'a', '\n'} was scored for guard-pass x
// mutant-survival against `j += 1`, `j += 3`, and the escape branch deleted.
// 4080 inputs pass the premise below and NONE of them leaves any of the three
// alive, so this row cannot be defeated by an unlucky fixture edit that still
// satisfies its guard.
//
// That sweep was COMPLETE OVER ITS INPUT SPACE AND SHORT OVER ITS MUTANT
// SPACE, which review caught: re-run against a fourth mutant — the escape
// skipping only when it escapes the quote — all 4080 of those inputs leave it
// alive. A sweep varies the input against a FIXED mutant set, so it answers
// "does the guard admit a defeating input" and says nothing about "does the
// fixture pin the mutant family". The fourth shape is pinned by the sibling
// row below; c5 here is what keeps it out of reach (drop c5 and 200 of the
// admitted fixtures do kill it).
//
// PREMISE. The rows in TestPythonMaskInertRegions_MustNotPanic cannot be
// copied here: two of them FORBID a backslash outright and a third requires an
// ODD trailing run of them, because they sit on index bounds that an escape
// skip would step off. This row needs the opposite — the escape must be the
// thing that RUNS — so its guard is derived from scratch and each clause is
// annotated with the drop analysis that measured it.
//
// Refs #6624; siblings #6418 (terminated literals stay intact), #6614
// (unterminated tails are blanked), #6612/#6617 (the index bounds).
func TestPythonMaskInertRegions_EscapedQuoteDoesNotTerminateLiteral_6624(t *testing.T) {
	const src = `x = "\""`

	// c0 LOAD-BEARING. The scanner dispatches on the first `#`, `"` or `'`, and
	// `#` goes to the COMMENT branch, which eats the line without ever reaching
	// the escape skip. `#\##` satisfies every other clause here and leaves the
	// mutant alive at exit 0 (2040 such survivors in the sweep).
	q := strings.IndexAny(src, "#\"'")
	if q < 0 || src[q] == '#' {
		t.Fatalf("#6624 premise broken: the first quote-or-comment character in %q must be a QUOTE — "+
			"a `#` dispatches to the comment branch and the escape skip never RUNS. This row is now VACUOUS.", src)
	}
	quote := src[q]

	// c1 DECORATIVE. The single-line branch runs only when the triple-quote
	// probe fails, which needs the byte after the opener to differ from it. The
	// sweep found no fixture this clause alone rejects — c3 already rejects
	// every one of them, since a repeated opening quote is a quote standing
	// before the backslash. Kept to name the branch under test.
	if q+1 >= len(src) || src[q+1] == quote {
		t.Fatalf("#6624 premise broken: the byte after the opening quote must exist and DIFFER from it, "+
			"so the triple-quote probe fails and the SINGLE-LINE branch is the one that runs; got %q. "+
			"This row is now VACUOUS.", src)
	}
	body := src[q+1:]

	// c2 is a precondition of the clauses below rather than an independently
	// droppable check: without a backslash there is no escape offset to derive,
	// so it cannot be scored. It is what makes the branch exist at all.
	e := strings.IndexByte(body, '\\')
	if e < 0 {
		t.Fatalf("#6624 premise broken: fixture %q contains no backslash inside the literal, so the escape "+
			"skip — the whole subject of this row — never executes. This row is VACUOUS.", src)
	}

	// c3 LOAD-BEARING. Containing a backslash is not enough; the scan must
	// REACH it. The scan breaks on the quote character or on a newline, so
	// either one standing between the opener and the backslash ends the literal
	// first. `"\n\""` passes every other clause and leaves the mutant alive
	// (1080 such survivors) — this is the "contains a backslash" trap that has
	// cost this function four fixtures.
	if bad := strings.IndexAny(body[:e], string(quote)+"\n"); bad >= 0 {
		t.Fatalf("#6624 premise broken: no %q and no newline may stand between the opening quote and the "+
			"backslash, or the scan BREAKS before the escape skip is ever evaluated; found %q at body "+
			"offset %d in %q. This row is now VACUOUS.", quote, body[bad], bad, src)
	}

	// c4 LOAD-BEARING against the UNDER-skip family (`j += 1`, and the escape
	// branch deleted — analytically the same mutant, since a backslash is
	// neither the quote nor a newline and falls through to `j++`). The escaped
	// byte must be one the scan would otherwise stop on. `"\x"` escapes an
	// ordinary letter: skipping one byte or two lands on the same terminator,
	// and 28560 such fixtures leave the under-skip alive. DECORATIVE against
	// `j += 3`, which c5 catches instead.
	if e+1 >= len(body) || body[e+1] != quote {
		t.Fatalf("#6624 premise broken: the backslash must escape the QUOTE character %q — escaping an "+
			"ordinary byte makes a one-byte and a two-byte skip land on the same terminator and observes "+
			"nothing; fixture %q. This row is now VACUOUS.", quote, src)
	}
	rest := body[e+2:]

	// c5 LOAD-BEARING against the OVER-skip family (`j += 3`). The terminator
	// must sit IMMEDIATELY after the escaped quote, so a skip of three steps
	// past it and runs the literal to the end of the line. Move it one byte
	// further out — `"\"x` — and 30640 fixtures leave `j += 3` alive while
	// still killing the under-skip. DECORATIVE against the under-skip family.
	// It is also what makes the literal TERMINATED, so #6418 says the pristine
	// output is the input unchanged.
	if len(rest) == 0 || rest[0] != quote {
		t.Fatalf("#6624 premise broken: the byte after the escaped quote must be the CLOSING quote %q, so "+
			"the literal terminates immediately and an over-skip steps past the terminator; got %q. This "+
			"row is now VACUOUS.", quote, src)
	}

	// c6 DECORATIVE, c7 DECORATIVE. Nothing after the closing quote may open
	// another inert region, and no newline may precede the opener, so the
	// whole fixture is one line whose only inert region is the literal under
	// test and the expected output is the input unchanged. The sweep found no
	// fixture either clause alone rejects; both are kept because they are what
	// licenses the byte-exact identity assertion below.
	if strings.ContainsAny(rest[1:], "#\"'") {
		t.Fatalf("#6624 premise broken: nothing after the closing quote may open another literal or "+
			"comment, or the identity assertion below would be observing that region instead: %q. "+
			"This row is now VACUOUS.", src)
	}
	if strings.Contains(src[:q], "\n") {
		t.Fatalf("#6624 premise broken: no newline may precede the opening quote; fixture %q must be a "+
			"single line. This row is now VACUOUS.", src)
	}

	// A TERMINATED single-line literal is left INTACT — that is #6418, and it
	// is load-bearing because `prefix="/x"` lives inside one. An escaped quote
	// does not terminate it, so the identity holds byte for byte. Both the
	// under-skip and the over-skip break this identity by closing the literal
	// at the wrong byte and blanking from there.
	if got := pythonMaskInertRegions(src); got != src {
		t.Fatalf("#6624: an escaped quote inside a terminated single-line literal changed the masked copy: "+
			"got %q, want %q. The backslash escape must skip the byte it escapes (`j += 2`): a shorter skip "+
			"reads the escaped quote as a TERMINATOR, closes the literal early and leaves the real closing "+
			"quote to open an unterminated one whose tail is then blanked (#6614) — which eats live code, "+
			"including the `prefix=` value the mount scan reads (#6418). A longer skip steps past the real "+
			"terminator and blanks a literal that was closed.", got, src)
	}
}

// TestPythonMaskInertRegions_EscapedBackslashDoesNotEatTerminator_6624 is the
// mirror of the row above and was added AFTER review, which found a fourth
// mutant shape the first row cannot see:
//
//	pristine  if b[j] == '\\' {
//	mutant    if b[j] == '\\' && j+1 < len(b) && b[j+1] == c {
//
// i.e. skip only when the backslash escapes the QUOTE character. `go vet` 0,
// full package exit 0 — a survivor against the row above, and *necessarily*
// so: of the 4080 inputs that satisfy that row's guard, ALL 4080 leave this
// mutant alive. No edit to its fixture could have caught it, because its c4
// pins the escaped byte to the quote and this mutant differs from pristine
// only when the escaped byte is NOT the quote. The mutant space, not the
// input space, was what the first row's sweep left short.
//
// The defect, measured before this row was written:
//
//	in       `x = "a\\" and app.include_router(r, prefix=p)`
//	pristine unchanged
//	mutant   `x =                                          ` (blanked from the quote)
//
// An escaped BACKSLASH immediately before the closing quote: the mutant does
// not skip it (the next byte is a quote-escape it is not looking at from that
// index), reaches the second backslash, sees the closing quote after it and
// skips OVER the real terminator. The literal now reads as unterminated and
// #6614's blanking eats the rest of the line — losing a legitimate
// `include_router` mount. That is the OVER-masking direction: a false
// negative, which is the harder failure to notice.
//
// FIXTURE. `x = "\\"` — an assignment of one backslash character. Brute
// forced over the same alphabet and length bound as the row above, scored now
// against FOUR mutants: 4080 inputs satisfy this row's guard, and none of them
// leaves `j += 3` or the conditional-escape mutant alive. All 4080 leave
// `j += 1` (and the deleted branch) alive — that direction is the other row's
// job. The two guards are disjoint by construction: c4 there requires the
// escaped byte to BE the quote, c4 here requires it to be a backslash.
//
// Refs #6624.
func TestPythonMaskInertRegions_EscapedBackslashDoesNotEatTerminator_6624(t *testing.T) {
	const src = `x = "\\"`

	// c0 LOAD-BEARING (`j += 3`, conditional-escape). A `#` first dispatches to
	// the comment branch and the escape skip never runs; 2040 such fixtures
	// leave each of those two alive.
	q := strings.IndexAny(src, "#\"'")
	if q < 0 || src[q] == '#' {
		t.Fatalf("#6624 premise broken: the first quote-or-comment character in %q must be a QUOTE — "+
			"a `#` dispatches to the comment branch and the escape skip never RUNS. This row is now VACUOUS.", src)
	}
	quote := src[q]

	// c1 DECORATIVE. Names the single-line branch (the triple-quote probe must
	// fail); the sweep found no fixture this clause alone rejects, because c3
	// already rejects a repeated opening quote.
	if q+1 >= len(src) || src[q+1] == quote {
		t.Fatalf("#6624 premise broken: the byte after the opening quote must exist and DIFFER from it, "+
			"so the triple-quote probe fails and the SINGLE-LINE branch is the one that runs; got %q. "+
			"This row is now VACUOUS.", src)
	}
	body := src[q+1:]

	// c2, as in the row above, is the precondition that makes the escape
	// branch exist at all rather than an independently droppable check.
	e := strings.IndexByte(body, '\\')
	if e < 0 {
		t.Fatalf("#6624 premise broken: fixture %q contains no backslash inside the literal, so the escape "+
			"skip — the whole subject of this row — never executes. This row is VACUOUS.", src)
	}

	// c3 LOAD-BEARING (`j += 3`, conditional-escape). The scan must REACH the
	// backslash: a quote or a newline before it ends the literal first. 1080
	// fixtures such as `"\n\\"` pass every other clause and leave both alive.
	if bad := strings.IndexAny(body[:e], string(quote)+"\n"); bad >= 0 {
		t.Fatalf("#6624 premise broken: no %q and no newline may stand between the opening quote and the "+
			"backslash, or the scan BREAKS before the escape skip is ever evaluated; found %q at body "+
			"offset %d in %q. This row is now VACUOUS.", quote, body[bad], bad, src)
	}

	// c4 LOAD-BEARING against the conditional-escape mutant, and the clause
	// that makes this row the mirror of the one above: the escaped byte must be
	// a BACKSLASH, not the quote. Escaping an ordinary byte (`"\x"`) leaves
	// 28560 conditional-escape survivors; escaping the quote is the other row's
	// shape and this mutant is identical to pristine there. DECORATIVE against
	// `j += 3`, which c5 catches.
	if e+1 >= len(body) || body[e+1] != '\\' {
		t.Fatalf("#6624 premise broken: the backslash must escape a BACKSLASH — if it escapes the quote "+
			"the conditional-escape mutant behaves exactly like pristine and this row observes NOTHING; "+
			"fixture %q. This row is now VACUOUS.", src)
	}
	rest := body[e+2:]

	// c5 LOAD-BEARING against both. The closing quote must sit IMMEDIATELY
	// after the escaped backslash — that is what the conditional-escape mutant
	// consumes as if it were escaped, and what `j += 3` steps past. Move it one
	// byte out (`"\\x`) and 32440 fixtures leave the conditional-escape mutant
	// alive and 30640 leave `j += 3` alive.
	if len(rest) == 0 || rest[0] != quote {
		t.Fatalf("#6624 premise broken: the byte after the escaped backslash must be the CLOSING quote %q, "+
			"so the mutant's look-ahead sees a quote it treats as escaped and eats the real terminator; "+
			"got %q. This row is now VACUOUS.", quote, src)
	}

	// c6 LOAD-BEARING, narrowly — and unlike its counterpart in the row above,
	// which the four-mutant sweep confirms is decorative there. Dropping it
	// here admits exactly two fixtures (`"\\"\""` and its single-quote twin)
	// that leave the conditional-escape mutant alive: a second escaped quote
	// after the close re-opens a region that masks the difference. c7 is
	// DECORATIVE (no fixture it alone rejects); both are what license the
	// byte-exact identity assertion below.
	if strings.ContainsAny(rest[1:], "#\"'") {
		t.Fatalf("#6624 premise broken: nothing after the closing quote may open another literal or "+
			"comment — two such fixtures leave the conditional-escape mutant alive: %q. "+
			"This row is now VACUOUS.", src)
	}
	if strings.Contains(src[:q], "\n") {
		t.Fatalf("#6624 premise broken: no newline may precede the opening quote; fixture %q must be a "+
			"single line. This row is now VACUOUS.", src)
	}

	// The literal is TERMINATED — the escaped backslash is content, the quote
	// after it is the real closer — so #6418 leaves it intact byte for byte.
	if got := pythonMaskInertRegions(src); got != src {
		t.Fatalf("#6624: an escaped backslash before the closing quote changed the masked copy: got %q, "+
			"want %q. The escape must skip the byte it escapes UNCONDITIONALLY (`j += 2`): a skip that "+
			"fires only when the escaped byte is the quote leaves the second backslash to be read as an "+
			"escape of the real TERMINATOR, so a closed literal reads as unterminated and #6614's "+
			"blanking eats the rest of the line — dropping a live `include_router` mount (#6418). "+
			"A skip of three steps past the terminator the same way.", got, src)
	}
}
