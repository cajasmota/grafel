package agenthooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// hostByName fetches a registered host by display name, failing the test if
// it is absent.
func hostByName(t *testing.T, name string) Host {
	t.Helper()
	for _, h := range Registry() {
		if h.Name() == name {
			return h
		}
	}
	t.Fatalf("host %q not in registry", name)
	return nil
}

// TestRegistry_CapabilityMatrix asserts the honest per-host hook-capability
// matrix: which hosts have a real pre-tool hook vs documented instruction-only.
func TestRegistry_CapabilityMatrix(t *testing.T) {
	want := map[string]bool{
		"Claude Code":    true,
		"Cursor":         true,
		"Windsurf":       false,
		"Codeium":        false,
		"GitHub Copilot": false,
	}
	got := map[string]bool{}
	for _, c := range Capabilities() {
		got[c.Name] = c.SupportsHook
		// No-hook hosts must explain why; hooking hosts must not.
		if c.SupportsHook && c.NoHookReason != "" {
			t.Errorf("%s supports a hook but has a NoHookReason: %q", c.Name, c.NoHookReason)
		}
		if !c.SupportsHook && c.NoHookReason == "" {
			t.Errorf("%s has no hook but no NoHookReason explaining the gap", c.Name)
		}
	}
	for name, sup := range want {
		if got[name] != sup {
			t.Errorf("host %q SupportsHook = %v, want %v", name, got[name], sup)
		}
	}
	if len(got) != len(want) {
		t.Errorf("registry hosts = %v, want exactly %v", got, want)
	}
}

// TestClaudeCodeHost_HookInstalled verifies the existing Claude Code behavior
// is preserved through the Host interface: a real hook file is written.
func TestClaudeCodeHost_HookInstalled(t *testing.T) {
	root := t.TempDir()
	h := hostByName(t, "Claude Code")

	p, err := h.InstallHook(root)
	if err != nil {
		t.Fatalf("InstallHook: %v", err)
	}
	if p != filepath.Join(root, SettingsRelPath) {
		t.Fatalf("hook path = %q, want %q", p, filepath.Join(root, SettingsRelPath))
	}
	if !h.IsHookInstalled(root) {
		t.Fatal("IsHookInstalled = false after InstallHook")
	}
	if _, err := os.Stat(filepath.Join(root, NudgeScriptRelPath)); err != nil {
		t.Fatalf("nudge script not written: %v", err)
	}
}

// TestCursorHost_RealHookWritten verifies a host with a real hook mechanism
// writes its hook file (.cursor/hooks.json) into a faked install layout, with
// the shared nudge script and the once-per-host marker. Also covers idempotent
// re-install (no duplicate) and user-content preservation.
func TestCursorHost_RealHookWritten(t *testing.T) {
	root := t.TempDir()
	h := hostByName(t, "Cursor")

	// Seed a user-authored Cursor hook + an unrelated key that must survive.
	seed := map[string]any{
		"version": 1,
		"hooks": map[string]any{
			"beforeShellExecution": []any{
				map[string]any{"command": "echo user-cursor-hook"},
			},
			"afterFileEdit": []any{
				map[string]any{"command": "echo unrelated"},
			},
		},
	}
	seedBytes, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.MkdirAll(filepath.Join(root, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, CursorHooksRelPath), seedBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// Install three times — must be idempotent.
	var p string
	for i := 0; i < 3; i++ {
		var err error
		p, err = h.InstallHook(root)
		if err != nil {
			t.Fatalf("InstallHook #%d: %v", i, err)
		}
	}
	if p != filepath.Join(root, CursorHooksRelPath) {
		t.Fatalf("hook path = %q, want %q", p, filepath.Join(root, CursorHooksRelPath))
	}
	if !h.IsHookInstalled(root) {
		t.Fatal("IsHookInstalled = false after InstallHook")
	}
	// Shared nudge script written for Cursor.
	if _, err := os.Stat(filepath.Join(root, CursorNudgeScriptRelPath)); err != nil {
		t.Fatalf("cursor nudge script not written: %v", err)
	}

	doc := readCursorDoc(t, root)
	hooks := doc["hooks"].(map[string]any)
	evt := hooks[CursorHookEvent].([]any)

	// Exactly one managed entry (no duplication across 3 installs) + the user's.
	managed, user := 0, 0
	for _, raw := range evt {
		m := raw.(map[string]any)
		cmd, _ := m["command"].(string)
		if containsMarker(cmd) {
			managed++
		} else if cmd == "echo user-cursor-hook" {
			user++
		}
	}
	if managed != 1 {
		t.Fatalf("managed cursor entries = %d, want 1", managed)
	}
	if user != 1 {
		t.Fatalf("user cursor hook preserved = %d, want 1", user)
	}
	// Unrelated keys + events preserved.
	if doc["version"] != float64(1) {
		t.Fatalf("unrelated version key lost: %v", doc["version"])
	}
	if _, ok := hooks["afterFileEdit"]; !ok {
		t.Fatal("unrelated afterFileEdit event dropped")
	}

	// Uninstall removes our entry + script, leaves the user's hook.
	if err := h.UninstallHook(root); err != nil {
		t.Fatalf("UninstallHook: %v", err)
	}
	if h.IsHookInstalled(root) {
		t.Fatal("still installed after UninstallHook")
	}
	if _, err := os.Stat(filepath.Join(root, CursorNudgeScriptRelPath)); !os.IsNotExist(err) {
		t.Fatalf("cursor nudge script not removed: %v", err)
	}
	doc2 := readCursorDoc(t, root)
	evt2 := doc2["hooks"].(map[string]any)[CursorHookEvent].([]any)
	if len(evt2) != 1 || evt2[0].(map[string]any)["command"] != "echo user-cursor-hook" {
		t.Fatalf("user cursor hook not preserved on uninstall: %v", evt2)
	}
	// Idempotent second uninstall.
	if err := h.UninstallHook(root); err != nil {
		t.Fatalf("second UninstallHook: %v", err)
	}
}

// TestInstructionOnlyHost_NoHookFile verifies a no-hook host writes NO hook
// file and never errors — the rules file (written elsewhere) carries the
// guidance. We assert the absence of any host-specific hook artifacts.
func TestInstructionOnlyHost_NoHookFile(t *testing.T) {
	for _, name := range []string{"Windsurf", "Codeium", "GitHub Copilot"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			h := hostByName(t, name)

			p, err := h.InstallHook(root)
			if err != nil {
				t.Fatalf("InstallHook: %v", err)
			}
			if p != "" {
				t.Fatalf("no-hook host wrote a hook path %q, want empty", p)
			}
			if h.IsHookInstalled(root) {
				t.Fatal("no-hook host reports IsHookInstalled = true")
			}
			// No hook files of any kind were created.
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatalf("no-hook host created files %v, want none", entries)
			}
			// Honest gap is documented.
			if h.NoHookReason() == "" {
				t.Fatal("no-hook host missing NoHookReason")
			}
			// Uninstall is a no-op (no error).
			if err := h.UninstallHook(root); err != nil {
				t.Fatalf("UninstallHook no-op errored: %v", err)
			}
		})
	}
}

// TestPackageInstall_AllHosts verifies the package-level Install installs every
// hooking host (Claude Code + Cursor) and skips no-hook hosts, idempotently.
func TestPackageInstall_AllHosts(t *testing.T) {
	root := t.TempDir()

	for i := 0; i < 2; i++ {
		paths, err := Install(root)
		if err != nil {
			t.Fatalf("Install #%d: %v", i, err)
		}
		// Two hooking hosts → two written paths (Claude Code first for
		// back-compat with callers that read paths[0]).
		if len(paths) != 2 {
			t.Fatalf("Install wrote %d paths, want 2: %v", len(paths), paths)
		}
		if paths[0] != filepath.Join(root, SettingsRelPath) {
			t.Fatalf("paths[0] = %q, want Claude Code settings path", paths[0])
		}
	}

	if !IsInstalled(root) {
		t.Fatal("IsInstalled = false after Install")
	}
	// Both host hook files exist.
	for _, rel := range []string{SettingsRelPath, CursorHooksRelPath} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("expected hook file %s: %v", rel, err)
		}
	}

	if err := Uninstall(root); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if IsInstalled(root) {
		t.Fatal("IsInstalled = true after Uninstall")
	}
}

func readCursorDoc(t *testing.T, root string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, CursorHooksRelPath))
	if err != nil {
		t.Fatalf("read cursor hooks: %v", err)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse cursor hooks: %v", err)
	}
	return doc
}
