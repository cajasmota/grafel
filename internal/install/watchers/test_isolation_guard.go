package watchers

// test_isolation_guard.go — fail-closed guard against a test mutating the
// developer's real OS service manager.
//
// ── The incident this exists for ─────────────────────────────────────────────
//
// The first cut of the persistent-stop fix put `launchctl disable` as the FIRST
// statement of darwinLoader.Unload, ahead of the read-only `launchctl list`
// guard that had made Unload a no-op for a label that was never loaded.
// cleanup_test.go calls watchers.Cleanup with no launchctl seam — it redirects
// $HOME, which is irrelevant, because `disable` targets `gui/<uid>/<label>`, a
// SYSTEM database that no environment variable sandboxes. Running
// `go test ./internal/install/...` therefore wrote
//
//	com.grafel.watcher.demo.core  => disabled
//	com.grafel.watcher.demo.never => disabled
//
// into the developer's launchd override database, permanently, with no cleanup.
// Those labels exist nowhere but two test fixtures. Had a real user's group
// been named `demo` with a repo basename `core` — entirely plausible — the test
// suite would have silently and persistently disabled their live watcher.
//
// ── Why a guard and not just a fixed ordering ────────────────────────────────
//
// The ordering IS fixed (see loader_darwin.go: disable now runs only after the
// not-loaded early return). But that fix is one line away from being undone by
// anyone who does not know why the line is where it is, and the failure is
// SILENT — the suite stays green while the damage accrues on the developer's
// machine. So the invariant is enforced structurally instead: under `go test`,
// any MUTATING service-manager verb that reaches a real exec is a panic.
// Read-only verbs (list/print) are allowed through: they are common, harmless,
// and several tests legitimately probe them.
//
// A test that genuinely needs to exercise a mutating path installs a fake
// runner (SetLaunchctlRunnerForTest / the launchctlRunner var), which never
// reaches this guard. It is inert in the shipping binary — testing.Testing() is
// false there — so production behaviour is untouched.

import (
	"fmt"
	"testing"
)

// mutatingServiceVerbs are the sub-commands that change OS service-manager
// state. Anything not listed is treated as read-only and allowed.
var mutatingServiceVerbs = map[string]bool{
	// launchctl
	"disable":   true,
	"enable":    true,
	"bootout":   true,
	"bootstrap": true,
	"load":      true,
	"unload":    true,
	"kickstart": true,
	"remove":    true,
	"stop":      true,
	"start":     true,
	// systemctl (verb is args[1] there; guardServiceCall checks every arg)
	"daemon-reload": true,
	// schtasks
	"/create": true,
	"/delete": true,
	"/run":    true,
	"/end":    true,
	"/change": true,
}

// guardServiceCall panics if, while running under `go test`, a MUTATING
// service-manager command is about to be executed for real. See the file
// comment for the incident this prevents.
func guardServiceCall(tool string, args []string) {
	if !testing.Testing() {
		return
	}
	for _, a := range args {
		if mutatingServiceVerbs[a] {
			panic(fmt.Sprintf(
				"watchers: test attempted a REAL mutating service-manager call: %s %v\n"+
					"This would change the developer's actual launchd/systemd state, which no\n"+
					"environment variable sandboxes (gui/<uid>/<label> is a system database).\n"+
					"Install a fake runner (watchers.SetLaunchctlRunnerForTest) in this test.",
				tool, args))
		}
	}
}
