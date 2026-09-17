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
// This is the SILENT-TOTAL-MISS mode: no entity, no edge, no diagnostic. It
// is also strictly worse here than at the `module`/`type` sites, because an
// active-pattern definition is the ONLY thing that mints the case
// sub-entities — so a missed definition silently takes its whole case set
// with it, and every match arm `| Even ->` in the file then resolves to
// nothing. The base let-scanner cannot compensate: letRE's name capture is
// `[a-zA-Z_][a-zA-Z0-9_']*` and the banana clip starts `(`, so a missed
// active pattern is not even mis-named — it is absent. Every assertion below
// is therefore on the entity COUNT, because an absence is exactly what a
// presence-only assertion cannot see.
//
// WHY A MISS AND NOT A MIS-NAME. Unlike `memberRE` (arm 1), this pattern has
// nothing a modifier could be captured INTO: the literal `\(\|` after the
// separator has to match, and `private ` does not begin a banana clip, so the
// match is abandoned altogether.
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
// unreachable on THIS sub-form (`let mutable (|Even|Odd|)` binds a pattern,
// not a storage location, so no F# source can produce it), which means
// excluding it removes no real-world case and adding it admits none. It is
// LENIENCE, labelled per row as `lenienceSubform` — not a claim that the row
// is legal F#. A fabricated modifier is a widening with no real-world case
// and an all-DEAD mutant score cannot detect one, so the boundary that DOES
// bite is graded separately: see
// TestActivePatternModifiers_AllowlistUpperBoundary.
//
// DELIBERATE LENIENCE, stated rather than left silent. The repeated group
// `(?:\s+(?:rec|mutable|inline|private|internal|public)\b)*` accepts an
// ARBITRARY MULTISET in ANY order, so `let private public (|A|B|)` and
// `let rec rec (|A|B|)` both match while the grammar admits one access
// modifier and one `rec`. That is the same call the `module`/`type` arm made
// (0ed6767d7) and it is made the same way here: unreachable from compiling
// source, tolerated on purpose, and labelled at the row level so no row's
// label claims legality it does not have.
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
// EXECUTED. No fixture below was compiled; the labels are per row.
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
			g := mp.grammar
			// `mutable` is legal on a let VALUE binding but not on an
			// active-pattern head, so any phrase containing it is
			// lenience on THIS sub-form regardless of order.
			if strings.Contains(mp.phrase, "mutable") {
				g = lenienceSubform
			}
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
		for _, phrase := range []string{"", "private", "internal", "public", "rec private", "private rec"} {
			decl := strings.TrimRight("let "+phrase, " ") + " (|Even|Odd|) n ="
			t.Run(fmt.Sprintf("%s/%s", indent.label, phrase), func(t *testing.T) {
				src := "module Patterns\n\n" + indent.pad + decl + "\n" +
					indent.pad + "    failwith \"x\"\n"
				ents := runFSharp(t, src, "patterns.fs")
				got := fsAPNames(ents)
				if len(got) != 1 || got[0] != "(|Even|Odd|)" {
					t.Errorf("%q at %s produced %v, want [(|Even|Odd|)]", decl, indent.label, got)
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
	for _, phrase := range []string{"", "private", "internal", "public", "inline private"} {
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
				t.Fatalf("%q produced %d active-pattern entities, want 1: %v", decl, len(got), got)
			}
			for _, c := range []string{"Even", "Odd"} {
				dotted := "(|Even|Odd|)." + c
				if !fsHasRel(ents, "describe", "SCOPE.Operation", "USES",
					fsSchemaRef("patterns.fs", dotted)) {
					t.Errorf("%q: match arm %q did not resolve to %q", decl, c, dotted)
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
	for _, phrase := range []string{
		"rec private", "private rec", "inline internal", "internal inline",
		"rec inline", "inline rec", "private private",
	} {
		t.Run(phrase, func(t *testing.T) {
			src := "module Patterns\n\nlet " + phrase +
				" (|Even|Odd|) n =\n    if n % 2 = 0 then Even else Odd\n"
			got := fsAPNames(runFSharp(t, src, "patterns.fs"))
			if len(got) != 1 || got[0] != "(|Even|Odd|)" {
				t.Errorf("two-modifier phrase %q produced %v, want [(|Even|Odd|)] "+
					"— the modifier group must REPEAT, not be a single option", phrase, got)
			}
		})
	}
}
