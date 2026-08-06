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
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// NoServiceMutationEnv activates the guard in a process where
// testing.Testing() is false.
//
// That gap is real and cannot be closed from here: testing.Testing() reads a
// flag the linker sets for a test binary, so it is false in anything produced
// by `go build` — and internal/verify/harness_test.go,
// internal/cli/patterns_test.go and internal/daemon/selfdefense_test.go all
// build ./cmd/grafel and exec it. Today those harnesses only run
// `grafel daemon`, which touches no service manager. The moment one of them
// runs `grafel stop`, `install` or `update`, the guard is bypassed completely
// and silently, and the incident this file exists for happens again through a
// door the file cannot see. Every build-and-run harness therefore exports this
// variable to the child.
const NoServiceMutationEnv = "GRAFEL_NO_SERVICE_MUTATION"

// serviceMutationGuardActive reports whether mutating service-manager calls
// should be refused: under `go test`, or whenever a parent test process has
// exported NoServiceMutationEnv into this one.
func serviceMutationGuardActive() bool {
	return testing.Testing() || os.Getenv(NoServiceMutationEnv) == "1"
}

// readOnlyServiceVerbs is an ALLOWLIST of sub-commands known not to change
// service-manager state. Everything else is refused.
//
// The first cut was the inverse — a denylist of known-mutating verbs, with
// "anything not listed is treated as read-only and allowed" — which is
// fail-OPEN however it is described. It was complete for the verbs this
// package happens to use and silently permissive for every other one:
// `launchctl submit`, `setenv`, `kill` and `config` all mutate and were all
// absent. An allowlist fails the right way: a verb nobody has classified is
// refused, and adding a read-only one is a deliberate, reviewable act.
var readOnlyServiceVerbs = map[string]bool{
	// launchctl
	"list":           true,
	"print":          true,
	"print-disabled": true,
	"procinfo":       true,
	"examine":        true,
	"blame":          true,
	"managername":    true,
	"manageruid":     true,
	"managerpid":     true,
	// systemctl
	"is-active":  true,
	"is-enabled": true,
	"is-failed":  true,
	"status":     true,
	"show":       true,
	"cat":        true,
	"list-units": true,
	// schtasks
	"/query": true,
}

// serviceVerb extracts the sub-command from a service-manager argv.
//
// The check has to be on the VERB, not on every argument: systemctl is invoked
// as `--user is-active --quiet <unit>` and schtasks as `/query /tn <name>`, so
// testing each argument would treat flags, unit names and file paths as
// unrecognised verbs and refuse everything. schtasks verbs are `/`-prefixed and
// come first; for the others the verb is the first non-flag argument.
func serviceVerb(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "/") {
			return a // schtasks: /query, /create, /delete, …
		}
		if strings.HasPrefix(a, "-") {
			continue // a flag (--user, --quiet, -k …)
		}
		return a
	}
	return ""
}

// guardServiceCall panics if, while running under `go test` (or with the env
// belt set), a command that is not a known read-only probe is about to be
// executed for real against the OS service manager. See the file comment for
// the incident this prevents.
func guardServiceCall(tool string, args []string) {
	if !serviceMutationGuardActive() {
		return
	}
	verb := serviceVerb(args)
	if readOnlyServiceVerbs[verb] {
		return
	}
	panic(fmt.Sprintf(
		"watchers: test attempted a REAL service-manager call that is not a known "+
			"read-only probe: %s %v\n"+
			"This can change the developer's actual launchd/systemd/Task Scheduler state,\n"+
			"which no environment variable sandboxes (gui/<uid>/<label> is a system\n"+
			"database). Install a fake runner (watchers.SetLaunchctlRunnerForTest, or the\n"+
			"systemctlRunner / schtasksRunner vars) in this test. If %q really is read-only,\n"+
			"add it to readOnlyServiceVerbs deliberately.",
		tool, args, verb))
}

// serviceCallsStubbed, when true, turns every service-manager invocation in
// this package into a success-returning no-op.
//
// It exists because the seam has to be CROSS-PLATFORM. The per-platform runner
// vars (launchctlRunner, systemctlRunner) are build-tagged and Windows drives
// schtasks through an *exec.Cmd rather than a runner func, so a test that is
// itself cross-platform — cleanup_test.go is the one that started all of this —
// has no single seam to reach for. Without one, such a test either invokes the
// developer's real service manager or trips the guard, and the only remaining
// option is to delete the coverage.
var serviceCallsStubbed atomic.Bool

// StubServiceCallsForTest makes every launchctl/systemctl/schtasks invocation
// in this package a no-op that reports success, and returns a restore func.
// Test-only; production code never sets it.
func StubServiceCallsForTest() (restore func()) {
	prev := serviceCallsStubbed.Swap(true)
	return func() { serviceCallsStubbed.Store(prev) }
}

// serviceCallsAreStubbed reports whether runners should short-circuit.
func serviceCallsAreStubbed() bool { return serviceCallsStubbed.Load() }

// GuardServiceCall is the exported form of the guard, for other packages that
// shell out to a service manager.
//
// internal/daemon/service does exactly that, and against a HIGHER-value target
// than the watcher fleet: `com.grafel.daemon` is the label serving the user's
// live MCP session, so a test that reaches its bootout does not disable a
// watcher, it kills the running daemon. Those call sites are package vars
// (seamed) but were never guarded, which is the same "declared a seam, then
// did not use it" shape that let install.go drive real launchctl calls.
//
// A note on why a PATH-shim measurement is not a substitute for this: a shim
// that makes `launchctl list` exit non-zero causes every Unload to
// short-circuit before its bootout, so "zero mutating verbs observed" proves
// only that the path was not reached under that launchd state — not that it
// cannot be. Guarding the call site is what makes the property hold regardless
// of what happens to be loaded.
func GuardServiceCall(tool string, args []string) { guardServiceCall(tool, args) }
