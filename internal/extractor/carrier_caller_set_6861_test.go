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
// body against its size on disk (a token check is satisfied by a truncating
// read), a floor on the anchor list itself, and a floor on the call sites found.
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
	// final identifier, so
	// it is independent of the import alias the caller uses — the javascript
	// extractor imports this package as `extreg`, and a qualifier-anchored
	// matcher would silently miss such a caller.
	prependFileCarrier = "PrependFileCarrier"
	fileCarrierFor     = "FileCarrierFor"
	// tagEntitiesLanguage is the helper whose presence in a caller's package
	// decides whether an empty lang is observable there.
	tagEntitiesLanguage = "TagEntitiesLanguage"
)

// nonTaggingCallers is the roster the prose used to carry: the caller packages
// that do NOT run TagEntitiesLanguage, and are therefore the callers for which
// the lang argument is load-bearing rather than cosmetic. Each names the tests
// that grade its carrier's token — the tests that fail if lang is mutated to
// "" — and the guard asserts those tests exist in that package.
//
// A caller package that tags needs no entry: TagEntitiesLanguage fills an empty
// Language with the extractor's own token, so the stated equivalence covers it.
// A caller package that does NOT tag and is absent here is precisely the shape
// the equivalence does not cover, and the guard fails on it.
// rosterEntry is one caller package that does not tag, plus the tests that
// grade the token its carrier keeps.
type rosterEntry struct {
	dir   string
	tests []string
}

var nonTaggingCallers = []rosterEntry{
	{"internal/extractors/bicep", []string{"TestBicep_CarrierShape_6852"}},
	{"internal/extractors/hcl", []string{
		"TestTerraform_CarrierShape_6852",
		"TestHCLToken_ImportsFromEndResolves_6852",
	}},
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

	// ---- per-caller classification --------------------------------------
	//
	// The decision is a pure function of (callers, tagging, roster) so a
	// control test can drive it with inputs this tree does not contain. That
	// separation is what grades the guard's own clauses: every branch below is
	// SATISFIED on the live tree today, and a satisfied branch can be deleted
	// without any test noticing unless something exercises it from the other
	// side. TestCarrierCallerSetDecisionCatchesEachDrift_6861 is that side.
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
