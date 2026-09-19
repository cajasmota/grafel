package process

// kill_testguard_test.go — both branches of the guard, neither of which
// signals anything.

import (
	"errors"
	"strings"
	"testing"
)

// TestKillGuardUnderGoTest_IsTrueHere is the premise every other assertion in
// this file rests on. If testing.Testing() ever stopped reporting true under
// `go test`, KillGuarded would silently become a pass-through and the guard
// would be dead while the suite stayed green.
func TestKillGuardUnderGoTest_IsTrueHere(t *testing.T) {
	if !killGuardUnderGoTest {
		t.Fatal("killGuardUnderGoTest is false inside a test binary — the guard is inert")
	}
}

// TestKillGuarded_PanicsInTestBinary drives the REAL exported entry point, so
// the wiring from KillGuarded to the guard is graded, not just the helper.
func TestKillGuarded_PanicsInTestBinary(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("KillGuarded did not panic in a test binary — it would have signalled pid 424242")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value %v is not a string", r)
		}
		// The PID must be named: a guard that fires without saying which
		// process it was about to signal is much harder to act on.
		if !strings.Contains(msg, "424242") {
			t.Errorf("panic message does not name the pid: %q", msg)
		}
	}()
	_ = KillGuarded(424242)
}

// TestKillGuardedFor_ForwardsWhenNotUnderTest grades the production branch
// without signalling: the kill function is injected, so "it forwards, with the
// right pid, and returns the error" is observable against a recorder.
func TestKillGuardedFor_ForwardsWhenNotUnderTest(t *testing.T) {
	var got []int
	sentinel := errors.New("sentinel")
	err := killGuardedFor(false, func(pid int) error {
		got = append(got, pid)
		return sentinel
	}, 4242)

	if len(got) != 1 || got[0] != 4242 {
		t.Errorf("forwarded %v, want exactly [4242]", got)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want the kill function's error returned unchanged", err)
	}
}
