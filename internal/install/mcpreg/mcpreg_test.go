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
