// installsh_registermcp_6169_test.go pins the installer half of #6169.
//
// install.sh downloaded, verified, placed the binary, refreshed install.json,
// ran doctor and restarted the daemon — and never registered the MCP server.
// The only two registration sites in the tree are RunCopy step 3 and
// ApplyToolDelta, and this script reached neither, so a first-ever curl install
// left a binary on PATH that Claude Code could not see. It then told the user
// to "run `grafel wizard`", which registers no MCP server either.
//
// These tests SOURCE install.sh as a library (GRAFEL_INSTALL_SH_LIB=1) against
// a fake binary on a temp PREFIX and a temp HOME, and call the real functions —
// so they verify runtime behaviour, not the presence of a substring. Nothing
// here touches the real ~/.grafel or the real ~/.claude.json.
package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sourceInstallSh runs `snippet` with install.sh sourced as a library, a fake
// grafel on a temp BIN_DIR that logs its args to $GRAFEL_ARG_LOG, and HOME
// pointed at a temp dir. Returns the arg log and the combined output.
func sourceInstallSh(t *testing.T, binScript, snippet string) (argLog, combined string, err error) {
	t.Helper()
	bash, lookErr := exec.LookPath("bash")
	if lookErr != nil {
		t.Skip("bash not available; skipping install.sh runtime test")
	}

	fakeHome := t.TempDir()
	fakePrefix := t.TempDir()
	fakeBinDir := filepath.Join(fakePrefix, "bin")
	if mkErr := os.MkdirAll(fakeBinDir, 0o755); mkErr != nil {
		t.Fatalf("mkdir fake bin dir: %v", mkErr)
	}
	writeExecutable(t, fakeBinDir, "grafel", binScript)

	logPath := filepath.Join(t.TempDir(), "grafel-args.log")

	script := `set -eu; GRAFEL_INSTALL_SH_LIB=1 . "$1"; ` + snippet
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

// TestInstallSh_RegistersMCPWithTheNarrowFlag is #6169 itself: the installer
// must actually perform the registration, and must do it with --register-mcp
// rather than the full transaction.
func TestInstallSh_RegistersMCPWithTheNarrowFlag(t *testing.T) {
	argLog, out, err := sourceInstallSh(t, fakeGrafelArgLogScript, "register_mcp")
	if err != nil {
		t.Fatalf("register_mcp failed: %v (out=%q)", err, out)
	}
	if !strings.Contains(argLog, "install --register-mcp") {
		t.Fatalf("install.sh never registered MCP; grafel was invoked as: %q", argLog)
	}
	// A bare `install` line would be the seven-step RunCopy transaction: daemon
	// restart, .gitignore append and four git hooks in the caller's cwd.
	for _, line := range strings.Split(strings.TrimSpace(argLog), "\n") {
		if strings.TrimSpace(line) == "install" {
			t.Errorf("install.sh ran the full `grafel install` transaction: %q", argLog)
		}
	}
}

// TestInstallSh_RegisterMCPIsBestEffort: every post-download step in this
// script is best-effort. A host config that cannot be written must not abort an
// install whose binary is already on disk.
func TestInstallSh_RegisterMCPIsBestEffort(t *testing.T) {
	argLog, out, err := sourceInstallSh(t, fakeGrafelArgLogFailScript, "register_mcp")
	if err != nil {
		t.Fatalf("a failing registration must not abort the installer: %v (out=%q)", err, out)
	}
	if !strings.Contains(argLog, "install --register-mcp") {
		t.Errorf("registration was not attempted: %q", argLog)
	}
}

// TestInstallSh_RegisterMCPReportsWhatItDid: this is the step that makes the
// product usable, and it is invisible in the doctor output that follows. If the
// installer swallowed it the user would have no way to tell a registration that
// worked from one that silently did nothing — which is how #6169 survived.
func TestInstallSh_RegisterMCPReportsWhatItDid(t *testing.T) {
	const speaks = `#!/usr/bin/env bash
echo "$@" >> "$GRAFEL_ARG_LOG"
echo "GRAFEL MCP REGISTERED IN: /somewhere/.claude.json"
exit 0
`
	_, out, err := sourceInstallSh(t, speaks, "register_mcp")
	if err != nil {
		t.Fatalf("register_mcp failed: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "GRAFEL MCP REGISTERED IN: /somewhere/.claude.json") {
		t.Errorf("registration output was swallowed: %q", out)
	}
}

// TestInstallSh_FinalMessageNeverPrescribesBareInstall.
//
// The closing advice is the installer's only interface once it has finished,
// and it has to be safe to follow at the moment it is printed. Bare `grafel
// install` is not: it is RunCopy, whose step-4 daemon restart failing rolls
// step 3 back and restores MCP host configs from snapshots — deleting the file
// and every foreign server in it on a first-ever install (#6168) — and whose
// steps 5 and 7 write to whatever repository the caller's shell was in (#6162).
//
// The `stale` branch prescribed exactly that, in the one branch where the
// daemon is KNOWN not to have picked up the new binary, i.e. where the step-4
// restart is most likely to fail.
func TestInstallSh_FinalMessageNeverPrescribesBareInstall(t *testing.T) {
	for _, status := range []string{"restarted", "stale", "", "anything-else"} {
		name := status
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			_, out, err := sourceInstallSh(t, fakeGrafelArgLogScript,
				"final_message '"+status+"' 1.2.3")
			if err != nil {
				t.Fatalf("final_message %q: %v (out=%q)", status, err, out)
			}
			if out == "" {
				t.Fatalf("final_message %q printed nothing", status)
			}
			// Match the QUOTED command form the installer uses to prescribe a
			// next step ("run 'grafel install' to finish"), not the bare
			// substring — "grafel installed" contains the latter and is not a
			// prescription. Every flagged form here is a bare invocation:
			// `grafel install --register-mcp` never appears inside quotes in
			// user-facing advice.
			for _, form := range []string{"'grafel install'", `"grafel install"`} {
				if strings.Contains(out, form) {
					t.Errorf("final_message %q prescribes bare %s, which is "+
						"unsafe to run at this point: %q", status, form, out)
				}
			}
		})
	}
}

// TestInstallSh_StaleDaemonGetsTheSafeRemedy: it is not enough to drop the
// unsafe advice — the stale branch describes a real problem (the daemon is
// running the previous binary) and must still say how to fix it.
func TestInstallSh_StaleDaemonGetsTheSafeRemedy(t *testing.T) {
	_, out, err := sourceInstallSh(t, fakeGrafelArgLogScript, "final_message 'stale' 1.2.3")
	if err != nil {
		t.Fatalf("final_message stale: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "grafel restart") {
		t.Errorf("the stale branch offers no remedy: %q", out)
	}
}

// TestInstallSh_MainRegistersMCPBeforeReportingSuccess: register_mcp existing is
// not the fix — main() calling it is. This asserts the wiring by grepping the
// script for the call site inside main, which is the one property a runtime
// test of the helper alone cannot reach (main() downloads from the network).
func TestInstallSh_MainRegistersMCPBeforeReportingSuccess(t *testing.T) {
	src := installShSource(t)
	mainIdx := strings.Index(src, "\nmain() {")
	if mainIdx < 0 {
		t.Fatal("could not locate main() in install.sh")
	}
	body := src[mainIdx:]
	callIdx := strings.Index(body, "\n  register_mcp\n")
	if callIdx < 0 {
		t.Fatalf("main() never calls register_mcp; the helper is dead code")
	}
	finalIdx := strings.Index(body, "final_message")
	if finalIdx < 0 {
		t.Fatal("could not locate the closing message in main()")
	}
	if callIdx > finalIdx {
		t.Errorf("register_mcp is called after the success message is printed")
	}
}
