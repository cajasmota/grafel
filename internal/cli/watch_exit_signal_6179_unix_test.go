//go:build !windows

// This test is excluded from Windows at BUILD time rather than skipped at run
// time, and the exclusion is about the assertion, not about a missing API.
//
// The property under test is unix signal semantics: a SIGTERM delivered to a
// running `grafel watch` must be HANDLED (the handler returns normally and the
// process exits 0) rather than taking the default action. That distinction is
// what launchd/systemd read — death BY an unhandled signal counts as an
// unsuccessful exit and re-triggers KeepAlive/Restart, which is the exact
// reap-respawn oscillation #6179 fixed.
//
// Windows has no mechanism to express that. syscall.Kill does not exist there,
// and the portable spellings do not preserve the intent either: os.Process.Signal
// on Windows accepts only os.Kill, which terminates the process outright and can
// never be observed by a handler. Rewriting the test to use it would assert the
// opposite of what #6179 requires. There is also nothing on Windows for the
// assertion to protect — the respawn contract belongs to launchd and systemd;
// the Windows watcher backend is schtasks, which has no equivalent semantics.
//
// The other four tests in watch_exit_6179_test.go stay untagged and keep running
// on all three platforms.

package cli

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestRunWatch_SignalExitsSuccessfully covers the SIGTERM path end-to-end.
//
// This one matters specifically for the reaper interaction noted in #6179: the
// daemon's sweep SIGTERMs foreign/duplicate `grafel watch` processes, and under
// the old unconditional KeepAlive launchd relaunched them immediately — a
// reap↔respawn oscillation. Exiting 0 on signal is what makes the reap stick.
// (launchd treats death BY an unhandled signal as an unsuccessful exit, so the
// handler returning normally, rather than the process being signal-killed, is
// load-bearing.)
func TestRunWatch_SignalExitsSuccessfully(t *testing.T) {
	home := withSandboxHome(t)
	repo := filepath.Join(home, "repos", "signalled")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	// Register a test-owned handler FIRST so the SIGTERM below can never take
	// the default action and kill the test binary, whatever runWatch is doing.
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	t.Cleanup(func() { signal.Stop(guard) })

	prevTrigger := indexTriggerFunc
	indexTriggerFunc = func(string) error { return nil }
	t.Cleanup(func() { indexTriggerFunc = prevTrigger })

	done := make(chan error, 1)
	go func() { done <- runWatch(repo, "", time.Hour) }()

	// Let runWatch reach its own signal.Notify before signalling.
	time.Sleep(250 * time.Millisecond)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("raise SIGTERM: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runWatch returned %v on SIGTERM; a stop request is deliberate and must "+
				"exit 0 so launchd does not respawn what the reaper just killed (#6179)", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("runWatch did not exit on SIGTERM")
	}
}
