package registry_test

// home_sweep_guard_6178_test.go — the guard that stops the #6178 class
// re-entering.
//
// Three rounds of issue #6178 found the same bug at a new suffix each time:
// round 1 fixed internal/cli/links.go's two call sites and links.PathsFor's
// own "" fallback; round 2 found four more readers (internal/mcp,
// internal/dashboard, internal/docgen, internal/cli) that independently
// re-derived "<home>/groups/<group>-links.json" with a hand-rolled
// os.UserHomeDir() join instead of calling PathsFor; round 3 found the SAME
// shape on eleven more sidecars, because every downstream pass's own
// sidecar reader re-derived its path by hand too. Grep-and-patch does not
// end this — a twelfth sidecar next year re-opens it exactly the same way.
//
// This test scans every non-test .go file in the repository for a function
// whose body contains BOTH (a) a call to os.UserHomeDir() or
// os.Getenv("HOME"/"USERPROFILE"), AND (b) a string literal containing
// ".grafel" — the two ingredients of a hand-rolled "resolve the grafel
// home" join. Every hit must be declared in EXACTLY one of the two
// allow-lists below, each with a reason:
//
//   - deliberatelyAbsolute: the function has a real reason to bypass
//     GRAFEL_HOME (a different, documented override governs it instead —
//     GRAFEL_DAEMON_ROOT for daemon runtime paths; a machine-level
//     install-state cache; the canonical resolver itself), OR it already
//     resolves GRAFEL_HOME correctly via an independent, intentional
//     duplicate of registry.HomeDir()'s three lines.
//   - knownDeferred: a genuine instance of the #6178 bug class, real and
//     still broken, that is OUT OF SCOPE for this round — tracked
//     separately (the original #6178 report's wider-pattern sweep, or a
//     distinct issue). This list is a debt ledger: it must only ever
//     shrink as follow-ups land, never grow with a NEW offender — a new
//     hit that isn't a fix to an existing entry is exactly the defect
//     class this guard exists to catch before merge, not after a fourth
//     review round.
//
// # What this catches vs. what it catches
//
// This guard detects GRAFEL_HOME-*unawareness* — a function that resolves
// the OS home directly and never looks at GRAFEL_HOME. It does NOT detect
// derivation *duplication* — a function that resolves GRAFEL_HOME
// correctly (via registry.HomeDir(), links.PathsFor, or any of its
// siblings) and then re-derives the REST of the path by hand instead of
// calling the shared PassSidecarPath/MemoryDir/PatternsDir. That second
// shape has no os.UserHomeDir()/os.Getenv("HOME") call at all, so it is
// structurally invisible to an AST search keyed on those — see #6178
// round 4 finding L1 (internal/cli/patterns.go's resolvePatternsDir,
// fixed in that round): it called registry.HomeDir() correctly and then
// hand-joined "groups/<group>-patterns" itself, agreeing with
// links.PatternsDir today by coincidence, not by sharing code — a future
// change to PatternsDir's layout would have silently left it behind, and
// this guard would stay green throughout. Keeping "exactly one function
// per sidecar family" true is a property this test cannot enforce by
// itself; it takes review noticing a hand-rolled JOIN even when the HOME
// half is correct.
//
// # What this does NOT catch, within the shape it DOES target — read
// this before trusting it
//
// The scan is per-function-body: a resolver split across two functions in
// the same file (a `homeDir()` helper called by a sibling that builds the
// ".grafel" path) is invisible to it, because a single ast.Inspect over one
// FuncDecl never sees the callee's body. internal/install/state.go's
// userHomeDir()+DefaultStatePath() and internal/install/mcpreg/mcpreg.go's
// homeDir()+backupDir() are both exactly this shape — real, deliberate,
// $HOME-only install-state resolvers — and neither trips this guard. They
// are documented here, in prose, because the guard cannot see them: adding
// a NEW cross-function split of this kind is not something this test can
// detect. A scan of the tree at the time of writing found zero OTHER live
// instances of that shape, so this is a stated gap, not a known violation.
//
// Also uncaught: a ".grafel" substring in an unrelated string in the same
// function as an unrelated os.Getenv("HOME") call (a false-positive the
// allow-list absorbs case by case — see buildDaemonDiagnostics below for a
// worked example) can go the other way too: two co-located but unrelated
// hits that happen to both look legitimate would pass review without this
// test ever being able to tell they're unrelated. The allow-list reason is
// the human check; the guard only proves the shape recurs somewhere in that
// function, not that it's the SAME somewhere as declared.
//
// Five more evasions of the SAME "OS-home lookup + .grafel literal, same
// function" shape, each confirmed empirically against the live detector
// (findHandRolledHomePaths) by TestGuardDetectsTheShape below — every one
// of them returns 0 offenders on source where the bug shape is plainly
// present:
//
//   - A named constant referenced by identifier:
//     `const grafelDir = ".grafel"` used later as `filepath.Join(h,
//     grafelDir)`. The joining call site has an *ast.Ident, not an
//     *ast.BasicLit — the detector only resolves file-local string
//     consts when matching os.Getenv's ARGUMENT (see constStringValue in
//     guard_6018_test.go's sibling detector for that pattern), not when
//     matching the ".grafel" half of THIS detector.
//   - Literal concatenation: `filepath.Join(h, ".gra"+"fel", ...)`. Two
//     separate *ast.BasicLits, neither containing ".grafel" on its own —
//     Go the LANGUAGE constant-folds this at compile time; go/ast the
//     PARSER does not fold it before this detector ever sees the tree.
//   - An indirected env-var name: `os.Getenv(homeEnv)` where `homeEnv` is
//     a const equal to "HOME". The os.Getenv argument-type assertion to
//     *ast.BasicLit fails on an *ast.Ident, so hasHomeCall never becomes
//     true, regardless of what the identifier resolves to.
//   - A top-level closure: `var f = func() { ...os.UserHomeDir()...
//     ...".grafel"... }`. This is an *ast.GenDecl (a var spec whose value
//     happens to be a function literal), not an *ast.FuncDecl — the
//     per-file scan only inspects top-level FuncDecls, so it never
//     descends into a var-bound closure's body at all. (A closure
//     declared and called INSIDE an already-scanned FuncDecl's body IS
//     caught — ast.Inspect walks the whole subtree of a matched
//     FuncDecl's Body, closures included.)
//   - An aliased import: `import osx "os"` then `osx.UserHomeDir()`. The
//     detector checks `pkg.Name != "os"` on the SelectorExpr's package
//     identifier — an aliased import's identifier is the alias, not the
//     package's real name, so the comparison never matches.
//
// None of these is a reason to abandon the guard — it is a cost/benefit
// AST heuristic, not a type-checker, and a determined evader is not the
// threat model this exists for. The threat is the ACCIDENTAL twelfth
// sidecar, written the same unremarkable way as the first eleven, which
// is exactly the shape TestGuardDetectsTheShape's seven positive/negative
// cases already cover. Also uncaught by construction, not listed as a
// "finding" because they're outside what an os/os.Getenv-keyed detector
// could ever claim to cover: a path assembled from a struct field or a
// loaded config value, a third-party or vendored home-directory helper,
// and anything inside a _test.go file (excluded from the scan entirely —
// see scanHandRolledHomePaths).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// deliberatelyAbsolute lists functions that legitimately combine an
// OS-home lookup with a ".grafel" (or ".grafel_write_test" etc.) literal
// WITHOUT going through the shared GRAFEL_HOME-aware resolvers
// (registry.HomeDir / links.GroupHome / links.PathsFor / links.MemoryDir /
// links.PatternsDir). Each entry states which axis governs it instead, or
// why it's the resolver itself.
var deliberatelyAbsolute = map[string]string{
	// The canonical resolver. This IS "GRAFEL_HOME or ~/.grafel" — there is
	// nowhere further to route it.
	"internal/registry/registry.go:HomeDir": "the canonical resolver itself",

	// Daemon runtime layout (socket, pid, log, and the store when no
	// GRAFEL_DAEMON_ROOT override is in effect) is governed by
	// GRAFEL_DAEMON_ROOT, a documented, separate override from GRAFEL_HOME
	// — see internal/testsupport/guard_main.go's comment on the two axes.
	"internal/daemon/paths_unix.go:selectSocketPath":               "GRAFEL_DAEMON_ROOT axis (daemon socket path), not GRAFEL_HOME",
	"internal/daemon/paths_unix.go:DefaultLayout":                  "GRAFEL_DAEMON_ROOT axis (daemon runtime layout: socket/pid/log/store)",
	"internal/daemon/service/service_unix.go:resolvePlatformPaths": "GRAFEL_DAEMON_ROOT-adjacent axis (launchd service socket/log paths, resolved before any daemon env is guaranteed available)",
	"internal/daemon/state_path.go:homeDir":                        "deliberate lightweight duplicate of registry.HomeDir(), documented in-file as intentional (\"Kept dependency-light so this hot-path helper does not pull in the registry package\") — this hot path (FindGraphFileAnyRef → MCP group revive) cannot afford the registry package's import weight",

	// Machine-level install-state caches: what this MACHINE last installed,
	// not group-scoped state. Deliberately $HOME-only, same as
	// internal/install/state.go and internal/install/mcpreg/mcpreg.go
	// (both real but invisible to this guard — see the file doc's
	// "what this does NOT catch" section).
	"internal/install/mcptools/mcptools.go:LastChoicePath":           "machine-level install-state cache (what this machine last chose), not group data — same family as install.json",
	"internal/install/skilllink/skilllink.go:embeddedSkillsCacheDir": "explicit stated policy in-file: \"XDG intentionally not consulted... grafel state root is HOME/.grafel everywhere\"",

	// Independent, intentional duplicates of registry.HomeDir()'s three
	// lines that ALREADY resolve GRAFEL_HOME correctly (verified by
	// reading each implementation, not just by the guard's shape match).
	// Consolidating these into the shared resolver is desirable but is a
	// non-behavior-changing cleanup, not a #6178 defect — none of them is
	// broken today.
	"internal/embed/config.go:homeDir":                    "deliberate duplicate, already GRAFEL_HOME-aware; embeddings.json is per-user backend config, a different concern from group-scoped sidecars",
	"internal/mcp/docstate.go:defaultDocstateDir":         "deliberate duplicate, already checks GRAFEL_HOME first (see the function's own doc comment)",
	"internal/dashboard/handlers_paths.go:getDocFilePath": "deliberate duplicate, already checks GRAFEL_HOME first (see the function's own doc comment, which cites this exact convention)",
	"internal/docgen/cleanup.go:resolveHomeDir":           "deliberate duplicate, already checks GRAFEL_HOME first (see the function's own doc comment: \"same convention as registry.HomeDir\")",

	// This file's own migration-warning helper: RealHomeOrphanWarning
	// MUST build the pre-#6178 legacy path independently of GRAFEL_HOME —
	// that comparison is its entire purpose (see internal/links/
	// group_paths.go's doc comment on RealHomeOrphanWarning).
	"internal/links/group_paths.go:RealHomeOrphanWarning": "deliberately builds the LEGACY (pre-#6178, real-home) path for comparison — that comparison is the function's entire purpose",

	// False positive of the co-occurrence heuristic: this function's
	// ".grafel"-containing literal (".grafel_write_test") pairs with a
	// `homeDir` LOCAL VARIABLE that already comes from registry.HomeDir()
	// two lines up (correct). The unrelated os.Getenv("HOME") call in the
	// same function builds a macOS LaunchAgents path, which is deliberately
	// the REAL OS home — launchd is an OS-level per-user resource, not
	// grafel state, so GRAFEL_HOME must NOT apply to it.
	"internal/dashboard/handlers_diagnostics.go:buildDaemonDiagnostics": "false positive: the .grafel literal pairs with registry.HomeDir()-derived homeDir (correct); the unrelated os.Getenv(\"HOME\") builds a LaunchAgents path, deliberately the real OS home",
}

// knownDeferred is a debt ledger of REAL, STILL-BROKEN instances of the
// #6178 bug class that are explicitly out of scope for this round. Every
// entry must reference where it's tracked. This list must only shrink.
var knownDeferred = map[string]string{
	"internal/audit/log.go:DefaultLogPath":                  "RELOCATABLE — audit log path ignores GRAFEL_HOME. Reported in #6178's original wider-pattern sweep as out-of-issue scope for the maintainer to file separately.",
	"internal/quality/history.go:historyFilename":           "RELOCATABLE — quality-history sidecar's \"\" fallback ignores GRAFEL_HOME. Reported in #6178's original wider-pattern sweep as out-of-issue scope.",
	"internal/cli/feedback_rollup.go:runFeedbackRollup":     "RELOCATABLE — feedback rollup output dir keys off $HOME only, never GRAFEL_HOME. Same family as feedback_timeline.go; reported in the wider-pattern sweep.",
	"internal/cli/feedback_timeline.go:runFeedbackTimeline": "RELOCATABLE — feedback timeline events/out dirs key off $HOME only, never GRAFEL_HOME. Reported in the wider-pattern sweep.",
	"internal/mcp/activity_log.go:DefaultActivityLogPath":   "UNCLEAR in the wider-pattern sweep: may be deliberate (user-level telemetry, not group data) or should follow GRAFEL_HOME like the sidecar family — needs a maintainer decision, not a mechanical fix.",
	"internal/mcp/persona_telemetry.go:personaEventsDir":    "UNCLEAR in the wider-pattern sweep, same open question as activity_log.go's DefaultActivityLogPath.",
	"internal/mcp/docgen.go:canonicalDocsPath":              "LIVE SPLIT-BRAIN TODAY, not merely latent — no GRAFEL_HOME check at all (os.Getenv(\"HOME\") then os.UserHomeDir(), full stop) while every other docs writer IS GRAFEL_HOME-aware (internal/daemon/docs_path.go, internal/docgen/tier0.go through tier4.go, internal/cli/docgen.go's promote path) — so an isolated GRAFEL_HOME run promotes docs today at the wrong location relative to everything else that reads/writes them. Feeds handleDocgenPromote's ~/.grafel/docs path — the same feature area as issue #6075 (\"related and not duplicate\" per #6178's own text), so left for that issue rather than folded into this one; flagged here as live, not latent, so whoever reads this ledger prioritises it correctly.",
	"internal/mcp/server.go:NewServer":                      "RELOCATABLE, low severity — the metrics dir (~/.grafel/metrics/) is a best-effort diagnostic path, plain os.UserHomeDir() only. Reported in the wider-pattern sweep as low severity.",
}

func TestNoHandRolledGrafelHomePaths(t *testing.T) {
	root := repoRoot(t)
	offenders := scanHandRolledHomePaths(t, root)

	seen := map[string]bool{}
	var unexpected []string
	for _, o := range offenders {
		key := o.file + ":" + o.fn
		seen[key] = true
		_, deliberate := deliberatelyAbsolute[key]
		_, deferred := knownDeferred[key]
		if !deliberate && !deferred {
			unexpected = append(unexpected, o.String())
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Errorf("hand-rolled GRAFEL_HOME-unaware path(s) (#6178) not on either allow-list:\n  %s\n\n"+
			"A function combining os.UserHomeDir()/os.Getenv(\"HOME\"|\"USERPROFILE\") with a "+
			"\".grafel\"-containing string literal must resolve the grafel home via "+
			"registry.HomeDir() (or links.GroupHome/PathsFor/PassSidecarPath/MemoryDir/PatternsDir, "+
			"which all call it) instead. If this is genuinely deliberate (a different override "+
			"axis, or an already-correct independent duplicate), add it to deliberatelyAbsolute "+
			"with a one-line reason. Do NOT add a new, still-broken instance to knownDeferred — "+
			"that list only shrinks; fix it or get an explicit maintainer decision to defer it.",
			strings.Join(unexpected, "\n  "))
	}

	// Both allow-lists are debt ledgers / declarations, not wishlists: an
	// entry that no longer matches the live scan means either the function
	// was fixed (knownDeferred should shrink) or renamed/removed
	// (deliberatelyAbsolute should be updated) — either way, leaving a
	// stale entry hides a change from the next reader of this file.
	var stale []string
	for key := range deliberatelyAbsolute {
		if !seen[key] {
			stale = append(stale, "deliberatelyAbsolute: "+key)
		}
	}
	for key := range knownDeferred {
		if !seen[key] {
			stale = append(stale, "knownDeferred: "+key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("allow-list entries that no longer match the live scan (fixed, renamed, or removed — update or delete):\n  %s",
			strings.Join(stale, "\n  "))
	}
}

// TestGuardDetectsTheShape is the guard's own guard: runs the same detector
// over synthetic source so a refactor that silently stops binding is caught
// even though the scan above would be vacuously green in that failure mode.
func TestGuardDetectsTheShape(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "UserHomeDir plus .grafel literal",
			src: `package p
import "os"
import "path/filepath"
func f() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".grafel", "groups")
}`,
			want: 1,
		},
		{
			name: "Getenv HOME plus .grafel literal",
			src: `package p
import "os"
func f() string {
	h := os.Getenv("HOME")
	return h + "/.grafel/groups"
}`,
			want: 1,
		},
		{
			name: "Getenv USERPROFILE plus .grafel literal",
			src: `package p
import "os"
func f() string {
	h := os.Getenv("USERPROFILE")
	return h + "/.grafel/groups"
}`,
			want: 1,
		},
		{
			name: "UserHomeDir without any .grafel literal is clean",
			src: `package p
import "os"
func f() (string, error) { return os.UserHomeDir() }`,
			want: 0,
		},
		{
			name: ".grafel literal without a home lookup is clean",
			src: `package p
func f(base string) string { return base + "/.grafel/groups" }`,
			want: 0,
		},
		{
			name: "Getenv of an unrelated var is clean",
			src: `package p
import "os"
func f() string { return os.Getenv("GRAFEL_HOME") + "/.grafel/groups" }`,
			want: 0,
		},
		{
			name: "two offending functions in one file",
			src: `package p
import "os"
func f() string { h, _ := os.UserHomeDir(); return h + "/.grafel/a" }
func g() string { h, _ := os.UserHomeDir(); return h + "/.grafel/b" }`,
			want: 2,
		},

		// --- CONFIRMED EVASIONS (documented in the file doc's "Five more
		// evasions" section) — asserted at want:0 so this file states its
		// real reach rather than implying total coverage. If a future
		// change to the detector starts catching one of these, flip its
		// want to 1 (or the offending count) and delete the corresponding
		// bullet from the file doc — do not delete the row.
		{
			name: "MISS: .grafel behind a named constant",
			src: `package p
import "os"
import "path/filepath"
const grafelDir = ".grafel"
func f() string { h, _ := os.UserHomeDir(); return filepath.Join(h, grafelDir) }`,
			want: 0,
		},
		{
			name: "MISS: .grafel built from concatenated literals",
			src: `package p
import "os"
import "path/filepath"
func f() string { h, _ := os.UserHomeDir(); return filepath.Join(h, ".gra"+"fel") }`,
			want: 0,
		},
		{
			name: "MISS: Getenv key behind an identifier, not a literal",
			src: `package p
import "os"
const homeEnv = "HOME"
func f() string { return os.Getenv(homeEnv) + "/.grafel/groups" }`,
			want: 0,
		},
		{
			name: "MISS: var-bound top-level closure, not a FuncDecl",
			src: `package p
import "os"
var f = func() string { h, _ := os.UserHomeDir(); return h + "/.grafel/groups" }`,
			want: 0,
		},
		{
			name: "MISS: aliased os import",
			src: `package p
import osx "os"
func f() string { h, _ := osx.UserHomeDir(); return h + "/.grafel/groups" }`,
			want: 0,
		},

		// Closures declared AND invoked inside an already-scanned
		// FuncDecl's body ARE caught — ast.Inspect walks the whole
		// FuncDecl subtree, closures included. This is the positive
		// counterpart to the "var-bound top-level closure" miss above:
		// the miss is specifically about a closure that IS the top-level
		// declaration, not any closure anywhere.
		{
			name: "caught: closure INSIDE a scanned FuncDecl's body",
			src: `package p
import "os"
func f() string {
	inner := func() string {
		h, _ := os.UserHomeDir()
		return h + "/.grafel/nested"
	}
	return inner()
}`,
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "x.go", tc.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := len(findHandRolledHomePaths(fset, f, "x.go"))
			if got != tc.want {
				t.Fatalf("detector found %d offenders, want %d", got, tc.want)
			}
		})
	}
}

type homePathOffence struct {
	file string // repo-relative
	fn   string
	line int
}

func (o homePathOffence) String() string {
	return o.file + ":" + strconv.Itoa(o.line) + " " + o.fn
}

// findHandRolledHomePaths reports every top-level func/method in f whose
// body contains both an OS-home lookup and a ".grafel"-containing string
// literal.
func findHandRolledHomePaths(fset *token.FileSet, f *ast.File, rel string) []homePathOffence {
	var out []homePathOffence
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		hasHomeCall := false
		hasGrafelLit := false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.CallExpr:
				sel, ok := e.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "os" {
					return true
				}
				if sel.Sel.Name == "UserHomeDir" {
					hasHomeCall = true
					return true
				}
				if sel.Sel.Name == "Getenv" && len(e.Args) == 1 {
					if lit, ok := e.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, uerr := strconv.Unquote(lit.Value); uerr == nil && (s == "HOME" || s == "USERPROFILE") {
							hasHomeCall = true
						}
					}
				}
			case *ast.BasicLit:
				if e.Kind == token.STRING {
					if s, uerr := strconv.Unquote(e.Value); uerr == nil && strings.Contains(s, ".grafel") {
						hasGrafelLit = true
					}
				}
			}
			return true
		})
		if hasHomeCall && hasGrafelLit {
			out = append(out, homePathOffence{file: rel, fn: fd.Name.Name, line: fset.Position(fd.Pos()).Line})
		}
	}
	return out
}

func scanHandRolledHomePaths(t *testing.T, root string) []homePathOffence {
	t.Helper()
	var out []homePathOffence
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("parse %s: %v", rel, perr)
		}
		out = append(out, findHandRolledHomePaths(fset, f, rel)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(out) == 0 {
		t.Fatalf("scan of %s found no hand-rolled home-path shape at all — the walk is not "+
			"binding the repository (expected at least the allow-listed entries)", root)
	}
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// here = <root>/internal/registry/home_sweep_guard_6178_test.go
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
}
