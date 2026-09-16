package main

import (
	"bytes"
	"regexp"
	"strconv"
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

// AGGREGATES ARE NOT GRADED BY GRADING THEIR ITEMS.
//
// #7076 round 3 named the pattern that had by then gone five deep in this one
// PR: grading a per-item label does not grade the aggregate computed from those
// items. M-P2 pinned the per-row "[baselined]" tag; the COUNTER that sums those
// same rows stayed open, and folding the deferrals into it printed "77 misses =
// 77 tolerated by the baseline + 0 alias-form" — laundering eleven fresh
// findings as suppressed, which is exactly what the comment three lines above
// that Fprintf forbids. The same round found printReport had no test at all, so
// dropping a term from its accounting line printed a sum that disagreed with
// the total one line above it, suite green.
//
// So: every number this tool prints that is derived from other numbers it
// prints is checked HERE, by re-deriving it from the emitted text. Matching a
// fixed string would not have caught either mutant — both print a well-formed
// line with a wrong number in it.

// numsIn pulls every integer out of one emitted line, found by a fragment.
func numsIn(t *testing.T, out, fragment string) []int {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, fragment) {
			continue
		}
		var got []int
		for _, f := range regexp.MustCompile(`\d+`).FindAllString(line, -1) {
			n, err := strconv.Atoi(f)
			if err != nil {
				t.Fatalf("bad integer %q in %q", f, line)
			}
			got = append(got, n)
		}
		return got
	}
	t.Fatalf("no emitted line contains %q:\n%s", fragment, out)
	return nil
}

// reportFor renders printReport and returns the emitted text.
func reportFor(res Result, grammars map[string]*Grammar) string {
	var b bytes.Buffer
	printReport(&b, res, grammars)
	return b.String()
}

// reportFixture is one hand-built surface whose every headline number is known
// by construction, so the emitted arithmetic can be checked against the input
// rather than against itself.
//
// 6 sites: 3 resolved in a mapped dir, 2 whitelisted (ERROR and "") in that
// same mapped dir, 1 in an unmapped dir. So total 6 = 3 resolved + 1 skipped +
// 2 whitelisted.
func reportFixture() (Result, map[string]*Grammar) {
	mk := func(dir, lit, form string) Site {
		return Site{Pkg: dir, Dir: dir, File: dir + "/x.go", Line: 1, Lit: lit, Form: form, Const: true}
	}
	const mapped, unmapped = "internal/extractors/scala", "internal/engine"
	return Result{
			Sites: []Site{
				mk(mapped, "identifier", FormCmp),
				mk(mapped, "block", FormCmp),
				mk(mapped, "not_a_scala_node", FormSwitch),
				mk(mapped, "ERROR", FormCmp),
				mk(mapped, "", FormCmp),
				mk(unmapped, "arrow_function", FormCmp),
			},
			Resolved:        3,
			Distinct:        3,
			DirsWithGrammar: []string{mapped},
			GrammarsForDir:  map[string][]string{mapped: {"scala"}},
			Skipped:         []SkippedDir{{Dir: unmapped, Sites: 1, Reason: "runtime language"}},
			SkippedSites:    1,
			Misses:          []Miss{{Site: mk(mapped, "not_a_scala_node", FormSwitch), Grammars: []string{"scala"}}},
			Failures:        []Miss{{Site: mk(mapped, "not_a_scala_node", FormSwitch), Grammars: []string{"scala"}}},
			Sinks:           []string{"a/b.IsKind#1", "a/b.Match#0"},
			Sources:         []string{"a/b.IsDeclType#0"},
		}, map[string]*Grammar{
			"scala": {Key: "scala", Kinds: map[string]bool{"identifier": true, "block": true}},
		}
}

// TestPrintReport_AccountingReconcilesWithTheTotal is the printReport half of
// blocker 1. The accounting line exists ONLY to let a reader check the headline
// numbers against each other; an accounting line that does not add up is worse
// than none, because it is read as a confirmation.
//
// VARIED: nothing — this is a single-surface arithmetic check, and its input is
// hand-built precisely so every term is known independently of the code under
// test.
// HELD CONSTANT: everything; the assertion is that the emitted sum equals the
// emitted total AND that each term equals what the fixture put in.
func TestPrintReport_AccountingReconcilesWithTheTotal(t *testing.T) {
	res, grammars := reportFixture()
	out := reportFor(res, grammars)

	total := numsIn(t, out, "  total ")
	if len(total) < 1 || total[0] != len(res.Sites) {
		t.Fatalf("total line does not report %d sites: %v\n%s", len(res.Sites), total, out)
	}
	acct := numsIn(t, out, "  accounting ")
	if len(acct) != 4 {
		t.Fatalf("accounting line has %d numbers, want 4 (resolved, skipped, whitelisted, sum): %v\n%s", len(acct), acct, out)
	}
	resolved, skipped, whitelisted, sum := acct[0], acct[1], acct[2], acct[3]

	// Each term against the fixture, so a mutant cannot satisfy the identity by
	// moving two terms at once.
	if resolved != res.Resolved {
		t.Errorf("accounting reports %d resolved, want %d", resolved, res.Resolved)
	}
	if skipped != res.SkippedSites {
		t.Errorf("accounting reports %d skipped, want %d", skipped, res.SkippedSites)
	}
	if whitelisted != 2 {
		t.Errorf("accounting reports %d whitelisted, want 2 — the fixture has one ERROR and one empty literal in a MAPPED package", whitelisted)
	}
	// The identity itself, both ways: the printed sum must be the sum of the
	// printed terms, and it must equal the printed total.
	if got := resolved + skipped + whitelisted; got != sum {
		t.Errorf("accounting terms sum to %d but the line prints %d", got, sum)
	}
	if sum != total[0] {
		t.Errorf("accounting sums to %d but the total one line above says %d sites — an accounting line that does not reconcile is read as a confirmation and is worse than none.\n%s", sum, total[0], out)
	}
}

// TestPrintReport_TagsMissesAndNamesTheSkippedSurface covers the rest of
// printReport's output, so "the report prints nothing at all" cannot pass the
// arithmetic test above vacuously.
func TestPrintReport_TagsMissesAndNamesTheSkippedSurface(t *testing.T) {
	res, grammars := reportFixture()
	out := reportFor(res, grammars)
	wantAll(t, out,
		"== derived surface ==",
		"== grammar keys per package ==",
		"helper surface",
		"internal/extractors/scala",
		"== packages with sites but no derivable grammar ==",
		"internal/engine",
		"== misses ==",
		"[NEW      ]",
		"not_a_scala_node",
	)
	// A baselined miss must be tagged differently from a new one. Same
	// aggregate/label distinction as above, in the other direction: the per-row
	// tag is what the counter in printVerdict aggregates.
	res.Misses[0].Baselined = true
	res.Failures = nil
	out = reportFor(res, grammars)
	if strings.Contains(out, "[NEW      ]") {
		t.Errorf("a baselined miss is still tagged NEW:\n%s", out)
	}
	wantAll(t, out, "[baselined]")
}

// TestPrintVerdict_MissLineSeparatesSuppressionFromDeferral is the printVerdict
// half of blocker 1, and the direct kill for M-P4.
//
// A baselined miss is SUPPRESSED — somebody decided not to fix it. An alias-form
// miss is DEFERRED — nobody has decided anything yet, it is waiting on an issue.
// Printing the second as the first launders a fresh finding as a triaged one,
// and it is the aggregate of exactly the per-row tag that M-P2 already pins.
//
// VARIED across the fixture's three misses: the disposition of each one
// (baselined / alias-deferred / new). That is the only axis the miss line
// reports, and every mutant on it moves a miss from one bucket to another.
// HELD CONSTANT: the package, the file, the grammars, and the literal shape —
// the misses differ ONLY in disposition, so a wrong count cannot be blamed on
// anything else.
func TestPrintVerdict_MissLineSeparatesSuppressionFromDeferral(t *testing.T) {
	if enforceAliasForms {
		t.Skip("enforceAliasForms is true: alias misses are failures now, and this line's deferral term is retired. Delete this test with the constant.")
	}
	mk := func(lit string, alias, baselined bool) Miss {
		return Miss{
			Site:      Site{Dir: "internal/extractors/swift", File: "internal/extractors/swift/swift.go", Line: 7, Lit: lit, Form: FormCmp, Alias: alias},
			Grammars:  []string{"swift"},
			Baselined: baselined,
		}
	}
	suppressed := mk("old_known_dead", false, true)
	deferred := mk("attributes", true, false)
	brandNew := mk("brand_new_dead", false, false)
	res := Result{
		Resolved:    3000,
		Distinct:    900,
		AliasSites:  230,
		Misses:      []Miss{suppressed, deferred, brandNew},
		Failures:    []Miss{brandNew},
		AliasMisses: []Miss{deferred},
	}
	out := verdictFor(res)

	n := numsIn(t, out, "misses = ")
	if len(n) != 4 {
		t.Fatalf("miss line has %d numbers, want 4 (total, baselined, alias-deferred, new): %v\n%s", len(n), n, out)
	}
	total, baselined, alias, fresh := n[0], n[1], n[2], n[3]
	if total != 3 {
		t.Errorf("miss line reports %d misses, want 3", total)
	}
	if baselined != 1 {
		t.Errorf("miss line reports %d tolerated by the baseline, want 1. Folding the alias deferrals into this counter launders fresh findings as suppressed — the thing the comment above the Fprintf forbids.", baselined)
	}
	if alias != 1 {
		t.Errorf("miss line reports %d alias-form deferrals, want 1", alias)
	}
	if fresh != 1 {
		t.Errorf("miss line reports %d new, want 1", fresh)
	}
	if got := baselined + alias + fresh; got != total {
		t.Errorf("miss line terms sum to %d but it reports %d total misses:\n%s", got, total, out)
	}
	// And the words must still say which is which: a reader who sees the right
	// numbers under the wrong labels is misled just as badly.
	wantAll(t, out, "tolerated by the baseline", "reported not enforced", "new")
}

// TestPrintReport_NamesBothFixpointSurfaces keeps the two fixpoints from being
// invisible. `Sinks` used to say "Reported" in its doc comment and was printed
// nowhere, and `sources` had no exported field at all (#7076 round 3). A miss
// count cannot distinguish "the fixpoint found nothing" from "the fixpoint
// found the wrong thing", so both counts are printed and both are checked here
// — separately, because one number standing in for two is how the distinction
// got lost in the first place.
//
// VARIED: which fixpoint a position belongs to (two sinks, one source) — an
// asymmetric fixture on purpose, so a line that prints the same count twice, or
// swaps the two, cannot pass.
// HELD CONSTANT: the rest of the surface.
func TestPrintReport_NamesBothFixpointSurfaces(t *testing.T) {
	res, grammars := reportFixture()
	out := reportFor(res, grammars)
	n := numsIn(t, out, "helper surface")
	if len(n) != 2 {
		t.Fatalf("helper-surface line has %d numbers, want 2 (sinks, sources): %v\n%s", len(n), n, out)
	}
	if n[0] != len(res.Sinks) {
		t.Errorf("reports %d sink positions, want %d", n[0], len(res.Sinks))
	}
	if n[1] != len(res.Sources) {
		t.Errorf("reports %d source positions, want %d — the sources fixpoint added in this PR had no exported field and no output at all", n[1], len(res.Sources))
	}
}
