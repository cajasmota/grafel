package cli

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// TestApplyToggle_Pure exercises the pure enable/disable set logic without any
// filesystem — feeding simulated toggles and checking the resulting set.
func TestApplyToggle_Pure(t *testing.T) {
	cases := []struct {
		name   string
		prev   []string
		ids    []string
		enable bool
		want   []string
	}{
		{"enable adds cursor", []string{"claude"}, []string{"cursor"}, true, []string{"claude", "cursor"}},
		{"disable removes windsurf", []string{"claude", "windsurf"}, []string{"windsurf"}, false, []string{"claude"}},
		{"enable is idempotent", []string{"claude"}, []string{"claude"}, true, []string{"claude"}},
		{"disable unknown-in-prev no-op", []string{"claude"}, []string{"cursor"}, false, []string{"claude"}},
		{"case-insensitive enable", []string{"claude"}, []string{"CURSOR"}, true, []string{"claude", "cursor"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyToggle(tc.prev, tc.ids, tc.enable)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("applyToggle = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveToolSelection_FlagWins(t *testing.T) {
	cmd := newTestCmd(&bytes.Buffer{})
	var out bytes.Buffer
	ids, ok, err := resolveToolSelection(cmd, &out, "claude,cursor", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected applied=true when --tools given")
	}
	if !reflect.DeepEqual(ids, []string{"claude", "cursor"}) {
		t.Fatalf("ids = %v", ids)
	}
}

func TestResolveToolSelection_FlagUnknownErrors(t *testing.T) {
	cmd := newTestCmd(&bytes.Buffer{})
	var out bytes.Buffer
	if _, _, err := resolveToolSelection(cmd, &out, "claude,nope", false, false); err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// No flag + --yes (or no TTY) → no change, never blocks/promts.
func TestResolveToolSelection_NoFlagYesIsNoOp(t *testing.T) {
	cmd := newTestCmd(&bytes.Buffer{})
	var out bytes.Buffer
	ids, ok, err := resolveToolSelection(cmd, &out, "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	if ok || ids != nil {
		t.Fatalf("expected no-op (--yes), got ok=%v ids=%v", ok, ids)
	}
}

// No flag, non-TTY (stubbed) → no prompt, no change. Critical for CI.
func TestResolveToolSelection_NoTTYNoPrompt(t *testing.T) {
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return false }
	defer func() { stdinIsTTY = orig }()

	cmd := newTestCmd(&bytes.Buffer{})
	var out bytes.Buffer
	ids, ok, err := resolveToolSelection(cmd, &out, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if ok || ids != nil {
		t.Fatalf("non-TTY must be a no-op, got ok=%v ids=%v", ok, ids)
	}
}

// End-to-end through the registry: enable cursor on a group, assert the config
// persisted the new Tools and the output reports the artifacts.
func TestToolsToggle_EnablePersists(t *testing.T) {
	// runToolsToggle (enable path) reaches install.ApplyToolDelta ->
	// mcpreg.Register, which resolves the Cursor MCP settings path off the
	// real $HOME. IsolateHome redirects HOME (and USERPROFILE etc.) into a
	// per-test TempDir and fails closed if the redirect didn't take, so this
	// test can never write to the developer's real ~/.cursor/mcp.json.
	//
	// IsolateHome sandboxes HOME/GRAFEL_HOME/XDG_CONFIG_HOME into ONE tempdir
	// and returns its root; reuse that root as the group's home so registry,
	// config, and editor state all live under the same sandbox.
	home := testsupport.IsolateHome(t)
	makeTestRegistryGroup(t, home, "acme", "core")

	// Seed an explicit selection so EnabledTools is literal (claude only).
	seedTools(t, "acme", []string{"claude"})

	var buf bytes.Buffer
	if err := runToolsToggle(&buf, "acme", []string{"cursor"}, true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	cfg := loadCfg(t, "acme")
	if !reflect.DeepEqual(cfg.Tools, []string{"claude", "cursor"}) {
		t.Fatalf("persisted Tools = %v, want [claude cursor]", cfg.Tools)
	}
	if !strings.Contains(buf.String(), "enabled") {
		t.Fatalf("output should report enable: %s", buf.String())
	}

	// Positive assertion: the Cursor MCP config was written UNDER the
	// isolated temp home, not the developer's real home, and it registered
	// the grafel server.
	cursorPath, err := mcpreg.SettingsPath(mcpreg.Cursor)
	if err != nil {
		t.Fatalf("mcpreg.SettingsPath(Cursor): %v", err)
	}
	testsupport.AssertUnderHome(t, cursorPath)
	b, err := os.ReadFile(cursorPath)
	if err != nil {
		t.Fatalf("read cursor mcp config at %s: %v", cursorPath, err)
	}
	if !strings.Contains(string(b), mcpreg.ServerName) {
		t.Fatalf("cursor mcp config %s missing %q entry: %s", cursorPath, mcpreg.ServerName, b)
	}
	if !strings.HasPrefix(cursorPath, home) {
		t.Fatalf("cursor mcp config path %s not under isolated home %s", cursorPath, home)
	}
}

func TestToolsToggle_DisablePersists(t *testing.T) {
	// Disabling windsurf reaches install.ApplyToolDelta -> mcpreg.Unregister,
	// which resolves the Windsurf/Codeium settings path off $HOME.
	home := testsupport.IsolateHome(t)
	makeTestRegistryGroup(t, home, "acme", "core")
	seedTools(t, "acme", []string{"claude", "windsurf"})

	var buf bytes.Buffer
	if err := runToolsToggle(&buf, "acme", []string{"windsurf"}, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	cfg := loadCfg(t, "acme")
	if !reflect.DeepEqual(cfg.Tools, []string{"claude"}) {
		t.Fatalf("persisted Tools = %v, want [claude]", cfg.Tools)
	}
}

func TestToolsToggle_UnknownIDErrors(t *testing.T) {
	// No real toggle happens here (validation fails first), but isolate
	// anyway — belt-and-braces against future changes reordering validation.
	home := testsupport.IsolateHome(t)
	makeTestRegistryGroup(t, home, "acme", "core")

	var buf bytes.Buffer
	if err := runToolsToggle(&buf, "acme", []string{"bogus"}, true); err == nil {
		t.Fatal("expected error for unknown tool id")
	}
}

func TestToolsList_RunsAndShowsState(t *testing.T) {
	// Read-only (no ApplyToolDelta reached), but isolate for consistency and
	// as a guard against future changes to runToolsList touching $HOME.
	home := testsupport.IsolateHome(t)
	makeTestRegistryGroup(t, home, "acme", "core")
	seedTools(t, "acme", []string{"claude"})

	var buf bytes.Buffer
	if err := runToolsList(&buf, "acme"); err != nil {
		t.Fatalf("list: %v", err)
	}
	s := buf.String()
	if !strings.Contains(s, "claude") || !strings.Contains(s, "enabled") {
		t.Fatalf("list output missing expected rows: %s", s)
	}
	if !strings.Contains(s, "cursor") || !strings.Contains(s, "disabled") {
		t.Fatalf("list should show cursor disabled: %s", s)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────

func seedTools(t *testing.T, group string, ids []string) {
	t.Helper()
	cfgPath, err := registeredConfigPath(group)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools = ids
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
}

func loadCfg(t *testing.T, group string) *registry.GroupConfig {
	t.Helper()
	cfgPath, err := registeredConfigPath(group)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := registry.LoadGroupConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// guard against accidental import drift.
var _ = tooladapter.AllIDs
