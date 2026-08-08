package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// setupHome points HOME at a temp dir so detection reads only files we create,
// and stamps a fixed "now" so the recent-window default is deterministic.
func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// USERPROFILE too: os.UserHomeDir reads it on Windows, and mcpreg's
	// SettingsPath falls back to os.UserHomeDir when HOME is unset.
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	// GRAFEL_HOME too: nothing in this package resolves it today, but leaving
	// it pointed at the developer's real ~/.grafel is exactly how the two
	// sandbox escapes this cycle happened.
	t.Setenv("GRAFEL_HOME", filepath.Join(home, ".grafel"))
	// Fail fast if any of that failed to take effect: these tests WRITE
	// ~/.grafel/mcp-tools.json via SaveLastChoice.
	testsupport.GuardRealHome(t)
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

	// An empty selection ("chose none") must round-trip as a non-nil set in
	// which every offered tool is explicitly FALSE (#6191). Claude's config
	// path is $HOME/.claude.json, so its parent dir — the temp HOME — always
	// exists and claude is always among the offered tools here.
	if err := SaveLastChoice([]string{}); err != nil {
		t.Fatalf("SaveLastChoice(empty): %v", err)
	}
	set, err = ReadLastChoice()
	if err != nil {
		t.Fatalf("ReadLastChoice after empty: %v", err)
	}
	if set == nil {
		t.Fatal("empty choice round-trip = nil, want a non-nil set")
	}
	for id, sel := range set {
		if sel {
			t.Errorf("empty choice round-trip has %q selected; want every entry false", id)
		}
	}
	if sel, ok := set["claude"]; !ok || sel {
		t.Errorf(`empty choice round-trip: set["claude"] = (%v, ok=%v), want (false, ok=true)`, sel, ok)
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

// TestPreviouslyRegistered_OptOutViaRealRoundTrip is THE safety property, and
// it is asserted through the ONLY API that can record an opt-out in
// production: SaveLastChoice writes the file, DetectWithPrevious reads it back.
//
// A hand-built map{"claude": false} would certify nothing about the file: it
// asserts a map shape rather than anything SaveLastChoice produces. Going
// through the real writer is what makes the assertion about production
// behaviour. (Before #6191 that map shape was not even reachable — the file
// stored only `selected` — which is the sharper form of the same objection.)
func TestPreviouslyRegistered_OptOutViaRealRoundTrip(t *testing.T) {
	setupHome(t)
	path := staleClaudeNoEntry(t)
	writeConfig(t, mcpreg.Cursor, false, nowFunc())

	// The user unchecked claude and kept cursor. This is exactly what the
	// dashboard wizard persists via registerWizardMCP → SaveLastChoice.
	if err := SaveLastChoice([]string{"cursor"}); err != nil {
		t.Fatalf("SaveLastChoice: %v", err)
	}

	tools := DetectWithPrevious(map[string]bool{path: true})
	if c := find(t, tools, "claude"); c.DefaultSelected {
		t.Errorf("claude was deliberately unchecked and the choice was SAVED; the install.json record must not re-check it. got %+v", c)
	}
	if c := find(t, tools, "cursor"); !c.DefaultSelected {
		t.Errorf("cursor was chosen and saved; it must stay checked. got %+v", c)
	}
}

// TestPreviouslyRegistered_SuppressedByAnyRecordedChoice pins the bound on B2:
// it repairs an accident only while NO choice has been recorded. Once a
// last-choice file exists — even one that says "none" — the recorded decision
// owns the default and the durable registration record is inert.
func TestPreviouslyRegistered_SuppressedByAnyRecordedChoice(t *testing.T) {
	setupHome(t)
	path := staleClaudeNoEntry(t)
	prev := map[string]bool{path: true}

	// No file yet → B2 repairs.
	if c := find(t, DetectWithPrevious(prev), "claude"); !c.DefaultSelected {
		t.Fatalf("precondition: with no recorded choice B2 must check claude; got %+v", c)
	}

	// The user chose "none" — an explicit decision, not an accident.
	if err := SaveLastChoice([]string{}); err != nil {
		t.Fatalf("SaveLastChoice(none): %v", err)
	}
	if c := find(t, DetectWithPrevious(prev), "claude"); c.DefaultSelected {
		t.Errorf("a recorded choice of NONE must suppress B2; got %+v", c)
	}
}

// TestPreviouslyRegistered_SuppressedForAToolTheChoiceDoesNotName is the bound
// on its own. Since #6191 a saved choice NAMES the tools it declined, so the
// sibling above would now hold on the strength of the (C) override alone even
// if the bound were deleted. The bound's remaining job is the residual case
// (C) says nothing about: a tool that was not installed when the choice was
// made, and so appears in neither list.
func TestPreviouslyRegistered_SuppressedForAToolTheChoiceDoesNotName(t *testing.T) {
	setupHome(t)
	writeConfig(t, mcpreg.ClaudeCode, false, nowFunc())

	// The choice is made while cursor is NOT installed, so it names neither.
	if err := SaveLastChoice([]string{"claude"}); err != nil {
		t.Fatalf("SaveLastChoice: %v", err)
	}
	set, err := ReadLastChoice()
	if err != nil {
		t.Fatalf("ReadLastChoice: %v", err)
	}
	if _, ok := set["cursor"]; ok {
		t.Fatalf("precondition: the saved choice must not name cursor; got %v", set)
	}

	// Cursor turns up later: stale, entry gone, but recorded in install.json.
	cursorPath := writeConfig(t, mcpreg.Cursor, false, nowFunc().Add(-90*24*time.Hour))

	tools := DetectWithPrevious(map[string]bool{cursorPath: true})
	if c := find(t, tools, "cursor"); c.DefaultSelected {
		t.Errorf("a choice has been recorded, so B2 is suppressed even for a tool that choice does not name; got %+v", c)
	}
}

// writeRawLastChoice writes arbitrary bytes to ~/.grafel/mcp-tools.json,
// bypassing SaveLastChoice, so a malformed document can be staged.
func writeRawLastChoice(t *testing.T, content []byte, mode os.FileMode) string {
	t.Helper()
	path, err := LastChoicePath()
	if err != nil {
		t.Fatalf("LastChoicePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) }) // so TempDir cleanup can unlink
	return path
}

// TestPreviouslyRegistered_MalformedChoiceFileSuppressesB2 closes the bound's
// fail-open leak: B2's whole premise is that a nil lastChoice faithfully means
// "no choice was ever recorded". A file that EXISTS but cannot be parsed or
// read is evidence a choice WAS recorded — the same rule mcpprev.go already
// applies to an unreadable group config. Treating it as first-run would let B2
// re-check a box the user cleared, which is the very regression the bound was
// introduced to prevent.
func TestPreviouslyRegistered_MalformedChoiceFileSuppressesB2(t *testing.T) {
	cases := map[string]struct {
		content []byte
		mode    os.FileMode
	}{
		"truncated JSON": {[]byte(`{"selected":["curs`), 0o600},
		"zero bytes":     {[]byte(``), 0o600},
		"unreadable":     {[]byte(`{"selected":["cursor"]}`), 0o000},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			setupHome(t)
			path := staleClaudeNoEntry(t)
			writeRawLastChoice(t, tc.content, tc.mode)

			tools := DetectWithPrevious(map[string]bool{path: true})
			if c := find(t, tools, "claude"); c.DefaultSelected {
				t.Errorf("a %s choice file must suppress B2 — the file existing is evidence a choice was recorded; got %+v", name, c)
			}
		})
	}
}

// TestReadLastChoice_CorruptIsARecordedChoice pins the representation the bound
// depends on: a corrupt document yields a NON-NIL empty set, so it is
// distinguishable from the genuine no-file case (nil). Both still leave the
// (C) override naming nothing, so the (B) default is unchanged.
func TestReadLastChoice_CorruptIsARecordedChoice(t *testing.T) {
	setupHome(t)
	writeRawLastChoice(t, []byte(`{not json`), 0o600)

	set, err := ReadLastChoice()
	if err != nil {
		t.Fatalf("a corrupt file must not error out of the wizard: %v", err)
	}
	if set == nil {
		t.Error("corrupt file must yield a NON-NIL empty set (a choice exists but is unreadable), not nil (no choice)")
	}
	if len(set) != 0 {
		t.Errorf("corrupt file must name no tools; got %v", set)
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

// ── (#6191) the last-choice file must be able to say "off" ──────────────────

// readRawLastChoice decodes ~/.grafel/mcp-tools.json as the on-disk document,
// so a test can assert what was actually written rather than what the reader
// makes of it.
func readRawLastChoice(t *testing.T) lastChoiceFile {
	t.Helper()
	path, err := LastChoicePath()
	if err != nil {
		t.Fatalf("LastChoicePath: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc lastChoiceFile
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return doc
}

// has reports whether ids contains id.
func has(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// TestOptOut_SurvivesRecentConfig is THE reported defect (#6191), routed
// through the only API that records a choice in production. Claude Code's
// config is rewritten constantly, so `recent` — and with it the (B) default —
// is essentially always true for it. Unchecking it and saving therefore has to
// beat a live (B) signal, which is exactly what the old file shape could not
// express: an unchecked tool was merely ABSENT, and detectWith's `ok` guard
// never fired.
func TestOptOut_SurvivesRecentConfig(t *testing.T) {
	setupHome(t)
	now := nowFunc()

	// Both recent → (B) would check BOTH.
	writeConfig(t, mcpreg.ClaudeCode, false, now)
	writeConfig(t, mcpreg.Cursor, false, now)

	// The user unchecked claude and kept cursor.
	if err := SaveLastChoice([]string{"cursor"}); err != nil {
		t.Fatalf("SaveLastChoice: %v", err)
	}

	tools := DetectWithPrevious(nil)
	if c := find(t, tools, "claude"); c.DefaultSelected {
		t.Errorf("claude was deliberately unchecked and saved; a recent mtime must not re-check it. got %+v", c)
	}
	if c := find(t, tools, "cursor"); !c.DefaultSelected {
		t.Errorf("cursor was chosen and saved; it must stay checked. got %+v", c)
	}
}

// TestOptOut_RecordedOnDisk pins the WRITE half independently: SaveLastChoice
// must persist the tools that were on offer and left unchecked, not only the
// chosen ones. Asserted against the file, so it dies if the writer stops
// recording deselections even when the reader still understands them.
func TestOptOut_RecordedOnDisk(t *testing.T) {
	setupHome(t)
	now := nowFunc()
	writeConfig(t, mcpreg.ClaudeCode, false, now)
	writeConfig(t, mcpreg.Cursor, false, now)

	if err := SaveLastChoice([]string{"cursor"}); err != nil {
		t.Fatalf("SaveLastChoice: %v", err)
	}

	doc := readRawLastChoice(t)
	if !has(doc.Selected, "cursor") {
		t.Errorf("selected = %v, want it to contain cursor", doc.Selected)
	}
	if !has(doc.Deselected, "claude") {
		t.Errorf("deselected = %v, want it to contain claude (offered and left unchecked)", doc.Deselected)
	}
	if has(doc.Deselected, "cursor") {
		t.Errorf("deselected = %v, must not contain the CHOSEN tool", doc.Deselected)
	}
}

// TestReadLastChoice_DeselectedYieldsFalse pins the READ half independently,
// from a hand-written document, so it dies if the reader stops honouring the
// deselected list even while the writer still emits it.
func TestReadLastChoice_DeselectedYieldsFalse(t *testing.T) {
	setupHome(t)
	writeRawLastChoice(t, []byte(`{"selected":["cursor"],"deselected":["claude"]}`), 0o600)

	set, err := ReadLastChoice()
	if err != nil {
		t.Fatalf("ReadLastChoice: %v", err)
	}
	sel, ok := set["claude"]
	if !ok {
		t.Fatal(`set["claude"] missing: a deselected tool must be PRESENT and false, not absent`)
	}
	if sel {
		t.Error(`set["claude"] = true, want false`)
	}
	if !set["cursor"] {
		t.Errorf("set = %v, want cursor true", set)
	}
}

// TestReadLastChoice_SelectedWinsOverDeselected pins the ORDER in which the two
// lists are applied. A document naming a tool in both is contradictory and
// should not depend on map-iteration luck: the affirmative wins, so a reader
// that applies `selected` first and lets `deselected` overwrite it is caught.
func TestReadLastChoice_SelectedWinsOverDeselected(t *testing.T) {
	setupHome(t)
	writeRawLastChoice(t, []byte(`{"selected":["claude"],"deselected":["claude"]}`), 0o600)

	set, err := ReadLastChoice()
	if err != nil {
		t.Fatalf("ReadLastChoice: %v", err)
	}
	if !set["claude"] {
		t.Errorf(`set["claude"] = %v; a tool named in BOTH lists must read as selected`, set["claude"])
	}
}

// TestOptOut_DoesNotStrandAnUnofferedTool is the counter-ratchet the issue
// warned about. A deselection records only what was actually OFFERED at the
// time — a tool not installed then is in neither list, so when it appears
// later the (B) default decides and it is checked, exactly as on a first run.
//
// Only that half is asserted here, so this test dies for one reason: the
// deselected set being computed over every registered adapter instead of the
// detected ones.
func TestOptOut_DoesNotStrandAnUnofferedTool(t *testing.T) {
	setupHome(t)
	now := nowFunc()

	// Offered at save time: claude (chosen) and windsurf (left unchecked).
	// Cursor is NOT installed — ~/.cursor does not exist.
	writeConfig(t, mcpreg.ClaudeCode, false, now)
	writeConfig(t, mcpreg.Windsurf, false, now)
	if c := mcpAdapterDetected(t, "cursor"); c {
		t.Fatal("precondition: cursor must be undetected at save time")
	}

	if err := SaveLastChoice([]string{"claude"}); err != nil {
		t.Fatalf("SaveLastChoice: %v", err)
	}

	// Cursor is installed afterwards.
	writeConfig(t, mcpreg.Cursor, false, now)

	if c := find(t, DetectWithPrevious(nil), "cursor"); !c.DefaultSelected {
		t.Errorf("cursor was never offered, so it cannot have been opted out of; (B) must decide and check it. got %+v", c)
	}
}

// TestOptOut_RewriteForgetsADeclineForAnUninstalledTool documents a KNOWN
// limit of the whole-document rewrite, so the package comment's claim about it
// is checked rather than asserted. SaveLastChoice records the tools detected at
// the moment of the save; it does not merge with what the file already held. So
// a decline survives only while its tool stays detected:
//
//	decline cursor -> uninstall cursor -> any later save -> reinstall cursor
//
// leaves cursor back on the (B) default. Whether a merge would be better is a
// separate call; this pins what the code actually does today.
func TestOptOut_RewriteForgetsADeclineForAnUninstalledTool(t *testing.T) {
	setupHome(t)
	now := nowFunc()
	writeConfig(t, mcpreg.ClaudeCode, false, now)
	cursorPath := writeConfig(t, mcpreg.Cursor, false, now)

	// 1. Cursor is declined, and the decline sticks.
	if err := SaveLastChoice([]string{"claude"}); err != nil {
		t.Fatalf("save (decline cursor): %v", err)
	}
	if c := find(t, DetectWithPrevious(nil), "cursor"); c.DefaultSelected {
		t.Fatal("precondition: the decline must hold while cursor is installed")
	}

	// 2. Cursor is uninstalled — config AND its parent dir go away.
	if err := os.RemoveAll(filepath.Dir(cursorPath)); err != nil {
		t.Fatal(err)
	}
	if mcpAdapterDetected(t, "cursor") {
		t.Fatal("precondition: cursor must be undetected after removal")
	}

	// 3. Any later save rewrites the document without it.
	if err := SaveLastChoice([]string{"claude"}); err != nil {
		t.Fatalf("save (cursor gone): %v", err)
	}
	set, err := ReadLastChoice()
	if err != nil {
		t.Fatalf("ReadLastChoice: %v", err)
	}
	if _, ok := set["cursor"]; ok {
		t.Errorf("the rewrite drops a tool that is no longer detected; got %v", set)
	}

	// 4. Reinstalled, cursor is back on the (B) default.
	writeConfig(t, mcpreg.Cursor, false, now)
	if c := find(t, DetectWithPrevious(nil), "cursor"); !c.DefaultSelected {
		t.Errorf("after the rewrite forgot the decline, (B) decides again and checks a fresh config; got %+v", c)
	}
}

// mcpAdapterDetected reports whether the given tool ID is currently detected.
func mcpAdapterDetected(t *testing.T, id string) bool {
	t.Helper()
	for _, tl := range detectWith(map[string]bool{}, nil) {
		if tl.ID == id {
			return true
		}
	}
	return false
}

// TestOptOut_ChooseOptOutChooseAgain walks the whole TRANSITION, not its halves:
// chosen → opted out → chosen again, each step through SaveLastChoice and read
// back through DetectWithPrevious, with (B) saying "checked" the entire time so
// every observation is attributable to the recorded choice alone.
func TestOptOut_ChooseOptOutChooseAgain(t *testing.T) {
	setupHome(t)
	now := nowFunc()
	writeConfig(t, mcpreg.ClaudeCode, false, now)
	writeConfig(t, mcpreg.Cursor, false, now)

	claudeChecked := func() bool {
		t.Helper()
		return find(t, DetectWithPrevious(nil), "claude").DefaultSelected
	}

	// 1. chosen.
	if err := SaveLastChoice([]string{"claude", "cursor"}); err != nil {
		t.Fatalf("save (chosen): %v", err)
	}
	if !claudeChecked() {
		t.Fatal("step 1: claude was chosen and saved; it must be checked")
	}

	// 2. opted out — the step the old shape could not express.
	if err := SaveLastChoice([]string{"cursor"}); err != nil {
		t.Fatalf("save (opt out): %v", err)
	}
	if claudeChecked() {
		t.Fatal("step 2: claude was unchecked and saved; it must NOT be checked")
	}

	// 3. chosen again — the opt-out must not be sticky either.
	if err := SaveLastChoice([]string{"claude", "cursor"}); err != nil {
		t.Fatalf("save (re-chosen): %v", err)
	}
	if !claudeChecked() {
		t.Fatal("step 3: claude was re-checked and saved; the earlier opt-out must not persist")
	}
}

// TestReadLastChoice_LegacyFileHasNoDeselections is the backward-compatibility
// direction: a file written by an older binary carries no `deselected` key, and
// must keep meaning exactly what it meant then — the named tools ON, everything
// else left to (B). Absence must NOT be reinterpreted as "off".
func TestReadLastChoice_LegacyFileHasNoDeselections(t *testing.T) {
	setupHome(t)
	now := nowFunc()
	writeConfig(t, mcpreg.ClaudeCode, false, now) // recent → (B) checks it
	writeRawLastChoice(t, []byte(`{"selected":["cursor"],"savedAt":"2026-01-01T00:00:00Z"}`), 0o600)

	set, err := ReadLastChoice()
	if err != nil {
		t.Fatalf("ReadLastChoice: %v", err)
	}
	if _, ok := set["claude"]; ok {
		t.Errorf("legacy file: claude must be ABSENT from the set, not %v — absence is not an opt-out", set)
	}
	if c := find(t, DetectWithPrevious(nil), "claude"); !c.DefaultSelected {
		t.Errorf("legacy file: claude is recent, so (B) must still check it. got %+v", c)
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
