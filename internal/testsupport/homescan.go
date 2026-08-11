package testsupport

// homescan.go — the #6171 "this test forgot to isolate $HOME" detector, made
// reusable so more than one package can run it.
//
// # Why it moved here (#6288)
//
// #6171 added this detector as unexported functions inside
// internal/install/home_isolation_guard_6171_test.go, scanning exactly one
// directory: its own. The file said so on purpose — "not subdirectories,
// sibling packages guard themselves" — and no sibling ever did. So when
// internal/mcp's newDocgenServer grew the identical defect (t.Setenv("HOME",…)
// and nothing else), there was no scan anywhere that could see it, and
// TestDocgenPromote_Atomic promoted docs into C:\Users\runneradmin\.grafel on
// the Windows runner. A guard whose reach is one directory is a guard that
// covers one directory; the reach was the defect, not the rule.
//
// It lives in a non-_test.go file because a _test.go file is not importable by
// another package. testsupport is already the test-only home for
// IsolateHome/GuardRealHome and is imported by test code across the tree.
//
// # The rule, and how #6288 widened it
//
// A function that redirects HOME must also pin the two things HOME alone does
// not cover:
//
//   - GRAFEL_HOME — registry.HomeDir() prefers it, so an inherited GRAFEL_HOME
//     beats the test's own HOME and the write lands outside the sandbox. This
//     is #6171's original rule.
//   - USERPROFILE — on Windows os.UserHomeDir() reads %USERPROFILE% and IGNORES
//     $HOME. A test that sets only HOME is isolated on unix and NOT isolated on
//     Windows, which is why #6288 surfaced only on windows-latest and why
//     several packages already carry a hand-written
//     `t.Setenv("USERPROFILE", home) // #6178` line. #6171's rule could not
//     see the missing ones.
//
// testsupport.IsolateHome sets both (and XDG_CONFIG_HOME, XDG_RUNTIME_DIR and
// GRAFEL_DAEMON_ROOT), so calling it satisfies the rule outright and is the
// remediation every report names.
//
// # The blind spot, restated because it did not move
//
// This detector is keyed on the PRESENCE of a Setenv("HOME", …) call. It says
// nothing about a function that isolates nothing at all — deleting an
// IsolateHome call and leaving no Setenv behind is invisible to it. That is
// pinned by a MISS row in the detector's table test, not assumed. See #6171's
// analysis in internal/install/home_isolation_guard_6171_test.go for what
// closing it would take.
//
// It also has FALSE POSITIVES, which matter as much as the misses for anyone
// wiring it into a new package. Two shapes, both real and both measured:
//
//   - Composed isolation. A helper that sets HOME while its CALLER pins the
//     rest is reported, because the detector reasons one function at a time.
//     Pinned as a FALSE POSITIVE row in homescan_test.go.
//   - Tests that point HOME at the real home ON PURPOSE.
//     isolate_test.go's TestGuardRealHomeFailsWhenHomeIsRealHome does exactly
//     that in order to assert GuardRealHome fires; pinning GRAFEL_HOME there
//     would defeat the test it is asserting.
//
// # Adopting this in another package is NOT mechanical (#6290)
//
// Run repo-wide at the time of writing, this rule reports 82 functions across
// internal/cli, internal/daemon{,/service}, internal/dashboard,
// internal/install/{mcpreg,watchers,skilllink,detect}, internal/extractors,
// internal/registry, internal/testsupport and cmd/grafel. That number is a list
// of things to LOOK at, not a list of things to change: it includes at least the
// deliberate offender above, and every package adopting the guard needs a
// suppression story for its own equivalents before the report can be made
// binding. Treating the remainder as a sed is how a guard acquires a wall of
// noise that everybody learns to ignore.

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
)

// UnisolatedHomeTest is one function that redirects HOME without also pinning
// everything HOME alone does not cover.
type UnisolatedHomeTest struct {
	File    string   // repo-relative, slash-separated
	Fn      string   // function name
	Line    int      // declaration line
	Missing []string // the env vars it should have pinned and did not
}

func (u UnisolatedHomeTest) String() string {
	return u.File + ":" + strconv.Itoa(u.Line) + " " + u.Fn +
		" (missing " + strings.Join(u.Missing, ", ") + ")"
}

// FindUnisolatedHomeTests reports every top-level func in f whose body sets
// HOME but neither calls IsolateHome nor pins both GRAFEL_HOME and USERPROFILE.
//
// rel is the display path used in the result; it is not read from disk.
func FindUnisolatedHomeTests(fset *token.FileSet, f *ast.File, rel string) []UnisolatedHomeTest {
	var out []UnisolatedHomeTest
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		var setsHome, isolates bool
		pinned := map[string]bool{}
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
				case envHome:
					setsHome = true
				case envGrafelHome, envUserProfile:
					pinned[s] = true
				}
			}
			return true
		})
		if !setsHome || isolates {
			continue
		}
		var missing []string
		for _, v := range []string{envGrafelHome, envUserProfile} {
			if !pinned[v] {
				missing = append(missing, v)
			}
		}
		if len(missing) == 0 {
			continue
		}
		out = append(out, UnisolatedHomeTest{
			File:    rel,
			Fn:      fd.Name.Name,
			Line:    fset.Position(fd.Pos()).Line,
			Missing: missing,
		})
	}
	return out
}

// ScanUnisolatedHomeTests parses every _test.go file directly under dir (NOT
// subdirectories — each package wires its own guard, and a package that has not
// wired one is not silently covered by its parent) and returns the offenders
// plus the number of files actually parsed.
//
// relPrefix is prepended to each file name for display, e.g. "internal/mcp/".
func ScanUnisolatedHomeTests(dir, relPrefix string) ([]UnisolatedHomeTest, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	var out []UnisolatedHomeTest
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
		out = append(out, FindUnisolatedHomeTests(fset, f, relPrefix+e.Name())...)
	}
	return out, scanned, nil
}

// ReportUnisolatedHomeTests scans dir and writes a report naming every function
// that redirects HOME without isolating the rest. It returns the number of
// offenders; 0 means nothing was written.
//
// Call it from TestMain BEFORE m.Run(), so the diagnosis is the first thing the
// run emits: internal/registry's write-path guard PANICS on a sandbox escape,
// and a panic aborts the whole test binary, so a report emitted by an ordinary
// test is liable to be truncated by the very failure it exists to explain.
//
// It must therefore not depend on a *testing.T. Scan errors are reported as
// offenders in their own right rather than swallowed: a walk that silently
// reads nothing is the vacuous-gate failure this is trying to prevent, not a
// clean bill of health.
func ReportUnisolatedHomeTests(w io.Writer, dir, relPrefix string) int {
	offenders, scanned, err := ScanUnisolatedHomeTests(dir, relPrefix)
	if err != nil {
		fmt.Fprintf(w, "%shome-isolation guard failed to scan %s: %v\n", relPrefix, dir, err)
		return 1
	}
	if scanned == 0 {
		fmt.Fprintf(w, "%shome-isolation guard parsed 0 _test.go files under %s — "+
			"the scan is not binding and proves nothing\n", relPrefix, dir)
		return 1
	}
	if len(offenders) == 0 {
		return 0
	}
	names := make([]string, 0, len(offenders))
	for _, o := range offenders {
		names = append(names, o.String())
	}
	sort.Strings(names)
	fmt.Fprintf(w,
		"\n%sHOME-ISOLATION DEFECT (#6171/#6288) — %d function(s) redirect $HOME without pinning\n"+
			"everything $HOME alone does not cover:\n\n"+
			"  GRAFEL_HOME  — registry.HomeDir() prefers it, so an inherited GRAFEL_HOME beats the\n"+
			"                 test's own HOME, the write lands outside the sandbox, and\n"+
			"                 registry.saveTo's guard PANICS, aborting this whole test binary.\n"+
			"  USERPROFILE  — on Windows os.UserHomeDir() reads %%USERPROFILE%% and IGNORES $HOME, so\n"+
			"                 the test is isolated on unix and writes into the real profile on\n"+
			"                 Windows, silently (#6288).\n\n  %s\n\n"+
			"Fix: replace the t.Setenv(\"HOME\", …) block with `home := testsupport.IsolateHome(t)`,\n"+
			"which sets HOME, USERPROFILE, GRAFEL_HOME, GRAFEL_DAEMON_ROOT and both XDG vars.\n"+
			"This report is printed from TestMain, before any test runs, so it survives the panic.\n\n",
		relPrefix, len(offenders), strings.Join(names, "\n  "))
	return len(offenders)
}

// PackageDirOfCaller returns the directory holding the source file skip frames
// up the stack; skip=0 means the immediate caller's file.
//
// runtime.Caller is used rather than os.Getwd because TestMain may run before
// anything has established a working directory convention, and because the
// answer must not change if a test chdirs.
func PackageDirOfCaller(skip int) (string, error) {
	_, here, _, ok := runtime.Caller(skip + 1)
	if !ok {
		return "", fmt.Errorf("runtime.Caller(%d) failed", skip+1)
	}
	return filepath.Dir(here), nil
}
