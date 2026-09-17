package fsharp_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7135, THIRD AND LAST ARM — `activePatternRE` in
// compexpr_active_patterns.go accommodated a FIXED modifier sequence of
// exactly two optional words, in exactly one order:
//
//	let(?:\s+rec)?(?:\s+inline)?\s+\(\|...
//
// so any ACCESS modifier made the whole active-pattern definition vanish.
// Measured against the extractor at 45f9745d0, BEFORE this change:
//
//	let (|Even|Odd|) n = ...            -> 1 SCOPE.Pattern  (accommodated)
//	let rec (|Even|Odd|) n = ...        -> 1 SCOPE.Pattern  (accommodated)
//	let inline (|Even|Odd|) n = ...     -> 1 SCOPE.Pattern  (accommodated)
//	let rec inline (|Even|Odd|) n = ... -> 1 SCOPE.Pattern  (accommodated)
//	let private (|Even|Odd|) n = ...    -> 0 entities       (want 1)
//	let internal (|Even|Odd|) n = ...   -> 0 entities       (want 1)
//	let public (|Even|Odd|) n = ...     -> 0 entities       (want 1)
//	let inline rec (|Even|Odd|) n = ... -> 0 entities       (want 1; ORDER)
//	let private (|Positive|_|) n = ...  -> 0 entities       (want 1)
//
// THE FAILURE MODE, stated precisely, because the obvious phrasing is
// measurably WRONG. It is NOT "no entity, no edge, no diagnostic": pre-fix,
// `let private (|Even|Odd|) n =` minted a `SCOPE.Operation`/`let` named
// **`private`** on that very line. What was lost is
//
//	no active-pattern entity, no case sub-entity, no CONTAINS edge, no
//	match-site USES edge, and no diagnostic — only letRE's phantom (#7163).
//
// The active-pattern census was 0, which is what every table below asserts;
// but the LINE was not silent, it was mis-attributed. The first draft of this
// header said "no entity, no edge, no diagnostic" — inherited from #7135's
// title, where it is true of the `module` and `type` arms — and contradicted
// its own #7163 note four paragraphs later. Measured, not reasoned: see the
// probe table at fsAPNoModifierNamedEntity.
//
// It is also strictly worse here than at the `module`/`type` sites, because
// an active-pattern definition is the ONLY thing that mints the case
// sub-entities — so a missed definition takes its whole case set with it, and
// every match arm `| Even ->` in the file then resolves to nothing. Every
// assertion below is therefore on the entity COUNT, because an absence is
// exactly what a presence-only assertion cannot see.
//
// WHY THE ACTIVE PATTERN IS MISSED RATHER THAN MIS-NAMED. Unlike `memberRE`
// (arm 1), THIS pattern has nothing a modifier could be captured into: the
// literal `\(\|` after the separator has to match, and `private ` does not
// begin a banana clip, so the match is abandoned altogether. The mis-naming
// that does happen on the line comes from a DIFFERENT scanner (letRE), whose
// name capture can fall back onto the modifier — #7163.
//
// WHICH SOURCES WERE UNIONED. An active pattern IS a `let` binding — § 10.5's
// `access` applies to it as to any value/function definition — so the set is
// the same one letRE (#7131) already carries. Four sources:
//
//  1. F# Language Specification § 10.5 "Accessibility Annotations"
//     (https://fsharp.github.io/fslang-spec/namespaces-and-modules/):
//     `access := public | private | internal`. There is no `protected`.
//  2. MS Learn "Access Control" (learn.microsoft.com/dotnet/fsharp/
//     language-reference/access-control): the specifiers "can be applied to
//     modules, types, methods, value definitions, functions, properties, and
//     explicit fields" — an active pattern is a function definition — and
//     "The access specifier is put in front of the name of the entity". The
//     SAME page states `protected` "is not used in F#", so it is
//     deliberately NOT allowlisted, repo-wide.
//  3. MS Learn "Active Patterns" (learn.microsoft.com/dotnet/fsharp/
//     language-reference/active-patterns), whose syntax block is
//     `let (|identifier|_|) [ arguments ] = expression`, i.e. an ordinary
//     `let` head — the construct carries no modifier vocabulary of its own.
//     That is the reason this arm does not need a fourth, separate
//     allowlist: it needs letRE's.
//  4. The sibling scanner letRE in extractor.go, which allowlists
//     `rec|mutable|inline|private|internal|public`.
//
// AM I MATCHING letRE, AND WHY. Yes — byte-for-byte the same alternation,
// deliberately, including `mutable`. Two reasons. (a) This scanner and letRE
// consume the SAME grammar production (`let`, § 14.6) and differ only in the
// binding-pattern head, so a divergent allowlist would be a twinned surface
// that drifts — the exact failure this three-arm issue is about, and the
// reason `typeKindRE` was left un-widened and filed (#7153) rather than
// hand-tuned. (b) The divergence would buy nothing measurable: `mutable` is
// unreachable on THIS sub-form, on three independent grounds — the spec
// grammar admits `mutable` only in `value-defn := mutable? access? pat`,
// while a parameterised active-pattern head is a
// `function-defn := inline? access? ident-or-op …` with NO `mutable` slot at
// all; `FSComp.txt` **874**, "Only record fields and simple, non-recursive
// 'let' bindings may be marked mutable"; and `FSComp.txt` **831**, "Mutable
// function values should be written 'let mutable f = (fun args ...)'". So no
// compiling F# source produces the form, which means excluding the word
// removes no real-world case and admitting it mis-extracts none. It is
// LENIENCE, labelled per row as `lenienceSubform` — not a claim that the row
// is legal F#. A fabricated modifier is a widening with no real-world case
// and an all-DEAD mutant score cannot detect one, so the boundary that DOES
// bite is graded separately: see
// TestActivePatternModifiers_AllowlistUpperBoundary.
//
// THE REPEATING GROUP IS RECALL FIRST, LENIENCE SECOND — and the first draft
// of this file had that backwards. `function-defn := inline? access?
// ident-or-op …` (with `rec` supplied by the `let rec` group ahead of it)
// makes `inline private` and `rec private` the SPEC-LEGAL order, not a
// reversed one. Measured: `let inline private (|Even|Odd|) n =` and
// `let rec private (|Even|Odd|) n =` match the new pattern and do NOT match
// the old two-slot form. So two-modifier LEGAL F# is exactly what the old
// pattern missed, and `)*` is required for recall — it is not merely
// tolerated. Six phrases x 5 clip shapes = 30 subtests were mislabelled
// `lenienceOnly` here; fsAPLabel below re-derives the label from the
// grammar's modifier ORDER instead of inheriting it, because the shared
// `modifierPhrases` helper (arm 2, 0ed6767d7) labels BOTH orders as reversed.
// Correcting the shared helper would relabel arm 2's tables too and wants its
// own commit; this arm simply stops repeating the claim.
//
// WHAT IS GENUINELY LENIENCE, stated rather than left silent. The group still
// accepts an ARBITRARY MULTISET in any order, so `let private public (|A|B|)`
// (two access modifiers), `let private rec (|A|B|)` (the reversed order) and
// `let rec rec (|A|B|)` all match while the grammar admits one access
// modifier and one `rec` in a fixed order. Same call as the `module`/`type`
// arm (0ed6767d7): unreachable from compiling source, tolerated on purpose.
// 16 subtests DEPEND on that lenience — the three `a a` phrases x 5 clip
// shapes in the recall table, plus `GroupIsRepeating/private private` — so it
// is load-bearing for the suite, not incidental, and every such row carries
// an `fsGrammar` label naming which kind of lenience it pins.
//
// `\b` AFTER EACH MODIFIER WORD is not load-bearing, and here — unlike at
// both sibling patterns — it is UNCONDITIONALLY so rather than conditional on
// the mandatory `\s+`: `\b` can only bite when an allowlist word is followed
// by a WORD character, and the next obligatory token after the modifier group
// is the literal `(` of the banana clip. Measured over 11,924,640 enumerated
// declaration lines under EACH separator (`\s+` and the #7158 `\s*`
// relaxation), 0 divergences in all four cells, plus every single byte after
// each of the six words. The mutant is therefore ALIVE + EQUIVALENT and is
// left untested ON PURPOSE — it is not a gap to fill with a fixture. The
// SEPARATOR itself is still ungraded (ALIVE at 0 `--- FAIL`), as at both
// sibling sites; that is **#7158** and this arm does not re-litigate it. Its
// consequence differs by site, which is why no row pins it here: relaxed, it
// can only ADMIT `let(|Even|Odd|)` with no space, and this environment has no
// F# toolchain to settle that form's legality.
//
// There is NO F# toolchain in this environment (`dotnet`, `fsc`, `fsharpc`,
// `fsi` and `mono` are all absent; `javac` is the only compiler present), so
// every legality statement here is DERIVED FROM THE SOURCES ABOVE AND NOT
// EXECUTED. No fixture below was compiled; the labels are per row, in all
// FOUR enumerated tables (NeverMissed, Indentation, MatchSite and
// GroupIsRepeating) — the first draft labelled only NeverMissed while three
// other tables carried unlabelled lenience rows. AllowlistUpperBoundary
// carries a per-row `reason` string naming the source of the exclusion
// instead of an `fsGrammar`, since every one of its rows is by construction
// not legal on a `let` binding.
//
// Axes VARIED: modifier identity (`rec`, `inline`, `mutable`, `public`,
// `private`, `internal`, and the EXCLUDED `protected`, `static`, `val`);
// modifier count (0, 1, 2); modifier ORDER (`rec private` vs `private rec`,
// `rec inline` vs `inline rec`); repetition of an access modifier
// (`private private`); clip shape (total two-case, total three-case, partial
// `|_|`); parameterisation (0, 1 and 2 arguments); indentation (0, 4 spaces
// and a tab); and the downstream consequence (case sub-entity + CONTAINS
// edge + match-site USES edge under a modifier).
//
// Axes deliberately NOT varied, so the labels claim no more than the fixtures
// contain: no three-modifier row (every phrase is one or two words); no
// `rec rec`/`inline inline` row (only the ACCESS words are repeated); and no
// two-same-name active patterns in one file — `extractActivePatterns`' `seen`
// map is keyed on the clip name ALONE with no enclosing scope, so a
// same-name sibling would be dropped. That is a pre-existing hole of the
// #7144 family, it reproduces with NO modifier at all, and it is neither
// fixed nor asserted as correct here.
//
// THIS WIDENING ENLARGES AN OPEN SIBLING DEFECT'S SURFACE, and saying so is
// part of shipping it. `extractActivePatterns` is called with the RAW `src`
// (extractor.go:522), not the comment/string-scrubbed copy the edge scanners
// use, so **#7152** applies here: a commented-out `let private (|A|B|)` at
// column 0 inside a `(* … *)` block or a `"""…"""` literal now mints a
// phantom where the two-slot form would have skipped it. The surface grows by
// exactly the declarations this arm newly recognises. #7152 is not fixed here
// — it is one shared call-site convention across all four F# declaration
// scanners, and changing it is its own arm — but this arm is one of its
// causes and Refs it for that reason.
//
// A SEPARATE PRE-EXISTING DEFECT found while enumerating this space, and
// deliberately NOT fixed here: `letRE` ALSO matches an active-pattern head
// that carries a modifier and mis-names it, minting a phantom
// SCOPE.Operation under the MODIFIER's name (`let rec (|Even|Odd|) n =`
// yields an operation called `rec`). It reproduces at 45f9745d0, before this
// change, on a declaration this scanner already extracted correctly — so it
// is neither caused nor cured by this commit. Filed as #7163. See the note on
// fsAPNoModifierNamedEntity for the mechanism and for why no row below pins
// it.
//
// Axes HELD CONSTANT: the case names (always `Even`/`Odd`, `Positive`, or
// `Zero`/`Pos`/`Neg`, so a wrong capture is unambiguous); the entity Kind
// (SCOPE.Pattern) and case Kind (SCOPE.Schema); one active-pattern
// declaration of interest per source file; and the file path.

// lenienceSubform is a fifth grammar label, distinct from the three that
// already exist. `lenienceOnly` pins a REVERSED modifier order and
// `lenienceRepetition` a REPEATED one; neither describes a word that is legal
// on the `let` production but not on this SUB-FORM of it. Mislabelling
// `mutable` as either would claim something about order or repetition that
// the row does not test.
const lenienceSubform fsGrammar = "NOT legal F# — legal on a `let` value binding, not on an active-pattern head; pins the allowlist shared with letRE"

// fsAPSubtypes is the set of subtypes extractActivePatterns can stamp.
var fsAPSubtypes = map[string]bool{
	"active_pattern": true, "partial_active_pattern": true,
}

// fsAPNames lists the Name of every active-pattern SCOPE.Pattern, in order.
// SCOPE.Pattern is reused by other producers, so an active-pattern census
// must name the subtypes rather than count the Kind.
func fsAPNames(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind == "SCOPE.Pattern" && fsAPSubtypes[ents[i].Subtype] {
			out = append(out, ents[i].Name)
		}
	}
	return out
}

// fsAPCaseNames lists the Name of every active-pattern case sub-entity.
func fsAPCaseNames(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind == "SCOPE.Schema" && ents[i].Subtype == "active_pattern_case" {
			out = append(out, ents[i].Name)
		}
	}
	return out
}

// fsAPModifierWords are the words that must never become an entity name —
// the complementary direction to recall. A count-and-name check on
// `(|Even|Odd|)` cannot see an entity minted under a modifier's name, which
// is how arm 1 (`memberRE`) failed.
var fsAPModifierWords = []string{
	"rec", "mutable", "inline", "public", "private", "internal",
	"protected", "static", "val", "let",
}

// fsAPNoModifierNamedEntity asserts no modifier word became the name of an
// entity THIS SCANNER mints — SCOPE.Pattern (the definition) or SCOPE.Schema
// (a case sub-entity).
//
// It is deliberately scoped to those two Kinds rather than to every entity in
// the file, and the reason is a SEPARATE pre-existing defect this guard found
// while it was still unscoped: `letRE` in extractor.go ALSO matches an
// active-pattern head that carries a modifier, and mis-names it. Its repeated
// modifier group is optional, so on `let rec (|Even|Odd|) n =` the engine
// prefers `rec`, finds the name capture `[a-zA-Z_]...` cannot match `(`,
// backtracks the group to ZERO repetitions, and then takes `rec` as the NAME
// — minting a phantom `SCOPE.Operation`/`let` called `rec` at that line, with
// `(|Even|Odd|) n` swallowed by the trailing `[^=\n]*`.
//
// Measured at 45f9745d0, i.e. BEFORE this change: `let rec (|Even|Odd|) n =`
// already produced the correct SCOPE.Pattern AND the phantom operation named
// `rec`, so the phantom is not caused by this commit and is not removed by
// it. It is modifier-DEPENDENT (`let (|Even|Odd|) n =` with no modifier
// produces no phantom, because letRE's mandatory `\s+` then leaves the name
// capture facing `(`), which makes it an adjacent arm of this same issue
// family rather than an unrelated bug — filed as #7163 — but fixing it means
// changing a
// DIFFERENT scanner, whose accepted-name shape needs its own enumeration, and
// RE2 has no negative lookahead to express "a name that is not a banana
// clip". It is filed rather than fixed here, and NOT asserted as correct:
// there is no row below pinning the phantom's presence, so a future fix to
// letRE will not have to delete a test that blessed it.
func fsAPNoModifierNamedEntity(t *testing.T, ents []types.EntityRecord, decl string, g fsGrammar) {
	t.Helper()
	for _, bad := range fsAPModifierWords {
		for i := range ents {
			if ents[i].Kind != "SCOPE.Pattern" && ents[i].Kind != "SCOPE.Schema" {
				continue
			}
			if ents[i].Name == bad {
				t.Errorf("modifier %q recorded as an active-pattern entity name (%s/%s, line %d) for %q [%s]",
					bad, ents[i].Kind, ents[i].Subtype, ents[i].StartLine, decl, g)
			}
		}
	}
}

// fsAPExtra is the non-access half of letRE's allowlist. `mutable` is in it
// on purpose (see the header): it is lenience, and every row it generates is
// labelled.
var fsAPExtra = []string{"rec", "inline", "mutable"}

// fsAPModifierRank is the modifier's position in the grammar's fixed order:
// `let rec` supplies `rec` ahead of the binding, then
// `function-defn := inline? access? ident-or-op …` supplies `inline` and then
// `access`. A phrase is spec-legal iff its words are strictly increasing in
// rank. `mutable` has no rank on this sub-form at all — see fsAPLabel.
//
// Derived from the spec grammar and MS Learn, NOT executed: there is no F#
// toolchain here.
var fsAPModifierRank = map[string]int{
	"rec": 0, "inline": 1,
	"public": 2, "private": 2, "internal": 2,
}

// fsAPLabel re-derives a modifier phrase's grammar label from the ORDER of
// its words, rather than inheriting it.
//
// It exists because the shared `modifierPhrases` helper (arm 2, 0ed6767d7)
// labels BOTH orders of an `extra`+`access` pair `lenienceOnly`, and that is
// wrong for the `let` production: `inline private` and `rec private` are the
// SPEC-LEGAL order (`function-defn := inline? access? ident-or-op`), so 30
// subtests here were labelled "NOT legal F#" while being exactly the legal
// two-modifier forms the old two-slot pattern missed. Relabelling via the
// shared helper would move arm 2's labels too and wants its own commit.
//
// The four dispositions:
//   - any phrase containing `mutable` -> lenienceSubform (legal on a `let`
//     VALUE binding, absent from `function-defn` entirely);
//   - 0 or 1 words, or strictly increasing rank -> specLegal;
//   - two words of EQUAL rank (two access modifiers, repeated or not)
//     -> lenienceRepetition;
//   - otherwise (decreasing rank) -> lenienceOnly, a reversed order.
func fsAPLabel(phrase string) fsGrammar {
	w := strings.Fields(phrase)
	for _, x := range w {
		if x == "mutable" {
			return lenienceSubform
		}
	}
	// Rank EVERY word before any shortcut. An earlier revision took a
	// `len(w) < 2 -> specLegal` shortcut first, which labelled a single
	// non-allowlisted word (`sealed`) as legal F# — caught by
	// TestActivePatternModifiers_LabelDerivation, which is the reason that
	// test exists: a label is only ever read from a t.Errorf format string,
	// so on a green run a wrong one is invisible.
	ranks := make([]int, 0, len(w))
	for _, x := range w {
		r, ok := fsAPModifierRank[x]
		if !ok {
			return lenienceOnly
		}
		ranks = append(ranks, r)
	}
	if len(ranks) < 2 {
		return specLegal
	}
	for i := 1; i < len(ranks); i++ {
		if ranks[i] == ranks[i-1] {
			return lenienceRepetition
		}
		if ranks[i] < ranks[i-1] {
			return lenienceOnly
		}
	}
	return specLegal
}

// fsAPClipShapes enumerates the banana-clip shapes a definition can take,
// crossed with the modifier space, because the clip is what the pattern must
// still match AFTER the modifier group consumes its words — a fix that only
// reached the two-case total form would leave the partial and three-case
// forms missed.
var fsAPClipShapes = []struct {
	label   string
	clip    string // as written, between `(` and `)`
	name    string // expected entity Name
	subtype string
	args    string // between the clip and `=`
	cases   []string
}{
	{"total2", "|Even|Odd|", "(|Even|Odd|)", "active_pattern", " n ", []string{"Even", "Odd"}},
	{"total3", "|Zero|Pos|Neg|", "(|Zero|Pos|Neg|)", "active_pattern", " n ", []string{"Zero", "Pos", "Neg"}},
	{"partial", "|Positive|_|", "(|Positive|_|)", "partial_active_pattern", " n ", []string{"Positive"}},
	{"partial/2args", "|DivisibleBy|_|", "(|DivisibleBy|_|)", "partial_active_pattern", " divisor n ", []string{"DivisibleBy"}},
	{"total2/noargs", "|On|Off|", "(|On|Off|)", "active_pattern", " ", []string{"On", "Off"}},
}

// TestActivePatternModifiers_NeverMissed enumerates the modifier space as the
// CROSS PRODUCT of letRE's allowlist (via the shared modifierPhrases
// generator: the empty case, each modifier alone, BOTH orders of every
// cross-kind pair, and each access modifier repeated) crossed with the clip
// shapes. Each row must yield EXACTLY ONE active-pattern entity under the
// banana-clipped name. The COUNT is the load-bearing assertion:
// `let private (|Even|Odd|)` produced zero entities before this change, and
// zero is invisible to a presence-only check.
//
// Hand-picking is how a two-slot fixed sequence came to be written in the
// first place, at all three sites in this issue.
func TestActivePatternModifiers_NeverMissed(t *testing.T) {
	for _, shape := range fsAPClipShapes {
		for _, mp := range modifierPhrases(fsAccess, fsAPExtra) {
			// Re-derive the label from the grammar's modifier ORDER
			// rather than inheriting mp.grammar, which labels both
			// orders of an extra+access pair as reversed — see
			// fsAPLabel. `rec private` / `inline private` are the
			// LEGAL order and are exactly what the old pattern missed.
			g := fsAPLabel(mp.phrase)
			decl := strings.TrimRight("let "+mp.phrase, " ") +
				" (" + shape.clip + ")" + shape.args + "="
			t.Run(fmt.Sprintf("%s/%s", shape.label, mp.phrase), func(t *testing.T) {
				src := "module Patterns\n\n" + decl + "\n    failwith \"x\"\n"
				ents := runFSharp(t, src, "patterns.fs")
				got := fsAPNames(ents)
				if len(got) != 1 {
					t.Errorf("%q [%s] produced %d active-pattern entities, want 1: %v",
						decl, g, len(got), got)
				}
				if len(got) != 1 || got[0] != shape.name {
					t.Errorf("%q [%s] named the pattern %v, want [%s]", decl, g, got, shape.name)
				}
				ap := fsFind(ents, shape.name, "SCOPE.Pattern")
				if ap == nil {
					t.Fatalf("%q [%s]: no SCOPE.Pattern %q", decl, g, shape.name)
				}
				if ap.Subtype != shape.subtype {
					t.Errorf("%q [%s] subtype=%q, want %q", decl, g, ap.Subtype, shape.subtype)
				}
				if want := strings.Join(shape.cases, ","); ap.Properties["active_pattern_cases"] != want {
					t.Errorf("%q [%s] cases=%q, want %q", decl, g,
						ap.Properties["active_pattern_cases"], want)
				}
				// The downstream consequence a missed definition takes with
				// it: the case sub-entities and their CONTAINS edges.
				if n := len(fsAPCaseNames(ents)); n != len(shape.cases) {
					t.Errorf("%q [%s] produced %d case sub-entities, want %d: %v",
						decl, g, n, len(shape.cases), fsAPCaseNames(ents))
				}
				for _, c := range shape.cases {
					dotted := shape.name + "." + c
					if fsFind(ents, dotted, "SCOPE.Schema") == nil {
						t.Errorf("%q [%s]: missing case sub-entity %q", decl, g, dotted)
					}
					if !fsHasRel(ents, shape.name, "SCOPE.Pattern", "CONTAINS",
						fsSchemaRef("patterns.fs", dotted)) {
						t.Errorf("%q [%s]: missing CONTAINS edge to %q", decl, g, dotted)
					}
				}
				fsAPNoModifierNamedEntity(t, ents, decl, g)
			})
		}
	}
}

// TestActivePatternModifiers_Indentation varies the indentation axis, which
// the pattern CAPTURES (group 1) and which the other tables hold at column 0.
// A nested active pattern inside a module or a function body is the common
// real shape.
func TestActivePatternModifiers_Indentation(t *testing.T) {
	for _, indent := range []struct{ label, pad string }{
		{"col0", ""},
		{"spaces4", "    "},
		{"tab", "\t"},
	} {
		// `private rec` is the REVERSED order and is the one lenience row
		// in this table — labelled, like every other lenience row in the
		// file, so no row's label claims legality it does not have.
		for _, phrase := range []string{"", "private", "internal", "public", "rec private", "private rec"} {
			g := fsAPLabel(phrase)
			decl := strings.TrimRight("let "+phrase, " ") + " (|Even|Odd|) n ="
			t.Run(fmt.Sprintf("%s/%s", indent.label, phrase), func(t *testing.T) {
				src := "module Patterns\n\n" + indent.pad + decl + "\n" +
					indent.pad + "    failwith \"x\"\n"
				ents := runFSharp(t, src, "patterns.fs")
				got := fsAPNames(ents)
				if len(got) != 1 || got[0] != "(|Even|Odd|)" {
					t.Errorf("%q [%s] at %s produced %v, want [(|Even|Odd|)]",
						decl, g, indent.label, got)
				}
			})
		}
	}
}

// TestActivePatternModifiers_MatchSiteResolvesUnderAModifier grades the
// consequence that no count on the definition can reach: a match arm
// `| Even ->` resolves to the case sub-entity of a definition that carries an
// access modifier. Before this change the definition was missed, so
// `collectActivePatternCases` returned an EMPTY map and every match arm in
// the file resolved to nothing — a silent loss of the whole match-site edge
// class, not just of one entity.
func TestActivePatternModifiers_MatchSiteResolvesUnderAModifier(t *testing.T) {
	// Every phrase here is spec-legal, `inline private` included: it is the
	// `function-defn := inline? access? ident-or-op` order, not a reversed
	// one. Labelled anyway so the table states what it pins.
	for _, phrase := range []string{"", "private", "internal", "public", "inline private"} {
		g := fsAPLabel(phrase)
		t.Run(phrase, func(t *testing.T) {
			decl := strings.TrimRight("let "+phrase, " ") + " (|Even|Odd|) n ="
			src := "module Patterns\n\n" + decl + `
    if n % 2 = 0 then Even else Odd

let describe n =
    match n with
    | Even -> "even"
    | Odd -> "odd"
`
			ents := runFSharp(t, src, "patterns.fs")
			if got := fsAPNames(ents); len(got) != 1 {
				t.Fatalf("%q [%s] produced %d active-pattern entities, want 1: %v",
					decl, g, len(got), got)
			}
			for _, c := range []string{"Even", "Odd"} {
				dotted := "(|Even|Odd|)." + c
				if !fsHasRel(ents, "describe", "SCOPE.Operation", "USES",
					fsSchemaRef("patterns.fs", dotted)) {
					t.Errorf("%q [%s]: match arm %q did not resolve to %q", decl, g, c, dotted)
				}
			}
		})
	}
}

// fsAPForbidden are the words that must NOT be accepted between `let` and the
// banana clip. Each is paired with a POSITIVE CONTROL differing ONLY in the
// modifier word, so a forbidden row cannot pass because the harness silently
// stopped extracting anything at all.
//
// This is the direction that no count on the recall table can reach, and it
// is the direction that was the merge blocker on the sibling arm: there,
// `protected` and `rec`-on-types were excluded in PROSE only and three
// widening mutants survived the whole package suite at 0 `--- FAIL`.
var fsAPForbidden = []struct {
	word   string
	reason string
}{
	{"protected", "MS Learn Access Control: `protected` \"is not used in F#\" — excluded repo-wide"},
	{"static", "a MEMBER modifier (§ 8.13), not a `let`-binding one; letRE does not allowlist it either"},
	{"val", "a MEMBER/field modifier (§ 8.13 `val`), not a `let`-binding one"},
	{"abstract", "a MEMBER modifier (§ 8.13), not a `let`-binding one"},
	{"override", "a MEMBER modifier (§ 8.13), not a `let`-binding one"},
	{"qqzz", "not an F# keyword at all — the generic guard against an allowlist that accepts any word"},
}

// TestActivePatternModifiers_AllowlistUpperBoundary grades the allowlist's
// UPPER boundary. A word outside the allowlist must leave the declaration
// unmatched (0 entities), while the SAME declaration with an allowlisted word
// in that one position must mint exactly 1 — so the forbidden row's zero is
// attributable to the WORD and not to a broken fixture, a broken harness, or
// a missing registration.
func TestActivePatternModifiers_AllowlistUpperBoundary(t *testing.T) {
	const control = "private"
	for _, f := range fsAPForbidden {
		t.Run(f.word, func(t *testing.T) {
			body := " (|Even|Odd|) n =\n    if n % 2 = 0 then Even else Odd\n"

			// Positive control FIRST: identical source, allowlisted word.
			ctlSrc := "module Patterns\n\nlet " + control + body
			if got := fsAPNames(runFSharp(t, ctlSrc, "ctl.fs")); len(got) != 1 {
				t.Fatalf("positive control %q minted %d active patterns, want 1: %v — "+
					"the forbidden row below cannot be trusted until this passes",
					"let "+control+" (|Even|Odd|)", len(got), got)
			}

			// Forbidden row: same source, non-allowlisted word.
			badSrc := "module Patterns\n\nlet " + f.word + body
			if got := fsAPNames(runFSharp(t, badSrc, "bad.fs")); len(got) != 0 {
				t.Errorf("%q minted %d active patterns, want 0 (%s): %v",
					"let "+f.word+" (|Even|Odd|)", len(got), f.reason, got)
			}
		})
	}
}

// TestActivePatternModifiers_GroupIsRepeating grades the REPEATING property
// of the modifier group independently of its contents: a two-word phrase must
// still match. A non-repeating group `(?:\s+(?:...))?` passes every
// single-modifier row in the recall table and fails only here, so without
// this the shape of the fix would be ungraded even with the allowlist right.
func TestActivePatternModifiers_GroupIsRepeating(t *testing.T) {
	// Three of these are the SPEC-LEGAL order (`rec private`,
	// `inline internal`, `rec inline` — strictly increasing grammar rank) and
	// are therefore RECALL rows: the old two-slot pattern matched only
	// `rec inline`, so the other two are legal F# it missed outright. The
	// remaining four are lenience and are labelled as such —
	// `private private` is the file's only PURE-MULTISET row, and it is one
	// of the 16 subtests that depend on the group tolerating a multiset.
	for _, phrase := range []string{
		"rec private", "private rec", "inline internal", "internal inline",
		"rec inline", "inline rec", "private private",
	} {
		g := fsAPLabel(phrase)
		t.Run(phrase, func(t *testing.T) {
			src := "module Patterns\n\nlet " + phrase +
				" (|Even|Odd|) n =\n    if n % 2 = 0 then Even else Odd\n"
			got := fsAPNames(runFSharp(t, src, "patterns.fs"))
			if len(got) != 1 || got[0] != "(|Even|Odd|)" {
				t.Errorf("two-modifier phrase %q [%s] produced %v, want [(|Even|Odd|)] "+
					"— the modifier group must REPEAT, not be a single option",
					phrase, g, got)
			}
		})
	}
}

// TestActivePatternModifiers_LabelDerivation grades fsAPLabel itself.
//
// Every label in this file is consumed ONLY inside a t.Errorf format string,
// so on a green run a wrong label is invisible — which is precisely how 30
// subtests came to be labelled "NOT legal F#" while being the spec-legal
// order, and how that mislabel survived a full package suite plus a mutation
// round. A label is a claim about F#; if nothing observes it, it is prose.
//
// Each expectation cites its ground: `function-defn := inline? access?
// ident-or-op …` for the order, `let rec` for `rec`'s position ahead of it,
// `value-defn := mutable? access? pat` (plus FSComp 874 / 831) for
// `mutable`'s absence from a function head, and § 10.5 for the access set.
// Derived-not-executed — there is no F# toolchain here.
func TestActivePatternModifiers_LabelDerivation(t *testing.T) {
	for _, tc := range []struct {
		phrase string
		want   fsGrammar
		why    string
	}{
		{"", specLegal, "no modifier at all"},
		{"rec", specLegal, "`let rec`"},
		{"inline", specLegal, "function-defn `inline?`"},
		{"private", specLegal, "§ 10.5 access"},
		{"internal", specLegal, "§ 10.5 access"},
		{"public", specLegal, "§ 10.5 access"},

		// Increasing grammar rank: rec (0) -> inline (1) -> access (2).
		// These are the rows the first draft wrongly called reversed, and
		// they are exactly what the old two-slot pattern missed.
		{"rec inline", specLegal, "rec then inline, increasing rank"},
		{"rec private", specLegal, "rec then access — LEGAL, was mislabelled"},
		{"rec internal", specLegal, "rec then access — LEGAL, was mislabelled"},
		{"rec public", specLegal, "rec then access — LEGAL, was mislabelled"},
		{"inline private", specLegal, "function-defn `inline? access?` — LEGAL"},
		{"inline internal", specLegal, "function-defn `inline? access?` — LEGAL"},
		{"inline public", specLegal, "function-defn `inline? access?` — LEGAL"},

		// Decreasing rank: the genuinely reversed order.
		{"inline rec", lenienceOnly, "inline before rec, reversed"},
		{"private rec", lenienceOnly, "access before rec, reversed"},
		{"internal rec", lenienceOnly, "access before rec, reversed"},
		{"public rec", lenienceOnly, "access before rec, reversed"},
		{"private inline", lenienceOnly, "access before inline, reversed"},
		{"internal inline", lenienceOnly, "access before inline, reversed"},
		{"public inline", lenienceOnly, "access before inline, reversed"},

		// Equal rank: two access modifiers, which no source admits in any
		// order. This is the pure-multiset lenience.
		{"private private", lenienceRepetition, "repeated access modifier"},
		{"internal internal", lenienceRepetition, "repeated access modifier"},
		{"public public", lenienceRepetition, "repeated access modifier"},
		{"private public", lenienceRepetition, "two distinct access modifiers"},

		// `mutable` dominates in either position: it has no slot in
		// function-defn at all, so the row is sub-form lenience rather than
		// an ordering or repetition claim.
		{"mutable", lenienceSubform, "no `mutable` slot in function-defn"},
		{"mutable private", lenienceSubform, "mutable dominates the label"},
		{"private mutable", lenienceSubform, "mutable dominates the label"},
		{"rec mutable", lenienceSubform, "mutable dominates the label"},
		{"mutable mutable", lenienceSubform, "mutable dominates the label"},

		// A word outside the allowlist is never labelled legal.
		{"sealed", lenienceOnly, "not an allowlisted `let` modifier"},
		{"protected private", lenienceOnly, "not an allowlisted `let` modifier"},
	} {
		t.Run(tc.phrase, func(t *testing.T) {
			if got := fsAPLabel(tc.phrase); got != tc.want {
				t.Errorf("fsAPLabel(%q) = %q, want %q (%s)", tc.phrase, got, tc.want, tc.why)
			}
		})
	}

	// The four labels must be mutually distinct, or the table above could
	// pass while collapsing two dispositions into one.
	seen := map[fsGrammar]string{}
	for _, l := range []struct {
		g    fsGrammar
		name string
	}{
		{specLegal, "specLegal"}, {lenienceOnly, "lenienceOnly"},
		{lenienceRepetition, "lenienceRepetition"}, {lenienceSubform, "lenienceSubform"},
	} {
		if prev, dup := seen[l.g]; dup {
			t.Errorf("labels %s and %s are the same string %q — the label table above "+
				"cannot distinguish the two dispositions", prev, l.name, string(l.g))
		}
		seen[l.g] = l.name
	}
}
