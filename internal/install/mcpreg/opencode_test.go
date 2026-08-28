package mcpreg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── opencode tests (#6730) ────────────────────────────────────────────────────
//
// opencode's config is `.json`, so without a dedicated arm it would fall into
// the generic JSON writer and be written wrong in four ways at once: the
// top-level key would be `mcpServers` rather than `mcp`, `command` would be a
// string with a sibling `args` array rather than a single argv array, and
// `type` would be "stdio" rather than "local".
//
// Since opencode v1.18.16 unknown top-level fields are IGNORED rather than
// failing config parsing, so every one of those mistakes writes successfully,
// passes any "did the file get written" check, and simply never loads the
// server. That is why these tests decode and assert the exact shape.

// opencodePath returns the user-global opencode config path under the
// isolated home, creating its parent directory (so the host reads as
// "installed").
func opencodePath(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "opencode.json")
}

// decodeOpencode reads path and returns the raw decoded document. Assertions
// are made on the parsed structure, never on a substring of the file: a string
// match would pass on `"mcpServers"` too, which is precisely the bug.
func decodeOpencode(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("opencode config is not valid JSON: %v\n%s", err, b)
	}
	return doc
}

// TestOpencode_SettingsPathIsXDG pins the user-global path.
func TestOpencode_SettingsPathIsXDG(t *testing.T) {
	home := withHome(t)
	got, err := SettingsPath(Opencode)
	if err != nil {
		t.Fatalf("SettingsPath(Opencode): %v", err)
	}
	want := filepath.Join(home, ".config", "opencode", "opencode.json")
	if got != want {
		t.Fatalf("SettingsPath(Opencode) = %q, want %q", got, want)
	}
}

// TestOpencode_WritesExactSchemaShape is the load-bearing test for this arm.
// It asserts every axis of the opencode McpLocalConfig schema independently:
// the container key, the absence of the JSON-world key, the `type` enum value,
// `command` being a two-element argv array, and the absence of `args`.
func TestOpencode_WritesExactSchemaShape(t *testing.T) {
	home := withHome(t)
	path := opencodePath(t, home)

	if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	doc := decodeOpencode(t, path)

	if _, bad := doc["mcpServers"]; bad {
		t.Fatalf("opencode config contains the JSON-world key \"mcpServers\"; "+
			"opencode ignores unknown top-level fields, so this would silently "+
			"never load. doc = %v", doc)
	}
	mcp, ok := doc["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("top-level \"mcp\" object missing; doc = %v", doc)
	}
	entry, ok := mcp[ServerName].(map[string]any)
	if !ok {
		t.Fatalf("mcp.%s missing or not an object; mcp = %v", ServerName, mcp)
	}

	if got := entry["type"]; got != "local" {
		t.Fatalf("mcp.%s.type = %v, want \"local\" (the schema's only local enum value)", ServerName, got)
	}
	if _, bad := entry["args"]; bad {
		t.Fatalf("mcp.%s has an \"args\" key; McpLocalConfig sets additionalProperties:false "+
			"and has no args — argv belongs in command. entry = %v", ServerName, entry)
	}
	cmd, ok := entry["command"].([]any)
	if !ok {
		t.Fatalf("mcp.%s.command = %#v, want a JSON array (opencode's command is argv, not a string)",
			ServerName, entry["command"])
	}
	if len(cmd) != 2 || cmd[0] != "/usr/local/bin/grafel" || cmd[1] != "mcp-bridge" {
		t.Fatalf("mcp.%s.command = %v, want [/usr/local/bin/grafel mcp-bridge]", ServerName, cmd)
	}
	// Only the two required keys; `enabled` is deliberately omitted (it
	// defaults true) to keep the merge minimal.
	if len(entry) != 2 {
		t.Fatalf("mcp.%s has %d keys (%v), want exactly type+command", ServerName, len(entry), entry)
	}

	// HasGrafelEntry must learn the `mcp` key or mcptools/doctor reports a
	// false negative on a correctly-registered opencode.
	if !HasGrafelEntry(path) {
		t.Fatalf("HasGrafelEntry = false for a correctly-registered opencode config")
	}
}

// TestOpencode_JSONCRoundTripPreservesComments: opencode's docs actively
// encourage comments and trailing commas. encoding/json errors on both, and
// MarshalIndent would silently destroy the comments even if the read
// succeeded. A user's comments must survive a register/unregister round trip.
func TestOpencode_JSONCRoundTripPreservesComments(t *testing.T) {
	home := withHome(t)
	path := opencodePath(t, home)

	seed := `{
  // my opencode config
  "$schema": "https://opencode.ai/config.json",
  /* block comment about the model */
  "model": "anthropic/claude-opus-4",
  "mcp": {
    // a server I already had
    "other": {
      "type": "local",
      "command": ["/bin/other"],
    },
  },
}
`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
		t.Fatalf("RegisterPath on a JSONC config: %v", err)
	}

	afterRegister, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, comment := range []string{
		"// my opencode config",
		"/* block comment about the model */",
		"// a server I already had",
	} {
		if !strings.Contains(string(afterRegister), comment) {
			t.Fatalf("register destroyed the user's comment %q:\n%s", comment, afterRegister)
		}
	}

	if err := UnregisterPath(path); err != nil {
		t.Fatalf("UnregisterPath on a JSONC config: %v", err)
	}
	afterUnregister, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, comment := range []string{
		"// my opencode config",
		"/* block comment about the model */",
		"// a server I already had",
	} {
		if !strings.Contains(string(afterUnregister), comment) {
			t.Fatalf("unregister destroyed the user's comment %q:\n%s", comment, afterUnregister)
		}
	}
}

// TestOpencode_PreservesSiblings: an existing unrelated MCP server and
// unrelated top-level keys survive registration untouched.
func TestOpencode_PreservesSiblings(t *testing.T) {
	home := withHome(t)
	path := opencodePath(t, home)

	seed := `{"model":"anthropic/claude-opus-4","theme":"tokyonight",` +
		`"mcp":{"other":{"type":"local","command":["/bin/other","serve"]}}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	doc := decodeOpencode(t, path)
	if doc["model"] != "anthropic/claude-opus-4" {
		t.Fatalf("register clobbered unrelated key model: %v", doc["model"])
	}
	if doc["theme"] != "tokyonight" {
		t.Fatalf("register clobbered unrelated key theme: %v", doc["theme"])
	}
	mcp, _ := doc["mcp"].(map[string]any)
	other, ok := mcp["other"].(map[string]any)
	if !ok {
		t.Fatalf("register dropped the foreign server; mcp = %v", mcp)
	}
	cmd, _ := other["command"].([]any)
	if len(cmd) != 2 || cmd[0] != "/bin/other" || cmd[1] != "serve" {
		t.Fatalf("register mutated the foreign server's command: %v", other)
	}
	if _, ok := mcp[ServerName]; !ok {
		t.Fatalf("register did not add mcp.%s; mcp = %v", ServerName, mcp)
	}
}

// TestOpencode_UnregisterRemovesOnlyGrafel mirrors the mcpServers behaviour:
// siblings survive, and the container key is dropped entirely when grafel was
// its last member.
func TestOpencode_UnregisterRemovesOnlyGrafel(t *testing.T) {
	home := withHome(t)

	t.Run("keeps siblings", func(t *testing.T) {
		path := opencodePath(t, home)
		seed := `{"theme":"tokyonight","mcp":{"other":{"type":"local","command":["/bin/other"]}}}`
		if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
			t.Fatal(err)
		}
		if err := UnregisterPath(path); err != nil {
			t.Fatal(err)
		}
		doc := decodeOpencode(t, path)
		mcp, ok := doc["mcp"].(map[string]any)
		if !ok {
			t.Fatalf("unregister dropped mcp even though a sibling remained: %v", doc)
		}
		if _, still := mcp[ServerName]; still {
			t.Fatalf("unregister left mcp.%s behind: %v", ServerName, mcp)
		}
		if _, ok := mcp["other"]; !ok {
			t.Fatalf("unregister removed the foreign server: %v", mcp)
		}
		if doc["theme"] != "tokyonight" {
			t.Fatalf("unregister clobbered unrelated key theme: %v", doc["theme"])
		}
		if HasGrafelEntry(path) {
			t.Fatalf("HasGrafelEntry = true after unregister")
		}
	})

	t.Run("drops empty mcp", func(t *testing.T) {
		dir := filepath.Join(home, ".config", "opencode2")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "opencode.json")
		if err := os.WriteFile(path, []byte(`{"theme":"tokyonight"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
			t.Fatal(err)
		}
		if err := UnregisterPath(path); err != nil {
			t.Fatal(err)
		}
		doc := decodeOpencode(t, path)
		if _, still := doc["mcp"]; still {
			t.Fatalf("unregister left an orphan empty mcp object: %v", doc)
		}
		if doc["theme"] != "tokyonight" {
			t.Fatalf("unregister clobbered unrelated key theme: %v", doc["theme"])
		}
	})
}

// TestOpencode_UnregisterIsIdempotent: a missing file and a file with no
// grafel entry are both no-ops (the uninstall loop only has a recorded path).
func TestOpencode_UnregisterIsIdempotent(t *testing.T) {
	home := withHome(t)
	path := opencodePath(t, home)

	if err := UnregisterPath(path); err != nil {
		t.Fatalf("UnregisterPath on absent file: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("UnregisterPath created %s out of nothing", path)
	}

	seed := `{"mcp":{"other":{"type":"local","command":["/bin/other"]}}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UnregisterPath(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "other") {
		t.Fatalf("UnregisterPath damaged a config with no grafel entry: %s", b)
	}
	if HasGrafelEntry(path) {
		t.Fatalf("HasGrafelEntry = true for a config with only a foreign server")
	}
}

// TestOpencode_RegisterIsIdempotent: re-registering does not duplicate the
// entry or accumulate keys.
func TestOpencode_RegisterIsIdempotent(t *testing.T) {
	home := withHome(t)
	path := opencodePath(t, home)

	if _, err := RegisterPath(path, "/old/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPath(path, "/usr/local/bin/grafel"); err != nil {
		t.Fatal(err)
	}
	doc := decodeOpencode(t, path)
	mcp, _ := doc["mcp"].(map[string]any)
	if len(mcp) != 1 {
		t.Fatalf("re-register produced %d servers, want 1: %v", len(mcp), mcp)
	}
	entry, _ := mcp[ServerName].(map[string]any)
	cmd, _ := entry["command"].([]any)
	if len(cmd) != 2 || cmd[0] != "/usr/local/bin/grafel" {
		t.Fatalf("re-register did not update command: %v", cmd)
	}
}

// TestInstall_SkipsAbsentOpencode: no ~/.config/opencode dir → detection is
// empty (mirrors TestInstall_SkipsAbsentAntigravity).
func TestInstall_SkipsAbsentOpencode(t *testing.T) {
	withHome(t)
	if targets := DetectOpencodePaths(); len(targets) != 0 {
		t.Fatalf("expected no opencode paths when dir absent, got %v", targets)
	}
}

// TestInstall_DetectsOpencode: ~/.config/opencode present → the user-global
// opencode.json is returned and matches SettingsPath.
func TestInstall_DetectsOpencode(t *testing.T) {
	home := withHome(t)
	want := opencodePath(t, home)

	targets := DetectOpencodePaths()
	if len(targets) != 1 || targets[0].Path != want {
		t.Fatalf("DetectOpencodePaths = %v, want [{%s}]", targets, want)
	}
	sp, err := SettingsPath(Opencode)
	if err != nil {
		t.Fatal(err)
	}
	if sp != want {
		t.Fatalf("SettingsPath(Opencode) = %q, want %q", sp, want)
	}
}
