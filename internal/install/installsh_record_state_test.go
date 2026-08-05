// installsh_record_state_test.go pins the installer half of the curl-upgrade
// defect.
//
// install.sh places the new binary with `install -m 0755 …` (falling back to
// cp+chmod) and then, two lines later, runs `"$BIN_DIR/grafel" doctor`. It
// never runs anything that writes ~/.grafel/install.json — so the CLI SHA
// recorded there stays the PREVIOUS binary's, RunQuickDoctor's check-1 fires,
// and the installer prints the very warning it just caused. It then tells the
// user "grafel updated and daemon restarted" on the happy path, so the
// outstanding step is never mentioned.
//
// The fix must (a) record the just-placed binary BEFORE the doctor call, and
// (b) do it WITHOUT running the full `grafel install` transaction — steps 5 and
// 7 of RunCopy append to .gitignore and install four git hooks in
// opts.WorkingDir, i.e. in whatever repository the user's shell happened to be
// sitting in when they piped the installer to bash. A curl installer must not
// mutate an unrelated repo.
//
// Like installsh_restart_stopped_test.go, this test SOURCES install.sh as a
// library (GRAFEL_INSTALL_SH_LIB=1) and calls the real function with a fake
// binary on a temp BIN_DIR, so it verifies runtime behaviour and not just the
// presence of a substring.
package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGrafelArgLogScript is a bash stand-in for the just-installed grafel
// binary that appends every invocation's args to $GRAFEL_ARG_LOG and exits 0.
const fakeGrafelArgLogScript = `#!/usr/bin/env bash
echo "$@" >> "$GRAFEL_ARG_LOG"
exit 0
`

// fakeGrafelArgLogFailScript is the same recorder but always exits 1, so we can
// prove the installer's best-effort contract (a failing state refresh must not
// abort the install).
const fakeGrafelArgLogFailScript = `#!/usr/bin/env bash
echo "$@" >> "$GRAFEL_ARG_LOG"
exit 1
`

// runRecordInstallState sources install.sh and invokes record_install_state
// with a fake grafel binary in a temp BIN_DIR. Returns the argument log and the
// combined output.
func runRecordInstallState(t *testing.T, binScript string) (argLog string, combined string, err error) {
	t.Helper()
	bash, lookErr := exec.LookPath("bash")
	if lookErr != nil {
		t.Skip("bash not available; skipping install.sh runtime test")
	}

	fakeHome := t.TempDir()
	// install.sh hard-sets PREFIX="${GRAFEL_PREFIX:-$HOME/.grafel}" and
	// BIN_DIR="$PREFIX/bin" at sourcing time, so redirect via GRAFEL_PREFIX.
	fakePrefix := t.TempDir()
	fakeBinDir := filepath.Join(fakePrefix, "bin")
	if mkErr := os.MkdirAll(fakeBinDir, 0o755); mkErr != nil {
		t.Fatalf("mkdir fake bin dir: %v", mkErr)
	}
	writeExecutable(t, fakeBinDir, "grafel", binScript)

	logPath := filepath.Join(t.TempDir(), "grafel-args.log")

	script := `set -eu; GRAFEL_INSTALL_SH_LIB=1 . "$1"; record_install_state`
	cmd := exec.Command(bash, "-c", script, "bash", installShPath(t))
	cmd.Env = append(os.Environ(),
		"HOME="+fakeHome,
		"GRAFEL_PREFIX="+fakePrefix,
		"GRAFEL_ARG_LOG="+logPath,
	)
	out, runErr := cmd.CombinedOutput()

	data, readErr := os.ReadFile(logPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read arg log: %v", readErr)
	}
	return string(data), string(out), runErr
}

// TestInstallSh_RecordInstallState_InvokesTheNarrowRefresh is the RED test:
// after placing the binary the installer must run something that rewrites
// install.json's CLI record — and it must be the NARROW refresh, not the full
// install transaction.
func TestInstallSh_RecordInstallState_InvokesTheNarrowRefresh(t *testing.T) {
	argLog, combined, err := runRecordInstallState(t, fakeGrafelArgLogScript)
	if err != nil {
		t.Fatalf("record_install_state: %v\noutput:\n%s", err, combined)
	}
	if argLog == "" {
		t.Fatalf("record_install_state never invoked the installed binary (empty arg log)\noutput:\n%s", combined)
	}
	if !strings.Contains(argLog, "install --refresh-state") {
		t.Errorf("record_install_state must invoke `grafel install --refresh-state`, got invocations:\n%s", argLog)
	}
	// The full transaction (a bare `grafel install`) appends to .gitignore and
	// installs git hooks in the caller's cwd — never acceptable from a curl
	// installer. Assert no invocation is a bare install.
	for _, line := range strings.Split(strings.TrimSpace(argLog), "\n") {
		if strings.TrimSpace(line) == "install" {
			t.Errorf("record_install_state must not run the full `grafel install` transaction "+
				"(it mutates .gitignore and installs git hooks in the caller's cwd); got invocations:\n%s", argLog)
		}
	}
}

// TestInstallSh_RecordInstallState_IsBestEffort: every post-download step in
// install.sh is best-effort (none of them call the fatal err() helper). A
// non-zero exit from the refresh must not propagate — under `set -e` a bare
// failing command would abort the whole installer right before the daemon
// restart.
func TestInstallSh_RecordInstallState_IsBestEffort(t *testing.T) {
	argLog, combined, err := runRecordInstallState(t, fakeGrafelArgLogFailScript)
	if err != nil {
		t.Fatalf("record_install_state must swallow a failing refresh (best-effort), got: %v\noutput:\n%s", err, combined)
	}
	if !strings.Contains(argLog, "--refresh-state") {
		t.Errorf("expected the refresh to have been attempted, got:\n%s", argLog)
	}
}

// TestInstallSh_Main_RecordsStateBeforeReportingDoctor is the ordering pin: the
// state refresh must happen BEFORE `"$BIN_DIR/grafel" doctor` runs, otherwise
// the installer still prints the stale-SHA warning it just caused.
func TestInstallSh_Main_RecordsStateBeforeReportingDoctor(t *testing.T) {
	src := installShSource(t)
	mainFn := extractFunc(t, src, "main")

	recordIdx := strings.Index(mainFn, "record_install_state")
	if recordIdx < 0 {
		t.Fatalf("main() must call record_install_state after placing the binary; body:\n%s", mainFn)
	}
	doctorIdx := strings.Index(mainFn, `"$BIN_DIR/grafel" doctor`)
	if doctorIdx < 0 {
		t.Fatalf(`main() no longer invokes "$BIN_DIR/grafel" doctor; body:\n%s`, mainFn)
	}
	if recordIdx > doctorIdx {
		t.Errorf("record_install_state must run BEFORE the doctor report (otherwise the installer "+
			"prints the stale-SHA warning it just caused); record at %d, doctor at %d", recordIdx, doctorIdx)
	}

	// It must also come after the binary is actually in place — recording the
	// SHA of the previous binary would be worse than not recording at all.
	placeIdx := strings.Index(mainFn, `"$BIN_DIR/grafel"`)
	installCmdIdx := strings.Index(mainFn, `install -m 0755`)
	if installCmdIdx < 0 {
		t.Fatalf("main() no longer places the binary with `install -m 0755`; body:\n%s", mainFn)
	}
	if recordIdx < installCmdIdx {
		t.Errorf("record_install_state must run AFTER the new binary is placed; record at %d, place at %d (first $BIN_DIR ref at %d)",
			recordIdx, installCmdIdx, placeIdx)
	}
}
