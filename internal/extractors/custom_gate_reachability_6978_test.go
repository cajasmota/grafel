package extractors_test

// #6978 — a path gate inside a custom extractor can be UNREACHABLE BY
// CONSTRUCTION, and when it is, it produces silence rather than an error.
//
// THE MECHANISM. RunCustomExtractors (custom_dispatch.go) is the ONLY
// dispatcher that ever selects a prefixed custom-extractor key: ordinary
// dispatch is an exact-key extractor.Get(file.Language) lookup
// (registry.go:95), which can never match "custom_csharp_blazor". And
// RunCustomExtractors selects purely on file.Language, via
// CustomExtractorsFor. So a file reaches the extractors of package P if and
// only if the classifier's language for that file maps — through
// customPrefixForLanguage / extraCustomPrefixesForLanguage — to a registry
// prefix one of P's keys carries.
//
// A gate that tests a file SUFFIX therefore selects nothing, forever, when the
// classifier's language for that suffix routes somewhere else (or nowhere).
// `strings.HasSuffix(file.Path, ".razor")` inside internal/custom/csharp is the
// canonical case: `.razor` classifies as language "razor", "razor" has no
// custom prefix, so no custom_csharp_* extractor can ever see such a file.
//
// The argument is CONSTRUCTIVE, not corpus-derived: it is a statement about
// language routing, so it holds for every repo and every dispatch path. It is
// deliberately independent of the `file.TSTree != nil` guard that the in-proc
// (cmd/grafel/index.go:4179) and incremental (incremental.go:1126) paths carry
// and the daemon subprocess path (subproc.go:364) does not — language routing
// alone already decides these sites, in all three paths.
//
// WHY A GUARD RATHER THAN A FIX. The sites below are NOT one defect. Some are
// unreachable code beside a live path (blazor_deep's `.razor` arm); some are a
// whole capability that has never run (rust's diesel/sqlx `.sql` migration
// parsing). Deleting them uniformly would throw away the second kind. This
// test freezes the population so a NEW one cannot be written in silence.

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/classifier"
	"github.com/cajasmota/grafel/internal/extractors"
)

// customGateRoot is the tree scanned. Relative to this package's directory,
// which is where `go test` runs.
const customGateRoot = "../custom"

// knownUnreachableGates is the frozen population of extension gates that
// cannot fire, as `<pkg>/<file>|<enclosing func>|<literal>`.
//
// IT IS A HAND-WRITTEN LITERAL, deliberately not derived from the scan it
// grades (#6975): a list built from the thing under test cannot detect an
// addition to that thing. Set equality is asserted in BOTH directions, so
// deleting or repairing a site requires deleting its line here too.
//
// Line numbers are omitted on purpose — they rot on every edit above the site,
// and a guard that has to be re-baselined for unrelated edits gets re-baselined
// without being read.
var knownUnreachableGates = []string{
	// `.xml` is classified as nothing at all, and `file.Language == "xml"`
	// beside it names a language the classifier never emits. The `isCPP` arm
	// of the same gate is live; only the XML alternative is dead.
	"cpp/ros_extractor.go|Extract|.xml",
	// `.Build.cs` classifies as "csharp", which routes to custom_csharp_ —
	// not to custom_cpp_, where this extractor lives. The `file.Language ==
	// "csharp"` disjunct beside it is dead for the same reason, so the whole
	// isCSharp alternative is unreachable; isCPP is live.
	"cpp/unreal_extractor.go|Extract|.Build.cs",
	// `.razor` classifies as "razor" (classifier.go), and "razor" has no
	// custom prefix. `.razor.cs` classifies as "csharp" and IS live, so both
	// extractors still run — over code-behind only. In blazor.go this means
	// the MARKUP rule set, which is further restricted to isRazorMarkup, has
	// never run on any file.
	"csharp/blazor.go|Extract|.razor",
	"csharp/blazor_deep.go|Extract|.razor",
	// SQLDelight `.sq`/`.sqm` are classified as nothing, so no FileInput with
	// that path ever exists. The kotlin-language arm of both gates is live.
	"kotlin/orm_query.go|Extract|.sq",
	"kotlin/orm_query.go|Extract|.sqm",
	"kotlin/orm_schema.go|isSQLDelightFile|.sq",
	"kotlin/orm_schema.go|isSQLDelightFile|.sqm",
	// `.moon` (MoonScript) is classified as nothing. `.conf` likewise, except
	// the single basename `nginx.conf`, which classifies as language "nginx" —
	// also without a custom prefix. The `strings.Contains(ext, "nginx")`
	// disjunct beside these is NOT dead: it fires for a `.lua` file under an
	// nginx/ directory.
	"lua/middleware.go|Extract|.moon",
	"lua/middleware.go|Extract|.conf",
	"lua/routing.go|Extract|.moon",
	"lua/routing.go|Extract|.conf",
	// `.sql` classifies as "sql", which customPrefixForLanguage routes to
	// custom_js_ ONLY. These three sites are whole branches, not disjuncts:
	// ormin's SQL-DSL half, diesel's migration parser and sqlx's migration
	// parser have never run outside their own unit tests.
	"nim/ormin_orm.go|Extract|.sql",
	"rust/diesel.go|Extract|.sql",
	"rust/sqlx_rbatis.go|isSqlxMigrationFile|.sql",
}

// liveGateControls are gates in the SAME files that the scan must find and
// must NOT report as dead. This is the positive control for the "read the
// wrong file / matched nothing" no-op: if the scan is pointed at a tree that
// parses fine but contains no gates, or its matcher stops recognising gate
// shapes, these disappear and the test fails on them rather than passing
// vacuously on an empty dead set.
var liveGateControls = []string{
	"csharp/blazor.go|Extract|.razor.cs",
	"csharp/blazor_deep.go|Extract|.razor.cs",
	"lua/middleware.go|Extract|.lua",
	"lua/routing.go|Extract|.lua",
}

// minGateLiterals / minFilesParsed are floors on the scan's raw work. They
// catch only the crudest no-op (read nothing), which is why the two set
// assertions above carry the real weight.
const (
	minGateLiterals = 25
	minFilesParsed  = 300
)

type gateSite struct {
	id   string // "<pkg>/<file>|<func>|<literal>"
	lit  string
	file string
	line int
}

// scanCustomGates walks internal/custom/** and returns every extension-shaped
// literal tested against a path-shaped expression, split into unreachable and
// reachable, plus the number of non-test files parsed.
func scanCustomGates(t *testing.T) (dead, live []gateSite, filesParsed int) {
	t.Helper()

	keysByDir := map[string][]string{}
	var all []gateSite

	err := filepath.Walk(customGateRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		filesParsed++
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", p, perr)
		}
		var funcs []*ast.FuncDecl
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				funcs = append(funcs, fd)
			}
		}
		enclosing := func(pos token.Pos) string {
			for _, fd := range funcs {
				if pos >= fd.Pos() && pos <= fd.End() {
					return fd.Name.Name
				}
			}
			return "<file-scope>"
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, customGateRoot+string(filepath.Separator)))
		dir := filepath.Dir(p)

		ast.Inspect(f, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok || len(ce.Args) == 0 {
				return true
			}
			se, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, _ := se.X.(*ast.Ident)
			if pkg == nil {
				return true
			}
			// extractor.Register("<key>", …) — the package's dispatch keys.
			if pkg.Name == "extractor" && se.Sel.Name == "Register" {
				if bl, ok := ce.Args[0].(*ast.BasicLit); ok && bl.Kind == token.STRING {
					if s, uerr := strconv.Unquote(bl.Value); uerr == nil {
						keysByDir[dir] = append(keysByDir[dir], s)
					}
				}
				return true
			}
			if pkg.Name != "strings" {
				return true
			}
			switch se.Sel.Name {
			case "HasSuffix", "EqualFold":
			default:
				return true
			}
			var subj strings.Builder
			if perr := printer.Fprint(&subj, fset, ce.Args[0]); perr != nil {
				t.Fatalf("print subject in %s: %v", p, perr)
			}
			if !pathShaped(subj.String()) {
				return true
			}
			for _, a := range ce.Args[1:] {
				bl, ok := a.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					continue
				}
				s, uerr := strconv.Unquote(bl.Value)
				if uerr != nil || !extensionShaped(s) {
					continue
				}
				all = append(all, gateSite{
					id:   rel + "|" + enclosing(bl.Pos()) + "|" + s,
					lit:  s,
					file: p,
					line: fset.Position(bl.Pos()).Line,
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", customGateRoot, err)
	}

	for _, s := range all {
		if gateIsReachable(s, keysByDir[filepath.Dir(s.file)]) {
			live = append(live, s)
		} else {
			dead = append(dead, s)
		}
	}
	return dead, live, filesParsed
}

// pathShaped reports whether a HasSuffix/EqualFold subject is plausibly a file
// path rather than file CONTENT. Gates read `file.Path`, a `path`/`rel`/`base`
// local, or a lowercased copy of one; content checks read `src`/`line`.
func pathShaped(subj string) bool {
	for _, m := range []string{"Path", "path", "rel", "Rel", "base", "Base", "file", "File", "name", "Name", "ext", "Ext"} {
		if strings.Contains(subj, m) {
			return true
		}
	}
	return false
}

// extensionShaped reports whether a literal looks like a file extension rather
// than a method-call or expression fragment (".route", ".Adapt<", ".cache.").
// Extensions are a leading dot plus letters/digits, optionally compound
// (".razor.cs", ".Build.cs").
func extensionShaped(s string) bool {
	if len(s) < 2 || s[0] != '.' {
		return false
	}
	for _, r := range s[1:] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.':
		default:
			return false
		}
	}
	return !strings.HasSuffix(s, ".")
}

// gateIsReachable reports whether any file matching the literal could reach an
// extractor registered in the same package. The classifier's language for the
// extension decides it; CustomExtractorsFor is the real dispatch predicate.
func gateIsReachable(s gateSite, dirKeys []string) bool {
	if len(dirKeys) == 0 {
		// A package registering nothing is out of this test's scope.
		return true
	}
	lang := classifier.LanguageForExtension(s.lit)
	if lang == "" {
		// Compound suffix: the classifier keys on the final extension, so
		// ".razor.cs" is decided by ".cs".
		if i := strings.LastIndex(s.lit, "."); i > 0 {
			lang = classifier.LanguageForExtension(s.lit[i:])
		}
	}
	if lang == "" {
		return false // classified as nothing: no FileInput ever carries it
	}
	reachable := map[string]bool{}
	for _, e := range extractors.CustomExtractorsFor(lang) {
		reachable[e.Language()] = true
	}
	for _, k := range dirKeys {
		if reachable[k] {
			return true
		}
	}
	return false
}

func ids(sites []gateSite) []string {
	out := make([]string, 0, len(sites))
	for _, s := range sites {
		out = append(out, s.id)
	}
	sort.Strings(out)
	return out
}

// TestCustomExtractorGatesAreReachable6978 fails when a NEW unreachable
// extension gate appears in internal/custom/**, and when a listed one is
// repaired or removed without updating the list.
func TestCustomExtractorGatesAreReachable6978(t *testing.T) {
	dead, live, filesParsed := scanCustomGates(t)

	if filesParsed < minFilesParsed {
		t.Fatalf("scan parsed %d non-test files under %s, want >= %d — the walk read (almost) nothing",
			filesParsed, customGateRoot, minFilesParsed)
	}
	if n := len(dead) + len(live); n < minGateLiterals {
		t.Fatalf("scan found %d extension gate literals, want >= %d — the matcher recognised (almost) nothing",
			n, minGateLiterals)
	}

	// Positive control: the scan must see, and correctly classify as LIVE,
	// gates that are known to fire. Without this a scan pointed at a tree with
	// no gates in it reports an empty dead set and passes.
	liveSet := map[string]bool{}
	for _, id := range ids(live) {
		liveSet[id] = true
	}
	for _, want := range liveGateControls {
		if !liveSet[want] {
			t.Errorf("live-gate control %q was not found as a REACHABLE gate — the scan is looking at the wrong files, or its matcher no longer recognises this gate shape", want)
		}
	}

	got := ids(dead)
	want := append([]string(nil), knownUnreachableGates...)
	sort.Strings(want)

	gotSet := map[string]bool{}
	for _, g := range got {
		gotSet[g] = true
	}
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w] = true
	}
	for _, g := range got {
		if !wantSet[g] {
			t.Errorf("NEW unreachable gate %q: no file matching this suffix can reach an extractor registered in that package, so the gate selects nothing, forever, in silence. Either route the language in customPrefixForLanguage (internal/extractors/custom_dispatch.go) or drop the gate — see #6978.", g)
		}
	}
	for _, w := range want {
		if !gotSet[w] {
			t.Errorf("listed unreachable gate %q is no longer detected as unreachable — if it was repaired or deleted, delete its line from knownUnreachableGates; if the SCAN stopped seeing it, that is the bug.", w)
		}
	}
}
