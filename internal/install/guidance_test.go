package install_test

// guidance_test.go — #5702: grafel's Claude guidance defaults to the PERSONAL
// ~/.claude/CLAUDE.md (self-gating, opt-in per developer), the project-root
// CLAUDE.md is decluttered on install, and --project-guidance (ProjectGuidance)
// commits a repo-specific block to <repo>/.claude/CLAUDE.md. Non-grafel content
// is never touched.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/registry"
)

// applyGuidance runs a real (non-dry-run) install under an isolated HOME with
// only the Claude tool enabled, and returns the Result plus the paths that
// matter for guidance assertions. MCP/hooks/watchers are skipped so the test
// isolates the guidance path.
func applyGuidance(t *testing.T, repo string, projectGuidance bool) *install.Result {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(home, ".grafel"))

	cfg := &registry.GroupConfig{
		Name:  "demo",
		Repos: []registry.Repo{{Slug: "r", Path: repo}},
		Tools: []string{"claude"},
	}
	res, err := install.Apply(install.Options{
		Group:           "demo",
		Config:          cfg,
		BinPath:         "/usr/local/bin/grafel",
		SkipHooks:       true,
		SkipWatchers:    true,
		SkipMCP:         true,
		ProjectGuidance: projectGuidance,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return res
}

// TestApply_WritesPersonalGuidanceIdempotent covers requirement (a): install
// writes the self-gating block to the fake HOME's .claude/CLAUDE.md between the
// markers; a re-run updates it in place (no duplicate).
func TestApply_WritesPersonalGuidanceIdempotent(t *testing.T) {
	repo := t.TempDir()
	res := applyGuidance(t, repo, false)

	personal := res.PersonalGuidancePath
	if personal == "" {
		t.Fatal("PersonalGuidancePath empty; personal guidance not written")
	}
	want := filepath.Join(os.Getenv("HOME"), ".claude", "CLAUDE.md")
	if personal != want {
		t.Fatalf("personal path = %q, want %q", personal, want)
	}
	data, err := os.ReadFile(personal)
	if err != nil {
		t.Fatalf("read personal guidance: %v", err)
	}
	s := string(data)
	for _, sub := range []string{"self-gating", "grafel:mcp-usage:start", "ignore this section entirely"} {
		if !strings.Contains(s, sub) {
			t.Errorf("personal guidance missing %q:\n%s", sub, s)
		}
	}
	if strings.Contains(s, "part of grafel group") {
		t.Errorf("personal guidance must be group-agnostic:\n%s", s)
	}

	// Re-run: same HOME (env persists for the test), block updated in place.
	cfg := &registry.GroupConfig{
		Name:  "demo",
		Repos: []registry.Repo{{Slug: "r", Path: repo}},
		Tools: []string{"claude"},
	}
	if _, err := install.Apply(install.Options{
		Group: "demo", Config: cfg, BinPath: "/usr/local/bin/grafel",
		SkipHooks: true, SkipWatchers: true, SkipMCP: true,
	}); err != nil {
		t.Fatalf("re-Apply: %v", err)
	}
	data2, _ := os.ReadFile(personal)
	if n := strings.Count(string(data2), "grafel:mcp-usage:start"); n != 1 {
		t.Fatalf("expected exactly 1 block after re-install, got %d:\n%s", n, data2)
	}
}

// TestApply_MigratesRepoRootClaudeMd covers requirement (b): a project-root
// CLAUDE.md of "before\n<grafel block>\nafter" has ONLY the grafel block
// removed; the before/after content is preserved.
func TestApply_MigratesRepoRootClaudeMd(t *testing.T) {
	repo := t.TempDir()
	// Seed a repo-root CLAUDE.md with user content wrapping a grafel block.
	block := "<!-- grafel:mcp-usage:start v=2 -->\n## grafel MCP\nold repo guidance\n<!-- grafel:mcp-usage:end -->"
	seeded := "before-block content\n\n" + block + "\n\nafter-block content\n"
	rootClaude := filepath.Join(repo, "CLAUDE.md")
	if err := os.WriteFile(rootClaude, []byte(seeded), 0o644); err != nil {
		t.Fatal(err)
	}

	res := applyGuidance(t, repo, false)

	data, err := os.ReadFile(rootClaude)
	if err != nil {
		t.Fatalf("repo-root CLAUDE.md should still exist: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "grafel:mcp-usage") {
		t.Errorf("grafel block not stripped from repo-root CLAUDE.md:\n%s", s)
	}
	if !strings.Contains(s, "before-block content") || !strings.Contains(s, "after-block content") {
		t.Errorf("surrounding content not preserved:\n%s", s)
	}
	found := false
	for _, r := range res.MigratedGuidanceRepos {
		if r == repo {
			found = true
		}
	}
	if !found {
		t.Errorf("repo not reported in MigratedGuidanceRepos: %v", res.MigratedGuidanceRepos)
	}
}

// TestApply_ProjectGuidanceWritesRepoFile covers requirement (c):
// --project-guidance writes the repo-specific block to <repo>/.claude/CLAUDE.md.
func TestApply_ProjectGuidanceWritesRepoFile(t *testing.T) {
	repo := t.TempDir()
	res := applyGuidance(t, repo, true)

	projPath := filepath.Join(repo, ".claude", "CLAUDE.md")
	data, err := os.ReadFile(projPath)
	if err != nil {
		t.Fatalf("project guidance not written to %s: %v", projPath, err)
	}
	s := string(data)
	if !strings.Contains(s, "grafel:mcp-usage:start") {
		t.Errorf("project guidance missing block:\n%s", s)
	}
	// Repo-specific block carries the group name.
	if !strings.Contains(s, "**demo**") {
		t.Errorf("project guidance should embed group name:\n%s", s)
	}
	var reported bool
	for _, p := range res.ProjectGuidanceFiles {
		if p == projPath {
			reported = true
		}
	}
	if !reported {
		t.Errorf("project file not reported in ProjectGuidanceFiles: %v", res.ProjectGuidanceFiles)
	}
	// Personal file is ALSO written (project guidance is additive).
	if res.PersonalGuidancePath == "" {
		t.Errorf("personal guidance should still be written alongside project guidance")
	}
}

// TestApply_LeavesNonGrafelClaudeMdUntouched covers requirement (d): a
// repo-root CLAUDE.md with NO grafel block is never modified by install.
func TestApply_LeavesNonGrafelClaudeMdUntouched(t *testing.T) {
	repo := t.TempDir()
	rootClaude := filepath.Join(repo, "CLAUDE.md")
	original := "# Team conventions\n\nUse tabs. No grafel here.\n"
	if err := os.WriteFile(rootClaude, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	res := applyGuidance(t, repo, false)

	data, _ := os.ReadFile(rootClaude)
	if string(data) != original {
		t.Errorf("non-grafel CLAUDE.md was modified:\nwant %q\ngot  %q", original, string(data))
	}
	for _, r := range res.MigratedGuidanceRepos {
		if r == repo {
			t.Errorf("no-op repo wrongly reported as migrated: %v", res.MigratedGuidanceRepos)
		}
	}
}
