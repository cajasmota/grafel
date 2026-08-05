// doctor_cli_remedy_test.go pins the FULL `grafel doctor` cli check against the
// same contract quick-doctor already satisfies.
//
// Review finding: the first pass fixed quick-doctor and left full doctor
// pointing at the dangerous command. `checkCLI` still emitted
// "re-run 'grafel install' to refresh" — a seven-step transaction that appends
// to the .gitignore and installs four git hooks in whatever repository the user
// is standing in (see internal/install/refreshstate.go) — and it had no
// identity check at all, so it kept handing out the false all-clear that
// quick-doctor had just learned to catch. The two surfaces disagreed and the
// MORE authoritative one gave the WORSE advice.
package install_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/install"
)

// runDoctorAs runs full doctor with an explicit running-binary identity.
func runDoctorAs(t *testing.T, env *doctorTestEnv, selfPath, ver string) *install.CheckResult {
	t.Helper()
	report, err := install.RunDoctor(install.DoctorOptions{
		StatePath:          env.statePath,
		ClaudeConfigDirs:   []string{env.claudeJSON},
		DaemonTimeout:      200 * time.Millisecond,
		ProbeDaemonVersion: daemonDownProbe,
		SkillsDir:          env.skillsDir,
		SelfPath:           selfPath,
		Version:            ver,
	})
	if err != nil {
		t.Fatalf("RunDoctor error: %v", err)
	}
	cli := findCheck(report, "cli")
	if cli == nil {
		t.Fatal("report has no cli check")
	}
	return cli
}

// TestDoctorCLI_SHADriftNamesTheNarrowCommand: the in-place upgrade case must
// prescribe the narrow refresh, not the full transaction.
func TestDoctorCLI_SHADriftNamesTheNarrowCommand(t *testing.T) {
	env := newDoctorTestEnv(t)
	if err := os.WriteFile(env.fakeBin, []byte("#!/bin/sh\necho upgraded"), 0o755); err != nil {
		t.Fatalf("upgrade binary: %v", err)
	}

	cli := runDoctorAs(t, env, env.fakeBin, "0.2.0")
	drift := strings.Join(cli.Drift, " ")

	if !strings.Contains(drift, "grafel install --refresh-state") {
		t.Errorf("cli drift must prescribe the narrow refresh, got: %q", drift)
	}
	if strings.Contains(drift, "re-run 'grafel install'") {
		t.Errorf("cli drift must not prescribe the full install transaction "+
			"(it mutates .gitignore and installs git hooks in the caller's cwd), got: %q", drift)
	}
	if cli.Severity != install.SeverityWarning {
		t.Errorf("#4463: SHA drift stays a Warning, got %v", cli.Severity)
	}
}

// TestDoctorCLI_TwoInstallsAreReported is the false all-clear full doctor was
// still giving: the recorded binary hashes perfectly while the process is a
// DIFFERENT install found earlier on PATH. Before this, doctor said "cli OK".
func TestDoctorCLI_TwoInstallsAreReported(t *testing.T) {
	env := newDoctorTestEnv(t)

	other := filepath.Join(t.TempDir(), "grafel")
	if err := os.WriteFile(other, []byte("#!/bin/sh\necho second install"), 0o755); err != nil {
		t.Fatalf("write second install: %v", err)
	}

	cli := runDoctorAs(t, env, other, "0.2.0")
	drift := strings.Join(cli.Drift, " ")

	if cli.OK {
		t.Errorf("running a different binary than install.json records must not be reported OK; drift=%q", drift)
	}
	if !strings.Contains(drift, "which -a grafel") {
		t.Errorf("two-installs drift must tell the user how to find the duplicate, got: %q", drift)
	}
	if strings.Contains(drift, "refresh-state") {
		t.Errorf("two coexisting installs must NOT be prescribed --refresh-state "+
			"(it just re-points install.json at whichever binary was invoked), got: %q", drift)
	}
	if !strings.Contains(drift, other) || !strings.Contains(drift, env.fakeBin) {
		t.Errorf("two-installs drift must name both paths, got: %q", drift)
	}
}

// TestDoctorCLI_RelocatedInstallGetsTheRefresh: recorded binary gone, one
// binary left — re-recording is right and cannot ping-pong.
func TestDoctorCLI_RelocatedInstallGetsTheRefresh(t *testing.T) {
	env := newDoctorTestEnv(t)
	if err := os.Remove(env.fakeBin); err != nil {
		t.Fatalf("remove recorded binary: %v", err)
	}

	moved := filepath.Join(t.TempDir(), "grafel")
	if err := os.WriteFile(moved, []byte("#!/bin/sh\necho relocated"), 0o755); err != nil {
		t.Fatalf("write moved binary: %v", err)
	}

	cli := runDoctorAs(t, env, moved, "0.2.0")
	drift := strings.Join(cli.Drift, " ")

	if !strings.Contains(drift, "no longer exists") {
		t.Errorf("relocated-install drift must say the recorded binary is gone, got: %q", drift)
	}
	if !strings.Contains(drift, "grafel install --refresh-state") {
		t.Errorf("relocated install must be offered the refresh, got: %q", drift)
	}
	if cli.Severity == install.SeverityCritical {
		t.Errorf("a relocated install is advisory, not corruption; got Critical: %q", drift)
	}
}

// TestDoctorCLI_SymlinkedBinDirIsOK: one binary, two names — still OK.
func TestDoctorCLI_SymlinkedBinDirIsOK(t *testing.T) {
	env := newDoctorTestEnv(t)

	link := filepath.Join(t.TempDir(), "grafel")
	if err := os.Symlink(env.fakeBin, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	cli := runDoctorAs(t, env, link, "0.2.0")
	if !cli.OK {
		t.Errorf("a symlink to the recorded binary is the same install; drift=%q", strings.Join(cli.Drift, " "))
	}
}

// TestDoctorCLI_DevBuildSkipsTheIdentityCheck: a contributor's scratch build
// must not be told to record itself as THE install, on either surface.
func TestDoctorCLI_DevBuildSkipsTheIdentityCheck(t *testing.T) {
	env := newDoctorTestEnv(t)

	scratch := filepath.Join(t.TempDir(), "grafel")
	if err := os.WriteFile(scratch, []byte("#!/bin/sh\necho scratch"), 0o755); err != nil {
		t.Fatalf("write scratch: %v", err)
	}

	cli := runDoctorAs(t, env, scratch, "0.0.0-dev")
	drift := strings.Join(cli.Drift, " ")
	if strings.Contains(drift, "which -a grafel") || strings.Contains(drift, "refresh-state") {
		t.Errorf("a dev build's path says nothing about the install, got: %q", drift)
	}
}
