package homeguard

// homeguard_test.go — the decision (Escapes) is tested against synthetic homes
// so BOTH answers are exercised on any host, and the panic wrapper is tested
// against the process's real home.
//
// Escapes is split out of Guard precisely so this file does not have to arrange
// a process whose real home is a TempDir in order to test the negative case.

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEscapes(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "dev")
	cases := []struct {
		name     string
		resolved string
		home     string
		want     bool
	}{
		{"the home itself", home, home, true},
		{"a dotfile in the home", filepath.Join(home, ".claude.json"), home, true},
		{"nested under the home", filepath.Join(home, ".cursor", "mcp.json"), home, true},
		{"a sibling with the home as a name PREFIX", home + "-sandbox/x.json", home, false},
		{"an unrelated absolute path", filepath.Join(string(filepath.Separator), "tmp", "t", "x"), home, false},
		{"empty home disables the guard", filepath.Join(home, "x"), "", false},
		{"empty path is not a write", "", home, false},
		{"a traversal back out of the home", filepath.Join(home, "..", "other", "x"), home, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Escapes(tc.resolved, tc.home); got != tc.want {
				t.Fatalf("Escapes(%q, %q) = %v, want %v", tc.resolved, tc.home, got, tc.want)
			}
		})
	}
}

// TestGuard_PanicsOnTheRealHome proves the wrapper actually fires, and that the
// message carries both the marker every caller's test greps for and the caller's
// own remediation text.
func TestGuard_PanicsOnTheRealHome(t *testing.T) {
	if RealUserHome == "" {
		t.Skip("no real user home captured; cannot exercise the escape path")
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("Guard did not panic for a path inside the real home %q", RealUserHome)
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "TEST SANDBOX ESCAPE") {
			t.Errorf("panic message lacks the marker callers grep for: %q", msg)
		}
		if !strings.Contains(msg, "call IsolateHome") {
			t.Errorf("panic message dropped the caller's remediation text: %q", msg)
		}
		if !strings.Contains(msg, "widget") {
			t.Errorf("panic message dropped the caller's component name: %q", msg)
		}
	}()
	// A path that is never created — Guard panics before any caller writes.
	Guard("widget", "a thing", filepath.Join(RealUserHome, ".grafel-homeguard-unit-probe"),
		"call IsolateHome(t).")
	t.Fatalf("Guard returned without panicking")
}

// TestGuard_IsInertOffTheRealHome is the other half: the ~40 correctly-isolated
// tests in every calling package must keep running.
func TestGuard_IsInertOffTheRealHome(t *testing.T) {
	dir := t.TempDir()
	if Escapes(dir, RealUserHome) {
		// Windows puts t.TempDir() below %USERPROFILE%\AppData\Local\Temp. See
		// the package doc on the temp-root exemption this deliberately does not
		// have.
		t.Skipf("t.TempDir() %q is inside the real home on this platform", dir)
	}
	Guard("widget", "a thing", filepath.Join(dir, "x.json"), "call IsolateHome(t).")
}
