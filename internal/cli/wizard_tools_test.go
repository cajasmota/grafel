package cli

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/cli/wiztui"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/registry"
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
