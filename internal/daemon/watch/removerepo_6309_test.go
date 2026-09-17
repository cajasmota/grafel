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

// muProbeTimeout is how long the detector below waits for a FOREIGN goroutine
// to take w.mu while a backend call is in flight. It is not a tuning knob for
// flakiness: it separates two states that differ in kind, not in degree.
//
//   - Caller does NOT hold w.mu. Every other w.mu holder in this package is
//     transient — the loop goroutine takes it once per event (watcher.go), and
//     the reconcile sweep takes it around map reads and around each scanDir
//     (reconcile.go), all of them bounded map/stat work over the 4-directory
//     fixture tree. Go's sync.Mutex enters starvation mode after 1ms of waiting
//     and hands ownership over FIFO, so the probe cannot be starved by a stream
//     of transient acquisitions either. It acquires in well under a millisecond.
//
//   - Caller DOES hold w.mu (the #6309 defect). The backend seam is called from
//     inside the critical section and blocks until the seam returns, so the
//     caller provably cannot release the mutex until this probe gives up. The
//     wait is unbounded, not slow — the probe is observing exactly the deadlock
//     #6309 is about.
//
// So any value large enough to clear the transient case separates them. Two
// seconds is ~3 orders of magnitude of headroom over the transient holders,
// including under -race.
const muProbeTimeout = 2 * time.Second

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
		return false
	}
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
			t.Errorf("%s(%s) was called while holding w.mu — no other goroutine could "+
				"acquire w.mu in %v while this backend call was in flight, and the call "+
				"cannot return until this seam does. On Windows the backend cannot "+
				"complete this call until the loop goroutine drains, and the loop cannot "+
				"drain until it gets this mutex", what, name, muProbeTimeout)
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
			// Comfortably longer than any real holder, and still ~13x under
			// muProbeTimeout, so this pins tolerance rather than the boundary.
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
				"(muProbeTimeout=%v)", muProbeTimeout)
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
