// Regression tests for issue #6178 (read-side): defaultLinksFile,
// defaultMemoryDir, defaultLinkCandidatesFile, and defaultRegistryPath used
// os.UserHomeDir() directly, ignoring GRAFEL_HOME — the read-side twin of
// the write-side bug in internal/cli/links.go. Under an isolated GRAFEL_HOME
// the MCP server would read (or resolve as its registry path) the real
// home's files even though a link pass just wrote its output under
// GRAFEL_HOME.
package mcp

import (
	"path/filepath"
	"testing"
)

// withDivergentHome sets HOME and GRAFEL_HOME to two DIFFERENT temp dirs —
// the shape a real isolated run uses (only GRAFEL_HOME overridden) — and
// returns (sandboxHome, grafelHome).
func withDivergentHome(t *testing.T) (string, string) {
	t.Helper()
	sandboxHome := t.TempDir()
	grafelHome := t.TempDir()
	t.Setenv("HOME", sandboxHome)
	t.Setenv("USERPROFILE", sandboxHome) // os.UserHomeDir() reads this on Windows
	t.Setenv("GRAFEL_HOME", grafelHome)
	return sandboxHome, grafelHome
}

func TestDefaultLinksFile_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	got := defaultLinksFile("g6178")
	want := filepath.Join(grafelHome, "groups", "g6178-links.json")
	if got != want {
		t.Fatalf("defaultLinksFile = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-links.json")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

func TestDefaultMemoryDir_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	got := defaultMemoryDir("g6178")
	want := filepath.Join(grafelHome, "groups", "g6178-memory")
	if got != want {
		t.Fatalf("defaultMemoryDir = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-memory")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

func TestDefaultLinkCandidatesFile_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	got := defaultLinkCandidatesFile("g6178")
	want := filepath.Join(grafelHome, "groups", "g6178-link-candidates.json")
	if got != want {
		t.Fatalf("defaultLinkCandidatesFile = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-link-candidates.json")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

func TestDefaultRegistryPath_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	got := defaultRegistryPath()
	want := filepath.Join(grafelHome, "registry.json")
	if got != want {
		t.Fatalf("defaultRegistryPath = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "registry.json")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}
