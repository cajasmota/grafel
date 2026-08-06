//go:build darwin

package watchers

// loader_darwin_persist2_test.go pins the property round 2 broke.
//
// Round 1 put `launchctl disable` first, unconditionally — persistent, but it
// made Unload mutate real launchd state for a label that was never loaded.
// Round 2 fixed that by moving the disable AFTER the not-loaded early return,
// which threw away the persistence guarantee for any unit that happens to be
// unloaded at the moment `grafel stop` runs. With KeepAlive={SuccessfulExit:
// false} (#6179) that is a live subset at any instant across 140 units: the
// plist stays on disk with RunAtLoad=true, loads at the next login, and starts
// indexing — while stop said "even across reboot".
//
// The correct gate is neither "always" nor "only when loaded" but "whenever
// there is a plist on disk to suppress". That keeps the guarantee AND keeps a
// never-installed unit from touching launchd at all.

import (
	"os"
	"strconv"
	"testing"
)

// TestUnload_DisablesWhenPlistExistsButJobIsNotLoaded is the regression test
// for the round-2 blocker.
func TestUnload_DisablesWhenPlistExistsButJobIsNotLoaded(t *testing.T) {
	u := testUnit(t)
	if _, err := Write(u); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var calls [][]string
	orig := launchctlRunner
	launchctlRunner = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "list" {
			return nil, os.ErrNotExist // job NOT currently loaded
		}
		return nil, nil
	}
	t.Cleanup(func() { launchctlRunner = orig })

	if err := (darwinLoader{}).Unload(u); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	target := "gui/" + strconv.Itoa(os.Getuid()) + "/" + u.Label()
	if !hasArgv(calls, []string{"disable", target}) {
		t.Fatalf("a plist on disk must be disabled even when the job is not loaded, "+
			"or it comes back at next login; calls=%v", calls)
	}
}

// TestUnload_NoPlistNoMutation keeps the test-hygiene property that motivated
// the round-2 reordering: a unit that was never installed must not touch
// launchd at all.
func TestUnload_NoPlistNoMutation(t *testing.T) {
	u := testUnit(t) // no Write — nothing on disk

	var calls [][]string
	orig := launchctlRunner
	launchctlRunner = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "list" {
			// Never installed implies never loaded. (A job that IS loaded with
			// no plist on disk still gets booted out — that is correct, and is
			// covered by TestUnload_DisablesWithExactTarget.)
			return nil, os.ErrNotExist
		}
		return nil, nil
	}
	t.Cleanup(func() { launchctlRunner = orig })

	if err := (darwinLoader{}).Unload(u); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	for _, c := range calls {
		if len(c) > 0 && !readOnlyServiceVerbs[c[0]] {
			t.Fatalf("Unload of a never-installed unit must issue no mutating call, got %v", c)
		}
	}
}
