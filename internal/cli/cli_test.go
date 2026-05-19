package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/registry"
)

// withSandboxHome redirects every path the CLI might write to into a
// per-test TempDir so concurrent tests can't collide.
func withSandboxHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", filepath.Join(dir, ".archigraph"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("HOME", dir)
	return dir
}

func makeRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWizardNonInteractive(t *testing.T) {
	home := withSandboxHome(t)
	repoA := filepath.Join(home, "repos", "alpha")
	repoB := filepath.Join(home, "repos", "beta")
	makeRepo(t, repoA)
	makeRepo(t, repoB)

	// Skip MCP/watcher real installs (paths don't matter — sandbox).
	out := &bytes.Buffer{}
	err := runWizard(out, wizardOptions{
		NonInteractive: true,
		GroupName:      "demo",
		ReposCSV:       repoA + "," + repoB,
		Watchers:       false,
		GitHooks:       true,
		RunInstall:     true,
	})
	if err != nil {
		t.Fatalf("wizard: %v\n%s", err, out.String())
	}

	groups, err := registry.Groups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Name != "demo" {
		t.Fatalf("registry: %+v", groups)
	}
	cfg, err := registry.LoadGroupConfig(groups[0].ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("repos: %+v", cfg.Repos)
	}
	for _, r := range cfg.Repos {
		hookPath := filepath.Join(r.Path, ".git/hooks/post-commit")
		if _, err := os.Stat(hookPath); err != nil {
			t.Fatalf("hook missing for %s: %v", r.Slug, err)
		}
	}
	// Manifest written into both repos.
	for _, p := range []string{repoA, repoB} {
		if _, err := os.Stat(filepath.Join(p, ".archigraph/group.json")); err != nil {
			t.Fatalf("manifest missing in %s", p)
		}
	}
}

func TestDoctorRunsCleanly(t *testing.T) {
	home := withSandboxHome(t)
	repo := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repo)
	cfg := &registry.GroupConfig{Name: "demo"}
	cfg.Features.GitHooks = true
	cfg.Repos = []registry.Repo{{Slug: "alpha", Path: repo, Stack: "go"}}
	cfgPath, err := registry.ConfigPathFor("demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup("demo", cfgPath); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	if err := runDoctor(out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Group: demo") {
		t.Fatalf("doctor missing group: %s", out.String())
	}
	if !strings.Contains(out.String(), "alpha") {
		t.Fatalf("doctor missing repo: %s", out.String())
	}
}

func TestStatusFiltering(t *testing.T) {
	home := withSandboxHome(t)
	for _, name := range []string{"alpha", "beta"} {
		repo := filepath.Join(home, "repos", name)
		makeRepo(t, repo)
		cfg := &registry.GroupConfig{Name: name}
		cfg.Repos = []registry.Repo{{Slug: name, Path: repo, Stack: "go"}}
		p, _ := registry.ConfigPathFor(name)
		if err := registry.SaveGroupConfig(p, cfg); err != nil {
			t.Fatal(err)
		}
		if err := registry.AddGroup(name, p); err != nil {
			t.Fatal(err)
		}
	}
	out := &bytes.Buffer{}
	if err := runStatus(out, "alpha"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "Group: alpha") || strings.Contains(got, "Group: beta") {
		t.Fatalf("filter broken: %s", got)
	}
}

func TestStatusGraphFileDetection(t *testing.T) {
	home := withSandboxHome(t)

	repo := filepath.Join(home, "repos", "test")
	makeRepo(t, repo)

	// Create a group with one repo but no graph files yet
	cfg := &registry.GroupConfig{Name: "demo"}
	cfg.Repos = []registry.Repo{{Slug: "test", Path: repo, Stack: "go"}}
	cfgPath, _ := registry.ConfigPathFor("demo")
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup("demo", cfgPath); err != nil {
		t.Fatal(err)
	}

	archigraphDir := filepath.Join(repo, ".archigraph")

	// Test 1: No graph files exist
	out := &bytes.Buffer{}
	if err := runStatus(out, "demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "graph: (none)") {
		t.Errorf("status should show 'graph: (none)' when no files exist: %s", out.String())
	}

	// Test 2: Only graph.json exists
	if err := os.MkdirAll(archigraphDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archigraphDir, "graph.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	out = &bytes.Buffer{}
	if err := runStatus(out, "demo"); err != nil {
		t.Fatal(err)
	}
	statusText := out.String()
	if !strings.Contains(statusText, "graph:") || strings.Contains(statusText, "(none)") {
		t.Errorf("status should show graph timestamp when json exists: %s", statusText)
	}
	if strings.Contains(statusText, "graph.json:") {
		t.Errorf("status should not show 'graph.json:' label (issue #822): %s", statusText)
	}

	// Test 3: graph.fb exists (the main #822 fix)
	if err := os.Remove(filepath.Join(archigraphDir, "graph.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archigraphDir, "graph.fb"), []byte("fb"), 0644); err != nil {
		t.Fatal(err)
	}

	out = &bytes.Buffer{}
	if err := runStatus(out, "demo"); err != nil {
		t.Fatal(err)
	}
	statusText = out.String()
	if !strings.Contains(statusText, "graph:") || strings.Contains(statusText, "(none)") {
		t.Errorf("status should show graph timestamp when fb exists (fix for #822): %s", statusText)
	}
	if strings.Contains(statusText, "graph.json:") {
		t.Errorf("status should not show 'graph.json:' label when only fb exists: %s", statusText)
	}

	// Test 4: Both graph.fb and graph.json exist
	if err := os.WriteFile(filepath.Join(archigraphDir, "graph.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	out = &bytes.Buffer{}
	if err := runStatus(out, "demo"); err != nil {
		t.Fatal(err)
	}
	statusText = out.String()
	if !strings.Contains(statusText, "graph:") || strings.Contains(statusText, "(none)") {
		t.Errorf("status should show graph timestamp when both files exist: %s", statusText)
	}
}

func TestPrimaryHelpHidesAdvanced(t *testing.T) {
	root := newRoot()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "wizard") || !strings.Contains(got, "doctor") {
		t.Fatalf("primary help missing setup commands: %s", got)
	}
	if strings.Contains(got, "remerge") {
		t.Fatalf("advanced command leaked into primary help: %s", got)
	}
}

func TestHelpAdvancedListsEverything(t *testing.T) {
	root := newRoot()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetArgs([]string{"help", "advanced"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, cmd := range []string{"wizard", "doctor", "rebuild", "reset", "uninstall", "monorepo", "watch"} {
		if !strings.Contains(got, cmd) {
			t.Errorf("advanced help missing %q\n%s", cmd, got)
		}
	}
}
