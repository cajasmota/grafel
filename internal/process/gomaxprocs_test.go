package process

import (
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
)

// gomaxprocs_test.go — behavioural coverage for WithGOMAXPROCSCap and
// ApplyGOMAXPROCS (#6108).
//
// These assert the EFFECTIVE runtime.GOMAXPROCS observed from inside the
// callback, and the SCHEDULING behaviour around it — not the presence of any
// particular source construct.
//
// EVERY TEST PINS ITS OWN BASELINE. GOMAXPROCS is settable to any value
// independent of the host, so nothing here is conditioned on runtime.NumCPU()
// and nothing skips: a 1-core CI runner exercises exactly the same clamps as a
// 12-core laptop. The first draft skipped on small hosts, which meant the
// central clamp assertion silently vanished on precisely the machines most
// likely to be running CI. It also inherited whatever GOMAXPROCS a previous test
// had leaked — a mutant run demonstrated that by turning a FAIL into a SKIP.

// pinGOMAXPROCS installs a deterministic baseline for one test and restores both
// the runtime value and this package's cap bookkeeping afterwards.
func pinGOMAXPROCS(t *testing.T, n int) {
	t.Helper()
	prev := runtime.GOMAXPROCS(n)
	t.Cleanup(func() {
		capMu.Lock()
		activeCaps = nil
		capBaseline, capCurrent = prev, prev
		capMu.Unlock()
		runtime.GOMAXPROCS(prev)
	})
}

func TestWithGOMAXPROCSCap_ClampsInsideCallback(t *testing.T) {
	pinGOMAXPROCS(t, 8)

	var inside int
	if err := WithGOMAXPROCSCap(3, func() error { inside = runtime.GOMAXPROCS(0); return nil }); err != nil {
		t.Fatalf("WithGOMAXPROCSCap: %v", err)
	}
	if inside != 3 {
		t.Errorf("GOMAXPROCS inside the capped region = %d, want 3 — the cap is not in force on the work it is supposed to bound (#6108)", inside)
	}
	if after := runtime.GOMAXPROCS(0); after != 8 {
		t.Errorf("GOMAXPROCS after the capped region = %d, want the prior 8 — the cap leaked past the pass and throttles the whole daemon", after)
	}
}

func TestWithGOMAXPROCSCap_NeverRaises(t *testing.T) {
	pinGOMAXPROCS(t, 4)

	var inside int
	if err := WithGOMAXPROCSCap(16, func() error { inside = runtime.GOMAXPROCS(0); return nil }); err != nil {
		t.Fatalf("WithGOMAXPROCSCap: %v", err)
	}
	if inside != 4 {
		t.Errorf("GOMAXPROCS inside = %d, want the unchanged 4 — a cap ABOVE the current value must never widen the runtime (foreground work is uncapped, not over-capped)", inside)
	}
	if after := runtime.GOMAXPROCS(0); after != 4 {
		t.Errorf("GOMAXPROCS after = %d, want 4", after)
	}
}

func TestWithGOMAXPROCSCap_NonPositiveIsNoOp(t *testing.T) {
	pinGOMAXPROCS(t, 4)

	for _, n := range []int{0, -1} {
		var inside int
		if err := WithGOMAXPROCSCap(n, func() error { inside = runtime.GOMAXPROCS(0); return nil }); err != nil {
			t.Fatalf("WithGOMAXPROCSCap(%d): %v", n, err)
		}
		if inside != 4 {
			t.Errorf("WithGOMAXPROCSCap(%d): GOMAXPROCS inside = %d, want 4 — a bogus core count must not pin the process to 1", n, inside)
		}
	}
	if after := runtime.GOMAXPROCS(0); after != 4 {
		t.Errorf("GOMAXPROCS after = %d, want 4", after)
	}
}

func TestWithGOMAXPROCSCap_RestoresOnErrorAndPanic(t *testing.T) {
	pinGOMAXPROCS(t, 8)

	sentinel := errors.New("boom")
	if err := WithGOMAXPROCSCap(1, func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Errorf("error not propagated: got %v, want %v", err, sentinel)
	}
	if after := runtime.GOMAXPROCS(0); after != 8 {
		t.Errorf("GOMAXPROCS after an erroring pass = %d, want 8", after)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("panic was swallowed — a wedged pass must still crash loudly")
			}
		}()
		_ = WithGOMAXPROCSCap(1, func() error { panic("wedged") })
	}()
	if after := runtime.GOMAXPROCS(0); after != 8 {
		t.Errorf("GOMAXPROCS after a panicking pass = %d, want 8 — the daemon is left throttled for the rest of its life", after)
	}
}

func TestWithGOMAXPROCSCap_ConcurrentRegionsAreAllClamped(t *testing.T) {
	pinGOMAXPROCS(t, 8)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = WithGOMAXPROCSCap(1, func() error {
				if got := runtime.GOMAXPROCS(0); got != 1 {
					t.Errorf("GOMAXPROCS inside a concurrent capped region = %d, want 1", got)
				}
				return nil
			})
		}()
	}
	wg.Wait()
	if after := runtime.GOMAXPROCS(0); after != 8 {
		t.Errorf("GOMAXPROCS after concurrent capped regions = %d, want 8 — a restore raced and the baseline was lost", after)
	}
}

// TestWithGOMAXPROCSCap_NoOpCapDoesNotBlockBehindALiveRegion is the H1
// regression.
//
// The first draft took a package mutex BEFORE deciding whether it had anything
// to clamp, so a call that needed no clamp at all still queued behind every live
// region. In the daemon that means a user-awaited foreground pass — whose cap
// resolves to the host core count and therefore lowers nothing — waits out a
// background sweep that can run for minutes or hours, uncancellably. An
// assertion about the cap VALUE cannot see this: the integer was always right;
// the SCHEDULING was not.
func TestWithGOMAXPROCSCap_NoOpCapDoesNotBlockBehindALiveRegion(t *testing.T) {
	pinGOMAXPROCS(t, 8)

	entered, holdUntil, bgDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(bgDone)
		_ = WithGOMAXPROCSCap(1, func() error { close(entered); <-holdUntil; return nil })
	}()
	<-entered
	defer func() { close(holdUntil); <-bgDone }()

	// A cap at or above the baseline must complete immediately, NOT wait for the
	// background region above to release.
	ran := make(chan struct{})
	go func() {
		_ = WithGOMAXPROCSCap(8, func() error { close(ran); return nil })
	}()
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("a no-op (non-lowering) cap blocked behind a live background region — a foreground pass would serialise behind a multi-hour background sweep (#6108 H1)")
	}
}

// TestWithGOMAXPROCSCap_NestingDoesNotDeadlock: a region opened inside another
// must complete. A plain non-reentrant mutex taken unconditionally wedges the
// daemon permanently here.
func TestWithGOMAXPROCSCap_NestingDoesNotDeadlock(t *testing.T) {
	pinGOMAXPROCS(t, 8)

	done := make(chan int, 1)
	go func() {
		var inner int
		_ = WithGOMAXPROCSCap(2, func() error {
			return WithGOMAXPROCSCap(1, func() error { inner = runtime.GOMAXPROCS(0); return nil })
		})
		done <- inner
	}()
	select {
	case inner := <-done:
		if inner != 1 {
			t.Errorf("nested cap: GOMAXPROCS inside = %d, want the tighter 1", inner)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nested WithGOMAXPROCSCap deadlocked — a permanent daemon wedge (#6108 H1)")
	}
	if after := runtime.GOMAXPROCS(0); after != 8 {
		t.Errorf("GOMAXPROCS after nested regions = %d, want 8", after)
	}
}

// TestWithGOMAXPROCSCap_OverlappingExitDoesNotRaiseAboveALiveSibling: two
// independent regions, the looser exiting first. Its exit must not restore the
// runtime above the tighter one's still-live cap.
func TestWithGOMAXPROCSCap_OverlappingExitDoesNotRaiseAboveALiveSibling(t *testing.T) {
	pinGOMAXPROCS(t, 8)

	aIn, aRelease, aDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	bIn, bRelease, bDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(aDone)
		_ = WithGOMAXPROCSCap(4, func() error { close(aIn); <-aRelease; return nil })
	}()
	<-aIn
	go func() {
		defer close(bDone)
		_ = WithGOMAXPROCSCap(1, func() error { close(bIn); <-bRelease; return nil })
	}()
	<-bIn

	close(aRelease)
	<-aDone
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Errorf("after the looser region exited, GOMAXPROCS = %d, want 1 — the exit raised the runtime above a sibling region's live cap", got)
	}
	close(bRelease)
	<-bDone
	if got := runtime.GOMAXPROCS(0); got != 8 {
		t.Errorf("after all regions exited, GOMAXPROCS = %d, want the baseline 8", got)
	}
}

// TestApplyGOMAXPROCS_OperatorCapSurvivesAnActiveRegion is the H2 regression.
//
// #5137 lets an operator retune the daemon's GOMAXPROCS live via cpu.json +
// SIGHUP. A handler that reads and writes runtime.GOMAXPROCS directly loses that
// change whenever a capped region is open — and it loses it in the dangerous
// direction: the region's restore writes back the pre-region value, leaving the
// daemon permanently ABOVE the operator's configured cap.
func TestApplyGOMAXPROCS_OperatorCapSurvivesAnActiveRegion(t *testing.T) {
	pinGOMAXPROCS(t, 12)

	// Case 1: the operator's target EQUALS the value the region has installed.
	// A direct-read handler sees cur == target and no-ops.
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = WithGOMAXPROCSCap(2, func() error { close(entered); <-release; return nil })
	}()
	<-entered
	if _, changed := ApplyGOMAXPROCS(2); !changed {
		t.Errorf("ApplyGOMAXPROCS(2) during a region already clamped to 2 reported unchanged — the operator's new BASELINE was dropped, so the region's restore will undo it (#6108 H2 / #5137)")
	}
	close(release)
	<-done
	if got := runtime.GOMAXPROCS(0); got != 2 {
		t.Fatalf("after the region exited, GOMAXPROCS = %d, want the operator's 2 — the daemon is running permanently ABOVE its configured cap (#6108 H2 / #5137)", got)
	}

	// Case 2: the operator RAISES mid-region. The region keeps its clamp while
	// live; the raise takes effect on release.
	entered2, release2, done2 := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done2)
		_ = WithGOMAXPROCSCap(1, func() error { close(entered2); <-release2; return nil })
	}()
	<-entered2
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Fatalf("region did not clamp: GOMAXPROCS = %d, want 1", got)
	}
	ApplyGOMAXPROCS(3)
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Errorf("an operator RAISE mid-region lifted the region's clamp to %d — a live background pass must stay bounded", got)
	}
	close(release2)
	<-done2
	if got := runtime.GOMAXPROCS(0); got != 3 {
		t.Errorf("after the region exited, GOMAXPROCS = %d, want the operator's 3 — the restore wrote back a stale baseline (#6108 H2)", got)
	}
}

// TestApplyGOMAXPROCS_TightensImmediatelyMidRegion: an operator LOWERING below a
// live region's cap must take effect at once, not at the end of the pass.
func TestApplyGOMAXPROCS_TightensImmediatelyMidRegion(t *testing.T) {
	pinGOMAXPROCS(t, 12)

	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = WithGOMAXPROCSCap(3, func() error { close(entered); <-release; return nil })
	}()
	<-entered
	ApplyGOMAXPROCS(1)
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Errorf("an operator cap tighter than a live region's did not apply: GOMAXPROCS = %d, want 1", got)
	}
	close(release)
	<-done
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Errorf("after the region exited, GOMAXPROCS = %d, want the operator's 1", got)
	}
}

// TestApplyGOMAXPROCS_NoActiveRegion keeps the plain path honest: with nothing
// in flight it behaves exactly like the direct runtime call it replaces.
func TestApplyGOMAXPROCS_NoActiveRegion(t *testing.T) {
	pinGOMAXPROCS(t, 8)

	prev, changed := ApplyGOMAXPROCS(3)
	if prev != 8 || !changed {
		t.Errorf("ApplyGOMAXPROCS(3) from 8 = (%d, %v), want (8, true)", prev, changed)
	}
	if got := runtime.GOMAXPROCS(0); got != 3 {
		t.Errorf("GOMAXPROCS = %d, want 3", got)
	}
	if prev, changed := ApplyGOMAXPROCS(3); prev != 3 || changed {
		t.Errorf("re-applying the same target = (%d, %v), want (3, false)", prev, changed)
	}
}
