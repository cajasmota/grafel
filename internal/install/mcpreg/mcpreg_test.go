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
	if len(got.Args) != 4 || got.Args[0] != "mcp" || got.Args[1] != "serve" || got.Args[3] != "/r/registry.json" {
		t.Fatalf("args: %+v", got.Args)
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
