package watch

import (
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// #6309 — RemoveRepo must not call the fsnotify backend while holding w.mu.
//
// fsnotify's Windows backend serialises Add and Remove through its single I/O
// goroutine (`w.input <- in; return <-in.reply`, backend_windows.go:141-146,
// :162-167), and that same goroutine is the only sender on Events and Errors,
// via a sendEvent/sendError that blocks until grafel receives (:69-91). So a
// backend Remove completes only while grafel's loop goroutine is draining. The
// loop needs w.mu to drain (handleEvent → chargeEventOpen). RemoveRepo held
// w.mu across fs.Remove: the two wait on each other and neither moves.
//
// The kqueue and inotify backends do that work in the caller's goroutine, so
// the hazard is invisible off Windows. The seam is what makes it visible: the
// requirement ("no backend call under the lock") is a property of grafel's own
// locking, and the test below fails deterministically on any host if it
// regresses.
// ---------------------------------------------------------------------------

// muProbeTimeout is how long the detector below waits for ONE attempt by a
// foreign goroutine to take w.mu while a backend call is in flight;
// muProbeAttempts is how many such attempts must ALL time out before the test
// reports a defect.
//
// The two states being separated differ in KIND, not in degree:
//
//   - Caller does NOT hold w.mu. Every other w.mu holder in this package is
//     transient, and more strongly than merely "brief": all 10 w.mu sections in
//     reconcile.go (scanDir 5, nextReconcileBatch 1, repairDir 4) are pure map
//     work — os.ReadDir is outside the lock in scanDir, and repairDir captures
//     add := w.fsAdd under the lock and calls it outside. loop's own direct acquisition (watcher.go:1484) is a
//     startup-only capture before its for loop, so the only acquisition loop can
//     make inside this window is handleEvent → chargeEventOpen, which needs an
//     fs event this test does not generate.
//
//   - Caller DOES hold w.mu (the #6309 defect). The seam is called from inside
//     the critical section and blocks until the seam returns, so the caller
//     provably cannot release the mutex until the probe gives up — across ANY
//     number of attempts. The wait is unbounded, not slow. That is also why
//     retrying is free here: it cannot weaken the true-defect case at all, it
//     only lowers the false-positive rate.
//
// What governs the tail is NOT mutex hold time. Under load the sweep rate
// collapsed (5,175/s to 1.25/s) as the observed wait grew, so the probe is
// losing the CPU, not the mutex: this is SCHEDULER STARVATION of the probe
// goroutine, about which sync.Mutex's starvation-mode FIFO handoff says
// nothing. Measured on this host only (Apple Silicon, macOS), with two
// goroutines driving reconcileOnce at ~280x production cadence plus N busy
// spinners — max wait for one foreign blocking Lock:
//
//	idle          20.7M probes    12.1 ms
//	12 spinners    5.3M probes     113 ms
//	48 spinners    1.9M probes     243 ms
//	96 spinners     981k probes     432 ms
//	-race, 48        34k probes     737 ms
//	-race, 96        32k probes   1.617 s  <- only 1.24x under a single 2s deadline
//
// So one 2s deadline is not the enormous margin it looks like: under -race plus
// heavy CPU oversubscription the tail comes within 1.24x of it. The three
// attempts are what buy the margin back. Two things also work in this test's
// favour: it draws ONE sample per run rather than 10^6, and this package's
// automatic CI legs run WITHOUT -race (only a tag, a manual dispatch or the
// ci:full label add it), where the same load peaked at 432 ms.
//
// Linux and Windows are UNMEASURED; no headroom figure is claimed for them.
//
// WHICH DIRECTION IS DANGEROUS, for whoever edits these next: only DECREASING
// them can break the detector. A larger timeout, or more attempts, makes a
// regression slower to report but never invisible — the seam sits inside the
// critical section, so the timeout is the only path by which detection can
// fire at all. A smaller one spends exactly the starvation margin measured
// above, and that is what a future "let us reduce the flakiness here" edit
// will reach for.
const (
	muProbeTimeout  = 2 * time.Second
	muProbeAttempts = 3
)

// foreignGoroutineCanTakeMu reports whether a goroutine OTHER than the caller
// can acquire w.mu within muProbeTimeout.
//
// This replaces a w.mu.TryLock() probe, which was unsound (#7145): TryLock
// fails if ANY goroutine holds the mutex at that instant, and this test runs a
// watcher whose loop and reconcile goroutines take w.mu at production cadence —
// so a background sweep landing in the window reported the CALLER as the
// holder. Contention at an instant and the caller holding across a blocking
// call are different properties; only the second one is #6309.
//
// On the false branch the spawned goroutine is still parked on w.mu. It is not
// leaked: RemoveRepo releases the mutex when its critical section ends, the
// goroutine then acquires, unlocks and returns.
func foreignGoroutineCanTakeMu(w *Watcher) bool {
	for range muProbeAttempts {
		got := make(chan struct{})
		go func() {
			w.mu.Lock()
			w.mu.Unlock() //nolint:staticcheck // the acquisition is the observation
			close(got)
		}()
		select {
		case <-got:
			return true
		case <-time.After(muProbeTimeout):
			// Retry. Free in the true-defect case — the caller cannot release
			// across the retries either — so this costs only wall time on a
			// genuine regression and buys margin against a starved probe.
		}
	}
	return false
}

// lockAssertingBackend replaces the Add/Remove seams with stand-ins that fail
// if the CALLER holds w.mu when the backend is called, and records what was
// called.
//
// Recording happens on BOTH branches. It used to be skipped on the failing
// branch, which turned one lock complaint into a second, independent-looking
// count failure whose message blamed production for "dropping" backend calls
// that were in fact made and merely not recorded (#7145). The count assertion
// exists to catch a vacuous pass, so it must measure what RemoveRepo did, not
// what the lock detector concluded.
func lockAssertingBackend(t *testing.T, w *Watcher) *[]string {
	t.Helper()
	var mu sync.Mutex
	seen := []string{}
	// Probe at most once. Each detection costs muProbeTimeout, and RemoveRepo
	// calls the seam once per detached directory, so a regression would
	// otherwise pay the timeout for every one of them. One report is the whole
	// signal.
	reported := false
	check := func(what, name string) error {
		mu.Lock()
		skip := reported
		mu.Unlock()
		if !skip && !foreignGoroutineCanTakeMu(w) {
			mu.Lock()
			reported = true
			mu.Unlock()
			t.Errorf("%s(%s): no other goroutine could acquire w.mu in %d attempts of "+
				"%v while this backend call was in flight. The expected reading is that "+
				"the caller holds w.mu across the backend call — the #6309 regression: on "+
				"Windows the backend cannot complete this call until the loop goroutine "+
				"drains, and the loop cannot drain until it gets this mutex. The one "+
				"alternative reading is a probe goroutine starved of CPU for %v total "+
				"without the lock ever being held by the caller; that was measured at a "+
				"1.6s tail only under -race plus heavy oversubscription, so if this test "+
				"is the sole failure on an otherwise green, heavily loaded -race run, "+
				"rule that out before reading it as a production defect",
				what, name, muProbeAttempts, muProbeTimeout,
				time.Duration(muProbeAttempts)*muProbeTimeout)
		}
		mu.Lock()
		seen = append(seen, name)
		mu.Unlock()
		return nil
	}
	w.fsAdd = func(name string) error { return check("Add", name) }
	w.fsRemove = func(name string) error { return check("Remove", name) }
	return &seen
}

// TestRemoveRepoDoesNotCallTheBackendUnderTheLock is the regression. The
// subscription is made against the real backend so the watcher's maps hold
// real subscribed directories; the asserting stand-in is installed only for
// the teardown under test.
//
// The count assertion is not decoration: without it a RemoveRepo that stopped
// unwatching anything at all would pass the lock assertion vacuously.
func TestRemoveRepoDoesNotCallTheBackendUnderTheLock(t *testing.T) {
	root := makeTree(t, 4)
	dirs, files := countTree(t, root)
	w := newBudgetedWatcher(t, dirs+files)

	added, err := w.AddRepo(root)
	if err != nil {
		t.Fatalf("premise broken: AddRepo failed, so RemoveRepo would have nothing "+
			"to unwatch: %v", err)
	}
	if added != dirs {
		t.Fatalf("premise broken: subscribed %d dirs, want %d", added, dirs)
	}

	seen := lockAssertingBackend(t, w)
	w.RemoveRepo(root)

	if len(*seen) != dirs {
		t.Fatalf("RemoveRepo unwatched %d directories (%v), want %d — RemoveRepo must "+
			"still unwatch every directory it subscribed; one that unwatches nothing "+
			"would satisfy the lock assertion above vacuously",
			len(*seen), *seen, dirs)
	}
}

// TestMuProbeDistinguishesAForeignHoldFromACallerHeldLock grades the detector
// itself. The old detector (w.mu.TryLock) conflated these two states, which is
// what made the regression above unsound (#7145):
//
//   - a FOREIGN goroutine holding w.mu for a bounded window — what loop and the
//     reconcile sweep do at production cadence — must NOT be reported, and
//   - the CALLER holding w.mu across the probe must ALWAYS be reported.
//
// A bare Watcher is enough: the probe reads nothing but w.mu, and using a
// zero-value one keeps this free of watcher startup and of the very background
// goroutines whose interference is the subject.
func TestMuProbeDistinguishesAForeignHoldFromACallerHeldLock(t *testing.T) {
	t.Run("foreign transient hold is not reported", func(t *testing.T) {
		w := &Watcher{}
		held, done := make(chan struct{}), make(chan struct{})
		go func() {
			w.mu.Lock()
			close(held)
			// Longer than any real holder measured here (12.1 ms idle, 432 ms
			// under a 96-spinner non-race load), and still well under one
			// muProbeTimeout — so this pins tolerance, not the boundary.
			time.Sleep(150 * time.Millisecond)
			w.mu.Unlock()
			close(done)
		}()
		<-held
		if w.mu.TryLock() {
			w.mu.Unlock() //nolint:staticcheck // premise, not the assertion
			t.Fatal("premise broken: the foreign goroutine was not holding w.mu, so " +
				"this case does not exercise foreign contention at all")
		}
		got := foreignGoroutineCanTakeMu(w)
		<-done
		if !got {
			t.Errorf("the probe reported the caller as the holder while only a FOREIGN "+
				"goroutine held w.mu — that is the #7145 false positive, back again "+
				"(muProbeAttempts=%d x muProbeTimeout=%v)", muProbeAttempts, muProbeTimeout)
		}
	})

	t.Run("caller-held lock is reported", func(t *testing.T) {
		w := &Watcher{}
		w.mu.Lock()
		got := foreignGoroutineCanTakeMu(w)
		w.mu.Unlock()
		if got {
			t.Error("the probe said w.mu was acquirable while this goroutine held it, " +
				"so it can no longer detect #6309 at all")
		}
	})
}
