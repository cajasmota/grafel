package service

// uninstall_kill_identity_7280_test.go — the sole grader of the wiring line
// `var sweepOrphanKill = process.KillGuarded` (#7280, site 1).
//
// Site 1 is the dangerous one, and the reason is not the sweep but its ROOT:
// Uninstall resolves daemon.DefaultLayout(), the real user root, so the pid the
// sweep reads comes from the developer's or CI runner's live engine.pid rather
// than from a t.TempDir(). No test reaches it today — every caller injects, via
// install's stopDaemonFn and internal/cli/uninstall.go — and that was confirmed
// by panic probe over ./internal/daemon/service/, ./internal/install/... and
// ./internal/cli/ rather than assumed. The hazard #7268 exists to close is the
// FUTURE test that forgets to inject, and that shape was live on this repo:
// TestRunDoctorStaleDaemons_DryRunOutputsNoneWhenClean drove production with no
// seam installed and was proved by panic probe to reach the real process.Kill
// with the user's running launchd daemon pid.
//
// WHY IDENTITY AND NOT BEHAVIOUR. The obvious form of this test calls
// sweepOrphanKill(somePID) with no seam and asserts it panics. That test is
// only safe while it PASSES: in the exact failure mode it exists to catch — the
// default reverted to the real process.Kill — it does not panic, it sends
// SIGTERM to somePID. There is no pid that is safe to name (0 and negatives
// address process GROUPS). Here the behavioural form is worse still, because
// the only way to reach this wiring through production is to call Uninstall,
// which tears down the user's real service registration and sweeps their real
// engine.pid. Comparing the function value costs nothing and cannot signal in
// any state.
//
// IT REJECTS EVEN A SEMANTICALLY EQUIVALENT WRAPPER, deliberately — a closure
// has its own code pointer. The fix for a future refactor that needs one is to
// keep this default a bare reference to process.KillGuarded and put the wrapper
// elsewhere, or to change this test having re-established how the guard applies.
//
// The control below establishes that the comparison DISCRIMINATES, not that
// `want` is the right anchor: rewriting want to reflect.ValueOf(sweepOrphanKill)
// is ALIVE and no assertion inside a test whose body IS the comparison can
// catch that. Worth knowing so the control is not read as stronger than it is.
//
// WHAT IT DOES NOT GRADE. It says nothing about Uninstall still wiring
// sweepOrphanKill into sweepOrphanEngineDeps; the var and its use are two
// lines. Reverting `kill: sweepOrphanKill` to `kill: process.Kill` leaves this
// green and is caught instead by
// internal/process/direct_kill_sweep_guard_7280_test.go, which fails on any
// unmarked non-test reference to process.Kill under internal/ or cmd/. Scored
// as a mutant, not asserted here.

import (
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

func TestSweepOrphanKillDefaultIsGuarded_7280(t *testing.T) {
	got := reflect.ValueOf(sweepOrphanKill).Pointer()
	want := reflect.ValueOf(process.KillGuarded).Pointer()
	if got != want {
		t.Fatal("sweepOrphanKill's default is not process.KillGuarded — a test that calls " +
			"service.Uninstall without injecting now reads the REAL user root's engine.pid " +
			"and SIGTERMs whatever it names, which on a developer box or a CI runner is the " +
			"live daemon")
	}
	// Control: the assertion above must be capable of failing. If every func
	// value compared equal, the check would be vacuous.
	if reflect.ValueOf(process.Kill).Pointer() == want {
		t.Fatal("process.Kill and process.KillGuarded compare equal — the identity check is vacuous")
	}
}
