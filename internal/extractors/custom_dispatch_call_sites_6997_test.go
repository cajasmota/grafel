package extractors_test

// #6997 — the three RunCustomExtractors dispatch sites must carry the SAME two
// guards, and a fourth site must not be able to appear with a fourth
// combination in silence.
//
// WHAT THE TWO GUARDS ARE, AND WHY NEITHER IS OPTIONAL.
//
//   - `CustomExtractorsEnabled(cfg)` is the whole custom-extractor gate — the
//     config half and the env half (custom_gate.go). #6966 keeps it
//     default-OFF and treats flipping it as the owner's decision. A dispatch
//     site that does not consult it makes that decision locally, for whichever
//     users happen to be on that path.
//   - `TSTree != nil` is load-bearing for a reason independent of the gate: a
//     subset of custom extractors work on file CONTENT and emit entities with
//     no parse tree, so a site without it produces custom entities for files
//     that FAILED TO PARSE — the exact inputs the indexer decided it could not
//     understand. "Make them agree" therefore cannot be satisfied downward, by
//     deleting the guard from the two sites that have it.
//
// WHY A SCAN AND NOT THREE HAND-WRITTEN ASSERTIONS. Before this test, nothing
// failed when the sites disagreed — internal/daemon/extract/subproc.go
// dispatched under `runExtract` alone and had done so for its whole life. A
// test naming three files can only ever re-state today's population; the
// failure mode is the FOURTH site. So the population is derived by walking
// every non-test .go file in the repo, and set equality against the frozen
// list below is asserted in BOTH directions: a new site fails, and deleting or
// moving one fails until this list is edited to match.
//
// WHY THE CONDITION IS EXPANDED RATHER THAN MATCHED AS TEXT. subproc.go's
// guard reads `if runCustom && file.TSTree != nil`, with `runCustom` bound
// earlier in the same function to `runExtract && CustomExtractorsEnabled(...)`.
// A literal text match on the `if` condition would score that site as
// unguarded and push the next author towards inlining a long expression into a
// hot loop to satisfy a test. Single assignments within the enclosing function
// are therefore substituted before the guard is judged.
//
// GRADED PART BY PART. Each site is checked for the two guards SEPARATELY and
// reported separately, because `A && B` scored only as a unit grades neither
// half: dropping only the gate, or only the nil-tree check, must each be a
// distinct failure.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// dispatchScanRoot is the tree walked, relative to this package's directory
// (where `go test` runs). The whole repo: a fourth dispatch site is not
// obliged to appear in a package this test could have predicted.
const dispatchScanRoot = "../.."

// dispatcherName is the only dispatcher that selects a prefixed custom
// extractor key; ordinary dispatch is an exact-key Get(file.Language) lookup
// (registry.go) which cannot match "python_django".
const dispatcherName = "RunCustomExtractors"

// knownDispatchSites is the frozen population, as `<path>|<enclosing func>`.
// HAND-WRITTEN, deliberately not derived from the scan it grades: a list built
// from the thing under test cannot detect an addition to that thing.
//
// Line numbers are omitted on purpose — they rot on every edit above the site,
// and a guard that must be re-baselined for unrelated edits gets re-baselined
// without being read.
var knownDispatchSites = []string{
	"cmd/grafel/index.go|classifyAndReadWithProgress",
	"internal/daemon/extract/subproc.go|Run",
	"internal/extractors/incremental.go|tryIncremental",
}

// skipDirs are trees with no production dispatch site in them. Kept short: a
// long skip list is how a scan quietly stops covering the repo.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "testdata": true, "vendor": true,
	"dist": true, "build": true, ".claude": true,
}

type dispatchSite struct {
	key       string
	guards    []string // expanded conjuncts of every enclosing if-condition
	hasGate   bool
	hasNilChk bool
}

func TestRunCustomExtractorsCallSitesCarryBothGuards6997(t *testing.T) {
	sites := scanDispatchSites6997(t)

	if len(sites) == 0 {
		// A scan that finds nothing reports "all sites guarded" — the vacuous
		// pass this test exists to make impossible.
		t.Fatalf("scan found no %s call sites at all; the scanner is broken, "+
			"not the repo (root %s)", dispatcherName, dispatchScanRoot)
	}

	// COVERAGE CHECK — the scanner must not be able to MISS a dispatch.
	//
	// scanDispatchSites6997 descends into func declarations and matches a
	// direct callee, so on review two fourth sites slipped past it with the
	// whole suite green: one written as a package-level `var x = func(...)`,
	// and one called through a function VALUE (`d := RunCustomExtractors;
	// d(ctx, f)`). The second shape is not hypothetical here — `extract.Run`
	// itself reaches production only as a function value (`Hooks{RunExtract:
	// …}`), so the net was blind to the calling convention the surrounding
	// code already uses.
	//
	// Every mention of the dispatcher in the scanned files is therefore
	// counted by an INDEPENDENT traversal (its own walk, its own parse, its
	// own visitor) and required to be accounted for by a collected site. A
	// count derived from the collection it grades could not detect an omission
	// in that collection — which is the shape of failure this whole test
	// exists to prevent, one level down.
	idents, filesScanned := scanDispatcherMentions6997(t)
	if filesScanned == 0 || len(idents) == 0 {
		t.Fatalf("the independent mention scan saw %d file(s) and %d mention(s) of %s; "+
			"either would make the coverage check below pass trivially",
			filesScanned, len(idents), dispatcherName)
	}
	if len(idents) != len(sites) {
		t.Errorf("the scanner did not attribute every %s mention to a dispatch site: "+
			"%d mention(s) across %d file(s), %d site(s) collected.\n  mentions:\n    %s\n"+
			"A mention the site walk cannot see is a dispatch this test cannot grade — "+
			"a package-level `var x = func(...)`, or a call through a function value. "+
			"Teach scanDispatchSites6997 that shape rather than relaxing this count.",
			dispatcherName, len(idents), filesScanned, len(sites), strings.Join(idents, "\n    "))
	}

	var found []string
	for _, s := range sites {
		found = append(found, s.key)
	}
	sort.Strings(found)
	want := append([]string(nil), knownDispatchSites...)
	sort.Strings(want)

	if strings.Join(found, "\n") != strings.Join(want, "\n") {
		t.Errorf("the set of %s dispatch sites changed.\n  found:\n    %s\n  frozen:\n    %s\n\n"+
			"A NEW site must carry BOTH guards (see this file's header) and be added "+
			"to knownDispatchSites. A REMOVED site must be deleted from that list.",
			dispatcherName, strings.Join(found, "\n    "), strings.Join(want, "\n    "))
	}

	for _, s := range sites {
		if !s.hasGate {
			t.Errorf("%s: dispatches %s WITHOUT the custom-extractor gate "+
				"(CustomExtractorsEnabled). Guards in scope: %v.\n"+
				"Without it this path decides #6966's default-OFF question locally, "+
				"for whoever happens to be on it.", s.key, dispatcherName, s.guards)
		}
		if !s.hasNilChk {
			t.Errorf("%s: dispatches %s WITHOUT the `TSTree != nil` guard. "+
				"Guards in scope: %v.\n"+
				"Content-only custom extractors emit entities with no parse tree, so "+
				"this site produces custom entities for files that FAILED to parse.",
				s.key, dispatcherName, s.guards)
		}
	}
}

// scanDispatchSites6997 walks the repo and returns one record per
// non-test call to RunCustomExtractors, with every enclosing if-condition
// expanded through single assignments in the enclosing function.
func scanDispatchSites6997(t *testing.T) []dispatchSite {
	t.Helper()
	root, err := filepath.Abs(dispatchScanRoot)
	if err != nil {
		t.Fatalf("abs(%s): %v", dispatchScanRoot, err)
	}
	var out []dispatchSite
	fset := token.NewFileSet()

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil // unreadable trees are not evidence of absence we can act on
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		if !strings.Contains(string(src), dispatcherName) {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, src, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", p, perr)
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			binds := singleAssignments6997(fset, fn)
			collectSitesIn6997(fset, rel, fn.Name.Name, fn.Body, nil, binds, &out)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// collectSitesIn6997 walks a statement tree carrying the stack of enclosing
// if-conditions, so a call's guards are the conditions that actually dominate
// it rather than whatever `if` happens to sit nearest in the file.
func collectSitesIn6997(fset *token.FileSet, rel, fnName string, n ast.Node, guards []string, binds map[string]string, out *[]dispatchSite) {
	switch s := n.(type) {
	case *ast.IfStmt:
		inner := append(append([]string(nil), guards...), conjuncts6997(fset, s.Cond, binds)...)
		if s.Init != nil {
			collectSitesIn6997(fset, rel, fnName, s.Init, guards, binds, out)
		}
		collectSitesIn6997(fset, rel, fnName, s.Body, inner, binds, out)
		if s.Else != nil {
			// An else branch is NOT guarded by the condition.
			collectSitesIn6997(fset, rel, fnName, s.Else, guards, binds, out)
		}
		return
	case *ast.BlockStmt:
		for _, st := range s.List {
			collectSitesIn6997(fset, rel, fnName, st, guards, binds, out)
		}
		return
	}

	// Any other node: record calls directly inside it, then recurse into
	// children with the same guard stack (a nested if is handled above when
	// reached, because ast.Inspect visits it as an *ast.IfStmt).
	ast.Inspect(n, func(x ast.Node) bool {
		if x == nil {
			return false
		}
		if ifs, ok := x.(*ast.IfStmt); ok && ifs != n {
			collectSitesIn6997(fset, rel, fnName, ifs, guards, binds, out)
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !isDispatcherCall6997(call.Fun) {
			return true
		}
		site := dispatchSite{key: rel + "|" + fnName, guards: guards}
		for _, g := range guards {
			if strings.Contains(g, "CustomExtractorsEnabled") {
				site.hasGate = true
			}
			if strings.Contains(strings.ReplaceAll(g, " ", ""), "TSTree!=nil") {
				site.hasNilChk = true
			}
		}
		*out = append(*out, site)
		return true
	})
}

func isDispatcherCall6997(fun ast.Expr) bool {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name == dispatcherName
	case *ast.SelectorExpr:
		return f.Sel.Name == dispatcherName
	}
	return false
}

// singleAssignments6997 maps each identifier assigned EXACTLY ONCE in fn to the
// source text of its right-hand side. Identifiers assigned more than once are
// dropped: substituting one of several values would be a guess.
func singleAssignments6997(fset *token.FileSet, fn *ast.FuncDecl) map[string]string {
	rhs := map[string]string{}
	count := map[string]int{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, l := range as.Lhs {
			id, ok := l.(*ast.Ident)
			if !ok {
				continue
			}
			count[id.Name]++
			rhs[id.Name] = exprText6997(fset, as.Rhs[i])
		}
		return true
	})
	for name, c := range count {
		if c > 1 {
			delete(rhs, name)
		}
	}
	return rhs
}

// conjuncts6997 splits a boolean condition into its top-level && operands and
// expands single-assignment identifiers inside each, bounded to a few rounds so
// a cyclic binding cannot hang the scan.
func conjuncts6997(fset *token.FileSet, cond ast.Expr, binds map[string]string) []string {
	var parts []ast.Expr
	var split func(ast.Expr)
	split = func(e ast.Expr) {
		if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.LAND {
			split(b.X)
			split(b.Y)
			return
		}
		parts = append(parts, e)
	}
	split(cond)

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		txt := exprText6997(fset, p)
		for round := 0; round < 4; round++ {
			expanded := txt
			for name, val := range binds {
				if identUsed6997(txt, name) {
					expanded = strings.ReplaceAll(expanded, name, "("+val+")")
				}
			}
			if expanded == txt {
				break
			}
			txt = expanded
		}
		out = append(out, txt)
	}
	return out
}

// identUsed6997 reports whether name appears in txt as a whole identifier
// rather than as a substring of a longer one.
func identUsed6997(txt, name string) bool {
	isIdentRune := func(r byte) bool {
		return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
	}
	for i := 0; ; {
		j := strings.Index(txt[i:], name)
		if j < 0 {
			return false
		}
		j += i
		before := j == 0 || !isIdentRune(txt[j-1])
		after := j+len(name) >= len(txt) || !isIdentRune(txt[j+len(name)])
		if before && after {
			return true
		}
		i = j + len(name)
	}
}

func exprText6997(fset *token.FileSet, e ast.Expr) string {
	var sb strings.Builder
	if err := printer.Fprint(&sb, fset, e); err != nil {
		return ""
	}
	return sb.String()
}

// scanDispatcherMentions6997 counts every mention of the dispatcher across the
// same non-test file set, and returns one "<path>:<line>:<col>" per mention
// plus the number of files it looked at.
//
// DELIBERATELY INDEPENDENT of scanDispatchSites6997: its own walk, its own
// parse, and a flat ast.Inspect over identifiers rather than a statement walk
// that has to understand guards, function literals or callee shapes. Sharing
// either the walk or the visitor would make it agree with the site scan by
// construction and detect nothing.
//
// The declaration's own name is not a mention: `func RunCustomExtractors(...)`
// in custom_dispatch.go is the thing being called, not a call of it.
func scanDispatcherMentions6997(t *testing.T) (mentions []string, files int) {
	t.Helper()
	root, err := filepath.Abs(dispatchScanRoot)
	if err != nil {
		t.Fatalf("abs(%s): %v", dispatchScanRoot, err)
	}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || (p != root && strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(p)
		if rerr != nil || !strings.Contains(string(src), dispatcherName) {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, src, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", p, perr)
		}
		files++
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		declNames := map[*ast.Ident]bool{}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				declNames[fn.Name] = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || id.Name != dispatcherName || declNames[id] {
				return true
			}
			pos := fset.Position(id.Pos())
			mentions = append(mentions, fmt.Sprintf("%s:%d:%d", rel, pos.Line, pos.Column))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(mentions)
	return mentions, files
}
