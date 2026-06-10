package install_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/install"
	"github.com/cajasmota/archigraph/internal/install/skilllink"
)

// TestRunUninstall_HappyPath verifies the full uninstall flow:
// - skills are removed
// - MCP is deregistered
// - install.json is removed
// - CLI binary is removed (--yes)
func TestRunUninstall_HappyPath(t *testing.T) {
	env := newTestEnv(t)

	// First run a full install so there is a valid install.json.
	_, err := install.RunCopy(install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	})
	if err != nil {
		t.Fatalf("RunCopy: %v", err)
	}

	// Confirm skills are present (check all canonical skills).
	destSkillsDir := filepath.Join(filepath.Dir(env.claudeJSON), "skills")
	for _, name := range skilllink.SkillNames {
		dst := filepath.Join(destSkillsDir, name)
		if _, err := os.Stat(dst); err != nil {
			t.Fatalf("skill %s should exist before uninstall: %v", name, err)
		}
	}

	// Run uninstall.
	result, err := install.RunUninstall(install.UninstallOptions{
		StatePath:      env.statePath,
		Yes:            true, // skip confirmation
		SkipDaemonStop: true,
	})
	if err != nil {
		t.Fatalf("RunUninstall: %v", err)
	}

	// Skills should be removed.
	if len(result.SkillsRemoved) == 0 {
		t.Error("no skills reported as removed")
	}
	for _, name := range result.SkillsRemoved {
		dst := filepath.Join(destSkillsDir, name)
		if _, err := os.Stat(dst); err == nil {
			t.Errorf("skill %s still exists after uninstall", name)
		}
	}

	// MCP should be deregistered.
	if len(result.MCPPaths) == 0 {
		t.Error("no MCP paths reported as deregistered")
	}
	assertMCPDeregistered(t, env.claudeJSON)

	// CLI binary should be removed.
	if !result.BinaryRemoved {
		t.Error("BinaryRemoved should be true")
	}
	if _, err := os.Stat(env.fakeBin); err == nil {
		t.Error("CLI binary still exists after uninstall")
	}

	// install.json should be removed.
	if !result.StateRemoved {
		t.Error("StateRemoved should be true")
	}
	if _, err := os.Stat(env.statePath); err == nil {
		t.Error("install.json still exists after uninstall")
	}
}

// TestRunUninstall_Idempotent verifies that uninstalling twice is safe.
func TestRunUninstall_Idempotent(t *testing.T) {
	env := newTestEnv(t)

	// First install.
	if _, err := install.RunCopy(install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	}); err != nil {
		t.Fatalf("RunCopy: %v", err)
	}

	// First uninstall.
	if _, err := install.RunUninstall(install.UninstallOptions{
		StatePath:      env.statePath,
		Yes:            true,
		SkipDaemonStop: true,
	}); err != nil {
		t.Fatalf("first RunUninstall: %v", err)
	}

	// Second uninstall — should succeed without error.
	if _, err := install.RunUninstall(install.UninstallOptions{
		StatePath:      env.statePath,
		Yes:            true,
		SkipDaemonStop: true,
	}); err != nil {
		t.Fatalf("second RunUninstall (idempotency): %v", err)
	}
}

// TestRunUninstall_Purge verifies that --purge also removes store/ and docs/.
func TestRunUninstall_Purge(t *testing.T) {
	env := newTestEnv(t)

	// First install.
	if _, err := install.RunCopy(install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	}); err != nil {
		t.Fatalf("RunCopy: %v", err)
	}

	// Create fake store/ and docs/ directories.
	archigraphDir := filepath.Dir(env.statePath)
	storePath := filepath.Join(archigraphDir, "store")
	docsPath := filepath.Join(archigraphDir, "docs")
	for _, p := range []string{storePath, docsPath} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("create %s: %v", p, err)
		}
		// Put a sentinel file inside to confirm removal.
		if err := os.WriteFile(filepath.Join(p, "sentinel"), []byte("data"), 0o644); err != nil {
			t.Fatalf("write sentinel in %s: %v", p, err)
		}
	}

	result, err := install.RunUninstall(install.UninstallOptions{
		StatePath:      env.statePath,
		Purge:          true,
		Yes:            true,
		SkipDaemonStop: true,
	})
	if err != nil {
		t.Fatalf("RunUninstall --purge: %v", err)
	}

	if !result.StoreRemoved {
		t.Error("StoreRemoved should be true with --purge")
	}
	if !result.DocsRemoved {
		t.Error("DocsRemoved should be true with --purge")
	}
	if _, err := os.Stat(storePath); err == nil {
		t.Error("store/ still exists after --purge")
	}
	if _, err := os.Stat(docsPath); err == nil {
		t.Error("docs/ still exists after --purge")
	}
}

// TestRunUninstall_NoBinary verifies that when no binary confirmation is
// needed (binary doesn't exist), the command still completes cleanly.
func TestRunUninstall_NoBinary(t *testing.T) {
	env := newTestEnv(t)

	// Install, then remove the binary manually.
	if _, err := install.RunCopy(install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	}); err != nil {
		t.Fatalf("RunCopy: %v", err)
	}

	// Remove the binary before uninstall.
	if err := os.Remove(env.fakeBin); err != nil {
		t.Fatalf("remove binary: %v", err)
	}

	_, err := install.RunUninstall(install.UninstallOptions{
		StatePath:      env.statePath,
		Yes:            true,
		SkipDaemonStop: true,
	})
	if err != nil {
		t.Fatalf("RunUninstall when binary already missing: %v", err)
	}
}

// TestRunUninstall_ConfirmNo verifies that when the user says "N" to the
// binary removal prompt, the binary is kept.
func TestRunUninstall_ConfirmNo(t *testing.T) {
	env := newTestEnv(t)

	if _, err := install.RunCopy(install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	}); err != nil {
		t.Fatalf("RunCopy: %v", err)
	}

	// Inject a "No" confirmation.
	result, err := install.RunUninstall(install.UninstallOptions{
		StatePath:      env.statePath,
		Yes:            false,
		SkipDaemonStop: true,
		ConfirmFn: func(string) (bool, error) {
			return false, nil // user said N
		},
	})
	if err != nil {
		t.Fatalf("RunUninstall: %v", err)
	}

	if result.BinaryRemoved {
		t.Error("BinaryRemoved should be false when user declined")
	}
	// Binary should still exist.
	if _, err := os.Stat(env.fakeBin); err != nil {
		t.Errorf("binary should still exist after user declined: %v", err)
	}
}

// TestRunUninstall_NonTTYAutoYes verifies the #4462 fix: with no --yes and no
// injected ConfirmFn, a non-interactive stdin (as in `go test`, CI, agents)
// auto-confirms binary removal rather than blocking on the prompt. The test
// completing at all proves there is no hang; we also assert the binary is gone.
func TestRunUninstall_NonTTYAutoYes(t *testing.T) {
	env := newTestEnv(t)

	if _, err := install.RunCopy(install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	}); err != nil {
		t.Fatalf("RunCopy: %v", err)
	}

	// Force a non-interactive stdin (a pipe is not a character device), so the
	// test is deterministic regardless of how `go test` is invoked. With
	// Yes:false and ConfirmFn:nil, the uninstall must auto-confirm. If the
	// auto-yes were missing, promptConfirm would read EOF and decline (binary
	// kept) — or block on a real terminal.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	w.Close() // immediate EOF; this would make promptConfirm decline if reached
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin; r.Close() })

	result, err := install.RunUninstall(install.UninstallOptions{
		StatePath:      env.statePath,
		SkipDaemonStop: true,
	})
	if err != nil {
		t.Fatalf("RunUninstall (non-tty auto-yes): %v", err)
	}
	if !result.BinaryRemoved {
		t.Error("BinaryRemoved should be true under non-interactive stdin (auto-yes)")
	}
	if _, err := os.Stat(env.fakeBin); err == nil {
		t.Error("binary should be removed under non-interactive auto-yes")
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func assertMCPDeregistered(t *testing.T, claudeJSON string) {
	t.Helper()
	data, err := os.ReadFile(claudeJSON)
	if err != nil {
		// File may have been removed — that's fine.
		return
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse .claude.json: %v", err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		return // no mcpServers key — already gone
	}
	if _, ok := servers["archigraph"]; ok {
		t.Error(".claude.json: archigraph entry still present after MCP deregistration")
	}
}
