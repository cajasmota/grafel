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

// fsLetModifierGroupRE extracts, from a pattern's own `String()`, the `let`
// keyword together with its repeated modifier group — the substring both
// scanners are claimed to share. It matches the pattern TEXT, so `\s` here is
// a literal backslash followed by `s`, not a whitespace class.
//
// If a scanner's group is restructured so this no longer matches (an
// upper-case word breaking `[a-z|]+`, a non-repeating `)?`, a nested group),
// the extraction returns "" and the test FAILS rather than silently
// comparing two empty strings against each other. That vacuity guard is the
// load-bearing half of this test: a parity assertion over two failed
// extractions would pass no matter what either pattern said.
var fsLetModifierGroupRE = regexp.MustCompile(`let\(\?:\\s\+\(\?:[a-z|]+\)\\b\)\*`)

func fsLetModifierGroup(t *testing.T, label, pattern string) string {
	t.Helper()
	got := fsLetModifierGroupRE.FindString(pattern)
	if got == "" {
		t.Fatalf("%s: could not locate a `let` + repeated-modifier-group prefix in its "+
			"compiled pattern, so the parity comparison below would be vacuous.\n"+
			"  pattern: %s\n"+
			"  expected a substring shaped like: let(?:\\s+(?:word|word)\\b)*\n"+
			"If the group was deliberately restructured, update fsLetModifierGroupRE "+
			"and re-verify that the `sealed` mutant is still DEAD — this test is the "+
			"only thing holding the unbounded widening direction closed.",
			label, pattern)
	}
	return got
}

// TestActivePatternREModifierParityWithLetRE pins the claim the whole
// allowlist choice rests on: the two `let` scanners accept the SAME modifier
// set, byte for byte.
func TestActivePatternREModifierParityWithLetRE(t *testing.T) {
	ap := fsLetModifierGroup(t, "activePatternRE", activePatternRE.String())
	lt := fsLetModifierGroup(t, "letRE", letRE.String())

	if ap != lt {
		t.Errorf("activePatternRE and letRE no longer accept the same modifier set.\n"+
			"  activePatternRE: %q (%d bytes)\n"+
			"  letRE:           %q (%d bytes)\n"+
			"An active pattern IS a `let` binding, so the two sets are kept identical "+
			"on purpose (#7135): a divergent allowlist is a twinned surface that "+
			"drifts. If the divergence is INTENDED, it needs its own grading — the "+
			"forbidden rows in active_pattern_modifiers_7135_test.go cover only six "+
			"hand-picked words, so an added word that is not on that list is otherwise "+
			"ungraded (`sealed` was ALIVE at 0 --- FAIL before this test existed).",
			ap, len(ap), lt, len(lt))
	}

	// Vacuity guards on the extraction itself, independent of the comparison
	// above. Without these, a change that made BOTH extractions degenerate to
	// the same short string would leave the test green while grading nothing.
	for _, c := range []struct {
		label, substr string
		why           string
	}{
		{"repeat", `)*`, "the group must REPEAT — a non-repeating `)?` accepts only one modifier"},
		{"separator", `\s+`, "each modifier must be whitespace-separated from the last"},
		{"alternation", `(?:`, "the modifier set must be an alternation group"},
	} {
		if !strings.Contains(ap, c.substr) {
			t.Errorf("extracted prefix %q lacks %s (%q): %s — the parity comparison "+
				"above would still pass while grading nothing", ap, c.label, c.substr, c.why)
		}
	}
	if n := strings.Count(ap, "|"); n < 1 {
		t.Errorf("extracted prefix %q contains no alternation bar, so it cannot be a "+
			"multi-word allowlist; the parity comparison would be vacuous", ap)
	}
}
