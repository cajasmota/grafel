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
// mutant is ALIVE at 0 without the BOTH cells and DEAD with them.
//
// The unterminated column asserts the runaway that a missing terminator MUST
// still produce: an unterminated verbatim string blanks to EOF exactly as the
// pre-existing `"unterminated"` row of TestScrubPreservesLengthAndNewlines does
// for the ordinary form. That is the compiler's reading too (fsc rejects the
// file), so the four unterminated cells are must-haves, not tolerated damage.
//
// NOTED, not reworked (review round 3): across the mutants scored, no
// unterminated cell is ever a failing row — the artefact-level
// TestFSharp7199_UnterminatedVerbatimStringStillSuppresses is what fires. So
// the four-way escape split WITHIN the unterminated column may grade nothing
// beyond what one cell would. Four occupied cells is still true and is all
// that is claimed for it.
//
// CROSSED SEPARATELY, in TestScrub7199_OpenerFormsAndAdjacency: the OPENER FORM
// (the three the lexer admits) against the ADJACENCY of the byte before the
// opener. Every cell of the escape-content cross below holds adjacency constant
// at "space before the `@`", so it grades none of it; that is a separate table
// rather than a wider one because the two axes have different consequences —
// escape content decides where the literal ENDS, adjacency decides which
// delimiter bytes are SUPPRESSED.
//
// WAS "NOT CLAIMED", NOW SCORED. This list used to open with `$$@"`,
// described as "extended interpolation — only one `$` is blanked". That was
// wrong twice: rule 611 needs THREE quotes so extended interpolation does not
// apply, and the code was not merely blanking it partially — it was entering
// verbatim mode and blanking the REST OF THE FILE. Naming it here as an
// accepted gap is precisely what kept it out of the mutant table, so no mutant
// could surface it. It is now a row in
// TestScrub7199_InterpolatedNonVerbatimIsUNCHANGED and a cell of
// TestScrub7199_OpenerFormsAndAdjacency. Disclosure is not coverage: a shape
// named in a "not claimed" list is a reason to score it.
//
// STILL not crossed, and so not claimed: the `@`-before-a-triple-quote form,
// where the triple-quote check runs FIRST and DISAGREES with the lexer —
// recorded and explained by
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

// # THE OPENER / ADJACENCY AXIS (review findings 1 and 3, round 3)
//
// Every cell of scrubCases7199 has a SPACE before the `@`, so that table holds
// this axis constant and grades none of it. Three separate defects lived here,
// each ALIVE at 0 against the whole package before this table existed:
//
//  1. over-blanking one byte PAST the opener, eating the `(` of `(@"abc")`;
//  2. accepting a bare `$` as an opener, taking the C-style escape away from
//     `$"a\"b"`;
//  3. worst, and found by review: entering verbatim mode on OPERATOR-SUFFIX
//     openers (`$$@"`, `.@"`, `?@"`, `$@$"`, `@@"`, `x=@"`, `x<>@"`, `x+@"`,
//     `x&@"`, `x|@"`, `x!@"`, `x*@"`). The lexer opens an ORDINARY string
//     there, so treating it as verbatim ate the closing quote and blanked the
//     rest of the file — #7199's own defect, reintroduced permissively.
//
// EACH CELL IS ASSERTED WITH TWO BODIES, which is what makes it a verdict about
// the READING rather than a single-sided smoke test. The two bodies fail in
// opposite directions, so no cell can pass by accident:
//
//	`C:\`    verbatim -> `\` is ordinary, the literal closes, tail SURVIVES
//	         ordinary -> `\` escapes the closer, runaway, tail SWALLOWED
//	`a\"b`   verbatim -> closes early at the inner quote, runaway, SWALLOWED
//	         ordinary -> `\"` is an escape, the literal closes, tail SURVIVES
//
// So `wantVerbatim` is checked twice per cell, once from each side, and a cell
// that is silently reclassified fails both ways instead of neither.
type openerCase7199 struct {
	label string
	// before is the text between `let p = ` and the opener; it sets the
	// adjacency being tested.
	before string
	opener string
	// wantVerbatim is the F# LEXER's reading of this cell — see
	// verbatimOpenerStart for the rule citations, including why rule 976 makes
	// `x=$@"` verbatim while `x=@"` is not.
	wantVerbatim bool
	// prefixBlanked applies only to verbatim cells: whether the opener's
	// `@`/`$` bytes become spaces. For ORDINARY cells the prefix is an operator,
	// i.e. real code, and must survive untouched — asserted as such below.
	prefixBlanked bool
	// fileStart drops the `let p = ` lead so `before` sits at OFFSET 0.
	//
	// THIS IS ITS OWN AXIS, and leaving it uncrossed hid a live hole. The
	// offset-0 probes elsewhere cover a bare `@"`, `$@"` and `@$"` at offset 0;
	// this table covers operator prefixes at ordinary positions. Neither
	// covers the CONJUNCTION — an operator prefix AT offset 0 — and that
	// conjunction is the only thing that exercises the walk's lower bound:
	// with `runStart > 1` instead of `> 0` the walk never examines src[0], so
	// a file BEGINNING `=@"` is read as verbatim when the lexer says operator
	// plus ordinary string, and the file blanks to EOF. That mutant was ALIVE
	// against the entire package until these cells existed.
	fileStart bool
}

func openerCases7199() []openerCase7199 {
	return []openerCase7199{
		// ---- VERBATIM, not abutting an identifier: prefix blanked ----
		{"space before @", "", `@`, true, true, false},
		{"open paren before @", "(", `@`, true, true, false},
		{"comma before @", "(1,", `@`, true, true, false},
		{"open bracket before @", "[", `@`, true, true, false},
		{"interpolated verbatim $@", "", `$@`, true, true, false},
		{"interpolated verbatim @$", "", `@$`, true, true, false},
		// ---- VERBATIM, abutting an identifier: prefix left visible ----
		// Per rule 655 these ARE verbatim strings, so the runaway is fixed; only
		// the blanking is withheld, so no CALLS edge rests on that reading.
		{"identifier before @", "helper", `@`, true, false, false},
		{"close bracket before @", "[1]", `@`, true, false, false},
		{"close paren before @", "f()", `@`, true, false, false},
		{"digit before @", "x1", `@`, true, false, false},
		{"primed identifier before @", "c'", `@`, true, false, false},
		// An identifier before the TWO-byte openers still opens one: rule 670
		// matches three bytes and beats the two-byte operator munch.
		{"identifier before $@", "x", `$@`, true, false, false},
		// ---- VERBATIM via rule 976: `=` then a TWO-byte opener ----
		// `| '=' ("$@" | "@$") '"'` consumes the `=` and rewinds, so the opener
		// is re-lexed as verbatim. The `=` is an op_char, so these two cells are
		// the exception that stops the guard being a flat byte list.
		{"equals before $@ (rule 976)", "x=", `$@`, true, true, false},
		{"equals before @$ (rule 976)", "x=", `@$`, true, true, false},
		// ---- ORDINARY: a longer operator munch reaches the quote ----
		// `ignored_op_char*` is `.$?` and every operator rule ends in `op_char*`,
		// which includes `@`; so the token starting at or before the preceding
		// operator byte swallows the `@` and rule 586 opens an ordinary string.
		// There is NO `'=' '@' '"'` rule, which is why `x=@` is here while
		// `x=$@` is above.
		{"double dollar before @", "", `$$@`, false, false, false},
		{"dot before @", "x ", `.@`, false, false, false},
		{"question before @", "x ", `?@`, false, false, false},
		{"dollar-at-dollar", "x ", `$@$`, false, false, false},
		{"doubled at", "", `@@`, false, false, false},
		// NOTE THE REALISTIC SPELLING OF THIS CELL: `let p=@"C:\"`, with no space
		// around the `=`, is an operator `=@` plus an ORDINARY string, so #7199
		// does NOT fix it and the file still blanks to EOF. That is not a
		// regression — pre-#7199 every `@"C:\"` ran away, so this shape is
		// UNCHANGED — and it is not a defect either: fsc rejects the file, and
		// rule 976 was added upstream (dotnet/fsharp#16696) for `=$"` and
		// `=$@"`/`=@$"` precisely because this class does not lex the way a reader
		// expects. Matching the compiler beats out-guessing it, as with `(*)`.
		// `let p = @"C:\"` — with the space — is verbatim and IS fixed.
		{"equals before @ (no 976 rule)", "x=", `@`, false, false, false},
		{"compare op before @", "x<>", `@`, false, false, false},
		{"plus before @", "x+", `@`, false, false, false},
		{"amp before @", "x&", `@`, false, false, false},
		{"bar before @", "x|", `@`, false, false, false},
		{"bang before @", "x!", `@`, false, false, false},
		{"star before @", "x*", `@`, false, false, false},
		// The `<@` / `<@@` QUOTATION rules (802/804) tie with the operator munch
		// on length, and the tie does not need breaking: both readings consume the
		// `@`, so both leave rule 586 at the quote and the verdict is ORDINARY
		// either way.
		{"quotation open before @", "x<", `@`, false, false, false},
		{"typed quotation before @", "x<@", `@`, false, false, false},
		// ---- ORDINARY: the `=` does not START the run (review round 4) ----
		// Rule 976 can only fire where the lexer starts a token at the `=`. When
		// the `=` is itself inside an operator run, the leftward munch swallows
		// it: for `x<=$@"`, rule 981 matches `<=$@` at the `<` — four bytes, and
		// `"` is not an op_char — which 976 cannot beat and cannot even start at.
		// Every cell here ran away to EOF while the exception was keyed on the `=`
		// BYTE rather than on the `=` starting a token, and the direction was
		// COMPLETELY UNGRADED: the correct scoping left the suite at 0 --- FAIL.
		{"double equals before $@", "x==", `$@`, false, false, false},
		{"less-equals before $@", "x<=", `$@`, false, false, false},
		{"greater-equals before $@", "x>=", `$@`, false, false, false},
		{"plus-equals before $@", "x+=", `$@`, false, false, false},
		{"minus-equals before $@", "x-=", `$@`, false, false, false},
		{"star-equals before $@", "x*=", `$@`, false, false, false},
		{"bar-equals before $@", "x|=", `$@`, false, false, false},
		{"amp-equals before $@", "x&=", `$@`, false, false, false},
		{"bang-equals before $@", "x!=", `$@`, false, false, false},
		{"percent-equals before $@", "x%=", `$@`, false, false, false},
		{"slash-equals before $@", "x/=", `$@`, false, false, false},
		{"tilde-equals before $@", "x~=", `$@`, false, false, false},
		{"dot-equals before $@", ".=", `$@`, false, false, false},
		{"dollar-equals before $@", "$=", `$@`, false, false, false},
		{"question-equals before $@", "?=", `$@`, false, false, false},
		{"compare-equals before $@", "x<>=", `$@`, false, false, false},
		{"double equals before @$", "x==", `@$`, false, false, false},
		{"less-equals before @$", "x<=", `@$`, false, false, false},
		// ---- VERBATIM via the COLON family (review round 4, finding 2) ----
		// `:` is the ONLY op_char that is neither an ignored_op_char nor any
		// rule's core, so no operator rule can start at it — only the fixed
		// `:` `::` `:>` `:?` `:=` tokens (846-862). The run therefore CONTINUES
		// after such a token and the opener does start a token. Round 3 read
		// these as ordinary, which was an under-fix (not a regression: it matched
		// pre-#7199), and the left-walk gets them right for free.
		{"assign-colon before @", "r:=", `@`, true, true, false},
		{"cons before @", "x::", `@`, true, true, false},
		{"colon-greater before @", "x:>", `@`, true, true, false},
		// ---- OFFSET 0 x OPERATOR PREFIX (the uncrossed conjunction) ----
		// Each axis was already covered alone: a bare opener at offset 0, and
		// operator prefixes at ordinary positions. Only the conjunction
		// reaches the walk's lower bound, which was ALIVE at 0 until these
		// cells existed.
		//
		// THREE of the six flip under `runStart > 1` — `=@`, `$$@` and `:=@` —
		// and the other three (`@`, `$@`, `=$@`) do NOT, because the walk has
		// nothing to its left to examine in those. Counted by measurement, not
		// by eye: an earlier version of this comment claimed "four of five",
		// which was wrong on both numbers. The three that do not flip stay as
		// the controls that stop the flipping three being read as "offset 0 is
		// special".
		{"file starts with @", "", `@`, true, true, true},
		{"file starts with $@", "", `$@`, true, true, true},
		{"file starts with =@ (operator at offset 0)", "=", `@`, false, false, true},
		{"file starts with $$@ (operator at offset 0)", "", `$$@`, false, false, true},
		{"file starts with =$@ (rule 976 at offset 0)", "=", `$@`, true, true, true},
		{"file starts with :=@ (colon at offset 0)", ":=", `@`, true, true, true},
	}
}

// TestScrub7199_OpCharsAreEitherIgnoredOrCoreOrColon is the enumeration that
// makes lexerOpensTokenAt's case analysis EXHAUSTIVE rather than merely
// plausible — the property three revisions of a flat byte test lacked.
//
// Every one of lex.fsl's 18 `op_char`s (line 238) must be an `ignored_op_char`
// (line 240), or the `<core>` of some symbolic-operator rule (961-985), or `:`.
// `:` being the sole leftover is what lets the walk treat "not a core and not
// `:`" as unreachable-in-practice and fall back conservatively.
func TestScrub7199_OpCharsAreEitherIgnoredOrCoreOrColon(t *testing.T) {
	const opChars = "!$%&*+-./<=>?@^|~:" // lex.fsl:238, verbatim and in order
	const ignored = ".$?"                // lex.fsl:240
	const cores = "*/%+-@^=<>&|!~"       // the <core> of each rule at 961-985

	if len(opChars) != 18 {
		t.Fatalf("op_char set has %d entries, want 18 — it was edited without re-deriving it from lex.fsl:238", len(opChars))
	}
	var leftover []string
	for i := 0; i < len(opChars); i++ {
		c := opChars[i]
		if strings.IndexByte(ignored, c) < 0 && strings.IndexByte(cores, c) < 0 {
			leftover = append(leftover, string(c))
		}
		// Every op_char must also be reported as one by the implementation.
		if !isOpChar(c) {
			t.Errorf("isOpChar(%q) = false, but it is in lex.fsl:238", c)
		}
	}
	if len(leftover) != 1 || leftover[0] != ":" {
		t.Errorf("op_chars that are neither ignored nor a rule core = %v, want exactly [\":\"]. "+
			"lexerOpensTokenAt's case analysis is built on `:` being the only one; if that changed, "+
			"the walk has a new unhandled run-start class and its `default` arm is silently "+
			"under-fixing it", leftover)
	}
	// And nothing OUTSIDE the set may be reported as an op_char, or the walk
	// would treat ordinary code as an operator run.
	for c := 0; c < 256; c++ {
		if isOpChar(byte(c)) && strings.IndexByte(opChars, byte(c)) < 0 {
			t.Errorf("isOpChar(%q) = true, but it is not in lex.fsl:238", byte(c))
		}
	}
}

func TestScrub7199_OpenerFormsAndAdjacency(t *testing.T) {
	for _, tc := range openerCases7199() {
		lead := "let p = "
		if tc.fileStart {
			lead = "" // the prefix begins at offset 0
		}
		prefix := lead + tc.before + tc.opener
		// Body 1: a trailing backslash. Verbatim reads it as an ordinary
		// character and the tail survives; an ordinary string escapes the closer
		// and runs away.
		srcBackslash := prefix + "\"C:\\\"\nlet after = 1\n"
		// Body 2: an escaped quote. The verdicts are exactly inverted.
		srcEscape := prefix + "\"a\\\"b\"\nlet after = 1\n"

		for _, probe := range []struct {
			name        string
			src         string
			wantTailFor bool // the wantVerbatim value for which the tail survives
		}{
			{"trailing backslash", srcBackslash, true},
			{"escaped quote", srcEscape, false},
		} {
			got := stripStringsAndComments(probe.src)
			if len(got) != len(probe.src) {
				t.Errorf("%s / %s: len(scrub) = %d, want %d", tc.label, probe.name, len(got), len(probe.src))
				continue
			}
			if strings.Count(got, "\n") != strings.Count(probe.src, "\n") {
				t.Errorf("%s / %s: newline count changed", tc.label, probe.name)
			}
			tailSurvived := strings.Contains(got, "let after = 1")
			wantTail := tc.wantVerbatim == probe.wantTailFor
			if tailSurvived != wantTail {
				verdict := "ORDINARY"
				if tc.wantVerbatim {
					verdict = "VERBATIM"
				}
				t.Errorf("%s / %s: tail survived = %v, want %v — this cell must be read as %s per the F# lexer "+
					"(see verbatimOpenerStart); scrub = %q",
					tc.label, probe.name, tailSurvived, wantTail, verdict, got)
			}
		}

		// The remaining assertions are about WHICH BYTES move, measured on the
		// backslash body.
		got := stripStringsAndComments(srcBackslash)
		openerAt := strings.Index(srcBackslash, tc.opener+"\"")
		if openerAt < 0 {
			t.Fatalf("%s: fixture does not contain its own opener %q", tc.label, tc.opener)
		}

		// Suppression must never reach OUTSIDE the literal.
		//
		// PARTIALLY VACUOUS BY CONSTRUCTION, stated so a rewrite does not shed
		// the live cells and keep the dead ones: in the three cells whose
		// preceding byte is a SPACE (`space before @`, `$@`, `@$`), over-blanking
		// writes a space over a space and this assertion CANNOT fail. It is live
		// in the other cells, where the preceding byte is `(`, `,`, `[`, an
		// identifier byte, or an operator — and that is what kills the
		// over-blank mutant.
		if openerAt > 0 && got[openerAt-1] != srcBackslash[openerAt-1] {
			t.Errorf("%s: suppression reached OUTSIDE the literal: byte %d was %q, now %q — scrub %q",
				tc.label, openerAt-1, srcBackslash[openerAt-1], got[openerAt-1], got)
		}

		openerBytes := got[openerAt : openerAt+len(tc.opener)]
		if tc.wantVerbatim {
			blanked := strings.TrimSpace(openerBytes) == ""
			if blanked != tc.prefixBlanked {
				t.Errorf("%s: opener %q scrubbed to %q (blanked=%v), want blanked=%v — the adjacency rule at the call site changed",
					tc.label, tc.opener, openerBytes, blanked, tc.prefixBlanked)
			}
		} else if openerBytes != tc.opener {
			// An ORDINARY cell's prefix is an OPERATOR — real code. Blanking it
			// would delete a token the call scanners read.
			t.Errorf("%s: the operator %q was scrubbed to %q, but it is code, not part of a literal — scrub %q",
				tc.label, tc.opener, openerBytes, got)
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

	// `$$@"` IS NOT EXTENDED INTERPOLATION, and it is not a verbatim opener
	// either. An earlier revision of this file listed it under "not claimed" as
	// "extended interpolation, only one `$` blanked" — wrong twice over, and
	// naming it as an accepted gap is what stopped it being scored while the
	// code was in fact entering verbatim mode on it and blanking the rest of
	// the file.
	//
	// Rule 611 (`('$'+) '"' '"' '"'`) requires THREE quotes, so it does not
	// apply here at all. What applies is the operator rule
	// `967 | ignored_op_char* ('@'|'^') op_char*` with
	// `ignored_op_char = '.' | '$' | '?'`: it munches `$$@` as one
	// INFIX_AT_HAT_OP, leaving rule 586 to open an ORDINARY string at the quote
	// — where `\` escapes.
	t.Run("$$@ is an operator plus an ORDINARY string, not a verbatim opener", func(t *testing.T) {
		src := "let s = $$@\"a\\\"b\"\nlet after = 1\n"
		got := stripStringsAndComments(src)

		if !strings.Contains(got, "let after = 1") {
			t.Errorf("the rest of the file was swallowed — `$$@\"a\\\"b\"` was read as a verbatim string, so the "+
				"escaped quote closed it early and the reopened quote ran to EOF. This is #7199's own defect: %q", got)
		}
		if strings.Contains(got, "b") {
			t.Errorf("the literal body leaked — `\\\"` must still escape here: %q", got)
		}
		if !strings.Contains(got, "$$@") {
			t.Errorf("the `$$@` operator was blanked, but it is code rather than part of a literal: %q", got)
		}
	})
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
