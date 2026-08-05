package install_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/install"
)

// ── #6168 helpers ────────────────────────────────────────────────────────────

// writeConfigWithForeignServer seeds a config file with one non-grafel MCP
// server so every assertion below can distinguish "grafel entry removed" from
// "whole file clobbered".
func writeConfigWithForeignServer(t *testing.T, path string) {
	t.Helper()
	doc := map[string]any{
		"mcpServers": map[string]any{
			"someoneElse": map[string]any{"command": "/usr/bin/other", "type": "stdio"},
		},
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed config: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write seed config %s: %v", path, err)
	}
}

// mcpServersOf reads the mcpServers map out of a JSON config file.
func mcpServersOf(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v (content: %s)", path, err, data)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	return servers
}

func assertGrafelPresent(t *testing.T, path, why string) {
	t.Helper()
	servers := mcpServersOf(t, path)
	if servers == nil {
		t.Fatalf("%s: mcpServers key is absent from %s — the whole registration was dropped", why, path)
	}
	if _, ok := servers["grafel"]; !ok {
		t.Fatalf("%s: grafel entry missing from %s (servers: %v)", why, path, servers)
	}
}

func assertGrafelAbsent(t *testing.T, path, why string) {
	t.Helper()
	servers := mcpServersOf(t, path)
	if servers == nil {
		return
	}
	if _, ok := servers["grafel"]; ok {
		t.Fatalf("%s: grafel entry still present in %s", why, path)
	}
}

func assertForeignServerIntact(t *testing.T, path, why string) {
	t.Helper()
	servers := mcpServersOf(t, path)
	if servers == nil {
		t.Fatalf("%s: mcpServers key is absent from %s — foreign server lost", why, path)
	}
	if _, ok := servers["someoneElse"]; !ok {
		t.Fatalf("%s: foreign server 'someoneElse' lost from %s (servers: %v)", why, path, servers)
	}
}

// ── 1. A step-4 (daemon) failure must NOT un-register MCP ────────────────────

// TestRunCopy_Step4Failure_KeepsMCPRegistration is the core #6168 regression.
//
// Registering an MCP server is not undone by a daemon that failed to restart:
// the entry is correct either way, and the bridge tolerates a down daemon
// (internal/cli/mcp_bridge.go answers `initialize` locally and falls back to
// offlineToolList). Deleting a working registration because an unrelated later
// step failed is strictly worse for the user than leaving it.
func TestRunCopy_Step4Failure_KeepsMCPRegistration(t *testing.T) {
	env := newTestEnv(t)
	writeConfigWithForeignServer(t, env.claudeJSON)

	opts := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: false, // let step 4 run and fail
		RestartDaemon: func(_ string, _ int, _ time.Duration) (string, error) {
			return "", fmt.Errorf("injected daemon restart failure (#6168)")
		},
	}

	if _, err := install.RunCopy(opts); err == nil {
		t.Fatal("expected RunCopy to fail when the daemon restart fails")
	}

	assertGrafelPresent(t, env.claudeJSON, "after a step-4 daemon failure")
	assertForeignServerIntact(t, env.claudeJSON, "after a step-4 daemon failure")
	assertMCPRegistered(t, env.claudeJSON, env.fakeBin)
}

// TestRunCopy_Step4VerifyFailure_KeepsMCPRegistration covers the OTHER step-4
// exit: the restart "succeeds" but the running daemon reports the wrong
// version and the escalated restart still cannot fix it (#5850 path). This is
// exactly the shape a #6167 reindex stampede produces on a saturated box.
func TestRunCopy_Step4VerifyFailure_KeepsMCPRegistration(t *testing.T) {
	env := newTestEnv(t)
	writeConfigWithForeignServer(t, env.claudeJSON)

	opts := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: false,
		InstalledVersion:  "v9.9.9",
		RestartDaemon: func(_ string, _ int, _ time.Duration) (string, error) {
			return "ok", nil
		},
		EscalateDaemonRestart: func(_ string, _ int, _ time.Duration) (string, error) {
			return "ok", nil
		},
		ProbeDaemonVersion: func() (install.ProbedVersion, error) {
			// never matches v9.9.9 -> verify fails before and after escalation
			return install.ProbedVersion{Display: "v0.0.0-stale", Bare: "v0.0.0-stale"}, nil
		},
	}

	if _, err := install.RunCopy(opts); err == nil {
		t.Fatal("expected RunCopy to fail when the daemon version never verifies")
	}

	assertGrafelPresent(t, env.claudeJSON, "after a step-4 version-verify failure")
	assertForeignServerIntact(t, env.claudeJSON, "after a step-4 version-verify failure")
}

// TestRunCopy_Step4Failure_KeepsAPreexistingGrafelEntry is the reporter's own
// situation: grafel was ALREADY registered, they re-ran `grafel install` on the
// advice of install.sh, the daemon did not come back, and their MCP server was
// gone afterwards.
//
// This test separates the two defects that combine to produce that outcome.
// With the sidecar backup still alive, a rollback would have RESTORED the
// pre-existing entry (present, old command) — bad but survivable. With the
// backup already discarded at step 3, RestoreSnapshot degraded to UnregisterPath
// and the entry was DELETED outright. Asserting the entry is present at all
// therefore fails only under the full original code.
func TestRunCopy_Step4Failure_KeepsAPreexistingGrafelEntry(t *testing.T) {
	env := newTestEnv(t)
	doc := map[string]any{
		"mcpServers": map[string]any{
			"grafel": map[string]any{"command": "/old/path/grafel", "args": []any{"mcp-bridge"}, "type": "stdio"},
		},
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(env.claudeJSON, b, 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	opts := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: false,
		RestartDaemon: func(_ string, _ int, _ time.Duration) (string, error) {
			return "", fmt.Errorf("injected daemon restart failure (#6168)")
		},
	}

	if _, err := install.RunCopy(opts); err == nil {
		t.Fatal("expected RunCopy to fail when the daemon restart fails")
	}

	assertGrafelPresent(t, env.claudeJSON, "after a step-4 failure over a pre-existing registration")
	// The registration must also point at the binary the install just wrote,
	// not be silently reverted to the stale one.
	assertMCPRegistered(t, env.claudeJSON, env.fakeBin)
}

// TestRunDev_Step4Failure_KeepsMCPRegistration mirrors the above for DEV mode,
// which carries a byte-identical copy of the same defect.
func TestRunDev_Step4Failure_KeepsMCPRegistration(t *testing.T) {
	env := newTestEnv(t)
	writeConfigWithForeignServer(t, env.claudeJSON)

	opts := install.DevOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: false,
		NoHooks:           true,
		RestartDaemon: func(_ string, _ int, _ time.Duration) (string, error) {
			return "", fmt.Errorf("injected daemon restart failure (#6168)")
		},
	}

	if _, err := install.RunDev(opts); err == nil {
		t.Fatal("expected RunDev to fail when the daemon restart fails")
	}

	assertGrafelPresent(t, env.claudeJSON, "after a DEV-mode step-4 daemon failure")
	assertForeignServerIntact(t, env.claudeJSON, "after a DEV-mode step-4 daemon failure")
}

// ── 2. A step-3 failure MID-WAY through the targets must clean up ────────────

// TestRunCopy_Step3PartialFailure_RestoresAlreadyWrittenTargets is the case
// that genuinely DOES need step 3 undone: the transaction aborts inside the
// registration loop, so some config files carry a grafel entry and some do
// not. Before the fix nothing cleaned those up at all — `rollback(3)` fires
// before `completedSteps` contains 3, so the `case 3` arm never ran, AND
// `state.MCP.RegisteredPaths` (the only list the arm consulted) was still
// empty because it is assigned only after the loop completes.
//
// The already-written file must come back byte-identical to its pre-install
// content: foreign server intact, no grafel entry, no orphan `{}`.
func TestRunCopy_Step3PartialFailure_RestoresAlreadyWrittenTargets(t *testing.T) {
	env := newTestEnv(t)

	// Target 1: a healthy config that WILL be written.
	good := env.claudeJSON
	writeConfigWithForeignServer(t, good)
	pristine, err := os.ReadFile(good)
	if err != nil {
		t.Fatalf("read pristine: %v", err)
	}

	// Target 2: malformed JSON, so RegisterPath fails on it after target 1
	// has already been modified.
	bad := filepath.Join(filepath.Dir(good), "broken", ".claude.json")
	if err := os.MkdirAll(filepath.Dir(bad), 0o755); err != nil {
		t.Fatalf("mkdir bad: %v", err)
	}
	if err := os.WriteFile(bad, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatalf("write bad config: %v", err)
	}

	opts := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{good, bad},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: true,
	}

	if _, err := install.RunCopy(opts); err == nil {
		t.Fatal("expected RunCopy to fail when a step-3 target cannot be registered")
	}

	got, err := os.ReadFile(good)
	if err != nil {
		t.Fatalf("read good config after rollback: %v", err)
	}
	if string(got) != string(pristine) {
		t.Errorf("step-3 partial failure did not restore the already-written target.\n got: %s\nwant: %s", got, pristine)
	}
	assertGrafelAbsent(t, good, "after a step-3 partial failure")
	assertForeignServerIntact(t, good, "after a step-3 partial failure")
}

// ── 3. The sidecar backup must outlive step 3 ────────────────────────────────

// TestRunCopy_SidecarBackupSurvivesUntilCommit pins the lifetime invariant
// that makes the restore-becomes-delete trap unreachable: the pristine
// `.grafel.bak` sidecar is discarded only when the WHOLE transaction commits.
// While the transaction is still in flight (here: aborted at step 4) the
// snapshot must still be on disk, so any restore that does run restores rather
// than degrading to a delete.
func TestRunCopy_SidecarBackupSurvivesUntilCommit(t *testing.T) {
	env := newTestEnv(t)
	writeConfigWithForeignServer(t, env.claudeJSON)
	sidecar := env.claudeJSON + ".grafel.bak"

	failing := install.CopyOptions{
		BinPath:           env.fakeBin,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkipDaemonRestart: false,
		RestartDaemon: func(_ string, _ int, _ time.Duration) (string, error) {
			return "", fmt.Errorf("injected daemon restart failure (#6168)")
		},
	}
	if _, err := install.RunCopy(failing); err == nil {
		t.Fatal("expected RunCopy to fail at step 4")
	}
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("sidecar backup %s was discarded while the transaction was still in flight: %v", sidecar, err)
	}

	// And on a committed install it IS cleared, so the next install snapshots
	// fresh and a later restore never resurrects grafel-containing content.
	ok := failing
	ok.SkipDaemonRestart = true
	ok.RestartDaemon = nil
	ok.Force = true
	if _, err := install.RunCopy(ok); err != nil {
		t.Fatalf("second RunCopy (committing): %v", err)
	}
	if _, err := os.Stat(sidecar); err == nil {
		t.Errorf("sidecar backup %s still present after a committed install", sidecar)
	}
}

// ── 4. `grafel update`'s failure path must not leave the user MCP-less ───────

// TestRunUpdate_ReinstallFailure_LeavesMCPRegistered covers update.go's error
// path: on a RunCopy error only rollbackBinary() runs, so nothing re-registers
// MCP. The user must still end up with a working binary AND a working
// registration pointing at it.
func TestRunUpdate_ReinstallFailure_LeavesMCPRegistered(t *testing.T) {
	env := newTestEnv(t)
	writeConfigWithForeignServer(t, env.claudeJSON)

	newBin := filepath.Join(t.TempDir(), "new-grafel")
	if err := os.WriteFile(newBin, []byte("#!/bin/sh\necho new-grafel"), 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	opts := install.UpdateOptions{
		BinPath:          env.fakeBin,
		StatePath:        env.statePath,
		WorkingDir:       env.gitRepo,
		SkillsSourceDir:  env.skillsSourceDir,
		ClaudeConfigDirs: []string{env.claudeJSON},
		Tag:              "v0.0.1-test",
		DownloadBinary: func(_ *http.Client, _, _, _, destPath string) error {
			return copyTestFile(newBin, destPath)
		},
		// Step 4 of the re-install fails — the #6167 saturated-daemon shape.
		SkipDaemonRestart: false,
		RestartDaemon: func(_ string, _ int, _ time.Duration) (string, error) {
			return "", fmt.Errorf("injected daemon restart failure (#6168)")
		},
	}

	if _, err := install.RunUpdate(opts); err == nil {
		t.Fatal("expected RunUpdate to fail when the re-install's daemon restart fails")
	}

	// The old binary is back at the same path...
	if _, err := os.Stat(env.fakeBin); err != nil {
		t.Fatalf("binary missing after update rollback: %v", err)
	}
	// ...and the MCP entry still points at it.
	assertGrafelPresent(t, env.claudeJSON, "after a failed grafel update")
	assertForeignServerIntact(t, env.claudeJSON, "after a failed grafel update")
	assertMCPRegistered(t, env.claudeJSON, env.fakeBin)
}
