//go:build !windows

package sched

// cancel_escaped_pipe_test.go — cancellation must return even when a surviving
// descriptor holder ESCAPED the killed process group.
//
// cancel_processgroup_5999_test.go pins the case where the grandchild is IN the
// child's process group, so the group kill reaches it. That fix is a claim
// about child behaviour, not a bound: a grandchild that leaves the group before
// it dies (setsid, setpgid, a job started under a shell in monitor mode, a tool
// that daemonises itself) still holds the inherited stdout/stderr pipes, and
// the runner's drain-to-EOF loop — which runs BEFORE cmd.Wait — never sees EOF.
// The runner then never returns, with the daemon's exclusive heavy write-stage
// token still held: one escaped descriptor stops every heavy stage in the
// daemon until it is restarted.
//
// os/exec's own remedy does not cover this. cmd.WaitDelay force-closes the
// parent's pipe ends only inside `if c.goroutineErr != nil` (os/exec watchCtx),
// i.e. only for a Cmd whose output os/exec is COPYING. These runners use
// StdoutPipe/StderrPipe, so there are no copying goroutines and that branch is
// unreachable — and the runner blocks before Wait anyway. Hence
// boundPostCancelDrain.
//
// PLATFORM COVERAGE — read this before trusting a green run.
//
// The fixture injects the escaped-descriptor condition directly rather than
// racing real processes, by two routes, because no single one is portable:
//
//   - setsid(1) where it exists (Linux/util-linux). A new session is a new
//     process group by definition.
//   - `set -m` (shell job control) otherwise. On macOS /bin/sh is bash, which
//     honours it and puts the backgrounded job in its own group.
//
// dash — /bin/sh on Ubuntu — refuses job control without a controlling tty, and
// `go test` under GitHub Actions has none. setsid is what carries Linux; if it
// is ever absent there, these tests SKIP rather than pass vacuously. The
// precondition is verified with getpgid before anything is asserted, so a skip
// is always honest. Windows is excluded outright by the build tag (and see
// subprocess_drainbound.go on whether the bound even binds there).
//
// So: this file is the only coverage boundPostCancelDrain has, and it does not
// cover every platform the bound ships to. That is a known gap, not an
// oversight.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// escapingChildScript is a stand-in child that leaks a pipe holder OUT of its
// own process group and then blocks.
//
// The holder records its OWN pid ($$ from inside the subshell) rather than the
// shell's $!: under setsid(1) those can differ (setsid forks when it is already
// a group leader), and a pid that names a process which has already exited
// would make the test track the wrong thing. `exec sleep` then keeps that same
// pid, holding the inherited stdout/stderr, for the life of the sleep.
func escapingChildScript(pidFile string) string {
	holder := "/bin/sh -c 'echo $$ > " + pidFile + "; exec sleep 60'"
	return "if command -v setsid >/dev/null 2>&1; then\n" +
		"  setsid " + holder + " &\n" +
		"else\n" +
		"  set -m\n" +
		"  " + holder + " &\n" +
		"  set +m\n" +
		"fi\n" +
		"sleep 60"
}

// withDrainGrace overrides the post-cancel drain bound for the duration of a
// test, so the deadlock contract can be checked in seconds instead of the
// shipped grace.
//
// The write races any runner goroutine still alive when the cleanup fires, so
// every test that uses this MUST cancel its context and wait for its runner to
// return before finishing — see exerciseEscapedHolder, which enforces that on
// every exit path including t.Skip and t.Fatal.
func withDrainGrace(t *testing.T, d time.Duration) {
	t.Helper()
	prev := postCancelDrainGrace
	postCancelDrainGrace = d
	t.Cleanup(func() { postCancelDrainGrace = prev })
}

// exerciseEscapedHolder runs one runner against a child that leaks a pipe
// holder out of its process group, and asserts the runner still returns.
//
// The exit discipline is the load-bearing part. Whatever happens — assertion
// failure, a shell that would not detach the holder, a child that never
// recorded a pid — this cancels the context and waits for the runner goroutine
// to return before it lets the test finish. A leaked runner goroutine is not a
// cosmetic problem here: it is blocked in the drain having already read
// postCancelDrainGrace and groupAlgoChildBinary, so `withDrainGrace`'s and
// `fakeGroupAlgoBinary`'s cleanups would race it and turn a skip into a
// -race FAILURE on the release gate.
func exerciseEscapedHolder(t *testing.T, grace time.Duration, install func(*testing.T, string), run func(context.Context) error) {
	t.Helper()
	withDrainGrace(t, grace)
	pidFile := filepath.Join(t.TempDir(), "escaped.pid")
	install(t, escapingChildScript(pidFile))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()

	returned := false
	defer func() {
		// Cancel is idempotent, and on the happy path the runner has already
		// returned; this is here for every OTHER path.
		cancel()
		if returned {
			return
		}
		select {
		case <-done:
		case <-time.After(grace + 30*time.Second):
			// Deliberately Errorf, not Fatalf: Fatal from a deferred call in a
			// skipped/failed test only muddies the report, and the goroutine is
			// leaked either way — say so and let the race detector speak.
			t.Errorf("runner goroutine never returned after cancel; it is leaked and will race this test's cleanups")
		}
	}()

	holder := trackHolder(t, pidFile)
	requireEscaped(t, holder)

	start := time.Now()
	cancel()

	select {
	case err := <-done:
		returned = true
		elapsed := time.Since(start)
		if err == nil {
			t.Fatal("cancelled run returned nil error, want a cancellation error")
		}
		if elapsed < grace {
			t.Fatalf("runner returned after %s, before the %s drain grace — the drain is being cut short, "+
				"not bounded; a dying child's final output would be dropped", elapsed, grace)
		}
	case <-time.After(grace + 20*time.Second):
		t.Fatalf("runner never returned: an escaped descriptor holder is holding the drain open past the %s bound\n%s",
			grace, describeHolders(holder))
	}
}

// requireEscaped asserts the precondition the whole test rests on: the holder
// really is outside the child's process group.
func requireEscaped(t *testing.T, h trackedHolder) {
	t.Helper()
	if h.pgid == 0 {
		t.Skipf("holder pid %d vanished before its process group could be read — "+
			"the escaped-descriptor condition cannot be verified here", h.pid)
	}
	// The stand-in child is itself a group leader (applyGroupAlgoNice sets
	// Setpgid), so a holder that stayed in the child's group carries the CHILD's
	// pid as its pgid. pgid == its own pid therefore means, and only means, that
	// it was given a group of its own: it escaped.
	if h.pgid != h.pid {
		t.Skipf("this shell detached no holder (pid %d has pgid %d): no setsid(1), and job control "+
			"unavailable — the escaped-descriptor condition cannot be injected here", h.pid, h.pgid)
	}
}

func TestRunSubprocessGroupAlgo_CancelReturnsWhenAHolderEscapedTheGroup(t *testing.T) {
	exerciseEscapedHolder(t, 2*time.Second, fakeGroupAlgoBinary, func(ctx context.Context) error {
		return RunSubprocessGroupAlgo(ctx, "g", nil)
	})
}

func TestRunSubprocessLinks_CancelReturnsWhenAHolderEscapedTheGroup(t *testing.T) {
	exerciseEscapedHolder(t, 2*time.Second, fakeChildScript, func(ctx context.Context) error {
		return RunSubprocessLinks(ctx, "g", nil)
	})
}

// TestBoundPostCancelDrain_LeavesAnUncancelledRunAlone: the bound must be inert
// when nothing is cancelled. Without this, "close the pipes after a while"
// could pass the tests above while silently truncating every healthy pass's
// output.
func TestBoundPostCancelDrain_LeavesAnUncancelledRunAlone(t *testing.T) {
	withDrainGrace(t, 50*time.Millisecond)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	stop := boundPostCancelDrain(context.Background(), r)
	defer stop()
	time.Sleep(300 * time.Millisecond) // 6x the grace

	if _, err := w.WriteString("still open\n"); err != nil {
		t.Fatalf("write end broke on an uncancelled run: %v", err)
	}
	buf := make([]byte, 32)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("read end was closed on an uncancelled run: %v", err)
	}
	if got := strings.TrimSpace(string(buf[:n])); got != "still open" {
		t.Fatalf("read %q, want %q", got, "still open")
	}
}

// TestBoundPostCancelDrain_StopDisarmsTheWatchdog: after the drain goroutines
// have returned, the runner calls stop() and then cmd.Wait(). If stop did not
// disarm the watchdog, a close would land later, on pipes os/exec is by then
// managing itself.
//
// The grace must stay SHORT relative to the observation window below, or this
// test cannot see the bug: with a long grace, a watchdog that ignored stop()
// would fire after the assertions had already run and passed. 50ms grace, 300ms
// window — stop() lands first, and a watchdog that disregarded it is observed.
func TestBoundPostCancelDrain_StopDisarmsTheWatchdog(t *testing.T) {
	withDrainGrace(t, 50*time.Millisecond)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := boundPostCancelDrain(ctx, r)
	cancel()
	stop() // drain finished before the grace elapsed — the normal cancelled path
	time.Sleep(300 * time.Millisecond)

	if _, err := w.WriteString("x\n"); err != nil {
		t.Fatalf("write end broke after stop(): %v", err)
	}
	if _, err := r.Read(make([]byte, 8)); err != nil {
		t.Fatalf("read end was closed after stop(): %v", err)
	}
}

// TestPostCancelDrainGrace_IsABoundNotAnEternity guards the value itself. It is
// a coarse sanity range, not a measurement — the grace is a judgement call (see
// its doc comment), and this only pins that it stays a bound: positive, so the
// drain is not cut short on every cancel, and short enough that the heavy
// write-stage token is not held for an operator-visible age.
func TestPostCancelDrainGrace_IsABoundNotAnEternity(t *testing.T) {
	if postCancelDrainGrace <= 0 {
		t.Fatalf("postCancelDrainGrace = %s: a non-positive grace cuts the drain short on every cancel", postCancelDrainGrace)
	}
	if postCancelDrainGrace > 30*time.Second {
		t.Fatalf("postCancelDrainGrace = %s: too long to be a deadlock bound — the heavy write-stage "+
			"token is held for the whole of it", postCancelDrainGrace)
	}
}
