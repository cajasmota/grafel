package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	return dir
}

func TestLoadEmpty(t *testing.T) {
	withHome(t)
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != 1 || len(r.Groups) != 0 {
		t.Fatalf("expected empty registry, got %+v", r)
	}
}

func TestAddGroupValidatesConfigExists(t *testing.T) {
	home := withHome(t)
	cfgPath := filepath.Join(home, "xdg", "archigraph", "missing.fleet.json")

	// Try to add a group with a non-existent config file.
	err := AddGroup("missing", cfgPath)
	if err == nil {
		t.Fatal("expected error for missing config file, got nil")
	}
	errMsg := err.Error()
	if !contains(errMsg, "does not exist") && !contains(errMsg, "cannot access") {
		t.Fatalf("expected error about config file not being accessible, got: %v", err)
	}

	// Verify the group was not added.
	groups, _ := Groups()
	if len(groups) != 0 {
		t.Fatalf("expected no groups, got %d", len(groups))
	}
}

func TestAddRemoveGroup(t *testing.T) {
	home := withHome(t)
	cfgPath, err := ConfigPathFor("alpha")
	if err != nil {
		t.Fatal(err)
	}
	// Create the config file first.
	os.MkdirAll(filepath.Dir(cfgPath), 0o755)
	if err := os.WriteFile(cfgPath, []byte(`{"name":"alpha"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := AddGroup("alpha", cfgPath); err != nil {
		t.Fatal(err)
	}

	betaCfgPath := filepath.Join(home, "xdg", "archigraph", "beta.fleet.json")
	os.MkdirAll(filepath.Dir(betaCfgPath), 0o755)
	if err := os.WriteFile(betaCfgPath, []byte(`{"name":"beta"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddGroup("beta", betaCfgPath); err != nil {
		t.Fatal(err)
	}
	groups, err := Groups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].Name != "alpha" || groups[1].Name != "beta" {
		t.Fatalf("groups: %+v", groups)
	}
	// Idempotent re-add.
	if err := AddGroup("alpha", cfgPath); err != nil {
		t.Fatal(err)
	}
	groups, _ = Groups()
	if len(groups) != 2 {
		t.Fatalf("idempotent add broken: %+v", groups)
	}
	if err := RemoveGroup("alpha"); err != nil {
		t.Fatal(err)
	}
	groups, _ = Groups()
	if len(groups) != 1 || groups[0].Name != "beta" {
		t.Fatalf("after remove: %+v", groups)
	}
	// Idempotent remove of unknown group.
	if err := RemoveGroup("ghost"); err != nil {
		t.Fatal(err)
	}
}

func TestSaveLoadGroupConfig(t *testing.T) {
	dir := withHome(t)
	cfg := &GroupConfig{
		Name:  "demo",
		Repos: []Repo{{Slug: "core", Path: "/tmp/core", Stack: "go"}},
	}
	cfg.Features.Watchers = true
	cfg.Features.GitHooks = true
	p := filepath.Join(dir, "demo.fleet.json")
	if err := SaveGroupConfig(p, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := LoadGroupConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || len(got.Repos) != 1 || !got.Features.Watchers {
		t.Fatalf("roundtrip: %+v", got)
	}
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".archigraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"group":"demo","repos":[{"slug":"core","clone_url":"git@x:y.git"}]}`
	if err := os.WriteFile(filepath.Join(dir, ".archigraph", "group.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Group != "demo" || len(m.Repos) != 1 || m.Repos[0].Slug != "core" {
		t.Fatalf("manifest: %+v", m)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
