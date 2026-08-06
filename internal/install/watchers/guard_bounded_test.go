//go:build darwin

package watchers

// guard_bounded_test.go covers the two properties the first mutation round
// found unprotected: the test-isolation guard itself, and cmd.WaitDelay.

import (
	"strings"
	"testing"
	"time"
)

// TestGuardServiceCall_PanicsOnMutatingVerb: the guard is the structural
// backstop that stops a test writing into the developer's real launchd
// database. If it stops firing, nothing else notices.
func TestGuardServiceCall_PanicsOnMutatingVerb(t *testing.T) {
	for _, verb := range []string{"disable", "enable", "bootout", "bootstrap"} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("guardServiceCall(%q) did not panic", verb)
				}
				if !strings.Contains(r.(string), verb) {
					t.Fatalf("panic message does not name the verb: %v", r)
				}
			}()
			guardServiceCall("launchctl", []string{verb, "gui/0/x"})
		}()
	}
}

// TestGuardServiceCall_AllowsReadOnlyVerbs: `launchctl list` / `print` are
// common, harmless and legitimately exercised, so the guard must not block
// them — a guard that fires on everything gets disabled.
func TestGuardServiceCall_AllowsReadOnlyVerbs(t *testing.T) {
	for _, verb := range []string{"list", "print", "print-disabled"} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("guardServiceCall(%q) must not panic, got %v", verb, r)
				}
			}()
			guardServiceCall("launchctl", []string{verb, "com.grafel.watcher.x.y"})
		}()
	}
}

// TestRunBoundedServiceCmd_ReturnsWhenAGrandchildHoldsThePipe is the WaitDelay
// test.
//
// A context deadline alone is NOT enough: exec.CommandContext's cancel kills
// only the DIRECT child, while CombinedOutput blocks until every writer to the
// output pipe has closed it. A grandchild that inherited stdout keeps that pipe
// open, so Wait blocks well past the deadline — measured elsewhere in this tree
// at 20.5s past a 1s deadline. cmd.WaitDelay is what caps it unconditionally.
//
// The fixture reproduces exactly that shape: /bin/sh backgrounds a long sleep
// (which inherits the pipe) and exits immediately. Without WaitDelay this call
// blocks for the full sleep; with it, it returns within the grace period.
func TestRunBoundedServiceCmd_ReturnsWhenAGrandchildHoldsThePipe(t *testing.T) {
	origT, origW := launchctlTimeout, launchctlWaitDelay
	launchctlTimeout = 200 * time.Millisecond
	launchctlWaitDelay = 300 * time.Millisecond
	t.Cleanup(func() { launchctlTimeout, launchctlWaitDelay = origT, origW })

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		_, _ = runBoundedServiceCmd("/bin/sh", "-c", "sleep 30 & exit 0")
		done <- time.Since(start)
	}()

	select {
	case elapsed := <-done:
		// Generous ceiling: the point is "seconds, not 30 seconds".
		if elapsed > 5*time.Second {
			t.Fatalf("runBoundedServiceCmd took %s — WaitDelay is not capping the pipe wait", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runBoundedServiceCmd never returned: a grandchild holding the pipe blocks it forever")
	}
}
