package install

// pertool_test.go — per-enabled-tool doctor checks + multi-tool uninstall MCP
// sweep (#5258). These tests drive the injectable hooks on DoctorOptions /
// UninstallOptions so they never read a real registry or touch live config
// files; every path is under a t.TempDir HOME.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/install/rulesfiles"
	"github.com/cajasmota/grafel/internal/registry"
)

// fakeGroups returns a groupsFn/loadGroupFn pair backed by an in-memory config.
func fakeGroups(cfg *registry.GroupConfig) (
	func() ([]registry.GroupRef, error),
	func(string) (*registry.GroupConfig, error),
) {
	groupsFn := func() ([]registry.GroupRef, error) {
		return []registry.GroupRef{{Name: cfg.Name, ConfigPath: "/fake/" + cfg.Name + ".json"}}, nil
	}
	loadFn := func(string) (*registry.GroupConfig, error) { return cfg, nil }
	return groupsFn, loadFn
}

func findCheck(checks []CheckResult, surface string) (CheckResult, bool) {
	for _, c := range checks {
		if c.Surface == surface {
			return c, true
		}
	}
	return CheckResult{}, false
}

// writeJSONMCP writes a config file with grafel plus a foreign server entry.
func writeJSONMCP(t *testing.T, path string, withGrafel bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	servers := map[string]any{
		"other": map[string]any{"command": "/bin/other", "args": []string{"x"}},
	}
	if withGrafel {
		servers[mcpreg.ServerName] = map[string]any{"command": "/bin/grafel", "args": []string{"mcp-bridge"}}
	}
	doc := map[string]any{"mcpServers": servers}
	b, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCheckEnabledTools_PerToolStatus verifies doctor emits one row per
// (group, enabled-tool) and reports OK when a tool's MCP entry + rules file are
// present, missing when the MCP entry is absent, and "not wired" when the
// tool's config file does not exist.
func TestCheckEnabledTools_PerToolStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write the rules block to all targets so rules checks pass for every tool.
	if _, err := rulesfiles.WriteAll(repo, rulesfiles.WriteOptions{GroupName: "g1"}); err != nil {
		t.Fatalf("WriteAll rules: %v", err)
	}

	// claude: MCP entry present in ~/.claude.json (OK).
	claudePath, _ := mcpreg.SettingsPath(mcpreg.ClaudeCode)
	writeJSONMCP(t, claudePath, true)
	// cursor: config file present but NO grafel entry → "mcp entry absent".
	cursorPath, _ := mcpreg.SettingsPath(mcpreg.Cursor)
	writeJSONMCP(t, cursorPath, false)
	// codex: config file ABSENT entirely → "mcp not wired".

	cfg := &registry.GroupConfig{
		Name:  "g1",
		Repos: []registry.Repo{{Path: repo}},
		Tools: []string{"claude", "cursor", "codex"},
	}
	groupsFn, loadFn := fakeGroups(cfg)

	checks := checkEnabledTools(DoctorOptions{groupsFn: groupsFn, loadGroupFn: loadFn})

	claudeRow, ok := findCheck(checks, "tool/g1/claude")
	if !ok {
		t.Fatalf("missing claude row; got %d checks", len(checks))
	}
	if !claudeRow.OK {
		t.Errorf("claude row should be OK, drift=%v", claudeRow.Drift)
	}

	cursorRow, ok := findCheck(checks, "tool/g1/cursor")
	if !ok {
		t.Fatal("missing cursor row")
	}
	if cursorRow.OK || cursorRow.Severity != SeverityWarning {
		t.Errorf("cursor row should be Warning+not-OK, got OK=%v sev=%v", cursorRow.OK, cursorRow.Severity)
	}
	if !strings.Contains(strings.Join(cursorRow.Drift, " "), "mcp entry absent") {
		t.Errorf("cursor drift should mention 'mcp entry absent', got %v", cursorRow.Drift)
	}

	codexRow, ok := findCheck(checks, "tool/g1/codex")
	if !ok {
		t.Fatal("missing codex row")
	}
	if codexRow.OK {
		t.Error("codex row should be not-OK (config absent)")
	}
	if !strings.Contains(strings.Join(codexRow.Drift, " "), "not wired") {
		t.Errorf("codex drift should mention 'not wired', got %v", codexRow.Drift)
	}
}

// TestCheckEnabledTools_RulesDrift verifies a missing rules file for an enabled
// tool is reported on that tool's row.
func TestCheckEnabledTools_RulesDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	// Do NOT write any rules files → cursor's .cursorrules will be MISSING.
	cursorPath, _ := mcpreg.SettingsPath(mcpreg.Cursor)
	writeJSONMCP(t, cursorPath, true) // MCP fine; only rules should drift.

	cfg := &registry.GroupConfig{
		Name:  "g1",
		Repos: []registry.Repo{{Path: repo}},
		Tools: []string{"cursor"},
	}
	groupsFn, loadFn := fakeGroups(cfg)
	checks := checkEnabledTools(DoctorOptions{groupsFn: groupsFn, loadGroupFn: loadFn})

	row, ok := findCheck(checks, "tool/g1/cursor")
	if !ok {
		t.Fatal("missing cursor row")
	}
	if row.OK {
		t.Error("cursor row should report rules drift")
	}
	if !strings.Contains(strings.Join(row.Drift, " "), ".cursorrules") {
		t.Errorf("expected .cursorrules drift, got %v", row.Drift)
	}
}

// TestCheckEnabledTools_NoGroups verifies an empty registry yields no rows.
func TestCheckEnabledTools_NoGroups(t *testing.T) {
	groupsFn := func() ([]registry.GroupRef, error) { return nil, nil }
	loadFn := func(string) (*registry.GroupConfig, error) { return nil, nil }
	if got := checkEnabledTools(DoctorOptions{groupsFn: groupsFn, loadGroupFn: loadFn}); got != nil {
		t.Errorf("expected nil checks for no groups, got %v", got)
	}
}

// TestUninstall_SweepsAllEnabledToolsMCP verifies uninstall removes grafel's
// MCP entry from EVERY enabled tool's own config (JSON: cursor/windsurf/kiro;
// TOML: codex) while preserving each foreign entry, and leaves no grafel entry.
func TestUninstall_SweepsAllEnabledToolsMCP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	binPath := filepath.Join(home, "grafel")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Register grafel into each tool's real config file (JSON + Codex TOML),
	// alongside a pre-existing foreign entry, via the real mcpreg writers.
	jsonTools := []mcpreg.Tool{mcpreg.Cursor, mcpreg.Windsurf, mcpreg.Kiro}
	for _, tool := range jsonTools {
		p, _ := mcpreg.SettingsPath(tool)
		writeJSONMCP(t, p, false) // seed a foreign "other" entry only
		if _, err := mcpreg.Register(tool, binPath, ""); err != nil {
			t.Fatalf("Register %s: %v", tool, err)
		}
	}
	// Codex TOML with a foreign table + grafel.
	codexPath, _ := mcpreg.SettingsPath(mcpreg.Codex)
	if err := os.MkdirAll(filepath.Dir(codexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte("[mcp_servers.other]\ncommand = \"/bin/other\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mcpreg.Register(mcpreg.Codex, binPath, ""); err != nil {
		t.Fatalf("Register codex: %v", err)
	}

	// Minimal install.json so RunUninstall proceeds past the state read.
	statePath := filepath.Join(home, ".grafel", "install.json")
	st := NewState(ModeCopy)
	st.CLI = CLIRecord{Path: binPath}
	if err := WriteState(statePath, st); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	cfg := &registry.GroupConfig{
		Name:  "g1",
		Tools: []string{"cursor", "windsurf", "kiro", "codex"},
	}
	groupsFn, loadFn := fakeGroups(cfg)

	res, err := RunUninstall(UninstallOptions{
		StatePath:      statePath,
		SkipDaemonStop: true,
		Yes:            true,
		groupsFn:       groupsFn,
		loadGroupFn:    loadFn,
	})
	if err != nil {
		t.Fatalf("RunUninstall: %v", err)
	}

	// All four tools should be in the sweep result.
	if len(res.MCPToolsDeregistered) != 4 {
		t.Errorf("expected 4 tools deregistered, got %v", res.MCPToolsDeregistered)
	}

	// JSON tools: grafel gone, foreign "other" preserved.
	for _, tool := range jsonTools {
		p, _ := mcpreg.SettingsPath(tool)
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("read %s: %v", tool, rerr)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("parse %s: %v", tool, err)
		}
		servers, _ := doc["mcpServers"].(map[string]any)
		if servers == nil {
			t.Fatalf("%s: mcpServers vanished (foreign entry lost)", tool)
		}
		if _, ok := servers[mcpreg.ServerName]; ok {
			t.Errorf("%s: grafel entry still present after uninstall", tool)
		}
		if _, ok := servers["other"]; !ok {
			t.Errorf("%s: foreign 'other' entry was removed (should be preserved)", tool)
		}
	}

	// Codex TOML: grafel table gone, foreign table preserved.
	tomlData, rerr := os.ReadFile(codexPath)
	if rerr != nil {
		t.Fatalf("read codex toml: %v", rerr)
	}
	tomlStr := string(tomlData)
	if strings.Contains(tomlStr, "[mcp_servers.grafel]") {
		t.Errorf("codex: grafel table still present:\n%s", tomlStr)
	}
	if !strings.Contains(tomlStr, "[mcp_servers.other]") {
		t.Errorf("codex: foreign table removed (should be preserved):\n%s", tomlStr)
	}
}

// TestUninstall_NoEnabledMCPTools verifies the sweep is a no-op (and does not
// error) when no enabled tool supports MCP.
func TestUninstall_NoEnabledMCPTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	statePath := filepath.Join(home, ".grafel", "install.json")
	if err := WriteState(statePath, NewState(ModeCopy)); err != nil {
		t.Fatal(err)
	}
	cfg := &registry.GroupConfig{Name: "g1", Tools: []string{"copilot"}} // rules-only
	groupsFn, loadFn := fakeGroups(cfg)

	res, err := RunUninstall(UninstallOptions{
		StatePath:      statePath,
		SkipDaemonStop: true,
		Yes:            true,
		groupsFn:       groupsFn,
		loadGroupFn:    loadFn,
	})
	if err != nil {
		t.Fatalf("RunUninstall: %v", err)
	}
	if len(res.MCPToolsDeregistered) != 0 {
		t.Errorf("expected no tools deregistered, got %v", res.MCPToolsDeregistered)
	}
}
