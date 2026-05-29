package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cajasmota/archigraph/internal/registry"
)

func TestParseRepoSpecs(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "alpha")
	b := filepath.Join(tmp, "beta")
	c := filepath.Join(tmp, "gamma")

	repos, err := parseRepoSpecs([]string{"core=" + a, b}, c)
	if err != nil {
		t.Fatalf("parseRepoSpecs: %v", err)
	}
	if len(repos) != 3 {
		t.Fatalf("want 3 repos, got %d: %+v", len(repos), repos)
	}
	// Explicit slug honored.
	if repos[0].Slug != "core" || repos[0].Path != a {
		t.Errorf("repo0 = %+v, want slug=core path=%s", repos[0], a)
	}
	// Bare --repo path → slug = basename.
	if repos[1].Slug != "beta" || repos[1].Path != b {
		t.Errorf("repo1 = %+v, want slug=beta path=%s", repos[1], b)
	}
	// CSV path → slug = basename.
	if repos[2].Slug != "gamma" || repos[2].Path != c {
		t.Errorf("repo2 = %+v, want slug=gamma path=%s", repos[2], c)
	}
}

func TestParseRepoSpecs_EmptyPathErrors(t *testing.T) {
	if _, err := parseRepoSpecs([]string{"slug="}, ""); err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestGroupAdd_RegistersConfigAndJSON(t *testing.T) {
	home := withSandboxHome(t)
	repoA := filepath.Join(home, "repos", "alpha")
	repoB := filepath.Join(home, "repos", "beta")
	makeRepo(t, repoA)
	makeRepo(t, repoB)

	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetOut(out)

	err := runGroupAddImpl(cmd, "demo", groupAddFlags{
		repoArgs: []string{"core=" + repoA, repoB},
		watchers: false,
		gitHooks: true,
		rules:    false, // skip writing IDE rules files in the sandbox
		mcp:      false, // skip MCP registration
		runInst:  true,
		doIndex:  false, // no daemon in unit test
		jsonOut:  true,
	}, "")
	if err != nil {
		t.Fatalf("group add: %v\n%s", err, out.String())
	}

	// Registry has the group.
	groups, err := registry.Groups()
	if err != nil {
		t.Fatal(err)
	}
	var found *registry.GroupRef
	for i := range groups {
		if groups[i].Name == "demo" {
			found = &groups[i]
		}
	}
	if found == nil {
		t.Fatalf("group demo not registered; groups=%+v", groups)
	}

	// Fleet config has both repos with the right slugs.
	cfg, err := registry.LoadGroupConfig(found.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("want 2 repos in config, got %d", len(cfg.Repos))
	}
	if cfg.Repos[0].Slug != "core" || cfg.Repos[1].Slug != "beta" {
		t.Errorf("slugs = %q,%q; want core,beta", cfg.Repos[0].Slug, cfg.Repos[1].Slug)
	}

	// JSON result is well-formed.
	var res groupAddResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("json output: %v\n%s", err, out.String())
	}
	if res.Group != "demo" || len(res.Repos) != 2 || res.Indexed {
		t.Errorf("result = %+v, want group=demo, 2 repos, indexed=false", res)
	}
	if res.Installed == nil {
		t.Error("expected installed counts in result")
	}
}

func TestGroupAdd_NoReposErrors(t *testing.T) {
	withSandboxHome(t)
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	if err := runGroupAddImpl(cmd, "demo", groupAddFlags{runInst: false}, ""); err == nil {
		t.Fatal("expected error when no repos supplied")
	}
}

func TestGroupAdd_Idempotent(t *testing.T) {
	home := withSandboxHome(t)
	repoA := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repoA)

	run := func() error {
		cmd := &cobra.Command{}
		cmd.SetOut(&bytes.Buffer{})
		return runGroupAddImpl(cmd, "demo", groupAddFlags{
			repoArgs: []string{repoA},
			rules:    false, mcp: false, runInst: true, jsonOut: true,
		}, "")
	}
	if err := run(); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := run(); err != nil {
		t.Fatalf("second add (idempotent): %v", err)
	}
	groups, err := registry.Groups()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range groups {
		if g.Name == "demo" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("group registered %d times, want exactly 1 (idempotent)", n)
	}
}
