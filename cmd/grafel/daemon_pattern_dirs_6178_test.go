// Regression test for issue #6178 round 3: daemonPatternGroupDirs
// hand-rolled an os.UserHomeDir() join for each group's patterns
// directory, ignoring GRAFEL_HOME — the same shape internal/mcp/
// patterns.go and internal/dashboard/handlers_patterns.go independently
// hand-rolled too. It now delegates to links.PatternsDir, the shared
// derivation all three use.
package main

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/registry"
)

func TestDaemonPatternGroupDirs_HonorsGRAFELHomeEnv_6178(t *testing.T) {
	sandboxHome := t.TempDir()
	grafelHome := t.TempDir()
	t.Setenv("HOME", sandboxHome)
	t.Setenv("USERPROFILE", sandboxHome)
	t.Setenv("GRAFEL_HOME", grafelHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(sandboxHome, ".config"))

	const group = "g6178-daemon-patterns"
	cfg := &registry.GroupConfig{Name: group}
	cfg.Repos = []registry.Repo{{Slug: "alpha", Path: filepath.Join(sandboxHome, "alpha"), Stack: registry.StackList{"go"}}}
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup(group, cfgPath); err != nil {
		t.Fatal(err)
	}

	dirs := daemonPatternGroupDirs()
	got, ok := dirs[group]
	if !ok {
		t.Fatalf("daemonPatternGroupDirs() missing entry for %q: %+v", group, dirs)
	}
	want := filepath.Join(grafelHome, "groups", group+"-patterns")
	if got != want {
		t.Fatalf("daemonPatternGroupDirs()[%q] = %q, want %q", group, got, want)
	}
	fallback := filepath.Join(sandboxHome, ".grafel", "groups", group+"-patterns")
	if got == fallback {
		t.Fatalf("regression: resolved via HOME (%s) instead of GRAFEL_HOME", sandboxHome)
	}
}
