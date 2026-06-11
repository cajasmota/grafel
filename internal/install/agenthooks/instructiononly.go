package agenthooks

// instructiononly.go is the Host implementation for agent hosts that have NO
// programmable pre-tool / pre-shell hook surface (Windsurf, Codeium, GitHub
// Copilot, …).
//
// For these hosts the honest answer is: the pre-tool hook is a documented
// no-op. The "query the graph, don't grep" guidance still reaches the agent —
// it is written into the host's project-context rules file by
// internal/install/rulesfiles (.windsurfrules, .codeium/instructions.md,
// .github/copilot-instructions.md). We deliberately do NOT fabricate a hook
// API for a host that lacks one; InstallHook returns ("", nil) and
// NoHookReason explains the gap for honest reporting.

// instructionOnlyHost is a Host with no real pre-tool hook surface. All hook
// operations are no-ops; the rules file (written elsewhere) carries the
// guidance.
type instructionOnlyHost struct {
	name   string
	reason string
}

func (h instructionOnlyHost) Name() string         { return h.name }
func (instructionOnlyHost) SupportsHook() bool     { return false }
func (h instructionOnlyHost) NoHookReason() string { return h.reason }

// InstallHook is a documented no-op: no hook file is written.
func (instructionOnlyHost) InstallHook(string) (string, error) { return "", nil }

// UninstallHook is a no-op: there is nothing to remove.
func (instructionOnlyHost) UninstallHook(string) error { return nil }

// IsHookInstalled is always false: this host never has a pre-tool hook.
func (instructionOnlyHost) IsHookInstalled(string) bool { return false }
