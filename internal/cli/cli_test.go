package cli

import (
	"bytes"
	"encoding/json"
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

// TestRegisterMCPInClaudeConfigs verifies that registerMCPInClaudeConfigs
// correctly writes archigraph MCP entries to detected Claude config files.
// This tests the fix for issue #841: `archigraph install` must write
// mcpServers.archigraph to ~/.claude.json (and any ~/.claude-*/.claude.json).
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
	binPath := "/usr/local/bin/archigraph"

	// Call the MCP registration function.
	out := &bytes.Buffer{}
	registered := registerMCPInClaudeConfigs(out, binPath, []string{claudeDir, claudePersonalJSON})

	// Verify it reports both directories as registered.
	if len(registered) != 2 {
		t.Fatalf("expected 2 registered dirs, got %d: %v", len(registered), registered)
	}

	// Verify that the primary Claude config was created and contains the archigraph entry.
	primaryContent, err := os.ReadFile(claudeDir)
	if err != nil {
		t.Fatalf("failed to read primary config: %v", err)
	}

	var primaryDoc map[string]interface{}
	if err := json.Unmarshal(primaryContent, &primaryDoc); err != nil {
		t.Fatalf("primary config not valid JSON: %v", err)
	}

	// Check for mcpServers.archigraph entry.
	servers, ok := primaryDoc["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcpServers not found or not a map in primary config: %+v", primaryDoc)
	}

	archigraphEntry, ok := servers["archigraph"].(map[string]interface{})
	if !ok {
		t.Fatalf("archigraph MCP entry not found in primary config: %+v", servers)
	}

	// Verify the entry structure: command, args:["mcp-bridge"], type:"stdio"
	if archigraphEntry["command"] != binPath {
		t.Fatalf("command not set correctly: got %q, want %q", archigraphEntry["command"], binPath)
	}

	args, ok := archigraphEntry["args"].([]interface{})
	if !ok || len(args) != 1 || args[0] != "mcp-bridge" {
		t.Fatalf("args not set correctly (want [mcp-bridge]): %+v", archigraphEntry["args"])
	}

	if archigraphEntry["type"] != "stdio" {
		t.Fatalf("type not set to 'stdio': %+v", archigraphEntry["type"])
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

	if _, ok := secondaryServers["archigraph"]; !ok {
		t.Fatalf("archigraph entry not found in secondary config: %+v", secondaryServers)
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
// registerMCPInClaudeConfigs twice doesn't duplicate the archigraph entry.
func TestRegisterMCPInClaudeConfigsIdempotent(t *testing.T) {
	home := withSandboxHome(t)
	claudeDir := filepath.Join(home, ".claude.json")
	binPath := "/usr/local/bin/archigraph"

	// Register twice.
	out1 := &bytes.Buffer{}
	registerMCPInClaudeConfigs(out1, binPath, []string{claudeDir})

	out2 := &bytes.Buffer{}
	registerMCPInClaudeConfigs(out2, binPath, []string{claudeDir})

	// Verify the config has exactly one archigraph entry.
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

	if _, ok := servers["archigraph"]; !ok {
		t.Fatalf("archigraph entry missing after idempotent re-registration: %+v", servers)
	}
}

// TestUnregisterMCPFromClaudeConfigs verifies that unregisterMCPFromClaudeConfigs
// correctly removes archigraph MCP entries from Claude config files.
func TestUnregisterMCPFromClaudeConfigs(t *testing.T) {
	home := withSandboxHome(t)

	// Create mock Claude config directories with existing MCP entries.
	claudeDir := filepath.Join(home, ".claude.json")
	claudePersonalDir := filepath.Join(home, ".claude-personal")
	if err := os.MkdirAll(claudePersonalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	claudePersonalJSON := filepath.Join(claudePersonalDir, ".claude.json")

	// Pre-populate configs with archigraph MCP entries.
	binPath := "/usr/local/bin/archigraph"
	registerOut := &bytes.Buffer{}
	registerMCPInClaudeConfigs(registerOut, binPath, []string{claudeDir, claudePersonalJSON})

	// Verify both configs have the entry before unregistering.
	content, _ := os.ReadFile(claudeDir)
	var doc map[string]interface{}
	json.Unmarshal(content, &doc)
	servers, _ := doc["mcpServers"].(map[string]interface{})
	if _, ok := servers["archigraph"]; !ok {
		t.Fatalf("archigraph entry not found before unregister")
	}

	// Call unregister.
	unregOut := &bytes.Buffer{}
	removed := unregisterMCPFromClaudeConfigs(unregOut, []string{claudeDir, claudePersonalJSON})

	// Verify it reports both directories as unregistered.
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed dirs, got %d: %v", len(removed), removed)
	}

	// Verify both configs no longer have the archigraph entry.
	primaryContent, _ := os.ReadFile(claudeDir)
	var primaryDoc map[string]interface{}
	json.Unmarshal(primaryContent, &primaryDoc)
	primaryServers, _ := primaryDoc["mcpServers"].(map[string]interface{})
	if _, ok := primaryServers["archigraph"]; ok {
		t.Fatalf("archigraph entry still present in primary config after unregister")
	}

	secondaryContent, _ := os.ReadFile(claudePersonalJSON)
	var secondaryDoc map[string]interface{}
	json.Unmarshal(secondaryContent, &secondaryDoc)
	secondaryServers, _ := secondaryDoc["mcpServers"].(map[string]interface{})
	if _, ok := secondaryServers["archigraph"]; ok {
		t.Fatalf("archigraph entry still present in secondary config after unregister")
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
	registerMCPInClaudeConfigs(registerOut, "/usr/local/bin/archigraph", []string{claudeDir})

	// Verify the entry exists.
	content, _ := os.ReadFile(claudeDir)
	var doc map[string]interface{}
	json.Unmarshal(content, &doc)
	servers, _ := doc["mcpServers"].(map[string]interface{})
	if _, ok := servers["archigraph"]; !ok {
		t.Fatalf("archigraph entry not found before unregister")
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
	if _, ok := servers2["archigraph"]; ok {
		t.Fatalf("archigraph entry still present after first unregister")
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
