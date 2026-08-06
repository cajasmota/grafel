// Regression tests for issue #6178 round 3: the shared derivation in
// group_paths.go (GroupHome, PassSidecarPath, MemoryDir, PatternsDir,
// RealHomeOrphanWarning). These are the single source every reader/writer
// in internal/mcp, internal/dashboard, internal/docgen, internal/cli, and
// cmd/grafel now routes through instead of hand-rolling
// os.UserHomeDir()+".grafel" joins — so a regression here breaks every one
// of those call sites at once, and a test here catches it without needing
// per-package fixtures.
package links

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withDivergentHome sets HOME/USERPROFILE and GRAFEL_HOME to two DIFFERENT
// temp dirs — the shape a real isolated run uses (only GRAFEL_HOME set) —
// and returns (sandboxHome, grafelHome).
func withDivergentHome(t *testing.T) (string, string) {
	t.Helper()
	sandboxHome := t.TempDir()
	grafelHome := t.TempDir()
	t.Setenv("HOME", sandboxHome)
	t.Setenv("USERPROFILE", sandboxHome)
	t.Setenv("GRAFEL_HOME", grafelHome)
	return sandboxHome, grafelHome
}

func TestGroupHome_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	_, grafelHome := withDivergentHome(t)
	got, err := GroupHome("")
	if err != nil {
		t.Fatalf("GroupHome: %v", err)
	}
	if got != grafelHome {
		t.Fatalf("GroupHome(\"\") = %q, want %q", got, grafelHome)
	}
}

func TestGroupHome_ExplicitWins_6178(t *testing.T) {
	withDivergentHome(t)
	explicit := t.TempDir()
	got, err := GroupHome(explicit)
	if err != nil {
		t.Fatalf("GroupHome: %v", err)
	}
	if got != explicit {
		t.Fatalf("GroupHome(%q) = %q, want the explicit value unchanged", explicit, got)
	}
}

func TestPassSidecarPath_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	got, err := PassSidecarPath("", "g6178", "effects")
	if err != nil {
		t.Fatalf("PassSidecarPath: %v", err)
	}
	want := filepath.Join(grafelHome, "groups", "g6178-links-effects.json")
	if got != want {
		t.Fatalf("PassSidecarPath = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-links-effects.json")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

// TestPassSidecarPath_AllEightSuffixes covers every downstream-pass sidecar
// suffix by name, so a future rename of one pass's suffix string is caught
// here rather than only in whichever MCP/dashboard test happens to exercise
// that one pass.
func TestPassSidecarPath_AllEightSuffixes_6178(t *testing.T) {
	_, grafelHome := withDivergentHome(t)
	suffixes := []string{
		"effects", "data-flow", "reachability", "taint",
		"module-cycles", "pure-functions", "def-use", "template-patterns",
	}
	for _, suffix := range suffixes {
		got, err := PassSidecarPath("", "g6178", suffix)
		if err != nil {
			t.Fatalf("PassSidecarPath(%q): %v", suffix, err)
		}
		want := filepath.Join(grafelHome, "groups", "g6178-links-"+suffix+".json")
		if got != want {
			t.Errorf("PassSidecarPath(%q) = %q, want %q", suffix, got, want)
		}
	}
}

func TestMemoryDir_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	got, err := MemoryDir("", "g6178")
	if err != nil {
		t.Fatalf("MemoryDir: %v", err)
	}
	want := filepath.Join(grafelHome, "groups", "g6178-memory")
	if got != want {
		t.Fatalf("MemoryDir = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-memory")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

func TestPatternsDir_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	got, err := PatternsDir("", "g6178")
	if err != nil {
		t.Fatalf("PatternsDir: %v", err)
	}
	want := filepath.Join(grafelHome, "groups", "g6178-patterns")
	if got != want {
		t.Fatalf("PatternsDir = %q, want %q", got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-patterns")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}

// --- RealHomeOrphanWarning ---------------------------------------------

func TestRealHomeOrphanWarning_EmptyWhenGRAFELHomeUnset_6178(t *testing.T) {
	sandboxHome := t.TempDir()
	t.Setenv("HOME", sandboxHome)
	t.Setenv("USERPROFILE", sandboxHome)
	t.Setenv("GRAFEL_HOME", "")
	resolved := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-memory")
	if got := RealHomeOrphanWarning("saved findings", resolved, "g6178-memory"); got != "" {
		t.Fatalf("want empty warning when GRAFEL_HOME unset, got: %s", got)
	}
}

func TestRealHomeOrphanWarning_EmptyWhenNoLegacyData_6178(t *testing.T) {
	_, grafelHome := withDivergentHome(t)
	resolved := filepath.Join(grafelHome, "groups", "g6178-memory")
	if got := RealHomeOrphanWarning("saved findings", resolved, "g6178-memory"); got != "" {
		t.Fatalf("want empty warning when legacy path has no data, got: %s", got)
	}
}

func TestRealHomeOrphanWarning_EmptyWhenResolvedHasData_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	// Legacy dir has data.
	legacyDir := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-memory")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "finding-1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Resolved (GRAFEL_HOME) dir ALSO has data — nothing orphaned.
	resolvedDir := filepath.Join(grafelHome, "groups", "g6178-memory")
	if err := os.MkdirAll(resolvedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resolvedDir, "finding-1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RealHomeOrphanWarning("saved findings", resolvedDir, "g6178-memory"); got != "" {
		t.Fatalf("want empty warning when resolved path already has data, got: %s", got)
	}
}

func TestRealHomeOrphanWarning_FiresWhenOrphaned_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	legacyDir := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-memory")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "finding-1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolvedDir := filepath.Join(grafelHome, "groups", "g6178-memory") // does not exist
	got := RealHomeOrphanWarning("saved findings", resolvedDir, "g6178-memory")
	if got == "" {
		t.Fatal("want a non-empty warning: legacy data exists, resolved path is empty, GRAFEL_HOME is set")
	}
	if !strings.Contains(got, legacyDir) || !strings.Contains(got, resolvedDir) {
		t.Fatalf("warning should name both paths, got: %s", got)
	}
}

func TestRealHomeOrphanWarning_EmptyDirIsNotData_6178(t *testing.T) {
	sandboxHome, grafelHome := withDivergentHome(t)
	legacyDir := filepath.Join(sandboxHome, ".grafel", "groups", "g6178-memory")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err) // exists but EMPTY — not "data"
	}
	resolvedDir := filepath.Join(grafelHome, "groups", "g6178-memory")
	if got := RealHomeOrphanWarning("saved findings", resolvedDir, "g6178-memory"); got != "" {
		t.Fatalf("an empty legacy dir must not trigger a warning, got: %s", got)
	}
}
