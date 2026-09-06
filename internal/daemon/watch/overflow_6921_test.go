package watch

// overflow_6921_test.go — #6921. fsnotify's ErrEventOverflow was consumed by
// the same `logger.Error("watcher: error")` arm as every other backend error,
// so an inotify IN_Q_OVERFLOW (or a Windows buffer overflow) dropped an UNKNOWN
// set of events, was never redelivered — fsnotify is edge-triggered — and left
// the graph silently stale while the watcher went on reporting healthy.
//
// SCOPE. The overflow is a property of the fsnotify INSTANCE, not of a repo:
// the queue whose depth is exceeded is the one inotify keeps per inotify fd.
// grafel constructs exactly one Watcher per daemon process
// (internal/daemon/engineplane.go:203), and one Watcher owns one
// *fsnotify.Watcher (Watcher.fs, watcher.go:180) shared by every subscribed
// repo. So an overflow invalidates EVERY subscribed repo, which is what
// TestQueueOverflowRescansEverySubscribedRepo pins — a per-repo rescan would be
// a fix that leaves most of the staleness in place.
//
// The whole suite here drives the loop's error channel directly and asserts
// against the injected clock, never a sleep. Nothing below is platform-gated:
// kqueue and fen never RAISE ErrEventOverflow (fsnotify.go:262-270), but
// grafel's handling of one is its own code and is required to behave
// identically wherever it is compiled.

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// overflowHarness is a Watcher whose loop goroutine drains channels the TEST
// owns, so an ErrEventOverflow can be delivered on any platform without asking
// the kernel to co-operate.
//
// The real backend stays live — AddRepo must subscribe a real watch set for
// "every subscribed repo" to mean anything — and its own Events/Errors get a
// discard sink for the lifetime of the test. Withholding events from the LOOP
// is not the same as leaving the backend unread: on Windows an unread backend
// deadlocks the next Add (see watcher.go:172-185 and #6380).
type overflowHarness struct {
	w    *Watcher
	errs chan error
	clk  *manualClock

	mu      sync.Mutex
	rescans []string
	bulkAll bool // false once any sink call arrived with bulk=false
	fired   chan string
}

func newOverflowHarness(t *testing.T) *overflowHarness {
	t.Helper()
	h := &overflowHarness{
		bulkAll: true,
		fired:   make(chan string, 64),
	}
	ev := make(chan fsnotify.Event)
	errs := make(chan error)
	w, err := NewWatcherConfig(Config{
		Debounce:          time.Hour,
		BulkThreshold:     10000,
		HeartbeatInterval: time.Hour,
		reconcileInterval: -1,
		disableQuarantine: true,
		testEvents:        ev,
		testErrors:        errs,
	}, func(repo string, bulk bool) {
		h.mu.Lock()
		h.rescans = append(h.rescans, repo)
		if !bulk {
			h.bulkAll = false
		}
		h.mu.Unlock()
		h.fired <- repo
	}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	// Installed before any repo is subscribed, so nothing is ever armed against
	// the real clock.
	h.clk = newManualClock()
	w.clk = h.clk
	h.w, h.errs = w, errs

	w.mu.Lock()
	fw := w.fs
	w.mu.Unlock()
	drained := discardBackendChannels(fw.Events, fw.Errors)
	realClose := w.closeBackend
	w.closeBackend = func() error {
		cerr := realClose()
		select {
		case <-drained:
		case <-time.After(5 * time.Second):
			t.Errorf("the backend discard sink outlived Close by 5s")
		}
		close(ev)
		close(errs)
		return cerr
	}
	t.Cleanup(w.Stop)
	return h
}

// addRepo subscribes a fresh single-file repo and returns its absolute path.
func (h *overflowHarness) addRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.w.AddRepo(dir); err != nil {
		t.Fatalf("AddRepo(%s): %v", dir, err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// markStopping publishes the shutdown signal the way Stop's first statement
// does, WITHOUT the rest of Stop. stopOnce is consumed here so the harness's
// own t.Cleanup(w.Stop) becomes a no-op rather than a second close of stopCh;
// the backend is instead released by the cleanup registered below, which runs
// first (LIFO) and closes ev/errs, retiring the loop.
func (h *overflowHarness) markStopping(t *testing.T) {
	t.Helper()
	h.w.stopOnce.Do(func() {
		h.w.mu.Lock()
		close(h.w.stopCh)
		h.w.mu.Unlock()
	})
	t.Cleanup(func() { _ = h.w.closeBackend() })
}

// send delivers one error to the loop and returns only once the loop has
// FINISHED handling it. errs is unbuffered, so the second send completes only
// after the loop has come back around to its select — which is the
// synchronisation point that lets the assertions below be exact rather than
// polled. The sentinel is a plain error, which the fix must ignore.
func (h *overflowHarness) send(err error) {
	h.errs <- err
	h.errs <- errors.New("sentinel: not an overflow")
}

// awaitRescans collects exactly n sink calls, failing if fewer arrive in time.
// The rescan runs on its own goroutine (the loop goroutine must never block on
// the sink: it is the sole receiver on the backend's Events), so this is a
// bounded wait on a real signal, not a sleep.
func (h *overflowHarness) awaitRescans(t *testing.T, n int) []string {
	t.Helper()
	var got []string
	for i := 0; i < n; i++ {
		select {
		case r := <-h.fired:
			got = append(got, r)
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d of %d expected rescan sink calls arrived", len(got), n)
		}
	}
	return got
}

// noMoreRescans asserts nothing further is queued. Safe to call immediately
// after a send() returns: the counter mutations happen synchronously on the
// loop goroutine, so a rescan that was going to be armed already has been.
func (h *overflowHarness) noMoreRescans(t *testing.T) {
	t.Helper()
	select {
	case r := <-h.fired:
		t.Fatalf("unexpected extra rescan of %s", r)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestQueueOverflowRescansEverySubscribedRepo is the headline regression, and
// the scope assertion: one overflow on the single shared fsnotify instance must
// rescan BOTH subscribed repos with bulk=true — the EventSink(repo, true)
// contract the heartbeat's restart path already uses for the same "we lost
// events" condition. Before #6921 this arm only logged.
func TestQueueOverflowRescansEverySubscribedRepo(t *testing.T) {
	h := newOverflowHarness(t)
	a := h.addRepo(t)
	b := h.addRepo(t)

	h.send(fsnotify.ErrEventOverflow)

	got := h.awaitRescans(t, 2)
	seen := map[string]bool{}
	for _, r := range got {
		seen[r] = true
	}
	if !seen[a] || !seen[b] {
		t.Fatalf("overflow rescanned %v; want both %s and %s — the overflow is per fsnotify "+
			"INSTANCE and grafel runs one instance for every repo, so a partial rescan leaves "+
			"the rest of the daemon's repos silently stale", got, a, b)
	}
	h.mu.Lock()
	bulk := h.bulkAll
	h.mu.Unlock()
	if !bulk {
		t.Error("a recovery rescan was signalled with bulk=false: an overflow means the event " +
			"history is INCOMPLETE, so a file-level diff has nothing to diff against")
	}

	overflows, rescans, coalesced, last := h.w.OverflowStats()
	if overflows != 1 || rescans != 1 || coalesced != 0 {
		t.Errorf("OverflowStats() = (overflows=%d rescans=%d coalesced=%d), want (1,1,0)",
			overflows, rescans, coalesced)
	}
	if last.IsZero() {
		t.Error("OverflowStats() reported no last-overflow time; `grafel status` has nothing to show")
	}
}

// TestPlainBackendErrorNeverRescans is the over-firing guard. Recall alone
// cannot catch a predicate that fires on everything, and every non-overflow
// error still reaching this channel (EACCES on a watch, a syscall failure)
// would then reindex the whole daemon.
func TestPlainBackendErrorNeverRescans(t *testing.T) {
	h := newOverflowHarness(t)
	h.addRepo(t)

	h.send(errors.New("permission denied"))
	h.send(os.ErrPermission)

	h.noMoreRescans(t)
	overflows, rescans, coalesced, last := h.w.OverflowStats()
	if overflows != 0 || rescans != 0 || coalesced != 0 || !last.IsZero() {
		t.Fatalf("a non-overflow error was counted as an overflow: (overflows=%d rescans=%d "+
			"coalesced=%d last=%v)", overflows, rescans, coalesced, last)
	}
}

// TestOverflowStormCoalescesIntoOneRescan: an overflow storm must not become a
// reindex storm. inotify raises IN_Q_OVERFLOW once per read that finds the flag
// set, so a single burst can produce many; a rescan per overflow would reindex
// every repo in the daemon several times over, generating exactly the churn
// that overflowed the queue.
//
// Bound is a cooldown measured on the injected clock, so this test asserts the
// window rather than outrunning it.
func TestOverflowStormCoalescesIntoOneRescan(t *testing.T) {
	h := newOverflowHarness(t)
	h.addRepo(t)

	for i := 0; i < 5; i++ {
		h.send(fsnotify.ErrEventOverflow)
	}
	h.awaitRescans(t, 1)
	h.noMoreRescans(t)

	overflows, rescans, coalesced, _ := h.w.OverflowStats()
	if overflows != 5 || rescans != 1 || coalesced != 4 {
		t.Fatalf("OverflowStats() = (overflows=%d rescans=%d coalesced=%d), want (5,1,4)",
			overflows, rescans, coalesced)
	}

	// Still inside the window: one tick short of the cooldown changes nothing.
	h.clk.Advance(overflowRescanCooldown - time.Nanosecond)
	h.send(fsnotify.ErrEventOverflow)
	h.noMoreRescans(t)

	// Past it: a genuinely new overflow is a genuinely new staleness event.
	h.clk.Advance(time.Nanosecond)
	h.send(fsnotify.ErrEventOverflow)
	h.awaitRescans(t, 1)

	overflows, rescans, coalesced, _ = h.w.OverflowStats()
	if overflows != 7 || rescans != 2 || coalesced != 5 {
		t.Fatalf("after the cooldown elapsed OverflowStats() = (overflows=%d rescans=%d "+
			"coalesced=%d), want (7,2,5)", overflows, rescans, coalesced)
	}
}

// TestOverflowDuringStopDoesNotRescan: Stop tears the backend down and the loop
// keeps draining what the teardown pushes out (#6287). Acting on an overflow
// there would arm a reindex against a watcher that is going away.
func TestOverflowDuringStopDoesNotRescan(t *testing.T) {
	h := newOverflowHarness(t)
	h.addRepo(t)

	h.markStopping(t)
	h.send(fsnotify.ErrEventOverflow)

	h.noMoreRescans(t)
	if overflows, rescans, _, _ := h.w.OverflowStats(); overflows != 0 || rescans != 0 {
		t.Fatalf("an overflow arriving during shutdown was acted on: overflows=%d rescans=%d",
			overflows, rescans)
	}
}
