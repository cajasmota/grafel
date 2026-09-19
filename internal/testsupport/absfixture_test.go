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
