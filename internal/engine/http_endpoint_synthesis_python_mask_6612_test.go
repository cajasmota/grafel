package engine

import (
	"strings"
	"testing"
)

// #6612 — pythonMaskInertRegions must not panic on a Python file that ends with
// a backslash INSIDE an unterminated triple-quoted string.
//
// In the triple-quoted branch the scan advances `j += 2` on a backslash. With
// the backslash as the final byte and no closing delimiter, `j` reaches
// len(b)+1 and the subsequent `blank(k)` loop would index past the end. A clamp
// (`if j > len(b) { j = len(b) }`) is the only thing preventing the crash, and
// before this test nothing in the package observed it: removing the clamp left
// `go vet` at 0 and the full package suite at exit 0.
//
// A file does not have to be valid Python to be indexed — partially-written
// files, generated fragments and mid-edit incremental indexes all reach here —
// but it does have to not crash the indexer.
//
// Scope: this pins the CRASH only. What the masker produces for an unterminated
// literal's own line is #6614, and is deliberately not asserted here.
func TestPythonMaskInertRegions_UnterminatedTripleQuoteTrailingBackslash_6612(t *testing.T) {
	const src = "x = \"\"\"abc\\"

	// Premise guard — the mutant this test scores is only reachable for a
	// fixture of exactly this shape. If a later edit terminates the literal or
	// drops the trailing backslash, the assertion below would still pass while
	// observing nothing, so fail loudly here instead of going quietly vacuous.
	if !strings.HasSuffix(src, "\\") {
		t.Fatalf("#6612 premise broken: fixture must end with a trailing backslash so the "+
			"triple-quote scan advances j past len(b); got %q. This test is now VACUOUS — "+
			"restore the trailing backslash or delete the test.", src)
	}
	if n := strings.Count(src, `"""`); n != 1 {
		t.Fatalf("#6612 premise broken: fixture must contain exactly one `\"\"\"` so the "+
			"triple-quoted literal is UNTERMINATED; got %d occurrences in %q. A terminated "+
			"literal never runs j past the end, so this test is now VACUOUS — restore the "+
			"unterminated literal or delete the test.", n, src)
	}
	// Containing a `"""` is not the same as REACHING the triple-quote branch:
	// `# """abc\` is eaten by the comment branch and `x = "a"""abc\` by the
	// single-line branch, and both satisfy the two guards above while observing
	// nothing. Require the `"""` to be the FIRST quote-or-comment character, so
	// it is the opener the scanner actually dispatches on.
	if first, triple := strings.IndexAny(src, "#\"'"), strings.Index(src, `"""`); first != triple {
		t.Fatalf("#6612 premise broken: the `\"\"\"` must be the FIRST quote-or-comment "+
			"character in the fixture so the triple-quote branch is the one that RUNS; first "+
			"such character is at %d, the `\"\"\"` is at %d, in %q. A `#` comment or an "+
			"earlier single quote consumes the line before the triple-quote branch is ever "+
			"entered, so this test is now VACUOUS — restore a fixture whose `\"\"\"` opens "+
			"the first literal, or delete the test.", first, triple, src)
	}

	// Recover so the mutant surfaces as a test failure rather than taking the
	// whole test binary down with it.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("#6612: pythonMaskInertRegions panicked on an unterminated triple-quoted "+
				"literal ending in a backslash (%q): %v — the out-of-range clamp in the "+
				"triple-quote branch is load-bearing and must stay.", src, r)
		}
	}()

	got := pythonMaskInertRegions(src)

	// The masker's contract is a same-length copy with the same newline
	// positions; a length change here would mean the clamp truncated output
	// rather than merely bounding the loop.
	if len(got) != len(src) {
		t.Fatalf("#6612: pythonMaskInertRegions changed length: got %d (%q), want %d (%q)",
			len(got), got, len(src), src)
	}
}
