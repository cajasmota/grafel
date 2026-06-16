package tooladapter

import (
	"os"

	"github.com/cajasmota/grafel/internal/install/mcpreg"
)

// Rules-file target constants mirror entries in rulesfiles.Targets. They
// are duplicated here as string literals (rather than indexing the slice)
// so that each adapter's target is self-documenting and stable even if the
// order of rulesfiles.Targets changes.
const (
	rulesAGENTS   = "AGENTS.md"
	rulesCLAUDE   = "CLAUDE.md"
	rulesWindsurf = ".windsurfrules"
	rulesCursor   = ".cursorrules"
	rulesCodeium  = ".codeium/instructions.md"
	rulesCopilot  = ".github/copilot-instructions.md"
)

// hasMCPHost reports whether the given tool's MCP config file (or its
// parent dir) exists — a best-effort "is this tool installed" signal.
func hasMCPHost(tool mcpreg.Tool) bool {
	p, err := mcpreg.SettingsPath(tool)
	if err != nil {
		return false
	}
	if _, err := os.Stat(p); err == nil {
		return true
	}
	return false
}

// ── claude ───────────────────────────────────────────────────────────
//
// The flagship: MCP (.claude.json) + skills (~/.claude/skills/) + rules
// (CLAUDE.md) + the opt-in PreToolUse agent hook. Skills and the agent
// hook stay Claude-only. CLAUDE.md is written here; AGENTS.md is owned by
// the codex adapter (Claude Code also reads AGENTS.md, but to avoid two
// adapters writing the same file the canonical owner is codex — see
// rulesfiles package doc).
type claudeAdapter struct{}

func (claudeAdapter) ID() string                 { return "claude" }
func (claudeAdapter) DisplayName() string        { return "Claude Code" }
func (claudeAdapter) DetectInstalled() bool      { return hasMCPHost(mcpreg.ClaudeCode) }
func (claudeAdapter) RulesFileTargets() []string { return []string{rulesCLAUDE} }
func (claudeAdapter) SupportsMCP() bool          { return true }
func (claudeAdapter) MCPTool() mcpreg.Tool       { return mcpreg.ClaudeCode }
func (claudeAdapter) SupportsSkills() bool       { return true }
func (claudeAdapter) SupportsAgentHook() bool    { return true }

// ── codex ────────────────────────────────────────────────────────────
//
// Codex / OpenAI reads AGENTS.md and registers an MCP server in its TOML
// config (~/.codex/config.toml, table [mcp_servers.grafel]) — #5254.
type codexAdapter struct{}

func (codexAdapter) ID() string                 { return "codex" }
func (codexAdapter) DisplayName() string        { return "Codex" }
func (codexAdapter) DetectInstalled() bool      { return hasMCPHost(mcpreg.Codex) }
func (codexAdapter) RulesFileTargets() []string { return []string{rulesAGENTS} }
func (codexAdapter) SupportsMCP() bool          { return true }
func (codexAdapter) MCPTool() mcpreg.Tool       { return mcpreg.Codex }
func (codexAdapter) SupportsSkills() bool       { return false }
func (codexAdapter) SupportsAgentHook() bool    { return false }

// ── cursor ───────────────────────────────────────────────────────────
//
// Cursor Composer reads .cursorrules and registers an MCP server in its
// user-global JSON config (~/.cursor/mcp.json, mcpServers.grafel) — #5254.
type cursorAdapter struct{}

func (cursorAdapter) ID() string                 { return "cursor" }
func (cursorAdapter) DisplayName() string        { return "Cursor" }
func (cursorAdapter) DetectInstalled() bool      { return hasMCPHost(mcpreg.Cursor) }
func (cursorAdapter) RulesFileTargets() []string { return []string{rulesCursor} }
func (cursorAdapter) SupportsMCP() bool          { return true }
func (cursorAdapter) MCPTool() mcpreg.Tool       { return mcpreg.Cursor }
func (cursorAdapter) SupportsSkills() bool       { return false }
func (cursorAdapter) SupportsAgentHook() bool    { return false }

// ── windsurf ─────────────────────────────────────────────────────────
//
// Windsurf Cascade reads .windsurfrules. NOTE: grafel DOES register a
// Windsurf MCP entry today (mcpreg.Windsurf), so unlike the other
// rules-only tools this adapter reports SupportsMCP()==true to preserve
// current behaviour.
type windsurfAdapter struct{}

func (windsurfAdapter) ID() string                 { return "windsurf" }
func (windsurfAdapter) DisplayName() string        { return "Windsurf" }
func (windsurfAdapter) DetectInstalled() bool      { return hasMCPHost(mcpreg.Windsurf) }
func (windsurfAdapter) RulesFileTargets() []string { return []string{rulesWindsurf} }
func (windsurfAdapter) SupportsMCP() bool          { return true }
func (windsurfAdapter) MCPTool() mcpreg.Tool       { return mcpreg.Windsurf }
func (windsurfAdapter) SupportsSkills() bool       { return false }
func (windsurfAdapter) SupportsAgentHook() bool    { return false }

// ── codeium ──────────────────────────────────────────────────────────
//
// Codeium reads .codeium/instructions.md. No grafel MCP entry today.
type codeiumAdapter struct{}

func (codeiumAdapter) ID() string                 { return "codeium" }
func (codeiumAdapter) DisplayName() string        { return "Codeium" }
func (codeiumAdapter) DetectInstalled() bool      { return false }
func (codeiumAdapter) RulesFileTargets() []string { return []string{rulesCodeium} }
func (codeiumAdapter) SupportsMCP() bool          { return false }
func (codeiumAdapter) MCPTool() mcpreg.Tool       { return "" }
func (codeiumAdapter) SupportsSkills() bool       { return false }
func (codeiumAdapter) SupportsAgentHook() bool    { return false }

// ── copilot ──────────────────────────────────────────────────────────
//
// GitHub Copilot reads .github/copilot-instructions.md. No grafel MCP
// entry today.
type copilotAdapter struct{}

func (copilotAdapter) ID() string                 { return "copilot" }
func (copilotAdapter) DisplayName() string        { return "GitHub Copilot" }
func (copilotAdapter) DetectInstalled() bool      { return false }
func (copilotAdapter) RulesFileTargets() []string { return []string{rulesCopilot} }
func (copilotAdapter) SupportsMCP() bool          { return false }
func (copilotAdapter) MCPTool() mcpreg.Tool       { return "" }
func (copilotAdapter) SupportsSkills() bool       { return false }
func (copilotAdapter) SupportsAgentHook() bool    { return false }
