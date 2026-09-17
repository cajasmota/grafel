package fsharp_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7163 — `letRE` in extractor.go mints a PHANTOM `SCOPE.Operation`/`let`
// named after a MODIFIER when it meets an active-pattern head.
//
// MECHANISM (re-derived from the pattern on disk, not inherited). letRE is
//
//	(?m)^([ \t]*)let(?:\s+(?:rec|mutable|inline|private|internal|public)\b)*
//	    \s+([a-zA-Z_][a-zA-Z0-9_']*)\s*(?:<[^>]*>)?\s*(?:[^=\n]*)=
//
// The modifier group is REPEATED and therefore optional. On
// `let rec (|Even|Odd|) x = 1` the engine first takes ` rec` as one
// repetition, then the mandatory `\s+` plus the NAME capture
// `[a-zA-Z_][a-zA-Z0-9_']*` face `(` and cannot proceed — so the engine
// GIVES BACK the last repetition, `\s+` takes the space after `let`, and the
// NAME capture takes the word `rec`. `(|Even|Odd|) x ` is then swallowed by
// the trailing `[^=\n]*` and the `=` matches. The captured name is always the
// LAST modifier in the phrase, because only the last repetition is given
// back: `let inline private (|Foo|_|) y = 2` yields `private`, having KEPT
// the ` inline` repetition. That partial backtrack is the row a hand-picked
// single-modifier fixture would miss, and it is pinned explicitly below.
//
// With NO modifier there is nothing to give back, so the mandatory `\s+`
// leaves the name capture facing `(` and letRE correctly declines. The defect
// is therefore modifier-DEPENDENT, and every row here that carries no
// modifier is a control on that.
//
// CONSEQUENCE, and why a must-have row cannot see it. The SCOPE.Pattern
// census is CORRECT in every row: the phantom is an EXTRA entity beside a
// correct one, so this is the over-firing direction and only a FORBIDDEN row
// detects it. `TestActivePatternModifiers_NeverMissed` (#7135 arm 3) passes
// straight through the phantom, and says so in its own header.
//
// WHY THE FORBIDDEN NAME IS UNIQUE IN EVERY FIXTURE. Both F# `let`-side dedup
// keys are name-keyed — `letSeenKey(indent, name)` is `indent + ":let:" +
// name` — so a phantom that shares a real declaration's name MERGES with it
// and becomes invisible to both a presence check and a count check (#7144,
// #7152). Every fixture below therefore names its one real `let` binding
// `sentinelValue`, which is not in any modifier allowlist, and declares no
// binding whose name is a modifier word. That is also why the census
// assertion is an EXACT list rather than a floor.
//
// A LEGITIMATE BINDING NAMED `rec`/`private`/… CANNOT EXIST, so suppressing
// these names costs no recall. Derived, NOT executed — there is no F#
// toolchain in this environment (`dotnet`, `fsc`, `fsharpc`, `fsi`, `mono`
// all absent; `javac` is the only compiler present), so this rests on the
// F# Language Specification § 3.4 "Identifiers and Keywords", whose
// `ident-keyword` list contains `rec`, `mutable`, `inline`, `private`,
// `internal`, `public` (corroborated by MS Learn "Keyword Reference",
// https://learn.microsoft.com/dotnet/fsharp/language-reference/keyword-reference).
// A keyword is not an `ident`, and `function-defn`/`value-defn` (§ 14.6)
// bind an `ident-or-op`, so `let rec = 1` cannot be written. The escaped form
// ``let ``rec`` = 1`` IS legal, but its source text contains backticks and so
// never reaches letRE's name class at all — no row here forbids it.
//
// Axes VARIED: modifier identity (the full six-word allowlist), modifier
// COUNT (0, 1, 2), modifier ORDER (both orders of every cross-kind pair,
// plus each access modifier repeated), banana-clip shape (two-case, three-
// case, partial, parameterised-partial, no-argument), indentation (top level
// vs nested, via TestActivePatternLetPhantom_Indented), and the number of
// modifier-carrying active patterns per file (one, vs two at the same indent
// in TestActivePatternLetPhantom_NoDedupCollapse).
//
// Axes HELD CONSTANT: the case names (`Even`/`Odd`, `Zero`/`Pos`/`Neg`,
// `Positive`, `DivisibleBy`, `On`/`Off` — so a wrong capture is
// unambiguous); the real binding's name (`sentinelValue`); the enclosing
// `module Patterns`; the entity Kinds under assertion (SCOPE.Operation for
// the forbidden half, SCOPE.Pattern + SCOPE.Schema for the required half);
// and the file path (`patterns.fs`).

// fsPhantomForbiddenNames are the words that must never name a
// SCOPE.Operation. It is letRE's own allowlist plus `let` itself, which would
// indicate the keyword leaking into the name capture. It is deliberately a
// SUPERSET of the phrases enumerated, so a future widening of letRE's
// allowlist that re-opens the defect for a word no row generates is still
// caught by the explicit rows that do carry it.
var fsPhantomForbiddenNames = []string{
	"rec", "mutable", "inline", "public", "private", "internal", "let",
}

// fsOpCensus lists "subtype/name" for every SCOPE.Operation, in order. It
// covers `let` AND `member` subtypes on purpose: the assertion is that no
// OPERATION is minted under a modifier's name, and scoping the census to the
// `let` subtype would let a fix that merely re-subtyped the phantom pass.
func fsOpCensus(ents []types.EntityRecord) []string {
	var out []string
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" {
			out = append(out, ents[i].Subtype+"/"+ents[i].Name)
		}
	}
	return out
}

// fsPhantomAssertNoPhantom is the FORBIDDEN half: no SCOPE.Operation carries
// a modifier word as its Name.
func fsPhantomAssertNoPhantom(t *testing.T, ents []types.EntityRecord, decl string, g fsGrammar) {
	t.Helper()
	for _, bad := range fsPhantomForbiddenNames {
		for i := range ents {
			if ents[i].Kind != "SCOPE.Operation" {
				continue
			}
			if ents[i].Name == bad {
				t.Errorf("phantom operation named %q (%s/%s, line %d) minted for %q [%s]; census=%v",
					bad, ents[i].Kind, ents[i].Subtype, ents[i].StartLine, decl, g, fsOpCensus(ents))
			}
		}
	}
}

// fsPhantomAssertPatternMinted is the REQUIRED half, asserted SEPARATELY from
// the forbidden half so that a "fix" which suppresses the phantom by
// suppressing the whole line is caught. It checks the active pattern itself
// AND its case set — the two entities the correct scanner produces.
func fsPhantomAssertPatternMinted(
	t *testing.T, ents []types.EntityRecord, decl, wantName, wantSubtype string, wantCases []string,
) {
	t.Helper()
	got := fsAPNames(ents)
	if len(got) != 1 || got[0] != wantName {
		t.Errorf("%q produced active patterns %v, want exactly [%s]", decl, got, wantName)
	}
	ap := fsFind(ents, wantName, "SCOPE.Pattern")
	if ap == nil {
		t.Errorf("%q: no SCOPE.Pattern %q", decl, wantName)
		return
	}
	if ap.Subtype != wantSubtype {
		t.Errorf("%q: pattern subtype=%q, want %q", decl, ap.Subtype, wantSubtype)
	}
	var wantCaseNames []string
	for _, c := range wantCases {
		wantCaseNames = append(wantCaseNames, wantName+"."+c)
	}
	gotCases := fsAPCaseNames(ents)
	if strings.Join(gotCases, ",") != strings.Join(wantCaseNames, ",") {
		t.Errorf("%q: case entities %v, want %v", decl, gotCases, wantCaseNames)
	}
}

// fsPhantomSource builds a fixture holding exactly one active-pattern
// declaration of interest plus one real `let` binding whose name
// (`sentinelValue`) collides with nothing.
func fsPhantomSource(decl string) string {
	return "module Patterns\n\n" + decl + "\n    failwith \"x\"\n\nlet sentinelValue = 0\n"
}

// TestActivePatternLetPhantom_IssueRows pins the four shapes measured on the
// issue, with the EXPECTED PHANTOM NAME named per row rather than only
// forbidden as a set. The `inline private` row is the partial-backtrack
// proof: the phantom was `private` (the LAST modifier), not `inline` (the
// first), which is what distinguishes "the group gave back its last
// repetition" from "the group matched zero repetitions".
func TestActivePatternLetPhantom_IssueRows(t *testing.T) {
	rows := []struct {
		label       string
		grammar     fsGrammar
		decl        string
		wasPhantom  string // the name minted before the fix ("" = none)
		wantName    string
		wantSubtype string
		wantCases   []string
	}{
		{
			label: "no modifier (control: never defective)", grammar: specLegal,
			decl: `let (|Even|Odd|) x =`, wasPhantom: "",
			wantName: "(|Even|Odd|)", wantSubtype: "active_pattern",
			wantCases: []string{"Even", "Odd"},
		},
		{
			label: "rec", grammar: specLegal,
			decl: `let rec (|Even|Odd|) x =`, wasPhantom: "rec",
			wantName: "(|Even|Odd|)", wantSubtype: "active_pattern",
			wantCases: []string{"Even", "Odd"},
		},
		{
			label: "private", grammar: specLegal,
			decl: `let private (|Even|Odd|) x =`, wasPhantom: "private",
			wantName: "(|Even|Odd|)", wantSubtype: "active_pattern",
			wantCases: []string{"Even", "Odd"},
		},
		{
			label: "inline private (PARTIAL backtrack)", grammar: specLegal,
			decl: `let inline private (|Foo|_|) y =`, wasPhantom: "private",
			wantName: "(|Foo|_|)", wantSubtype: "partial_active_pattern",
			wantCases: []string{"Foo"},
		},
	}
	for _, r := range rows {
		t.Run(r.label, func(t *testing.T) {
			ents := runFSharp(t, fsPhantomSource(r.decl), "patterns.fs")

			// FORBIDDEN half, per-row and by name: the specific word the
			// pre-fix extractor minted must not appear as an operation.
			if r.wasPhantom != "" {
				if op := fsFindLet(ents, r.wasPhantom); op != nil {
					t.Errorf("%q minted the phantom operation %q at line %d (census=%v)",
						r.decl, r.wasPhantom, op.StartLine, fsOpCensus(ents))
				}
			}
			fsPhantomAssertNoPhantom(t, ents, r.decl, r.grammar)

			// The `let` census is EXACT: the only real binding is the
			// sentinel. An exact list, not a floor, so an extra phantom
			// under any name at all fails.
			if got := fsLetNames(ents); strings.Join(got, ",") != "sentinelValue" {
				t.Errorf("%q: let census=%v, want [sentinelValue]", r.decl, got)
			}

			// REQUIRED half, asserted separately.
			fsPhantomAssertPatternMinted(t, ents, r.decl, r.wantName, r.wantSubtype, r.wantCases)
		})
	}
}

// TestActivePatternLetPhantom_ModifierSpace enumerates the modifier space as
// the CROSS PRODUCT of letRE's allowlist (via the shared `modifierPhrases`
// generator — the empty phrase, each modifier alone, BOTH orders of every
// cross-kind pair, and each access modifier repeated) with the banana-clip
// shapes, rather than hand-picking. Hand-picking is what left the partial
// backtrack undetected on #7135 arm 3.
//
// Both halves are asserted on every row: no phantom operation, AND the
// active pattern with its cases still minted.
func TestActivePatternLetPhantom_ModifierSpace(t *testing.T) {
	for _, shape := range fsAPClipShapes {
		for _, mp := range modifierPhrases(fsAccess, fsAPExtra) {
			g := fsAPLabel(mp.phrase)
			decl := strings.TrimRight("let "+mp.phrase, " ") +
				" (" + shape.clip + ")" + shape.args + "="
			t.Run(fmt.Sprintf("%s/%s", shape.label, mp.phrase), func(t *testing.T) {
				ents := runFSharp(t, fsPhantomSource(decl), "patterns.fs")

				fsPhantomAssertNoPhantom(t, ents, decl, g)
				if got := fsLetNames(ents); strings.Join(got, ",") != "sentinelValue" {
					t.Errorf("%q [%s]: let census=%v, want [sentinelValue]", decl, g, got)
				}
				fsPhantomAssertPatternMinted(t, ents, decl, shape.name, shape.subtype, shape.cases)
			})
		}
	}
}

// TestActivePatternLetPhantom_Indented varies the one axis the tables above
// hold constant: indentation. `letSeenKey` puts the indent in the dedup key,
// so a nested declaration keys differently and a fix that only reached column
// zero would pass everything above.
func TestActivePatternLetPhantom_Indented(t *testing.T) {
	for _, mp := range []string{"rec", "private", "inline private"} {
		t.Run(mp, func(t *testing.T) {
			decl := "    let " + mp + " (|Even|Odd|) x ="
			src := "module Patterns\n\nlet outerValue =\n" + decl +
				"\n        failwith \"x\"\n    0\n"
			ents := runFSharp(t, src, "patterns.fs")
			fsPhantomAssertNoPhantom(t, ents, decl, specLegal)
			if got := fsLetNames(ents); strings.Join(got, ",") != "outerValue" {
				t.Errorf("%q: let census=%v, want [outerValue]", decl, got)
			}
			fsPhantomAssertPatternMinted(t, ents, decl, "(|Even|Odd|)", "active_pattern",
				[]string{"Even", "Odd"})
		})
	}
}

// TestActivePatternLetPhantom_NoDedupCollapse is the collision direction the
// issue calls out. `letSeen` is keyed `indent + ":let:" + name`, so two
// same-indent active patterns carrying the SAME modifier both keyed on that
// modifier and the second phantom was silently dropped — one phantom stood in
// for two declarations. A per-file count of one is therefore NOT evidence
// that the second declaration is clean.
//
// Asserted in both directions: zero phantoms, and BOTH active patterns
// (and all four case entities) still minted.
func TestActivePatternLetPhantom_NoDedupCollapse(t *testing.T) {
	for _, tc := range []struct {
		label    string
		modA     string
		modB     string
		wantOnce []string // the phantom names the pre-fix extractor collapsed onto
	}{
		{"same modifier twice", "private", "private", []string{"private"}},
		{"different modifiers", "private", "internal", []string{"private", "internal"}},
		{"partial backtrack twice", "inline private", "rec private", []string{"private"}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			src := "module Patterns\n\n" +
				"let " + tc.modA + " (|Even|Odd|) x =\n    failwith \"x\"\n\n" +
				"let " + tc.modB + " (|On|Off|) y =\n    failwith \"y\"\n\n" +
				"let sentinelValue = 0\n"
			ents := runFSharp(t, src, "patterns.fs")

			for _, bad := range tc.wantOnce {
				if op := fsFindLet(ents, bad); op != nil {
					t.Errorf("phantom %q minted at line %d (census=%v)",
						bad, op.StartLine, fsOpCensus(ents))
				}
			}
			fsPhantomAssertNoPhantom(t, ents, src, specLegal)
			if got := fsLetNames(ents); strings.Join(got, ",") != "sentinelValue" {
				t.Errorf("let census=%v, want [sentinelValue]", got)
			}

			// REQUIRED half: two patterns, four cases, neither collapsed.
			if got := fsAPNames(ents); strings.Join(got, ",") != "(|Even|Odd|),(|On|Off|)" {
				t.Errorf("active patterns=%v, want [(|Even|Odd|) (|On|Off|)]", got)
			}
			wantCases := "(|Even|Odd|).Even,(|Even|Odd|).Odd,(|On|Off|).On,(|On|Off|).Off"
			if got := strings.Join(fsAPCaseNames(ents), ","); got != wantCases {
				t.Errorf("case entities=%q, want %q", got, wantCases)
			}
		})
	}
}

// TestActivePatternLetPhantom_PlainLetStillExtracted is the too-strict
// direction: the guard must not cost a plain `let` binding, a tuple-pattern
// head, or a parenthesised operator definition its entity. Those last two are
// the shapes a guard written as "reject any head containing a `(`" would
// silently drop, and a silent miss is the worse failure mode.
//
// Each row states what letRE ALREADY did before the fix, measured on the
// pre-change file — not what it ought to do. `let (x, y) = …` and
// `let (+.) a b = …` were NEVER extracted by letRE (its name class excludes
// `(`), so their rows assert an unchanged ABSENCE, and are controls on the
// guard not becoming a fix for a different issue by accident.
func TestActivePatternLetPhantom_PlainLetStillExtracted(t *testing.T) {
	for _, tc := range []struct {
		label     string
		decl      string
		wantNames string // exact `let` census, sentinel included
	}{
		{"plain", `let target x = x`, "target,sentinelValue"},
		{"rec", `let rec target x = target x`, "target,sentinelValue"},
		{"inline private", `let inline private target x = x`, "target,sentinelValue"},
		{"generic", `let inline target<'T> (x: 'T) = x`, "target,sentinelValue"},
		// Pre-existing absences, unchanged by this fix (measured, not desired):
		{"tuple head", `let (x, y) = (1, 2)`, "sentinelValue"},
		{"operator defn", `let (+.) a b = a + b`, "sentinelValue"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			src := "module Values\n\n" + tc.decl + "\n\nlet sentinelValue = 0\n"
			ents := runFSharp(t, src, "values.fs")
			if got := strings.Join(fsLetNames(ents), ","); got != tc.wantNames {
				t.Errorf("%q: let census=%q, want %q", tc.decl, got, tc.wantNames)
			}
			fsPhantomAssertNoPhantom(t, ents, tc.decl, specLegal)
		})
	}
}
