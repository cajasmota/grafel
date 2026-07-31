package transport

// closebounded_test.go pins the mechanism behind the Windows CI hang that
// blocked the v0.2.0 tag (run 30642094648): the whole windows-latest job died
// at `-timeout 15m` with
//
//	panic: test timed out after 15m0s
//	  running tests:
//	    TestRunDaemonStop_RPCPath_ReportsFailureWhenStillRunning (14m10s)
//
// and the goroutine dump showed the test goroutine parked forever in
// t.Cleanup at go-winio pipe.go:578 — `<-l.doneCh` inside win32PipeListener.
// Close — while the listener's own listenerRoutine sat at pipe.go:462, the
// top-of-loop select, with closed == false. In that state nothing will ever
// close doneCh, so Close never returns.
//
// The tests below reproduce that state machine faithfully (line references are
// to github.com/Microsoft/go-winio@v0.6.2/pipe.go) and pin both directions:
// the model DOES deadlock when the aborted connect is misclassified, and it
// does NOT when it is classified correctly — so the fixture is not merely
// always-stuck. closeBounded is then shown to convert the deadlock into a
// genuine, confirmed close rather than a give-up.
//
// NOTE: this runs on every platform because it models the library rather than
// calling it. The real Windows behaviour could not be executed here.

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Sentinels standing in for go-winio's exported errors and for the Windows
// errno that the library fails to recognise.
var (
	errPipeListenerClosedModel = errors.New("use of closed network connection")
	errFileClosedModel         = errors.New("file has already been closed")
	// errOperationAbortedModel stands in for windows.ERROR_OPERATION_ABORTED,
	// the errno a pending ConnectNamedPipe reports once its handle is closed
	// out from under it. pipe.go:452 normalises only nil and ErrFileClosed to
	// ErrPipeListenerClosed, so this value escapes as an ordinary accept
	// error — which is the whole bug.
	errOperationAbortedModel = errors.New("the I/O operation has been aborted")
)

// winioListenerModel reproduces win32PipeListener's concurrency structure:
// the unbuffered closeCh/acceptCh rendezvous, the doneCh broadcast, the
// listenerRoutine loop (pipe.go:459-483), makeConnectedServerPipe's abort
// branch (pipe.go:441-455) and Close (pipe.go:575-582).
type winioListenerModel struct {
	closeCh  chan int
	doneCh   chan int
	acceptCh chan chan error

	// parked is signalled every time makeConnectedServerPipe enters its
	// select, i.e. the listener is genuinely waiting for a client. It exists
	// only so the tests have a deterministic sync point instead of a sleep.
	parked chan struct{}

	// abortErr is what the aborted async connect reports back at pipe.go:451.
	abortErr error

	closes atomic.Int32
}

func newWinioListenerModel(abortErr error) *winioListenerModel {
	m := &winioListenerModel{
		closeCh:  make(chan int),
		doneCh:   make(chan int),
		acceptCh: make(chan chan error),
		parked:   make(chan struct{}, 8),
		abortErr: abortErr,
	}
	go m.listenerRoutine()
	return m
}

// listenerRoutine mirrors pipe.go:459-483.
func (m *winioListenerModel) listenerRoutine() {
	closed := false
	for !closed {
		select {
		case <-m.closeCh:
			closed = true
		case respCh := <-m.acceptCh:
			err := m.makeConnectedServerPipe()
			respCh <- err
			// pipe.go:479 — the loop only terminates when the error is
			// recognised as ErrPipeListenerClosed.
			closed = errors.Is(err, errPipeListenerClosedModel)
		}
	}
	close(m.doneCh)
}

// makeConnectedServerPipe mirrors pipe.go:429-457. No client ever arrives in
// these tests, so the only reachable branch is the closeCh abort.
func (m *winioListenerModel) makeConnectedServerPipe() error {
	connectDone := make(chan error, 1) // the real one is fed by connectPipe

	m.parked <- struct{}{}

	select {
	case err := <-connectDone:
		return err
	case <-m.closeCh:
		// pipe.go:447-454. The handle is closed, the pending connect
		// completes with abortErr, and ONLY nil / ErrFileClosed are mapped to
		// ErrPipeListenerClosed.
		err := m.abortErr
		if err == nil || errors.Is(err, errFileClosedModel) {
			err = errPipeListenerClosedModel
		}
		return err
	}
}

// accept mirrors pipe.go:556-573.
func (m *winioListenerModel) accept() error {
	ch := make(chan error)
	select {
	case m.acceptCh <- ch:
		return <-ch
	case <-m.doneCh:
		return errPipeListenerClosedModel
	}
}

// Close mirrors pipe.go:575-582.
func (m *winioListenerModel) Close() error {
	m.closes.Add(1)
	select {
	case m.closeCh <- 1:
		<-m.doneCh
	case <-m.doneCh:
	}
	return nil
}

func (m *winioListenerModel) isDone() bool {
	select {
	case <-m.doneCh:
		return true
	default:
		return false
	}
}

// waitParked blocks until the listener is inside makeConnectedServerPipe's
// select, which is the precondition for the deadlock.
func (m *winioListenerModel) waitParked(t *testing.T) {
	t.Helper()
	select {
	case <-m.parked:
	case <-time.After(5 * time.Second):
		t.Fatal("listener never parked in makeConnectedServerPipe")
	}
}

// driveToPendingAccept puts the model in the exact state captured by the CI
// goroutine dump: an Accept outstanding, the listener parked waiting for a
// client that will never come.
func driveToPendingAccept(t *testing.T, m *winioListenerModel) {
	t.Helper()
	go func() { _ = m.accept() }()
	m.waitParked(t)
}

// --- fixture validity: the model must be able to produce BOTH outcomes ------

// TestWinioModel_MisclassifiedAbortDeadlocksASingleClose is the negative
// anchor. With ERROR_OPERATION_ABORTED escaping pipe.go:452, one Close hangs
// forever. If this ever starts passing quickly the model has stopped
// reproducing the bug and every other test in this file is vacuous.
func TestWinioModel_MisclassifiedAbortDeadlocksASingleClose(t *testing.T) {
	m := newWinioListenerModel(errOperationAbortedModel)
	driveToPendingAccept(t, m)

	returned := make(chan struct{})
	go func() {
		_ = m.Close()
		close(returned)
	}()

	select {
	case <-returned:
		t.Fatal("Close returned; the model no longer reproduces the go-winio deadlock, " +
			"so the recovery tests in this file prove nothing")
	case <-time.After(300 * time.Millisecond):
		// Still stuck, as observed in CI for 14m10s.
	}
	if m.isDone() {
		t.Fatal("doneCh closed while Close was still blocked — model is inconsistent")
	}
}

// TestWinioModel_CorrectlyClassifiedAbortClosesOnFirstTry is the positive
// anchor for the SAME model: when the abort is reported as nil (the case
// pipe.go:452 does handle), a single Close converges. This proves the fixture
// is not simply always-deadlocked, and isolates the misclassification as the
// sole cause.
func TestWinioModel_CorrectlyClassifiedAbortClosesOnFirstTry(t *testing.T) {
	m := newWinioListenerModel(nil)
	driveToPendingAccept(t, m)

	returned := make(chan struct{})
	go func() {
		_ = m.Close()
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return even though the abort was classified correctly")
	}
	if !m.isDone() {
		t.Fatal("Close returned but doneCh was never closed")
	}
	if got := m.closes.Load(); got != 1 {
		t.Fatalf("expected exactly 1 Close call, got %d", got)
	}
}

// --- the fix ---------------------------------------------------------------

// TestCloseBounded_RecoversTheMisclassifiedAbortDeadlock is the core claim:
// a second Close is not a workaround, it is the precise wakeup the stuck
// listenerRoutine needs. Parked at pipe.go:462 it still has a live
// `case <-l.closeCh`, so one more send sets closed = true and closes doneCh —
// releasing the original Close as well. The listener is genuinely released,
// not abandoned.
func TestCloseBounded_RecoversTheMisclassifiedAbortDeadlock(t *testing.T) {
	m := newWinioListenerModel(errOperationAbortedModel)
	driveToPendingAccept(t, m)

	start := time.Now()
	err := closeBoundedFor(m.Close, 50*time.Millisecond, 4)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("closeBounded: %v", err)
	}
	if !m.isDone() {
		t.Fatal("closeBounded returned but the listener was never actually closed — " +
			"that would be a give-up, not a fix")
	}
	if got := m.closes.Load(); got < 2 {
		t.Fatalf("expected the retry to be the thing that unblocked it (>=2 Close calls), got %d", got)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("recovery took %v, far above the configured budget", elapsed)
	}
}

// TestCloseBounded_NoRetryWhenCloseReturnsPromptly pins that the retry is
// conditional. A listener that closes normally must be closed exactly once —
// otherwise every shutdown would fire spurious closeCh sends.
func TestCloseBounded_NoRetryWhenCloseReturnsPromptly(t *testing.T) {
	var calls atomic.Int32
	err := closeBoundedFor(func() error {
		calls.Add(1)
		return nil
	}, 50*time.Millisecond, 4)
	if err != nil {
		t.Fatalf("closeBounded: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 Close call on the happy path, got %d", got)
	}
}

// TestCloseBounded_PropagatesCloseError pins that a real close error is not
// swallowed by the retry machinery.
func TestCloseBounded_PropagatesCloseError(t *testing.T) {
	sentinel := errors.New("boom")
	err := closeBoundedFor(func() error { return sentinel }, 50*time.Millisecond, 4)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the underlying close error, got %v", err)
	}
}

// TestCloseBounded_ReturnsWithinBudgetWhenCloseCanNeverSucceed pins the
// ceiling: against a close that no number of retries can rescue, the helper
// must still return — reporting honestly that the close was not confirmed —
// rather than reproducing the 15-minute hang in a slower form.
func TestCloseBounded_ReturnsWithinBudgetWhenCloseCanNeverSucceed(t *testing.T) {
	const perAttempt = 40 * time.Millisecond
	const attempts = 4

	start := time.Now()
	err := closeBoundedFor(func() error { select {} }, perAttempt, attempts)
	elapsed := time.Since(start)

	if !errors.Is(err, errCloseNotConfirmed) {
		t.Fatalf("expected errCloseNotConfirmed, got %v", err)
	}
	if elapsed < perAttempt*attempts {
		t.Fatalf("gave up after %v, before the budget was spent", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("took %v; the total budget is not being enforced", elapsed)
	}
}

// TestCloseBounded_ProductionConstantsCanRecover guards the two properties the
// production numbers must have: more than one attempt (a single attempt cannot
// recover the deadlock at all — see the model above), and a bounded total.
func TestCloseBounded_ProductionConstantsCanRecover(t *testing.T) {
	if closeMaxAttempts < 2 {
		t.Fatalf("closeMaxAttempts=%d: with fewer than 2 attempts the retry can never "+
			"deliver the second closeCh send that unblocks listenerRoutine", closeMaxAttempts)
	}
	if total := closeAttemptBudget * time.Duration(closeMaxAttempts); total > 2*time.Second {
		t.Fatalf("total close budget %v is too large for a shutdown path", total)
	}
}
