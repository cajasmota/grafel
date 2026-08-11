package watch

import (
	"errors"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ---------------------------------------------------------------------------
// #6287 — Stop() must not tear down the drain before the backend has finished
// closing.
//
// On Windows the whole package hung for 40 minutes in
// TestSkippedDirectoryPushesASubscriptionOverBudget's t.Cleanup(w.Stop). The
// goroutine dump named the mechanism exactly:
//
//	goroutine 18: readDirChangesW.Close  (backend_windows.go:112, `return <-ch`)
//	goroutine 19: readDirChangesW.sendError <- startRead <- readEvents
//	              (backend_windows.go:89, blocked on the Errors channel)
//	and no watch.loop goroutine anywhere in the process.
//
// fsnotify's Windows backend finishes Close by walking its watch set through
// deleteWatch and startRead, both of which SEND on the user-facing Events and
// Errors channels before Close's acknowledgement is delivered. Stop's first
// statement, close(w.stopCh), used to make the loop goroutine return — so by
// the time Stop called fs.Close there was nobody left to receive, the I/O
// thread parked on a channel send, Close never returned, and Stop never
// returned.
//
// Neither test below is Windows-specific: the requirement ("the drain outlives
// the backend's Close") is a property of grafel's own shutdown ordering, and
// both fail deterministically on any host if that ordering regresses.
// ---------------------------------------------------------------------------

// stoppableWatcher builds a watcher WITHOUT registering t.Cleanup(w.Stop). If
// Stop can hang, a cleanup that calls it again would park on stopOnce and take
// the rest of the binary down with it — which is the failure being tested.
//
// ev/errs, when non-nil, become the channels the loop goroutine drains instead
// of the backend's own. A test may only send on channels it owns: fsnotify's
// readEvents goroutine sends on AND closes its own (backend_kqueue.go:441-443),
// so a second sender there is a data race rather than a simulation.
func stoppableWatcher(t *testing.T, ev chan fsnotify.Event, errs chan error) *Watcher {
	t.Helper()
	w, err := NewWatcherConfig(Config{
		Debounce:          time.Hour,
		HeartbeatInterval: time.Hour,
		testEvents:        ev,
		testErrors:        errs,
	}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	return w
}

// shortStopTimeout shrinks the shutdown bound for the duration of a test, so a
// regression costs a couple of seconds rather than the default's twenty.
func shortStopTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := watcherStopTimeout
	watcherStopTimeout = d
	t.Cleanup(func() { watcherStopTimeout = prev })
}

// TestStopLetsTheBackendFinishItsTeardownSends is the regression. The stand-in
// backend does what the Windows one does inside Close — push an event and an
// error out through the user-facing channels — and only a live drain can
// accept them. With the pre-#6287 ordering the loop goroutine has already
// returned by this point and both sends block forever.
func TestStopLetsTheBackendFinishItsTeardownSends(t *testing.T) {
	shortStopTimeout(t, 2*time.Second)
	ev := make(chan fsnotify.Event)
	errs := make(chan error)
	w := stoppableWatcher(t, ev, errs)

	realClose := w.closeBackend
	sent := make(chan struct{})
	w.closeBackend = func() error {
		// backend_windows.go: deleteWatch -> sendEvent (:453, :457) and
		// startRead -> sendError (:465, :475), all before Close's ack.
		// Create, not Chmod: handleEvent returns early on Chmod, so a Chmod
		// could not tell "drained and ignored" from "drained and handled".
		ev <- fsnotify.Event{Name: "teardown", Op: fsnotify.Create}
		errs <- errors.New("teardown")
		close(sent)
		err := realClose()
		// A backend closes its channels as the last act of Close; that is what
		// tells the loop to finish.
		close(ev)
		close(errs)
		return err
	}
	_, _, eventsBefore, _, _ := w.Stats()

	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Stop() never returned at all — even the bounded waits did not fire")
	}
	select {
	case <-sent:
	default:
		t.Fatal("Stop() abandoned the backend mid-Close: the event loop stopped draining " +
			"before fsnotify had finished pushing its teardown events and errors, which is " +
			"the Windows deadlock in #6287")
	}

	// Drained, but NOT acted on. handleEvent is the first thing that would
	// count the event, and it can also call back into w.fs (Add, via
	// subscribeDirRecursive) and arm a reindex — neither is wanted against a
	// backend that is being closed underneath it.
	if _, _, eventsAfter, _, _ := w.Stats(); eventsAfter != eventsBefore {
		t.Fatalf("the loop handled %d event(s) delivered during shutdown (total went %d -> %d); "+
			"after a stop is requested it must drain without acting",
			eventsAfter-eventsBefore, eventsBefore, eventsAfter)
	}
}

// TestStopIsBoundedWhenTheBackendCloseNeverReturns is the belt-and-braces half.
// Whatever else is true of a backend, Stop must not be able to hang: a wedged
// shutdown blanks every remaining test in the package (a 40-minute timeout
// reports nothing about the other 80 tests) and wedges a daemon shutdown. A
// loud, bounded failure is strictly better.
func TestStopIsBoundedWhenTheBackendCloseNeverReturns(t *testing.T) {
	shortStopTimeout(t, 100*time.Millisecond)
	// The real backend's channels: this test never sends, so there is no
	// second sender to race fsnotify's own close.
	w := stoppableWatcher(t, nil, nil)

	realClose := w.closeBackend
	block := make(chan struct{})
	w.closeBackend = func() error {
		<-block
		return realClose()
	}

	done := make(chan struct{})
	start := time.Now()
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		close(block)
		t.Fatal("Stop() is unbounded: a backend whose Close never returns hangs the caller " +
			"forever, and in a test binary that blanks the whole package (#6287)")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Stop() took %s against a bound of %s", elapsed, watcherStopTimeout)
	}
	close(block)
}

// TestStopDoesNotWaitOutItsTimeoutWhenTheHeartbeatIsRestarting is the other
// half of the shutdown contract, and the one the reordering initially broke.
//
// The loop has TWO exits, and only one of them used to be a stop. When the
// backend dies underneath it, the loop pushes restartCh and returns WITHOUT
// signalling the stopper — the heartbeat is expected to build a replacement. A
// Stop landing in that window therefore had nothing left to wait for and burned
// its entire timeout, which in production is a ten-second stall on daemon
// shutdown and, in the sub-case where the heartbeat wins the race, an orphaned
// fsnotify backend whose loop goroutine has no exit at all.
//
// The old `case <-w.stopCh: return` arm covered this interleaving for free.
// Removing it (the Windows fix) means the property now needs its own guard —
// loop generations counted on a WaitGroup, and a restart that stands down once
// a stop is published — and its own test.
//
// Iterated with jitter because the window is an interleaving, not a state: a
// single-shot version would pass against the broken code most of the time.
func TestStopDoesNotWaitOutItsTimeoutWhenTheHeartbeatIsRestarting(t *testing.T) {
	shortStopTimeout(t, 300*time.Millisecond)

	const iterations = 200
	for i := 0; i < iterations; i++ {
		func() {
			ev := make(chan fsnotify.Event)
			errs := make(chan error)
			w := stoppableWatcher(t, ev, errs)

			// The backend dies on its own: the loop takes its unexpected-close
			// exit, asks the heartbeat for a restart, and signals the stopper
			// nothing.
			close(ev)
			close(errs)

			// Land Stop somewhere across the restart. 0 lands before the
			// heartbeat has reacted; a few ms lands during or after it.
			if d := time.Duration(i%4) * time.Millisecond; d > 0 {
				time.Sleep(d)
			}

			start := time.Now()
			w.Stop()
			if elapsed := time.Since(start); elapsed >= watcherStopTimeout {
				t.Fatalf("iteration %d: Stop() took %s, i.e. it waited out the %s bound. "+
					"A stop that lands while the heartbeat is restarting has nothing to "+
					"wait for unless retiring loop generations are accounted (#6287)",
					i, elapsed, watcherStopTimeout)
			}
		}()
	}
}

// TestBackendRestartStandsDownOnceAStopIsPublished pins the other guard, the
// one the iterated test above cannot reach: the window between Stop publishing
// the stop and Stop's closeBackend reading w.fs is a few instructions wide, so
// a restart landing exactly inside it is not something interleaving finds.
// Calling restartBackend directly removes the timing from the question
// entirely.
//
// What it protects: a replacement backend installed after Stop has read past it
// is never closed by anyone, and the loop generation draining it never exits —
// an orphaned fsnotify handle plus a stuck goroutine on the shutdown path.
func TestBackendRestartStandsDownOnceAStopIsPublished(t *testing.T) {
	shortStopTimeout(t, 2*time.Second)
	w := stoppableWatcher(t, nil, nil)

	w.mu.Lock()
	before := w.fs
	w.mu.Unlock()

	w.Stop()

	if w.restartBackend() {
		t.Fatal("restartBackend carried on after a stop was published; the heartbeat would " +
			"keep running and could install another backend")
	}
	w.mu.Lock()
	after := w.fs
	w.mu.Unlock()
	if after != before {
		t.Fatal("restartBackend installed a replacement backend after a stop was published. " +
			"Nothing will ever close it, and the loop generation draining it has no exit (#6287)")
	}

	// And no loop generation was left running by that call: a second Stop
	// (once-guarded, so this is really just the assertion) must not block.
	done := make(chan struct{})
	go func() {
		w.loopWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a loop generation is still running after the stood-down restart")
	}
}
