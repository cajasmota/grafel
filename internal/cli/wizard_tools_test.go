package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/cli/wiztui"
	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/install/mcptools"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// enabledIDs resolves the effective adapter IDs for a config via the same
// enablement path install.Apply uses, so asserting on it proves exactly which
// adapters would be scaffolded (rules folders + MCP).
func enabledIDs(cfg *registry.GroupConfig) []string {
	var out []string
	for _, a := range tooladapter.EnabledAdapters(cfg) {
		out = append(out, a.ID())
	}
	return out
}

// TestWizard_ToolsFlagRestrictsSelection is the missing wizard→subset coverage
// (#5701): a non-interactive wizard run with --tools claude,codex must persist
// exactly {claude,codex} into GroupConfig.Tools and scaffold ONLY those two
// adapters — none of kiro/codium/cursor/windsurf/copilot/antigravity.
func TestWizard_ToolsFlagRestrictsSelection(t *testing.T) {
	home := withSandboxHome(t)
	repoA := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repoA)

	out := &bytes.Buffer{}
	err := runWizard(out, wizardOptions{
		NonInteractive: true,
		GroupName:      "demo",
		ReposCSV:       repoA,
		Tools:          "claude,codex",
		Watchers:       false,
		GitHooks:       true,
		RunInstall:     true,
	})
	if err != nil {
		t.Fatalf("wizard: %v\n%s", err, out.String())
	}

	cfg := loadCfg(t, "demo")
	if !reflect.DeepEqual(cfg.Tools, []string{"claude", "codex"}) {
		t.Fatalf("persisted Tools = %v, want [claude codex]", cfg.Tools)
	}
	if got := enabledIDs(cfg); !reflect.DeepEqual(got, []string{"claude", "codex"}) {
		t.Fatalf("enabled adapters = %v, want only [claude codex]", got)
	}
}

// TestWizard_MCPToolsFlagRecordsDeclines pins the CROSS-SURFACE consequence of
// --mcp-tools (#6191). This path renders no picker at all, yet it writes the
// last-choice file that the dashboard wizard later reads, so the semantics of
// the flag have to be stated deliberately rather than inherited.
//
// They are: --mcp-tools IS the picker's answer (its own help text says it skips
// the interactive picker), so a DETECTED tool the user left out of an explicit
// list is one they declined, and the decline is recorded. The flag already
// minted a durable "on" for the tools it named; recording the matching "off" is
// what stops that record being one-directional.
//
// Asserted both on disk and through the surface that consumes it: cursor's
// config is fresh here, so the (B) default would otherwise check it.
func TestWizard_MCPToolsFlagRecordsDeclines(t *testing.T) {
	home := withSandboxHome(t)
	repoA := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repoA)

	// A cursor config that EXISTS and is fresh → (B) alone would check it.
	cursorPath, err := mcpreg.SettingsPath(mcpreg.Cursor)
	if err != nil {
		t.Fatalf("SettingsPath(cursor): %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cursorPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cursorPath, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	sel := []string{"claude"}
	out := &bytes.Buffer{}
	if err := runWizard(out, wizardOptions{
		NonInteractive: true,
		GroupName:      "demo",
		ReposCSV:       repoA,
		Watchers:       false,
		GitHooks:       true,
		RunInstall:     true,
		MCPTools:       &sel,
	}); err != nil {
		t.Fatalf("wizard: %v\n%s", err, out.String())
	}

	// On disk: the flag's omission of cursor is recorded as a decline.
	set, err := mcptools.ReadLastChoice()
	if err != nil {
		t.Fatalf("ReadLastChoice: %v", err)
	}
	if !set["claude"] {
		t.Errorf("--mcp-tools=claude must record claude as chosen; got %v", set)
	}
	chosen, ok := set["cursor"]
	if !ok {
		t.Fatalf("cursor was detected and left out of the explicit list; it must be recorded as declined, not merely absent. got %v", set)
	}
	if chosen {
		t.Errorf(`set["cursor"] = true, want false`)
	}

	// Through the surface that reads it: the dashboard wizard's default.
	var sawCursor bool
	for _, tool := range mcptools.Detect() {
		if tool.ID != "cursor" {
			continue
		}
		sawCursor = true
		if tool.DefaultSelected {
			t.Errorf("cursor must arrive UNCHECKED in the wizard: it was declined via --mcp-tools, and that must beat a fresh mtime. got %+v", tool)
		}
	}
	if !sawCursor {
		t.Fatal("test premise broken: cursor not detected at all")
	}
}

// TestWizard_NoToolsFlagPreservesDefaultAll documents the non-interactive
// default: with no --tools the historical empty-means-all contract is kept
// (Tools stays empty; EnabledTools falls back to every adapter).
func TestWizard_NoToolsFlagPreservesDefaultAll(t *testing.T) {
	home := withSandboxHome(t)
	repoA := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repoA)

	out := &bytes.Buffer{}
	if err := runWizard(out, wizardOptions{
		NonInteractive: true,
		GroupName:      "demo",
		ReposCSV:       repoA,
		Watchers:       false,
		GitHooks:       true,
		RunInstall:     true,
	}); err != nil {
		t.Fatalf("wizard: %v\n%s", err, out.String())
	}

	cfg := loadCfg(t, "demo")
	if len(cfg.Tools) != 0 {
		t.Fatalf("Tools should stay empty without --tools, got %v", cfg.Tools)
	}
	if got := enabledIDs(cfg); !reflect.DeepEqual(got, tooladapter.AllIDs()) {
		t.Fatalf("empty Tools must resolve to all adapters, got %v", got)
	}
}

// TestWizard_ToolsFlagUnknownErrors: a bogus --tools token is rejected.
func TestWizard_ToolsFlagUnknownErrors(t *testing.T) {
	home := withSandboxHome(t)
	repoA := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repoA)

	out := &bytes.Buffer{}
	err := runWizard(out, wizardOptions{
		NonInteractive: true,
		GroupName:      "demo",
		ReposCSV:       repoA,
		Tools:          "claude,bogus",
		RunInstall:     false,
	})
	if err == nil {
		t.Fatal("expected error for unknown tool in --tools")
	}
}

// TestGroupAdd_ToolsFlagRestrictsSelection: `group add --tools claude,codex`
// persists exactly {claude,codex} and gates scaffolding to those adapters.
func TestGroupAdd_ToolsFlagRestrictsSelection(t *testing.T) {
	home := withSandboxHome(t)
	repoA := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repoA)

	cmd := newTestCmd(&bytes.Buffer{})
	err := runGroupAddImpl(cmd, "demo", groupAddFlags{
		repoArgs: []string{repoA},
		tools:    "claude,codex",
		rules:    false,
		mcp:      false,
		runInst:  true,
		jsonOut:  true,
	}, "")
	if err != nil {
		t.Fatalf("group add: %v", err)
	}

	cfg := loadCfg(t, "demo")
	if !reflect.DeepEqual(cfg.Tools, []string{"claude", "codex"}) {
		t.Fatalf("persisted Tools = %v, want [claude codex]", cfg.Tools)
	}
	if got := enabledIDs(cfg); !reflect.DeepEqual(got, []string{"claude", "codex"}) {
		t.Fatalf("enabled adapters = %v, want only [claude codex]", got)
	}
}

// TestGroupAdd_ToolsFlagUnknownErrors: bogus --tools rejected before install.
func TestGroupAdd_ToolsFlagUnknownErrors(t *testing.T) {
	home := withSandboxHome(t)
	repoA := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repoA)

	cmd := newTestCmd(&bytes.Buffer{})
	err := runGroupAddImpl(cmd, "demo", groupAddFlags{
		repoArgs: []string{repoA},
		tools:    "bogus",
		runInst:  false,
	}, "")
	if err == nil {
		t.Fatal("expected error for unknown tool in --tools")
	}
}

// TestInteractiveTUI_CapturesEnablement is the reporter's exact scenario
// (#5701): a human running the ordinary terminal `grafel wizard` (the
// alt-screen TUI) picks {claude,codex}. Driving the SAME seam the TUI uses
// (resolveInteractiveTools → newGroupConfigFromResult), we assert cfg.Tools is
// set to exactly the picked set and only those two adapters would be scaffolded
// — none of kiro/codium/cursor/windsurf/copilot/antigravity — with NO --tools
// flag. The huh picker is injected so no real TTY is required.
func TestInteractiveTUI_CapturesEnablement(t *testing.T) {
	restore := promptTools
	promptTools = func() ([]string, error) { return []string{"claude", "codex"}, nil }
	defer func() { promptTools = restore }()

	var out bytes.Buffer
	toolIDs, err := resolveInteractiveTools(&out, wizardOptions{}) // no --tools
	if err != nil {
		t.Fatalf("resolveInteractiveTools: %v", err)
	}
	if !reflect.DeepEqual(toolIDs, []string{"claude", "codex"}) {
		t.Fatalf("resolved enablement = %v, want [claude codex]", toolIDs)
	}

	repo := t.TempDir()
	cfg := newGroupConfigFromResult(
		wiztui.Result{GroupName: "demo"},
		[]registry.Repo{{Slug: "r", Path: repo}},
		false,
		toolIDs,
	)
	if !reflect.DeepEqual(cfg.Tools, []string{"claude", "codex"}) {
		t.Fatalf("cfg.Tools = %v, want [claude codex]", cfg.Tools)
	}
	if got := enabledIDs(cfg); !reflect.DeepEqual(got, []string{"claude", "codex"}) {
		t.Fatalf("enabled adapters = %v, want only [claude codex]", got)
	}
}

// TestInteractiveTUI_ToolsFlagSkipsPrompt: --tools pre-seeds the interactive
// enablement and the picker is NOT shown.
func TestInteractiveTUI_ToolsFlagSkipsPrompt(t *testing.T) {
	restore := promptTools
	promptTools = func() ([]string, error) {
		t.Fatal("promptTools must not run when --tools is provided")
		return nil, nil
	}
	defer func() { promptTools = restore }()

	var out bytes.Buffer
	toolIDs, err := resolveInteractiveTools(&out, wizardOptions{Tools: "claude,codex"})
	if err != nil {
		t.Fatalf("resolveInteractiveTools: %v", err)
	}
	if !reflect.DeepEqual(toolIDs, []string{"claude", "codex"}) {
		t.Fatalf("resolved enablement = %v, want [claude codex]", toolIDs)
	}
}

// TestInteractiveTUI_DeselectAllHintsAndDefaultsToAll: unchecking everything is
// treated as "no explicit choice" (empty→all preserved) and prints a --tools
// hint rather than scaffolding nothing.
func TestInteractiveTUI_DeselectAllHintsAndDefaultsToAll(t *testing.T) {
	restore := promptTools
	promptTools = func() ([]string, error) { return nil, nil } // deselect-all
	defer func() { promptTools = restore }()

	var out bytes.Buffer
	toolIDs, err := resolveInteractiveTools(&out, wizardOptions{})
	if err != nil {
		t.Fatalf("resolveInteractiveTools: %v", err)
	}
	if len(toolIDs) != 0 {
		t.Fatalf("deselect-all should yield empty enablement, got %v", toolIDs)
	}
	if !strings.Contains(out.String(), "--tools") {
		t.Fatalf("expected a --tools hint on deselect-all, got %q", out.String())
	}
	// Empty enablement resolves to all adapters at the Apply boundary.
	cfg := newGroupConfigFromResult(wiztui.Result{GroupName: "demo"}, nil, false, toolIDs)
	if got := enabledIDs(cfg); !reflect.DeepEqual(got, tooladapter.AllIDs()) {
		t.Fatalf("empty enablement must resolve to all adapters, got %v", got)
	}
}

// TestSingleToolsScreen_PrechecksInstalledToolEvenIfOldConfig is the #44
// preselection-regression guard. The wizard now asks about AI tools ONCE, and
// that single screen drives BOTH rules-file scaffolding AND MCP registration.
// Its precheck must therefore be the BROAD "installed tools" default
// (DetectInstalled — config/parent dir present at all), NOT the mcptools B+C
// narrowing (config-modified-≤30d OR has-grafel-entry). Otherwise a first-time
// user whose Claude config EXISTS but is stale (>30d) and has no grafel entry
// would see Claude UNCHECKED and get NO scaffolding for a tool they actually
// have installed. This creates exactly that stale-config-no-grafel-entry state
// and asserts the screen's precheck source (wizardToolChoices) still pre-checks
// it — while confirming the mcptools default (the WRONG source, which the buggy
// toolsSmartPreselection used) would have left it unchecked.
func TestSingleToolsScreen_PrechecksInstalledToolEvenIfOldConfig(t *testing.T) {
	testsupport.IsolateHome(t)

	// Claude Code reads ~/.claude.json; write one that EXISTS, is 40 days old,
	// and has NO grafel MCP entry.
	claudePath, err := mcpreg.SettingsPath(mcpreg.ClaudeCode)
	if err != nil {
		t.Fatalf("SettingsPath(claude): %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(claudePath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(claudePath, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatalf("write claude config: %v", err)
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(claudePath, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// The screen's precheck source (what promptTools renders): claude must be
	// pre-checked because DetectInstalled() sees the config file at all.
	var claudePrechecked, claudeSeen bool
	for _, c := range wizardToolChoices() {
		if c.ID == "claude" {
			claudeSeen = true
			claudePrechecked = c.PreChecked
		}
	}
	if !claudeSeen {
		t.Fatal("claude missing from wizardToolChoices()")
	}
	if !claudePrechecked {
		t.Error("claude NOT pre-checked on the single tools screen despite an installed (if stale) config — scaffolding would be silently skipped for an installed tool")
	}

	// Cross-check the regression's root cause: the mcptools B+C default (the
	// source the buggy toolsSmartPreselection used) DOES leave this stale,
	// grafel-entry-less config unchecked. This is precisely why the precheck
	// must come from the broad installed-tools default, not from here.
	var mcpDefaultForClaude, mcpSawClaude bool
	for _, tool := range mcptools.Detect() {
		if tool.ID == "claude" {
			mcpSawClaude = true
			mcpDefaultForClaude = tool.DefaultSelected
		}
	}
	if !mcpSawClaude {
		t.Fatal("claude missing from mcptools.Detect() (config should be detected)")
	}
	if mcpDefaultForClaude {
		t.Fatal("test premise broken: mcptools default unexpectedly TRUE for a stale, grafel-entry-less config")
	}
}

// TestHuhFallback_AsksToolsOnce is the #44 ask-once guard for the non-TTY (huh)
// fallback path. The huh flow used to ask about AI tools/agents TWICE — step 4a
// promptTools (rules scaffolding, cfg.Tools) AND step 4b promptMCPTools ("which
// tools get the grafel MCP server?", opts.MCPTools). resolveHuhTools now asks
// ONCE via promptTools and leaves opts.MCPTools nil so install.Apply reuses
// cfg.Tools's adapters for MCP too. This drives the exact seam finishWizard uses
// (promptTools injected so no TTY is needed) and asserts: the tools picker runs
// exactly once, no second MCP selection is captured (opts.MCPTools stays nil),
// and MCP+rules registration resolves to exactly the single tools selection.
func TestHuhFallback_AsksToolsOnce(t *testing.T) {
	calls := 0
	restore := promptTools
	promptTools = func() ([]string, error) {
		calls++
		return []string{"claude", "cursor"}, nil
	}
	defer func() { promptTools = restore }()

	cfg := &registry.GroupConfig{Name: "demo"}
	opts := wizardOptions{} // interactive (NonInteractive false), no --tools, no --mcp-tools

	if err := resolveHuhTools(cfg, &opts); err != nil {
		t.Fatalf("resolveHuhTools: %v", err)
	}

	if calls != 1 {
		t.Errorf("tools picker ran %d times, want exactly 1 (asked once, no second MCP prompt)", calls)
	}
	if opts.MCPTools != nil {
		t.Errorf("opts.MCPTools = %v, want nil (MCP follows the tools selection; no separate MCP question)", *opts.MCPTools)
	}
	if !reflect.DeepEqual(cfg.Tools, []string{"claude", "cursor"}) {
		t.Errorf("cfg.Tools = %v, want [claude cursor]", cfg.Tools)
	}
	// With opts.MCPTools nil, install.Apply reuses cfg.Tools's adapters — so both
	// rules scaffolding and MCP registration target exactly the single selection.
	if got := enabledIDs(cfg); !reflect.DeepEqual(got, []string{"claude", "cursor"}) {
		t.Errorf("enabled adapters (rules + MCP) = %v, want only [claude cursor]", got)
	}
}

// TestPendingTools_AppliedAtGroupCreation covers the install-before-wizard
// ordering footgun (#5701 item 3): a tool selection made by `grafel install`
// while no groups exist is stashed as pending and consumed by the next group
// registration instead of being silently dropped.
func TestPendingTools_AppliedAtGroupCreation(t *testing.T) {
	home := withSandboxHome(t)
	repoA := filepath.Join(home, "repos", "alpha")
	makeRepo(t, repoA)

	// Simulate `grafel install --tools claude,codex` with no groups yet.
	out := &bytes.Buffer{}
	if err := persistToolSelection(out, []string{"claude", "codex"}); err != nil {
		t.Fatalf("persistToolSelection: %v", err)
	}

	// Now register a group (no --tools) — it must pick up the pending set.
	cmd := newTestCmd(&bytes.Buffer{})
	if err := runGroupAddImpl(cmd, "demo", groupAddFlags{
		repoArgs: []string{repoA},
		rules:    false, mcp: false, runInst: true, jsonOut: true,
	}, ""); err != nil {
		t.Fatalf("group add: %v", err)
	}

	cfg := loadCfg(t, "demo")
	if !reflect.DeepEqual(cfg.Tools, []string{"claude", "codex"}) {
		t.Fatalf("pending selection not applied: Tools = %v, want [claude codex]", cfg.Tools)
	}

	// Pending file must be consumed (not re-applied to a second group).
	repoB := filepath.Join(home, "repos", "beta")
	makeRepo(t, repoB)
	cmd2 := newTestCmd(&bytes.Buffer{})
	if err := runGroupAddImpl(cmd2, "demo2", groupAddFlags{
		repoArgs: []string{repoB},
		rules:    false, mcp: false, runInst: true, jsonOut: true,
	}, ""); err != nil {
		t.Fatalf("group add 2: %v", err)
	}
	cfg2 := loadCfg(t, "demo2")
	if len(cfg2.Tools) != 0 {
		t.Fatalf("pending selection must be consumed once; demo2 Tools = %v", cfg2.Tools)
	}
}
