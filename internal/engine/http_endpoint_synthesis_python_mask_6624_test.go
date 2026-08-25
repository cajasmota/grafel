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
