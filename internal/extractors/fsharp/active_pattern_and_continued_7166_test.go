package fsharp_test

import (
	"strings"
	"testing"
)

// #7166 arm 1 — `and`-CONTINUED active patterns were never extracted.
//
// F# continues a recursive binding group with `and`, not with a second `let`:
//
//	let rec (|Even|Odd|) x = …
//	and (|Positive|Negative|) y = …
//
// `activePatternRE` was anchored `(?m)^([ \t]*)let…`, which requires `let` to
// be the first non-whitespace token of the line, so the SECOND declaration —
// and, with it, its whole case set and every match-site USES edge those cases
// resolve — was invisible. Measured before the fix: 1 SCOPE.Pattern, not 2.
//
// THIS IS A MISSING ENTITY, NOT A SURVIVING PHANTOM, and that was measured
// rather than assumed (issue comment on #7166). `letRE` is anchored
// `^([ \t]*)let` exactly as this scanner is, so an `and` head is not a `let`
// head and BOTH scanners miss it together — there is nothing left over to
// mis-name. That is the opposite of #7163, where the two anchors agreed and
// only the name class differed, leaving `letRE` free to mint a phantom. The
// pre-existing row TestActivePatternLetPhantom_AndContinuedMintsNoPhantom
// pins that census, and every must-have row below ALSO calls
// fsPhantomAssertNoPhantom so that a fix which recovered the pattern by
// re-opening #7163's phantom would fail here too.
//
// LEGALITY IS DERIVED, NOT EXECUTED. There is no F# toolchain in this
// environment (`dotnet`, `fsc`, `fsharpc`, `fsi`, `mono` all absent; `javac`
// is the only compiler present), so no row here was compiled. Sources:
//
//  1. F# Language Specification, "Recursive definitions" / the `let rec`
//     binding group: a group is `let rec function-defn (and function-defn)*`
//     — the continuation keyword is `and`, and every member of the group is
//     an ordinary `function-defn` (https://fsharp.github.io/fslang-spec/).
//  2. MS Learn "Functions" → *Recursive Functions* and "let Bindings": the
//     `let rec f x = … and g x = …` form for mutually recursive definitions.
//  3. MS Learn "Active Patterns": an active-pattern definition IS a `let`
//     binding, `let (|identifier|…|) [arguments] = expression`, so it is a
//     `function-defn` and may be a member of such a group.
//
// Rows whose legality those sources do not settle are labelled per row below,
// never left silent.
//
// AXES VARIED: the head keyword (`let` vs `and` — the defect axis); the
// modifier phrase on the `and` head (none / private / internal / public /
// inline / inline private / rec / mutable — identity, count 0-2, and order);
// the CHAIN LENGTH (2 members and 3 members, so "handles `and`" cannot be
// confused with "handles exactly one `and`"); the clip shape (two-case total
// and partial `(|_|)`); indentation of the `and` head (column 0 and nested);
// and the surrounding text of a non-declaration `and` (line comment, inline
// string literal, mid-line, `and` as a word PREFIX).
//
// AXES HELD CONSTANT, deliberately: the case names (`Even`/`Odd` for the
// opening `let rec`, `Positive`/`Negative`, `On`/`Off`, `Up`/`Down` for the
// continuations, `Ghost`/`Ghoul` and friends for every forbidden row — so a
// wrong capture is unambiguous and a forbidden name is UNIQUE IN ITS FIXTURE,
// which matters because the F# dedup keys are name-keyed and a ghost sharing
// a real name merges silently, #7144/#7152); the real binding's name
// (`sentinelValue`, in no modifier allowlist); the enclosing `module
// Patterns`; the file path (`patterns.fs`); and the entity Kinds under
// assertion (SCOPE.Pattern for definitions, SCOPE.Schema for cases,
// SCOPE.Operation for the phantom census).
//
// NOT FIXED HERE: arm 2 of #7166, the same-line `[<Attr>] let (|A|B|)` case.
// It needs the `^[ \t]*` prefix itself loosened to tolerate preceding
// non-whitespace tokens, which is a strictly larger widening of a scanner
// that reads RAW `src` (#7152) — see TestActivePatternAndContinued_ScrubResidualParityWithLet.
// Adding a second KEYWORD at the same anchor does not move that anchor.

// lenienceAndRec labels a row whose modifier is accepted by the allowlist
// shared with letRE but is not grammatical on an `and` continuation.
const lenienceAndRec fsGrammar = "NOT legal F# — 'rec' belongs to the 'let rec' head that OPENS the group, not to an 'and' continuation; pins the allowlist shared with letRE"

// fsAndSrc builds the canonical two-member fixture: a `let rec` head plus one
// `and` continuation carrying the given modifier phrase. The `and` head is
// always on line 5, which every row asserts, so a pattern recovered from the
// WRONG line (the `let` head re-counted, say) cannot pass.
func fsAndSrc(mod string) string {
	if mod != "" {
		mod += " "
	}
	return "module Patterns\n\n" +
		"let rec (|Even|Odd|) x =\n    1\n" +
		"and " + mod + "(|Positive|Negative|) y =\n    2\n\n" +
		"let sentinelValue = 0\n"
}

// TestActivePatternAndContinued_BothPatternsExtracted is the must-have row the
// issue's own measurement demands: the two-member group must yield TWO
// patterns, both named, with both case sets — not the one the old anchor saw.
func TestActivePatternAndContinued_BothPatternsExtracted(t *testing.T) {
	src := fsAndSrc("")
	ents := runFSharp(t, src, "patterns.fs")

	if got := strings.Join(fsAPNames(ents), ","); got != "(|Even|Odd|),(|Positive|Negative|)" {
		t.Errorf("`let rec … and …` produced active patterns [%s], want "+
			"[(|Even|Odd|),(|Positive|Negative|)] — the `and`-continued member is "+
			"the one the `^[ \\t]*let` anchor could not reach (#7166 arm 1)", got)
	}
	wantCases := "(|Even|Odd|).Even,(|Even|Odd|).Odd," +
		"(|Positive|Negative|).Positive,(|Positive|Negative|).Negative"
	if got := strings.Join(fsAPCaseNames(ents), ","); got != wantCases {
		t.Errorf("case sub-entity census = [%s], want [%s] — the definition is the SOLE "+
			"producer of its cases, so a missed definition takes its whole case set with it",
			got, wantCases)
	}
	ap := fsFind(ents, "(|Positive|Negative|)", "SCOPE.Pattern")
	if ap == nil {
		t.Fatalf("no SCOPE.Pattern for the `and`-continued declaration; census=%v", fsAPNames(ents))
	}
	if ap.Subtype != "active_pattern" {
		t.Errorf("`and`-continued pattern subtype=%q, want %q", ap.Subtype, "active_pattern")
	}
	if ap.StartLine != 5 {
		t.Errorf("`and`-continued pattern StartLine=%d, want 5 (the `and` line) — a pattern "+
			"reported at the `let` line would mean the wrong match was credited", ap.StartLine)
	}
	if ap.Signature != "and (|Positive|Negative|)" {
		t.Errorf("`and`-continued pattern Signature=%q, want %q — the emitted signature must "+
			"name the head keyword the declaration actually carries",
			ap.Signature, "and (|Positive|Negative|)")
	}
	// The #7163 direction, re-asserted here: recovering the entity must not
	// re-open the phantom the sibling scanner was taught to decline.
	fsPhantomAssertNoPhantom(t, ents, "and (|Positive|Negative|) y =", specLegal)
	if got := strings.Join(fsOpCensus(ents), ","); got != "let/sentinelValue" {
		t.Errorf("operation census=%q, want [let/sentinelValue] — exactly the fixture's one "+
			"real binding, which is positive proof nothing extra was minted", got)
	}
}

// TestActivePatternAndContinued_ModifierAxis varies the modifier phrase on the
// `and` head. The modifier group is SHARED with letRE byte-for-byte
// (TestActivePatternREModifierParityWithLetRE), so an `and` head admits
// whatever a `let` head admits; these rows pin that the keyword widening did
// not accidentally amputate it on one branch of the alternation.
func TestActivePatternAndContinued_ModifierAxis(t *testing.T) {
	for _, tc := range []struct {
		mod     string
		grammar fsGrammar
	}{
		{"", specLegal},
		{"private", specLegal},  // MS Learn "Access Control": access precedes the name
		{"internal", specLegal}, // § 10.5 access := public | private | internal
		{"public", specLegal},   // § 10.5
		{"inline", specLegal},   // function-defn := inline? access? ident-or-op
		{"inline private", specLegal},
		{"private inline", lenienceOnly},
		{"rec", lenienceAndRec},
		{"mutable", lenienceSubform},
	} {
		label := tc.mod
		if label == "" {
			label = "none"
		}
		t.Run(label, func(t *testing.T) {
			ents := runFSharp(t, fsAndSrc(tc.mod), "patterns.fs")
			if got := strings.Join(fsAPNames(ents), ","); got != "(|Even|Odd|),(|Positive|Negative|)" {
				t.Errorf("`and %s (|Positive|Negative|)` [%s] produced patterns [%s], want both members",
					tc.mod, tc.grammar, got)
			}
			ap := fsFind(ents, "(|Positive|Negative|)", "SCOPE.Pattern")
			if ap == nil {
				t.Fatalf("`and %s …` [%s]: no SCOPE.Pattern", tc.mod, tc.grammar)
			}
			if ap.StartLine != 5 {
				t.Errorf("`and %s …` [%s]: StartLine=%d, want 5", tc.mod, tc.grammar, ap.StartLine)
			}
			if got := strings.Join(fsAPCaseNames(ents), ","); !strings.Contains(got,
				"(|Positive|Negative|).Positive,(|Positive|Negative|).Negative") {
				t.Errorf("`and %s …` [%s]: case census=[%s], missing the continuation's cases",
					tc.mod, tc.grammar, got)
			}
			fsPhantomAssertNoPhantom(t, ents, "and "+tc.mod+" (|Positive|Negative|) y =", tc.grammar)
		})
	}
}

// TestActivePatternAndContinued_ThreeMemberChain uses a chain of THREE, plus a
// partial clip and a nested indentation, because a two-element fixture cannot
// distinguish "handles `and`" from "handles exactly one `and`".
func TestActivePatternAndContinued_ThreeMemberChain(t *testing.T) {
	src := "module Patterns\n\n" +
		"let rec (|Even|Odd|) x =\n    1\n" +
		"and (|Positive|Negative|) y =\n    2\n" +
		"and (|On|_|) z =\n    3\n\n" +
		"module Nested =\n" +
		"    let rec (|Up|Down|) a =\n        4\n" +
		"    and (|Left|Right|) b =\n        5\n\n" +
		"let sentinelValue = 0\n"
	ents := runFSharp(t, src, "patterns.fs")

	want := "(|Even|Odd|),(|Positive|Negative|),(|On|_|),(|Up|Down|),(|Left|Right|)"
	if got := strings.Join(fsAPNames(ents), ","); got != want {
		t.Errorf("three-member chain + nested group produced [%s], want [%s] — a fixture with "+
			"exactly one `and` cannot tell a general fix from one that handles a single "+
			"continuation", got, want)
	}
	if ap := fsFind(ents, "(|On|_|)", "SCOPE.Pattern"); ap == nil {
		t.Errorf("the third member of the chain is missing entirely")
	} else {
		if ap.Subtype != "partial_active_pattern" {
			t.Errorf("`and (|On|_|)` subtype=%q, want partial_active_pattern — the clip shape "+
				"must still be read off an `and` head", ap.Subtype)
		}
		if ap.StartLine != 7 {
			t.Errorf("`and (|On|_|)` StartLine=%d, want 7", ap.StartLine)
		}
	}
	if ap := fsFind(ents, "(|Left|Right|)", "SCOPE.Pattern"); ap == nil {
		t.Errorf("the INDENTED `and` continuation inside `module Nested` is missing — the " +
			"anchor's indentation capture must apply to both keywords")
	} else if ap.StartLine != 13 {
		t.Errorf("indented `and` continuation StartLine=%d, want 13", ap.StartLine)
	}
	fsPhantomAssertNoPhantom(t, ents, "three-member chain", specLegal)
}

// TestActivePatternAndContinued_NotAtLineStartMintsNothing is the FORBIDDEN
// half, and it is the direction every must-have row above is structurally
// blind to. Adding a second keyword to the anchor is a WIDENING, and
// extractActivePatterns is handed the RAW `src` (#7152) rather than the
// comment/string-scrubbed copy the edge scanners use — so the only thing
// standing between a widened keyword set and a ghost minted out of prose is
// the `^([ \t]*)` line anchor. These rows grade exactly that anchor: in every
// one of them the text `and (|…|) … =` is present in the file and must NOT
// produce an entity, because it does not begin its line.
//
// Each ghost name is UNIQUE IN ITS FIXTURE (`Ghost`, `Ghoul`, `Cee`, …), never
// a reuse of `Positive`/`Negative`, because the dedup keys are name-keyed and
// a ghost that collides with a real declaration merges into it and is
// invisible to both a count and a presence assertion (#7144, #7152).
func TestActivePatternAndContinued_NotAtLineStartMintsNothing(t *testing.T) {
	head := "module Patterns\n\nlet rec (|Even|Odd|) x =\n    1\n"
	tail := "\nlet sentinelValue = 0\n"
	for _, tc := range []struct {
		label, line, why string
	}{
		{
			"line comment", "// and (|Ghost|Ghoul|) z = 3",
			"a commented-out continuation: `and` is not the first token of the line",
		},
		{
			"indented line comment", "    // and (|Cee|Dee|) z = 3",
			"same, behind indentation, so the `[ \\t]*` capture cannot be blamed",
		},
		{
			"inline string literal", `let s = "and (|Eee|Eff|) z = 1"`,
			"a continuation inside a string: the scanner reads raw src (#7152)",
		},
		{
			"mid-line", "let g = 1 in x = 1 and (|Kay|Ell|) z = 1",
			"`and` mid-line must not match — this is the row a `(?m)^` -> unanchored " +
				"mutant flips, and nothing else here catches that",
		},
		{
			"word with an `and` PREFIX", "andThen (|Emm|Enn|) z = 1",
			"`andThen` starts the line with the letters a-n-d; the mandatory separator " +
				"after the keyword is what rejects it",
		},
		{
			"word with an `and` SUFFIX", "band (|Oh|Pee|) z = 1",
			"`band` contains `and` but does not begin with it",
		},
	} {
		t.Run(tc.label, func(t *testing.T) {
			ents := runFSharp(t, head+tc.line+"\n"+tail, "patterns.fs")
			if got := strings.Join(fsAPNames(ents), ","); got != "(|Even|Odd|)" {
				t.Errorf("%s (%s) produced patterns [%s], want [(|Even|Odd|)] alone — "+
					"line %q must mint nothing", tc.label, tc.why, got, tc.line)
			}
			if got := strings.Join(fsAPCaseNames(ents), ","); got != "(|Even|Odd|).Even,(|Even|Odd|).Odd" {
				t.Errorf("%s (%s) produced cases [%s], want the real declaration's two alone — "+
					"a ghost DEFINITION suppressed while its CASES survive is still a defect",
					tc.label, tc.why, got)
			}
		})
	}
}

// TestActivePatternAndContinued_ScrubResidualParityWithLet measures, rather
// than assumes, what this widening does to #7152's surface.
//
// #7152 is that all four F# declaration scanners are handed the RAW source, so
// a declaration written inside a block comment or a triple-quoted literal —
// at the START of its line, which is the only thing the anchor checks — is
// extracted as if it were code. That is TRUE ON `main` FOR `let` (measured:
// a `(* … *)`-enclosed `let (|Ghost|Ghoul|) z =` mints the pattern and both
// cases), is not fixed here, and adding `and` to the alternation extends the
// same surface by exactly its `and` mirror — no new class.
//
// So this row pins PARITY, not correctness: whatever the scanner does with a
// scrubbed-context `let` head it must also do with an `and` head. It is
// deliberately NOT an assertion that the ghost is minted — pinning the gap as
// correct is what #7135 arm 3 declined to do, and a future fix to #7152 that
// silences BOTH keywords must leave this test green while a fix that silences
// only one must fail it.
func TestActivePatternAndContinued_ScrubResidualParityWithLet(t *testing.T) {
	for _, ctx := range []struct {
		label, before, after string
	}{
		{"block comment", "(*\n", "*)\n"},
		{"triple-quoted literal", "let doc = \"\"\"\n", "\"\"\"\n"},
	} {
		t.Run(ctx.label, func(t *testing.T) {
			mint := func(keyword string) bool {
				src := "module Patterns\n\n" +
					"let rec (|Even|Odd|) x =\n    1\n" +
					ctx.before +
					keyword + " (|Ghost|Ghoul|) z =\n    3\n" +
					ctx.after +
					"\nlet sentinelValue = 0\n"
				return fsFind(runFSharp(t, src, "patterns.fs"), "(|Ghost|Ghoul|)", "SCOPE.Pattern") != nil
			}
			gotLet, gotAnd := mint("let"), mint("and")
			if gotLet != gotAnd {
				t.Errorf("inside a %s, a line-start `let (|Ghost|Ghoul|)` mints=%v but an "+
					"`and (|Ghost|Ghoul|)` mints=%v. The two head keywords share one anchor and "+
					"one raw-src call-site (#7152); they must share its residual too. A #7152 fix "+
					"that reaches only one keyword is exactly what this row exists to catch.",
					ctx.label, gotLet, gotAnd)
			}
		})
	}
}
