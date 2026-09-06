package extractor

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/repowalk"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// carrier_caller_set_6861_test.go — #6861.
//
// file_carrier.go used to state its grading status as a MEASURED FACT ABOUT ITS
// CALLER SET: "all three current callers run extractor.TagEntitiesLanguage
// afterwards, so passing an empty lang is equivalent under the suite". That
// sentence was true when written and nothing observed it afterwards, so #6852's
// language arms falsified it twice in two consecutive PRs — bicep made it four
// callers with one non-tagging, terraform made it five with two — each arm
// correcting the count and the caller names as unplanned work, with ten arms
// still queued behind them.
//
// The count is the wrong thing to write down. This file takes the roster off
// the prose and puts it under a test: the prose now states only what is
// INVARIANT (a wrong lang token is caught for every caller; an empty one is
// caught only for a caller that does not tag), and the guard below reads the
// caller set out of the source tree on every run.
//
// WHY THE GUARD MUST GRADE ITS OWN ENUMERATION. A caller-set guard that finds
// zero callers reports the same PASS as one that finds five and likes them all,
// so an emptied or narrowed scan is the original defect wearing the fix's
// clothes. Everything the scan depends on is therefore itself asserted: a floor
// on the files walked, must-scan anchors naming specific production files (a
// floor alone does not catch an inverted .go/_test.go filter — inverting it
// reads MORE files here), a byte-for-byte comparison of each anchor's collected
// body against its size on disk, a floor on the anchor list itself, and a floor
// on the call sites found.
//
// The length comparison and the token check overlap, and where they do NOT is
// worth stating because the obvious reasoning about it is backwards. A LARGE
// truncation is caught by either: the anchor tokens sit deep enough in their
// files that a half-file prefix loses them. It is the SMALL cut the tokens miss
// — a one-byte prefix keeps every token and still hides the tail of the file
// from the matcher — and that is the cut the length comparison exists for. Both
// are scored, the small one against the length check alone.
//
// WHAT IT COSTS, measured rather than waved at. This file is the dominant cost
// of the package's suite: warm, internal/extractor runs in ~0.2s without it and
// ~1.5s with it. It also holds every scanned body in memory at once — ~57MB
// across ~4900 files — because the walk-integrity block must compare the EXACT
// bytes handed to the parser against the file on disk, so the bodies have to
// outlive the read. Dropping the non-test bodies after the parse would
// recover almost all of that, and is not done here on purpose: it works only
// while the anchor check runs before the scan, and an unstated ordering
// invariant of that kind is the shape of defect this whole file exists to
// remove. If the cost starts to matter, make the retention explicit rather than
// incidental.
//
// WHY NOT internal/entkinds. entkinds does source scanning of exactly this
// shape and is the right thing to reuse when it fits — but its two Go entry
// points, ScanGo and ScanGoReferences, both answer in ENTITY KIND STRINGS:
// Site.Kind is a kind, and the reference scan takes a `wanted []string` of
// kinds to look for. Neither reports call sites of a named function, which is
// the whole question here, and its parse tree is unexported. What is shared is
// the layer below both: repowalk.SkippedDir, so this walk skips .claude/
// (which holds whole worktree checkouts), vendor/ and testdata/ the same way
// entkinds' does.
const (
	// prependFileCarrier and fileCarrierFor are the two exported entry points
	// whose caller set this guard grades. Matching is on the call expression's
	// final identifier, so it is independent of the import alias the caller
	// uses — the javascript extractor imports this package as `extreg`, and a
	// qualifier-anchored matcher would silently miss such a caller.
	prependFileCarrier = "PrependFileCarrier"
	fileCarrierFor     = "FileCarrierFor"
	// tagEntitiesLanguage is the helper whose presence in a caller's package
	// decides whether an empty lang is observable there.
	tagEntitiesLanguage = "TagEntitiesLanguage"
)

// rosterEntry is one caller package that does not tag, plus the tests NAMED as
// grading the token its carrier keeps. Named, not verified — see
// nonTaggingCallers for exactly what an entry does and does not prove.
type rosterEntry struct {
	dir   string
	tests []string
}

// nonTaggingCallers is the roster the prose used to carry: the caller packages
// that do NOT run TagEntitiesLanguage, and are therefore the callers for which
// the lang argument is load-bearing rather than cosmetic. Each names the tests
// that are meant to grade its carrier's token, and the guard asserts those
// tests exist in that package.
//
// A caller package that tags needs no entry: TagEntitiesLanguage fills an empty
// Language with the extractor's own token, so the stated equivalence covers it.
// A caller package that does NOT tag and is absent here is precisely the shape
// the equivalence does not cover, and the guard fails on it.
//
// # WHAT AN ENTRY DOES AND DOES NOT PROVE
//
// This is the one claim in this file that a reader is most likely to over-read,
// so it is stated flatly. An entry is graded for the EXISTENCE and LOCATION of
// the tests it names — a renamed test fails the guard, and a test of that name
// in a different package does not satisfy it. Nothing here grades their
// CONTENT. Replace the named test's body with `_ = t` and this guard still
// reports the caller covered; add the empty-token mutant on top and the
// caller's own package suite passes too. Two edits remove a caller's
// empty-token grading entirely while the roster goes on saying the requirement
// is met. Measured, not reasoned: both steps were run.
//
// So an entry is a WITNESS of coverage — a pointer for a human reader, and a
// tripwire that fires when the caller set and the roster drift apart. It is not
// proof that anything is graded. Pinning the property itself means running each
// named test under the mutant it exists to kill, i.e. mutation testing in CI,
// which is not affordable here; this is left undone deliberately rather than
// approximated by something that reads stronger than it is.
//
// The named tests DO grade the token as of this commit — with
// TestBicep_CarrierShape_6852 intact, mutating bicep's lang argument to "" dies
// at file_carrier_6852_test.go:302 ("carrier Language = \"\", want \"bicep\"").
// That is a measurement taken now, not a property this guard maintains, and the
// distinction is the entire subject of #6861: file_carrier.go's MEASURED
// paragraph was a true measurement too, on the day it was written.
//
// THE TAGGING HALF IS WEAKER STILL, not different in kind, and saying so here
// keeps the two from looking unlike each other. A package is classed as covered
// on the existence of a call expression named TagEntitiesLanguage anywhere in
// its non-test sources: no test is named for it, so nothing is checked about
// whether the tagging is observed by anything, and (see carrierScan6861) the
// call need not even be on a path that runs. Both halves grade the existence of
// a witness; neither grades the property the witness stands for.
var nonTaggingCallers = []rosterEntry{
	{"internal/extractors/bicep", []string{"TestBicep_CarrierShape_6852"}},
	{"internal/extractors/hcl", []string{
		"TestTerraform_CarrierShape_6852",
		"TestHCLToken_ImportsFromEndResolves_6852",
	}},
	{"internal/extractors/html", []string{"TestHTML_CarrierShape_6852"}},
	{"internal/extractors/astro", []string{"TestAstro_CarrierShape_6852"}},
}

// scannedFile is one file the walk read, kept as a name plus the FULL body the
// parser was handed — the same bytes, not a second copy, so the completeness
// check below interrogates what the matcher actually saw.
type scannedFile struct {
	rel  string
	body string
	test bool
}

// callSite is one call of a carrier entry point.
type callSite struct {
	rel  string
	line int
	fn   string
}

func TestCarrierCallerSetIsGradedFromSource_6861(t *testing.T) {
	root := repoRootFor6861(t)
	files := scanRepoGoFiles6861(t, root)

	// ---- walk integrity -------------------------------------------------
	//
	// The floors are floors, not counts: they exist to catch a walk that
	// collapsed, and were derived by pointing each at an absurd value once and
	// reading the number the failure printed, not picked by eye. The walk reads
	// 2087 non-test .go files today, so the floor sits well under that and
	// ordinary churn never trips it. minAnchors is the anchor list's own floor
	// (7 entries today). minExternalCallers, below, is at today's exact count
	// of 5 — #6852's remaining arms only ADD callers, so a floor there never
	// needs revisiting downward.
	//
	// The anchors are REDUNDANCY, not seven independent checks: every walk
	// mutant scored against this guard fires on the FIRST anchor
	// (file_carrier.go), so any six of the seven could be dropped without a
	// verdict changing. They are kept because which anchor a broken walk trips
	// on is not a property to rely on, and because each names a file this guard
	// makes a claim about. What the floor on the list catches is the case that
	// does change a verdict: emptying it.
	const minScannedNonTestFiles = 1500
	const minAnchors = 6
	mustScan := []struct {
		file   string
		tokens []string
	}{
		{"internal/extractor/file_carrier.go", []string{"func FileCarrierFor(", "func PrependFileCarrier("}},
		{"internal/extractor/extractor.go", []string{"func TagEntitiesLanguage("}},
		{"internal/extractors/erlang/extractor.go", []string{prependFileCarrier}},
		{"internal/extractors/nim/nim.go", []string{prependFileCarrier}},
		{"internal/extractors/groovy/groovy.go", []string{prependFileCarrier}},
		{"internal/extractors/bicep/extractor.go", []string{prependFileCarrier}},
		{"internal/extractors/hcl/extractor.go", []string{prependFileCarrier}},
		{"internal/extractors/html/extractor.go", []string{prependFileCarrier}},
		{"internal/extractors/fsharp/extractor.go", []string{prependFileCarrier}},
		{"internal/extractors/shell/shell.go", []string{prependFileCarrier}},
		{"internal/extractors/dockerfile/dockerfile.go", []string{prependFileCarrier}},
		{"internal/extractors/lua/lua.go", []string{prependFileCarrier}},
		{"internal/extractors/astro/extractor.go", []string{prependFileCarrier}},
		{"internal/extractors/clojure/clojure.go", []string{prependFileCarrier}},
	}

	nonTest := 0
	for _, f := range files {
		if !f.test {
			nonTest++
		}
	}
	if len(mustScan) < minAnchors {
		t.Fatalf("the anchor list itself has been narrowed to %d entries, want at least %d — "+
			"the anchors are what catch a walk that read the wrong files, so emptying them "+
			"disarms the check that the floor cannot make", len(mustScan), minAnchors)
	}
	if nonTest < minScannedNonTestFiles {
		t.Fatalf("the walk read only %d non-test .go files, want at least %d — below that the walk is "+
			"broken and an empty caller set means nothing", nonTest, minScannedNonTestFiles)
	}
	for _, m := range mustScan {
		var got *scannedFile
		for i := range files {
			if files[i].rel == m.file && !files[i].test {
				got = &files[i]
				break
			}
		}
		if got == nil {
			t.Fatalf("%s was never scanned — the walk read %d non-test files, but not the production "+
				"sources this guard grades (an inverted .go/_test.go filter reads MORE files, not fewer)",
				m.file, nonTest)
		}
		fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(m.file)))
		if err != nil {
			t.Fatalf("stat %s: %v", m.file, err)
		}
		if int64(len(got.body)) != fi.Size() {
			t.Fatalf("the walk collected %d bytes for %s but the file is %d on disk — it read a PREFIX, "+
				"so every call site past the cut is invisible to the matcher", len(got.body), m.file, fi.Size())
		}
		for _, tok := range m.tokens {
			if !strings.Contains(got.body, tok) {
				t.Fatalf("%s was scanned but the body collected for it does not contain %q — the walk "+
					"passed the right file NAME with the wrong content", m.file, tok)
			}
		}
	}

	// ---- the caller set, read from source --------------------------------
	sites, tags := carrierScan6861(t, files)

	// Both names are graded. FileCarrierFor has no caller outside this
	// package today, so a matcher narrowed to PrependFileCarrier alone would
	// find the same five external callers and report the same PASS; the one
	// call that pins the second name is PrependFileCarrier's own, here.
	selfSites := 0
	for _, s := range sites {
		if s.rel == "internal/extractor/file_carrier.go" && s.fn == fileCarrierFor {
			selfSites++
		}
	}
	if selfSites == 0 {
		t.Fatalf("the scan found no %s call in internal/extractor/file_carrier.go — that call is "+
			"PrependFileCarrier's own and is always there, so the matcher has been narrowed to "+
			"%s and no longer sees the other entry point at all", fileCarrierFor, prependFileCarrier)
	}

	const minExternalCallers = 5
	external := map[string][]callSite{}
	for _, s := range sites {
		dir := path6861Dir(s.rel)
		if dir == "internal/extractor" {
			continue // the definitions and PrependFileCarrier's own call
		}
		external[dir] = append(external[dir], s)
	}
	nExternal := 0
	for _, ss := range external {
		nExternal += len(ss)
	}
	if nExternal < minExternalCallers {
		t.Fatalf("the scan found %d non-test carrier call sites outside internal/extractor, want at "+
			"least %d — a caller-set guard that finds no callers passes vacuously, which is the "+
			"defect #6861 exists to remove", nExternal, minExternalCallers)
	}

	// hasTestFunc6861 is scoped to ONE package, and that scoping is graded
	// here rather than left to the control test — the control passes synthetic
	// always/never closures, so it never exercises the real lookup at all.
	// Dropping the directory comparison would let a roster entry be satisfied
	// by a test declared in any package in the tree, which is the permissive
	// direction: the entry would keep claiming a grading test that does not
	// exist where the caller is. Two assertions, one per direction, over names
	// that really do and really do not live in bicep.
	if !hasTestFunc6861(files, "internal/extractors/bicep", "TestBicep_CarrierShape_6852") {
		t.Fatalf("hasTestFunc6861 did not find TestBicep_CarrierShape_6852 in " +
			"internal/extractors/bicep, where it is declared — the lookup that backs every " +
			"roster entry answers no for a test that exists")
	}
	if hasTestFunc6861(files, "internal/extractors/bicep", "TestErlang_CarrierIsLanguageTagged_6815") {
		t.Fatalf("hasTestFunc6861 accepted a test declared in ANOTHER package " +
			"(TestErlang_CarrierIsLanguageTagged_6815 lives in internal/extractors/erlang, not " +
			"bicep) — the lookup is not scoped to the caller's own package, so a roster entry can " +
			"be satisfied by a test that grades a different extractor entirely")
	}

	// ---- per-caller classification --------------------------------------
	//
	// The decision is a pure function of (callers, tagging, roster) so a
	// control test can drive it with inputs this tree does not contain. That
	// separation is what grades the guard's own clauses: every branch below is
	// SATISFIED on the live tree today, and a satisfied branch can be deleted
	// without any test noticing unless something exercises it from the other
	// side. TestCarrierCallerSetDecisionCatchesEachDrift_6861 is that side.
	//
	// GRADING STOPS AT THE t.Error LOOP BELOW, and that is stated rather than
	// papered over. Deleting the call plus the loop — i.e. running the whole
	// scan and then reporting nothing — PASSES, and no honest control inside
	// this binary kills it: any wrapper asserting "the loop ran" would be one
	// more terminal assertion with the same property one level out. What the
	// loop is NOT is an ungraded no-op traded for a graded one: mutants that
	// neuter the inputs it reports on (the tagging map, the roster, the scan)
	// all die THROUGH this loop, so the wiring from scan to failure is
	// exercised on every run. The residue is the deliberate deletion of a
	// terminal assertion that currently passes, which is where this guard, like
	// every other, stops.
	problems := checkCallerSet6861(external, tags, nonTaggingCallers,
		func(dir, name string) bool { return hasTestFunc6861(files, dir, name) })
	for _, p := range problems {
		t.Error(p)
	}
}

// checkCallerSet6861 grades one caller set against one roster and reports every
// way the two disagree with the invariant file_carrier.go states.
//
// Four disagreements, in both directions, because a roster is wrong when it is
// missing an entry AND when it keeps one it no longer earns:
//
//  1. a caller that does not tag and is not in the roster — the shape the
//     stated equivalence does not cover, and the one #6852's arms keep adding;
//  2. a roster entry whose package DOES tag — an empty lang is no longer
//     observable there, so the entry records something untrue;
//  3. a roster entry for a package that is not a caller at all — stale;
//  4. a roster entry naming no tests, or a test its package does not declare —
//     an entry that records a caller nothing actually grades.
func checkCallerSet6861(
	external map[string][]callSite,
	tags map[string]bool,
	roster []rosterEntry,
	hasTest func(dir, name string) bool,
) []string {
	listed := map[string][]string{}
	for _, e := range roster {
		listed[e.dir] = e.tests
	}

	var dirs []string
	for d := range external {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	var out []string
	for _, d := range dirs {
		_, inRoster := listed[d]
		if tags[d] {
			if inRoster {
				out = append(out, fmt.Sprintf("%s is listed as a NON-TAGGING caller but its package "+
					"does call %s — the roster has drifted the other way; an empty lang is no longer "+
					"observable there, so the entry (and the tests it names) no longer grade what "+
					"they claim", d, tagEntitiesLanguage))
			}
			continue
		}
		if !inRoster {
			out = append(out, fmt.Sprintf("%s calls %s at %s but never calls %s, and is not in "+
				"nonTaggingCallers — this is exactly the caller shape the stated equivalence does "+
				"NOT cover: its carrier keeps whatever token the lang argument is given, so mutating "+
				"that argument to \"\" must fail a test in that package. Add the entry naming those "+
				"tests (see #6861), or, if the caller should tag, make it tag",
				d, prependFileCarrier, sitesString6861(external[d]), tagEntitiesLanguage))
		}
	}

	for _, e := range roster {
		if _, ok := external[e.dir]; !ok {
			out = append(out, fmt.Sprintf("nonTaggingCallers lists %s, but the scan found no carrier "+
				"call site there — the roster is stale", e.dir))
			continue
		}
		if len(e.tests) == 0 {
			out = append(out, fmt.Sprintf("nonTaggingCallers lists %s with no grading tests — the "+
				"entry then records a caller whose lang argument nothing observes", e.dir))
		}
		for _, name := range e.tests {
			if !hasTest(e.dir, name) {
				out = append(out, fmt.Sprintf("nonTaggingCallers says %s grades %s's carrier token, "+
					"but no _test.go in that package declares func %s(", name, e.dir, name))
			}
		}
	}
	return out
}

// TestCarrierCallerSetDecisionCatchesEachDrift_6861 is the positive control for
// checkCallerSet6861. Without it every clause in that function is a branch the
// live tree already satisfies, and deleting one is invisible: the guard would
// report PASS for the same reason it reports PASS today.
//
// Each case is one drift, driven through synthetic inputs rather than through
// the real tree, so it stays a control after #6852's remaining arms change what
// the real tree contains.
func TestCarrierCallerSetDecisionCatchesEachDrift_6861(t *testing.T) {
	site := func(dir string) map[string][]callSite {
		return map[string][]callSite{dir: {{rel: dir + "/x.go", line: 7, fn: prependFileCarrier}}}
	}
	always := func(string, string) bool { return true }
	never := func(string, string) bool { return false }

	cases := []struct {
		name     string
		external map[string][]callSite
		tags     map[string]bool
		roster   []rosterEntry
		hasTest  func(string, string) bool
		want     string
	}{
		{
			name:     "a non-tagging caller absent from the roster",
			external: site("internal/extractors/newlang"),
			tags:     map[string]bool{},
			roster:   nil,
			hasTest:  always,
			want:     "is not in nonTaggingCallers",
		},
		{
			name:     "a roster entry whose package now tags",
			external: site("internal/extractors/newlang"),
			tags:     map[string]bool{"internal/extractors/newlang": true},
			roster:   []rosterEntry{{"internal/extractors/newlang", []string{"TestX"}}},
			hasTest:  always,
			want:     "is listed as a NON-TAGGING caller but its package does call",
		},
		{
			name:     "a roster entry for a package that is not a caller",
			external: site("internal/extractors/newlang"),
			tags:     map[string]bool{"internal/extractors/newlang": true},
			roster:   []rosterEntry{{"internal/extractors/gone", []string{"TestX"}}},
			hasTest:  always,
			want:     "the roster is stale",
		},
		{
			name:     "a roster entry naming no tests",
			external: site("internal/extractors/newlang"),
			tags:     map[string]bool{},
			roster:   []rosterEntry{{"internal/extractors/newlang", nil}},
			hasTest:  always,
			want:     "with no grading tests",
		},
		{
			name:     "a roster entry naming a test its package does not declare",
			external: site("internal/extractors/newlang"),
			tags:     map[string]bool{},
			roster:   []rosterEntry{{"internal/extractors/newlang", []string{"TestNoSuchThing"}}},
			hasTest:  never,
			want:     "no _test.go in that package declares func",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkCallerSet6861(c.external, c.tags, c.roster, c.hasTest)
			joined := strings.Join(got, "\n")
			if !strings.Contains(joined, c.want) {
				t.Fatalf("checkCallerSet6861 reported %q, want a problem containing %q — the clause "+
					"that catches this drift no longer fires", joined, c.want)
			}
		})
	}

	// The negative half: a caller set the invariant DOES cover reports nothing.
	// Without it every case above is satisfied by a function that returns a
	// problem unconditionally.
	clean := map[string][]callSite{
		"internal/extractors/tagger":   {{rel: "internal/extractors/tagger/x.go", line: 1, fn: prependFileCarrier}},
		"internal/extractors/notagger": {{rel: "internal/extractors/notagger/x.go", line: 1, fn: prependFileCarrier}},
	}
	got := checkCallerSet6861(clean,
		map[string]bool{"internal/extractors/tagger": true},
		[]rosterEntry{{"internal/extractors/notagger", []string{"TestNoTagger"}}},
		always)
	if len(got) != 0 {
		t.Fatalf("checkCallerSet6861 reported %v for a caller set the invariant covers — a guard that "+
			"always complains grades nothing", got)
	}
}

// repoRootFor6861 locates the repository root from this package's own directory
// rather than from the working directory, which `go test` sets per package.
func repoRootFor6861(t *testing.T) string {
	t.Helper()
	dir, err := testsupport.PackageDirOfCaller(0)
	if err != nil {
		t.Fatalf("locate package dir: %v", err)
	}
	root := filepath.Dir(filepath.Dir(dir)) // internal/extractor -> internal -> repo
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolved repo root %q has no go.mod: %v", root, err)
	}
	return root
}

// scanRepoGoFiles6861 reads every .go file under root, skipping the directories
// repowalk skips — .claude/ above all, which holds whole worktree checkouts and
// would multiply every count here.
func scanRepoGoFiles6861(t *testing.T, root string) []scannedFile {
	t.Helper()
	var out []scannedFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && repowalk.SkippedDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, scannedFile{
			rel:  filepath.ToSlash(rel),
			body: string(src),
			test: strings.HasSuffix(d.Name(), "_test.go"),
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// carrierScan6861 parses every non-test file the walk collected — ONCE, from
// the same bytes the completeness check above graded — and answers both
// questions from that single pass: where the carrier entry points are called,
// and which package directories call TagEntitiesLanguage.
//
// There is no cheap "does the body mention the name" prefilter in front of the
// parse. A prefilter here is one more thing that can be narrowed without any
// test noticing: dropping fileCarrierFor from a two-name Contains() check
// leaves today's answer unchanged, because the only file that calls
// FileCarrierFor also happens to mention PrependFileCarrier — so that mutant
// survives, and it is a real narrowing the moment some file calls the one
// without naming the other. Parsing everything removes the knob rather than
// documenting it.
//
// Tagging is keyed by PACKAGE directory, not by enclosing function or line
// number, because the tag call is routinely in a different function from the
// carrier call, later in the flow but EARLIER in the file — erlang prepends the
// carrier inside extractErlang and tags in Extract, which calls it. A
// line-ordered check would report erlang as non-tagging, which is false.
//
// THE PACKAGE RULE OVER-APPROXIMATES, in the false-negative direction, and the
// hole is stated rather than left to be discovered. Any TagEntitiesLanguage
// call anywhere in the package marks the whole package as tagging — including
// one in a different file, on a code path that never reaches the carrier, or in
// a dead function called by nothing. Such a package silences the roster
// requirement for the path that actually mints the carrier, which is a miss in
// the very check this guard exists for. Closing it needs reachability from the
// carrier call to the tag call, not a source scan; erlang is what rules out the
// cheap approximations (line order, same function, same file), so this is the
// coarsest rule that is not simply wrong about a live caller.
func carrierScan6861(t *testing.T, files []scannedFile) (sites []callSite, tags map[string]bool) {
	t.Helper()
	tags = map[string]bool{}
	for _, f := range files {
		if f.test {
			continue
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, f.rel, f.body, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", f.rel, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := calleeName6861(call.Fun)
			switch name {
			case prependFileCarrier, fileCarrierFor:
				sites = append(sites, callSite{
					rel:  f.rel,
					line: fset.Position(call.Pos()).Line,
					fn:   name,
				})
			case tagEntitiesLanguage:
				tags[path6861Dir(f.rel)] = true
			}
			return true
		})
	}
	return sites, tags
}

// hasTestFunc6861 reports whether some _test.go in dir declares func name.
func hasTestFunc6861(files []scannedFile, dir, name string) bool {
	for _, f := range files {
		if !f.test || path6861Dir(f.rel) != dir {
			continue
		}
		if strings.Contains(f.body, "func "+name+"(") {
			return true
		}
	}
	return false
}

// calleeName6861 returns the final identifier of a call's function expression,
// so `extractor.PrependFileCarrier`, `extreg.PrependFileCarrier` and a
// package-local `PrependFileCarrier` all answer the same.
func calleeName6861(fun ast.Expr) string {
	switch e := fun.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}

func path6861Dir(rel string) string {
	i := strings.LastIndex(rel, "/")
	if i < 0 {
		return "."
	}
	return rel[:i]
}

func sitesString6861(ss []callSite) string {
	var parts []string
	for _, s := range ss {
		parts = append(parts, fmt.Sprintf("%s:%d", s.rel, s.line))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
