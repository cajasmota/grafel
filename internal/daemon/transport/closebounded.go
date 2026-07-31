package transport

import (
	"errors"
	"time"
)

// closeBounded exists because closing a Windows named-pipe listener is not
// guaranteed to terminate.
//
// # The observed wedge
//
// The windows-latest CI job died at a 15-minute test timeout with a goroutine
// dump showing, on one github.com/Microsoft/go-winio@v0.6.2 listener:
//
//   - Close parked at pipe.go:578 — `<-l.doneCh`
//   - that listener's own listenerRoutine parked at pipe.go:462 — the
//     top-of-loop select — with `closed` still false
//
// Those two facts together are diagnostic. Close sends exactly one value on
// the unbuffered closeCh (pipe.go:577) and then waits on doneCh; only
// listenerRoutine ever closes doneCh, and only when it leaves its loop. For
// listenerRoutine to be back at 462 with closed == false, the send must have
// been consumed somewhere OTHER than the top select — and the only other
// receiver in the package is makeConnectedServerPipe's abort case at
// pipe.go:447. Having consumed it, that function returned an error which
// pipe.go:452 did not map to ErrPipeListenerClosed, so `closed = err ==
// ErrPipeListenerClosed` at pipe.go:479 was false and the loop went back
// around — with no value left for it to receive. doneCh is never closed and
// Close blocks forever.
//
// # Which errno escapes pipe.go:452 — read this before bumping go-winio
//
// The classification at 452 promotes only nil and ErrFileClosed. It is
// tempting to conclude the escapee is ERROR_OPERATION_ABORTED, since that is
// what a cancelled ConnectNamedPipe reports. That is WRONG for v0.6.2 and the
// mistake is worth recording: file.go:194-197 already remaps
// ERROR_OPERATION_ABORTED to ErrFileClosed whenever f.closing is set, and
// closeHandle (file.go:117) sets closing BEFORE cancelIoEx and before
// f.wg.Wait(). connectPipe holds that waitgroup (pipe.go:541-545), so
// closeHandle cannot return until asyncIO does, and asyncIO reads closing
// (file.go:195) after the cancellation it caused. closing is therefore
// necessarily true and the remap always fires. That path cannot wedge.
//
// The reachable variant is a race, not an errno-classification gap:
// connectNamedPipe can fail IMMEDIATELY with an errno that is neither
// ERROR_IO_PENDING nor ERROR_PIPE_CONNECTED, in which case asyncIO returns it
// raw (file.go:175-177) and connectPipe passes it through unmapped
// (pipe.go:549-551). If Close has sent by then, the select at pipe.go:441 has
// BOTH cases ready; Go picks uniformly at random, the closeCh branch can win,
// and `err = <-ch` at 451 picks up that raw errno — which escapes 452 verbatim.
// The ERROR_NO_DATA retry loop at pipe.go:470-477 is a second way in: it
// re-enters makeConnectedServerPipe after the single closeCh value is already
// spent, parking at 441 with nothing left to wake it.
//
// This remedy is deliberately errno-agnostic and covers every variant,
// including ones a future go-winio may introduce. Treat the specific mechanism
// above as the best-supported reading of the dump — the dump proves SOME error
// escaped 452, not which one. No Windows machine was available to observe it
// directly.
//
// # Why this is a product bug and not a test artefact
//
// internal/daemon/server.go closes this listener on the graceful-shutdown
// path. A wedge there means the Windows daemon never exits, which is exactly
// the user-visible symptom of issue #6044.
//
// # The remedy
//
// The stuck listenerRoutine is not dead; it is parked at 462 with a live
// `case <-l.closeCh`. One more send is therefore precisely the wakeup it
// needs: it sets closed = true, exits the loop, closes the real handle and
// closes doneCh — which releases the original Close call too. The result is a
// genuine, confirmed close, not an abandoned handle.
//
// closeCh is unbuffered, which makes the retry safe rather than merely likely
// to work: a retry issued while listenerRoutine is busy is not lost, it simply
// blocks as a pending sender until the routine returns to 462 and receives it.
//
// Concurrent Close calls are safe on win32PipeListener: Close only sends on
// closeCh and reads doneCh (pipe.go:575-582), and it is listenerRoutine — not
// Close — that closes the handle and doneCh, exactly once.

// closeAttemptBudget is how long a single Close is given before another is
// issued.
//
// A healthy Close is a channel rendezvous plus a handle close: microseconds.
// The slowest legitimate case waits for a cancelled kernel I/O to complete —
// sub-millisecond normally, tens of milliseconds on a loaded CI runner. 250 ms
// sits two orders of magnitude above that, so a close that is merely slow but
// still progressing is never interrupted by a spurious retry, while a
// genuinely wedged one is detected fast enough that shutdown still feels
// immediate.
const closeAttemptBudget = 250 * time.Millisecond

// closeMaxAttempts bounds the total wait at closeAttemptBudget * attempts.
//
// Two attempts suffice for the listenerRoutine-parked-at-462 shape: the
// listener is waiting on closeCh and a single extra send releases it. The
// headroom is for the ERROR_NO_DATA variant (pipe.go:470-477), where the retry
// loop can re-enter makeConnectedServerPipe and consume another closeCh value
// per iteration, so more than one extra send may be needed. Four caps the
// worst case at 1 s — small against any shutdown watchdog or test timeout, and
// vastly better than blocking forever.
const closeMaxAttempts = 4

// errCloseNotConfirmed reports that the listener did not confirm its close
// within the budget. The caller has stopped waiting.
//
// This is deliberately worse-but-finite. Two costs are accepted, and callers
// must understand both:
//
//   - The underlying handle may still be open, and the listener may still
//     accept. Callers must NOT treat this call as their bound on shutdown;
//     internal/daemon/server.go relies on its watchdog for that, precisely
//     because a listener that did not close can still wedge the accept loop.
//   - Up to closeMaxAttempts goroutines are leaked, one per attempt, each
//     blocked inside the wedged Close. They are unrecoverable by construction:
//     the whole point is that the call they are in does not return. The `done`
//     channel is buffered to closeMaxAttempts so that if one of them DOES
//     later return it can send and exit rather than leak again on an unread
//     channel.
//
// It is returned rather than swallowed so a recurrence is visible in logs
// instead of silent.
var errCloseNotConfirmed = errors.New("listener close not confirmed within budget")

// closeBounded closes a listener with the retry policy described above.
func closeBounded(closeFn func() error) error {
	return closeBoundedFor(closeFn, closeAttemptBudget, closeMaxAttempts)
}

// closeAttemptResult tags a close result with the attempt that produced it, so
// a retry's error can never be mistaken for the real close's outcome.
type closeAttemptResult struct {
	attempt int
	err     error
}

// closeBoundedFor is closeBounded with the budget injected, so tests can pin
// the mechanism without paying wall-clock for it.
//
// Attribution matters here. The retries are this function's own doing, so a
// retry that fails says nothing about whether the listener closed — a wrapped
// listener whose second Close reports "use of closed network connection" has
// in fact closed successfully. Returning that would turn a clean shutdown into
// a spurious warning at the server.go call site. So:
//
//   - attempt 0 is authoritative: its result is returned verbatim, error or not
//   - any attempt returning nil confirms the close
//   - a LATER attempt's error is discarded and we keep waiting; it carries no
//     information about attempt 0, which will land promptly if the listener
//     really did close
func closeBoundedFor(closeFn func() error, perAttempt time.Duration, maxAttempts int) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	// Buffered so a late-returning attempt can always send and exit rather
	// than leaking again on an unread channel.
	done := make(chan closeAttemptResult, maxAttempts)

	timer := time.NewTimer(perAttempt)
	defer timer.Stop()

	// resolve applies the attribution rules above. ok is false when the result
	// carries no information and we should keep waiting.
	resolve := func(r closeAttemptResult) (error, bool) {
		if r.attempt == 0 || r.err == nil {
			return r.err, true
		}
		return nil, false
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// The previous iteration drained timer.C via its <-timer.C case,
			// which is the only way to reach here, so Reset is safe.
			timer.Reset(perAttempt)
		}
		n := attempt
		go func() { done <- closeAttemptResult{attempt: n, err: closeFn()} }()

		expired := false
		for !expired {
			select {
			case r := <-done:
				if err, ok := resolve(r); ok {
					return err
				}
				// A retry failed; it tells us nothing. Keep waiting out this
				// attempt's budget.
			case <-timer.C:
				expired = true
			}
		}
	}

	// A close may have landed in the same instant the last budget expired.
	for {
		select {
		case r := <-done:
			if err, ok := resolve(r); ok {
				return err
			}
			continue
		default:
		}
		break
	}
	return errCloseNotConfirmed
}
