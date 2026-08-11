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
// #6288 added USERPROFILE to that list. On Windows os.UserHomeDir() reads
// %USERPROFILE% and ignores $HOME, so pinning GRAFEL_HOME alone still leaves
// every path that resolves through os.UserHomeDir() rather than
// registry.HomeDir() pointing at the developer's real profile.
//
// # Reach, stated rather than implied
//
// go/parser applies no build constraints, so this scan reads
// //go:build darwin files on every platform. #6171 lived in a darwin-only file
// and Linux CI could never have surfaced it by running the test; this guard
// surfaces it by reading it.
//
// What it does NOT read is any directory but this one. That was a deliberate
// choice in #6171 — "sibling packages guard themselves" — and #6288 is the bill
// for it: no sibling ever did, so when internal/mcp's newDocgenServer grew the
// identical defect there was no scan anywhere that could see it, and
// TestDocgenPromote_Atomic promoted docs into the Windows runner's real
// C:\Users\runneradmin\.grafel. The detector therefore moved to
// internal/testsupport (homescan.go), which is importable; this file is now the
// wiring, and internal/mcp has its own copy of the wiring. Its table test moved
// with it and pins what the detector does and does not see.
//
// # The blind spot — read this before trusting a green result
//
// (Restated in internal/testsupport/homescan.go, which now owns the detector.)
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
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// ReportUnisolatedHomeTests scans THIS package's _test.go files and writes a
// report naming every function that redirects HOME without isolating the rest.
// It returns the number of offenders; 0 means nothing was written.
//
// It is called from TestMain BEFORE m.Run(), so it must not depend on any
// *testing.T.
//
// #6288 moved the detector itself into internal/testsupport so other packages
// can run it too; this stays as the wiring, because the placement (TestMain,
// this package) is what the two tests below pin and what #6171 was about.
func ReportUnisolatedHomeTests(w io.Writer) int {
	dir, err := testsupport.PackageDirOfCaller(0)
	if err != nil {
		fmt.Fprintf(w, "install: home-isolation guard could not locate its own package directory: %v\n", err)
		return 1
	}
	return testsupport.ReportUnisolatedHomeTests(w, dir, "internal/install/")
}

// TestHomeIsolationGuardIsClean restates the TestMain report as an ordinary
// test result, so a green package means "scanned and clean" rather than
// "scanned, output scrolled past". It is deliberately redundant with TestMain:
// when the package is already broken this test may never run, which is exactly
// why the TestMain call exists and why this one cannot replace it.
//
// "Clean" here means only "no function sets HOME without pinning GRAFEL_HOME
// and USERPROFILE". It does NOT mean every test in this package is isolated — a
// test that isolates nothing at all passes this. See the blind spot recorded in
// internal/testsupport/homescan.go.
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
// nothing names the cause.
//
// "The #6171 shape" is load-bearing there, and narrower than it sounds: it
// means a function that still sets HOME literally while dropping the pins. A
// mutant that simply deletes an IsolateHome call and leaves no Setenv behind is
// invisible to the detector in the first place, so nothing downstream of it —
// including this test — has anything to report. See the blind spot in
// internal/testsupport/homescan.go.
//
// #6290: this asserts a CALL, positioned BEFORE m.Run(). It previously walked
// for an *ast.Ident of the right name anywhere in the body, which was measurably
// dead: it survived replacing the call with `_ = ReportUnisolatedHomeTests`, and
// it survived moving the report to AFTER m.Run(). Both leave this package in
// exactly the #6171 state — panicking out of registry.saveTo with nothing in the
// output naming the cause — which is the failure the test exists to prevent. Its
// old failure string also claimed "TestMain no longer calls…", something an
// identifier walk cannot establish. Ported from the equivalent in
// internal/mcp/home_isolation_guard_6288_test.go, where it was written correctly
// first; this file is the one #6288 rewrote, so leaving the weaker copy in place
// would have been the same "reach was one directory" argument turned inward.
func TestHomeIsolationGuardIsWiredIntoTestMain(t *testing.T) {
	dir, err := testsupport.PackageDirOfCaller(0)
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

	reportPos, runPos := token.NoPos, token.NoPos
	ast.Inspect(main.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			if fn.Name == "ReportUnisolatedHomeTests" && !reportPos.IsValid() {
				reportPos = call.Pos()
			}
		case *ast.SelectorExpr:
			if fn.Sel.Name == "Run" && !runPos.IsValid() {
				runPos = call.Pos()
			}
		}
		return true
	})
	if !reportPos.IsValid() {
		t.Fatal("TestMain no longer CALLS ReportUnisolatedHomeTests. Without it, a test that " +
			"forgets testsupport.IsolateHome panics out of internal/registry's write guard and " +
			"aborts this binary with nothing in the output naming the cause — the #6171 defect.")
	}
	if !runPos.IsValid() {
		t.Fatal("TestMain no longer calls m.Run() — the suite does not run")
	}
	if reportPos > runPos {
		t.Fatal("TestMain reports AFTER m.Run(). internal/registry's write guard panics out of " +
			"the binary on a sandbox escape, so a report ordered after the suite is exactly the " +
			"one a panic truncates. Move it before m.Run().")
	}
}
