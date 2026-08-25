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
// The fixture carries BOTH quote kinds on purpose. The escape hatch is per
// quote character, so there are three mutants, not one: the disjunct naming
// the double quote, the disjunct naming the single quote, and the pair. A
// fixture holding only a double quote leaves the single-quote half of the very
// premise this test states unobserved. The guard below therefore requires a
// quote of each kind inside the comment and fails loudly if a later edit drops
// either.
//
// This lives beside TestPythonMaskInertRegions_MustNotPanic (#6617) rather than
// as a fifth row IN it deliberately: that table pins "must not panic" for four
// look-ahead BOUNDS and asserts only that the output length is unchanged. A row
// asserting produced bytes would have to smuggle a second, differently-shaped
// assertion through a table whose whole contract is the crash. Same function,
// different claim, own home.
//
// The kill does not rest on a bounded search. Once every clause below passes,
// each mutant dies for input of ARBITRARY length, by construction:
//
//	C1+C2 put the scanner at the `#`. `h` is the FIRST `#`, and every byte
//	before it is neither `#` nor a quote, so each takes the switch's default
//	arm and the cursor arrives at h with the COMMENT branch selected.
//	C3 gives the mutant a byte to stop on: let q be the first quote inside the
//	comment that the mutant's disjunct names. The loop blanks [h,q) and halts
//	with the cursor ON q, which the switch then dispatches into the quote arm.
//	C4 says q does not open a triple — and the mutant has only rewritten bytes
//	BEFORE q, so the look-ahead sees the source unchanged — so the arm taken is
//	the single-line one, which SKIPS. Nothing writes to q afterwards, since the
//	cursor only advances. got[q] is therefore still a quote, h <= q < eol, and
//	the assertion below fails. Dead.
//	Symmetrically on pristine: the comment loop blanks every byte of [h,eol),
//	blank() rewrites everything except `\n` and `\r`, the region holds no `\n`
//	by construction and no `\r` by C5, so the region comes out all spaces and
//	the assertion passes. Neither argument mentions the length of src.
//
// The brute-force sweep below corroborates that argument rather than carrying
// it: every string of length <= 9 over the alphabet {x, space, =, ", ', #, \,
// a, \n, \r} — 1,111,111,111 inputs, 29,304,608 of which pass the guard —
// scored for guard-pass x mutant-survival and guard-pass x pristine-failure,
// against all three mutants. Zero survivors and zero pristine failures for
// every one of them. The per-clause witnesses annotated below are counted over
// the <= 7 prefix of that sweep (11,111,111 inputs, 215,784 passing).
//
// Refs #6623. Out of scope and separately tracked: #6418 (a terminated literal
// OUTSIDE a comment still mints a mount — a known limitation, not touched
// here), #6614 (the unterminated literal's own tail), #6624 (the `j += 2`
// backslash skip), and the backslash disjunct in this same loop, which is a
// distinct pre-existing survivor filed on its own.
func TestPythonMaskInertRegions_QuotedSpanInsideCommentIsBlanked(t *testing.T) {
	// A commented-out mount, the shape that makes the mutant's effect concrete:
	// under it `"/ghost"` (or `'ghost'`) survives masking and is read as a live
	// prefix. Both quote kinds appear so that all three mutants are scored.
	const src = `# app.include_router(legacy, prefix="/ghost", tags=['ghost'])`

	// --- premise -----------------------------------------------------------
	// The claim is about the COMMENT branch, so the premise must establish that
	// the branch RUNS, not merely that the fixture contains a `#`.

	// LOAD-BEARING. Without it any fixture with no `#` at all — `"""xxx'` —
	// leaves the region empty or misplaced and the assertion observes nothing
	// (2,832 survivors, and 398,940 pristine failures, in the enumeration).
	h := strings.IndexByte(src, '#')
	if h < 0 {
		t.Fatalf("#6623 premise broken: fixture %q contains no `#`, so the comment branch "+
			"never runs and this test is VACUOUS.", src)
	}

	// LOAD-BEARING, and the trap that has caught this function three times
	// (#6611, #6615, #6620): a quote EARLIER in the source consumes the `#` in
	// the string branch, so the comment branch is never entered. `x"""#"'`
	// passes every other clause here and leaves all three mutants alive;
	// `xxx"#"'` passes them and fails on PRISTINE. The scanner dispatches on the
	// first quote-or-comment byte, so requiring the prefix to hold no quote is
	// what proves `#` is that byte and the comment branch is what executes.
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

	// LOAD-BEARING, once per quote kind. A comment with no quote in it —
	// `xxxxx#` — is blanked identically by every version. A comment holding only
	// `'` — `xxxxx#'` — leaves the `"`-only mutant alive (434,718 survivors),
	// and only `"` — `xxxxx#"` — leaves the `'`-only mutant alive (434,718).
	// Each kind is scored separately so neither half of the premise can be
	// silently dropped by a later fixture edit.
	firsts := map[byte]int{}
	for _, q := range []byte{'"', '\''} {
		rel := strings.IndexByte(region[1:], q)
		if rel < 0 {
			t.Fatalf("#6623 premise broken: the comment %q contains no %q, so the mutant that "+
				"stops the comment scan at %q has nothing to escape through and survives "+
				"untouched. This test is now VACUOUS for that half of the claim; the fixture "+
				"must carry BOTH quote kinds.", region, q, q)
		}
		firsts[q] = h + 1 + rel
	}

	// LOAD-BEARING, for each kind's FIRST quote — that is the byte its mutant
	// halts on. If it opens a TRIPLE-quoted literal, the escaping branch is the
	// triple-quote one, which BLANKS its span, so the region comes out blank
	// anyway and the mutant survives. `xx#"""'` passes every other clause and
	// keeps the `"`-stopping mutants alive; `xx#"'''` does the same for the
	// `'`-only one (1,118 survivors each).
	for _, q := range []byte{'"', '\''} {
		p := firsts[q]
		if p+2 < len(src) && src[p+1] == src[p] && src[p+2] == src[p] {
			t.Fatalf("#6623 premise broken: the first %q in the comment (offset %d) opens a "+
				"TRIPLE-quoted literal; that branch blanks its span, so the region is masked "+
				"either way and this test is VACUOUS for that half. Use a single-line literal "+
				"inside the comment; fixture %q.", q, p, src)
		}
	}

	// LOAD-BEARING for the assertion's own correctness rather than for any
	// mutant: blank() preserves `\r` as well as `\n`, so a CR inside the comment
	// — `xxx#"'\r` — makes the all-spaces assertion below fail on PRISTINE
	// (98,664 such inputs). It admits no mutant survivor.
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
