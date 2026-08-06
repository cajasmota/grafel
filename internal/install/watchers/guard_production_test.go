package watchers

// guard_production_test.go pins the two properties that separate a guard from a
// liability: it must never panic in a SHIPPED binary, and the plumbing that
// activates it outside `go test` must itself be exercised.

import (
	"strings"
	"testing"
)

// withGuardInputs forces the guard's two inputs for the duration of a test.
// Only these seams make the production case reachable: a test process always
// has testing.Testing()==true, so nothing else can observe what a `go build`
// binary would do.
func withGuardInputs(t *testing.T, underGoTest bool, env string) {
	t.Helper()
	origTest, origGetenv := guardUnderGoTest, getenvForGuard
	guardUnderGoTest = underGoTest
	getenvForGuard = func(k string) string {
		if k == NoServiceMutationEnv {
			return env
		}
		return ""
	}
	t.Cleanup(func() { guardUnderGoTest, getenvForGuard = origTest, origGetenv })
}

// TestGuard_BeltInProductionReturnsErrorNeverPanics is the finding-1 test.
//
// The belt is reachable in a shipped binary — internal/verify and
// internal/daemon now export it to a spawned `grafel daemon`, which serves
// group-delete RPCs that reach watchers.Cleanup. A panic there is three wrongs
// at once: it aborts code whose own contract is best-effort (Cleanup is
// documented idempotent, and `_ = loader.Unload(u)` deliberately discards), it
// happens inside a goroutine spawned by sweepFleetWatchers where no recover()
// can reach it so the whole process dies, and it says "test attempted" when
// there is no test.
func TestGuard_BeltInProductionReturnsErrorNeverPanics(t *testing.T) {
	withGuardInputs(t, false /* NOT under go test */, "1" /* belt set */)

	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("guardServiceCall must NOT panic outside go test, got: %v", r)
			}
		}()
		err = guardServiceCall("launchctl", []string{"bootout", "gui/501/com.grafel.daemon"})
	}()
	if err == nil {
		t.Fatal("guardServiceCall must return an error when the belt is set")
	}
	if strings.Contains(err.Error(), "test attempted") {
		t.Fatalf("the production message must not claim a test is running: %q", err)
	}
	if !strings.Contains(err.Error(), "bootout") {
		t.Fatalf("error should name the refused verb: %q", err)
	}
}

// TestGuard_PanicsUnderGoTest keeps the loud behaviour where it belongs: a real
// test leak must be impossible to ignore, not returned as an error some
// best-effort call site discards.
func TestGuard_PanicsUnderGoTest(t *testing.T) {
	withGuardInputs(t, true /* under go test */, "")
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("guardServiceCall must panic under go test")
		}
	}()
	_ = guardServiceCall("launchctl", []string{"bootout", "gui/501/x"})
}

// TestGuard_InertInProductionWithoutTheBelt: a shipped binary with no belt must
// be completely unaffected — no panic, no error, no behaviour change.
func TestGuard_InertInProductionWithoutTheBelt(t *testing.T) {
	withGuardInputs(t, false, "")
	if err := guardServiceCall("launchctl", []string{"bootout", "gui/501/x"}); err != nil {
		t.Fatalf("guard must be inert in production, got %v", err)
	}
}

// TestGuard_ReadsTheEnvVarByName is the finding-3 test (mutant P5').
//
// guardActiveFor's decision was pinned, but the plumbing from os.Getenv into it
// was not: keeping the os.Getenv call and discarding its value left every suite
// green while the belt was silently dead. Since finding 1 proves the belt is
// the ONLY thing between a `go build` child and real launchd mutation — with
// two harnesses now depending on it — a dead belt is a silent removal of the
// protection, not a cosmetic defect.
func TestGuard_ReadsTheEnvVarByName(t *testing.T) {
	var asked []string
	origTest, origGetenv := guardUnderGoTest, getenvForGuard
	guardUnderGoTest = false
	getenvForGuard = func(k string) string {
		asked = append(asked, k)
		if k == NoServiceMutationEnv {
			return "1"
		}
		return ""
	}
	t.Cleanup(func() { guardUnderGoTest, getenvForGuard = origTest, origGetenv })

	if !serviceMutationGuardActive() {
		t.Fatal("the belt's value must reach the decision, not be read and discarded")
	}
	found := false
	for _, k := range asked {
		if k == NoServiceMutationEnv {
			found = true
		}
	}
	if !found {
		t.Fatalf("guard never read %s; asked=%v", NoServiceMutationEnv, asked)
	}
}
