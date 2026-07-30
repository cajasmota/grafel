// installsh_restart_stopped_test.go pins issue #6044 review item 6's second
// sub-finding: after `grafel stop` on macOS (which pairs `launchctl bootout`
// with a persistent `launchctl disable` — see
// internal/daemon/service/launchd_darwin.go Unload), install.sh's
// restart_daemon must still be able to bring the daemon back during an
// upgrade. Before this fix, restart_daemon's "is a launchd agent
// registered?" guard (`launchctl print` / `launchctl list`) only detects a
// currently-LOADED job — which a disabled, bootout'd job is not, by
// definition — so it fell straight through to the generic "nothing
// registered" case and gave up silently, with only a generic post-install
// message and no attempt to restart anything.
//
// Unlike installsh_restart_verify_test.go's structural (string-contains)
// checks, this test actually SOURCES install.sh as a library and calls the
// real restart_daemon function with a fake `launchctl` on PATH — so it
// verifies the runtime branching, not just that certain substrings appear
// somewhere in the function body.
package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLaunchctlScript is a bash stand-in for launchctl. It logs every
// invocation (one line per call, space-joined args) to $LAUNCHCTL_LOG, and
// answers deterministically:
//   - print / list: simulate "not currently loaded" (the state after
//     `grafel stop`'s bootout) — print exits 1, list prints nothing that
//     matches "com.grafel.daemon".
//   - enable / bootstrap / bootout / kickstart: succeed (exit 0).
const fakeLaunchctlScript = `#!/usr/bin/env bash
echo "$@" >> "$LAUNCHCTL_LOG"
case "$1" in
  print) exit 1 ;;
  list) exit 0 ;;
  *) exit 0 ;;
esac
`

// fakeGrafelStatusScript is a bash stand-in for the just-installed grafel
// binary. It answers `grafel status` with a v-prefixed line reporting
// $WANT_VERSION so wait_for_daemon_version's very first poll matches
// immediately (no need to wait out its ~10s budget).
const fakeGrafelStatusScript = `#!/usr/bin/env bash
if [ "$1" = "status" ]; then
  echo "Daemon: running  pid=1  uptime=1s"
  echo "  version: v${WANT_VERSION}"
  exit 0
fi
exit 0
`

// writeExecutable writes an executable script at dir/name.
func writeExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestInstallSh_RestartDaemon_MacOS_RecoversFromPriorStop is the runtime pin:
// with a plist on disk but the launchd job reported as not-loaded (exactly
// what `grafel stop` leaves behind — see the package doc comment above), the
// real restart_daemon must call `launchctl enable` before `launchctl
// bootstrap`, and must ultimately report "restarted" — not silently give up
// with the generic post-install message.
func TestInstallSh_RestartDaemon_MacOS_RecoversFromPriorStop(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available; skipping install.sh runtime test")
	}

	fakeHome := t.TempDir()
	launchAgents := filepath.Join(fakeHome, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgents, 0o755); err != nil {
		t.Fatalf("mkdir LaunchAgents: %v", err)
	}
	plistPath := filepath.Join(launchAgents, "com.grafel.daemon.plist")
	if err := os.WriteFile(plistPath, []byte("<plist/>"), 0o644); err != nil {
		t.Fatalf("write fake plist: %v", err)
	}

	// install.sh unconditionally sets PREFIX="${GRAFEL_PREFIX:-$HOME/.grafel}"
	// and BIN_DIR="$PREFIX/bin" at sourcing time — overriding a bare BIN_DIR
	// env var — so redirect it via GRAFEL_PREFIX instead.
	fakePrefix := t.TempDir()
	fakeBinDir := filepath.Join(fakePrefix, "bin")
	if err := os.MkdirAll(fakeBinDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin dir: %v", err)
	}
	wantVersion := "9.9.9"
	writeExecutable(t, fakeBinDir, "grafel", fakeGrafelStatusScript)

	fakePathDir := t.TempDir()
	launchctlPath := writeExecutable(t, fakePathDir, "launchctl", fakeLaunchctlScript)
	logPath := filepath.Join(t.TempDir(), "launchctl.log")

	script := `set -eu; GRAFEL_INSTALL_SH_LIB=1 . "$1"; restart_daemon macos "$2"`
	cmd := exec.Command(bash, "-c", script, "bash", installShPath(t), wantVersion)
	cmd.Env = append(os.Environ(),
		"HOME="+fakeHome,
		"GRAFEL_PREFIX="+fakePrefix,
		"WANT_VERSION="+wantVersion,
		"LAUNCHCTL_LOG="+logPath,
		"PATH="+fakePathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Fatalf("restart_daemon macos: %v\noutput:\n%s", runErr, out)
	}
	stdout := strings.TrimSpace(string(out))
	if stdout != "restarted" {
		t.Fatalf(`restart_daemon must recover from a prior "grafel stop" and echo "restarted", got %q`, stdout)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read launchctl log (path=%s, launchctl=%s): %v", logPath, launchctlPath, err)
	}
	log := string(logBytes)
	enableIdx := strings.Index(log, "enable ")
	bootstrapIdx := strings.Index(log, "bootstrap ")
	if enableIdx < 0 {
		t.Errorf("expected restart_daemon to call `launchctl enable` to clear the persistent disable "+
			"a prior `grafel stop` left behind, got launchctl log:\n%s", log)
	}
	if bootstrapIdx < 0 {
		t.Errorf("expected restart_daemon to call `launchctl bootstrap` to reload the daemon, got launchctl log:\n%s", log)
	}
	if enableIdx >= 0 && bootstrapIdx >= 0 && enableIdx > bootstrapIdx {
		t.Errorf("expected `launchctl enable` before `launchctl bootstrap` (otherwise bootstrap loads a still-disabled "+
			"job and RunAtLoad silently no-ops), got launchctl log:\n%s", log)
	}
	if strings.Contains(log, "kickstart") {
		t.Errorf("did not expect a `launchctl kickstart` call on this path (the job was not loaded — "+
			"kickstart operates on an already-bootstrapped job), got launchctl log:\n%s", log)
	}
}

// TestInstallSh_RestartDaemon_MacOS_NothingRegisteredStillNoOps is the OTHER
// direction of the same fixture: when there is truly nothing registered (no
// plist on disk at all — a fresh machine, never `grafel install`ed), restart_daemon
// must still return non-zero and print nothing on stdout, exactly as before
// this fix. This proves the new elif branch is scoped to "plist present but
// not loaded" and does not fire for "nothing registered at all".
func TestInstallSh_RestartDaemon_MacOS_NothingRegisteredStillNoOps(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available; skipping install.sh runtime test")
	}

	fakeHome := t.TempDir() // no Library/LaunchAgents/com.grafel.daemon.plist at all
	fakePrefix := t.TempDir()
	fakeBinDir := filepath.Join(fakePrefix, "bin")
	if err := os.MkdirAll(fakeBinDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin dir: %v", err)
	}
	wantVersion := "9.9.9"
	writeExecutable(t, fakeBinDir, "grafel", fakeGrafelStatusScript)

	fakePathDir := t.TempDir()
	writeExecutable(t, fakePathDir, "launchctl", fakeLaunchctlScript)
	logPath := filepath.Join(t.TempDir(), "launchctl.log")

	script := `set -eu; GRAFEL_INSTALL_SH_LIB=1 . "$1"; restart_daemon macos "$2"`
	cmd := exec.Command(bash, "-c", script, "bash", installShPath(t), wantVersion)
	cmd.Env = append(os.Environ(),
		"HOME="+fakeHome,
		"GRAFEL_PREFIX="+fakePrefix,
		"WANT_VERSION="+wantVersion,
		"LAUNCHCTL_LOG="+logPath,
		"PATH="+fakePathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, runErr := cmd.CombinedOutput()
	stdout := strings.TrimSpace(string(out))
	if runErr == nil {
		t.Fatalf("restart_daemon must return non-zero when nothing is registered at all, got success with output %q", stdout)
	}
	if stdout != "" {
		t.Fatalf("restart_daemon must print nothing on stdout when nothing is registered, got %q", stdout)
	}
	// print/list ARE expected (that's how the "nothing loaded" state is
	// detected in the first place) — what must NOT happen is any attempt to
	// actually load/start anything, since there is nothing (no plist) to load.
	logBytes, rerr := os.ReadFile(logPath)
	if rerr != nil {
		t.Fatalf("read launchctl log: %v", rerr)
	}
	log := string(logBytes)
	for _, forbidden := range []string{"enable", "bootstrap", "kickstart", "bootout"} {
		if strings.Contains(log, forbidden) {
			t.Errorf("expected no %q call when nothing is registered at all, got launchctl log:\n%s", forbidden, log)
		}
	}
}
