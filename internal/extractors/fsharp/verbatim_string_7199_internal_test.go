package fsharp

import (
	"strings"
	"testing"
)

// # THE HELPER-LEVEL ROWS FOR #7199
//
// stripStringsAndComments had no verbatim-string mode: the `case '"'` arm
// carried the comment "Check for verbatim string @\"...\"" and performed no
// such check, so a `\` inside `@"C:\"` was consumed as a C-style escape and ate
// the closing quote. In F# a verbatim string treats `\` as an ordinary
// character and escapes a quote by DOUBLING it, so `@"C:\"` is complete and
// legal; the scanner nonetheless stayed inStr to EOF and blanked every
// remaining byte of the file. The comment is now TRUE — the check exists next
// to it — which is the half of #7199's "code and claim must agree" that was
// chosen (the alternative, rewriting the comment to disclaim the check, was
// not).
//
// # THE AXIS CROSS
//
// `@"` × (contains `\` / contains `""` / contains BOTH / contains neither) ×
// (terminated / unterminated at EOF) is EIGHT cells, and all eight are occupied
// below — `scrubCases7199` is written as the cross itself so no cell can be
// dropped silently.
//
// The BOTH column is not decoration, and it is why the escape axis is a power
// set here rather than a three-way choice. A mutant that drops the
// doubled-quote escape — closing the verbatim string at the FIRST interior
// quote — is ALIVE on the doubled-quote-only row: each doubled pair then reads
// as close+reopen, the quote parity is unchanged, and the literal still ends
// where it should. Only a literal holding a doubled quote AND a later `\`
// separates them: under the mutant the tail is scanned as an ORDINARY string,
// where `\` eats the closing quote and the file runs away. Measured — that
// mutant is ALIVE at 0 without the BOTH cells and DEAD with them. The unterminated column asserts the runaway that a missing
// terminator MUST still produce: an unterminated verbatim string blanks to EOF
// exactly as the pre-existing `"unterminated"` row of
// TestScrubPreservesLengthAndNewlines does for the ordinary form. That is the
// compiler's reading too (fsc rejects the file), so the three unterminated
// cells are must-haves, not tolerated damage.
//
// NOT crossed, and so not claimed: the interpolated verbatim forms `$@"…"` and
// `@$"…"`; `@"""` (the triple-quote check runs FIRST, so that input is read as
// a triple-quoted string and this commit does not change it); and a verbatim
// string inside a block comment (the comment arm consumes it before the quote
// arm is reached).
type scrubCase7199 struct {
	label string
	// hasBackslash/hasDoubled/terminated name the cell of the cross this row
	// occupies. They are asserted against the source, so a row whose text stops
	// matching its label fails instead of quietly re-testing a neighbour.
	hasBackslash bool
	hasDoubled   bool
	terminated   bool
	src          string
	// tail is the code AFTER the literal that must survive the scrub
	// (terminated rows) or must NOT survive it (unterminated rows).
	tail string
}

func scrubCases7199() []scrubCase7199 {
	return []scrubCase7199{
		// terminated × contains `\` — the defect of #7199.
		{"terminated, backslash", true, false, true, "let p = @\"C:\\\"\nlet after = 1\n", "let after = 1"},
		// terminated × contains `""` — the doubled-quote escape. A fix that
		// stops at the FIRST interior quote passes the row above and breaks
		// this one: it would close on the opening quote of `""hi""` and read
		// `hi` as code.
		{"terminated, doubled quote", false, true, true, "let s = @\"say \"\"hi\"\"\"\nlet after = 1\n", "let after = 1"},
		// terminated × neither — the ordinary verbatim string, the row that
		// stops the two above being read as "verbatim strings are special".
		{"terminated, neither", false, false, true, "let s = @\"abc\"\nlet after = 1\n", "let after = 1"},
		// unterminated × each of the three. `\` must NOT rescue a missing
		// terminator, and a trailing lone `"` inside the doubled row must not
		// close it either.
		{"unterminated, backslash", true, false, false, "let p = @\"C:\\\nlet after = 1\n", "let after = 1"},
		{"unterminated, doubled quote", false, true, false, "let s = @\"say \"\"hi\nlet after = 1\n", "let after = 1"},
		{"unterminated, neither", false, false, false, "let s = @\"abc\nlet after = 1\n", "let after = 1"},
		// BOTH × terminated / unterminated. The `\` comes AFTER the doubled
		// quote on purpose: that is the order in which a dropped-escape mutant
		// leaves the scanner in ordinary-string mode with a backslash still to
		// come.
		{"terminated, doubled quote and backslash", true, true, true, "let s = @\"say \"\"hi\"\" C:\\\"\nlet after = 1\n", "let after = 1"},
		{"unterminated, doubled quote and backslash", true, true, false, "let s = @\"say \"\"hi\"\" C:\\\nlet after = 1\n", "let after = 1"},
	}
}

// TestScrub7199_VerbatimStringCross is the must-have table: every cell of the
// cross, asserted on the scrub's OUTPUT rather than on a state flag.
func TestScrub7199_VerbatimStringCross(t *testing.T) {
	for _, tc := range scrubCases7199() {
		// The cell labels are load-bearing, so they are verified against the
		// source text rather than trusted.
		if strings.Contains(tc.src, "\\") != tc.hasBackslash {
			t.Errorf("%s: source backslash presence disagrees with the cell label (hasBackslash=%v)", tc.label, tc.hasBackslash)
		}
		if strings.Contains(tc.src, "\"\"") != tc.hasDoubled {
			t.Errorf("%s: source doubled-quote presence disagrees with the cell label (hasDoubled=%v)", tc.label, tc.hasDoubled)
		}

		got := stripStringsAndComments(tc.src)

		if len(got) != len(tc.src) {
			t.Errorf("%s: len(scrub) = %d, want %d", tc.label, len(got), len(tc.src))
			continue
		}
		if strings.Count(got, "\n") != strings.Count(tc.src, "\n") {
			t.Errorf("%s: newline count = %d, want %d", tc.label, strings.Count(got, "\n"), strings.Count(tc.src, "\n"))
		}

		if tc.terminated {
			if !strings.Contains(got, tc.tail) {
				t.Errorf("%s: code after the verbatim string was swallowed — scrub = %q, want it to contain %q", tc.label, got, tc.tail)
			}
		} else if strings.Contains(got, tc.tail) {
			t.Errorf("%s: an UNTERMINATED verbatim string must blank to EOF (fsc rejects the file) — scrub = %q still shows %q", tc.label, got, tc.tail)
		}
	}
}

// TestScrub7199_OrdinaryStringIsUNCHANGED grades the LOOKBEHIND, which is the
// only thing separating the new mode from the old one. Widening it to
// always-true — every `"` opens a verbatim string — was ALIVE at 0 against
// every other row in this package, and it is a real behaviour change in two
// independent ways:
//
//   - an ordinary string loses its C-style escape, so `"a\"b"` closes early and
//     leaks `b` and then runs away on the reopened quote;
//   - the verbatim arm blanks the byte BEFORE the quote (the `@`), so an
//     always-true lookbehind eats whatever precedes any string — `("abc")`
//     loses its opening paren, and a blanked bracket is exactly what the
//     insideBraces guards read.
//
// Both directions are asserted here so the lookbehind cannot be widened
// silently.
func TestScrub7199_OrdinaryStringIsUNCHANGED(t *testing.T) {
	t.Run("backslash still escapes inside an ordinary string", func(t *testing.T) {
		src := "let s = \"a\\\"b\"\nlet after = 1\n"
		got := stripStringsAndComments(src)
		if strings.Contains(got, "b") {
			t.Errorf("the escaped quote closed the string early and leaked its tail — %q", got)
		}
		if !strings.Contains(got, "let after = 1") {
			t.Errorf("code after an ordinary string with an escaped quote was swallowed — %q", got)
		}
	})

	t.Run("the byte before an ordinary string's quote survives", func(t *testing.T) {
		src := "let s = (\"abc\")\nlet after = 1\n"
		got := stripStringsAndComments(src)
		if !strings.Contains(got, "(") || !strings.Contains(got, ")") {
			t.Errorf("suppression reached OUTSIDE the literal and ate a bracket — %q", got)
		}
		if strings.Contains(got, "abc") {
			t.Errorf("the string body leaked — %q", got)
		}
	})
}

// TestScrub7199_VerbatimBodyIsStillBlanked is the FORBIDDEN row at the helper
// level: the new mode must SUPPRESS the literal's body, not merely survive it.
// It fails alone under the permissive direction (stop opening a string at `"`
// at all), which every row of TestScrub7199_VerbatimStringCross passes.
func TestScrub7199_VerbatimBodyIsStillBlanked(t *testing.T) {
	cases := map[string]string{
		"verbatim":           "let s = @\"inherit Ghost()\"\nlet after = 1\n",
		"verbatim doubled":   "let s = @\"say \"\"inherit Ghost()\"\" ok\"\nlet after = 1\n",
		"verbatim backslash": "let s = @\"inherit Ghost() C:\\\"\nlet after = 1\n",
		"ordinary string":    "let s = \"inherit Ghost()\"\nlet after = 1\n",
	}
	for name, src := range cases {
		got := stripStringsAndComments(src)
		if strings.Contains(got, "inherit") || strings.Contains(got, "Ghost") {
			t.Errorf("%s: the literal's BODY leaked into the scrub — %q", name, got)
		}
		if !strings.Contains(got, "let after = 1") {
			t.Errorf("%s: code after the literal was swallowed — %q", name, got)
		}
		// The `@` of the opening delimiter is blanked with the rest of the
		// literal. Asserted because the ALTERNATIVE (leaving it visible, which
		// is what the pre-#7199 code did) silently defends the anchored
		// consumers: `^\s*module` cannot match behind a surviving `@`, so a
		// permissive regression on the verbatim path would be unobservable
		// downstream. See the comment at the `case '"'` verbatim check.
		if strings.Contains(src, "@") && strings.Contains(got, "@") {
			t.Errorf("%s: the `@` of the verbatim delimiter survived the scrub — %q", name, got)
		}
	}
}
