package mcpreg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jsonHostCases covers the JSON-shaped MCP hosts whose grafel entries are
// added in #5254 (Cursor) plus the pre-existing Windsurf. Codex is TOML and
// covered separately in toml_test.go.
var jsonHostCases = []Tool{Cursor, Windsurf}

func TestJSONHosts_RegisterPreservesForeignEntry(t *testing.T) {
	for _, tool := range jsonHostCases {
		t.Run(string(tool), func(t *testing.T) {
			withHome(t)
			path, err := SettingsPath(tool)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(path, ".toml") {
				t.Fatalf("%s should be JSON, got %q", tool, path)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			pre := `{"someSetting":true,"mcpServers":{"other":{"command":"/x","args":["serve"]}}}`
			if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Register(tool, "/bin/grafel", "/r.json"); err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			b, _ := os.ReadFile(path)
			if err := json.Unmarshal(b, &doc); err != nil {
				t.Fatalf("invalid JSON after register: %v\n%s", err, b)
			}
			if doc["someSetting"] != true {
				t.Fatalf("lost top-level key: %s", b)
			}
			servers, _ := doc["mcpServers"].(map[string]any)
			if _, ok := servers["other"]; !ok {
				t.Fatalf("lost foreign server: %s", b)
			}
			g, ok := servers[ServerName].(map[string]any)
			if !ok {
				t.Fatalf("grafel entry missing: %s", b)
			}
			if g["command"] != "/bin/grafel" {
				t.Fatalf("grafel command wrong: %s", b)
			}
		})
	}
}

func TestJSONHosts_UnregisterRemovesOnlyGrafel(t *testing.T) {
	for _, tool := range jsonHostCases {
		t.Run(string(tool), func(t *testing.T) {
			withHome(t)
			path, _ := SettingsPath(tool)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			pre := `{"someSetting":true,"mcpServers":{"other":{"command":"/x"}}}`
			if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Register(tool, "/bin/grafel", "/r.json"); err != nil {
				t.Fatal(err)
			}
			if err := Unregister(tool); err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			b, _ := os.ReadFile(path)
			_ = json.Unmarshal(b, &doc)
			if doc["someSetting"] != true {
				t.Fatalf("lost top-level key after unregister: %s", b)
			}
			servers, _ := doc["mcpServers"].(map[string]any)
			if _, ok := servers["other"]; !ok {
				t.Fatalf("lost foreign server after unregister: %s", b)
			}
			if _, ok := servers[ServerName]; ok {
				t.Fatalf("grafel entry not removed: %s", b)
			}
		})
	}
}
