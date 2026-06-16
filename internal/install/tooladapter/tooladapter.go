// Package tooladapter defines the ToolAdapter abstraction: a uniform
// interface over the AI coding tools that `grafel install` targets, plus
// a registry of the tools grafel supports today.
//
// Motivation (#5253, EPIC #5252). Historically install.Apply hard-coded
// the sequence of artifacts it wrote: the six rules-file conventions, the
// Claude + Windsurf MCP entries, the Claude skills copy, and the opt-in
// Claude agent hook. Adding a new tool meant editing Apply in several
// places. This package inverts that: each tool is an Adapter that declares
// which of the three artifact kinds it supports —
//
//   - rules file(s)  — the marker-wrapped "use the grafel MCP" block,
//   - MCP registration — the per-tool mcpServers entry, and
//   - skills/commands — Claude-only skill copy,
//
// plus the opt-in agent hook (Claude-only). A tool that lacks a capability
// reports it as a no-op (empty target list / Supports* == false), so
// Apply can iterate adapters uniformly and a new tool is added by
// implementing Adapter and registering it — WITHOUT editing Apply.
//
// SCOPE: this package is the abstraction + the faithful re-expression of
// TODAY's behaviour. It deliberately does NOT add MCP writers for tools
// that grafel does not register today (Cursor/Windsurf-rules/Codex/etc.
// MCP is #5254/#5255), nor the CLI wizard (#5256) or web UI (#5257). The
// adapters DELEGATE to the existing writers (rulesfiles, mcpreg,
// agenthooks) so the file-format logic is unchanged.
package tooladapter

import (
	"github.com/cajasmota/grafel/internal/install/mcpreg"
	"github.com/cajasmota/grafel/internal/registry"
)

// Adapter is the uniform interface over a single AI coding tool grafel can
// install into. Implementations are value types registered in registry()
// below; a new tool is added by implementing this interface and appending
// it there — Apply never needs to change.
//
// The three capability groups are expressed so that a tool lacking a
// capability is a pure no-op:
//
//   - RulesFileTargets() returns nil/empty when the tool has no per-repo
//     rules file convention.
//   - SupportsMCP()/MCPTool() report whether grafel registers an MCP entry
//     for this tool (Claude, Windsurf, Cursor and Codex return true; Codex
//     is written as TOML, the rest as JSON — see mcpreg).
//   - SupportsSkills()/SupportsAgentHook() are Claude-only today.
type Adapter interface {
	// ID is the stable, lowercase identifier persisted in
	// GroupConfig.Tools (e.g. "claude", "cursor"). Never change an
	// existing ID — it is a config key.
	ID() string

	// DisplayName is the human-facing tool name (e.g. "Claude Code").
	DisplayName() string

	// DetectInstalled reports a best-effort guess that the tool is present
	// on this machine. It is advisory only — Apply still honours the
	// enabled-tools set regardless — and is intended for the future CLI
	// wizard (#5256) to pre-select likely tools.
	DetectInstalled() bool

	// RulesFileTargets returns the per-repo rules-file paths (relative,
	// forward-slash, drawn from rulesfiles.Targets) this tool reads. Empty
	// when the tool has no rules-file convention.
	RulesFileTargets() []string

	// SupportsMCP reports whether grafel registers an MCP entry for this
	// tool today. MCPTool returns the mcpreg.Tool to register when true.
	SupportsMCP() bool
	MCPTool() mcpreg.Tool

	// SupportsSkills reports whether this tool consumes the grafel skills
	// bundle (Claude-only today). The actual skills copy lives in the
	// global install transaction (copy.go); this flag lets future per-tool
	// gating of that step key off the adapter set.
	SupportsSkills() bool

	// SupportsAgentHook reports whether this tool exposes the opt-in
	// PreToolUse grep-interceptor agent hook (Claude-only today).
	SupportsAgentHook() bool
}

// registry is the ordered list of adapters for the tools grafel supports
// today. Order is the order Apply iterates, which determines the order of
// rules-file writes and Result reporting; keep it stable.
//
// To add a tool: implement Adapter and append it here.
func adapters() []Adapter {
	return []Adapter{
		claudeAdapter{},
		codexAdapter{},
		cursorAdapter{},
		windsurfAdapter{},
		codeiumAdapter{},
		copilotAdapter{},
		kiroAdapter{},
		antigravityAdapter{},
	}
}

// All returns every registered adapter in deterministic order.
func All() []Adapter {
	return adapters()
}

// Lookup returns the adapter with the given ID, or (nil, false) if no such
// tool is registered.
func Lookup(id string) (Adapter, bool) {
	for _, a := range adapters() {
		if a.ID() == id {
			return a, true
		}
	}
	return nil, false
}

// AllIDs returns the IDs of every registered adapter, in registry order.
// This is the historical default enablement set (every supported tool).
func AllIDs() []string {
	all := adapters()
	out := make([]string, 0, len(all))
	for _, a := range all {
		out = append(out, a.ID())
	}
	return out
}

// EnabledTools resolves the effective enabled-tool set for a group config.
//
// Back-compat contract (#5253): when cfg is nil, or cfg.Tools is absent or
// empty, the effective set is the historical default — every supported
// tool (AllIDs) — so existing configs and new installs that don't specify
// `tools` behave exactly as before (Claude full + all six rules files +
// Claude/Windsurf MCP). When cfg.Tools is non-empty it is honoured as the
// explicit allow-list, filtered to known adapter IDs (unknown IDs are
// dropped) and de-duplicated, preserving the caller's order.
func EnabledTools(cfg *registry.GroupConfig) []string {
	if cfg == nil || len(cfg.Tools) == 0 {
		return AllIDs()
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(cfg.Tools))
	for _, id := range cfg.Tools {
		if seen[id] {
			continue
		}
		if _, ok := Lookup(id); !ok {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		// Config named only unknown tools — fall back to default rather
		// than installing nothing.
		return AllIDs()
	}
	return out
}

// EnabledAdapters resolves EnabledTools and returns the matching Adapter
// values in registry order (not config order), so iteration over them is
// deterministic regardless of how the user ordered `tools`.
func EnabledAdapters(cfg *registry.GroupConfig) []Adapter {
	enabled := map[string]bool{}
	for _, id := range EnabledTools(cfg) {
		enabled[id] = true
	}
	var out []Adapter
	for _, a := range adapters() {
		if enabled[a.ID()] {
			out = append(out, a)
		}
	}
	return out
}
