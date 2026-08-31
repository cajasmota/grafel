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
	rulesWindsurf = ".windsurfrules"
	rulesCursor   = ".cursorrules"
	rulesCodeium  = ".codeium/instructions.md"
	rulesCopilot  = ".github/copilot-instructions.md"
	// rulesKiro is Kiro's project-level steering file. Kiro reads markdown
	// guidance from <repo>/.kiro/steering/*.md.
	rulesKiro = ".kiro/steering/grafel.md"
	// rulesAntigravity is Google Antigravity's workspace rules file. Rules
	// live as markdown under <repo>/.agent/rules/*.md.
	rulesAntigravity = ".agent/rules/grafel.md"
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
// The flagship: MCP (.claude.json) + skills (~/.claude/skills/) + the opt-in
// PreToolUse agent hook. Skills and the agent hook stay Claude-only.
//
// NOTE(#5702): Claude Code guidance is NO LONGER written as a per-repo
// CLAUDE.md via this adapter's RulesFileTargets. It now goes to the PERSONAL
// ~/.claude/CLAUDE.md (self-gating, opt-in per developer) — and, only with
// `--project-guidance`, to <repo>/.claude/CLAUDE.md — both handled directly in
// install.Apply via rulesfiles.UpsertGuidance. So this adapter returns NO
// per-repo rules targets. AGENTS.md, which Claude Code also reads, remains
// owned by the codex adapter.
type claudeAdapter struct{}

func (claudeAdapter) ID() string                 { return "claude" }
func (claudeAdapter) DisplayName() string        { return "Claude Code" }
func (claudeAdapter) DetectInstalled() bool      { return hasMCPHost(mcpreg.ClaudeCode) }
func (claudeAdapter) RulesFileTargets() []string { return nil }
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

// ── kiro ─────────────────────────────────────────────────────────────
//
// Kiro (AWS agentic IDE) reads project-level steering files from
// <repo>/.kiro/steering/*.md (we write .kiro/steering/grafel.md) and
// connects to MCP servers via a JSON config with the same { "mcpServers":
// { ... } } shape as Cursor. grafel registers in the user-global
// ~/.kiro/settings/mcp.json (mcpreg.Kiro) to match ADR-0004's single
// global-entry-per-tool model — the same choice made for Cursor. Kiro also
// supports a workspace-level .kiro/settings/mcp.json, but a per-repo entry
// is unnecessary since the daemon routes by caller-CWD. (#5255)
type kiroAdapter struct{}

func (kiroAdapter) ID() string                 { return "kiro" }
func (kiroAdapter) DisplayName() string        { return "Kiro" }
func (kiroAdapter) DetectInstalled() bool      { return hasMCPHost(mcpreg.Kiro) }
func (kiroAdapter) RulesFileTargets() []string { return []string{rulesKiro} }
func (kiroAdapter) SupportsMCP() bool          { return true }
func (kiroAdapter) MCPTool() mcpreg.Tool       { return mcpreg.Kiro }
func (kiroAdapter) SupportsSkills() bool       { return false }
func (kiroAdapter) SupportsAgentHook() bool    { return false }

// ── antigravity ──────────────────────────────────────────────────────
//
// Google Antigravity (agentic IDE) reads workspace rules as markdown from
// <repo>/.agent/rules/*.md (we write .agent/rules/grafel.md) and registers an
// MCP server in its user-global JSON config at
// ~/.gemini/antigravity/mcp_config.json (mcpServers.grafel) — #5280.
//
// grafel is a local stdio server (command=grafel binary, args=["mcp-bridge"]),
// so the entry uses the standard JSON { "mcpServers": { ... } } shape —
// identical to Cursor/Kiro and a drop-in for the existing JSON mcpreg writer.
// The `serverUrl` key is only for remote HTTP MCP servers and does NOT apply.
type antigravityAdapter struct{}

func (antigravityAdapter) ID() string                 { return "antigravity" }
func (antigravityAdapter) DisplayName() string        { return "Antigravity" }
func (antigravityAdapter) DetectInstalled() bool      { return hasMCPHost(mcpreg.Antigravity) }
func (antigravityAdapter) RulesFileTargets() []string { return []string{rulesAntigravity} }
func (antigravityAdapter) SupportsMCP() bool          { return true }
func (antigravityAdapter) MCPTool() mcpreg.Tool       { return mcpreg.Antigravity }
func (antigravityAdapter) SupportsSkills() bool       { return false }
func (antigravityAdapter) SupportsAgentHook() bool    { return false }

// ── opencode ─────────────────────────────────────────────────────────
//
// opencode (the open-source terminal agent) registers an MCP server in
// $XDG_CONFIG_HOME/opencode/opencode.json — under the top-level key `mcp`, with
// the whole argv as a `command` ARRAY and `type: "local"`. None of that matches
// the generic JSON writer; mcpreg has a dedicated arm for it (mcpreg/opencode.go,
// #6730). DisplayName is lowercase on purpose — the project styles its own name
// that way.
//
// RULES FILE = AGENTS.md, SHARED WITH CODEX — deliberate, not a copy-paste slip.
// opencode really does read AGENTS.md: the project file first (traversing up
// from the cwd), then ~/.config/opencode/AGENTS.md, then ~/.claude/CLAUDE.md as a
// Claude-compat fallback. Giving it a private file would either duplicate the
// same block on disk or leave the file opencode actually reads unwritten, so it
// takes joint ownership of the existing target instead. The consequence to know:
// enabling opencode ALONE still writes AGENTS.md into every repo. Removal is
// already ownership-aware — ApplyToolDelta strips a shared target only when NO
// surviving tool still owns it (tooldelta.go:114-122,140-159) — so disabling
// codex while opencode stays enabled (or vice versa) leaves AGENTS.md in place,
// and only disabling the last owner removes it.
//
// SKILLS = FALSE, and this looks wrong, so: opencode DOES read skills, including
// from ~/.claude/skills/<name>/SKILL.md — the exact directory grafel already
// populates for Claude. opencode users therefore DO get grafel's skills, via
// opencode's own Claude-compat fallback. Returning true would misdescribe the
// mechanism: the skills copy is Claude-pathed and GLOBAL (tooladapter.go:74-76 —
// the copy lives in the global install transaction, not a per-tool step), so
// there is no opencode-specific skills artifact for this flag to gate. It stays
// false to mean "grafel writes no skills for this tool", which is true.
//
// DETECTION IS NARROWER THAN REGISTRATION, and the gap is widest here.
// DetectInstalled uses hasMCPHost, which stats the config FILE — consistent
// with every sibling adapter. But mcpreg.DetectOpencodePaths goes through
// DetectHostPaths, which accepts the file OR its parent directory. So a user
// with ~/.config/opencode/ but no opencode.json yet gets: `grafel install`
// WILL write the MCP entry (registration finds the dir), while the wizard will
// NOT pre-check opencode (detection wants the file). Not a regression — the
// same asymmetry exists for every tool — but opencode is where it bites,
// because its config file is genuinely optional in a way ~/.cursor/mcp.json is
// not: opencode runs perfectly well with no opencode.json at all. Detection is
// advisory (install honours an explicit selection regardless), so the cost is a
// missing pre-tick, not a missing entry.
type opencodeAdapter struct{}

func (opencodeAdapter) ID() string                 { return "opencode" }
func (opencodeAdapter) DisplayName() string        { return "opencode" }
func (opencodeAdapter) DetectInstalled() bool      { return hasMCPHost(mcpreg.Opencode) }
func (opencodeAdapter) RulesFileTargets() []string { return []string{rulesAGENTS} }
func (opencodeAdapter) SupportsMCP() bool          { return true }
func (opencodeAdapter) MCPTool() mcpreg.Tool       { return mcpreg.Opencode }
func (opencodeAdapter) SupportsSkills() bool       { return false }
func (opencodeAdapter) SupportsAgentHook() bool    { return false }
