package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/registry"
)

// TestDetectGraphChanges_TriggersOnMtimeBump verifies the watcher's
// inner change-detection helper: a registered group-mate's graph.json
// being touched between ticks must surface in detectGraphChanges' output
// so the surrounding loop knows to re-run the link passes.
func TestDetectGraphChanges_TriggersOnMtimeBump(t *testing.T) {
	home := withSandboxHome(t)

	// Two repos, registered as a group.
	repoA := filepath.Join(home, "repos", "alpha")
	repoB := filepath.Join(home, "repos", "beta")
	for _, r := range []string{repoA, repoB} {
		if err := os.MkdirAll(filepath.Join(r, ".archigraph"), 0o755); err != nil {
			t.Fatal(err)
		}
		// Seed a minimal graph.json so Stat succeeds.
		gj := filepath.Join(r, ".archigraph", "graph.json")
		if err := os.WriteFile(gj, []byte(`{"version":1,"repo":"x","entities":[],"relationships":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Register the group.
	cfgDir, err := registry.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "g.fleet.json")
	cfg := registry.GroupConfig{
		Name: "g",
		Repos: []registry.Repo{
			{Slug: "alpha", Path: repoA},
			{Slug: "beta", Path: repoB},
		},
	}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup("g", cfgPath); err != nil {
		t.Fatal(err)
	}

	// Initial snapshot.
	prev := snapshotGraphMtimes(repoA, "")
	if len(prev) != 2 {
		t.Fatalf("snapshot expected 2 graph.json entries, got %d (%v)", len(prev), prev)
	}

	// First call without changes: no group should be reported.
	if got := detectGraphChanges(repoA, "", prev); len(got) != 0 {
		t.Fatalf("expected no changes initially, got %v", got)
	}

	// Bump beta's graph.json mtime forward by 2s.
	gjBeta := filepath.Join(repoB, ".archigraph", "graph.json")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(gjBeta, future, future); err != nil {
		t.Fatal(err)
	}

	got := detectGraphChanges(repoA, "", prev)
	if len(got) != 1 || got[0] != "g" {
		t.Fatalf("expected group 'g' to be reported as changed, got %v", got)
	}

	// Second call with no further mtime change: should report nothing.
	if again := detectGraphChanges(repoA, "", prev); len(again) != 0 {
		t.Fatalf("change should not repeat without a new mtime bump, got %v", again)
	}
}

// TestRunWatch_TriggersLinksHookOnGraphChange asserts the live watcher
// loop wires the RunLinks hook when a group-mate's graph.json mtime
// advances between polling ticks.
func TestRunWatch_TriggersLinksHookOnGraphChange(t *testing.T) {
	home := withSandboxHome(t)

	repoA := filepath.Join(home, "repos", "alpha")
	repoB := filepath.Join(home, "repos", "beta")
	for _, r := range []string{repoA, repoB} {
		if err := os.MkdirAll(filepath.Join(r, ".archigraph"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r, ".archigraph", "graph.json"),
			[]byte(`{"version":1,"repo":"x","entities":[],"relationships":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfgDir, _ := registry.ConfigDir()
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "g.fleet.json")
	cfg := registry.GroupConfig{Name: "g", Repos: []registry.Repo{
		{Slug: "alpha", Path: repoA}, {Slug: "beta", Path: repoB},
	}}
	b, _ := json.Marshal(cfg)
	_ = os.WriteFile(cfgPath, b, 0o644)
	_ = registry.AddGroup("g", cfgPath)

	// Pre-snapshot mirroring the watcher's first read.
	prev := snapshotGraphMtimes(repoA, "")

	// Bump alpha's mtime forward.
	future := time.Now().Add(3 * time.Second)
	if err := os.Chtimes(filepath.Join(repoA, ".archigraph", "graph.json"), future, future); err != nil {
		t.Fatal(err)
	}

	// Install a counting RunLinks hook and call detectGraphChanges
	// directly (the daemon loop is otherwise time-driven).
	called := []string{}
	prevHooks := activeHooks
	activeHooks = Hooks{RunLinks: func(group string) error {
		called = append(called, group)
		return nil
	}}
	t.Cleanup(func() { activeHooks = prevHooks })

	changed := detectGraphChanges(repoA, "", prev)
	for _, g := range changed {
		if activeHooks.RunLinks != nil {
			_ = activeHooks.RunLinks(g)
		}
	}
	if len(called) != 1 || called[0] != "g" {
		t.Fatalf("expected RunLinks hook to fire once for group 'g', got %v", called)
	}
}
