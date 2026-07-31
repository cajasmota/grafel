package transport

import (
	"errors"
	"time"
)

// closeBounded exists because closing a Windows named-pipe listener is not
// guaranteed to terminate.
//
// # The defect
//
// github.com/Microsoft/go-winio@v0.6.2 win32PipeListener has three
// interlocking pieces (pipe.go line numbers):
//
//   - Close (575) sends one value on the unbuffered closeCh, then waits on
//     doneCh: `select { case l.closeCh <- 1: <-l.doneCh; case <-l.doneCh: }`.
//   - listenerRoutine (459) loops until `closed`, closing doneCh on exit. Its
//     top-of-loop select (462) can receive closeCh directly, which is the
//     intended shutdown path.
//   - makeConnectedServerPipe (429), reached while an Accept is outstanding,
//     ALSO has a `case <-l.closeCh` (447). It aborts the pending connect,
//     reads the completion at 451, and at 452 maps the result to
//     ErrPipeListenerClosed — but only when that result is nil or
//     ErrFileClosed.
//
// When a connect is aborted by closing its handle, Windows may complete the
// pending I/O with ERROR_OPERATION_ABORTED. That value survives 452 unchanged,
// so listenerRoutine's `closed = err == ErrPipeListenerClosed` at 479 is false
// and the loop goes back to the top select — having already consumed the one
// and only value Close sent. doneCh is never closed and Close blocks forever.
//
// This is not theoretical. It is what killed the windows-latest CI job: the
// goroutine dump shows Close parked at pipe.go:578 and listenerRoutine parked
// at pipe.go:462 on the same listener, for the full 15-minute test timeout.
// The same call shape is on the daemon's own graceful-shutdown path
// (internal/daemon/server.go), where it means `grafel stop` cannot complete —
// the exact user-visible symptom of issue #6044.
//
// # The remedy
//
// The stuck listenerRoutine is not dead; it is parked at 462 with a live
// `case <-l.closeCh`. One more send is therefore exactly the wakeup it needs:
// it sets closed = true, exits the loop, closes the real handle and closes
// doneCh — which releases the original Close call too. So the fix is to issue
// another Close if the first has not returned, and it produces a genuine,
// confirmed close rather than an abandoned handle.
//
// The retry is only ever issued when a close is actually overdue, so the
// normal path calls Close exactly once.

// closeAttemptBudget is how long a single Close is given before another is
// issued.
//
// A healthy Close is a channel rendezvous plus a handle close: microseconds.
// The slowest legitimate case is the abort branch at pipe.go:451, which waits
// for a cancelled kernel I/O to complete — sub-millisecond normally, and tens
// of milliseconds on a loaded CI runner. 250 ms sits two orders of magnitude
// above that, so a close that is merely slow but still progressing is never
// interrupted by a spurious retry, while a genuinely wedged one is detected
// fast enough that shutdown still feels immediate.
const closeAttemptBudget = 250 * time.Millisecond

// closeMaxAttempts bounds the total wait at closeAttemptBudget * attempts.
//
// Each extra closeCh send can be swallowed by at most one in-flight
// makeConnectedServerPipe, and the accept loop only ever has one outstanding
// at a time, so in practice a single retry converges. Four attempts leaves
// headroom for a listener that is being hammered while it shuts down, and
// caps the worst case at 1 s — small against any shutdown watchdog or test
// timeout, and vastly better than blocking forever.
const closeMaxAttempts = 4

// errCloseNotConfirmed reports that the listener did not confirm its close
// within the budget. The caller has stopped waiting; a goroutine and the
// underlying handle may still be pending. That is deliberately worse-but-
// finite: the alternative is a process that never exits. Callers should log
// it rather than discard it, so a recurrence is visible instead of silent.
var errCloseNotConfirmed = errors.New("listener close not confirmed within budget")

// closeBounded closes a listener with the retry policy described above.
func closeBounded(closeFn func() error) error {
	return closeBoundedFor(closeFn, closeAttemptBudget, closeMaxAttempts)
}

// closeBoundedFor is closeBounded with the budget injected, so tests can pin
// the mechanism without paying wall-clock for it.
//
// Concurrent Close calls are safe on win32PipeListener: Close only sends on
// closeCh and reads doneCh, and it is listenerRoutine — not Close — that
// closes the handle and doneCh, exactly once.
func closeBoundedFor(closeFn func() error, perAttempt time.Duration, maxAttempts int) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	// Buffered so a late-returning attempt can always send and exit rather
	// than leaking a goroutine blocked on an unread channel.
	done := make(chan error, maxAttempts)

	timer := time.NewTimer(perAttempt)
	defer timer.Stop()

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// The previous iteration drained timer.C via its <-timer.C case,
			// which is the only way to reach here, so Reset is safe.
			timer.Reset(perAttempt)
		}
		go func() { done <- closeFn() }()

		select {
		case err := <-done:
			return err
		case <-timer.C:
		}
	}

	// A close may have landed in the same instant the last budget expired.
	select {
	case err := <-done:
		return err
	default:
	}
	return errCloseNotConfirmed
}
