package agenthooks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// cursor.go is the CURSOR host implementation of Host.
//
// Cursor exposes a real programmable pre-tool surface via Agent Hooks:
// `.cursor/hooks.json` declares lifecycle hooks, and the `beforeShellExecution`
// event fires immediately before the agent runs a shell command. The hook is a
// `{ "command": "<script>" }` entry under `hooks.beforeShellExecution`; the
// script receives the pending shell command as JSON on stdin and may emit
// advisory output / a permission decision on stdout.
//
// We reuse the SHARED advisory nudge script (nudge.go) verbatim: it reads the
// payload on stdin, applies the structural-grep heuristic, and prints a
// one-line advisory to stderr at most once per session. It never blocks (it
// exits 0 and the Cursor entry does not deny), preserving the advisory-only
// contract. Cursor's payload JSON differs in shape from Claude Code's, but the
// heuristic scans the raw payload text, so the same script works for both.
//
// Idempotency mirrors the Claude Code host: the managed entry carries the
// stable Marker (embedded as a leading shell comment in the command), so
// re-install upserts in place and any user-authored Cursor hooks plus every
// other key in hooks.json are preserved on the JSON round-trip.

// cursorHost is the Cursor implementation of Host.
type cursorHost struct{}

func (cursorHost) Name() string         { return "Cursor" }
func (cursorHost) SupportsHook() bool   { return true }
func (cursorHost) NoHookReason() string { return "" }

func (cursorHost) InstallHook(repoRoot string) (string, error) {
	return installCursor(repoRoot)
}
func (cursorHost) UninstallHook(repoRoot string) error  { return uninstallCursor(repoRoot) }
func (cursorHost) IsHookInstalled(repoRoot string) bool { return cursorInstalled(repoRoot) }

// CursorHooksRelPath is the project-relative path to Cursor's hooks config.
var CursorHooksRelPath = filepath.Join(".cursor", "hooks.json")

// CursorNudgeScriptRelPath is where the advisory nudge script is written for
// Cursor. It is a separate copy from the Claude Code script so each host owns
// its sidecar and uninstalling one host never deletes another's script.
var CursorNudgeScriptRelPath = filepath.Join(".cursor", "archigraph-grep-nudge.sh")

// CursorHookEvent is the Cursor Agent Hooks lifecycle event we attach to:
// it fires before the agent executes a shell command (the surface where a
// structural grep is about to run).
const CursorHookEvent = "beforeShellExecution"

func installCursor(repoRoot string) (string, error) {
	hooksPath := filepath.Join(repoRoot, CursorHooksRelPath)
	scriptPath := filepath.Join(repoRoot, CursorNudgeScriptRelPath)

	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		return "", fmt.Errorf("agenthooks(cursor): mkdir: %w", err)
	}
	if err := writeNudgeScript(scriptPath); err != nil {
		return "", err
	}

	doc, err := readSettings(hooksPath)
	if err != nil {
		return "", err
	}
	upsertCursorHook(doc, scriptPath)
	if err := writeSettings(hooksPath, doc); err != nil {
		return "", err
	}
	return hooksPath, nil
}

func uninstallCursor(repoRoot string) error {
	hooksPath := filepath.Join(repoRoot, CursorHooksRelPath)
	scriptPath := filepath.Join(repoRoot, CursorNudgeScriptRelPath)
	_ = os.Remove(scriptPath)

	doc, err := readSettings(hooksPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !removeCursorHook(doc) {
		return nil
	}
	return writeSettings(hooksPath, doc)
}

func cursorInstalled(repoRoot string) bool {
	hooksPath := filepath.Join(repoRoot, CursorHooksRelPath)
	doc, err := readSettings(hooksPath)
	if err != nil {
		return false
	}
	return findCursorHookIdx(doc) >= 0
}

// ── .cursor/hooks.json document surgery ──────────────────────────────────
//
// The Cursor hooks.json shape is:
//
//	{
//	  "hooks": {
//	    "beforeShellExecution": [
//	      { "command": "# <marker>\nsh \"<script>\"" }
//	    ]
//	  }
//	}
//
// We operate on map[string]any so every key we do not understand is preserved
// verbatim on the round-trip.

func cursorManagedEntry(scriptPath string) map[string]any {
	return map[string]any{
		// The Marker is embedded as a leading shell comment so it is inert at
		// runtime and reliably greppable for idempotent upsert.
		"command": fmt.Sprintf("# %s\nsh %q", Marker, scriptPath),
	}
}

func upsertCursorHook(doc map[string]any, scriptPath string) {
	entry := cursorManagedEntry(scriptPath)

	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	evt, _ := hooks[CursorHookEvent].([]any)

	if idx := findCursorHookInArr(evt); idx >= 0 {
		evt[idx] = entry
	} else {
		evt = append(evt, entry)
	}
	hooks[CursorHookEvent] = evt
	doc["hooks"] = hooks
}

func removeCursorHook(doc map[string]any) bool {
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	evt, _ := hooks[CursorHookEvent].([]any)
	idx := findCursorHookInArr(evt)
	if idx < 0 {
		return false
	}
	evt = append(evt[:idx], evt[idx+1:]...)
	if len(evt) == 0 {
		delete(hooks, CursorHookEvent)
	} else {
		hooks[CursorHookEvent] = evt
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	}
	return true
}

func findCursorHookIdx(doc map[string]any) int {
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		return -1
	}
	evt, _ := hooks[CursorHookEvent].([]any)
	return findCursorHookInArr(evt)
}

// findCursorHookInArr scans a beforeShellExecution array for the entry whose
// command carries our Marker. Returns its index, or -1.
func findCursorHookInArr(evt []any) int {
	for i, raw := range evt {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if cmd, _ := m["command"].(string); cmdHasMarker(cmd) {
			return i
		}
	}
	return -1
}
