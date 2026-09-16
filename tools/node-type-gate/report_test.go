package main

import (
	"bytes"
	"strings"
	"testing"
)

// THE REPORTING PATH IS GRADED HERE.
//
// #7076 round 2, blocker 2: deleting the ENTIRE always-on skip block from
// printVerdict left the whole suite green. That was the third time in this PR
// that the reporting path was the ungraded part — every other control asserts
// on Result fields, and Result fields survive a printer that prints nothing.
//
// A counter is not a diagnostic and a struct field is not output. These tests
// assert on the BYTES printVerdict emits, because that text is the entire
// product of the "count and name the skipped surface" requirement: a skipped
// package that nobody can see is indistinguishable from one that does not
// exist, and a Result field nobody prints is exactly that invisible.
//
// They drive printVerdict with a hand-built Result rather than a package load,
// so they grade the printer and nothing else: a change in the tree cannot make
// them pass or fail.

// verdictFor renders one Result and returns the emitted text.
func verdictFor(res Result) string {
	var b bytes.Buffer
	printVerdict(&b, res)
	return b.String()
}

// wantAll fails naming every missing fragment, not just the first: "the block
// was deleted" and "one field stopped being printed" must not look alike.
func wantAll(t *testing.T, got string, frags ...string) {
	t.Helper()
	var missing []string
	for _, f := range frags {
		if !strings.Contains(got, f) {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		t.Errorf("printVerdict output is missing %d fragment(s):\n  %s\n--- emitted ---\n%s",
			len(missing), strings.Join(missing, "\n  "), got)
	}
}

// TestPrintVerdict_EmitsTheSkippedSurface pins the always-on skip block.
//
// VARIED: the exemption state of the two skipped dirs (one carries a reason,
// one does not) — the axis that decides whether the reader sees a reviewed
// decision or an oversight, and the axis that M-P1 (delete the block) and a
// "print only the exempt ones" mutant both move.
// HELD CONSTANT: the site counts, the resolved/miss totals, the grammar map,
// and the fact that the run is otherwise clean — so any diff in the emitted
// text is attributable to the skip block alone.
func TestPrintVerdict_EmitsTheSkippedSurface(t *testing.T) {
	res := Result{
		Resolved:        3000,
		Distinct:        900,
		DirsWithGrammar: []string{"internal/extractors/scala"},
		Skipped: []SkippedDir{
			{Dir: "internal/engine", Sites: 92, Reason: "parses under a language computed at runtime"},
			{Dir: "internal/custom/brandnew", Sites: 7},
		},
		SkippedSites:    99,
		UnreviewedSkips: []SkippedDir{{Dir: "internal/custom/brandnew", Sites: 7}},
	}
	got := verdictFor(res)

	wantAll(t, got,
		// The headline: how much surface went unchecked, and in how many places.
		"99 sites in 2 package(s) were NOT checked",
		// Each dir named, WITH its own count. A total without a breakdown
		// cannot be acted on.
		"internal/engine",
		"92 sites",
		"internal/custom/brandnew",
		"7 sites",
		// The reviewed one shows its reason...
		"parses under a language computed at runtime",
		// ...and the unreviewed one is marked as such in the listing itself,
		// not only in the error below it.
		"NO EXEMPTION",
		// And an unreviewed skip emits an actionable CI error.
		"::error::internal/custom/brandnew produced 7 node-type literal sites",
	)

	// The exempt dir must NOT be reported as an error — over-reporting would
	// make the error line worthless and is the permissive direction here.
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "::error") && strings.Contains(line, "internal/engine") {
			t.Errorf("exempt dir emitted a CI error line: %q", line)
		}
	}
}

// TestPrintVerdict_NoSkippedSurfaceSaysNothing is the other direction: the
// block must be conditional, so a clean tree does not print a "0 sites in 0
// packages" line that trains readers to skip it.
func TestPrintVerdict_NoSkippedSurfaceSaysNothing(t *testing.T) {
	got := verdictFor(Result{Resolved: 3000, Distinct: 900})
	if strings.Contains(got, "were NOT checked") {
		t.Errorf("clean run printed a skipped-surface block:\n%s", got)
	}
	if !strings.Contains(got, "node-type-gate: OK") {
		t.Errorf("clean run did not print OK:\n%s", got)
	}
}

// TestPrintVerdict_EmitsTheAliasSurface pins the alias block the same way,
// BEFORE it can become the next ungraded reporting path. The alias findings are
// reported and not enforced (see enforceAliasForms), so this text is the ONLY
// thing that makes them visible — there is no exit code behind it.
//
// VARIED: whether an alias miss is already baselined — the axis that decides
// the "[baselined]" tag, and the one a reader needs to tell a triaged finding
// from a fresh one.
// HELD CONSTANT: the package, the alias-site count, and the literals' form.
func TestPrintVerdict_EmitsTheAliasSurface(t *testing.T) {
	mk := func(file, lit string, baselined bool) Miss {
		return Miss{
			Site:      Site{Dir: "internal/extractors/swift", File: file, Line: 697, Lit: lit, Form: FormCmp, Alias: true},
			Grammars:  []string{"swift"},
			Baselined: baselined,
		}
	}
	res := Result{
		Resolved:    3000,
		Distinct:    900,
		AliasSites:  230,
		AliasMisses: []Miss{mk("internal/extractors/swift/swift.go", "attributes", false), mk("internal/extractors/javascript/navigation.go", "object_expression", true)},
		Misses:      []Miss{mk("internal/extractors/swift/swift.go", "attributes", false)},
	}
	got := verdictFor(res)

	wantAll(t, got,
		// The count of the shape itself — the "make the skipped surface
		// visible" half of round 2's blocker.
		"230 of the CHECKED sites were reached through an alias",
		"2 alias-form dead literal(s)",
		// Reported, not enforced, and it says so rather than letting a reader
		// assume the gate is failing on them.
		"REPORTED, not enforced",
		// file:line:literal for each, so a finding can be acted on from the
		// log alone.
		"internal/extractors/swift/swift.go:697",
		`"attributes"`,
		"internal/extractors/javascript/navigation.go:697",
		`"object_expression"`,
		"[baselined]",
	)

	// An un-baselined alias miss must NOT carry the baselined tag: that is the
	// permissive direction, and it would silently launder a fresh finding as a
	// triaged one.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, `"attributes"`) && strings.Contains(line, "[baselined]") {
			t.Errorf("un-baselined alias miss tagged as baselined: %q", line)
		}
	}
}

// TestPrintVerdict_FloorBreachSaysTheVerdictIsMeaningless pins the last piece
// of always-on text. A gate that read nothing and printed OK is worse than no
// gate; the floor error must say so in the same breath as the count.
func TestPrintVerdict_FloorBreachSaysTheVerdictIsMeaningless(t *testing.T) {
	got := verdictFor(Result{Resolved: 1, Distinct: 1})
	wantAll(t, got,
		"::error::node-type-gate read only 1 sites",
		"the verdict below means nothing",
		"node-type-gate: FAIL (count floor)",
	)
}
