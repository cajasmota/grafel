package testsupport_test

// absfixture_test.go — AbsFixture's contract, asserted through filepath.IsAbs
// rather than by string shape, because IsAbs is the only reason the helper
// exists.

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

// NOTE ON WHAT THIS CAN AND CANNOT PROVE. On darwin and linux this test is
// TAUTOLOGICAL: AbsFixture returns its input unchanged there, and the inputs are
// already absolute, so a mutant gutting the helper to `return path` survives
// every package that uses it. That is unavoidable — the behaviour under test is
// windows-only — but it means the whole apparatus built on this helper is
// verified by exactly one thing: the windows CI leg. Which is why a fixture row
// that silently stops being routed through it must fail loudly there, and why
// the tables carry count floors rather than trusting the flag.
func TestAbsFixture_IsAbsoluteOnEveryPlatform(t *testing.T) {
	for _, p := range []string{
		"/usr/local/bin/grafel",
		"/opt/grafel/daemon/bin/grafel",
		"/Users/jane smith/Library/grafel",
		"/tmp/agent-worktree-1/grafel",
	} {
		got := testsupport.AbsFixture(p)
		if !filepath.IsAbs(got) {
			t.Errorf("AbsFixture(%q) = %q, which filepath.IsAbs rejects on %s — "+
				"a fixture table using it would grade nothing here", p, got, runtime.GOOS)
		}
	}
}

// TestAbsFixture_UnixIsUntouched pins that the helper is a no-op off windows.
// If it rewrote paths on unix, every /tmp-prefix boundary row in the kill-stale
// tables would silently move off its boundary on the platform where those rows
// are the coverage.
func TestAbsFixture_UnixIsUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("contract is about the non-windows branch")
	}
	const p = "/tmp/agent-worktree-1/grafel"
	if got := testsupport.AbsFixture(p); got != p {
		t.Errorf("AbsFixture(%q) = %q, want it returned unchanged on %s", p, got, runtime.GOOS)
	}
}

// TestAbsFixture_PreservesTheBasename is the property every IsCanonicalBinaryPath
// fixture depends on: prefixing a volume must not disturb what filepath.Base
// reports, or an identity gate keyed on the basename would be graded against a
// different binary name than the row intended.
func TestAbsFixture_PreservesTheBasename(t *testing.T) {
	for _, p := range []string{"/opt/grafel/bin/grafel", "/Users/jane/grafel-daemon-helper/bin/helper"} {
		if got, want := filepath.Base(testsupport.AbsFixture(p)), filepath.Base(p); got != want {
			t.Errorf("filepath.Base(AbsFixture(%q)) = %q, want %q", p, got, want)
		}
	}
}

// TestAbsFixture_IsIdempotent pins the neighbour of the axis the test above
// covers. That one asserts a SINGLE application produces an absolute path;
// nothing asserted what a SECOND application does, and the answer was
// "C:C:/usr/local/bin/grafel", which filepath.IsAbs rejects on windows
// (volumeNameLen is 2, and path[2:] then starts with 'C', not a separator).
//
// Both identity tables reach this function twice on one fixture — a shared
// selfExe variable is absolutised once at the top and then routed again as a
// table row — so double application is a live path, not a hypothetical.
//
// Asserted as a fixed point rather than as a string shape, because the
// interesting property is that the SECOND call changes nothing; comparing
// against a hand-built "C:" + p would just re-implement the function.
func TestAbsFixture_IsIdempotent(t *testing.T) {
	for _, p := range []string{
		"/usr/local/bin/grafel",
		"/opt/grafel/daemon/bin/grafel",
		"/Users/jane smith/Library/grafel",
		"/tmp/agent-worktree-1/grafel",
		"grafel",                  // relative in, relative out — still a fixed point
		"./grafel",                //
		"",                        // degenerate, and it must not grow a volume twice
		"/opt/grafel/daemon/bin/", // trailing separator
	} {
		once := testsupport.AbsFixture(p)
		twice := testsupport.AbsFixture(once)
		if once != twice {
			t.Errorf("AbsFixture is not idempotent for %q: once=%q twice=%q", p, once, twice)
		}
	}
}

// TestAbsFixture_DoubleRoutedFixtureStaysAbsolute states the consequence in the
// terms the callers care about: the shared-variable shape both identity tables
// use must still yield a path the identity gate accepts.
func TestAbsFixture_DoubleRoutedFixtureStaysAbsolute(t *testing.T) {
	selfExe := testsupport.AbsFixture("/usr/local/bin/grafel") // as a table computes it
	row := testsupport.AbsFixture(selfExe)                     // as the shared routing re-applies it
	if !filepath.IsAbs(row) {
		t.Errorf("a double-routed fixture is %q, which filepath.IsAbs rejects on %s — "+
			"the row would fail as 'not absolute' on a path that starts with a volume",
			row, runtime.GOOS)
	}
	if row != selfExe {
		t.Errorf("double routing changed the fixture: %q -> %q", selfExe, row)
	}
}
