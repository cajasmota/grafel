package main

// End-to-end tests for issue #2683: `grafel install` writes the
// canonical grafel rules block to every IDE-rules file convention
// (AGENTS.md, CLAUDE.md, .windsurfrules, .cursorrules,
// .codeium/instructions.md, .github/copilot-instructions.md) in each
// registered repo, and `grafel doctor` reports the per-repo state.
//
// These tests do NOT spawn the daemon or call launchctl — they drive
// install.Apply directly with a synthesized two-repo group config and
// stub out the OS-service / watcher / hook / MCP machinery.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install"
	"github.com/cajasmota/grafel/internal/install/rulesfiles"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// twoRepoGroup builds a GroupConfig with two empty repo directories on
// disk; returns the absolute paths so individual tests can seed extra
// files into them before invoking install.Apply.
func twoRepoGroup(t *testing.T) (root string, repos []string, cfg *registry.GroupConfig) {
	t.Helper()
	root = t.TempDir()
	for i, slug := range []string{"alpha", "beta"} {
		p := filepath.Join(root, slug)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir repo %d: %v", i, err)
		}
		repos = append(repos, p)
	}
	cfg = &registry.GroupConfig{
		Name: "issue2683",
		Repos: []registry.Repo{
			{Slug: "alpha", Path: repos[0]},
			{Slug: "beta", Path: repos[1]},
		},
	}
	return
}

// applyWithStubs invokes install.Apply with everything stubbed out
// except rules-file writing.
func applyWithStubs(t *testing.T, group string, cfg *registry.GroupConfig) *install.Result {
	t.Helper()
	// Redirect grafel state AND config (HOME/XDG_CONFIG_HOME/GRAFEL_HOME)
	// into a private tmp dir so install.Apply's SaveGroupConfig writes the
	// <group>.fleet.json under the sandbox, never the developer's real
	// ~/.config/grafel (see #5443). A bare HOME+GRAFEL_HOME redirect is NOT
	// enough: ConfigDir() honors XDG_CONFIG_HOME first, which on Linux/Windows
	// CI points at the real home — so isolate via the shared helper.
	testsupport.IsolateHome(t)

	res, err := install.Apply(install.Options{
		Group:        group,
		Config:       cfg,
		BinPath:      "/usr/local/bin/grafel", // fake; never executed
		SkipHooks:    true,
		SkipWatchers: true,
		SkipMCP:      true,
	})
	if err != nil {
		t.Fatalf("install.Apply: %v", err)
	}
	return res
}

// TestIssue2683_FreshInstall_WritesAllSixFiles confirms a brand-new
// repo gets every Target populated with the canonical block.
func TestIssue2683_FreshInstall_WritesAllSixFiles(t *testing.T) {
	_, repos, cfg := twoRepoGroup(t)
	applyWithStubs(t, cfg.Name, cfg)

	for _, repo := range repos {
		for _, target := range rulesfiles.Targets {
			path := filepath.Join(repo, target)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("%s missing: %v", path, err)
				continue
			}
			if !strings.Contains(string(data), rulesfiles.StartMarker) {
				t.Errorf("%s: start marker missing", path)
			}
			if !strings.Contains(string(data), "grafel MCP") {
				t.Errorf("%s: payload missing", path)
			}
		}
	}
}

// TestIssue2683_ReInstall_Idempotent confirms running install twice
// does NOT duplicate the block in any rules file.
func TestIssue2683_ReInstall_Idempotent(t *testing.T) {
	_, repos, cfg := twoRepoGroup(t)
	applyWithStubs(t, cfg.Name, cfg)
	applyWithStubs(t, cfg.Name, cfg)

	for _, repo := range repos {
		for _, target := range rulesfiles.Targets {
			data, err := os.ReadFile(filepath.Join(repo, target))
			if err != nil {
				t.Fatalf("read %s: %v", target, err)
			}
			count := strings.Count(string(data), rulesfiles.StartMarker)
			if count != 1 {
				t.Errorf("%s/%s: expected exactly 1 block, got %d", filepath.Base(repo), target, count)
			}
		}
	}
}

// TestIssue2683_StaleGraphifyOverwritten confirms a pure-stale graphify
// file is overwritten with the canonical block.
func TestIssue2683_StaleGraphifyOverwritten(t *testing.T) {
	_, repos, cfg := twoRepoGroup(t)

	stale := "# Graphify\n\n- Run `graphify update` for fresh data\n- See graphify-out/GRAPH_REPORT.md\n"
	if err := os.WriteFile(filepath.Join(repos[0], ".windsurfrules"), []byte(stale), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res := applyWithStubs(t, cfg.Name, cfg)
	replaced := res.RulesFilesStaleReplaced[repos[0]]
	if len(replaced) == 0 {
		t.Fatalf("expected ReplacedStale for repo %s, got: %+v", repos[0], res.RulesFilesStaleReplaced)
	}

	data, _ := os.ReadFile(filepath.Join(repos[0], ".windsurfrules"))
	if strings.Contains(string(data), "graphify") {
		t.Errorf("graphify content not removed: %s", data)
	}
}

// TestIssue2683_MixedStaleSkippedWithWarning confirms a mixed-content
// file is left alone and surfaced via RulesFilesStaleSkipped.
func TestIssue2683_MixedStaleSkippedWithWarning(t *testing.T) {
	_, repos, cfg := twoRepoGroup(t)

	mixed := "# Engineering Notes\n\n" +
		"This repo owns the billing service and exposes /v2/invoices.\n" +
		"Owners: @platform-team (see CODEOWNERS at the repo root).\n" +
		"Historical: graphify was used here before we migrated.\n" +
		"Refer to docs/architecture.md for the system design overview.\n" +
		"Refer to docs/runbooks for operational procedures and SLOs.\n" +
		"Incidents are tracked in PagerDuty using the billing rota.\n" +
		"Deployment uses ArgoCD with manual sync on main.\n"
	if err := os.WriteFile(filepath.Join(repos[0], ".cursorrules"), []byte(mixed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res := applyWithStubs(t, cfg.Name, cfg)
	skipped := res.RulesFilesStaleSkipped[repos[0]]
	if len(skipped) == 0 {
		t.Fatalf("expected SkippedMixedStale for repo %s, got: %+v", repos[0], res.RulesFilesStaleSkipped)
	}

	data, _ := os.ReadFile(filepath.Join(repos[0], ".cursorrules"))
	if !strings.Contains(string(data), "billing service") {
		t.Errorf("user content lost: %s", data)
	}
	if strings.Contains(string(data), rulesfiles.StartMarker) {
		t.Errorf("block was written to mixed-stale file; should have been skipped")
	}
}

// TestIssue2683_DoctorReportsAllStatuses confirms `grafel doctor`
// reports OK / MISSING / STALE / OUTDATED across the rules files of a
// registered group.
func TestIssue2683_DoctorReportsAllStatuses(t *testing.T) {
	_, repos, cfg := twoRepoGroup(t)

	// Register the group via install.Apply (which also writes initial
	// rules files into both repos as OK).
	applyWithStubs(t, cfg.Name, cfg)

	// Mutate repo[0] so we get one of every status:
	//   AGENTS.md            → OK (untouched)
	//   CLAUDE.md            → OUTDATED (rewrite with v=0 marker)
	//   .windsurfrules       → STALE (replace with graphify content)
	//   .cursorrules         → MISSING (delete)
	if err := os.WriteFile(
		filepath.Join(repos[0], "CLAUDE.md"),
		[]byte("<!-- grafel:mcp-usage:start v=0 -->\nold\n<!-- grafel:mcp-usage:end -->\n"),
		0o644,
	); err != nil {
		t.Fatalf("seed CLAUDE.md: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(repos[0], ".windsurfrules"),
		[]byte("# Graphify\n\nUse `graphify update`\n"),
		0o644,
	); err != nil {
		t.Fatalf("seed .windsurfrules: %v", err)
	}
	if err := os.Remove(filepath.Join(repos[0], ".cursorrules")); err != nil {
		t.Fatalf("remove .cursorrules: %v", err)
	}

	// Doctor refuses to run without an install.json — seed a minimal
	// one so it proceeds past the gate. We only care about the
	// rules-files surface here; other surfaces will report drift but
	// that does not affect this test.
	stateDir := filepath.Join(os.Getenv("HOME"), ".grafel")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	statePath := filepath.Join(stateDir, "install.json")
	if err := install.WriteState(statePath, install.NewState(install.ModeCopy)); err != nil {
		t.Fatalf("seed install.json: %v", err)
	}

	// Run doctor. It will fail several non-rules-files checks (no
	// daemon, no CLI sha, etc.) but the rules-files surface is what we
	// assert on.
	report, err := install.RunDoctor(install.DoctorOptions{StatePath: statePath})
	if err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}

	// Find the rules-files check for repo[0].
	target := "rules-files/" + cfg.Name + "/" + filepath.Base(repos[0])
	var found bool
	for _, c := range report.Checks {
		if c.Surface != target {
			continue
		}
		found = true
		joined := strings.Join(c.Drift, "\n")
		if !strings.Contains(joined, "CLAUDE.md [OUTDATED]") {
			t.Errorf("CLAUDE.md OUTDATED not reported: %s", joined)
		}
		if !strings.Contains(joined, ".windsurfrules [STALE]") {
			t.Errorf(".windsurfrules STALE not reported: %s", joined)
		}
		if !strings.Contains(joined, ".cursorrules [MISSING]") {
			t.Errorf(".cursorrules MISSING not reported: %s", joined)
		}
		// AGENTS.md should NOT appear (it's OK).
		if strings.Contains(joined, "AGENTS.md") {
			t.Errorf("AGENTS.md should not be in drift list (it was OK): %s", joined)
		}
	}
	if !found {
		var surfaces []string
		for _, c := range report.Checks {
			surfaces = append(surfaces, c.Surface)
		}
		t.Fatalf("rules-files check for %s not found; saw: %v", target, surfaces)
	}
}
