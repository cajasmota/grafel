package sched

// subprocess_drainbound.go — bound the post-cancel pipe drain so a cancelled
// subprocess runner ALWAYS returns.
//
// Every subprocess runner in this package has the same shape:
//
//	go drain(stdoutPipe); go drain(stderrPipe)
//	<-stdoutDone; <-stderrDone      // drain to EOF
//	waitErr := cmd.Wait()
//
// The drain finishes only at EOF, and EOF arrives only when the LAST write end
// of that pipe is closed — that is, when every process holding the inherited
// descriptor is gone. #5999 made cancellation SIGKILL the child's whole process
// GROUP so the ordinary fan-out (the index child's `grafel extract` batches)
// is covered. But a process-group kill is a claim about child BEHAVIOUR, not a
// bound: anything that leaves the group before dying — a helper that calls
// setsid/setpgid, a job started under a shell in monitor mode, a tool that
// daemonises itself — still holds the pipe, the drain never sees EOF, and the
// runner never returns. It is holding the daemon's EXCLUSIVE heavy write-stage
// token while it blocks, so a single escaped descriptor stops every heavy stage
// in the daemon until it is restarted.
//
// cmd.WaitDelay does NOT close this hole, which is why it is not the fix here.
// os/exec force-closes the parent's pipe ends on WaitDelay expiry only inside
// `if c.goroutineErr != nil` (os/exec/exec.go, watchCtx) — i.e. only for a Cmd
// whose Stdout/Stderr are being COPIED by os/exec's own goroutines. These
// runners use StdoutPipe/StderrPipe, so there are no copying goroutines, that
// branch is dead, and nothing in os/exec ever unblocks the read ends.
// Independently, the runners block BEFORE cmd.Wait, so no Wait-side delay could
// run at all. The read ends must be closed by us.
//
// So: once ctx is cancelled, give the drain a grace period to finish honestly
// (a killed child usually EOFs in milliseconds, and we want its final lines in
// the log), then force-close the read ends. On Unix, closing an *os.File that a
// goroutine is blocked reading evicts the pending Read with ErrFileClosing — so
// the scanner returns, the drain goroutine exits, and the runner proceeds to
// cmd.Wait and returns a cancellation error. That is the platform this was
// written for, and the only one its tests run on.
//
// WINDOWS — UNVERIFIED, and possibly inert. Go creates the anonymous pipes
// behind StdoutPipe without FILE_FLAG_OVERLAPPED there, so they are not
// registered with the runtime poller and a Close may not evict a blocked
// ReadFile; the drain could stay stuck until the pipe's last writer exits
// anyway. Windows is also the platform MOST exposed to this failure, because it
// gets no process-group kill at all (nice_windows.go's hook is a no-op), so a
// holder that outlives the child is not even the exotic case there. Nothing in
// this file is claimed to work on Windows: it is compiled, it is harmless, and
// it is untested. Fixing Windows properly means giving those runners a real job
// object, which is a separate change.

import (
	"context"
	"io"
	"sync"
	"time"
)

// postCancelDrainGrace is how long a runner keeps draining a cancelled child's
// stdout/stderr AFTER the child's process group has been signalled, before it
// force-closes the read ends and returns.
//
// This is a DEADLOCK bound, not a latency knob. Lines the child had already
// written are still drained; at worst the tail of a dying child's output is
// dropped from the log, which is strictly better than wedging the write stage.
//
// 5s IS A JUDGEMENT CALL, not a measurement, and it is stated as one rather
// than dressed up: nothing here was profiled. The reasoning is only that the
// normal case (every holder killed) EOFs in milliseconds, so any value well
// above that costs nothing on the healthy path; that the runs which DO reach
// the deadline are the ones that would otherwise block forever, so the cost of
// being slightly generous is bounded and one-sided; and that the daemon's
// exclusive heavy write-stage token is held for the whole grace, which is what
// stops it being minutes. If a child is ever seen losing meaningful output to
// this, raise it — the tests override the value rather than assume it.
var postCancelDrainGrace = 5 * time.Second

// boundPostCancelDrain force-closes pipes if ctx is cancelled and the drain has
// not finished within postCancelDrainGrace. The returned stop function ends the
// watchdog and MUST be called once the drain goroutines have returned (defer is
// fine).
//
// stop is not a hard barrier, and does not need to be: if it lands exactly as
// the grace timer fires, both select arms are ready and Go may still take the
// close. That is harmless — Close is idempotent and cmd.Wait closes these pipes
// itself immediately afterwards — but it is a race stop cannot win, so no
// caller may depend on "no Close after stop". What IS guaranteed is the part
// that matters: without cancellation the watchdog never reaches the timer at
// all, so an uncancelled run is completely unaffected.
func boundPostCancelDrain(ctx context.Context, pipes ...io.Closer) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	// Read the grace HERE, in the caller's goroutine, not inside the watchdog.
	// The var is overridable by tests, and a watchdog-side read would race every
	// later test that re-points it (the goroutine can outlive the call).
	grace := postCancelDrainGrace
	go func() {
		select {
		case <-done:
			return
		case <-ctx.Done():
		}
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-done:
			return
		case <-timer.C:
		}
		for _, p := range pipes {
			if p != nil {
				_ = p.Close()
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}
