package agenthooks

// This file defines the per-host capability model for the opt-in "query the
// graph, don't grep your way around it" pre-tool nudge (#4238, #4276, #4297).
//
// BACKGROUND
// ----------
// The cross-host rules block (internal/install/rulesfiles) writes the standing
// directive into every IDE's project-context file (CLAUDE.md, .cursorrules,
// .windsurfrules, .codeium/instructions.md, .github/copilot-instructions.md).
// Every supported host therefore ALREADY carries the guidance as passive
// context.
//
// This package adds the ACTIVE reinforcement: a pre-tool hook that fires the
// moment a structural grep is about to run and nudges toward the archigraph
// MCP. A pre-tool hook is only possible on hosts that expose a programmable
// pre-tool / pre-shell lifecycle surface. Most agent hosts do NOT. Rather than
// fabricate a hook API for a host that lacks one, each host honestly declares
// whether it supports a real pre-tool hook:
//
//   - SupportsHook() == true  → InstallHook writes a real, host-native hook.
//   - SupportsHook() == false → InstallHook is a documented no-op; the rules
//     file (written separately) carries the guidance. NoHookReason explains
//     why, so `doctor`/reporting can be honest about the gap.
//
// PER-HOST CAPABILITY MATRIX (honest, researched)
// -----------------------------------------------
//
//	Host          Pre-tool hook surface                         Hook?
//	-----------   -------------------------------------------   -----
//	Claude Code   .claude/settings.json PreToolUse (Bash/Grep)   YES
//	Cursor        .cursor/hooks.json beforeShellExecution        YES
//	Windsurf      rules + workflows, no pre-tool/-shell hook      no
//	Codeium       no programmable pre-tool hook API               no
//	Copilot       no programmable pre-tool hook API               no
//
// Adding a new host = implement Host and append it to Registry(). The shared
// NudgeScript (nudge.go) is reused verbatim by every hooking host so the
// behaviour (advisory-only, structural-heuristic, once-per-session) is
// identical across hosts.

// Host is one agent host's pre-tool-hook capability + installer. It mirrors
// the ServiceManager / rulesfiles per-target pattern: a small interface with
// one implementation per host and a deterministic registry.
type Host interface {
	// Name is the stable human-readable host name (e.g. "Claude Code").
	Name() string

	// SupportsHook reports whether this host exposes a real, programmable
	// pre-tool / pre-shell hook surface. When false, InstallHook is a
	// documented no-op and NoHookReason explains the gap.
	SupportsHook() bool

	// NoHookReason returns a one-line explanation of why this host has no
	// pre-tool hook. Empty for hosts where SupportsHook() == true.
	NoHookReason() string

	// InstallHook upserts this host's pre-tool hook under repoRoot and
	// returns the absolute path that was written. For a no-hook host it is a
	// no-op returning ("", nil). Safe to call repeatedly (idempotent upsert).
	InstallHook(repoRoot string) (string, error)

	// UninstallHook removes this host's managed pre-tool hook (and any
	// sidecar script) under repoRoot. A no-hook host is a no-op. Idempotent:
	// a missing file or missing entry is not an error.
	UninstallHook(repoRoot string) error

	// IsHookInstalled reports whether this host's managed pre-tool hook is
	// present under repoRoot. Always false for a no-hook host.
	IsHookInstalled(repoRoot string) bool
}

// Registry returns the supported agent hosts in deterministic order. The
// order is also the order in which install/uninstall iterate and in which
// reporting lists them, so output is stable across runs.
func Registry() []Host {
	return []Host{
		claudeCodeHost{},
		cursorHost{},
		// No-hook hosts: the rules file carries the guidance; the pre-tool
		// hook is a documented no-op because the host has no hook surface.
		instructionOnlyHost{
			name:   "Windsurf",
			reason: "Windsurf exposes rules (.windsurfrules) + workflows but no programmable pre-tool/pre-shell hook API; guidance is carried by the rules file.",
		},
		instructionOnlyHost{
			name:   "Codeium",
			reason: "Codeium has no programmable pre-tool hook API; guidance is carried by the .codeium/instructions.md rules file.",
		},
		instructionOnlyHost{
			name:   "GitHub Copilot",
			reason: "GitHub Copilot has no programmable pre-tool hook API; guidance is carried by the .github/copilot-instructions.md rules file.",
		},
	}
}

// HostCapability is a flat, reportable view of one host's hook capability,
// produced by Capabilities(). Used by doctor/reporting to print an honest
// per-host matrix without depending on the Host interface internals.
type HostCapability struct {
	// Name is the host name.
	Name string
	// SupportsHook is true when the host has a real pre-tool hook.
	SupportsHook bool
	// NoHookReason explains the gap when SupportsHook is false.
	NoHookReason string
}

// Capabilities returns the per-host hook-capability matrix in registry order.
func Capabilities() []HostCapability {
	hosts := Registry()
	out := make([]HostCapability, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, HostCapability{
			Name:         h.Name(),
			SupportsHook: h.SupportsHook(),
			NoHookReason: h.NoHookReason(),
		})
	}
	return out
}

// ── package-level convenience API (back-compat with the single-host caller) ──

// Install installs every supported host's pre-tool hook under repoRoot. Hosts
// without a real hook surface are skipped (no-op). It returns the absolute
// paths that were written (one per hooking host) and the first error
// encountered.
//
// Back-compat: the previous single-host API returned one path; callers that
// ignore the path (the installer) are unaffected. The Claude Code path is
// always element 0 when present, preserving the historical "settings path"
// expectation for any caller that reads paths[0].
func Install(repoRoot string) ([]string, error) {
	var written []string
	for _, h := range Registry() {
		if !h.SupportsHook() {
			continue
		}
		p, err := h.InstallHook(repoRoot)
		if err != nil {
			return written, err
		}
		if p != "" {
			written = append(written, p)
		}
	}
	return written, nil
}

// Uninstall removes every supported host's managed pre-tool hook under
// repoRoot. No-hook hosts are no-ops. It is idempotent and returns the first
// error encountered (continuing past per-host no-ops).
func Uninstall(repoRoot string) error {
	var firstErr error
	for _, h := range Registry() {
		if err := h.UninstallHook(repoRoot); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// IsInstalled reports whether ANY supported host's managed pre-tool hook is
// present under repoRoot. (Claude Code is the historical answer; a hook on any
// hooking host now counts.)
func IsInstalled(repoRoot string) bool {
	for _, h := range Registry() {
		if h.IsHookInstalled(repoRoot) {
			return true
		}
	}
	return false
}
