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
// CROSSED SEPARATELY, in TestScrub7199_OpenerFormsAndAdjacency: the OPENER FORM
// (the three the lexer admits) against the ADJACENCY of the byte before the
// opener. Every cell of the escape-content cross below holds adjacency constant
// at "space before the `@`", so it grades none of it; that is a separate table
// rather than a wider one because the two axes have different consequences —
// escape content decides where the literal ENDS, adjacency decides which
// delimiter bytes are SUPPRESSED.
//
// NOT crossed, and so not claimed: `$$@"` (extended interpolation — only one
// `$` is blanked); the `@`-before-a-triple-quote form, where the triple-quote
// check runs FIRST and DISAGREES with the lexer, recorded and explained by
// TestScrub7199_AtTripleQuoteIsReadAsTripleQuote_DISAGREES_WITH_FSC; and a
// verbatim string inside a block comment (the comment arm consumes it before
// the quote arm is reached).
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
// Both directions are asserted here. The claim is bounded to what is actually
// graded, because the unbounded version of this sentence was measured FALSE:
// this row catches the ALWAYS-TRUE widening, and the one-character widening to
// "any `$` also opens a verbatim string" is caught by
// TestScrub7199_InterpolatedNonVerbatimIsUNCHANGED instead — it was ALIVE at 0
// against the whole package until that row existed. Nothing here grades a
// widening to some other single byte.
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

// # THE ADJACENCY AXIS (review finding 1/3)
//
// Every cell of scrubCases7199 has a SPACE before the `@`, so that table holds
// adjacency constant and grades none of it. Two mutants proved the gap: blanking
// one byte FURTHER back than the opener (eating the `(` of `(@"abc")`) was ALIVE
// against the whole package, and so was the question of whether an identifier
// abutting the opener changes anything.
//
// The table below crosses OPENER FORM × ADJACENCY and asserts three separate
// things per row, so neither half can regress silently:
//
//   - tailVisible — the runaway is fixed (or, for `$"`, never existed);
//   - the byte BEFORE the opener always survives — suppression never reaches
//     outside the literal, which is the `insideBraces` harm the ordinary-path
//     row already pins for `("abc")` and nothing pinned for `@"`;
//   - prefixBlanked — whether the `@`/`$` prefix bytes are blanked, which is a
//     DELIBERATE function of adjacency and not incidental. See the call site in
//     extractor.go: where the opener abuts an identifier the prefix stays
//     visible on purpose, so no CALLS edge can be fabricated out of an
//     unexecuted lexing claim.
func TestScrub7199_OpenerFormsAndAdjacency(t *testing.T) {
	cases := []struct {
		label string
		// before is the text between `let p = ` and the opener; it sets the
		// adjacency being tested.
		before string
		opener string
		// prefixBlanked says whether the opener's `@`/`$` bytes become spaces.
		prefixBlanked bool
	}{
		// NOT abutting an identifier — prefix blanked.
		{"space before @", "", `@`, true},
		{"open paren before @", "(", `@`, true},
		{"comma before @", "(1,", `@`, true},
		{"open bracket before @", "[", `@`, true},
		{"equals before @", "x=", `@`, true},
		{"interpolated verbatim $@", "", `$@`, true},
		{"interpolated verbatim @$", "", `@$`, true},
		// ABUTTING an identifier or a closing bracket — prefix left visible.
		// Per the F# lexer these ARE verbatim strings (see
		// verbatimOpenerStart), so the runaway is still fixed; only the
		// blanking of the prefix is withheld.
		{"identifier before @", "helper", `@`, false},
		{"close bracket before @", "[1]", `@`, false},
		{"close paren before @", "f()", `@`, false},
		{"digit before @", "x1", `@`, false},
		{"primed identifier before @", "c'", `@`, false},
	}
	for _, tc := range cases {
		// A trailing backslash inside the literal, so a row that fails to
		// enter verbatim mode runs away and `tail` disappears.
		src := "let p = " + tc.before + tc.opener + "\"C:\\\"\nlet after = 1\n"
		got := stripStringsAndComments(src)

		if len(got) != len(src) {
			t.Errorf("%s: len(scrub) = %d, want %d", tc.label, len(got), len(src))
			continue
		}
		if !strings.Contains(got, "let after = 1") {
			t.Errorf("%s: code after the literal was swallowed — %q", tc.label, got)
		}

		openerAt := strings.Index(src, tc.opener+"\"")
		if openerAt < 0 {
			t.Fatalf("%s: fixture does not contain its own opener %q", tc.label, tc.opener)
		}
		// The byte before the opener must survive in EVERY row — this is the
		// assertion that over-blanking past the delimiter has to fail.
		if openerAt > 0 && got[openerAt-1] != src[openerAt-1] {
			t.Errorf("%s: suppression reached OUTSIDE the literal: byte %d was %q and is now %q — scrub %q",
				tc.label, openerAt-1, src[openerAt-1], got[openerAt-1], got)
		}
		// And the prefix itself, in whichever direction this row claims.
		prefix := got[openerAt : openerAt+len(tc.opener)]
		blanked := strings.TrimSpace(prefix) == ""
		if blanked != tc.prefixBlanked {
			t.Errorf("%s: opener prefix %q scrubbed to %q (blanked=%v), want blanked=%v — the adjacency rule at the call site changed",
				tc.label, tc.opener, prefix, blanked, tc.prefixBlanked)
		}
		// The body is suppressed either way.
		if strings.Contains(got, "C:") {
			t.Errorf("%s: the literal body leaked — %q", tc.label, got)
		}
	}
}

// TestScrub7199_InterpolatedNonVerbatimIsUNCHANGED grades the ONE-CHARACTER
// widening of the opener test, which the always-true mutant (M8) does not
// reach: accepting a bare `$` before the quote as a verbatim opener was ALIVE
// at 0 against the whole package.
//
// It is a real behaviour change, and it is precisely the line a follow-up
// touches, since `$@"` / `@$"` are handled here and `$$"""` is not. Per
// lex.fsl a plain `$"` is rule 626 — interpolated but NOT verbatim — so `\`
// still escapes inside it.
func TestScrub7199_InterpolatedNonVerbatimIsUNCHANGED(t *testing.T) {
	src := "let s = $\"a\\\"b\"\nlet after = 1\n"
	got := stripStringsAndComments(src)

	if strings.Contains(got, "b") {
		t.Errorf("`$\"` lost its C-style escape — the escaped quote closed the string early and leaked its tail: %q", got)
	}
	if !strings.Contains(got, "let after = 1") {
		t.Errorf("code after a `$\"` string was swallowed — %q", got)
	}
	// The `$` is NOT part of a verbatim opener here, so it survives — the
	// other direction of the same decision.
	if !strings.Contains(got, "$") {
		t.Errorf("the `$` of a non-verbatim interpolated string was blanked as if it were a verbatim opener — %q", got)
	}
}

// TestScrub7199_SuppressedBytesAreSpaces pins the representation every caller
// assumes: a suppressed byte is a SPACE (newlines excepted, #6336). Added
// because dropping one of the two writes in the doubled-quote escape leaves a
// byte at its zero value — a NUL where a space belongs — and nothing in the
// package noticed. Whether a consumer's regex tolerates a NUL is not the point;
// no branch of the scrub should be able to emit one.
func TestScrub7199_SuppressedBytesAreSpaces(t *testing.T) {
	srcs := []string{
		"let s = @\"say \"\"hi\"\"\"\nlet after = 1\n",
		"let s = @\"say \"\"hi\"\" C:\\\"\nlet after = 1\n",
		"let s = @\"C:\\\"\nlet after = 1\n",
		"let s = $@\"say \"\"hi\"\"\"\nlet after = 1\n",
		"let s = \"\"\"triple\"\"\"\nlet after = 1\n",
		"let s = \"ord\\\"inary\"\nlet after = 1\n",
		"let a = 1 (* c *) let b = 2\n",
	}
	for _, src := range srcs {
		got := stripStringsAndComments(src)
		for i := range got {
			if got[i] != src[i] && got[i] != ' ' && got[i] != '\n' {
				t.Errorf("byte %d of the scrub of %q is %q — a suppressed byte must be a space (or a newline), never a zero value",
					i, src, got[i])
			}
		}
	}
}

// TestScrub7199_AtTripleQuoteIsReadAsTripleQuote_DISAGREES_WITH_FSC RECORDS
// TODAY'S BEHAVIOUR AND NAMES IT AS A DISAGREEMENT. It is not an endorsement.
//
// `@"""x"""` reaches the TRIPLE-QUOTE branch, because that check runs before
// the verbatim one, and so the `@` survives the scrub. The F# lexer would read
// it the other way: no rule matches `@` followed by three quotes (verified —
// zero such rules in lex.fsl), so longest match at the `@` gives rule 655
// `'@' '"'`, a verbatim string whose body opens with an escaped quote.
//
// NOT fixed here, deliberately. #7199 is the backslash runaway; reordering the
// two checks changes which BYTES are suppressed for a different construct
// (`@"""a"b"""` ends the literal in a different place under each reading), and
// that is an extent change that deserves its own measurement rather than a ride
// on this one. This row exists so the disagreement is pinned and visible rather
// than latent, and so a later reorder shows up here as a deliberate update.
func TestScrub7199_AtTripleQuoteIsReadAsTripleQuote_DISAGREES_WITH_FSC(t *testing.T) {
	src := "let p = @\"\"\"x\"\"\"\nlet after = 1\n"
	got := stripStringsAndComments(src)

	if !strings.Contains(got, "@") {
		t.Errorf("RECORDED BEHAVIOUR CHANGED: the `@` before a triple quote is now blanked, i.e. `@\"\"\"` is being "+
			"read as a verbatim opener. That AGREES with the lexer — if it was done deliberately, update this row "+
			"and state the extent change for `@\"\"\"a\"b\"\"\"`; scrub = %q", got)
	}
	if strings.Contains(got, "x") {
		t.Errorf("the triple-quoted body leaked — %q", got)
	}
	if !strings.Contains(got, "let after = 1") {
		t.Errorf("code after `@\"\"\"x\"\"\"` was swallowed — %q", got)
	}
}
