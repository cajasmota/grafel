// doctor_quick_remedy_test.go pins the second half of the curl-upgrade defect:
// the one-line quick-doctor warning must NAME THE COMMAND THAT FIXES IT.
//
// As shipped, a user who upgraded via the curl installer saw, on EVERY grafel
// command:
//
//	grafel doctor: binary updated since last install (daemon still usable);
//	daemon unreachable at :47274 — run 'grafel doctor' for details
//
// The only prescription is `grafel doctor`, which re-prints the same line — the
// message is a closed loop. It also cannot distinguish "the binary at the
// recorded path was replaced" (the curl-upgrade case, fixed by re-recording it)
// from "you are running a DIFFERENT binary than the one install.json records"
// (two installs on PATH), which needs a different mental model entirely.
package install_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/install"
)

// hashFile returns the hex SHA-256 of path's contents (the same value
// install.RefreshState / step 1 of RunCopy record in install.json).
func hashFile(t *testing.T, path string) string {
	t.Helper()
	sum, err := install.SHA256FilePublic(path)
	if err != nil {
		t.Fatalf("hash %s: %v", path, err)
	}
	return sum
}

// quickEnv builds a temp HOME with an install.json recording binPath at the
// SHA of its current contents.
func quickEnv(t *testing.T) (statePath, binPath string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	statePath = filepath.Join(tmp, ".grafel", "install.json")
	binPath = filepath.Join(tmp, "bin", "grafel")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho original\n"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	st := install.NewState(install.ModeCopy)
	st.CLI = install.CLIRecord{Path: binPath, SHA256: hashFile(t, binPath)}
	if err := install.WriteState(statePath, st); err != nil {
		t.Fatalf("write state: %v", err)
	}
	return statePath, binPath
}

// runQuick runs quick-doctor against a dead daemon port as a RELEASE build and
// returns the output. (Under `go test` version.Version is the dev sentinel;
// these cases are about what a user who installed a release sees.)
func runQuick(t *testing.T, statePath, selfPath string) string {
	t.Helper()
	return runQuickAs(t, statePath, selfPath, "0.2.0")
}

func runQuickAs(t *testing.T, statePath, selfPath, ver string) string {
	t.Helper()
	var buf bytes.Buffer
	err := install.RunQuickDoctor(install.QuickOptions{
		StatePath:     statePath,
		SelfPath:      selfPath,
		Version:       ver,
		DaemonPort:    1,
		DaemonTimeout: 100 * time.Millisecond,
		Out:           &buf,
	})
	if err != nil {
		t.Fatalf("RunQuickDoctor must never return an error: %v", err)
	}
	return buf.String()
}

// TestQuickDoctor_SHADriftNamesTheFixCommand: when the binary at the RECORDED
// path was replaced in place (exactly what `install -m 0755` in install.sh
// does), the warning must tell the user the one command that clears it.
func TestQuickDoctor_SHADriftNamesTheFixCommand(t *testing.T) {
	statePath, binPath := quickEnv(t)

	// Simulate the curl upgrade: same path, new bytes.
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho upgraded\n"), 0o755); err != nil {
		t.Fatalf("upgrade bin: %v", err)
	}

	out := runQuick(t, statePath, binPath)

	if !strings.Contains(out, "grafel install --refresh-state") {
		t.Errorf("SHA-drift warning must name the command that resolves it, got: %q", out)
	}
	// #4463 regression guard: keep the non-alarming framing.
	if !strings.Contains(out, "still usable") {
		t.Errorf("SHA-drift warning must still reassure the daemon is usable, got: %q", out)
	}
	if strings.Contains(out, "reinstall recommended") {
		t.Errorf("SHA-drift wording must not read as corruption, got: %q", out)
	}
	// Still exactly one line, still short enough to be a status prefix.
	if n := countNonEmptyLines(out); n != 1 {
		t.Errorf("quick-doctor printed %d lines, want 1: %q", n, out)
	}
	if len(out) > 220 {
		t.Errorf("quick-doctor output too long (%d bytes): %q", len(out), out)
	}
}

// TestQuickDoctor_SilentWhenBinaryMatches: the whole point of the installer
// half of this fix. Once install.json records the on-disk binary, quick-doctor
// must say nothing about the CLI at all.
func TestQuickDoctor_SilentWhenBinaryMatches(t *testing.T) {
	statePath, binPath := quickEnv(t)
	out := runQuick(t, statePath, binPath)

	if strings.Contains(out, "binary") || strings.Contains(out, "refresh-state") {
		t.Errorf("quick-doctor must not warn about the CLI when the SHA matches, got: %q", out)
	}
}

// TestQuickDoctor_DifferentBinaryOnPathIsItsOwnCase: install.json records
// /a/grafel, but the process actually running is /b/grafel. Hashing /a/grafel
// says nothing about this process — a match there is a FALSE all-clear (you can
// be running a stale second install with a perfectly intact recorded SHA), and
// a mismatch is misdiagnosed as "your binary was updated". Report the real
// condition instead.
func TestQuickDoctor_DifferentBinaryOnPathIsItsOwnCase(t *testing.T) {
	statePath, recorded := quickEnv(t)

	other := filepath.Join(t.TempDir(), "grafel")
	if err := os.WriteFile(other, []byte("#!/bin/sh\necho other install\n"), 0o755); err != nil {
		t.Fatalf("write other bin: %v", err)
	}

	out := runQuick(t, statePath, other)

	if !strings.Contains(out, "grafel install --refresh-state") {
		t.Errorf("path-mismatch warning must name the command that resolves it, got: %q", out)
	}
	if strings.Contains(out, "binary updated since last install") {
		t.Errorf("a path mismatch must not be misreported as an in-place binary update, got: %q", out)
	}
	if !strings.Contains(out, other) || !strings.Contains(out, recorded) {
		t.Errorf("path-mismatch warning must name both paths (running=%s recorded=%s), got: %q", other, recorded, out)
	}
	if n := countNonEmptyLines(out); n != 1 {
		t.Errorf("quick-doctor printed %d lines, want 1: %q", n, out)
	}
}

// TestQuickDoctor_DevBuildDoesNotGetThePathWarning: a contributor running a
// locally built binary from the MAIN checkout (where .git is a directory, so
// isInGitWorktree() does not exempt them) must not be told to record that
// scratch build as their install — doing so would overwrite the record of
// their real one. Release builds still get the warning; see
// TestQuickDoctor_DifferentBinaryOnPathIsItsOwnCase.
func TestQuickDoctor_DevBuildDoesNotGetThePathWarning(t *testing.T) {
	statePath, _ := quickEnv(t)

	scratch := filepath.Join(t.TempDir(), "grafel")
	if err := os.WriteFile(scratch, []byte("#!/bin/sh\necho scratch build\n"), 0o755); err != nil {
		t.Fatalf("write scratch bin: %v", err)
	}

	out := runQuickAs(t, statePath, scratch, "0.0.0-dev")
	if strings.Contains(out, "refresh-state") {
		t.Errorf("a dev build must not be told to re-record itself as the install, got: %q", out)
	}
}

// TestQuickDoctor_DaemonWarningKeepsItsOwnRemedy: the daemon-unreachable branch
// is the one case where `grafel doctor` genuinely has more to say, so it — and
// only it — keeps that pointer.
func TestQuickDoctor_DaemonWarningKeepsItsOwnRemedy(t *testing.T) {
	statePath, binPath := quickEnv(t)
	out := runQuick(t, statePath, binPath)

	if !strings.Contains(out, "daemon unreachable") {
		t.Fatalf("expected a daemon-unreachable warning against port 1, got: %q", out)
	}
	if !strings.Contains(out, "grafel doctor") {
		t.Errorf("daemon warning should still point at 'grafel doctor', got: %q", out)
	}
}
