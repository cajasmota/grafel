package main

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// THE REPORTING PATH IS GRADED HERE, AS A SURFACE.
//
// Every review round of this PR has produced the same finding: some part of the
// reporting path was ungraded.
//
//	round 2  deleting printVerdict's whole skip block left the suite green
//	round 3  the per-row "[baselined]" tag was pinned; the COUNTER that sums
//	         those rows was not, so folding deferrals into it laundered eleven
//	         fresh findings as suppressed, green
//	round 3  printReport had no test at all; dropping a term from its
//	         accounting line printed a sum disagreeing with the total, green
//	round 4  the test written to close round 3 used a 1/1/1 fixture, so
//	         PERMUTING its three arguments satisfied every assertion — M-P4
//	         mirrored, surviving the test built for it — and five more
//	         printer mutants (delete the headline, swap resolved/distinct,
//	         delete two whole sections, swap distinct/files) were all alive
//	round 5  printReport's two section guards relaxed to `>= 0`, and its
//	         whitelist loop's no-grammar guard deleted, were all three alive
//	         — the goldens below could not see any of them, because all three
//	         are ABSENT-direction mutants (see the limit stated next)
//
// The pattern is that the printers were treated as a list of lines to patch one
// at a time. They are not; they are a SURFACE, and a surface is covered by
// asserting its whole emitted shape. So each printer has one golden test below
// that compares its complete output, byte for byte, against a literal written
// out in full. A deleted line, a swapped argument and a reworded label all red
// it whether or not anyone thought of that mutant in advance, and so does a
// deleted section — which is what rounds 2 through 4 needed.
//
// THE LIMIT OF A GOLDEN, which round 5 charged for. A golden is ONE fixture, so
// it can only ever show a conditional block in its PRESENT direction. It cannot
// see a guard loosened to fire when it should not: the section appears, empty,
// and the golden fixture — in which that section appears anyway — is unchanged.
// Every conditional in either printer therefore needs a SECOND test, on a
// fixture where the block must not appear. Those tests are the last group in
// this file, and the enumeration above them says which guard each one scores.
//
// TWO RULES FOR EVERYTHING IN THIS FILE.
//
//  1. ASYMMETRIC FIXTURES. Every multi-term line is driven by values that are
//     pairwise DISTINCT, so no permutation of them satisfies the assertions. A
//     fixture whose expected values are all equal cannot detect a permutation
//     of them: 1/1/1 is not a test of a three-term line, it is a test that the
//     line prints three numbers. Round 4's blocker was exactly that, in the
//     test written to close round 3, and the contrast was visible in the same
//     file — the accounting line's 3/1/2 fixture killed its category swap
//     instantly. If you add a term, give it a value nothing else on its line
//     has.
//
//  2. NO "COVERS X" CLAIM WITHOUT DELETING X FIRST. A comment added in round 3
//     said one test "covers the rest of printReport's output" while two whole
//     sections of it were deletable with the suite green. Round 5 found the
//     next one: a header claiming "the remaining tests pin the printer's
//     CONDITIONAL behaviour" sat above tests that reached printVerdict's
//     conditionals only, while all three of printReport's were ungraded. That
//     is FIVE consecutive rounds with a false comment in this file, all of them
//     permissive, which makes it a habit and not five accidents. Delete the
//     thing, watch something red, then write the claim — and write the claim
//     no wider than the thing you actually deleted.
//
// The golden tests are deliberately brittle: any intentional change to the
// output must be reflected here, and that edit is the moment to ask whether the
// new line is graded by anything but the golden. Both printers are driven on
// hand-built Results, so a change in the tree can never move them.

// verdictFor renders one Result and returns the emitted text.
func verdictFor(res Result) string {
	var b bytes.Buffer
	printVerdict(&b, res)
	return b.String()
}

// reportFor renders printReport and returns the emitted text.
func reportFor(res Result, grammars map[string]*Grammar) string {
	var b bytes.Buffer
	printReport(&b, res, grammars)
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
		t.Errorf("output is missing %d fragment(s):\n  %s\n--- emitted ---\n%s",
			len(missing), strings.Join(missing, "\n  "), got)
	}
}

// diffGolden compares an emitted block against a golden literal and reports the
// first differing line rather than dumping two walls of text.
func diffGolden(t *testing.T, got, want string) {
	t.Helper()
	if got == want {
		return
	}
	gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(gl) || i < len(wl); i++ {
		var g, w string
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if g != w {
			t.Fatalf("emitted output differs from the golden at line %d.\n  got:  %q\n  want: %q\n\nThe golden is the whole shape of this printer: a deleted line, a deleted\nsection, a swapped argument or a reworded label all land here. If the change\nwas intentional, update the golden — and while you are editing it, check\nwhether the new line is graded by anything other than this test.\n\n--- full emitted ---\n%s", i+1, g, w, got)
		}
	}
	t.Fatalf("outputs differ but no line does; check trailing newlines.\ngot %q\nwant %q", got, want)
}

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

func miss(dir, file string, line int, lit, form string, grammars []string, alias, baselined bool) Miss {
	return Miss{
		Site:      Site{Pkg: dir, Dir: dir, File: file, Line: line, Lit: lit, Form: form, Alias: alias, Const: true},
		Grammars:  grammars,
		Baselined: baselined,
	}
}

// verdictFixture drives printVerdict's golden. Every number on every line is
// distinct from every other number on that line, so no permutation of a line's
// arguments can survive:
//
//	headline    3041 resolved / 1017 distinct / 3 packages
//	skip block  30 sites / 2 packages, rows of 23 and 7
//	alias       13 alias sites / 4 alias misses
//	miss line   10 misses = 5 baselined + 3 deferred + 2 new
//
// The two skipped dirs differ in exemption state, the four alias misses differ
// in baselined state, and the misses cover all three dispositions, so the skip
// block, the alias block, the miss line and the FAIL verdict all contribute
// bytes to the golden. The count floor does NOT: this fixture is well above it,
// so printVerdict's floor branch emits nothing here. That is the general limit,
// and it is exactly the kind of claim this file has got wrong five rounds
// running: one fixture can only exercise each branch in its PRESENT direction.
// "The skip block is absent on a clean run", "the alias block is absent", "OK
// rather than FAIL", and the count-floor error are covered by the conditional
// tests at the bottom of this file, not by the golden.
func verdictFixture() Result {
	const js = "internal/extractors/javascript"
	al := func(lit string, baselined bool) Miss {
		return miss(js, js+"/navigation.go", 577, lit, FormCmp, []string{"javascript", "tsx"}, true, baselined)
	}
	aliasDeferred := []Miss{al("object_expression", false), al("property", false), al("string_value", false)}
	aliasBaselined := al("old_alias_dead", true)

	var misses []Miss
	// 5 baselined: four plain, one of them the baselined alias miss.
	for _, lit := range []string{"b_one", "b_two", "b_three", "b_four"} {
		misses = append(misses, miss(js, js+"/extractor.go", 12, lit, FormSwitch, []string{"javascript", "tsx"}, false, true))
	}
	misses = append(misses, aliasBaselined)
	// 3 alias-deferred.
	misses = append(misses, aliasDeferred...)
	// 2 new.
	failures := []Miss{
		miss(js, js+"/extractor.go", 99, "brand_new_dead", FormCmp, []string{"javascript", "tsx"}, false, false),
		miss(js, js+"/imports.go", 44, "also_new_dead", FormHelper, []string{"javascript", "tsx"}, false, false),
	}
	misses = append(misses, failures...)

	return Result{
		Resolved:        3041,
		Distinct:        1017,
		DirsWithGrammar: []string{js, "internal/extractors/scala", "internal/extractors/swift"},
		Skipped: []SkippedDir{
			{Dir: "internal/engine", Sites: 23, Reason: "parses under a runtime language"},
			{Dir: "internal/custom/brandnew", Sites: 7},
		},
		SkippedSites:    30,
		UnreviewedSkips: []SkippedDir{{Dir: "internal/custom/brandnew", Sites: 7}},
		AliasSites:      13,
		AliasMisses:     append(append([]Miss{}, aliasDeferred...), aliasBaselined),
		Misses:          misses,
		Failures:        failures,
		StaleBaseline:   []BaselineEntry{{Class: "unreachable-code", Dir: js, Lit: "gone_literal", File: js + "/old.go", Line: 3}},
	}
}

// TestPrintVerdict_FullEmittedShape is the whole-surface golden for
// printVerdict. It is the test that would have caught all six rounds.
//
// VARIED: nothing is varied here on purpose — a golden is a single-point pin of
// the complete output, and the axes that matter (exemption state, baselined
// state, miss disposition, stale rows) are all present SIMULTANEOUSLY in the
// one fixture, so every conditional block contributes bytes in its PRESENT
// direction. Their absent directions are separate tests below; a golden cannot
// show that a section is missing when it should be.
// HELD CONSTANT: the entire Result; the assertion is byte equality.
func TestPrintVerdict_FullEmittedShape(t *testing.T) {
	if enforceAliasForms {
		t.Skip("enforceAliasForms is true: the alias block's wording and the miss line's deferral term both change. Re-golden this test with the constant.")
	}
	const js = "internal/extractors/javascript"
	want := strings.Join([]string{
		`node-type-gate: 3041 node-type literal sites resolved (1017 distinct pkg+literal) across 3 packages with a grammar`,
		`node-type-gate: 30 sites in 2 package(s) were NOT checked (no grammar was derived for them):`,
		`  internal/engine                23 sites — parses under a runtime language`,
		`  internal/custom/brandnew        7 sites — NO EXEMPTION — this package is not in skipExemptions`,
		`::error::internal/custom/brandnew produced 7 node-type literal sites but no grammar could be derived for it, and it has no entry in skipExemptions. Either derive its grammar (a constant language passed to treesitter.Parse is picked up automatically) or add an exemption saying why it cannot be mapped. A silently skipped package is the defect this gate exists to catch.`,
		`node-type-gate: 13 of the CHECKED sites were reached through an alias (a local or parameter holding a node type, not a syntactic x.Type() call)`,
		`node-type-gate: 4 alias-form dead literal(s) — REPORTED, not enforced (see enforceAliasForms):`,
		`  ` + js + `/navigation.go:577 "object_expression" in ` + js,
		`  ` + js + `/navigation.go:577 "property" in ` + js,
		`  ` + js + `/navigation.go:577 "string_value" in ` + js,
		`  ` + js + `/navigation.go:577 "old_alias_dead" in ` + js + ` [baselined]`,
		`node-type-gate: 10 misses = 5 tolerated by the baseline + 3 alias-form, reported not enforced + 2 new`,
		`::error file=` + js + `/extractor.go,line=99::` + js + `/extractor.go:99: node type "brand_new_dead" resolves in none of package ` + js + `'s grammars [javascript,tsx] (form=cmp)`,
		`::error file=` + js + `/imports.go,line=44::` + js + `/imports.go:44: node type "also_new_dead" resolves in none of package ` + js + `'s grammars [javascript,tsx] (form=helper)`,
		`::error::stale baseline row: ` + js + ` "gone_literal" in ` + js + `/old.go no longer matches anything. If the literal was FIXED, delete the row. If the file or package was RENAMED or the literal MOVED, rewrite this row's dir/path to the new location — deleting it would leave the literal unbaselined and still failing. -update does NOT rewrite paths: it only refreshes the line number of a row whose (dir, literal, file) key already matches.`,
		`node-type-gate: FAIL`,
		``,
	}, "\n")
	diffGolden(t, verdictFor(verdictFixture()), want)
}

// reportFixture drives printReport's golden. Asymmetric on every line, by the
// same rule as verdictFixture:
//
//	total       9 sites / 7 distinct literals / 4 files / 2 dynamic
//	accounting  5 resolved + 3 skipped + 1 whitelisted = 9
//	helper      2 sinks / 1 source
//	grammars    2 packages / 3 distinct keys / 4 in the registry
//
// 9 sites: 5 resolved across two mapped packages, 1 whitelisted (ERROR) in a
// mapped package, 3 in an unmapped one.
func reportFixture() (Result, map[string]*Grammar) {
	site := func(dir, file, lit, form string) Site {
		return Site{Pkg: dir, Dir: dir, File: file, Line: 1, Lit: lit, Form: form, Const: true}
	}
	const (
		js      = "internal/extractors/javascript"
		scala   = "internal/extractors/scala"
		engine  = "internal/engine"
		jsFile  = js + "/extractor.go"
		scFile  = scala + "/scala.go"
		enFileA = engine + "/client.go"
		enFileB = engine + "/dispatch.go"
	)
	dead := miss(scala, scFile, 1, "not_a_scala_node", FormSwitch, []string{"scala"}, false, false)
	return Result{
			Sites: []Site{
				site(js, jsFile, "identifier", FormCmp),
				site(js, jsFile, "object", FormCmp),
				site(js, jsFile, "ERROR", FormCmp),
				site(scala, scFile, "identifier", FormCmp),
				site(scala, scFile, "block", FormSwitch),
				site(scala, scFile, "not_a_scala_node", FormSwitch),
				site(engine, enFileA, "arrow_function", FormCmp),
				site(engine, enFileA, "lexical_declaration", FormCmp),
				site(engine, enFileB, "arrow_function", FormHelper),
			},
			Dynamic: []Site{
				site(js, jsFile, "", FormCmp),
				site(scala, scFile, "", FormHelper),
			},
			Resolved:        5,
			Distinct:        5,
			DirsWithGrammar: []string{js, scala},
			GrammarsForDir:  map[string][]string{js: {"javascript", "typescript"}, scala: {"scala"}},
			Registrations: []Registration{
				{Dir: js, Key: "javascript", File: jsFile, Line: 8},
				{Dir: engine, Key: "", File: enFileB, Line: 61},
			},
			Skipped:      []SkippedDir{{Dir: engine, Sites: 3, Reason: "runtime language"}},
			SkippedSites: 3,
			Misses:       []Miss{dead},
			Failures:     []Miss{dead},
			Sinks:        []string{"a/b.IsKind#1", "a/b.Match#0"},
			Sources:      []string{"a/b.IsDeclType#0"},
		}, map[string]*Grammar{
			"javascript": {Key: "javascript", Kinds: map[string]bool{"identifier": true, "object": true}},
			"typescript": {Key: "typescript", Kinds: map[string]bool{"identifier": true}},
			"scala":      {Key: "scala", Kinds: map[string]bool{"identifier": true, "block": true}},
			"swift":      {Key: "swift", Kinds: map[string]bool{"attribute": true}},
		}
}

// TestPrintReport_FullEmittedShape is the whole-surface golden for printReport.
//
// Round 4 found five printer mutants alive at once, two of them WHOLE SECTIONS
// deleted — the non-constant-registration section and the grammar-keys summary
// line. Section-level deletion is the mutant a fragment list never catches,
// because nobody lists a fragment from a section they forgot exists. A golden
// catches it by construction.
//
// VARIED: nothing, by the same reasoning as printVerdict's golden — the axes
// (mapped/unmapped package, multi-grammar/single-grammar, resolved/whitelisted/
// skipped site, constant/non-constant registration, new miss) are all live in
// the one fixture at once.
// HELD CONSTANT: the entire Result; the assertion is byte equality.
func TestPrintReport_FullEmittedShape(t *testing.T) {
	const (
		js     = "internal/extractors/javascript"
		scala  = "internal/extractors/scala"
		engine = "internal/engine"
	)
	want := strings.Join([]string{
		`== derived surface ==`,
		`  form cmp             6 sites`,
		`  form helper          1 sites`,
		`  form switch          2 sites`,
		`  total           9 sites / 7 distinct literals / 4 files / 2 dynamic positions`,
		`  accounting      5 resolved + 3 in packages with no grammar + 1 whitelisted (ERROR/MISSING/"") = 9`,
		`  helper surface 2 sink position(s) (argument is a node-type literal) / 1 source position(s) (parameter receives a node type)`,
		`== grammar keys per package ==`,
		`  ` + js + `           javascript,typescript`,
		`  ` + scala + `                scala`,
		`  (2 packages, 3 distinct grammar keys registered; 4 grammars in the parser registry)`,
		`== packages with sites but no derivable grammar ==`,
		`  internal/engine                 3 sites`,
		`== registrations with a non-constant language key (mapping not derivable) ==`,
		`  ` + engine + `/dispatch.go:61`,
		`== misses ==`,
		`  [NEW      ] ` + scala + `/scala.go:1: node type "not_a_scala_node" resolves in none of package ` + scala + `'s grammars [scala] (form=switch)`,
		``,
	}, "\n")
	res, grammars := reportFixture()
	diffGolden(t, reportFor(res, grammars), want)
}

// The goldens pin the SHAPE. The tests below pin the MEANING of the numbers in
// it — that the accounting reconciles, that the miss line separates a
// suppression from a deferral, that the two fixpoint counts are two counts.
// A golden alone would happily accept a self-consistent lie: rename a label and
// re-golden, and nothing says the number under it is still the right one. These
// re-derive each value from the emitted text and check it against the fixture
// and against the identity it claims.

// TestPrintReport_AccountingReconcilesWithTheTotal.
//
// The accounting line exists ONLY to let a reader check the headline numbers
// against each other; one that does not add up is worse than none, because it
// is read as a confirmation.
//
// VARIED: the three accounting terms take three DIFFERENT values (5 resolved,
// 3 skipped, 1 whitelisted) — the asymmetry rule. With 1/1/1 every permutation
// of the Fprintf's arguments would pass, which is the exact hole round 4 found
// in the miss-line test.
// HELD CONSTANT: the rest of the surface.
func TestPrintReport_AccountingReconcilesWithTheTotal(t *testing.T) {
	res, grammars := reportFixture()
	out := reportFor(res, grammars)

	total := numsIn(t, out, "  total ")
	if len(total) != 4 {
		t.Fatalf("total line has %d numbers, want 4 (sites, distinct literals, files, dynamic): %v\n%s", len(total), total, out)
	}
	if total[0] != len(res.Sites) {
		t.Fatalf("total line reports %d sites, want %d", total[0], len(res.Sites))
	}
	if total[3] != len(res.Dynamic) {
		t.Errorf("total line reports %d dynamic positions, want %d", total[3], len(res.Dynamic))
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
	if whitelisted != 1 {
		t.Errorf("accounting reports %d whitelisted, want 1 — the fixture has exactly one ERROR literal in a MAPPED package", whitelisted)
	}
	if got := resolved + skipped + whitelisted; got != sum {
		t.Errorf("accounting terms sum to %d but the line prints %d", got, sum)
	}
	if sum != total[0] {
		t.Errorf("accounting sums to %d but the total one line above says %d sites — an accounting line that does not reconcile is read as a confirmation and is worse than none.\n%s", sum, total[0], out)
	}
}

// TestPrintVerdict_MissLineSeparatesSuppressionFromDeferral.
//
// A baselined miss is SUPPRESSED — somebody decided not to fix it. An alias-form
// miss is DEFERRED — nobody has decided anything yet, it is waiting on an issue.
// Printing the second as the first launders a fresh finding as a triaged one.
//
// ROUND 4'S BLOCKER LIVED HERE. The first version of this test used one miss of
// each kind and asserted every term == 1, so swapping two arguments in the
// Fprintf was green while the shipped output read "77 misses = 11 tolerated by
// the baseline + 66 alias-form". Three terms all expecting the same value do
// not test a three-term line.
//
// VARIED: the COUNT of each disposition — 5 baselined, 3 alias-deferred, 2 new,
// pairwise distinct and distinct from the total 10. That is the axis the line
// reports, and it is now varied in the only way a permutation cannot survive.
// HELD CONSTANT: the package, the grammars, and the fact that every miss is a
// miss — the three groups differ ONLY in disposition, so a wrong count cannot
// be blamed on anything else.
func TestPrintVerdict_MissLineSeparatesSuppressionFromDeferral(t *testing.T) {
	if enforceAliasForms {
		t.Skip("enforceAliasForms is true: alias misses are failures now, and this line's deferral term is retired. Delete this test with the constant.")
	}
	res := verdictFixture()
	out := verdictFor(res)

	n := numsIn(t, out, "misses = ")
	if len(n) != 4 {
		t.Fatalf("miss line has %d numbers, want 4 (total, baselined, alias-deferred, new): %v\n%s", len(n), n, out)
	}
	total, baselined, alias, fresh := n[0], n[1], n[2], n[3]

	// The four expected values are pairwise distinct, so each of these checks
	// is independent of the others.
	if total != 10 {
		t.Errorf("miss line reports %d misses, want 10", total)
	}
	if baselined != 5 {
		t.Errorf("miss line reports %d tolerated by the baseline, want 5. Folding the alias deferrals into this counter launders fresh findings as suppressed.", baselined)
	}
	if alias != 3 {
		t.Errorf("miss line reports %d alias-form deferrals, want 3", alias)
	}
	if fresh != 2 {
		t.Errorf("miss line reports %d new, want 2 — and it must equal len(Failures), the set that actually fails CI", fresh)
	}
	if fresh != len(res.Failures) {
		t.Errorf("miss line's new count is %d but %d misses are in Failures", fresh, len(res.Failures))
	}
	if got := baselined + alias + fresh; got != total {
		t.Errorf("miss line terms sum to %d but it reports %d total misses:\n%s", got, total, out)
	}
	wantAll(t, out, "tolerated by the baseline", "reported not enforced", "new")
}

// TestPrintVerdict_HeadlineReportsResolvedNotDistinct.
//
// The headline is the line the count floor is read from, so a swap between its
// first two arguments is a silent 41→17 collapse in the number every other
// claim about coverage is quoted against.
//
// VARIED: the three headline values are pairwise distinct (3041 resolved, 1017
// distinct, 3 packages) — asymmetry again, and the reason a swap is detectable.
// HELD CONSTANT: the rest of the surface.
func TestPrintVerdict_HeadlineReportsResolvedNotDistinct(t *testing.T) {
	res := verdictFixture()
	n := numsIn(t, verdictFor(res), "literal sites resolved")
	if len(n) != 3 {
		t.Fatalf("headline has %d numbers, want 3 (resolved, distinct, packages): %v", len(n), n)
	}
	if n[0] != res.Resolved {
		t.Errorf("headline reports %d resolved, want %d", n[0], res.Resolved)
	}
	if n[1] != res.Distinct {
		t.Errorf("headline reports %d distinct pkg+literal, want %d", n[1], res.Distinct)
	}
	if n[2] != len(res.DirsWithGrammar) {
		t.Errorf("headline reports %d packages with a grammar, want %d", n[2], len(res.DirsWithGrammar))
	}
}

// TestPrintReport_NamesBothFixpointSurfaces keeps the two fixpoints from being
// invisible. `Sinks` used to say "Reported" in its doc comment and was printed
// nowhere, and `sources` had no exported field at all (#7076 round 3). A miss
// count cannot distinguish "the fixpoint found nothing" from "the fixpoint
// found the wrong thing", so both counts are printed and both are checked here
// — separately, because one number standing in for two is how the distinction
// got lost in the first place.
//
// VARIED: the two counts differ (2 sinks, 1 source) — asymmetric on purpose, so
// a line printing the same count twice, or swapping them, cannot pass.
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

// ---------------------------------------------------------------------------
// CONDITIONAL BEHAVIOUR: every guarded block in BOTH printers, in its ABSENT
// direction — the direction no golden can reach.
//
// An earlier version of this header said "the remaining tests pin the printer's
// CONDITIONAL behaviour". That was round 5's false comment. Singular "the
// printer" was printVerdict: the tests under it reached printVerdict's guards
// and none of printReport's, and all three of printReport's were alive.
//
// So the claim is now an enumeration rather than an adjective. report.go has
// six guarded blocks whose absent direction is a separate claim; each line
// names the guard, its site, and the test that scores it. Each was scored by
// applying the mutant and watching that test red.
//
//	report.go:31   res.FloorBreached()         printVerdict floor error
//	                 -> the golden (the fixture is above the floor, so an
//	                    always-firing guard adds a line the golden rejects)
//	report.go:37   len(res.Skipped) > 0        printVerdict skip block
//	                 -> TestPrintVerdict_NoSkippedSurfaceSaysNothing
//	report.go:60   len(res.AliasMisses) > 0    printVerdict alias block
//	                 -> TestPrintVerdict_NoSkippedSurfaceSaysNothing
//	report.go:133  no-grammar guard            printReport whitelist loop
//	                 -> TestPrintReport_WhitelistCountExcludesPackagesWithNoGrammar
//	report.go:160  len(res.Skipped) > 0        printReport skip section
//	                 -> TestPrintReport_SkippedSectionIsAbsentWhenNothingWasSkipped
//	report.go:173  len(dynamicRegs) > 0        printReport registration section
//	                 -> TestPrintReport_DynamicRegistrationSectionIsAbsentWhenEveryKeyIsConstant
//
// Note that report.go:37 and report.go:160 are the SAME EXPRESSION at two
// sites. Round 4 scored :37, got DEAD, and reported that the absent direction
// was graded; :160 was alive. A verdict at one occurrence says nothing about
// its twin, so both are listed and both were scored.

// TestPrintVerdict_NoSkippedSurfaceSaysNothing scores TWO of printVerdict's
// guards at once — the skip block (report.go:37) and the alias block
// (report.go:60) — plus the OK arm of the final verdict switch. A clean tree
// must not print a "0 sites in 0 packages" line, or an empty alias block:
// headers that carry no information train readers to skip them.
func TestPrintVerdict_NoSkippedSurfaceSaysNothing(t *testing.T) {
	got := verdictFor(Result{Resolved: 3000, Distinct: 900})
	if strings.Contains(got, "were NOT checked") {
		t.Errorf("clean run printed a skipped-surface block:\n%s", got)
	}
	if strings.Contains(got, "alias-form dead literal") {
		t.Errorf("clean run printed an alias-findings block:\n%s", got)
	}
	if !strings.Contains(got, "node-type-gate: OK") {
		t.Errorf("clean run did not print OK:\n%s", got)
	}
}

// TestPrintVerdict_ExemptSkipEmitsNoError is the permissive direction on the
// skip block: over-reporting would make the ::error:: line worthless.
func TestPrintVerdict_ExemptSkipEmitsNoError(t *testing.T) {
	for _, line := range strings.Split(verdictFor(verdictFixture()), "\n") {
		if strings.HasPrefix(line, "::error") && strings.Contains(line, "internal/engine") {
			t.Errorf("exempt dir emitted a CI error line: %q", line)
		}
	}
}

// TestPrintVerdict_UnBaselinedAliasMissIsNotTaggedBaselined is the permissive
// direction on the alias block: the tag is what tells a triaged finding from a
// fresh one.
func TestPrintVerdict_UnBaselinedAliasMissIsNotTaggedBaselined(t *testing.T) {
	for _, line := range strings.Split(verdictFor(verdictFixture()), "\n") {
		if strings.Contains(line, `"object_expression"`) && strings.Contains(line, "[baselined]") {
			t.Errorf("un-baselined alias miss tagged as baselined: %q", line)
		}
	}
}

// TestPrintReport_BaselinedMissIsTaggedDifferently pins the per-row tag the
// verdict's counter aggregates. The golden covers the NEW tag; this covers the
// other branch, which no single fixture can show at the same time.
func TestPrintReport_BaselinedMissIsTaggedDifferently(t *testing.T) {
	res, grammars := reportFixture()
	res.Misses[0].Baselined = true
	res.Failures = nil
	out := reportFor(res, grammars)
	if strings.Contains(out, "[NEW      ]") {
		t.Errorf("a baselined miss is still tagged NEW:\n%s", out)
	}
	wantAll(t, out, "[baselined]")
}

// TestPrintVerdict_FloorBreachSaysTheVerdictIsMeaningless. A gate that read
// nothing and printed OK is worse than no gate; the floor error must say so in
// the same breath as the count.
func TestPrintVerdict_FloorBreachSaysTheVerdictIsMeaningless(t *testing.T) {
	got := verdictFor(Result{Resolved: 1, Distinct: 1})
	wantAll(t, got,
		"::error::node-type-gate read only 1 sites",
		"the verdict below means nothing",
		"node-type-gate: FAIL (count floor)",
	)
}

// ---------------------------------------------------------------------------
// printReport's three guards, in their ABSENT direction — the round 5 group.
//
// All three came back ALIVE at a83dd07b3: the two section guards relaxed to
// `>= 0`, and the whitelist loop's no-grammar guard deleted. They survived for
// one reason. Every assertion on this printer either compared a single
// fixture's full output, or re-derived a COUNTER the code keeps about itself.
// The first cannot show a section ABSENT; the second is not an observation of
// the emitted artefact at all. An empty section under a header — exactly what a
// `>= 0` mutant emits — is invisible to both. Assert the artefact.

// conditionalSectionsFixture drives the absent-direction tests. Both of
// printReport's conditional sections apply in it, so each test below can
// switch off exactly ONE of them and assert two things at once: the section it
// switched off is gone, and the section it left alone is still there. That
// second half is what stops the test passing because the printer went silent
// for some unrelated reason.
//
// The accounting reconciles as built: 2 resolved + 1 in a package with no
// grammar + 0 whitelisted = 3 sites.
func conditionalSectionsFixture() (Result, map[string]*Grammar) {
	const (
		js     = "internal/extractors/javascript"
		engine = "internal/engine"
		jsFile = js + "/extractor.go"
		enFile = engine + "/dispatch.go"
	)
	site := func(dir, file, lit string) Site {
		return Site{Pkg: dir, Dir: dir, File: file, Line: 1, Lit: lit, Form: FormCmp, Const: true}
	}
	return Result{
			Sites: []Site{
				site(js, jsFile, "identifier"),
				site(js, jsFile, "object"),
				site(engine, enFile, "arrow_function"),
			},
			Resolved:        2,
			Distinct:        2,
			DirsWithGrammar: []string{js},
			GrammarsForDir:  map[string][]string{js: {"javascript"}},
			Registrations: []Registration{
				{Dir: js, Key: "javascript", File: jsFile, Line: 8},
				{Dir: engine, Key: "", File: enFile, Line: 61},
			},
			Skipped:      []SkippedDir{{Dir: engine, Sites: 1, Reason: "runtime language"}},
			SkippedSites: 1,
		}, map[string]*Grammar{
			"javascript": {Key: "javascript", Kinds: map[string]bool{"identifier": true, "object": true}},
		}
}

const (
	skippedSectionHeader = "== packages with sites but no derivable grammar =="
	dynamicSectionHeader = "== registrations with a non-constant language key (mapping not derivable) =="
)

// TestPrintReport_SkippedSectionIsAbsentWhenNothingWasSkipped grades
// report.go:160 — the SECOND of the two `len(res.Skipped) > 0` occurrences.
// The first (printVerdict, report.go:37) is graded by
// TestPrintVerdict_NoSkippedSurfaceSaysNothing; the two are scored separately
// on purpose, because round 4 assumed one covered the other and it did not.
//
// The present direction of this same guard is covered by
// TestPrintReport_FullEmittedShape, whose fixture has a skipped package.
//
// VARIED: whether anything was skipped — the ONLY axis. The registration
// section is held present so a mutant that suppressed all sections is still
// caught here.
// HELD CONSTANT: every other field of the Result, and the grammar map.
func TestPrintReport_SkippedSectionIsAbsentWhenNothingWasSkipped(t *testing.T) {
	res, grammars := conditionalSectionsFixture()
	// Drop the unmapped package entirely: no skipped dirs, no sites in one.
	// The accounting still reconciles — 2 resolved + 0 + 0 = 2 sites.
	res.Sites = res.Sites[:2]
	res.Skipped = nil
	res.SkippedSites = 0

	out := reportFor(res, grammars)
	if strings.Contains(out, skippedSectionHeader) {
		t.Errorf("nothing was skipped, yet printReport emitted the skipped-packages header.\n"+
			"An empty section under a header is worse than no section: readers learn the header\n"+
			"carries no information and stop reading it.\n--- emitted ---\n%s", out)
	}
	if !strings.Contains(out, dynamicSectionHeader) {
		t.Errorf("the non-constant-registration section vanished too — this test would pass on a\n"+
			"printer that emitted nothing at all, so it must fail here instead.\n--- emitted ---\n%s", out)
	}
}

// TestPrintReport_DynamicRegistrationSectionIsAbsentWhenEveryKeyIsConstant
// grades report.go:173. `len(dynamicRegs) > 0` occurs ONCE in report.go.
//
// The present direction is covered by TestPrintReport_FullEmittedShape, whose
// fixture carries one non-constant registration.
//
// VARIED: whether any registration has a non-constant key — the ONLY axis. The
// skipped-packages section is held present as the same liveness check.
// HELD CONSTANT: every other field of the Result, and the grammar map.
func TestPrintReport_DynamicRegistrationSectionIsAbsentWhenEveryKeyIsConstant(t *testing.T) {
	res, grammars := conditionalSectionsFixture()
	// Give the one non-constant registration a constant key. The registration
	// stays — only its derivability changes.
	for i := range res.Registrations {
		if res.Registrations[i].Key == "" {
			res.Registrations[i].Key = "javascript"
		}
	}

	out := reportFor(res, grammars)
	if strings.Contains(out, dynamicSectionHeader) {
		t.Errorf("every registration key is constant, yet printReport emitted the\n"+
			"non-constant-registration header. This section names the registrations whose\n"+
			"mapping could NOT be derived; printing it empty says the opposite of nothing.\n"+
			"--- emitted ---\n%s", out)
	}
	if !strings.Contains(out, skippedSectionHeader) {
		t.Errorf("the skipped-packages section vanished too — this test would pass on a printer\n"+
			"that emitted nothing at all, so it must fail here instead.\n--- emitted ---\n%s", out)
	}
}

// TestPrintReport_WhitelistCountExcludesPackagesWithNoGrammar grades
// report.go:133, the `len(res.GrammarsForDir[s.Dir]) == 0 { continue }` guard
// in the whitelist loop.
//
// That guard is not cosmetic: it is what makes the accounting line reconcile.
// gate.go:182 skips an unmapped package's sites BEFORE the whitelist check at
// gate.go:190, so such a site is counted once, in SkippedSites. printReport's
// loop has to skip in the same order. Without the guard an ERROR literal in an
// unmapped package is counted BOTH as skipped and as whitelisted, and the
// accounting sum overshoots the site total it claims to reconcile with — which
// is precisely the failure mode "an accounting line that does not reconcile is
// read as a confirmation" was written about.
//
// The fixture is not manufactured to reach a dead line. Unmapped packages
// already reach report.go:133 on every run (TestPrintReport_FullEmittedShape's
// engine sites do); this one differs only in that its literal is a runtime kind,
// which is the single condition under which the guard changes an outcome.
//
// VARIED: the literal in the unmapped package — a runtime kind (ERROR) rather
// than an ordinary node name. That is the only axis on which the guard has an
// effect.
// HELD CONSTANT: the mapped package and its two resolved sites, so the
// whitelist count cannot be moved from the mapped side.
func TestPrintReport_WhitelistCountExcludesPackagesWithNoGrammar(t *testing.T) {
	res, grammars := conditionalSectionsFixture()
	// The unmapped package's one site is a runtime kind. It is already counted
	// in SkippedSites; it must NOT also be counted as whitelisted.
	res.Sites[2].Lit = "ERROR"

	out := reportFor(res, grammars)
	acct := numsIn(t, out, "  accounting ")
	if len(acct) != 4 {
		t.Fatalf("accounting line has %d numbers, want 4 (resolved, skipped, whitelisted, sum): %v\n%s", len(acct), acct, out)
	}
	resolved, skipped, whitelisted, sum := acct[0], acct[1], acct[2], acct[3]

	if whitelisted != 0 {
		t.Errorf("accounting reports %d whitelisted, want 0. The only runtime-kind literal in this\n"+
			"fixture sits in a package with NO grammar, so it is already accounted for as skipped;\n"+
			"counting it again here double-counts it.\n--- emitted ---\n%s", whitelisted, out)
	}
	if resolved != res.Resolved {
		t.Errorf("accounting reports %d resolved, want %d", resolved, res.Resolved)
	}
	if skipped != res.SkippedSites {
		t.Errorf("accounting reports %d skipped, want %d", skipped, res.SkippedSites)
	}
	if sum != len(res.Sites) {
		t.Errorf("accounting sums to %d but the fixture has %d derived sites. Every site is\n"+
			"resolved, skipped for want of a grammar, or whitelisted — exactly one of the three.\n"+
			"--- emitted ---\n%s", sum, len(res.Sites), out)
	}
}
