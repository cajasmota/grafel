package engine

import (
	"fmt"
	"strings"
	"testing"
)

// #6680 — the escape set of the SINGLE-LINE literal scan in
// pythonMaskInertRegions is exactly `\`, and no other byte.
//
// WHICH BRANCH. There are two escape skips in this function and they are byte
// identical apart from their indentation:
//
//	http_endpoint_synthesis.go:3218  the TRIPLE-QUOTED branch's skip (5 tabs)
//	http_endpoint_synthesis.go:3242  the SINGLE-LINE literal scan's skip (4 tabs)  <- THESE ROWS
//
// (line numbers re-derived from `git show e86a4ccd7:internal/engine/http_endpoint_synthesis.go`;
// this file has drifted twice, so never carry a line number over from a sibling
// row). The twin at :3218 is #6679 and is deliberately untouched here; g2 below
// makes it unreachable on every fixture in this file, so a kill scored against
// these rows cannot have landed on it.
//
// THE DEFECT. #6624 and #6637 pin the escape skip against mutants that NARROW
// what counts as an escape. Nothing pinned the opposite direction, on the
// stated grounds that `b[j] == '\\'` is "unconditional" and therefore has no
// strictly-more-permissive member. That claim is false: the predicate is
// unconditional in its OPERAND, not in its CHARACTER SET, and
// `b[j] == '\\' || b[j] == X` is strictly more permissive for every X. Measured
// survivors against the full package at e86a4ccd7, BEFORE these rows existed
// (`go vet` 0 for each; `go test -count=1 ./internal/engine/`, full package, no
// `-run` filter):
//
//	W1  `if b[j] == '\\' || b[j] == '/' {`    ALIVE  -> killed by the sweep, byte 0x2f
//	W2  `if b[j] == '\\' || b[j] == ' ' {`    ALIVE  -> killed by the ruled row AND the sweep, byte 0x20
//	W3  `if b[j] == '\\' || b[j] == '=' {`    ALIVE  -> killed by the sweep, byte 0x3d
//	W5  `if b[j] == '\\' || b[j] == '"' {`    already DEAD (20 rows, incl. #6385/#6418/#6624)
//	W4  `if b[j] == '\\' || b[j] == '\n' {`   already DEAD (#6598's row)
//
// W4 and W5 are REPORTED as already dead rather than claimed as kills: two of
// the five widenings tried were pinned before this file existed, by rows aimed
// at something else. The sweep re-covers 0x22 as a by-product of enumerating
// the space; it does not cover 0x0a, for the reason recorded under EXCLUSIONS.
//
// W1's witness is a canonical FastAPI line, and it is the most common prefix
// value there is:
//
//	in       `app.include_router(r, prefix="/")`
//	pristine `app.include_router(r, prefix="/")`
//	W1       `app.include_router(r, prefix=    `
//
// The `/` immediately before the closing quote is consumed as an escape, so the
// real terminator is skipped, the literal reads as UNTERMINATED, #6614 blanks
// its tail, and the mount disappears. That is the same over-masking false
// negative #6637 exists to pin, reached through the neighbouring door — and a
// repo missing an endpoint looks exactly like a repo that never had one.
// Pristine is CORRECT here: Python honours a backslash before any byte inside a
// single-line literal, so every widening is a defect and this is a pin, not a
// behaviour change. #6418's no-tokenizer constraint stands.
//
// WHY ONE LITERAL CANNOT KILL THREE, measured rather than argued. The kill
// mechanism is that the widened set eats the byte IMMEDIATELY BEFORE the
// closing quote, so the two-byte skip steps over the terminator. Only one byte
// can occupy that position, so a single literal pins exactly one member of the
// family. `app.include_router(r, prefix="/api = ")` carries `/`, ` ` and `=`,
// but its closer is preceded by a space, and it kills W2 ALONE — W1 and W3
// survive it, both predicted and then measured. Under W1 the `/` at content
// offset 0 skips to offset 2 and the scan still finds the real terminator;
// under W3 the `=` skips onto the terminator and breaks there. That row is kept
// verbatim below because it is the ruled fixture and because it is the shape a
// reader will reach for, with the measurement attached so the next reader does
// not repeat it.
//
// WHAT PINS THE FAMILY is therefore not a hand-picked literal but an
// enumeration of the fixture space: a ONE-BYTE literal `prefix="X"` for every
// admissible byte X. With exactly one content byte, the victim IS the byte
// before the closer, so the widening on X — and only the widening on X — dies
// on case X. 253 of the 256 bytes are covered; the three exclusions are
// recorded below rather than papered over.
//
// EXCLUSIONS, none of them manufactured away:
//
//   - `\\` (0x5c) — RECORDED EQUIVALENT, do not re-score as DEAD.
//     `b[j] == '\\' || b[j] == '\\'` is a tautological duplicate of pristine.
//     No input distinguishes them, so no fixture can kill it and none should be
//     built.
//   - `\n` (0x0a) — out of this row's SHAPE, not merely absent from it. The
//     single-line scan breaks on `\n`, so a fixture of the form above cannot
//     carry one inside its literal. `b[j] == '\\' || b[j] == '\n'` is a
//     multi-line question and belongs with #6637's escaped-newline shape. It was
//     scored anyway and was ALREADY DEAD against the package at e86a4ccd7,
//     killed by #6598's row — reported, not claimed by these rows.
//   - `\r` (0x0d) — deliberately not pinned. Whether the single-line scan
//     should treat a lone CR as a terminator is an OPEN decision tracked as
//     #6653; a row here that fixed CR behaviour would pre-empt it.
//
// NARROWING DIRECTION, scored too and expected dead — reported, not claimed:
// `..._6624_test.go` and #6637's row already cover it, and both narrowings
// tried here were already DEAD against the package at e86a4ccd7:
//
//	N1  `... && j+1 < len(b) && (b[j+1] == c || b[j+1] == '\\')`  already DEAD (#6637)
//	N2  `... && j > 0 && b[j-1] == 0`  (escape skip effectively off)  already DEAD (#6624, #6637)
//
// Neither is claimed as work done here.
//
// Refs #6680; siblings #6624, #6637 (narrowings), #6418 (terminated literals
// stay intact), #6614 (unterminated tails are blanked), #6659 (a sweep varies
// the INPUT against a FIXED mutant, so exhausting the input space says nothing
// about the mutant space). Out of scope and separately tracked: #6679 (the
// triple-quoted twin), #6653, #6654, #6655.

// guardSingleLineEscape6680 rejects any fixture that cannot observe a widened
// escape set, and returns the byte offsets of the opening and closing quotes of
// the literal under test. Every clause is a premise of the assertions that
// follow it; none of them counts, filters or de-duplicates anything, so there
// is no helper here that can hide what it observed.
//
// victim is the byte that MUST sit immediately before the closing quote. That
// clause (g5) is the load-bearing one: delete the victim byte from a fixture
// and the corresponding widening survives while the row keeps passing, which is
// exactly the vacuity this guard exists to prevent. It is derived from the
// fixture's own quote offsets rather than from a fixed column, because a
// column- or prefix-based guard is absorbed by padding and goes inert.
func guardSingleLineEscape6680(t *testing.T, src string, victim byte) (openIdx, closeIdx int) {
	t.Helper()

	// g0 The scanner dispatches on the first `#`, `"` or `'`. A `#` goes to the
	// COMMENT branch, which eats the line without ever reaching the escape skip.
	q := strings.IndexAny(src, "#\"'")
	if q < 0 || src[q] == '#' {
		t.Fatalf("#6680 premise broken: the first quote-or-comment byte in %q must be a QUOTE — a `#` "+
			"dispatches to the comment branch and the single-line escape skip never RUNS. "+
			"This row is now VACUOUS.", src)
	}
	quote := src[q]

	// g1 The byte after the opening quote must exist and DIFFER from it, so the
	// triple-quote probe fails and the SINGLE-LINE branch is the one that runs.
	if q+1 >= len(src) || src[q+1] == quote {
		t.Fatalf("#6680 premise broken: the byte after the opening quote must exist and differ from it, "+
			"or the TRIPLE-QUOTED branch runs instead of the single-line scan; got %q. "+
			"This row is now VACUOUS.", src)
	}

	// g2 BRANCH IDENTITY. The triple-quoted branch is entered only on three
	// consecutive identical quote bytes, so a fixture containing neither `"""`
	// nor `'''` cannot execute the twin's escape skip (:3218) on ANY dispatch.
	// Without this clause a kill here could belong to #6679 and be reported
	// against :3242.
	if strings.Contains(src, `"""`) || strings.Contains(src, "'''") {
		t.Fatalf("#6680 premise broken: fixture %q contains a triple-quote run, so the identical escape "+
			"skip in the TRIPLE-QUOTED branch (:3218) becomes reachable and this row can no longer say "+
			"which branch it observed. This row is now VACUOUS.", src)
	}

	// g3 No backslash anywhere. Pristine's own escape skip must never fire, so
	// any difference between pristine and a mutant is attributable to the
	// WIDENED byte alone and not to a shared escape.
	if i := strings.IndexByte(src, '\\'); i >= 0 {
		t.Fatalf("#6680 premise broken: fixture %q contains a backslash at offset %d, so pristine's own "+
			"escape skip fires and a difference can no longer be attributed to the widened byte. "+
			"This row is now VACUOUS.", src, i)
	}

	// g4 The literal must TERMINATE on this line: pristine leaves a terminated
	// single-line literal intact (#6418), which is what licenses the byte-exact
	// identity assertion. A newline inside it would end the scan first.
	body := src[q+1:]
	k := strings.IndexByte(body, quote)
	if k < 0 {
		t.Fatalf("#6680 premise broken: the literal in %q never closes, so pristine blanks it (#6614) and "+
			"there is no surviving mount to assert. This row is now VACUOUS.", src)
	}
	if bad := strings.IndexAny(body[:k], "\n\r"); bad >= 0 {
		t.Fatalf("#6680 premise broken: a line terminator stands between the opening quote and the closing "+
			"%q in %q, so the scan breaks before reaching the terminator. This row is now VACUOUS.",
			quote, src)
	}
	closeIdx = q + 1 + k

	// g5 KILL GUARD — the incidental byte the whole kill rests on. The widened
	// set only skips the terminator when the byte it eats sits IMMEDIATELY
	// before the closing quote; anywhere else the scan still finds the real
	// terminator and the mutant is indistinguishable from pristine.
	if closeIdx == 0 || src[closeIdx-1] != victim {
		t.Fatalf("#6680 premise broken: the byte immediately before the closing quote must be %q (0x%02x), "+
			"or the widening on that byte cannot skip the terminator and this row observes NOTHING; "+
			"fixture %q has %q there. This row is now VACUOUS.",
			victim, victim, src, src[closeIdx-1])
	}

	// g6 Nothing after the closing quote may open another inert region, and the
	// fixture must be a single line — otherwise the identity assertion would be
	// observing some other region instead of the literal under test.
	if strings.ContainsAny(src[closeIdx+1:], "#\"'") {
		t.Fatalf("#6680 premise broken: nothing after the closing quote may open another literal or "+
			"comment, or the identity assertion below observes that region instead: %q. "+
			"This row is now VACUOUS.", src)
	}
	if strings.ContainsAny(src, "\n\r") {
		t.Fatalf("#6680 premise broken: fixture %q must be a single line; a line terminator moves the "+
			"#6614 blanking boundary and the identity claim is no longer line-scoped. "+
			"This row is now VACUOUS.", src)
	}

	// EXECUTION WITNESS. Containing a quote proves only that the input has a
	// quote in it; five fixtures in this function have passed without executing
	// the branch they claimed to test. So run the scan on a PROBE that is the
	// fixture with its closing quote deleted and nothing else changed. Pristine
	// must then blank from the opening quote to end of input — an output that
	// ONLY the single-line scan's unterminated path (#6614) produces. The scan
	// walks the identical content bytes in the probe and in the fixture, so
	// reaching the terminator position in the probe proves it reaches it in the
	// fixture too.
	probe := src[:closeIdx] + src[closeIdx+1:]
	want := probe[:q] + strings.Repeat(" ", len(probe)-q)
	if got := pythonMaskInertRegions(probe); got != want {
		t.Fatalf("#6680 premise broken: the SINGLE-LINE literal scan did not run on probe %q — expected "+
			"its unterminated tail blanked from the opening quote (#6614), want %q, got %q. The fixture "+
			"%q therefore cannot be said to execute the branch under test. This row is now VACUOUS.",
			probe, want, got, src)
	}
	return q, closeIdx
}

// TestPythonMaskInertRegions_EscapeSetRuledLiteral_6680 is the ruled fixture:
// one literal carrying `/`, ` ` and `=` before its closer. It kills W2 and only
// W2 — see the WHY ONE LITERAL CANNOT KILL THREE note above, and the sweep row
// below for the rest of the family. The assertion is POSITIVE: the mount
// SURVIVES the mask byte for byte, which a fixture that never parsed cannot
// satisfy vacuously.
func TestPythonMaskInertRegions_EscapeSetRuledLiteral_6680(t *testing.T) {
	const src = `app.include_router(r, prefix="/api = ")`

	openIdx, closeIdx := guardSingleLineEscape6680(t, src, ' ')
	literal := src[openIdx : closeIdx+1]

	got := pythonMaskInertRegions(src)
	if got != src {
		t.Fatalf("#6680: a TERMINATED single-line literal changed under the mask: got %q, want %q. The "+
			"escape skip in the SINGLE-LINE branch (http_endpoint_synthesis.go:3242, NOT the "+
			"triple-quoted twin at :3218) must fire for `\\` and for NO other byte. Widen it by one byte "+
			"— here the space immediately before the closing quote — and the two-byte skip steps OVER "+
			"the terminator: the literal reads as unterminated, #6614 blanks its tail, and the live "+
			"app.include_router mount is gone. That is the OVER-masking direction, a false negative, and "+
			"it is invisible downstream because a missing endpoint looks like an endpoint that never "+
			"existed.", got, src)
	}
	if !strings.Contains(got, literal) {
		t.Fatalf("#6680: the mount's prefix literal %q did not survive the mask intact in %q; a "+
			"terminated literal is left alone (#6418) so the value can still be read out of the masked "+
			"copy.", literal, got)
	}
}

// TestPythonMaskInertRegions_EscapeSetIsExactlyBackslash_6680 enumerates the
// fixture space instead of hand-picking from it: a one-byte literal
// `prefix="X"` for every admissible byte X. The single content byte IS the byte
// before the closer, so case X kills the widening on X and no other — which is
// what makes the subtest name in a FAIL line a direct attribution rather than a
// guess. 253 bytes are covered; `\\`, `\n` and `\r` are excluded for the
// reasons recorded in the file comment above.
func TestPythonMaskInertRegions_EscapeSetIsExactlyBackslash_6680(t *testing.T) {
	covered := 0
	for v := 0; v < 256; v++ {
		b := byte(v)
		if b == '\\' || b == '\n' || b == '\r' {
			continue
		}
		covered++
		t.Run(fmt.Sprintf("byte_0x%02x", v), func(t *testing.T) {
			// The double-quoted template cannot carry a `"` inside its literal,
			// so that one byte is pinned with a single-quoted literal instead.
			// Both take the same branch: the scan is quote-agnostic, it tracks
			// whichever quote opened the literal.
			src := `app.include_router(a, prefix="` + string([]byte{b}) + `")`
			if b == '"' {
				src = `app.include_router(a, prefix='` + string([]byte{b}) + `')`
			}

			openIdx, closeIdx := guardSingleLineEscape6680(t, src, b)
			literal := src[openIdx : closeIdx+1]

			got := pythonMaskInertRegions(src)
			if got != src {
				t.Fatalf("#6680: widening the single-line escape set to include %q (0x%02x) skips the "+
					"closing quote of %q: got %q, want the input unchanged. The escape skip at "+
					"http_endpoint_synthesis.go:3242 must fire for `\\` and for NO other byte — every "+
					"other byte before a terminator is ordinary string content, and eating it leaves the "+
					"literal unterminated so #6614 blanks the mount away.", b, b, src, got)
			}
			if !strings.Contains(got, literal) {
				t.Fatalf("#6680: the prefix literal %q did not survive the mask intact in %q.", literal, got)
			}
		})
	}
	if covered != 253 {
		t.Fatalf("#6680 premise broken: the sweep must cover 253 of the 256 byte values (all but `\\`, "+
			"`\\n` and `\\r`); it covered %d. A shrunken sweep silently drops family members.", covered)
	}
}
