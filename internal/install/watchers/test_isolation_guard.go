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
// The ordering was then "fixed" by moving the disable AFTER the not-loaded
// probe — which silently dropped the persistence guarantee for any unit that
// happened to be unloaded, and had to be reversed again. It now gates on the
// PLIST existing (see loader_darwin.go), which satisfies both requirements at
// once. That two-round detour is the argument for this file: an ordering
// constraint held in place by a comment is one line from being undone by
// someone who does not know why the line is where it is, and the failure is
// SILENT — the suite stays green while the damage accrues on the developer's
// machine. So the invariant is enforced structurally instead: any MUTATING
// service-manager verb that reaches a real exec from a test is refused.
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
	return guardActiveFor(guardUnderGoTest, getenvForGuard(NoServiceMutationEnv))
}

// guardUnderGoTest and getenvForGuard are the guard's two inputs, as seams.
//
// They are seams because the interesting case — a process where
// testing.Testing() is FALSE — is one a test in this package can never be in,
// and both halves of it need pinning: that a shipped binary with the belt set
// gets an ERROR rather than a panic, and that the env var is actually READ
// rather than called and discarded. A mutant doing the latter left every suite
// green while the belt was silently dead, which matters because the belt is the
// only thing standing between a `go build` child and real launchd mutation.
var (
	guardUnderGoTest = testing.Testing()
	getenvForGuard   = os.Getenv
)

// guardActiveFor is the decision, split out from its two inputs so the env
// belt is observable on its own.
//
// Testing serviceMutationGuardActive directly cannot distinguish the belt from
// testing.Testing(): under `go test` the latter is always true, so the belt
// contributes nothing an assertion can see and a mutant deleting it survives
// (it did, in the first cut). The belt exists precisely for the case
// testing.Testing() is FALSE — a `go build` binary that a harness execs —
// which a test in this package can never itself be in. Passing both inputs
// explicitly is the only way to pin it.
func guardActiveFor(underGoTest bool, envValue string) bool {
	return underGoTest || envValue == "1"
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

// guardServiceCall refuses a command that is not a known read-only probe when
// the guard is active. See the file comment for the incident it prevents.
//
// It PANICS under `go test` and returns an ERROR otherwise. That split is not
// cosmetic:
//
//   - Under `go test`, a leak must be impossible to ignore. Every call site in
//     this package is best-effort — watchers.Cleanup is documented idempotent
//     and does `_ = loader.Unload(u)` — so an error would be discarded and the
//     guard would report nothing. A panic is the only signal those call sites
//     cannot swallow.
//
//   - Outside `go test` the guard is reachable in a SHIPPED binary, because the
//     env belt is just an environment variable and internal/verify and
//     internal/daemon export it to a spawned `grafel daemon` — which serves
//     group-delete RPCs that reach Cleanup. Panicking there would abort
//     best-effort code, and it would do so inside a goroutine spawned by
//     sweepFleetWatchers, where no recover() can reach it and the whole process
//     dies. The runners return ([]byte, error) and every call site absorbs an
//     error correctly, so an error is both survivable and honest.
func guardServiceCall(tool string, args []string) error {
	if !serviceMutationGuardActive() {
		return nil
	}
	verb := serviceVerb(args)
	if readOnlyServiceVerbs[verb] {
		return nil
	}
	if !guardUnderGoTest {
		return fmt.Errorf(
			"refusing a mutating service-manager call because %s is set: %s %v "+
				"(this process was told not to change launchd/systemd/Task Scheduler state)",
			NoServiceMutationEnv, tool, args)
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
//
// TEST-ONLY. It is exported, and therefore present in the shipping binary, for
// one reason: the seam has to be reachable from OTHER packages' tests, so
// export_test.go cannot provide it. Nothing in production ever calls it, and
// with it unset the only cost is one atomic load per service-manager
// invocation — of which there are at most a few hundred in the life of a
// process. Do not call it from non-test code.
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
func GuardServiceCall(tool string, args []string) error { return guardServiceCall(tool, args) }
