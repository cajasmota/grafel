package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/install/mcpreg"
)

// setupHome points HOME at a temp dir so detection reads only files we create,
// and stamps a fixed "now" so the recent-window default is deterministic.
func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = time.Now })
	return home
}

// writeConfig writes a JSON MCP config for the given tool and sets its mtime.
func writeConfig(t *testing.T, tool mcpreg.Tool, hasGrafel bool, mtime time.Time) string {
	t.Helper()
	path, err := mcpreg.SettingsPath(tool)
	if err != nil {
		t.Fatalf("SettingsPath(%s): %v", tool, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{}
	if hasGrafel {
		doc["mcpServers"] = map[string]any{
			mcpreg.ServerName: map[string]any{"command": "grafel", "args": []string{"mcp-bridge"}},
		}
	} else {
		doc["mcpServers"] = map[string]any{"other": map[string]any{"command": "x"}}
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

// find returns the detected Tool with the given ID, or fails.
func find(t *testing.T, tools []Tool, id string) Tool {
	t.Helper()
	for _, tl := range tools {
		if tl.ID == id {
			return tl
		}
	}
	t.Fatalf("tool %q not in detected set %v", id, ids(tools))
	return Tool{}
}

func ids(tools []Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.ID)
	}
	return out
}

// TestSmartDefault_B verifies the (B) default: recently-modified OR has-grafel →
// checked; clearly-stale (no grafel, old mtime) → unchecked.
func TestSmartDefault_B(t *testing.T) {
	setupHome(t)
	now := nowFunc()

	// claude: recent, no grafel → checked (recent).
	writeConfig(t, mcpreg.ClaudeCode, false, now.Add(-2*24*time.Hour))
	// cursor: stale, but HAS grafel → checked (previously configured).
	writeConfig(t, mcpreg.Cursor, true, now.Add(-365*24*time.Hour))
	// windsurf: stale, no grafel → unchecked.
	writeConfig(t, mcpreg.Windsurf, false, now.Add(-90*24*time.Hour))

	tools := detectWith(nil)

	if c := find(t, tools, "claude"); !c.DefaultSelected {
		t.Error("claude (recent) should be default-checked")
	}
	if c := find(t, tools, "cursor"); !c.DefaultSelected || !c.HasGrafel {
		t.Errorf("cursor (has grafel) should be checked + HasGrafel; got %+v", c)
	}
	if c := find(t, tools, "windsurf"); c.DefaultSelected {
		t.Error("windsurf (stale, no grafel) should be default-UNchecked")
	}
}

// TestRememberedChoice_C verifies (C): a saved last-choice overrides the smart
// (B) default for the tools it names.
func TestRememberedChoice_C(t *testing.T) {
	setupHome(t)
	now := nowFunc()

	// claude: recent → B would check it. cursor: stale, no grafel → B unchecks.
	writeConfig(t, mcpreg.ClaudeCode, false, now)
	writeConfig(t, mcpreg.Cursor, false, now.Add(-365*24*time.Hour))

	// Remembered choice: cursor IN, claude OUT — the inverse of B.
	last := map[string]bool{"cursor": true, "claude": false}
	tools := detectWith(last)

	if c := find(t, tools, "claude"); c.DefaultSelected {
		t.Error("claude should be UNchecked: remembered choice (C) overrides recent (B)")
	}
	if c := find(t, tools, "cursor"); !c.DefaultSelected {
		t.Error("cursor should be checked: remembered choice (C) overrides stale (B)")
	}
}

// TestDetect_OnlyDetectedTools verifies tools whose config + parent dir are
// absent are excluded.
func TestDetect_OnlyDetectedTools(t *testing.T) {
	setupHome(t)
	writeConfig(t, mcpreg.ClaudeCode, false, nowFunc())
	// Nothing for cursor/windsurf/etc.

	tools := detectWith(nil)
	if got := ids(tools); len(got) != 1 || got[0] != "claude" {
		t.Errorf("detected = %v, want only [claude]", got)
	}
}

// TestLastChoice_RoundTrip verifies SaveLastChoice / ReadLastChoice persistence
// (C) writes ~/.grafel/mcp-tools.json and reads it back as a set.
func TestLastChoice_RoundTrip(t *testing.T) {
	setupHome(t)

	if got, err := ReadLastChoice(); err != nil || got != nil {
		t.Fatalf("ReadLastChoice on fresh home = (%v, %v), want (nil, nil)", got, err)
	}

	if err := SaveLastChoice([]string{"cursor", "claude"}); err != nil {
		t.Fatalf("SaveLastChoice: %v", err)
	}
	path, _ := LastChoicePath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}

	set, err := ReadLastChoice()
	if err != nil {
		t.Fatalf("ReadLastChoice: %v", err)
	}
	if !set["claude"] || !set["cursor"] || set["windsurf"] {
		t.Errorf("read set = %v, want {claude, cursor}", set)
	}

	// An empty selection ("chose none") must round-trip as a non-nil empty set.
	if err := SaveLastChoice([]string{}); err != nil {
		t.Fatalf("SaveLastChoice(empty): %v", err)
	}
	set, err = ReadLastChoice()
	if err != nil {
		t.Fatalf("ReadLastChoice after empty: %v", err)
	}
	if set == nil || len(set) != 0 {
		t.Errorf("empty choice round-trip = %v, want non-nil empty set", set)
	}
}

// TestDefaultSelection extracts the checked IDs in order.
func TestDefaultSelection(t *testing.T) {
	tools := []Tool{
		{ID: "claude", DefaultSelected: true},
		{ID: "cursor", DefaultSelected: false},
		{ID: "windsurf", DefaultSelected: true},
	}
	got := DefaultSelection(tools)
	if len(got) != 2 || got[0] != "claude" || got[1] != "windsurf" {
		t.Errorf("DefaultSelection = %v, want [claude windsurf]", got)
	}
}
