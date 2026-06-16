package tooladapter_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/install/rulesfiles"
	"github.com/cajasmota/grafel/internal/install/tooladapter"
	"github.com/cajasmota/grafel/internal/registry"
)

// TestDefaultEnablement_ReproducesAllSixRulesFiles is the core back-compat
// guard: with no explicit Tools the union of rules-file targets across the
// enabled adapters must equal exactly rulesfiles.Targets (the historical
// all-six set).
func TestDefaultEnablement_ReproducesAllSixRulesFiles(t *testing.T) {
	for _, cfg := range []*registry.GroupConfig{nil, {}, {Tools: nil}, {Tools: []string{}}} {
		got := unionRulesTargets(tooladapter.EnabledAdapters(cfg))
		want := append([]string{}, rulesfiles.Targets...)
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("default enablement rules targets = %v, want %v", got, want)
		}
	}
}

// TestDefaultEnablement_MCPTools guards the set of MCP tools grafel
// registers under the default (all-tools) enablement. Cursor and Codex were
// added in #5254 alongside the pre-existing Claude + Windsurf; Kiro was added
// in #5255 (Antigravity is rules-only, no MCP).
func TestDefaultEnablement_MCPTools(t *testing.T) {
	var mcp []mcpreg.Tool
	for _, a := range tooladapter.EnabledAdapters(nil) {
		if a.SupportsMCP() {
			mcp = append(mcp, a.MCPTool())
		}
	}
	sort.Slice(mcp, func(i, j int) bool { return mcp[i] < mcp[j] })
	want := []mcpreg.Tool{mcpreg.ClaudeCode, mcpreg.Codex, mcpreg.Cursor, mcpreg.Windsurf, mcpreg.Kiro}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if !reflect.DeepEqual(mcp, want) {
		t.Fatalf("default MCP tools = %v, want %v", mcp, want)
	}
}

// TestRestrictedEnablement_OnlySubset proves a restricted Tools list yields
// only that subset's artifacts.
func TestRestrictedEnablement_OnlySubset(t *testing.T) {
	cfg := &registry.GroupConfig{Tools: []string{"cursor"}}
	ad := tooladapter.EnabledAdapters(cfg)
	if len(ad) != 1 || ad[0].ID() != "cursor" {
		t.Fatalf("expected only cursor adapter, got %v", idsOf(ad))
	}
	if rt := unionRulesTargets(ad); !reflect.DeepEqual(rt, []string{".cursorrules"}) {
		t.Fatalf("cursor rules targets = %v, want [.cursorrules]", rt)
	}
	// Cursor registers MCP (#5254) but has no agent hook or skills.
	if !ad[0].SupportsMCP() || ad[0].MCPTool() != mcpreg.Cursor {
		t.Fatalf("cursor must register Cursor MCP")
	}
	if ad[0].SupportsAgentHook() || ad[0].SupportsSkills() {
		t.Fatalf("cursor should not support hook/skills")
	}
}

// TestRestrictedEnablement_ClaudeOnly proves Claude-only keeps MCP + skills
// + hook + CLAUDE.md and drops the other five rules files.
func TestRestrictedEnablement_ClaudeOnly(t *testing.T) {
	cfg := &registry.GroupConfig{Tools: []string{"claude"}}
	ad := tooladapter.EnabledAdapters(cfg)
	if len(ad) != 1 || ad[0].ID() != "claude" {
		t.Fatalf("expected only claude adapter, got %v", idsOf(ad))
	}
	a := ad[0]
	if rt := unionRulesTargets(ad); !reflect.DeepEqual(rt, []string{"CLAUDE.md"}) {
		t.Fatalf("claude rules targets = %v, want [CLAUDE.md]", rt)
	}
	if !a.SupportsMCP() || a.MCPTool() != mcpreg.ClaudeCode {
		t.Fatalf("claude must register ClaudeCode MCP")
	}
	if !a.SupportsSkills() || !a.SupportsAgentHook() {
		t.Fatalf("claude must support skills + agent hook")
	}
}

// TestEnabledTools_UnknownIDsDropped_FallbackToDefault verifies unknown IDs
// are filtered, and an all-unknown list falls back to the full default
// rather than installing nothing.
func TestEnabledTools_UnknownIDsDropped_FallbackToDefault(t *testing.T) {
	cfg := &registry.GroupConfig{Tools: []string{"cursor", "nope", "cursor"}}
	got := tooladapter.EnabledTools(cfg)
	if !reflect.DeepEqual(got, []string{"cursor"}) {
		t.Fatalf("EnabledTools dedup/filter = %v, want [cursor]", got)
	}

	allUnknown := &registry.GroupConfig{Tools: []string{"nope", "ghost"}}
	if got := tooladapter.EnabledTools(allUnknown); !reflect.DeepEqual(got, tooladapter.AllIDs()) {
		t.Fatalf("all-unknown fallback = %v, want default %v", got, tooladapter.AllIDs())
	}
}

func TestLookupAndAllIDs(t *testing.T) {
	if _, ok := tooladapter.Lookup("claude"); !ok {
		t.Fatal("claude must be registered")
	}
	if _, ok := tooladapter.Lookup("does-not-exist"); ok {
		t.Fatal("unknown id must not resolve")
	}
	want := []string{"claude", "codex", "cursor", "windsurf", "codeium", "copilot", "kiro", "antigravity"}
	if got := tooladapter.AllIDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllIDs = %v, want %v", got, want)
	}
}

// TestKiroAdapter checks Kiro's targets: a steering rules file plus an MCP
// entry registered into the user-global ~/.kiro/settings/mcp.json (#5255).
func TestKiroAdapter(t *testing.T) {
	cfg := &registry.GroupConfig{Tools: []string{"kiro"}}
	ad := tooladapter.EnabledAdapters(cfg)
	if len(ad) != 1 || ad[0].ID() != "kiro" {
		t.Fatalf("expected only kiro adapter, got %v", idsOf(ad))
	}
	a := ad[0]
	if rt := unionRulesTargets(ad); !reflect.DeepEqual(rt, []string{".kiro/steering/grafel.md"}) {
		t.Fatalf("kiro rules targets = %v, want [.kiro/steering/grafel.md]", rt)
	}
	if !a.SupportsMCP() || a.MCPTool() != mcpreg.Kiro {
		t.Fatalf("kiro must register Kiro MCP")
	}
	if a.SupportsAgentHook() || a.SupportsSkills() {
		t.Fatalf("kiro should not support hook/skills")
	}
}

// TestAntigravityAdapter checks Antigravity is a rules-only adapter (#5255):
// a workspace .agent/rules/grafel.md file and NO MCP entry (path not yet
// confidently verifiable — see adapter doc TODO).
func TestAntigravityAdapter(t *testing.T) {
	cfg := &registry.GroupConfig{Tools: []string{"antigravity"}}
	ad := tooladapter.EnabledAdapters(cfg)
	if len(ad) != 1 || ad[0].ID() != "antigravity" {
		t.Fatalf("expected only antigravity adapter, got %v", idsOf(ad))
	}
	a := ad[0]
	if rt := unionRulesTargets(ad); !reflect.DeepEqual(rt, []string{".agent/rules/grafel.md"}) {
		t.Fatalf("antigravity rules targets = %v, want [.agent/rules/grafel.md]", rt)
	}
	if a.SupportsMCP() || a.MCPTool() != "" {
		t.Fatalf("antigravity must NOT register MCP (path unverified)")
	}
	if a.SupportsAgentHook() || a.SupportsSkills() {
		t.Fatalf("antigravity should not support hook/skills")
	}
}

func unionRulesTargets(ad []tooladapter.Adapter) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range ad {
		for _, t := range a.RulesFileTargets() {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

func idsOf(ad []tooladapter.Adapter) []string {
	out := make([]string, 0, len(ad))
	for _, a := range ad {
		out = append(out, a.ID())
	}
	return out
}
