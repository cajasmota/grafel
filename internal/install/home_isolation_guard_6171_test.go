package install_test

// home_isolation_guard_6171_test.go — makes "a test in this package forgot to
// isolate $HOME" name itself, instead of silently deleting the rest of the run.
//
// # The failure shape this exists for
//
// internal/registry.saveTo calls guardResolvedConfigPath, which PANICS when the
// path it is about to write lands inside the real user home (see
// internal/registry/test_isolation_guard.go). A panic aborts the whole test
// binary, so on #6171 `go test ./internal/install/` under a sandbox HOME ran
// exactly ONE of the package's 286 tests and reported nothing — not pass, not
// fail — for the other 285. The output named one failing test; the truth was a
// package-wide blackout.
//
// The panic itself is not the defect and is not fixable here.
// guardResolvedConfigPath takes no *testing.T, and its callers do not have one
// either: registry.Save resolves RegistryPath and hands it to saveTo, both
// ordinary production functions. Rewriting the panic as t.Fatalf does not
// compile — `undefined: t`. Even with a t threaded through, testing.T.FailNow
// (which Fatalf calls) is documented as having to be called from the goroutine
// running the test, so it would not stop a write on any other goroutine.
// Fail-closed with a panic is the only mechanism that path has. What IS fixable
// is that the diagnosis arrives too late to be trusted: whatever the panic
// truncated is invisible.
//
// # Why this runs from TestMain and not as an ordinary Test
//
// The test binary runs tests in the order it lists them, which follows source
// order across files sorted by name. Measured on the broken tree: with no -run
// filter, the first and only test to execute was
// TestApply_RetiresLegacyWatcherUnit, from apply_migrate_6183_darwin_test.go —
// alphabetically the first test file in this directory, and one of the two
// #6171 offenders. An ordinary Test in a file named after this issue would
// therefore be truncated by the very panic it exists to explain — a gate that
// cannot fail in the case it was written for.
//
// Running from TestMain (see copy_test.go) makes the report the FIRST thing the
// run emits, before any test starts, so no later panic can suppress it. It does
// NOT abort: m.Run() still executes, because turning one missing IsolateHome
// call into a refusal to run the package would be a second blackout with better
// manners. The offenders are named, the suite proceeds, and the package exit
// code is non-zero whatever m.Run() returns.
//
// # The rule
//
// registry.HomeDir (internal/registry/registry.go) returns $GRAFEL_HOME when it
// is set and falls back to <os home>/.grafel otherwise. A test that redirects
// HOME but leaves GRAFEL_HOME alone is therefore NOT isolated whenever the
// developer or CI exports GRAFEL_HOME — which is precisely the mandated way to
// run this suite, since the alternative is running it against a live ~/.grafel.
// The inherited GRAFEL_HOME wins over the test's own HOME, the write lands
// outside the TempDir, and the guard fires.
//
// So: a function that calls .Setenv("HOME", …) must also either call
// testsupport.IsolateHome (which sets HOME, USERPROFILE, XDG_CONFIG_HOME,
// XDG_RUNTIME_DIR, GRAFEL_DAEMON_ROOT and GRAFEL_HOME together — see
// internal/testsupport/isolate.go) or set GRAFEL_HOME itself.
//
// # Reach, stated rather than implied
//
// go/parser applies no build constraints, so this scan reads
// //go:build darwin files on every platform. #6171 lived in a darwin-only file
// and Linux CI could never have surfaced it by running the test; this guard
// surfaces it by reading it. TestHomeIsolationGuardDetectsTheShape below pins
// what the detector does and does not see.
//
// # The blind spot — read this before trusting a green result
//
// This detector recognises exactly ONE shape: a function that sets HOME
// literally and pins neither GRAFEL_HOME nor IsolateHome. It is keyed on the
// PRESENCE of a Setenv("HOME", …) call, so it says nothing whatsoever about a
// function that isolates nothing at all.
//
// Concretely, and measured rather than reasoned: replacing one
// `home := testsupport.IsolateHome(t)` in pertool_test.go with
// `home := t.TempDir()` — leaving the other three calls so the import stays
// used, and adding no Setenv — makes `setsHome` false, so the function is never
// a candidate. ReportUnisolatedHomeTests printed nothing and
// TestHomeIsolationGuardIsClean PASSED. Under a sandbox HOME the mutated test
// passed too, because TestCheckEnabledTools_PerToolStatus drives injectable
// hooks and never writes a registry, so registry's write-path guard is never
// consulted either. Nothing in the suite noticed.
//
// That silence is not harmless. With HOME left at a developer's real home, that
// same test resolves mcpreg.SettingsPath(...) — which joins onto mcpreg's
// homeDir(), and homeDir() returns $HOME — and its own writeJSONMCP helper then
// os.WriteFile's over the result. That is the live ~/.claude.json and
// ~/.cursor/mcp.json, written by a raw os.WriteFile in a test helper, which
// mcpreg's guarded writer never sees. The #6240 incident, reachable by deleting
// one call.
//
// So: deleting an IsolateHome call is the likelier regression now that all 24
// historical offenders are fixed, and THIS GUARD DOES NOT CATCH IT. That is a
// stated limit, pinned by the "MISS: IsolateHome deleted with no replacement
// Setenv" row in TestHomeIsolationGuardDetectsTheShape, not an oversight to be
// discovered later by whoever trusts the green.
//
// Closing it needs a different shape of check — roughly "a test that reaches a
// $HOME-derived path must carry an isolation signal, directly or through a
// helper it calls" — which needs intra-package call-graph resolution (most
// tests here isolate via newDoctorTestEnv / quickEnv / reconcileSandbox rather
// than inline) plus a policy for the many tests that legitimately need no
// isolation at all. That is a deliberate design question, not a widening of
// this heuristic, and it is not attempted here.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// unisolatedHomeTest is one function that redirects HOME without also pinning
// the grafel home.
type unisolatedHomeTest struct {
	file string // repo-relative, slash-separated
	fn   string
	line int
}

func (u unisolatedHomeTest) String() string {
	return u.file + ":" + strconv.Itoa(u.line) + " " + u.fn
}

// ReportUnisolatedHomeTests scans this package's _test.go files and writes a
// report naming every function that redirects HOME without isolating the grafel
// home. It returns the number of offenders; 0 means nothing was written.
//
// It is called from TestMain BEFORE m.Run(), so it must not depend on any
// *testing.T. Scan errors are reported as offenders in their own right rather
// than swallowed: a walk that silently reads nothing is the vacuous-gate
// failure this file is trying to prevent, not a clean bill of health.
func ReportUnisolatedHomeTests(w io.Writer) int {
	dir, err := packageDirForHomeGuard()
	if err != nil {
		fmt.Fprintf(w, "install: home-isolation guard could not locate its own package directory: %v\n", err)
		return 1
	}
	offenders, scanned, err := scanUnisolatedHomeTests(dir)
	if err != nil {
		fmt.Fprintf(w, "install: home-isolation guard failed to scan %s: %v\n", dir, err)
		return 1
	}
	if scanned == 0 {
		fmt.Fprintf(w, "install: home-isolation guard parsed 0 _test.go files under %s — "+
			"the scan is not binding and proves nothing\n", dir)
		return 1
	}
	if len(offenders) == 0 {
		return 0
	}
	var names []string
	for _, o := range offenders {
		names = append(names, o.String())
	}
	sort.Strings(names)
	fmt.Fprintf(w,
		"\ninstall: HOME-ISOLATION DEFECT (#6171) — %d function(s) redirect $HOME without pinning\n"+
			"$GRAFEL_HOME, so registry.HomeDir() resolves an inherited GRAFEL_HOME instead and\n"+
			"registry.saveTo's sandbox guard PANICS, aborting this whole test binary and reporting\n"+
			"nothing for every test ordered after it:\n\n  %s\n\n"+
			"Fix: replace the t.Setenv(\"HOME\", …) block with `home := testsupport.IsolateHome(t)`.\n"+
			"This report is printed from TestMain, before any test runs, so it survives the panic.\n\n",
		len(offenders), strings.Join(names, "\n  "))
	return len(offenders)
}

// TestHomeIsolationGuardIsClean restates the TestMain report as an ordinary
// test result, so a green package means "scanned and clean" rather than
// "scanned, output scrolled past". It is deliberately redundant with TestMain:
// when the package is already broken this test may never run, which is exactly
// why the TestMain call exists and why this one cannot replace it.
//
// "Clean" here means only "no function sets HOME without pinning GRAFEL_HOME".
// It does NOT mean every test in this package is isolated — a test that
// isolates nothing at all passes this. See the file doc's "blind spot".
func TestHomeIsolationGuardIsClean(t *testing.T) {
	var sb strings.Builder
	if n := ReportUnisolatedHomeTests(&sb); n > 0 {
		t.Fatalf("%d unisolated-HOME function(s):\n%s", n, sb.String())
	}
}

// TestHomeIsolationGuardIsWiredIntoTestMain pins the placement, not just the
// detector. Deleting the ReportUnisolatedHomeTests call from TestMain is
// invisible to every other test here: with an offender present, the panic
// aborts the binary before TestHomeIsolationGuardIsClean runs, so removing the
// call and reintroducing the #6171 shape together produce a run in which
// nothing names the cause. Measured by mutation, both singly and together.
//
// "The #6171 shape" is load-bearing there, and narrower than it sounds: it
// means a function that still sets HOME literally while dropping the
// GRAFEL_HOME pin. A mutant that simply deletes an IsolateHome call and leaves
// no Setenv behind is invisible to the detector in the first place, so nothing
// downstream of it — including this test — has anything to report. See the file
// doc's "blind spot" section.
//
// This test reads copy_test.go's TestMain and asserts the call is there. It can
// only run when the package is otherwise healthy, which is precisely when the
// TestMain call looks removable, so the two cover disjoint cases.
func TestHomeIsolationGuardIsWiredIntoTestMain(t *testing.T) {
	dir, err := packageDirForHomeGuard()
	if err != nil {
		t.Fatalf("locate package dir: %v", err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(dir, "copy_test.go"), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse copy_test.go: %v", err)
	}
	var main *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "TestMain" && fd.Body != nil {
			main = fd
		}
	}
	if main == nil {
		t.Fatal("copy_test.go no longer declares TestMain — the #6171 report has nowhere to run " +
			"before the tests; move it to whichever file declares TestMain now")
	}
	called := false
	ast.Inspect(main.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "ReportUnisolatedHomeTests" {
			called = true
		}
		return true
	})
	if !called {
		t.Fatal("TestMain no longer calls ReportUnisolatedHomeTests. Without it, a test that " +
			"forgets testsupport.IsolateHome panics out of internal/registry's write guard and " +
			"aborts this binary with nothing in the output naming the cause — the #6171 defect.")
	}
}

// TestHomeIsolationGuardDetectsTheShape is the guard's own guard. The scan
// above is vacuously green the moment the detector stops binding, so the
// detector is exercised against synthetic source with known answers.
//
// The MISS rows assert what the detector does NOT see. They are not aspirations
// — they are the stated edge of its reach, confirmed by running it. If a change
// makes one of them caught, flip its want and delete the matching sentence in
// the file doc; do not delete the row.
func TestHomeIsolationGuardDetectsTheShape(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "HOME redirected with no grafel home pinned",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
}`,
			want: 1,
		},
		{
			name: "HOME plus GRAFEL_HOME is isolated",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_HOME", home+"/.grafel")
}`,
			want: 0,
		},
		{
			name: "IsolateHome is isolated even alongside a HOME redirect",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := testsupport.IsolateHome(t)
	t.Setenv("HOME", home)
}`,
			want: 0,
		},
		{
			name: "GRAFEL_DAEMON_ROOT alone does NOT count (see #6134)",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_DAEMON_ROOT", home+"/.grafel")
}`,
			want: 1,
		},
		{
			name: "a non-test helper that redirects HOME is caught too",
			src: `package p
import "testing"
func newEnv(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}`,
			want: 1,
		},
		{
			name: "os.Setenv is caught, not just t.Setenv",
			src: `package p
import "os"
func setup() { os.Setenv("HOME", "/tmp/x") }`,
			want: 1,
		},
		{
			name: "a function that touches neither is clean",
			src: `package p
import "testing"
func TestX(t *testing.T) { _ = t.TempDir() }`,
			want: 0,
		},
		{
			name: "two offenders in one file are both named",
			src: `package p
import "testing"
func TestA(t *testing.T) { t.Setenv("HOME", "/tmp/a") }
func TestB(t *testing.T) { t.Setenv("HOME", "/tmp/b") }`,
			want: 2,
		},

		// --- CONFIRMED MISSES ---
		{
			name: "MISS: the env-var name behind an identifier, not a literal",
			src: `package p
import "testing"
const homeEnv = "HOME"
func TestX(t *testing.T) { t.Setenv(homeEnv, "/tmp/x") }`,
			want: 0,
		},
		{
			name: "MISS: HOME set in one function, GRAFEL_HOME in its caller",
			src: `package p
import "testing"
func setHome(t *testing.T) { t.Setenv("HOME", "/tmp/x") }
func TestX(t *testing.T) { t.Setenv("GRAFEL_HOME", "/tmp/x/.grafel"); setHome(t) }`,
			want: 1,
		},
		{
			// THE important miss, not a curiosity: this is what "someone
			// deletes an IsolateHome call" actually looks like, and it is the
			// likeliest future regression now that the 24 historical offenders
			// are fixed. Reproduced against the real tree on pertool_test.go —
			// the detector reported 0 and TestHomeIsolationGuardIsClean passed.
			// See the file doc's "blind spot" section for the consequence.
			name: "MISS: IsolateHome deleted with no replacement Setenv",
			src: `package p
import "testing"
func TestX(t *testing.T) {
	home := t.TempDir()
	_ = home
}`,
			want: 0,
		},
		{
			name: "MISS: a var-bound top-level closure is not a FuncDecl",
			src: `package p
import "testing"
var f = func(t *testing.T) { t.Setenv("HOME", "/tmp/x") }`,
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "x_test.go", tc.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := len(findUnisolatedHomeTests(fset, f, "x_test.go")); got != tc.want {
				t.Fatalf("detector found %d offender(s), want %d", got, tc.want)
			}
		})
	}
}

// findUnisolatedHomeTests reports every top-level func in f whose body sets
// HOME but neither sets GRAFEL_HOME nor calls IsolateHome.
func findUnisolatedHomeTests(fset *token.FileSet, f *ast.File, rel string) []unisolatedHomeTest {
	var out []unisolatedHomeTest
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		setsHome, pinsGrafelHome, isolates := false, false, false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "IsolateHome":
				isolates = true
			case "Setenv":
				if len(call.Args) == 0 {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				switch s {
				case "HOME":
					setsHome = true
				case "GRAFEL_HOME":
					pinsGrafelHome = true
				}
			}
			return true
		})
		if setsHome && !pinsGrafelHome && !isolates {
			out = append(out, unisolatedHomeTest{file: rel, fn: fd.Name.Name, line: fset.Position(fd.Pos()).Line})
		}
	}
	return out
}

// scanUnisolatedHomeTests parses every _test.go file directly under dir (not
// subdirectories — sibling packages guard themselves) and returns the offenders
// plus the number of files actually parsed.
func scanUnisolatedHomeTests(dir string) ([]unisolatedHomeTest, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	var out []unisolatedHomeTest
	scanned := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil, scanned, fmt.Errorf("parse %s: %w", e.Name(), perr)
		}
		scanned++
		out = append(out, findUnisolatedHomeTests(fset, f, "internal/install/"+e.Name())...)
	}
	return out, scanned, nil
}

// packageDirForHomeGuard returns the directory holding this source file.
// runtime.Caller is used rather than os.Getwd because TestMain may run before
// anything has established a working directory convention, and because the
// answer must not change if a test chdirs.
func packageDirForHomeGuard() (string, error) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller(0) failed")
	}
	return filepath.Dir(here), nil
}
