package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestRunWatcherRefreshCmd_BoundedEvenWithSurvivingGrandchild pins #6184 F1,
// found on review: exec.CommandContext SIGKILLs only the DIRECT child.
// CombinedOutput backs stdout/stderr with an os.Pipe, and Wait() blocks on
// that pipe until every WRITER closes it — including a grandchild that
// inherited the fd. install --refresh-state's own child is launchctl, which
// is exactly the process #6184 says can wedge: killing the direct child
// alone leaves a surviving launchctl grandchild holding the pipe open, so a
// context deadline with no WaitDelay does not bound wall-clock time at all.
//
// This exercises the REAL production function — runWatcherRefreshCmd — with
// a real child process, not a context-obeying stub, so it can actually fail
// against the pre-WaitDelay implementation (a prior version of this test
// substituted the whole function with a `select { case <-ctx.Done() }` stub,
// which honours the context by construction and could never observe this
// bug).
//
// The spawned shell backgrounds a grandchild that inherits stdout/stderr,
// then signals readiness (touches a file) before sleeping 20s itself. The
// grandchild also sleeps 20s, holding the pipe open after the direct child
// is killed. WaitDelay must bound the wait for that grandchild to a few
// seconds instead of the full 20s.
//
// #6184 R2 (found on round-2 review): the first cut of this test raced a
// fixed context deadline (300ms, later 1s) against how long /bin/sh takes to
// fork+exec the grandchild and print "partial". On a loaded machine that
// fork/exec cost is not bounded tightly enough for any fixed short deadline
// to be reliable — measured 6 of 8 mutant runs vacuous at 300ms, and even at
// 1s this machine still produced occasional vacuous runs (deadline fired
// before the child had backgrounded anything, so nothing was ever holding
// the pipe and the elapsed-time assertion proved nothing about WaitDelay).
//
// Fix: remove the race instead of tuning it. The script signals readiness
// (touches readyFile) only AFTER backgrounding its grandchild and printing
// "partial", so the test polls for that file — deterministically, not on a
// clock — before ever cancelling the context. Cancellation and the bounded-
// return assertion now measure only the thing #6184 F1 is about: how long
// CombinedOutput takes to return once a grandchild is confirmed to be
// holding the pipe open, not how fast a shell can fork.
func TestRunWatcherRefreshCmd_BoundedEvenWithSurvivingGrandchild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "wedged.sh")
	readyFile := filepath.Join(dir, "ready")
	body := "#!/bin/sh\n(sleep 20) &\necho partial\ntouch " + readyFile + "\nsleep 20\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := runWatcherRefreshCmd(ctx, script)
		done <- result{out, err}
	}()

	// Wait, deterministically (not on a clock), until the grandchild has
	// actually been backgrounded before cancelling — this is the fix for
	// R2's flake, not a longer timeout.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, statErr := os.Stat(readyFile); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never signalled readiness within 10s; test infrastructure issue, " +
				"not the thing under test")
		}
		time.Sleep(5 * time.Millisecond)
	}

	start := time.Now()
	cancel()
	var res result
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("runWatcherRefreshCmd did not return within 10s of cancellation " +
			"(#6184 F1: WaitDelay is not bounding the wait for the surviving grandchild)")
	}
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("runWatcherRefreshCmd did not return promptly after the surviving grandchild "+
			"was confirmed to be holding the output pipe open (#6184 F1): took %s, want well "+
			"under the 20s the grandchild sleeps\nerr=%v out=%q", elapsed, res.err, res.out)
	}
	if res.err == nil {
		t.Errorf("expected an error from the killed child, got nil (out=%q)", res.out)
	}
	if string(res.out) != "partial\n" {
		t.Fatalf("runWatcherRefreshCmd output = %q, want \"partial\\n\"", res.out)
	}
}
