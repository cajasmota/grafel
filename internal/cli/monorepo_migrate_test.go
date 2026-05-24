package cli

// monorepo_migrate_test.go — tests for MigratePerSubRepoFleet (M3 #2180).
//
// Three test cases:
//  1. simple-collapse: N sub-repos with shared git-toplevel → 1 parent + N modules.
//  2. idempotent: running migrate twice produces no change after first run.
//  3. mixed: some repos standalone (unique git root), some collapsable.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/archigraph/internal/registry"
)

// gitAvailableCLI returns true when git is on PATH.
func gitAvailableCLI() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// initGitRepoForMigrate creates a minimal git repo with one commit.
func initGitRepoForMigrate(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@test.invalid")
	run("config", "user.name", "Test")
	placeholder := filepath.Join(dir, "README.md")
	if err := os.WriteFile(placeholder, []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
}

// writeTestGroupConfig writes a minimal fleet config + registry in a temp dir
// and sets ARCHIGRAPH_HOME + XDG_CONFIG_HOME environment overrides so the
// production registry functions read from the temp dir.
func writeTestGroupConfig(t *testing.T, tmp, groupName string, repos []registry.Repo) (configPath string) {
	t.Helper()
	cfg := &registry.GroupConfig{
		Name:  groupName,
		Repos: repos,
	}
	configPath = filepath.Join(tmp, groupName+".fleet.json")
	if err := registry.SaveGroupConfig(configPath, cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}
	// Write registry.json pointing at our fleet config.
	archigraphHome := filepath.Join(tmp, ".archigraph")
	if err := os.MkdirAll(archigraphHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARCHIGRAPH_HOME", archigraphHome)
	// Also set XDG_CONFIG_HOME so ConfigPathFor writes to tmp.
	t.Setenv("XDG_CONFIG_HOME", tmp)
	if err := registry.AddGroup(groupName, configPath); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	return configPath
}

// TestMigratePerSubRepoFleet_SimpleCollapse verifies that N repos sharing a
// git-toplevel are collapsed into 1 parent repo with N-1 module sub-paths.
func TestMigratePerSubRepoFleet_SimpleCollapse(t *testing.T) {
	if !gitAvailableCLI() {
		t.Skip("git not on PATH — skipping migrate test")
	}

	tmp := t.TempDir()

	// Create one git repo that acts as the monorepo root.
	parentPath := filepath.Join(tmp, "platform")
	initGitRepoForMigrate(t, parentPath)

	// Create sub-dirs that are "sub-repos" in the old fleet config.
	paymentsPath := filepath.Join(parentPath, "services", "payments")
	ordersPath := filepath.Join(parentPath, "services", "orders")
	if err := os.MkdirAll(paymentsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ordersPath, 0o755); err != nil {
		t.Fatal(err)
	}

	repos := []registry.Repo{
		{Slug: "platform", Path: parentPath},
		{Slug: "payments-svc", Path: paymentsPath},
		{Slug: "orders-svc", Path: ordersPath},
	}
	writeTestGroupConfig(t, tmp, "fleet-x", repos)

	result, err := MigratePerSubRepoFleet("fleet-x")
	if err != nil {
		t.Fatalf("MigratePerSubRepoFleet: %v", err)
	}

	if len(result.Collapsed) != 1 || result.Collapsed[0] != "platform" {
		t.Errorf("collapsed: want [platform], got %v", result.Collapsed)
	}

	// Re-load config and verify shape.
	groups, err := registry.Groups()
	if err != nil {
		t.Fatalf("registry.Groups: %v", err)
	}
	var cfgPath string
	for _, g := range groups {
		if g.Name == "fleet-x" {
			cfgPath = g.ConfigPath
		}
	}
	if cfgPath == "" {
		t.Fatal("group fleet-x not found in registry")
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadGroupConfig: %v", err)
	}

	if len(cfg.Repos) != 1 {
		t.Fatalf("repos after migration: want 1, got %d", len(cfg.Repos))
	}
	parent := cfg.Repos[0]
	if parent.Slug != "platform" {
		t.Errorf("parent slug: want platform, got %q", parent.Slug)
	}
	sort.Strings(parent.Modules)
	want := []string{"services/orders", "services/payments"}
	if len(parent.Modules) != 2 {
		t.Fatalf("modules: want 2, got %d: %v", len(parent.Modules), parent.Modules)
	}
	for i, m := range parent.Modules {
		if m != want[i] {
			t.Errorf("module[%d]: want %q, got %q", i, want[i], m)
		}
	}
}

// TestMigratePerSubRepoFleet_Idempotent verifies that running migrate twice
// produces no change after the first run.
func TestMigratePerSubRepoFleet_Idempotent(t *testing.T) {
	if !gitAvailableCLI() {
		t.Skip("git not on PATH — skipping migrate idempotent test")
	}

	tmp := t.TempDir()
	parentPath := filepath.Join(tmp, "monorepo")
	initGitRepoForMigrate(t, parentPath)

	subPath := filepath.Join(parentPath, "pkg", "auth")
	if err := os.MkdirAll(subPath, 0o755); err != nil {
		t.Fatal(err)
	}

	repos := []registry.Repo{
		{Slug: "monorepo", Path: parentPath},
		{Slug: "auth-pkg", Path: subPath},
	}
	writeTestGroupConfig(t, tmp, "fleet-idem", repos)

	// First run — collapses.
	res1, err := MigratePerSubRepoFleet("fleet-idem")
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if len(res1.Collapsed) != 1 {
		t.Errorf("first run: want 1 collapsed, got %v", res1.Collapsed)
	}

	// Second run — idempotent: no new collapses and no changes to fleet config.
	res2, err := MigratePerSubRepoFleet("fleet-idem")
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	// After the first run, only 1 repo remains per git root, so the second run
	// sees it as a standalone and reports it as Unchanged. No additional
	// collapses should happen.
	if len(res2.Collapsed) != 0 {
		t.Errorf("second run: want 0 newly collapsed, got %v", res2.Collapsed)
	}

	// Fleet config should still have 1 repo with 1 module (unchanged from first run).
	groups, _ := registry.Groups()
	var cfgPath string
	for _, g := range groups {
		if g.Name == "fleet-idem" {
			cfgPath = g.ConfigPath
		}
	}
	cfg, _ := registry.LoadGroupConfig(cfgPath)
	if len(cfg.Repos) != 1 {
		t.Fatalf("after idempotent run: want 1 repo, got %d", len(cfg.Repos))
	}
	if len(cfg.Repos[0].Modules) != 1 {
		t.Errorf("after idempotent run: want 1 module, got %v", cfg.Repos[0].Modules)
	}
}

// TestMigratePerSubRepoFleet_Mixed verifies that standalone repos (unique git
// root) are left untouched while collapsable repos are merged.
func TestMigratePerSubRepoFleet_Mixed(t *testing.T) {
	if !gitAvailableCLI() {
		t.Skip("git not on PATH — skipping migrate mixed test")
	}

	tmp := t.TempDir()

	// Monorepo: platform + 2 sub-repos.
	platformPath := filepath.Join(tmp, "platform")
	initGitRepoForMigrate(t, platformPath)
	apiPath := filepath.Join(platformPath, "api")
	workerPath := filepath.Join(platformPath, "worker")
	for _, d := range []string{apiPath, workerPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Standalone repo: separate git repo.
	standaloneA := filepath.Join(tmp, "standalone-a")
	initGitRepoForMigrate(t, standaloneA)
	standaloneB := filepath.Join(tmp, "standalone-b")
	initGitRepoForMigrate(t, standaloneB)

	repos := []registry.Repo{
		{Slug: "platform", Path: platformPath},
		{Slug: "api-svc", Path: apiPath},
		{Slug: "worker-svc", Path: workerPath},
		{Slug: "standalone-a", Path: standaloneA},
		{Slug: "standalone-b", Path: standaloneB},
	}
	writeTestGroupConfig(t, tmp, "fleet-mixed", repos)

	result, err := MigratePerSubRepoFleet("fleet-mixed")
	if err != nil {
		t.Fatalf("MigratePerSubRepoFleet: %v", err)
	}

	// Exactly 1 parent was collapsed.
	if len(result.Collapsed) != 1 || result.Collapsed[0] != "platform" {
		t.Errorf("collapsed: want [platform], got %v", result.Collapsed)
	}
	// 2 standalone repos unchanged.
	if len(result.Unchanged) != 2 {
		t.Errorf("unchanged: want 2, got %v", result.Unchanged)
	}

	// Verify final fleet shape: 3 repos (platform + 2 standalones).
	groups, _ := registry.Groups()
	var cfgPath string
	for _, g := range groups {
		if g.Name == "fleet-mixed" {
			cfgPath = g.ConfigPath
		}
	}
	cfg, _ := registry.LoadGroupConfig(cfgPath)
	if len(cfg.Repos) != 3 {
		slugs := make([]string, len(cfg.Repos))
		for i, r := range cfg.Repos {
			slugs[i] = r.Slug
		}
		t.Errorf("repos: want 3, got %d: %v", len(cfg.Repos), slugs)
	}

	// Verify we can JSON-round-trip the result without errors.
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(result): %v", err)
	}
	var rt MigrateResult
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
}
