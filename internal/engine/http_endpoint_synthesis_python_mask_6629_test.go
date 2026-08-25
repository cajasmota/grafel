package engine

import (
	"strings"
	"testing"
)

// pythonMaskInertRegions' `#`-comment branch blanks the rest of the line
// unconditionally, and NOTHING may end the comment early except the newline.
// #6623 pinned that for a QUOTE inside the comment. The backslash half was
// left open and is a real, pre-existing hole: adding a third disjunct
//
//	for ; i < len(b) && b[i] != '\n' && b[i] != '\\'; i++ {
//
// left `go vet` at 0 and the full engine package at exit 0 on 32d9e1162.
//
// The effect is the same class of damage as #6623, by a different route.
// Under that mutant the comment loop halts ON the backslash, which the switch
// dispatches to its DEFAULT arm (a backslash is neither `#` nor a quote), so
// the cursor simply steps past it and the REST OF THE COMMENT is scanned as
// live Python. A commented-out `prefix="/ghost"` that sits after the backslash
// is then read by the single-line-literal branch, which SKIPS rather than
// blanks, and the ghost prefix reaches the mount scan. The witness from the
// issue: `# a \ prefix="/ghost"` masks to all spaces pristine, and to
// `    \ prefix=\"/ghost\"` under the mutant.
//
// Why the kill holds for input of ARBITRARY length, by construction rather
// than by a bounded search:
//
//	C1+C2 put the scanner at the `#`. `h` is the FIRST `#` and every byte
//	before it is neither `#` nor a quote, so each takes the switch's default
//	arm and the cursor arrives at h with the COMMENT branch selected.
//	C3 gives the mutant a byte to stop on: the first `\` inside the comment.
//	The loop blanks [h,bs) and halts with the cursor ON bs, and nothing
//	afterwards writes to bs — the cursor only ever advances, and the byte is
//	dispatched to the default arm, which writes nothing. got[bs] is therefore
//	still `\`, h <= bs < eol, and the all-spaces assertion below fails. Dead.
//	Symmetrically on pristine: the comment loop blanks every byte of [h,eol),
//	blank() rewrites everything except `\n` and `\r`, the region holds no `\n`
//	by construction and no `\r` by C4, so it comes out all spaces and the
//	assertion passes. Neither argument mentions the length of src.
//
// Four mutant families were scored separately, because a sweep that varies the
// INPUT against a FIXED mutant answers "does the guard admit a defeating
// input" and says nothing about "does the fixture pin the family" — the lesson
// #6625 paid for. Before this file: `\\` SURVIVOR; `'`, `"` and the combined
// three-disjunct form all DEAD, killed by #6623's fixture. After: all four
// DEAD. The combined form was therefore never a distinguishing family — it
// dies on its quote clauses alone — and `\\` alone is what this file adds.
//
// Per-clause drop analysis, measured (each guard neutralised in turn, the
// fixture degraded to the shape that guard forbids, then every mutant
// re-applied and this test run in isolation):
//
//	C1 dropped, fixture `'''x \ prefix="/ghost"'''`      pristine PASS, all four SURVIVE
//	C2 dropped, fixture `"""# \ prefix='/ghost'"""`      pristine PASS, all four SURVIVE
//	C3 dropped, fixture without the `\`                  pristine PASS, `\\` SURVIVES, quotes DEAD
//	C4 dropped, fixture with a `\r` in the comment       pristine FAIL, no survivor
//	control, all guards, this fixture                    pristine PASS, all four DEAD
//
// So C1 and C2 are load-bearing against every mutant; C3 is load-bearing
// against the `\\` mutant ONLY — the quote mutants die on this fixture with C3
// gone, which is precisely why a verdict has to name the family it is scored
// against (#6632); and C4 is DECORATIVE as a mutant guard, load-bearing only
// for the assertion's correctness on pristine code.
//
// Out of scope and separately tracked: #6418 (a terminated literal OUTSIDE a
// comment still mints a mount — the known limitation whose real fix is a
// Python tokenizer), #6624 (the `j += 2` escape skip inside a single-line
// literal), #6637 (the escaped-newline shape, where the backslash is the last
// byte of the line). Already covered and deliberately NOT re-pinned here: the
// over-masking direction (dropping `b[i] != '\n'` so the comment eats to EOF)
// is dead under TestSynth_FastAPI_MountedRouter_AbsentWhenUnresolvable_6414.
//
// Refs #6629, #6623, #6617, #6418.
func TestPythonMaskInertRegions_BackslashInsideCommentIsBlanked_6629(t *testing.T) {
	// A commented-out mount whose backslash sits BEFORE the prefix, so the
	// mutant's effect is concrete: the tail stays live and `"/ghost"` is read
	// as a real prefix by the mount scan.
	const src = `# app.include_router(legacy, \ prefix="/ghost", tags=['ghost'])`

	// --- premise -----------------------------------------------------------
	// The claim is about the COMMENT branch, so the premise must establish
	// that the branch RUNS — not merely that the fixture contains a `#` and a
	// backslash.

	// C1, LOAD-BEARING against all four mutants. With no `#` at all the
	// comment branch never runs; a fixture such as `'''x \ prefix="/ghost"'''`
	// is blanked by the TRIPLE-QUOTE branch instead, so the region comes out
	// all spaces under every mutant and the assertion observes nothing.
	h := strings.IndexByte(src, '#')
	if h < 0 {
		t.Fatalf("#6629 premise broken: fixture %q contains no `#`, so the comment branch "+
			"never runs and this test is VACUOUS.", src)
	}

	// C2, LOAD-BEARING against all four mutants, and the trap that has caught
	// this function repeatedly (#6611, #6615, #6620, and the `'`-half of
	// #6623): a quote EARLIER in the source consumes the `#` in the string
	// branch, so the comment branch is never entered. `"""# \ prefix='/x'"""`
	// passes every other clause here and leaves every mutant alive, because
	// the triple-quote branch blanks the region either way. The scanner
	// dispatches on the first quote-or-comment byte, so requiring the prefix
	// to hold no quote is what proves `#` is that byte.
	if strings.ContainsAny(src[:h], "\"'") {
		t.Fatalf("#6629 premise broken: the source before the `#` at %d must contain no quote, "+
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

	// C3, LOAD-BEARING against the `\\` mutant specifically — the one family
	// this file exists to kill. A comment with no backslash in it is blanked
	// identically by the pristine loop and by the `\\` mutant, which then
	// SURVIVES: #6623's own fixture, which differs from this one only by the
	// missing `\`, is exactly that shape and is why the hole was still open.
	// (The three quote-naming mutants die on this fixture regardless of this
	// clause, since it also carries both quote kinds — so a drop here is
	// invisible unless the verdict names the mutant. See #6632.)
	bs := strings.IndexByte(region, '\\')
	if bs < 0 {
		t.Fatalf("#6629 premise broken: the comment %q contains no backslash, so the mutant "+
			"that stops the comment scan at `\\` has nothing to escape through and survives "+
			"untouched. This test is now VACUOUS for the claim it exists to make.", region)
	}
	bs += h

	// C4 is for the ASSERTION's own correctness rather than for any mutant:
	// blank() preserves `\r` as well as `\n`, so a CR inside the comment makes
	// the all-spaces assertion below fail on PRISTINE. Dropping it admits no
	// mutant survivor — it is decorative as a mutant guard, and load-bearing
	// only as a guard against a later fixture edit breaking the test on
	// correct code.
	if strings.Contains(region, "\r") {
		t.Fatalf("#6629 premise broken: the comment %q contains a carriage return, which "+
			"blank() preserves; the assertion below would fail on correct code. Remove it.", region)
	}

	// --- claim -------------------------------------------------------------
	got := pythonMaskInertRegions(src)

	if len(got) != len(src) {
		t.Fatalf("#6629: pythonMaskInertRegions changed length: got %d (%q), want %d (%q)",
			len(got), got, len(src), src)
	}
	for k := h; k < eol; k++ {
		if got[k] != ' ' {
			t.Fatalf("#6629: byte %d of the `#` comment survived masking as %q — a backslash "+
				"(first one at %d) must NOT end the comment. If it does, the cursor lands on it, "+
				"takes the switch's default arm, and the REST of the comment is scanned as live "+
				"Python: the commented-out prefix reaches the mount scan and mints a mount that "+
				"does not exist. src %q, masked %q",
				k, got[k], bs, src, got)
		}
	}
}
