package daemon

// supervise_kill_identity_7280_test.go — the sole grader of the wiring line
// `var reapStaleKill = process.KillGuarded` (#7280, site 2).
//
// WHY IDENTITY AND NOT BEHAVIOUR. The obvious form of this test calls
// reapStaleKill(somePID) with no seam installed and asserts it panics. That
// test is only safe while it PASSES: in the exact failure mode it exists to
// catch — the default reverted to the real process.Kill — it does not panic, it
// sends SIGTERM to somePID. There is no pid that is safe to name (0 and
// negatives address process GROUPS, and a large fixture pid can exist on a host
// with a raised pid_max), so the regression detector would commit the harm once
// on the way to reporting it. Comparing the function value costs nothing and
// cannot signal in any state. #7268 reached the same conclusion at the two
// doctor sites and three reviewers declined the behavioural form; this follows
// it rather than relitigating it.
//
// WHY THE VAR EXISTS AT ALL. Before #7280 the wiring was `kill: process.Kill`
// inline in engineSupervisor.start's composite literal, where no test can
// observe it without calling start. #7268's review had already found that an
// UNOBSERVED default is an ungraded default: replacing KillGuarded with an
// inert function was ALIVE in both doctor packages until the identity test was
// added. Hoisting the default to a named package var is what makes this
// assertion possible; it changes no behaviour, since reapStaleEngineDeps.kill
// is still injected per call by every test that needs a recorder.
//
// IT REJECTS EVEN A SEMANTICALLY EQUIVALENT WRAPPER, deliberately — a closure
// has its own code pointer. The fix for a future refactor that needs one is to
// keep this default a bare reference to process.KillGuarded and put the wrapper
// elsewhere, or to change this test having re-established how the guard applies.
//
// The control below establishes that the comparison DISCRIMINATES, not that
// `want` is the right anchor: rewriting want to reflect.ValueOf(reapStaleKill)
// is ALIVE and no assertion inside a test whose body IS the comparison can
// catch that. Worth knowing so the control is not read as stronger than it is.

import (
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/process"
)

func TestReapStaleKillDefaultIsGuarded_7280(t *testing.T) {
	got := reflect.ValueOf(reapStaleKill).Pointer()
	want := reflect.ValueOf(process.KillGuarded).Pointer()
	if got != want {
		t.Fatal("reapStaleKill's default is not process.KillGuarded — every test that calls " +
			"sup.start(ctx) without injecting reapStaleEngineDeps.kill now sends a REAL SIGTERM " +
			"to whatever pid engine.pid happens to name under its root. ~10 tests call start, " +
			"and they are safe today only because the fixture leaves engine.pid absent")
	}
	// Control: the assertion above must be capable of failing. If every func
	// value compared equal, the check would be vacuous.
	if reflect.ValueOf(process.Kill).Pointer() == want {
		t.Fatal("process.Kill and process.KillGuarded compare equal — the identity check is vacuous")
	}
}

// WHAT THIS TEST DOES NOT GRADE, stated because the var and its USE are two
// separate lines and grading one is not grading the other.
//
// It says nothing about engineSupervisor.start still wiring reapStaleKill into
// the deps it builds. Reverting `kill: reapStaleKill` to `kill: process.Kill`
// leaves this test green — the var would still hold KillGuarded, and nothing
// would read it.
//
// That direction is covered, and covered by a mechanism rather than by this
// file: internal/process/direct_kill_sweep_guard_7280_test.go fails on any
// non-test reference to process.Kill under internal/ or cmd/ that does not
// carry a //killguard:direct marker, so the reverted line is a repo-wide red.
// Scored as a mutant, not asserted here. Wiring the field to some THIRD
// function that is neither — `kill: myOwnKiller` — is caught by neither and is
// the residual hole; it is a deliberate act with no plausible accidental form,
// unlike the revert.
