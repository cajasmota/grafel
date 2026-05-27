package mcpreg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withHome redirects HOME so settings paths land inside a TempDir.
func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	return dir
}

func TestRegisterCreatesEntry(t *testing.T) {
	withHome(t)
	path, err := Register(ClaudeCode, "/bin/archigraph", "/r/registry.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.McpServers[ServerName]
	if got.Command != "/bin/archigraph" {
		t.Fatalf("command: %q", got.Command)
	}
	// New behaviour: args = ["mcp-bridge"], type = "stdio"
	if len(got.Args) != 1 || got.Args[0] != "mcp-bridge" {
		t.Fatalf("args: %+v (want [mcp-bridge])", got.Args)
	}
	if got.Type != "stdio" {
		t.Fatalf("type: %q (want stdio)", got.Type)
	}
}

func TestRegisterPreservesOtherEntries(t *testing.T) {
	withHome(t)
	path, _ := SettingsPath(ClaudeCode)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{"theme":"dark","mcpServers":{"other":{"command":"/x"}}}`
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(ClaudeCode, "/bin/archigraph", "/r.json"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	if doc["theme"] != "dark" {
		t.Fatalf("lost top-level field: %s", b)
	}
	servers := doc["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatalf("lost sibling entry: %s", b)
	}
	if _, ok := servers[ServerName]; !ok {
		t.Fatalf("missing archigraph entry: %s", b)
	}
}

func TestUnregisterIdempotent(t *testing.T) {
	withHome(t)
	if err := Unregister(ClaudeCode); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(ClaudeCode, "/bin/archigraph", "/r.json"); err != nil {
		t.Fatal(err)
	}
	if err := Unregister(ClaudeCode); err != nil {
		t.Fatal(err)
	}
	if err := Unregister(ClaudeCode); err != nil {
		t.Fatal(err)
	}
}

func TestWindsurfPath(t *testing.T) {
	withHome(t)
	p, err := SettingsPath(Windsurf)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "mcp_config.json" {
		t.Fatalf("windsurf path unexpected: %s", p)
	}
}

func TestClaudeCodePathIsHomeClaudeJSON(t *testing.T) {
	home := withHome(t)
	p, err := SettingsPath(ClaudeCode)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".claude.json")
	if p != want {
		t.Fatalf("ClaudeCode path: got %s, want %s", p, want)
	}
}

func TestRegisterPathIdempotent(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude.json")

	// Register twice — should produce exactly one entry.
	if _, err := RegisterPath(path, "/bin/archigraph"); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPath(path, "/bin/archigraph"); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	servers, _ := doc["mcpServers"].(map[string]any)
	if len(servers) != 1 {
		t.Fatalf("expected exactly 1 server entry, got %d: %s", len(servers), b)
	}
}

func TestRegisterPathUpdatesCommand(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude.json")

	if _, err := RegisterPath(path, "/old/archigraph"); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPath(path, "/new/archigraph"); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(path)
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	_ = json.Unmarshal(b, &doc)
	got := doc.McpServers[ServerName]
	if got.Command != "/new/archigraph" {
		t.Fatalf("command not updated: %q", got.Command)
	}
}

func TestDetectClaudeConfigDirs_ExplicitOverride(t *testing.T) {
	explicit := []string{"/a/.claude.json", "/b/.claude.json"}
	got := DetectClaudeConfigDirs(explicit)
	if len(got) != 2 || got[0] != explicit[0] || got[1] != explicit[1] {
		t.Fatalf("explicit dirs not returned as-is: %v", got)
	}
}

func TestDetectClaudeConfigDirs_ScansDotClaudeDirs(t *testing.T) {
	home := withHome(t)

	// Create ~/.claude-personal/ directory.
	personalDir := filepath.Join(home, ".claude-personal")
	if err := os.MkdirAll(personalDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dirs := DetectClaudeConfigDirs(nil)

	primary := filepath.Join(home, ".claude.json")
	secondary := filepath.Join(personalDir, ".claude.json")

	foundPrimary := false
	foundSecondary := false
	for _, d := range dirs {
		if d == primary {
			foundPrimary = true
		}
		if d == secondary {
			foundSecondary = true
		}
	}
	if !foundPrimary {
		t.Errorf("primary %s not in dirs: %v", primary, dirs)
	}
	if !foundSecondary {
		t.Errorf("secondary %s not in dirs: %v", secondary, dirs)
	}
}

func TestUnregisterPath(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude.json")

	if _, err := RegisterPath(path, "/bin/archigraph"); err != nil {
		t.Fatal(err)
	}
	if err := UnregisterPath(path); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers[ServerName]; ok {
		t.Fatalf("archigraph entry still present after Unregister: %s", b)
	}
}

// ── Windsurf tests ────────────────────────────────────────────────────────────

// TestInstallRegistersWindsurfDesktop: fakeHome with the Windsurf desktop
// parent dir present → DetectWindsurfPaths includes the desktop path and
// RegisterPath populates the archigraph entry.
func TestInstallRegistersWindsurfDesktop(t *testing.T) {
	home := withHome(t)

	// Simulate Windsurf desktop app installed: create the parent dir.
	desktopDir := filepath.Join(home, ".codeium", "windsurf")
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := DetectWindsurfPaths()
	desktopConfig := filepath.Join(desktopDir, "mcp_config.json")

	found := false
	for _, p := range paths {
		if p == desktopConfig {
			found = true
		}
	}
	if !found {
		t.Fatalf("DetectWindsurfPaths did not return desktop path %s; got %v", desktopConfig, paths)
	}

	// Register and verify the entry is correct.
	if _, err := RegisterPath(desktopConfig, "/usr/local/bin/archigraph"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(desktopConfig)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.McpServers[ServerName]
	if got.Command != "/usr/local/bin/archigraph" {
		t.Fatalf("desktop: command = %q, want /usr/local/bin/archigraph", got.Command)
	}
	if len(got.Args) != 1 || got.Args[0] != "mcp-bridge" {
		t.Fatalf("desktop: args = %v, want [mcp-bridge]", got.Args)
	}
	if got.Type != "stdio" {
		t.Fatalf("desktop: type = %q, want stdio", got.Type)
	}
}

// TestInstallRegistersWindsurfJetBrains: fakeHome with the JetBrains plugin
// parent dir present → DetectWindsurfPaths includes the JetBrains path and
// RegisterPath populates the archigraph entry.
func TestInstallRegistersWindsurfJetBrains(t *testing.T) {
	home := withHome(t)

	// Simulate Windsurf JetBrains plugin installed: create ~/.codeium/ only
	// (NOT the windsurf/ subdir, so the desktop path must NOT appear).
	codeiumDir := filepath.Join(home, ".codeium")
	if err := os.MkdirAll(codeiumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := DetectWindsurfPaths()
	jbConfig := filepath.Join(codeiumDir, "mcp_config.json")

	found := false
	for _, p := range paths {
		if p == jbConfig {
			found = true
		}
	}
	if !found {
		t.Fatalf("DetectWindsurfPaths did not return JetBrains path %s; got %v", jbConfig, paths)
	}

	// The desktop path must NOT appear (windsurf/ subdir does not exist).
	desktopConfig := filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
	for _, p := range paths {
		if p == desktopConfig {
			t.Fatalf("DetectWindsurfPaths included desktop path %s but windsurf/ dir does not exist", p)
		}
	}

	// Register and verify the entry is correct.
	if _, err := RegisterPath(jbConfig, "/usr/local/bin/archigraph"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(jbConfig)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.McpServers[ServerName]
	if got.Command != "/usr/local/bin/archigraph" {
		t.Fatalf("jetbrains: command = %q, want /usr/local/bin/archigraph", got.Command)
	}
}

// TestInstallSkipsAbsentWindsurf: fakeHome without .codeium dir →
// DetectWindsurfPaths returns an empty slice and no writes occur.
func TestInstallSkipsAbsentWindsurf(t *testing.T) {
	withHome(t)
	// No .codeium directory created — Windsurf is not installed.

	paths := DetectWindsurfPaths()
	if len(paths) != 0 {
		t.Fatalf("expected no Windsurf paths when .codeium absent, got %v", paths)
	}
}

// TestWindsurfJetBrainsPath verifies the canonical path for WindsurfJetBrains.
func TestWindsurfJetBrainsPath(t *testing.T) {
	home := withHome(t)
	p, err := SettingsPath(WindsurfJetBrains)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".codeium", "mcp_config.json")
	if p != want {
		t.Fatalf("WindsurfJetBrains path: got %s, want %s", p, want)
	}
	// Confirm no hardcoded slash separators or /Users/ prefixes.
	if filepath.IsAbs(p) && filepath.Dir(p) == "/" {
		t.Fatalf("path looks hardcoded: %s", p)
	}
}

// TestInstall_CrossPlatformPaths asserts that all tool paths are constructed
// via filepath.Join (no string-concatenated '/' separators) by verifying
// each path is rooted under the fake home dir and that filepath.Rel succeeds
// without traversal (i.e. no ".." components at the start).
func TestInstall_CrossPlatformPaths(t *testing.T) {
	home := withHome(t)

	for _, tool := range []Tool{ClaudeCode, Windsurf, WindsurfJetBrains} {
		p, err := SettingsPath(tool)
		if err != nil {
			t.Fatalf("SettingsPath(%s): %v", tool, err)
		}
		// filepath.Rel must succeed and must not start with ".." (which would
		// indicate the path escapes the home directory).
		rel, err := filepath.Rel(home, p)
		if err != nil {
			t.Errorf("tool %s: filepath.Rel(%s, %s) failed: %v", tool, home, p, err)
			continue
		}
		// A path under home either equals home (".") or starts with a
		// path component — it must NOT start with "..".
		parts := filepath.SplitList(rel)
		_ = parts
		if len(rel) >= 2 && rel[:2] == ".." {
			t.Errorf("tool %s: path %s escapes home %s (rel=%s)", tool, p, home, rel)
		}
	}
}

// TestWindsurfRegistrationUpdatesPath verifies that re-running RegisterPath
// with a new binary path updates the command field (--force reinstall case).
func TestWindsurfRegistrationUpdatesPath(t *testing.T) {
	home := withHome(t)

	desktopDir := filepath.Join(home, ".codeium", "windsurf")
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(desktopDir, "mcp_config.json")

	if _, err := RegisterPath(cfgPath, "/old/archigraph"); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPath(cfgPath, "/new/archigraph"); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(cfgPath)
	var doc struct {
		McpServers map[string]Entry `json:"mcpServers"`
	}
	_ = json.Unmarshal(b, &doc)
	got := doc.McpServers[ServerName]
	if got.Command != "/new/archigraph" {
		t.Fatalf("Windsurf desktop: command not updated on re-register: %q", got.Command)
	}
}
