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
	// GRAFEL_HOME too: nothing in this package resolves it today, but leaving
	// it pointed at the developer's real ~/.grafel is exactly how the two
	// sandbox escapes this cycle happened.
	t.Setenv("GRAFEL_HOME", filepath.Join(home, ".grafel"))
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

	tools := detectWith(nil, nil)

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
	tools := detectWith(last, nil)

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

	tools := detectWith(nil, nil)
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

// ── (#6170) the durable previously-registered signal ────────────────────────

// staleClaudeNoEntry lays down the ratchet fixture: a Claude config that is
// OLDER than RecentWindow and carries NO grafel entry — i.e. the entry was
// removed (the #6168 rollback being the demonstrated way) and the file has not
// been touched since. Returns its path.
func staleClaudeNoEntry(t *testing.T) string {
	t.Helper()
	return writeConfig(t, mcpreg.ClaudeCode, false, nowFunc().Add(-90*24*time.Hour))
}

// TestRatchet_StaleConfigWithLostEntryIsUnchecked pins the BUG: with the entry
// gone, a stale config and no remembered choice, Claude Code arrives in the
// wizard UNCHECKED — the state from which a press-enter run persists the
// removal as the user's preference (#6170). This is the "before" half; the
// repair is TestPreviouslyRegistered_RepairsRatchet.
func TestRatchet_StaleConfigWithLostEntryIsUnchecked(t *testing.T) {
	setupHome(t)
	staleClaudeNoEntry(t)

	if c := find(t, detectWith(nil, nil), "claude"); c.DefaultSelected {
		t.Error("without the previously-registered signal, claude is expected to be unchecked here (that IS the ratchet)")
	}
}

// TestPreviouslyRegistered_RepairsRatchet is the fix: install.json still
// records that grafel registered itself at that exact config path, so the
// wizard must arrive CHECKED even though the entry is gone and the file is
// stale.
func TestPreviouslyRegistered_RepairsRatchet(t *testing.T) {
	setupHome(t)
	path := staleClaudeNoEntry(t)

	tools := detectWith(nil, map[string]bool{path: true})
	if c := find(t, tools, "claude"); !c.DefaultSelected {
		t.Errorf("claude must be default-checked: install.json records grafel was registered at %s; got %+v", path, c)
	}
}

// TestPreviouslyRegistered_UnrelatedPathDoesNotCheck verifies the signal is
// keyed on the tool's OWN config path — a recorded path belonging to some
// other host must not check this tool.
func TestPreviouslyRegistered_UnrelatedPathDoesNotCheck(t *testing.T) {
	setupHome(t)
	staleClaudeNoEntry(t)
	// A stale cursor config too, so cursor is detected but unchecked by B.
	writeConfig(t, mcpreg.Cursor, false, nowFunc().Add(-90*24*time.Hour))

	claudePath, _ := mcpreg.SettingsPath(mcpreg.ClaudeCode)
	tools := detectWith(nil, map[string]bool{claudePath: true})

	if c := find(t, tools, "cursor"); c.DefaultSelected {
		t.Errorf("cursor must stay unchecked: only claude's path is recorded; got %+v", c)
	}
}

// TestPreviouslyRegistered_EmptyOrNilIsInert verifies an absent/empty recorded
// set changes nothing — the (B) default stands on its own.
func TestPreviouslyRegistered_EmptyOrNilIsInert(t *testing.T) {
	setupHome(t)
	staleClaudeNoEntry(t)

	for name, prev := range map[string]map[string]bool{
		"nil":   nil,
		"empty": {},
	} {
		if c := find(t, detectWith(nil, prev), "claude"); c.DefaultSelected {
			t.Errorf("prev=%s: claude should be unchecked (B alone); got %+v", name, c)
		}
	}
}

// TestPreviouslyRegistered_DeliberateOptOutStillWins is THE safety property:
// the (C) remembered choice overrides the new (B2) term exactly as it
// overrides (B). A user who genuinely wants grafel's MCP off must not be
// re-checked by the durable registration record.
func TestPreviouslyRegistered_DeliberateOptOutStillWins(t *testing.T) {
	setupHome(t)
	path := staleClaudeNoEntry(t)

	// Deliberate opt-out for claude, and a RECENT cursor config so the same
	// override is exercised against (B) too.
	writeConfig(t, mcpreg.Cursor, false, nowFunc())
	last := map[string]bool{"claude": false, "cursor": false}

	tools := detectWith(last, map[string]bool{path: true})
	if c := find(t, tools, "claude"); c.DefaultSelected {
		t.Errorf("claude opted out (C) must stay UNCHECKED despite the previously-registered record; got %+v", c)
	}
	if c := find(t, tools, "cursor"); c.DefaultSelected {
		t.Errorf("cursor opted out (C) must stay UNCHECKED despite a recent config (B); got %+v", c)
	}
}

// TestDetectWithPrevious_ReadsLastChoice verifies the exported wrapper still
// consults the on-disk (C) choice, so the override is not bypassed by callers
// that supply a previously-registered set.
func TestDetectWithPrevious_ReadsLastChoice(t *testing.T) {
	setupHome(t)
	// Stale cursor, no grafel entry → B unchecks it; the saved choice checks it.
	writeConfig(t, mcpreg.Cursor, false, nowFunc().Add(-365*24*time.Hour))
	if err := SaveLastChoice([]string{"cursor"}); err != nil {
		t.Fatalf("SaveLastChoice: %v", err)
	}

	if c := find(t, DetectWithPrevious(nil), "cursor"); !c.DefaultSelected {
		t.Errorf("DetectWithPrevious must honour the saved (C) choice; got %+v", c)
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
