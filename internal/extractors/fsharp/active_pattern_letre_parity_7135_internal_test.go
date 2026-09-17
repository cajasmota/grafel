package fsharp

import (
	"regexp"
	"strings"
	"testing"
)

// #7135 arm 3 — the ALLOWLIST-PARITY assertion, and it exists because the
// justification it pins was prose-only.
//
// `activePatternRE`'s accepted modifier set is `letRE`'s, byte for byte, on
// the ground that an active pattern IS a `let` binding and a divergent
// allowlist would be a twinned surface that drifts. That claim is asserted in
// ~11 places across the two files and, before this test, was OBSERVED BY
// NOTHING.
//
// The consequence was not hypothetical. TestActivePatternModifiers_Allowlist-
// UpperBoundary grades a HAND-PICKED list of six forbidden words
// (`protected`, `static`, `val`, `abstract`, `override`, `qqzz`) — so every
// word NOT on that list was ungraded, and that is an UNBOUNDED class. Adding
// `sealed` to the group was measured **ALIVE at 0 `--- FAIL`** with `go vet`
// exiting 0, i.e. a compiling widening that the entire package suite could
// not see. Enumerating more words would close them one at a time and never
// close the class.
//
// This closes the class at once, in the only direction that can: instead of
// asking "is this particular word rejected?", it asks "is the modifier group
// still the SAME STRING as letRE's?". Any word added to one pattern and not
// the other — fabricated, misspelled, upper-case, or a real F# keyword that
// simply does not belong on a `let` — diverges the two prefixes and fails
// here. A word added to BOTH deliberately still passes, which is correct:
// parity, not a frozen allowlist, is the claim being made.
//
// This is an in-package (`package fsharp`) test because the two patterns are
// unexported. It asserts on `Regexp.String()` — the compiled pattern's own
// text, i.e. the artefact the engine actually uses — rather than on the
// source file's bytes, so a change made anywhere (a constant, a builder, a
// generated string) is still caught. That follows the repo rule to assert the
// emitted artefact and not a counter the code keeps about itself.
//
// Deliberately NOT asserted here: the CONTENT of the allowlist. Pinning the
// six words would freeze the set and make a legitimate coordinated widening
// (say, if a future F# release admits a new `let` modifier) fail in the test
// rather than in review, which is the wrong place for that conversation. The
// set's own upper boundary is graded by the forbidden rows in
// active_pattern_modifiers_7135_test.go; this test grades only that the two
// scanners agree.

// fsLetModifierGroupRE extracts, from a pattern's own `String()`, EVERY
// head-keyword + repeated-modifier-group occurrence — the substring the
// scanners are claimed to share. It matches the pattern TEXT, so `\s` here is
// a literal backslash followed by `s`, not a whitespace class. Submatch 1 is
// the modifier group ALONE, without the keyword, so occurrences headed by
// different keywords are directly comparable.
//
// #7166 WIDENED THIS FROM A LITERAL `let` TO `(?:let|and)`, AND FROM
// `FindString` TO ALL OCCURRENCES, and that is not cosmetic. activePatternRE
// now spells the group TWICE — once per head keyword — and a `FindString`
// anchored on the literal `let` always returned the FIRST occurrence, so the
// `and` branch's copy was compared to nothing: adding `sealed` to the `and`
// branch alone was measured ALIVE at 0 `--- FAIL` with `go vet` 0, while the
// identical widening on the `let` branch was DEAD. That is the exact
// unbounded-widening hole this test exists to close, re-opened on one branch
// of a duplicated allowlist.
//
// If a scanner's group is restructured so this no longer matches (an
// upper-case word breaking `[a-z|]+`, a non-repeating `)?`, a nested group),
// the extraction returns NOTHING and the test FAILS rather than silently
// comparing two empty strings against each other. That vacuity guard is the
// load-bearing half of this test: a parity assertion over two failed
// extractions would pass no matter what either pattern said.
var fsLetModifierGroupRE = regexp.MustCompile(`(?:let|and)(\(\?:\\s\+\(\?:[a-z|]+\)\\b\)\*)`)

// fsLetModifierGroups returns the modifier group of EVERY head-keyword
// occurrence in a pattern's compiled text, in order. Returning all of them —
// rather than the first — is what makes the comparison below cover every copy
// of a duplicated allowlist instead of whichever copy happens to come first.
func fsLetModifierGroups(t *testing.T, label, pattern string) []string {
	t.Helper()
	var out []string
	for _, m := range fsLetModifierGroupRE.FindAllStringSubmatch(pattern, -1) {
		out = append(out, m[1])
	}
	if len(out) == 0 {
		t.Fatalf("%s: could not locate a head-keyword + repeated-modifier-group prefix in "+
			"its compiled pattern, so the parity comparison below would be vacuous.\n"+
			"  pattern: %s\n"+
			"  expected a substring shaped like: let(?:\\s+(?:word|word)\\b)* or and(?:…)*\n"+
			"If the group was deliberately restructured, update fsLetModifierGroupRE "+
			"and re-verify that the `sealed` mutant is still DEAD ON EVERY BRANCH — this "+
			"test is the only thing holding the unbounded widening direction closed.",
			label, pattern)
	}
	return out
}

// TestActivePatternREModifierParityWithLetRE pins the claim the whole
// allowlist choice rests on: the two `let` scanners accept the SAME modifier
// set, byte for byte.
func TestActivePatternREModifierParityWithLetRE(t *testing.T) {
	aps := fsLetModifierGroups(t, "activePatternRE", activePatternRE.String())
	lts := fsLetModifierGroups(t, "letRE", letRE.String())

	// letRE has exactly one head keyword, so exactly one group. If that ever
	// changes the comparison below silently picks a side; fail instead.
	if len(lts) != 1 {
		t.Fatalf("letRE yielded %d modifier groups %q, want exactly 1 — the reference "+
			"side of this comparison must be unambiguous", len(lts), lts)
	}
	lt := lts[0]

	// COUNT GUARD. activePatternRE spells the group once per head keyword
	// (`let` and `and`, #7166). Pinning the COUNT is what catches a branch
	// whose group was DELETED or factored away, which an all-occurrences
	// equality check alone would pass vacuously.
	if len(aps) != 2 {
		t.Fatalf("activePatternRE yielded %d modifier groups %q, want exactly 2 — one per "+
			"head keyword (`let` and `and`, #7166), both from the fsLetModifiers constant. "+
			"A branch that lost its group accepts no modifier at all; a third branch needs "+
			"its own row here.", len(aps), aps)
	}

	// EVERY occurrence is compared, not the first. The first is always the
	// `let` branch, and grading only that is precisely the hole measured on
	// #7173: `sealed` added to the `and` branch alone was ALIVE at 0 --- FAIL.
	for i, ap := range aps {
		if ap != lt {
			t.Errorf("activePatternRE occurrence %d and letRE no longer accept the same "+
				"modifier set.\n"+
				"  activePatternRE[%d]: %q (%d bytes)\n"+
				"  letRE:              %q (%d bytes)\n"+
				"An active pattern IS a `let` binding, so the two sets are kept identical "+
				"on purpose (#7135): a divergent allowlist is a twinned surface that "+
				"drifts. If the divergence is INTENDED, it needs its own grading — the "+
				"forbidden rows in active_pattern_modifiers_7135_test.go cover only six "+
				"hand-picked words, so an added word that is not on that list is otherwise "+
				"ungraded (`sealed` was ALIVE at zero failing tests before this test existed, "+
				"and ALIVE again on the `and` branch alone until every occurrence was "+
				"compared). The phrase \"zero failing tests\" is deliberate: this message "+
				"must not contain the literal test-failure marker, or a mutation score that "+
				"counts those markers over the log reads one extra failure whenever this "+
				"very test fails.",
				i, i, ap, len(ap), lt, len(lt))
		}
	}

	// Vacuity guards on the extraction itself, independent of the comparison
	// above. Without these, a change that made BOTH extractions degenerate to
	// the same short string would leave the test green while grading nothing.
	// Applied to EVERY occurrence, for the same reason the equality check is.
	for i, ap := range aps {
		for _, c := range []struct {
			label, substr string
			why           string
		}{
			{"repeat", `)*`, "the group must REPEAT — a non-repeating `)?` accepts only one modifier"},
			{"separator", `\s+`, "each modifier must be whitespace-separated from the last"},
			{"alternation", `(?:`, "the modifier set must be an alternation group"},
		} {
			if !strings.Contains(ap, c.substr) {
				t.Errorf("extracted group %d %q lacks %s (%q): %s — the parity comparison "+
					"above would still pass while grading nothing", i, ap, c.label, c.substr, c.why)
			}
		}
		if n := strings.Count(ap, "|"); n < 1 {
			t.Errorf("extracted group %d %q contains no alternation bar, so it cannot be a "+
				"multi-word allowlist; the parity comparison would be vacuous", i, ap)
		}
	}
}
