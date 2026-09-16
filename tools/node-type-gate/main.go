// Command node-type-gate fails the build when a tree-sitter matcher names a
// node type that the grammar does not have.
//
// # The defect it exists to catch
//
// Every tree-sitter predicate in this repo is a string literal compared against
// a grammar node kind. A literal naming a node the grammar does not have is a
// SILENT NO-OP: the guard stops guarding, the pass still runs, records and
// edges are still emitted, and every recall-shaped instrument we own reports
// success because something was produced. The scala arm of #6912 shipped
//
//	case "variant_type_parameter", "type_parameter":
//
// against tree-sitter-scala, which spells them covariant_type_parameter and
// contravariant_type_parameter. The guard was dead for the language's dominant
// generic form and emitted a wrong edge that BOUND; 26/26 bound was reported.
// It was found by a reviewer dumping the CST, not by the suite. #7065 has the
// audit: 42 genuine dead literals, one of them behaviour-visible (#7068,
// csharp's for_each_statement where the grammar says foreach_statement).
//
// # How it works
//
// Derive → resolve → fail.
//
//   - DERIVE. An AST scan with full type information (go/packages) over
//     ./internal/... and ./cmd/..., collecting every string literal that sits
//     in a node-type position: a comparison against x.Type(), a case of
//     switch x.Type(), an argument to a helper whose parameter reaches such a
//     position (discovered to a fixpoint, never hand-listed), and the keys of
//     a table indexed by x.Type(). Literals are CONSTANT-FOLDED through
//     types.Info, so hiding one behind `const K = string(types.X)` cannot
//     silently zero the site count. The switch and helper forms alone are 2.6x
//     the surface a grep for `.Type() == "…"` can see.
//   - RESOLVE. Against the node-kind symbol table of every grammar the package
//     can actually receive, read from the same ts.Language handles the daemon
//     parses with (treesitter.GrammarLanguages). The package → grammar mapping
//     is derived from the extractor.Register call sites crossed with that
//     registry; multi-grammar packages are handled per-grammar (cpp → c+cpp,
//     javascript → javascript+typescript+tsx, hcl → hcl+terraform).
//   - FAIL, only when a literal resolves in NONE of its package's grammars and
//     no baseline row covers it. A literal absent from one grammar but present
//     in another is never a failure: C++-only names in the shared c/cpp
//     package and TS-only names in the shared js/ts/tsx package are legitimate
//     and number in the dozens.
//
// ERROR and MISSING are whitelisted: tree-sitter produces them at parse time
// and lists them in no symbol table. The empty string is a sentinel, never a
// node kind, and is not reported.
//
// # What this gate does NOT catch
//
// It is structurally blind to a matcher that names a REAL node that is the
// WRONG node. #7063 is the shape: the kotlin nesting table held the nested
// declaration's class_modifier constant, so four modifiers went ungraded — every
// literal involved resolves perfectly. Likewise a literal that can never be a
// child of the node under test (right name, wrong parent) resolves and passes.
//
// The complement is the #7065 audit's SECOND detector, which stays a review
// requirement rather than being replaced by this tool: for any matcher on a
// load-bearing name, make it unmatchable and assert at least one test dies.
// Zero deaths is the signature of dead code. That detector catches the
// wrong-node class this one cannot, and it needs no machinery.
//
// It also does not check field names (ChildByFieldName), does not model which
// grammar internal/engine's runtime dispatch selects, and says nothing about
// the 58-odd positions where the compared value is not a constant — those are
// reported as dynamic rather than silently dropped.
//
// # Usage
//
//	go run ./tools/node-type-gate              # gate; exit 1 on a new dead literal
//	go run ./tools/node-type-gate -report      # the full derived surface
//	go run ./tools/node-type-gate -update      # rewrite baseline.txt line numbers
package main

import (
	"flag"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// scanPatterns are the only trees scanned. The repo root is NOT scanned:
// .claude/worktrees/ holds full checkouts of this repository, and including it
// would multiply every count.
var scanPatterns = []string{"./internal/...", "./cmd/..."}

const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedSyntax |
	packages.NeedTypes |
	packages.NeedTypesInfo

func main() {
	var (
		baselinePath = flag.String("baseline", "", "path to baseline.txt (default: alongside this tool)")
		modRoot      = flag.String("root", "", "module root (default: discovered from the working directory)")
		report       = flag.Bool("report", false, "print the full derived surface instead of only the verdict")
		update       = flag.Bool("update", false, "rewrite the baseline's line numbers from the current tree")
		dumpSites    = flag.Bool("sites", false, "dump every derived site as file:line<TAB>literal<TAB>form, for cross-checking the surface against a grep")
	)
	flag.Parse()

	root := *modRoot
	if root == "" {
		r, err := findModuleRoot()
		if err != nil {
			fatal(err)
		}
		root = r
	}
	bp := *baselinePath
	if bp == "" {
		bp = filepath.Join(root, "tools", "node-type-gate", "baseline.txt")
	}

	loaded, err := LoadSurface(root, scanPatterns)
	if err != nil {
		fatal(err)
	}
	grammars, err := loadGrammars()
	if err != nil {
		fatal(err)
	}
	base, err := readBaseline(bp)
	if err != nil {
		fatal(err)
	}

	res := Evaluate(loaded.Scan, loaded.Registrations, grammars, base)

	if *update {
		if err := updateBaseline(bp, base, res); err != nil {
			fatal(err)
		}
		fmt.Printf("node-type-gate: baseline refreshed at %s\n", bp)
		return
	}

	if *dumpSites {
		for _, s := range res.Sites {
			fmt.Printf("%s:%d\t%s\t%s\n", s.File, s.Line, s.Lit, s.Form)
		}
		for _, s := range res.Dynamic {
			fmt.Printf("%s:%d\t<dynamic>\t%s\n", s.File, s.Line, s.Form)
		}
		return
	}

	if *report {
		printReport(os.Stdout, res, grammars)
	}
	printVerdict(os.Stdout, res)
	os.Exit(ExitCode(res))
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "node-type-gate: %v\n", err)
	os.Exit(2)
}

// Surface is a loaded, scanned tree.
type Surface struct {
	Scan          Scan
	Registrations []Registration
}

// LoadSurface loads the packages under patterns (rooted at root) and derives
// the node-type literal surface from them.
func LoadSurface(root string, patterns []string) (*Surface, error) {
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode:  loadMode,
		Dir:   root,
		Fset:  fset,
		Tests: false,
	}
	return loadSurfaceWithConfig(cfg, root, patterns)
}

// LoadSurfaceOverlay is LoadSurface with an in-memory overlay applied. It is
// how the positive controls inject a defect into a real extractor file without
// mutating the working tree.
func LoadSurfaceOverlay(root string, patterns []string, overlay map[string][]byte) (*Surface, error) {
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode:    loadMode,
		Dir:     root,
		Fset:    fset,
		Tests:   false,
		Overlay: overlay,
	}
	return loadSurfaceWithConfig(cfg, root, patterns)
}

func loadSurfaceWithConfig(cfg *packages.Config, root string, patterns []string) (*Surface, error) {
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	// Errors are fatal: a package that failed to type-check would silently
	// contribute zero sites, which is the vacuity mode this whole tool exists
	// to distrust.
	var errs []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			errs = append(errs, p.PkgPath+": "+e.Error())
		}
	})
	if len(errs) > 0 {
		sort.Strings(errs)
		if len(errs) > 10 {
			errs = append(errs[:10], fmt.Sprintf("... and %d more", len(errs)-10))
		}
		return nil, fmt.Errorf("packages failed to load:\n  %s", strings.Join(errs, "\n  "))
	}

	// The scan needs the ts package (for the Node interface) and every package
	// that defines a helper an extractor calls; Visit walks the whole reachable
	// graph, which is a superset of both.
	var all []*packages.Package
	packages.Visit(pkgs, nil, func(p *packages.Package) { all = append(all, p) })

	s := newScanner(cfg.Fset, root, all)
	if !s.findNodeIface() {
		return nil, fmt.Errorf("could not locate the %s.Node interface in the loaded package set — the scan would report zero sites for the wrong reason", nodeTypeIfacePath)
	}
	return &Surface{Scan: s.Run(), Registrations: s.findRegistrations()}, nil
}

func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}

func readBaseline(path string) (*Baseline, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ParseBaseline(strings.NewReader(""))
		}
		return nil, err
	}
	defer f.Close()
	b, err := ParseBaseline(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return b, nil
}

func constStringOf(tv types.TypeAndValue) string {
	if tv.Value == nil {
		return ""
	}
	s, err := stringVal(tv)
	if err != nil {
		return ""
	}
	return s
}
