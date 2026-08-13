package watch

import (
	"sync"
	"testing"
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

// lockAssertingBackend replaces the Add/Remove seams with stand-ins that fail
// if w.mu is held when the backend is called, and records what was called.
// TryLock is exact here: the only contended holder in this test is the caller
// itself.
func lockAssertingBackend(t *testing.T, w *Watcher) *[]string {
	t.Helper()
	var mu sync.Mutex
	seen := []string{}
	check := func(what, name string) error {
		if !w.mu.TryLock() {
			t.Errorf("%s(%s) was called while holding w.mu — on Windows the backend "+
				"cannot complete this call until the loop goroutine drains, and the loop "+
				"cannot drain until it gets this mutex", what, name)
			return nil
		}
		w.mu.Unlock()
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
		t.Fatalf("RemoveRepo unwatched %d directories (%v), want %d — the backend "+
			"calls were dropped rather than moved out of the critical section",
			len(*seen), *seen, dirs)
	}
}
