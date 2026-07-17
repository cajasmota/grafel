package install_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/install"
)

// TestRunUpdate_HappyPath verifies a full update cycle:
// - a "new" binary is downloaded (injected stub)
// - the binary is atomically replaced
// - RunCopy is re-run with Force=true
// - the stash file is cleaned up
func TestRunUpdate_HappyPath(t *testing.T) {
	env := newTestEnv(t)

	// Write a "new" binary content (different from fakeBin) to download.
	newContent := []byte("#!/bin/sh\necho new-grafel")
	newBinPath := filepath.Join(t.TempDir(), "new-grafel")
	if err := os.WriteFile(newBinPath, newContent, 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	opts := install.UpdateOptions{
		BinPath:           env.fakeBin,
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		SkipDaemonRestart: true,
		Tag:               "v0.0.1-test",
		DownloadBinary: func(_ *http.Client, _, _, _, destPath string) error {
			// Copy our "new" binary content to destPath.
			return copyTestFile(newBinPath, destPath)
		},
	}

	result, err := install.RunUpdate(opts)
	if err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}

	if result.Skipped {
		t.Error("expected update to proceed, but got Skipped=true")
	}

	if result.Tag != "v0.0.1-test" {
		t.Errorf("Tag = %q, want %q", result.Tag, "v0.0.1-test")
	}

	// Verify the binary was replaced.
	data, err := os.ReadFile(env.fakeBin)
	if err != nil {
		t.Fatalf("read updated binary: %v", err)
	}
	if string(data) != string(newContent) {
		t.Errorf("binary content = %q, want %q", data, newContent)
	}

	// Verify stash was cleaned up.
	stashPath := env.fakeBin + ".prev"
	if _, err := os.Stat(stashPath); err == nil {
		t.Error("stash file .prev still exists after successful update")
	}

	// Verify install.json was re-written.
	state := readState(t, env.statePath)
	if state == nil {
		t.Fatal("install.json not found after update")
	}
	if state.PartialInstall {
		t.Error("install.json: partial_install should be false after successful update")
	}

	// Install result should be present.
	if result.InstallResult == nil {
		t.Error("InstallResult is nil after update")
	}
}

// TestRunUpdate_Idempotent verifies that updating to the same version is a no-op.
func TestRunUpdate_Idempotent(t *testing.T) {
	env := newTestEnv(t)

	// The "downloaded" binary is identical to the current one.
	sameContent, err := os.ReadFile(env.fakeBin)
	if err != nil {
		t.Fatalf("read fakeBin: %v", err)
	}

	opts := install.UpdateOptions{
		BinPath:           env.fakeBin,
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		SkipDaemonRestart: true,
		Tag:               "v0.0.1-same",
		DownloadBinary: func(_ *http.Client, _, _, _, destPath string) error {
			return os.WriteFile(destPath, sameContent, 0o755)
		},
	}

	result, err := install.RunUpdate(opts)
	if err != nil {
		t.Fatalf("RunUpdate (idempotent): %v", err)
	}

	if !result.Skipped {
		t.Error("expected Skipped=true when binary is already at target version")
	}
}

// TestRunUpdate_RollbackOnInstallFailure verifies that when re-install fails
// after the binary has been replaced, the previous binary is restored.
func TestRunUpdate_RollbackOnInstallFailure(t *testing.T) {
	env := newTestEnv(t)

	origContent, err := os.ReadFile(env.fakeBin)
	if err != nil {
		t.Fatalf("read original binary: %v", err)
	}
	origSHA, err := install.SHA256FilePublic(env.fakeBin)
	if err != nil {
		t.Fatalf("sha original binary: %v", err)
	}

	opts := install.UpdateOptions{
		BinPath:          env.fakeBin,
		StatePath:        env.statePath,
		SkillsSourceDir:  env.skillsSourceDir,
		ClaudeConfigDirs: []string{env.claudeJSON},
		WorkingDir:       env.gitRepo,
		// Force the re-install (RunCopy) to fail at the daemon-restart step so
		// the binary rollback path is exercised. (Skills discovery now
		// gracefully degrades — #4460 — so it can no longer be used to force a
		// hard install failure.)
		SkipDaemonRestart: false,
		RestartDaemon: func(_ string, _ int, _ time.Duration) (string, error) {
			return "", fmt.Errorf("forced daemon restart failure for test")
		},
		Tag: "v0.0.1-fail",
		DownloadBinary: func(_ *http.Client, _, _, _, destPath string) error {
			// Download a different binary.
			return os.WriteFile(destPath, []byte("#!/bin/sh\necho different"), 0o755)
		},
	}

	_, err = install.RunUpdate(opts)
	if err == nil {
		t.Fatal("expected RunUpdate to fail when re-install fails")
	}

	// Verify the binary was rolled back.
	restoredContent, err := os.ReadFile(env.fakeBin)
	if err != nil {
		t.Fatalf("read binary after rollback: %v", err)
	}
	if string(restoredContent) != string(origContent) {
		t.Errorf("binary content after rollback = %q, want original %q",
			restoredContent[:min(20, len(restoredContent))],
			origContent[:min(20, len(origContent))])
	}

	restoredSHA, err := install.SHA256FilePublic(env.fakeBin)
	if err != nil {
		t.Fatalf("sha binary after rollback: %v", err)
	}
	if restoredSHA != origSHA {
		t.Errorf("SHA after rollback = %s, want %s", restoredSHA[:16], origSHA[:16])
	}

	// Stash should be gone (either restored or cleaned up).
	stashPath := env.fakeBin + ".prev"
	if _, err := os.Stat(stashPath); err == nil {
		t.Error("stash file .prev still exists after rollback")
	}
}

// TestRunUpdate_VerifiesAgainstTargetTag_NotRunningVersion is the regression
// guard for the CRITICAL #5850 update inversion: RunUpdate replaces the binary
// IN-PROCESS (no re-exec), so this process keeps reporting the OLD binary's
// version.Version. The daemon it restarts, however, runs the DOWNLOADED release
// and reports the TARGET tag. If RunUpdate lets RunCopy default
// InstalledVersion to the running (old) version.Version, the post-restart
// version check mismatches on EVERY real update → escalate → still mismatch →
// error → rollback to the old binary. This test drives the update path with a
// stubbed daemon that reports the TARGET tag and asserts success WITHOUT
// setting InstalledVersion explicitly — proving update.go threads the tag.
//
// Without the fix (InstalledVersion defaulting to version.Version, e.g.
// "0.0.0-dev" in the test binary), the probe returns the tag, the versions
// mismatch, escalation runs, the re-probe still returns the tag, and RunUpdate
// returns an error — so this test fails red until update.go sets
// InstalledVersion: tag.
func TestRunUpdate_VerifiesAgainstTargetTag_NotRunningVersion(t *testing.T) {
	env := newTestEnv(t)

	// A "new" binary distinct from the current one so the update proceeds past
	// the identical-SHA fast-path.
	newContent := []byte("#!/bin/sh\necho new-release-binary")
	newBinPath := filepath.Join(t.TempDir(), "new-grafel")
	if err := os.WriteFile(newBinPath, newContent, 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	const tag = "v9.9.9-tagtest" // deliberately != this test binary's version.Version

	escalateCalled := false
	opts := install.UpdateOptions{
		BinPath:           env.fakeBin,
		StatePath:         env.statePath,
		WorkingDir:        env.gitRepo,
		SkillsSourceDir:   env.skillsSourceDir,
		ClaudeConfigDirs:  []string{env.claudeJSON},
		SkipDaemonRestart: false, // exercise the real step-4 version check
		Tag:               tag,
		DownloadBinary: func(_ *http.Client, _, _, _, destPath string) error {
			return copyTestFile(newBinPath, destPath)
		},
		// The restarted daemon runs the downloaded release and reports the tag.
		RestartDaemon: func(_ string, _ int, _ time.Duration) (string, error) {
			return tag, nil
		},
		ProbeDaemonVersion: func() (string, error) {
			return tag, nil
		},
		EscalateDaemonRestart: func(_ string, _ int, _ time.Duration) (string, error) {
			escalateCalled = true
			return tag, nil
		},
		// NOTE: InstalledVersion intentionally NOT set — the fix must make
		// update.go default it to the target tag.
	}

	result, err := install.RunUpdate(opts)
	if err != nil {
		t.Fatalf("RunUpdate must succeed when the daemon reports the target tag (#5850 update inversion): %v", err)
	}
	if escalateCalled {
		t.Error("escalation must NOT run when the restarted daemon already reports the target tag")
	}
	if result.InstallResult == nil {
		t.Fatal("InstallResult is nil after a successful update")
	}
	if result.InstallResult.DaemonVersion != tag {
		t.Errorf("recorded daemon version = %q, want the target tag %q",
			result.InstallResult.DaemonVersion, tag)
	}

	// The binary must be the new one (i.e. no rollback happened).
	got, err := os.ReadFile(env.fakeBin)
	if err != nil {
		t.Fatalf("read binary after update: %v", err)
	}
	if string(got) != string(newContent) {
		t.Error("binary was rolled back to the old version — the update was spuriously inverted")
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func copyTestFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
