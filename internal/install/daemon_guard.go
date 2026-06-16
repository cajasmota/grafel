// daemon_guard.go implements the uninstall safety guard for issue #5277.
//
// The OS service label is GLOBAL (launchd `com.grafel.daemon`, systemd
// `grafel-daemon.service`, Windows task `com.grafel.daemon`). HOME /
// GRAFEL_DAEMON_ROOT isolation redirects state, sockets and config, but it does
// NOT scope that global service label. So a `grafel uninstall` run in an
// isolated HOME (e.g. an agent sandbox) would call
// `launchctl bootout gui/$uid/com.grafel.daemon` against the DEVELOPER'S live
// daemon and unregister it — KeepAlive cannot respawn an unloaded job. That
// exact sequence took down a live MCP daemon.
//
// The guard below decides, BEFORE any stop/bootout, whether the global service
// actually belongs to the install being removed. It only ever stops the daemon
// whose recorded root matches the uninstall target's root, and it never touches
// the global label when running under an isolated test/sandbox home.
package install

import (
	"os"
	"path/filepath"
	"strings"
)

// daemonStopDecision is the result of evaluating whether uninstall may stop the
// global OS daemon service.
type daemonStopDecision struct {
	// Stop is true when it is safe to stop/unregister the daemon (the global
	// service belongs to this uninstall target).
	Stop bool
	// Reason is a human-readable explanation, emitted as a WARN when Stop is
	// false so the user understands why their daemon was left running.
	Reason string
}

// evaluateDaemonStop is the pure decision function (no I/O) behind the #5277
// guard. Inputs:
//
//   - registeredRoot: the root the LIVE daemon serves (HOME baked into the
//     installed plist/unit). registeredFound reports whether a service was
//     installed at all.
//   - registeredErr: true when reading the recorded root failed (parse/IO).
//   - targetRoot: the root of the install being uninstalled (this process's
//     HOME / GRAFEL_DAEMON_ROOT).
//   - isolatedHome: true when GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1 (running in a
//     sandbox). In that mode we must NEVER touch a global service whose root we
//     cannot positively prove is our own.
//
// Decision table:
//   - No service installed (and no read error)  → Stop (idempotent no-op stop).
//   - Read error                                → SKIP (fail closed).
//   - Roots known and EQUAL                     → Stop.
//   - Roots known and DIFFERENT                 → SKIP (belongs to another root).
//   - Recorded root unknown + isolated home     → SKIP (can't prove ownership).
//   - Recorded root unknown + NOT isolated      → Stop (legacy/primary install).
func evaluateDaemonStop(registeredRoot string, registeredFound, registeredErr bool, targetRoot string, isolatedHome bool) daemonStopDecision {
	if registeredErr {
		return daemonStopDecision{
			Stop:   false,
			Reason: "could not read the installed daemon's recorded root; failing closed and leaving it running",
		}
	}

	// No service installed → the stop is a harmless idempotent no-op. Allow it
	// so a normal teardown of a never-fully-installed state still cleans up
	// stale artifacts.
	if !registeredFound {
		return daemonStopDecision{Stop: true, Reason: "no daemon service installed"}
	}

	reg := canonicalRoot(registeredRoot)
	tgt := canonicalRoot(targetRoot)

	// We positively know both roots: only stop when they match.
	if reg != "" && tgt != "" {
		if reg == tgt {
			return daemonStopDecision{Stop: true, Reason: "daemon root matches uninstall target"}
		}
		return daemonStopDecision{
			Stop: false,
			Reason: "skipping daemon stop: live daemon root " + registeredRoot +
				" != uninstall target root " + targetRoot,
		}
	}

	// Recorded root is unknown (legacy unit without HOME, or a platform that
	// doesn't bake it, e.g. Windows). Under an isolated/sandbox home we refuse
	// to touch the global label — that is exactly the outage scenario.
	if isolatedHome {
		return daemonStopDecision{
			Stop: false,
			Reason: "skipping daemon stop: running under an isolated home " +
				"(GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1) and the live daemon's root could not be " +
				"confirmed as the uninstall target",
		}
	}

	// Not isolated and root unknown → this is the primary/real install path;
	// behave as before (#4462) and stop our own daemon.
	return daemonStopDecision{Stop: true, Reason: "daemon root not recorded; primary (non-isolated) uninstall"}
}

// canonicalRoot normalises a root path for comparison: cleaned and, on
// case-insensitive filesystems, lower-cased. Mirrors transport.rootHash's
// canonicalisation so the comparison is robust to spelling variants. An empty
// input stays empty (meaning "unknown").
func canonicalRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	return strings.ToLower(filepath.Clean(root))
}

// isolatedHomeActive reports whether the process is running under the
// fail-closed sandbox guard (GRAFEL_TEST_REQUIRE_ISOLATED_HOME=1).
func isolatedHomeActive() bool {
	return os.Getenv("GRAFEL_TEST_REQUIRE_ISOLATED_HOME") == "1"
}

// uninstallTargetRoot returns the root of the install being removed. It is
// compared against service.RegisteredRoot(), which reads the HOME baked into
// the installed plist/unit — so the target must be resolved on the SAME
// dimension: HOME (the value the unit files record), preferring the HOME env
// var so an isolated sandbox home is honoured. os.UserHomeDir() is the final
// fallback.
func uninstallTargetRoot() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}
