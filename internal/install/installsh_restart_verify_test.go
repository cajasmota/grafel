// installsh_restart_verify_test.go asserts, structurally, that install.sh's
// restart_daemon function actually VERIFIES the running daemon's version
// after restarting it, and escalates to a hard restart when the running
// daemon is still stale.
//
// Background (#5850): `grafel install`, `grafel update`, and this curl
// installer all previously "restarted" the daemon by kickstarting/restarting
// the OS service unit and declaring success on exit 0 — never confirming the
// daemon that answered afterward was actually running the NEWLY-installed
// binary. A daemon that stayed bound to the socket (because launchctl
// kickstart / systemctl restart raced, or the unit didn't actually reload the
// binary path) was silently reported as "restarted" while still serving the
// old version.
//
// We cannot execute install.sh directly in CI (no real launchd/systemd, no
// network), so — mirroring installscript_version_test.go's approach for
// install.bat — we assert on the SHAPE of the script: the restart_daemon
// function must (a) verify the post-restart version against the
// just-installed binary via a reliable, non-HTML-shadowable channel, and (b)
// fall back to a hard-restart escalation (launchctl bootout on macOS,
// systemctl stop+start on Linux) when that verification fails.
package install

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// installShSource locates and reads install.sh relative to this test file's
// package directory (internal/install/../../install.sh at the repo root).
func installShSource(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test file path")
	}
	// internal/install/installsh_restart_verify_test.go -> repo root is two
	// directories up.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	path := filepath.Join(repoRoot, "install.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read install.sh at %s: %v", path, err)
	}
	return string(data)
}

// extractFunc pulls the body of a POSIX-shell `name() { ... }` function out
// of src by matching balanced braces starting at the function's opening
// brace. It fails the test if the function cannot be found.
func extractFunc(t *testing.T, src, name string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\(\)\s*\{`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		t.Fatalf("function %s() not found in install.sh", name)
	}
	depth := 0
	start := loc[1] - 1 // index of the opening '{'
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		}
	}
	t.Fatalf("function %s() has no matching closing brace", name)
	return ""
}

// TestInstallSh_RestartDaemon_VerifiesRunningVersion is the RED test for
// #5850 on the shell side: restart_daemon must not declare "restarted" purely
// because the kickstart/systemctl-restart command exited 0. It must probe the
// just-installed binary's reported daemon version and compare it against what
// is actually running before claiming success.
func TestInstallSh_RestartDaemon_VerifiesRunningVersion(t *testing.T) {
	src := installShSource(t)
	fn := extractFunc(t, src, "restart_daemon")

	// restart_daemon must call out to a version-verification step (whether
	// inline or via a helper it invokes) before declaring success.
	if !strings.Contains(fn, "wait_for_daemon_version") {
		t.Error("restart_daemon must call a post-restart version-verification step (e.g. wait_for_daemon_version) before declaring success")
	}

	// The verification helper itself must run the JUST-INSTALLED binary (not
	// rely on the dashboard HTTP route, which can be shadowed by the SPA
	// catch-all and return HTML instead of a version — see looksLikeVersion
	// in copy.go) to determine what the running daemon reports.
	verifyFn := extractFunc(t, src, "wait_for_daemon_version")
	if !strings.Contains(verifyFn, `"$BIN_DIR/grafel"`) {
		t.Error(`wait_for_daemon_version must invoke "$BIN_DIR/grafel" (the newly-installed binary) to verify the running daemon's version`)
	}

	// It must not fetch a bare HTTP path as its version source (the
	// HTML-shadowing hazard this task explicitly calls out).
	if strings.Contains(verifyFn, "curl") && strings.Contains(verifyFn, "/healthz") {
		t.Error("wait_for_daemon_version must not use the HTML-shadowable /healthz HTTP route as its version-verification channel")
	}

	// Some form of version comparison must be present: either a loop/poll
	// construct or an explicit compare against the target version.
	hasVersionSignal := strings.Contains(fn, "version") // covers "version:", "--version", "$want_version" etc.
	if !hasVersionSignal {
		t.Error("restart_daemon must reference a version check after restarting the daemon")
	}
}

// TestInstallSh_RestartDaemon_HasHardRestartEscalation verifies the fallback
// path: when the post-restart version check fails, restart_daemon must force
// a HARD restart rather than silently leaving the stale daemon running.
// - macOS: launchctl bootout (forces the OLD process to fully exit) before
//   reloading (bootstrap or kickstart).
// - Linux: systemctl --user stop, then start (a plain "restart" can race a
//   unit that isn't actually reloading the new binary).
func TestInstallSh_RestartDaemon_HasHardRestartEscalation(t *testing.T) {
	src := installShSource(t)
	fn := extractFunc(t, src, "restart_daemon")

	if !strings.Contains(fn, "launchctl bootout") {
		t.Error(`restart_daemon must escalate to "launchctl bootout" on macOS when the post-restart version check fails`)
	}
	if !strings.Contains(fn, "systemctl --user stop") {
		t.Error(`restart_daemon must escalate to "systemctl --user stop" (then start) on Linux when the post-restart version check fails`)
	}
}

// TestInstallSh_Main_ReportsAccurateFinalMessage verifies main() no longer
// unconditionally prints "daemon restarted" just because restart_daemon's
// underlying kickstart/systemctl command exited 0 — it must distinguish a
// CONFIRMED-fresh restart from a still-stale one and warn accordingly,
// telling the user to run `grafel install` to finish.
func TestInstallSh_Main_ReportsAccurateFinalMessage(t *testing.T) {
	src := installShSource(t)

	if !strings.Contains(src, "still running the old version") &&
		!strings.Contains(src, "still running an old version") {
		t.Error(`main() must print an explicit warning (e.g. "daemon still running the old version") when post-restart verification fails, instead of always claiming success`)
	}
	if !strings.Contains(src, "grafel install") {
		t.Error(`the stale-daemon warning must point the user at "grafel install" to finish`)
	}
}

// TestInstallSh_RestartDaemon_StaysBestEffort verifies restart_daemon and its
// escalation path never call `set -e`-fatal `exit` with a nonzero code that
// would abort the WHOLE installer — restart_daemon returning non-zero (which
// main() already treats as best-effort via `|| true`) is fine, but the
// function body itself must not call the top-level `err` helper (which calls
// `exit 1`) purely because verification failed.
func TestInstallSh_RestartDaemon_StaysBestEffort(t *testing.T) {
	src := installShSource(t)
	fn := extractFunc(t, src, "restart_daemon")

	// err() is install.sh's fatal helper (printf + exit 1). restart_daemon
	// must stay best-effort and never invoke it.
	if regexp.MustCompile(`(^|[^_a-zA-Z])err\s`).MatchString(fn) {
		t.Error("restart_daemon must remain best-effort and never call the fatal err() helper")
	}
}

// TestInstallSh_ShellcheckClean is a lightweight guard that requires no shell
// interpreter: it just confirms the script still declares `set -eu` (the
// baseline strict-mode this installer relies on) so a future edit can't
// accidentally drop it while adding the verification/escalation logic. Full
// shellcheck linting is run separately (`shellcheck install.sh`) as part of
// the definition of done; this test keeps a cheap regression guard in CI
// where shellcheck may not be installed.
func TestInstallSh_ShellcheckClean(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh targets macOS/Linux only")
	}
	src := installShSource(t)
	if !strings.Contains(src, "set -eu") {
		t.Error("install.sh must keep `set -eu` at the top")
	}
}
