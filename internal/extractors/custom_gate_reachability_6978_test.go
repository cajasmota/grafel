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
// parsing, php's Symfony `services.yaml` DI graph, the whole Vue/Svelte/Nuxt
// family). Deleting them uniformly would throw away the second kind. This test
// freezes the population so a NEW one cannot be written in silence.
//
// POLARITY: FAIL-CLOSED. The scan applies NO filter to the gate's subject
// expression. Every extension-shaped literal compared against anything is a
// candidate, and every candidate the reachability predicate calls unreachable
// must appear in one of the two hand-written lists below — as a real gate, or
// as a literal that is matched against file CONTENT rather than a path. An
// unreachable candidate in neither list FAILS. The first version of this test
// filtered candidates by a marker list of subject NAMES ("path", "rel", "ext",
// …) and silently dropped every gate whose subject was called `low`, `lower`
// or `fp` — that is the hand-picked-enumeration failure this derivation exists
// to avoid, so the filter is gone rather than extended.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
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

// registryImportPath is the package whose Register() calls name a package's
// dispatch keys. The scan resolves the LOCAL name of this import per file
// rather than matching the identifier `extractor`: internal/custom/javascript
// (34 registrations) and internal/custom/java (7) import it as `extreg`, and an
// identifier-matching scan silently found zero keys for both — which the
// empty-key branch then read as "out of scope", exempting two whole packages.
const registryImportPath = "github.com/cajasmota/grafel/internal/extractor"

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
	// extractors still run — over code-behind only.
	"csharp/blazor.go|Extract|.razor",
	"csharp/blazor_deep.go|Extract|.razor",
	// Vue SFCs classify as language "vue", which has no custom prefix, so no
	// custom_js_* extractor can see one. All four are DISJUNCTS, not whole
	// gates — checked at each site rather than assumed: vue.go and svelte.go
	// both read `lang != "typescript" && lang != "javascript" && !vueFile`, so
	// the ts/js arm keeps each extractor live; nuxt's `.server.vue`/
	// `.client.vue` sit beside live `.server.ts`/`.client.ts`/`.js` arms, and
	// its `.vue` feeds isPagesFile only, beside a live isServerAPI path. What
	// is unreachable is SFC-specific extraction (the `<template>` half), not
	// the extractors.
	"javascript/vue.go|isVueFile|.vue",
	"javascript/nuxt.go|Extract|.vue",
	"javascript/nuxt.go|Extract|.server.vue",
	"javascript/nuxt.go|Extract|.client.vue",
	// `.svelte` classifies as "svelte", also without a custom prefix. Same
	// disjunct shape as vue.go.
	"javascript/svelte.go|Extract|.svelte",
	// The java pattern adapter (patterns_dispatch.go) registers
	// `custom_java_patterns`, so only `.java` files reach these functions —
	// and the adapter stamps ctx.Language "java" literally, so the
	// `ctx.Language != "java"` guard rejects anything else a second time.
	// `.routes` classifies as "scala" (routing to custom_scala_) and `.xml` as
	// nothing. Both are disjuncts: each falls through to a live source-marker
	// path when false, so the extractors still work on `.java`.
	"java/play_routes.go|isPlayRoutesFile|.routes",
	"java/struts_routes.go|ExtractStruts|.xml",
	"java/struts_routes.go|extractStrutsRequestValidation|.xml",
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
	// `.yaml`/`.yml` classify as "yaml", which has no custom prefix. di_graph
	// has NO language guard, so routing alone decides it: the whole Symfony
	// `services.yaml` DI alias→implementation branch is latent capability,
	// exercised only by its own unit test. Subject is `lower`, which is why
	// the first version of this scan could not see it.
	"php/di_graph.go|Extract|.yaml",
	"php/di_graph.go|Extract|.yml",
	// Behat `.feature` files classify as nothing. Written as a slice compare
	// (`file.Path[len(file.Path)-8:] == ".feature"`) rather than a HasSuffix
	// call, which is why the scan also inspects equality comparisons.
	"php/test_data.go|Extract|.feature",
}

// knownContentMatchLiterals are extension-shaped literals that are NOT file
// gates: they are matched against file CONTENT (a source line, an identifier
// fragment, a config value), so "unreachable as an extension" says nothing
// about them. Same id shape, same hand-written discipline, same both-direction
// set equality — an entry here is an explicit claim that the site is not a
// path gate, and a reviewer can check it in one line.
//
// This list exists because the scan deliberately applies no subject filter: a
// filter that dropped these would also drop real gates, as the first version
// of this test did. Adjudicating them by hand is the price of fail-closed.
var knownContentMatchLiterals = []string{
	// Both are matched against `receiver` — an identifier parsed out of the
	// SOURCE (`step.run(...)`, `inngest.createFunction(...)`) — not against a
	// path. Extension-shaped by coincidence.
	"javascript/inngest.go|Extract|.inngest",
	"javascript/inngest.go|inngestStepReceiverAttributed|.step",
}

// liveGateControls are gates that the scan must find and must NOT report as
// dead. This is the positive control for the "read the wrong file / matched
// nothing" no-op: if the scan is pointed at a tree that parses fine but
// contains no gates, or its matcher stops recognising gate shapes, these
// disappear and the test fails on them rather than passing vacuously on an
// empty dead set.
var liveGateControls = []string{
	"csharp/blazor.go|Extract|.razor.cs",
	"csharp/blazor_deep.go|Extract|.razor.cs",
	"lua/middleware.go|Extract|.lua",
	"lua/routing.go|Extract|.lua",
}

// minGateLiterals / minFilesParsed are floors on the scan's raw work. They
// catch only the crudest no-op (read nothing), which is why the set assertions
// above carry the real weight.
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
// literal compared against something, split into unreachable and reachable,
// plus the number of non-test files parsed and the packages whose Register
// keys came back empty.
func scanCustomGates(t *testing.T) (dead, live []gateSite, filesParsed int, keylessDirs []string) {
	t.Helper()

	keysByDir := map[string][]string{}
	dirsSeen := map[string]bool{}
	var all []gateSite

	err := filepath.Walk(customGateRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// testdata/ holds Go SOURCE FIXTURES, not packages: they register
			// nothing, and judging them would make every scan report a keyless
			// package. `go build` ignores them for the same reason.
			if info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
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
		// SLASH FIRST, THEN STRIP. filepath.Walk yields OS-separator paths, so
		// on Windows `p` is `..\custom\csharp\blazor.go` while
		// customGateRoot is "../custom" — stripping a forward-slash prefix off
		// a backslash path is a silent NO-OP, every key keeps a `../custom/`
		// prefix the hand-written lists do not have, and BOTH directions of
		// the set comparison fail at once. That is exactly how this test went
		// red on the default windows-latest leg while green on macOS and
		// Linux. The in-tree convention (internal/resolve normalizePath) is
		// that paths are slash-form inside the program and only converted back
		// at the OS-disk boundary; keyFor asserts it rather than trusting it.
		rel := keyFor(t, p)
		dir := filepath.Dir(p)
		dirsSeen[dir] = true
		regName := localImportName(f, registryImportPath)
		strName := localImportName(f, "strings")

		// String constants/variables bound to a literal ANYWHERE in this file,
		// so `const razorSuffix = ".razor"` + HasSuffix(fp, razorSuffix) is
		// seen. Resolution is single-file and single-assignment: an ident bound
		// more than once, or bound to anything but a literal, is dropped rather
		// than guessed at.
		bindings := map[string]*ast.BasicLit{}
		bound := map[string]int{}
		ast.Inspect(f, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.ValueSpec:
				for i, nm := range d.Names {
					bound[nm.Name]++
					if i < len(d.Values) {
						if bl, ok := d.Values[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
							bindings[nm.Name] = bl
						}
					}
				}
			case *ast.AssignStmt:
				if d.Tok != token.DEFINE && d.Tok != token.ASSIGN {
					return true
				}
				for i, lhs := range d.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok {
						continue
					}
					bound[id.Name]++
					if i < len(d.Rhs) {
						if bl, ok := d.Rhs[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
							bindings[id.Name] = bl
						}
					}
				}
			}
			return true
		})
		// resolve returns the literal an expression denotes: itself when it is
		// one, or its unique single-literal binding when it is an identifier.
		resolve := func(e ast.Expr) *ast.BasicLit {
			switch v := e.(type) {
			case *ast.BasicLit:
				return v
			case *ast.Ident:
				if bound[v.Name] == 1 {
					return bindings[v.Name]
				}
			}
			return nil
		}

		record := func(bl *ast.BasicLit) {
			if bl.Kind != token.STRING {
				return
			}
			s, uerr := strconv.Unquote(bl.Value)
			if uerr != nil || !extensionShaped(s) {
				return
			}
			all = append(all, gateSite{
				id:   rel + "|" + enclosing(bl.Pos()) + "|" + s,
				lit:  s,
				file: p,
				line: fset.Position(bl.Pos()).Line,
			})
		}

		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.CallExpr:
				se, ok := v.Fun.(*ast.SelectorExpr)
				if !ok || len(v.Args) == 0 {
					return true
				}
				pkg, _ := se.X.(*ast.Ident)
				if pkg == nil {
					return true
				}
				// <registry>.Register("<key>", …) — the package's dispatch keys.
				if regName != "" && pkg.Name == regName && se.Sel.Name == "Register" {
					if bl, ok := v.Args[0].(*ast.BasicLit); ok && bl.Kind == token.STRING {
						if s, uerr := strconv.Unquote(bl.Value); uerr == nil {
							keysByDir[dir] = append(keysByDir[dir], s)
						}
					}
					return true
				}
				if strName == "" || pkg.Name != strName {
					return true
				}
				switch se.Sel.Name {
				case "HasSuffix", "EqualFold":
					for _, a := range v.Args {
						if bl := resolve(a); bl != nil {
							record(bl)
						}
					}
				}
			case *ast.BinaryExpr:
				// `file.Path[len(file.Path)-8:] == ".feature"` and friends.
				if v.Op != token.EQL && v.Op != token.NEQ {
					return true
				}
				lb, rb := resolve(v.X), resolve(v.Y)
				lok, rok := lb != nil, rb != nil
				if lok && !rok {
					record(lb)
				}
				if rok && !lok {
					record(rb)
				}
			case *ast.CaseClause:
				for _, e := range v.List {
					if bl := resolve(e); bl != nil {
						record(bl)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", customGateRoot, err)
	}

	for d := range dirsSeen {
		if len(keysByDir[d]) == 0 {
			keylessDirs = append(keylessDirs, filepath.ToSlash(d))
		}
	}
	sort.Strings(keylessDirs)

	for _, s := range all {
		if gateIsReachable(s, keysByDir[filepath.Dir(s.file)]) {
			live = append(live, s)
		} else {
			dead = append(dead, s)
		}
	}
	return dead, live, filesParsed, keylessDirs
}

// gateKey converts a walked path into the slash-form, root-relative key the
// hand-written lists are written in. It is a pure function so the Windows
// breakage can be pinned FROM ANY PLATFORM (see
// TestGateKeyNormalisesWindowsPaths6978) — filepath.ToSlash is a no-op on
// Unix, so a test built on it could not have caught this from the macOS or
// Linux legs, which is precisely how the bug reached CI.
//
// The backslash replacement is unconditional rather than OS-dependent: a
// backslash inside internal/custom/** would be a path separator on the only
// platform that produces one, and no file in that tree has one in its name.
func gateKey(walked string) (key string, problem string) {
	slashed := strings.ReplaceAll(walked, "\\", "/")
	prefix := strings.ReplaceAll(customGateRoot, "\\", "/") + "/"
	key = strings.TrimPrefix(slashed, prefix)
	switch {
	case strings.ContainsRune(key, '\\'):
		return key, "contains a backslash — it was not converted to slash form, so it can never match the hand-written lists"
	case key == slashed:
		return key, "still carries the " + customGateRoot + " prefix — the strip was a no-op"
	}
	return key, ""
}

// keyFor is gateKey with the failure wired to the test. Asserting the
// normalisation rather than only the outcome is the point: a key that merely
// happens to line up on the platform the author ran is one refactor away from
// silently reverting.
func keyFor(t *testing.T, walked string) string {
	t.Helper()
	key, problem := gateKey(walked)
	if problem != "" {
		t.Fatalf("key %q (from %q) %s", key, walked, problem)
	}
	return key
}

// TestGateKeyNormalisesWindowsPaths6978 pins the normalisation itself. The
// windows-latest leg reported EVERY gate as new-unreachable and ALL FOUR
// live-gate controls as not-found, because filepath.Walk yields
// `..\custom\csharp\blazor.go` there and the prefix strip was written in
// forward slashes — so it silently did nothing and both directions of the set
// comparison failed at once.
func TestGateKeyNormalisesWindowsPaths6978(t *testing.T) {
	for _, tc := range []struct{ walked, want string }{
		{"../custom/csharp/blazor.go", "csharp/blazor.go"},
		{"..\\custom\\csharp\\blazor.go", "csharp/blazor.go"},
		{"..\\custom\\javascript\\nuxt.go", "javascript/nuxt.go"},
	} {
		got, problem := gateKey(tc.walked)
		if problem != "" {
			t.Errorf("gateKey(%q) rejected its own input: %s", tc.walked, problem)
			continue
		}
		if got != tc.want {
			t.Errorf("gateKey(%q) = %q, want %q — a key that keeps an OS separator or a root prefix matches nothing in either direction", tc.walked, got, tc.want)
		}
	}
	// A path outside the root must be REPORTED, not silently passed through.
	if _, problem := gateKey("../engine/foo.go"); problem == "" {
		t.Error("gateKey accepted a path outside customGateRoot without reporting the no-op strip")
	}
}

// localImportName returns the identifier a file refers to importPath by, or ""
// when the file does not import it. Handles an explicit alias (`extreg "…"`),
// the implicit package name, and a dot import (reported as "" — no selector
// exists to match, and no file in this tree uses one).
func localImportName(f *ast.File, importPath string) string {
	for _, im := range f.Imports {
		p, err := strconv.Unquote(im.Path.Value)
		if err != nil || p != importPath {
			continue
		}
		if im.Name != nil {
			if im.Name.Name == "." || im.Name.Name == "_" {
				return ""
			}
			return im.Name.Name
		}
		// No alias: the local name is the package's own name, which for every
		// import in this tree is the final path segment.
		return path.Base(p)
	}
	return ""
}

// extensionShaped reports whether a literal looks like a file extension rather
// than an expression fragment (".Adapt<", ".cache.", ".Handle("). Extensions
// are a leading dot plus letters/digits, optionally compound (".razor.cs",
// ".server.vue"). Method-name fragments like ".route" pass this test too —
// they are adjudicated by hand into knownContentMatchLiterals rather than
// filtered out by a heuristic on the subject expression.
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
		// Reported separately as a scanner failure, not silently excused.
		return false
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
	seen := map[string]bool{}
	for _, s := range sites {
		if seen[s.id] {
			continue
		}
		seen[s.id] = true
		out = append(out, s.id)
	}
	sort.Strings(out)
	return out
}

// TestCustomExtractorGatesAreReachable6978 fails when a NEW unreachable
// extension gate appears in internal/custom/**, and when a listed one is
// repaired or removed without updating the list.
//
// WHAT IT CANNOT SEE — stated because a guard presented as complete is a trap,
// and each of these was scored ALIVE rather than assumed. The scan is
// syntactic. It reaches a string literal in a strings.HasSuffix/EqualFold
// call, an equality comparison, or a switch case, and it resolves an
// identifier bound exactly once to a literal in the SAME FILE. It does NOT
// see:
//
//   - a suffix built by concatenation (`"." + ext`) or returned by a function;
//   - an identifier bound in another file of the package, or imported;
//   - an identifier assigned more than once (deliberately dropped, not
//     guessed);
//   - `strings.Contains`, `HasPrefix`, a regexp, or a path-SEGMENT test — the
//     lua `strings.Contains(ext, "nginx")` disjunct is exactly this shape, and
//     it happens to be live;
//   - a gate in a helper package outside internal/custom/**.
//
// Those remain writable in silence. What this test does guarantee is that the
// shapes which produced all 26 known instances cannot grow a 27th without a
// failure, and that no package is exempt from being asked.
func TestCustomExtractorGatesAreReachable6978(t *testing.T) {
	dead, live, filesParsed, keylessDirs := scanCustomGates(t)

	if filesParsed < minFilesParsed {
		t.Fatalf("scan parsed %d non-test files under %s, want >= %d — the walk read (almost) nothing",
			filesParsed, customGateRoot, minFilesParsed)
	}
	if n := len(dead) + len(live); n < minGateLiterals {
		t.Fatalf("scan found %d extension literals, want >= %d — the matcher recognised (almost) nothing",
			n, minGateLiterals)
	}
	// Every internal/custom/<lang> package registers at least one extractor.
	// A package that yields zero keys is a SCANNER bug (a registry import the
	// alias resolution missed), never a real state of the tree — and left
	// unreported it exempts that package from the whole test.
	for _, d := range keylessDirs {
		t.Errorf("package %s yielded zero extractor.Register keys — the scan cannot judge reachability there, so the package is silently exempt. This is a scanner bug (an unresolved registry import alias?), not a property of the tree.", d)
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
	adjudicated := map[string]string{}
	for _, w := range knownUnreachableGates {
		adjudicated[w] = "gate"
	}
	for _, w := range knownContentMatchLiterals {
		if adjudicated[w] != "" {
			t.Errorf("%q is listed BOTH as an unreachable gate and as a content match — it can only be one", w)
		}
		adjudicated[w] = "content"
	}

	gotSet := map[string]bool{}
	for _, g := range got {
		gotSet[g] = true
		if adjudicated[g] == "" {
			t.Errorf("UNADJUDICATED unreachable literal %q: no file matching this suffix can reach an extractor registered in that package. If it is a file gate, it selects nothing, forever, in silence — route the language in customPrefixForLanguage (internal/extractors/custom_dispatch.go), or drop the gate, or list it in knownUnreachableGates. If it is matched against file CONTENT rather than a path, list it in knownContentMatchLiterals. See #6978.", g)
		}
	}
	for w, kind := range adjudicated {
		if !gotSet[w] {
			t.Errorf("listed %s %q is no longer detected as unreachable — if it was repaired or deleted, delete its line from the list; if the SCAN stopped seeing it, that is the bug.", kind, w)
		}
	}
}
