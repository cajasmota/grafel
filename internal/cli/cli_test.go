package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/install/skilllink"
	"github.com/cajasmota/grafel/internal/registry"
)

// withSandboxHome redirects every path the CLI might write to into a
// per-test TempDir so concurrent tests can't collide.
func withSandboxHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(dir, ".grafel"))
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
		if _, err := os.Stat(filepath.Join(p, ".grafel/group.json")); err != nil {
			t.Fatalf("manifest missing in %s", p)
		}
	}
}

// TestWizardCuratedReposExcludesSiblings is the regression for #5295: a
// curated --repos set registers EXACTLY the listed repos and never their
// siblings under a shared parent.
func TestWizardCuratedReposExcludesSiblings(t *testing.T) {
	home := withSandboxHome(t)
	parent := filepath.Join(home, "fleet")
	repoA := filepath.Join(parent, "alpha")
	repoB := filepath.Join(parent, "beta")
	repoC := filepath.Join(parent, "gamma") // sibling that must NOT be picked up
	makeRepo(t, repoA)
	makeRepo(t, repoB)
	makeRepo(t, repoC)

	out := &bytes.Buffer{}
	err := runWizard(out, wizardOptions{
		NonInteractive: true,
		GroupName:      "curated",
		ReposCSV:       repoA,
		Repos:          []string{repoB}, // mixing --repos and --repo
		ParentDir:      parent,          // present but must be ignored
		Watchers:       false,
		GitHooks:       false,
		RunInstall:     false,
	})
	if err != nil {
		t.Fatalf("wizard: %v\n%s", err, out.String())
	}
	groups, err := registry.Groups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups: %+v", groups)
	}
	cfg, err := registry.LoadGroupConfig(groups[0].ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	gotSlugs := map[string]bool{}
	for _, r := range cfg.Repos {
		gotSlugs[r.Slug] = true
	}
	if len(gotSlugs) != 2 || !gotSlugs["alpha"] || !gotSlugs["beta"] {
		t.Fatalf("expected exactly {alpha,beta}, got %v (cfg %+v)", gotSlugs, cfg.Repos)
	}
	if gotSlugs["gamma"] {
		t.Fatalf("over-discovery: sibling gamma was registered")
	}
}

// TestWizardCuratedRejectsBadPaths ensures non-git / missing paths are
// warned+skipped and an all-invalid set is a hard error, not a silent
// empty group.
func TestWizardCuratedRejectsBadPaths(t *testing.T) {
	home := withSandboxHome(t)
	good := filepath.Join(home, "repos", "good")
	makeRepo(t, good)
	notGit := filepath.Join(home, "repos", "plain") // dir, no .git
	if err := os.MkdirAll(notGit, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(home, "repos", "nope")

	// Mixed: good kept, others warned+skipped.
	out := &bytes.Buffer{}
	err := runWizard(out, wizardOptions{
		NonInteractive: true,
		GroupName:      "mixed",
		Repos:          []string{good, notGit, missing},
		RunInstall:     false,
	})
	if err != nil {
		t.Fatalf("wizard: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "warning: skipping") {
		t.Fatalf("expected skip warnings, got: %s", out.String())
	}
	groups, _ := registry.Groups()
	cfg, err := registry.LoadGroupConfig(groups[0].ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0].Slug != "good" {
		t.Fatalf("expected only good, got %+v", cfg.Repos)
	}

	// All-invalid: hard error.
	out2 := &bytes.Buffer{}
	err = runWizard(out2, wizardOptions{
		NonInteractive: true,
		GroupName:      "empty",
		Repos:          []string{notGit, missing},
		RunInstall:     false,
	})
	if err == nil {
		t.Fatalf("expected error for all-invalid curated set, got nil\n%s", out2.String())
	}
}

// TestWizardExcludePrunesParent verifies --exclude removes matching subdirs
// from --parent auto-discovery while leaving the rest intact.
func TestWizardExcludePrunesParent(t *testing.T) {
	home := withSandboxHome(t)
	parent := filepath.Join(home, "fleet")
	keep := filepath.Join(parent, "keepme")
	skip := filepath.Join(parent, "vendor")
	makeRepo(t, keep)
	makeRepo(t, skip)

	out := &bytes.Buffer{}
	err := runWizard(out, wizardOptions{
		NonInteractive: true,
		GroupName:      "pruned",
		ParentDir:      parent,
		Excludes:       []string{"vendor"},
		RunInstall:     false,
	})
	if err != nil {
		t.Fatalf("wizard: %v\n%s", err, out.String())
	}
	groups, _ := registry.Groups()
	cfg, err := registry.LoadGroupConfig(groups[0].ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0].Slug != "keepme" {
		t.Fatalf("expected only keepme after --exclude vendor, got %+v", cfg.Repos)
	}
}

// TestWizardParentBackCompat confirms plain --parent still discovers every
// git subdir (no flags = no behavior change).
func TestWizardParentBackCompat(t *testing.T) {
	home := withSandboxHome(t)
	parent := filepath.Join(home, "fleet")
	makeRepo(t, filepath.Join(parent, "a"))
	makeRepo(t, filepath.Join(parent, "b"))
	if err := os.MkdirAll(filepath.Join(parent, "notarepo"), 0o755); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	err := runWizard(out, wizardOptions{
		NonInteractive: true,
		GroupName:      "discovered",
		ParentDir:      parent,
		RunInstall:     false,
	})
	if err != nil {
		t.Fatalf("wizard: %v\n%s", err, out.String())
	}
	groups, _ := registry.Groups()
	cfg, err := registry.LoadGroupConfig(groups[0].ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("expected 2 discovered git repos, got %+v", cfg.Repos)
	}
}

func TestDoctorRunsCleanly(t *testing.T) {
	home := withSandboxHome(t)
	repo := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repo)
	cfg := &registry.GroupConfig{Name: "demo"}
	cfg.Features.GitHooks = true
	cfg.Repos = []registry.Repo{{Slug: "alpha", Path: repo, Stack: registry.StackList{"go"}}}
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
		cfg.Repos = []registry.Repo{{Slug: name, Path: repo, Stack: registry.StackList{"go"}}}
		p, _ := registry.ConfigPathFor(name)
		if err := registry.SaveGroupConfig(p, cfg); err != nil {
			t.Fatal(err)
		}
		if err := registry.AddGroup(name, p); err != nil {
			t.Fatal(err)
		}
	}
	out := &bytes.Buffer{}
	if err := runStatus(out, "alpha", "", false); err != nil {
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
	cfg.Repos = []registry.Repo{{Slug: "test", Path: repo, Stack: registry.StackList{"go"}}}
	cfgPath, _ := registry.ConfigPathFor("demo")
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup("demo", cfgPath); err != nil {
		t.Fatal(err)
	}

	// #1626: per-repo state lives in the external store (resolved by
	// daemon.StateDirForRepo under the sandbox GRAFEL_HOME), not in-repo.
	grafelDir := daemon.StateDirForRepo(repo)

	// Test 1: No graph files exist
	out := &bytes.Buffer{}
	if err := runStatus(out, "demo", "", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "indexed (never)") {
		t.Errorf("status should show 'indexed (never)' when no files exist: %s", out.String())
	}

	// Test 2: Only graph.json exists
	if err := os.MkdirAll(grafelDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(grafelDir, "graph.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	out = &bytes.Buffer{}
	if err := runStatus(out, "demo", "", false); err != nil {
		t.Fatal(err)
	}
	statusText := out.String()
	if !strings.Contains(statusText, "indexed") || strings.Contains(statusText, "(never)") {
		t.Errorf("status should show 'indexed' with timestamp when json exists: %s", statusText)
	}
	if strings.Contains(statusText, "graph.json:") {
		t.Errorf("status should not show 'graph.json:' label (issue #822): %s", statusText)
	}

	// Test 3: graph.fb exists (the main #822 fix)
	if err := os.Remove(filepath.Join(grafelDir, "graph.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(grafelDir, "graph.fb"), []byte("fb"), 0644); err != nil {
		t.Fatal(err)
	}

	out = &bytes.Buffer{}
	if err := runStatus(out, "demo", "", false); err != nil {
		t.Fatal(err)
	}
	statusText = out.String()
	if !strings.Contains(statusText, "indexed") || strings.Contains(statusText, "(never)") {
		t.Errorf("status should show 'indexed' with timestamp when fb exists (fix for #822): %s", statusText)
	}
	if strings.Contains(statusText, "graph.json:") {
		t.Errorf("status should not show 'graph.json:' label when only fb exists: %s", statusText)
	}

	// Test 4: Both graph.fb and graph.json exist
	if err := os.WriteFile(filepath.Join(grafelDir, "graph.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	out = &bytes.Buffer{}
	if err := runStatus(out, "demo", "", false); err != nil {
		t.Fatal(err)
	}
	statusText = out.String()
	if !strings.Contains(statusText, "indexed") || strings.Contains(statusText, "(never)") {
		t.Errorf("status should show 'indexed' with timestamp when both files exist: %s", statusText)
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

// TestRegisterMCPInClaudeConfigs verifies that registerMCPInClaudeConfigs
// correctly writes grafel MCP entries to detected Claude config files.
// This tests the fix for issue #841: `grafel install` must write
// mcpServers.grafel to ~/.claude.json (and any ~/.claude-*/.claude.json).
func TestRegisterMCPInClaudeConfigs(t *testing.T) {
	home := withSandboxHome(t)

	// Create mock Claude config directories: primary and secondary.
	claudeDir := filepath.Join(home, ".claude.json")
	claudePersonalDir := filepath.Join(home, ".claude-personal")
	if err := os.MkdirAll(claudePersonalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	claudePersonalJSON := filepath.Join(claudePersonalDir, ".claude.json")

	// Create a fake binary path for testing.
	binPath := "/usr/local/bin/grafel"

	// Call the MCP registration function.
	out := &bytes.Buffer{}
	registered := registerMCPInClaudeConfigs(out, binPath, []string{claudeDir, claudePersonalJSON})

	// Verify it reports both directories as registered.
	if len(registered) != 2 {
		t.Fatalf("expected 2 registered dirs, got %d: %v", len(registered), registered)
	}

	// Verify that the primary Claude config was created and contains the grafel entry.
	primaryContent, err := os.ReadFile(claudeDir)
	if err != nil {
		t.Fatalf("failed to read primary config: %v", err)
	}

	var primaryDoc map[string]interface{}
	if err := json.Unmarshal(primaryContent, &primaryDoc); err != nil {
		t.Fatalf("primary config not valid JSON: %v", err)
	}

	// Check for mcpServers.grafel entry.
	servers, ok := primaryDoc["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcpServers not found or not a map in primary config: %+v", primaryDoc)
	}

	grafelEntry, ok := servers["grafel"].(map[string]interface{})
	if !ok {
		t.Fatalf("grafel MCP entry not found in primary config: %+v", servers)
	}

	// Verify the entry structure: command, args:["mcp-bridge"], type:"stdio"
	if grafelEntry["command"] != binPath {
		t.Fatalf("command not set correctly: got %q, want %q", grafelEntry["command"], binPath)
	}

	args, ok := grafelEntry["args"].([]interface{})
	if !ok || len(args) != 1 || args[0] != "mcp-bridge" {
		t.Fatalf("args not set correctly (want [mcp-bridge]): %+v", grafelEntry["args"])
	}

	if grafelEntry["type"] != "stdio" {
		t.Fatalf("type not set to 'stdio': %+v", grafelEntry["type"])
	}

	// Verify that the secondary Claude config was also updated.
	secondaryContent, err := os.ReadFile(claudePersonalJSON)
	if err != nil {
		t.Fatalf("failed to read secondary config: %v", err)
	}

	var secondaryDoc map[string]interface{}
	if err := json.Unmarshal(secondaryContent, &secondaryDoc); err != nil {
		t.Fatalf("secondary config not valid JSON: %v", err)
	}

	secondaryServers, ok := secondaryDoc["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcpServers not found in secondary config: %+v", secondaryDoc)
	}

	if _, ok := secondaryServers["grafel"]; !ok {
		t.Fatalf("grafel entry not found in secondary config: %+v", secondaryServers)
	}

	// Verify the output messages.
	output := out.String()
	if !strings.Contains(output, "MCP registered in:") {
		t.Fatalf("output missing 'MCP registered in:' message: %s", output)
	}
	if !strings.Contains(output, "Restart Claude Code to load") {
		t.Fatalf("output missing 'Restart Claude Code' message: %s", output)
	}
}

// TestRegisterMCPInClaudeConfigsIdempotent verifies that calling
// registerMCPInClaudeConfigs twice doesn't duplicate the grafel entry.
func TestRegisterMCPInClaudeConfigsIdempotent(t *testing.T) {
	home := withSandboxHome(t)
	claudeDir := filepath.Join(home, ".claude.json")
	binPath := "/usr/local/bin/grafel"

	// Register twice.
	out1 := &bytes.Buffer{}
	registerMCPInClaudeConfigs(out1, binPath, []string{claudeDir})

	out2 := &bytes.Buffer{}
	registerMCPInClaudeConfigs(out2, binPath, []string{claudeDir})

	// Verify the config has exactly one grafel entry.
	content, err := os.ReadFile(claudeDir)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(content, &doc); err != nil {
		t.Fatalf("config not valid JSON: %v", err)
	}

	servers, _ := doc["mcpServers"].(map[string]interface{})
	if len(servers) != 1 {
		t.Fatalf("expected exactly 1 MCP server entry after 2 registrations, got %d: %+v", len(servers), servers)
	}

	if _, ok := servers["grafel"]; !ok {
		t.Fatalf("grafel entry missing after idempotent re-registration: %+v", servers)
	}
}

// TestUnregisterMCPFromClaudeConfigs verifies that unregisterMCPFromClaudeConfigs
// correctly removes grafel MCP entries from Claude config files.
func TestUnregisterMCPFromClaudeConfigs(t *testing.T) {
	home := withSandboxHome(t)

	// Create mock Claude config directories with existing MCP entries.
	claudeDir := filepath.Join(home, ".claude.json")
	claudePersonalDir := filepath.Join(home, ".claude-personal")
	if err := os.MkdirAll(claudePersonalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	claudePersonalJSON := filepath.Join(claudePersonalDir, ".claude.json")

	// Pre-populate configs with grafel MCP entries.
	binPath := "/usr/local/bin/grafel"
	registerOut := &bytes.Buffer{}
	registerMCPInClaudeConfigs(registerOut, binPath, []string{claudeDir, claudePersonalJSON})

	// Verify both configs have the entry before unregistering.
	content, _ := os.ReadFile(claudeDir)
	var doc map[string]interface{}
	json.Unmarshal(content, &doc)
	servers, _ := doc["mcpServers"].(map[string]interface{})
	if _, ok := servers["grafel"]; !ok {
		t.Fatalf("grafel entry not found before unregister")
	}

	// Call unregister.
	unregOut := &bytes.Buffer{}
	removed := unregisterMCPFromClaudeConfigs(unregOut, []string{claudeDir, claudePersonalJSON})

	// Verify it reports both directories as unregistered.
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed dirs, got %d: %v", len(removed), removed)
	}

	// Verify both configs no longer have the grafel entry.
	primaryContent, _ := os.ReadFile(claudeDir)
	var primaryDoc map[string]interface{}
	json.Unmarshal(primaryContent, &primaryDoc)
	primaryServers, _ := primaryDoc["mcpServers"].(map[string]interface{})
	if _, ok := primaryServers["grafel"]; ok {
		t.Fatalf("grafel entry still present in primary config after unregister")
	}

	secondaryContent, _ := os.ReadFile(claudePersonalJSON)
	var secondaryDoc map[string]interface{}
	json.Unmarshal(secondaryContent, &secondaryDoc)
	secondaryServers, _ := secondaryDoc["mcpServers"].(map[string]interface{})
	if _, ok := secondaryServers["grafel"]; ok {
		t.Fatalf("grafel entry still present in secondary config after unregister")
	}

	// Verify the output messages.
	output := unregOut.String()
	if !strings.Contains(output, "MCP removed from:") {
		t.Fatalf("output missing 'MCP removed from:' message: %s", output)
	}
}

// TestUnregisterMCPFromClaudeConfigsIdempotent verifies that calling
// unregisterMCPFromClaudeConfigs twice is safe and doesn't error.
func TestUnregisterMCPFromClaudeConfigsIdempotent(t *testing.T) {
	home := withSandboxHome(t)
	claudeDir := filepath.Join(home, ".claude.json")

	// Register first, then unregister twice.
	registerOut := &bytes.Buffer{}
	registerMCPInClaudeConfigs(registerOut, "/usr/local/bin/grafel", []string{claudeDir})

	// Verify the entry exists.
	content, _ := os.ReadFile(claudeDir)
	var doc map[string]interface{}
	json.Unmarshal(content, &doc)
	servers, _ := doc["mcpServers"].(map[string]interface{})
	if _, ok := servers["grafel"]; !ok {
		t.Fatalf("grafel entry not found before unregister")
	}

	// First unregister.
	unregOut1 := &bytes.Buffer{}
	removed1 := unregisterMCPFromClaudeConfigs(unregOut1, []string{claudeDir})
	if len(removed1) != 1 {
		t.Fatalf("expected 1 removed dir on first unregister, got %d", len(removed1))
	}

	// Verify the entry is gone.
	content, _ = os.ReadFile(claudeDir)
	var doc2 map[string]interface{}
	json.Unmarshal(content, &doc2)
	servers2, _ := doc2["mcpServers"].(map[string]interface{})
	if _, ok := servers2["grafel"]; ok {
		t.Fatalf("grafel entry still present after first unregister")
	}

	// Second unregister on same config (file exists but no entry).
	// UnregisterPath treats this as a no-op (returns nil), so it's counted as "removed".
	unregOut2 := &bytes.Buffer{}
	removed2 := unregisterMCPFromClaudeConfigs(unregOut2, []string{claudeDir})

	// Second unregister should still report success (idempotent).
	if len(removed2) != 1 {
		t.Fatalf("expected 1 removed dir on second unregister (idempotent), got %d", len(removed2))
	}

	// Verify no output is printed on second unregister (no successful removals in the message sense).
	// Actually, UnregisterPath succeeds silently, so it will be reported.
	// This is correct behavior - the system says "MCP removed from: ..." even if it was already gone.
}

// TestInstallSkillsInClaudeConfigs verifies that installSkillsInClaudeConfigs
// correctly symlinks the 6 grafel skills into detected Claude config
// directories' skills/ subdirectories. This tests the fix for issue #824:
// after `grafel install`, users should be able to invoke /grafel-quality-check
// and other skills directly in Claude Code.
func TestInstallSkillsInClaudeConfigs(t *testing.T) {
	home := withSandboxHome(t)

	// Create a SEPARATE directory for source skills (not inside home).
	// This avoids confusion where source and destination would be in the same tree.
	srcRoot := t.TempDir()
	skillsDir := filepath.Join(srcRoot, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Use the live canonical slice so adding a new skill never breaks this test.
	skillNames := skilllink.SkillNames
	for _, skillName := range skillNames {
		skillPath := filepath.Join(skillsDir, skillName)
		if err := os.MkdirAll(skillPath, 0o755); err != nil {
			t.Fatal(err)
		}
		// Write a marker file to verify symlink resolution.
		if err := os.WriteFile(filepath.Join(skillPath, "skill.yaml"), []byte("name: "+skillName), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create mock Claude config directories inside home.
	claudeDir := filepath.Join(home, ".claude.json")
	claudePersonalDir := filepath.Join(home, ".claude-personal")
	if err := os.MkdirAll(claudePersonalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	claudePersonalJSON := filepath.Join(claudePersonalDir, ".claude.json")

	// Call installSkillsInClaudeConfigs with explicit skillsSourceDir.
	out := &bytes.Buffer{}
	installed := installSkillsInClaudeConfigs(out, "/fake/bin/grafel", skillsDir, []string{claudeDir, claudePersonalJSON})

	// Verify both directories reported as installed.
	if len(installed) != 2 {
		t.Fatalf("expected 2 installed dirs, got %d: %v", len(installed), installed)
	}

	// Verify symlinks in primary Claude config (HOME/.claude.json → HOME/.claude/skills).
	primarySkillsDir := skilllink.ClaudeSkillsDirForConfig(claudeDir)
	for _, skillName := range skillNames {
		skillPath := filepath.Join(primarySkillsDir, skillName)
		info, err := os.Lstat(skillPath)
		if err != nil {
			t.Fatalf("symlink not created for %s: %v", skillName, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink", skillName)
		}

		// Verify symlink target.
		target, err := os.Readlink(skillPath)
		if err != nil {
			t.Fatalf("failed to read symlink %s: %v", skillName, err)
		}
		expectedTarget := filepath.Join(skillsDir, skillName)
		if target != expectedTarget {
			t.Errorf("symlink target mismatch for %s: expected %q, got %q", skillName, expectedTarget, target)
		}
	}

	// Verify symlinks in secondary Claude config (sidecar layout).
	secondarySkillsDir := skilllink.ClaudeSkillsDirForConfig(claudePersonalJSON)
	for _, skillName := range skillNames {
		skillPath := filepath.Join(secondarySkillsDir, skillName)
		info, err := os.Lstat(skillPath)
		if err != nil {
			t.Fatalf("symlink not created for %s in secondary config: %v", skillName, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink in secondary config", skillName)
		}
	}

	// Verify output reports the success.
	outStr := out.String()
	if !strings.Contains(outStr, "Skills linked in:") {
		t.Fatalf("output missing 'Skills linked in:' message: %s", outStr)
	}
}

// TestInstallSkillsIdempotent verifies that running installSkillsInClaudeConfigs
// twice on the same config is safe and doesn't error or duplicate symlinks.
func TestInstallSkillsIdempotent(t *testing.T) {
	home := withSandboxHome(t)

	// Create source skills directory in a separate temp dir.
	srcRoot := t.TempDir()
	skillsDir := filepath.Join(srcRoot, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Use the live canonical slice so adding a new skill never breaks this test.
	skillNames := skilllink.SkillNames
	for _, skillName := range skillNames {
		if err := os.MkdirAll(filepath.Join(skillsDir, skillName), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create Claude config directory in home.
	claudeDir := filepath.Join(home, ".claude.json")

	// First install.
	out1 := &bytes.Buffer{}
	installed1 := installSkillsInClaudeConfigs(out1, "/fake/bin", skillsDir, []string{claudeDir})
	if len(installed1) != 1 {
		t.Fatalf("first install: expected 1 dir, got %d", len(installed1))
	}

	// Re-run install (should be idempotent).
	out2 := &bytes.Buffer{}
	installed2 := installSkillsInClaudeConfigs(out2, "/fake/bin", skillsDir, []string{claudeDir})
	if len(installed2) != 1 {
		t.Fatalf("second install: expected 1 dir, got %d", len(installed2))
	}

	// Verify symlinks still exist and point correctly.
	skillsSubdir := skilllink.ClaudeSkillsDirForConfig(claudeDir)
	for _, skillName := range skillNames {
		skillPath := filepath.Join(skillsSubdir, skillName)
		target, err := os.Readlink(skillPath)
		if err != nil {
			t.Fatalf("failed to read symlink %s after re-install: %v", skillName, err)
		}
		expectedTarget := filepath.Join(skillsDir, skillName)
		if target != expectedTarget {
			t.Errorf("symlink target mismatch after re-install: expected %q, got %q", expectedTarget, target)
		}
	}

	// Both installs should report success.
	if !strings.Contains(out1.String(), "Skills linked in:") {
		t.Errorf("first install didn't report success: %s", out1.String())
	}
	if !strings.Contains(out2.String(), "Skills linked in:") {
		t.Errorf("second install didn't report success: %s", out2.String())
	}
}

// TestRemoveSkillsFromClaudeConfigs verifies that removeSkillsFromClaudeConfigs
// correctly removes symlinked skills from Claude config directories.
func TestRemoveSkillsFromClaudeConfigs(t *testing.T) {
	home := withSandboxHome(t)

	// Create source skills directory in a separate temp dir.
	srcRoot := t.TempDir()
	skillsDir := filepath.Join(srcRoot, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Use the live canonical slice so adding a new skill never breaks this test.
	skillNames := skilllink.SkillNames
	for _, skillName := range skillNames {
		if err := os.MkdirAll(filepath.Join(skillsDir, skillName), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create Claude config directory in home and install skills.
	claudeDir := filepath.Join(home, ".claude.json")
	installOut := &bytes.Buffer{}
	installSkillsInClaudeConfigs(installOut, "/fake/bin", skillsDir, []string{claudeDir})

	// Verify skills are installed.
	skillsSubdir := skilllink.ClaudeSkillsDirForConfig(claudeDir)
	for _, skillName := range skillNames {
		_, err := os.Lstat(filepath.Join(skillsSubdir, skillName))
		if err != nil {
			t.Fatalf("skill symlink missing before removal: %v", err)
		}
	}

	// Now remove the skills.
	removeOut := &bytes.Buffer{}
	removed := removeSkillsFromClaudeConfigs(removeOut, []string{claudeDir})

	// Verify removal was reported.
	if len(removed) != 1 {
		t.Fatalf("expected 1 removed dir, got %d", len(removed))
	}

	// Verify symlinks were removed.
	for _, skillName := range skillNames {
		_, err := os.Lstat(filepath.Join(skillsSubdir, skillName))
		if !os.IsNotExist(err) {
			t.Fatalf("symlink not removed for %s", skillName)
		}
	}

	// Verify output mentions removal.
	if !strings.Contains(removeOut.String(), "Skills removed from:") {
		t.Errorf("should report removal: %s", removeOut.String())
	}
}
