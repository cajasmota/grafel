// Regression tests for issue #6178 round 3: sidecarPath (phase3_tools.go),
// reachabilitySidecarPath (dead_code.go), and effectsSidecarPath
// (effects_tool.go) each hand-rolled their own os.UserHomeDir()/
// os.Getenv("HOME") join and never consulted GRAFEL_HOME. They now delegate
// to links.PassSidecarPath, the shared derivation; these tests pin that
// delegation at each of the three call sites independently, so a revert of
// any ONE of them (back to a hand-rolled join) fails here even though the
// other two — and the shared internal/links tests — stay green.
package mcp

import (
	"path/filepath"
	"testing"
)

func TestSidecarPath_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	got := sidecarPath("g6178", "pure-functions")
	want := filepath.Join(grafelHome, "groups", "g6178-links-pure-functions.json")
	if got != want {
		t.Fatalf("sidecarPath = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-links-pure-functions.json")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

func TestReachabilitySidecarPath_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	got := reachabilitySidecarPath("g6178")
	want := filepath.Join(grafelHome, "groups", "g6178-links-reachability.json")
	if got != want {
		t.Fatalf("reachabilitySidecarPath = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-links-reachability.json")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

func TestEffectsSidecarPath_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	got := effectsSidecarPath("g6178")
	want := filepath.Join(grafelHome, "groups", "g6178-links-effects.json")
	if got != want {
		t.Fatalf("effectsSidecarPath = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-links-effects.json")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

func TestDefaultPatternsDir_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	got := defaultPatternsDir("g6178")
	want := filepath.Join(grafelHome, "groups", "g6178-patterns")
	if got != want {
		t.Fatalf("defaultPatternsDir = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-patterns")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}
