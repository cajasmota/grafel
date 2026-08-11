package mcp

// home_isolation_guard_6288_test.go — this package's copy of the #6171 wiring.
//
// # Why it exists
//
// #6171 built an AST detector for "a test redirects HOME and isolates nothing
// else" and wired it into internal/install's TestMain. It scanned exactly one
// directory — its own — on the stated assumption that "sibling packages guard
// themselves". None did.
//
// So when newDocgenServer (docgen_test.go) set HOME and nothing else, no scan
// anywhere could see it. On unix that is harmless by luck: os.UserHomeDir()
// reads $HOME, so docsRoot() → registry.HomeDir() → the sandbox. On Windows
// os.UserHomeDir() reads %USERPROFILE% and IGNORES $HOME, so #6246's move of
// docsRoot() onto registry.HomeDir() turned it into a live escape:
// TestDocgenPromote_Atomic rotated and promoted docs into the CI user's real
// C:\Users\runneradmin\.grafel\docs, and TestDocgenList then read that real
// directory and found the file the previous test had left there instead of its
// own three. Two failures, one escape, no guard.
//
// The detector is now in internal/testsupport (homescan.go) where it is
// importable. This file is the wiring for internal/mcp, and it is why the
// package's TestMain exists at all.
//
// # Why TestMain and not an ordinary Test
//
// internal/registry's write-path guard PANICS on a sandbox escape, and a panic
// aborts the whole test binary. A report emitted by an ordinary test is
// therefore liable to be truncated by the very failure it exists to explain —
// #6171 measured a run in which 1 of 286 tests executed and nothing named the
// cause. Reporting from TestMain, before m.Run(), makes the diagnosis the first
// thing the run emits. It does NOT abort: m.Run() still executes, because
// refusing to run the package would be a second blackout with better manners.
//
// # Reach and blind spot
//
// go/parser applies no build constraints, so the scan reads //go:build-tagged
// files on every platform — a Windows-only defect is caught by READING it on
// macOS, which is the only reason #6288 is fixable without a Windows machine.
//
// It is keyed on the PRESENCE of a Setenv("HOME", …) call and says nothing
// about a function that isolates nothing at all. That limit is pinned by MISS
// rows in internal/testsupport/homescan_test.go, not assumed.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestMain reports unisolated-HOME functions before any test runs, then runs the
// suite. The report going to stderr rather than a *testing.T is deliberate: it
// has to survive a panic that kills the binary.
func TestMain(m *testing.M) {
	unisolated := reportUnisolatedHomeTests(os.Stderr)
	code := m.Run()
	if unisolated > 0 && code == 0 {
		code = 1
	}
	os.Exit(code)
}

// reportUnisolatedHomeTests scans THIS package's directory.
func reportUnisolatedHomeTests(w interface{ Write([]byte) (int, error) }) int {
	dir, err := testsupport.PackageDirOfCaller(0)
	if err != nil {
		fmt.Fprintf(w, "mcp: home-isolation guard could not locate its own package directory: %v\n", err)
		return 1
	}
	return testsupport.ReportUnisolatedHomeTests(w, dir, "internal/mcp/")
}

// TestHomeIsolationGuardIsClean restates the TestMain report as an ordinary
// test result, so a green package means "scanned and clean" rather than
// "scanned, output scrolled past". Deliberately redundant with TestMain: when
// the package is already broken this test may never run, which is exactly why
// the TestMain call exists and why this one cannot replace it.
func TestHomeIsolationGuardIsClean(t *testing.T) {
	var sb strings.Builder
	if n := reportUnisolatedHomeTests(&sb); n > 0 {
		t.Fatalf("%d unisolated-HOME function(s):\n%s", n, sb.String())
	}
}

// TestHomeIsolationGuardIsWiredIntoTestMain pins the placement, not the
// detector. With an offender present the registry panic aborts the binary
// before TestHomeIsolationGuardIsClean can run, so deleting the TestMain call
// and reintroducing the defect together produce a run in which nothing names
// the cause. The two tests cover disjoint cases.
//
// It asserts a CALL, not a mention. A first draft searched the TestMain body
// for the identifier and a mutant that replaced the call with `unisolated := 0`
// while leaving a `_ = reportUnisolatedHomeTests` reference behind SURVIVED it —
// "the name appears somewhere in the function" is not the property that matters,
// "the detector runs before m.Run" is. Measured, not reasoned.
func TestHomeIsolationGuardIsWiredIntoTestMain(t *testing.T) {
	dir, err := testsupport.PackageDirOfCaller(0)
	if err != nil {
		t.Fatalf("locate package dir: %v", err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset,
		filepath.Join(dir, "home_isolation_guard_6288_test.go"), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse this file: %v", err)
	}
	var main *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "TestMain" && fd.Body != nil {
			main = fd
		}
	}
	if main == nil {
		t.Fatal("this package no longer declares TestMain — the #6288 report has nowhere to run " +
			"before the tests; move it to whichever file declares TestMain now")
	}

	// The call must exist, and it must come before m.Run() — a report emitted
	// after the suite is a report a panic can truncate, which is the whole
	// reason it is not an ordinary test.
	reportPos, runPos := token.NoPos, token.NoPos
	ast.Inspect(main.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			if fn.Name == "reportUnisolatedHomeTests" && !reportPos.IsValid() {
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
		t.Fatal("TestMain no longer CALLS reportUnisolatedHomeTests. Without it, a test that " +
			"forgets testsupport.IsolateHome writes into the developer's real home on Windows " +
			"with nothing in the output naming the cause — the #6288 defect.")
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
