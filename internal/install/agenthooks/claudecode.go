// Package agenthooks installs OPT-IN agent-host hooks that actively
// reinforce the "query the graph, don't grep your way around it" standing
// directive (#4238, #4276) at the moment a structural grep is about to run.
//
// The package is a PER-HOST registry (see host.go): each supported agent host
// declares whether it exposes a real pre-tool / pre-shell hook surface and, if
// so, installs a host-native hook that runs the SHARED advisory nudge script
// (nudge.go). Hosts without a hook surface are honest, documented no-ops — the
// cross-host rules block (internal/install/rulesfiles) carries the guidance for
// them. This complements — does not replace — the rules-block directive.
//
// This file is the CLAUDE CODE host. Claude Code's pre-tool surface is a
// PreToolUse hook: a JSON entry in `.claude/settings.json` with a `matcher`
// (which tool it fires on) and a `command` (a shell command that receives the
// tool call as JSON on stdin and may emit advisory output).
//
// Design constraints (all enforced here and in the embedded nudge script):
//
//   - ADVISORY ONLY, NEVER BLOCKING. The hook exits 0 and prints a one-line
//     nudge to stderr. It never denies a tool call. A grep the agent wants
//     to run still runs; we only suggest archigraph might be faster.
//   - PRECISE heuristic, not "every grep". The nudge fires only on grep/rg
//     invocations that look like STRUCTURAL queries (symbol hunts,
//     definition lookups, who-calls patterns) — not TODO/string sweeps,
//     which grep is genuinely the right tool for. A nudge that fires on
//     every grep would train users to ignore it.
//   - ONCE-PER-SESSION dedup. The script touches a per-session marker file
//     the first time it fires and stays silent afterwards, so a session
//     doesn't get spammed.
//
// Idempotency mirrors internal/install/rulesfiles: the managed PreToolUse
// entry carries a stable marker id (Marker) so re-running install upserts
// in place, an existing managed entry is replaced, and every other key in
// `.claude/settings.json` — plus any unmanaged PreToolUse hooks the user
// added — is preserved byte-compatibly (we round-trip through the JSON
// document and only touch our own array element).
package agenthooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// claudeCodeHost is the Claude Code implementation of Host. Its pre-tool
// surface is the `.claude/settings.json` PreToolUse hook.
type claudeCodeHost struct{}

func (claudeCodeHost) Name() string         { return "Claude Code" }
func (claudeCodeHost) SupportsHook() bool   { return true }
func (claudeCodeHost) NoHookReason() string { return "" }

func (claudeCodeHost) InstallHook(repoRoot string) (string, error) {
	return installClaudeCode(repoRoot)
}
func (claudeCodeHost) UninstallHook(repoRoot string) error { return uninstallClaudeCode(repoRoot) }
func (claudeCodeHost) IsHookInstalled(repoRoot string) bool {
	return claudeCodeInstalled(repoRoot)
}

// Marker is the stable identifier embedded in the managed PreToolUse hook
// command so install/update/uninstall can find exactly our entry among any
// other user-authored hooks. Bumping the trailing version causes an older
// managed entry to be recognised and replaced in place.
const Marker = "archigraph:grep-interceptor:v1"

// MatcherTools is the Claude Code tool-matcher the hook fires on. Claude
// Code's structural code search runs through the Bash tool (grep/rg), and
// the harness also exposes dedicated Grep/Glob tools — matching all three
// catches the structural-query surface. The matcher is a regex alternation
// understood by Claude Code's PreToolUse matcher.
const MatcherTools = "Bash|Grep|Glob"

// SettingsRelPath is the project-relative path to the Claude Code project
// settings file the hook lives in.
var SettingsRelPath = filepath.Join(".claude", "settings.json")

// NudgeScriptRelPath is the project-relative path the advisory nudge script
// is written to. Keeping it on disk (rather than a giant inline command)
// keeps the settings.json entry small and lets users read/audit it.
var NudgeScriptRelPath = filepath.Join(".claude", "archigraph-grep-nudge.sh")

// installClaudeCode upserts the marker-identified PreToolUse nudge hook into
// the project's .claude/settings.json (creating the file and the nudge script
// if absent) and returns the absolute settings path that was touched.
//
// It is safe to call repeatedly: an existing managed entry is replaced in
// place, all other settings and unmanaged hooks are preserved, and the
// nudge script is rewritten to the current version.
func installClaudeCode(repoRoot string) (string, error) {
	settingsPath := filepath.Join(repoRoot, SettingsRelPath)
	scriptPath := filepath.Join(repoRoot, NudgeScriptRelPath)

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return "", fmt.Errorf("agenthooks: mkdir: %w", err)
	}
	if err := writeNudgeScript(scriptPath); err != nil {
		return "", err
	}

	doc, err := readSettings(settingsPath)
	if err != nil {
		return "", err
	}
	if err := upsertHook(doc, scriptPath); err != nil {
		return "", err
	}
	if err := writeSettings(settingsPath, doc); err != nil {
		return "", err
	}
	return settingsPath, nil
}

// uninstallClaudeCode removes the marker-identified managed PreToolUse entry
// and deletes the nudge script, leaving every other setting and any unmanaged
// hooks untouched. It is idempotent: a missing file or missing entry is a
// no-op.
func uninstallClaudeCode(repoRoot string) error {
	settingsPath := filepath.Join(repoRoot, SettingsRelPath)
	scriptPath := filepath.Join(repoRoot, NudgeScriptRelPath)
	_ = os.Remove(scriptPath)

	doc, err := readSettings(settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !removeManagedHook(doc) {
		return nil
	}
	return writeSettings(settingsPath, doc)
}

// claudeCodeInstalled reports whether the project's .claude/settings.json
// already contains the marker-identified managed PreToolUse entry.
func claudeCodeInstalled(repoRoot string) bool {
	settingsPath := filepath.Join(repoRoot, SettingsRelPath)
	doc, err := readSettings(settingsPath)
	if err != nil {
		return false
	}
	_, idx := findManagedHook(doc)
	return idx >= 0
}

// ── settings.json document surgery ───────────────────────────────────────

// The Claude Code settings.json hook shape is:
//
//	{
//	  "hooks": {
//	    "PreToolUse": [
//	      { "matcher": "Bash|Grep|Glob",
//	        "hooks": [ { "type": "command", "command": "<marker> sh ..." } ] }
//	    ]
//	  }
//	}
//
// We operate on map[string]any so that every key we do not understand is
// preserved verbatim on the round-trip.

// upsertHook inserts or replaces the managed PreToolUse matcher group.
func upsertHook(doc map[string]any, scriptPath string) error {
	entry := managedEntry(scriptPath)

	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	pre, _ := hooks["PreToolUse"].([]any)

	if _, idx := findManagedHookIn(pre); idx >= 0 {
		pre[idx] = entry
	} else {
		pre = append(pre, entry)
	}
	hooks["PreToolUse"] = pre
	doc["hooks"] = hooks
	return nil
}

// removeManagedHook deletes the managed matcher group from PreToolUse.
// Returns true if something was removed. Empty containers are pruned so an
// uninstall leaves no dangling "hooks":{"PreToolUse":[]} cruft.
func removeManagedHook(doc map[string]any) bool {
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	pre, _ := hooks["PreToolUse"].([]any)
	_, idx := findManagedHookIn(pre)
	if idx < 0 {
		return false
	}
	pre = append(pre[:idx], pre[idx+1:]...)
	if len(pre) == 0 {
		delete(hooks, "PreToolUse")
	} else {
		hooks["PreToolUse"] = pre
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	}
	return true
}

// findManagedHook locates the managed matcher group in the document.
func findManagedHook(doc map[string]any) (map[string]any, int) {
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		return nil, -1
	}
	pre, _ := hooks["PreToolUse"].([]any)
	return findManagedHookIn(pre)
}

// findManagedHookIn scans a PreToolUse array for the entry whose nested
// command carries our Marker. Returns the entry and its index, or (nil,-1).
func findManagedHookIn(pre []any) (map[string]any, int) {
	for i, raw := range pre {
		group, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		inner, _ := group["hooks"].([]any)
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, _ := hm["command"].(string); cmdHasMarker(cmd) {
				return group, i
			}
		}
	}
	return nil, -1
}

// managedEntry builds the marker-tagged PreToolUse matcher group.
func managedEntry(scriptPath string) map[string]any {
	return map[string]any{
		"matcher": MatcherTools,
		"hooks": []any{
			map[string]any{
				"type": "command",
				// The Marker is embedded as a leading shell comment so it is
				// (a) inert at runtime and (b) reliably greppable for upsert.
				"command": fmt.Sprintf("# %s\nsh %q", Marker, scriptPath),
			},
		},
	}
}

func cmdHasMarker(cmd string) bool {
	return len(cmd) > 0 && containsMarker(cmd)
}

// containsMarker is a tiny substring check kept separate so the marker
// match is the single source of truth for managed-entry identification.
func containsMarker(s string) bool {
	for i := 0; i+len(Marker) <= len(s); i++ {
		if s[i:i+len(Marker)] == Marker {
			return true
		}
	}
	return false
}

// ── JSON IO (preserve-everything-else round trip) ────────────────────────

func readSettings(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	doc := map[string]any{}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("agenthooks: parse %s: %w", filepath.Base(path), err)
	}
	return doc, nil
}

func writeSettings(path string, doc map[string]any) error {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// writeNudgeScript writes the advisory nudge script to disk (executable).
func writeNudgeScript(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(NudgeScript), 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
