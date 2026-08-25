package engine

import (
	"strings"
	"testing"
)

// pythonMaskInertRegions' `#`-comment branch blanks the rest of the line
// unconditionally. That is a CONTENT claim — what the masker PRODUCES for a
// commented-out line — and nothing in this package observed it: a mutant that
// stops the comment scan at a quote, letting the quoted span escape into the
// single-line-string branch (which SKIPS rather than blanks), left `go vet` at
// 0 and the full package suite at exit 0. Under that mutant a commented-out
// `app.include_router(..., prefix="/ghost")` keeps its literal live and the
// value reaches the mount scan.
//
// This lives beside TestPythonMaskInertRegions_MustNotPanic (#6617) rather than
// as a fifth row IN it deliberately: that table pins "must not panic" for four
// look-ahead BOUNDS and asserts only that the output length is unchanged. A row
// asserting produced bytes would have to smuggle a second, differently-shaped
// assertion through a table whose whole contract is the crash. Same function,
// different claim, own home.
//
// Guard clauses are annotated from a brute-force per-clause drop analysis over
// every string of length <= 7 on the alphabet {x, space, =, ", ', #, \, a, \n,
// \r} (11,111,111 inputs), scoring guard-pass x mutant-survival and guard-pass x
// pristine-failure. With every clause on: ZERO inputs pass the guard and leave
// the mutant alive, and ZERO pass the guard and fail on pristine. Each clause
// below carries the witness its removal admits.
//
// Refs #6623. Out of scope and separately tracked: #6418 (a terminated literal
// OUTSIDE a comment still mints a mount — a known limitation, not touched
// here), #6614 (the unterminated literal's own tail), #6624 (the `j += 2`
// backslash skip).
func TestPythonMaskInertRegions_QuotedSpanInsideCommentIsBlanked(t *testing.T) {
	// A commented-out mount, the shape that makes the mutant's effect concrete:
	// under it `"/ghost"` survives masking and is read as a live prefix.
	const src = `# app.include_router(legacy, prefix="/ghost")`

	// --- premise -----------------------------------------------------------
	// The claim is about the COMMENT branch, so the premise must establish that
	// the branch RUNS, not merely that the fixture contains a `#`.

	// LOAD-BEARING. Without it any fixture with no `#` at all — `x` — leaves an
	// empty region and the assertion below observes nothing (5,380,840 such
	// survivors in the enumeration).
	h := strings.IndexByte(src, '#')
	if h < 0 {
		t.Fatalf("#6623 premise broken: fixture %q contains no `#`, so the comment branch "+
			"never runs and this test is VACUOUS.", src)
	}

	// LOAD-BEARING, and the trap that has caught this function three times
	// (#6611, #6615, #6620): a quote EARLIER in the source consumes the `#` in
	// the string branch, so the comment branch is never entered. `xx"""#"`
	// passes every other clause here and leaves the mutant alive; `xxxx"#"`
	// passes them and fails on PRISTINE. The scanner dispatches on the first
	// quote-or-comment byte, so requiring the prefix to hold no quote is what
	// proves `#` is that byte and the comment branch is what executes.
	if strings.ContainsAny(src[:h], "\"'") {
		t.Fatalf("#6623 premise broken: the source before the `#` at %d must contain no quote, "+
			"or the string branch consumes the `#` and the COMMENT branch never runs; got %q. "+
			"This test is now VACUOUS.", h, src[:h])
	}

	// The region under test: the comment, up to its newline or EOF.
	eol := strings.IndexByte(src[h:], '\n')
	if eol < 0 {
		eol = len(src)
	} else {
		eol += h
	}
	region := src[h:eol]

	// LOAD-BEARING. A comment with no quote in it — `xxxxxx#` — is blanked
	// identically by both versions; the mutant only diverges when there is a
	// quote to escape through (1,314,517 such survivors).
	rel := strings.IndexAny(region[1:], "\"'")
	if rel < 0 {
		t.Fatalf("#6623 premise broken: the comment %q contains no quote, so there is nothing "+
			"for a mutant to let escape into the string branch and both versions blank it "+
			"identically. This test is now VACUOUS.", region)
	}
	p := h + 1 + rel

	// LOAD-BEARING. If the first quote in the comment opens a TRIPLE-quoted
	// literal, the escaping branch is the triple-quote one, which BLANKS its
	// span — so the region comes out blank anyway and the mutant survives.
	// `xxx#"""` passes every other clause here (7,712-8,838 such survivors).
	if p+2 < len(src) && src[p+1] == src[p] && src[p+2] == src[p] {
		t.Fatalf("#6623 premise broken: the first quote in the comment (offset %d) opens a "+
			"TRIPLE-quoted literal; that branch blanks its span, so the region is masked either "+
			"way and this test is VACUOUS. Use a single-quote literal inside the comment; "+
			"fixture %q.", p, src)
	}

	// LOAD-BEARING for the assertion's own correctness rather than for the
	// mutant: blank() preserves `\r` as well as `\n`, so a CR inside the comment
	// — `xxxx#"\r` — makes the all-spaces assertion below fail on PRISTINE
	// (532,698 such inputs). It admits no mutant survivor.
	if strings.Contains(region, "\r") {
		t.Fatalf("#6623 premise broken: the comment %q contains a carriage return, which "+
			"blank() preserves; the assertion below would fail on correct code. Remove it.", region)
	}

	// --- claim -------------------------------------------------------------
	got := pythonMaskInertRegions(src)

	if len(got) != len(src) {
		t.Fatalf("#6623: pythonMaskInertRegions changed length: got %d (%q), want %d (%q)",
			len(got), got, len(src), src)
	}
	for k := h; k < eol; k++ {
		if got[k] != ' ' {
			t.Fatalf("#6623: byte %d of the `#` comment survived masking as %q — the comment "+
				"branch must blank the rest of the line, including any quoted span inside it. "+
				"A quote inside a comment must NOT escape into the string branch, which skips "+
				"rather than blanks and would leave the commented-out prefix live for the mount "+
				"scan.\n src: %q\n got: %q", k, got[k], src, got)
		}
	}
}
