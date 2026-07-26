package main

// links_subprocess_5954_test.go — the scheduler-driven link pass runs in a
// child process (#5954).
//
// SCOPE. This is the BACKGROUND, scheduler-driven pass only. The foreground
// rebuild's link step (daemonForegroundLinks, called synchronously inside
// daemonRebuildFuncCore) is user-initiated and latency-sensitive and stays
// in-process; the second test here is the guard that keeps it that way.

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
)

func TestDaemonSchedulerLinks_ForksTheChild(t *testing.T) {
	var gotGroup string
	var gotCtx context.Context
	release := make(chan struct{})
	entered := make(chan struct{})

	prev := subprocessLinksRunner
	subprocessLinksRunner = func(ctx context.Context, group string, _ *slog.Logger) error {
		gotCtx, gotGroup = ctx, group
		close(entered)
		<-release
		return nil
	}
	t.Cleanup(func() { subprocessLinksRunner = prev })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- daemonSchedulerLinks(ctx, "acme") }()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("daemonSchedulerLinks never reached the subprocess runner")
	}

	// The scheduler holds the EXCLUSIVE heavy-stage token across this callback
	// (fireLinks acquires it, a defer releases it), so the callback must not
	// return while the child is still running — otherwise the gate stops
	// binding for exactly the stage it was built to serialise.
	select {
	case <-done:
		t.Fatal("daemonSchedulerLinks returned while the child was still running — the stage token would be released early")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("daemonSchedulerLinks: %v", err)
	}
	if gotGroup != "acme" {
		t.Errorf("group forwarded to the child = %q, want %q", gotGroup, "acme")
	}
	if gotCtx == nil {
		t.Fatal("no context forwarded to the child runner")
	}
	// Cancellation must survive the fork: the scheduler's per-group cancel ctx
	// is what CancelGroup trips mid-pass, and the runner turns it into SIGTERM.
	if gotCtx.Err() != nil {
		t.Fatalf("forwarded ctx already cancelled: %v", gotCtx.Err())
	}
	cancel()
	if !errors.Is(gotCtx.Err(), context.Canceled) {
		t.Errorf("cancelling the scheduler ctx did not reach the child runner's ctx (err=%v)", gotCtx.Err())
	}
}

// TestDaemonForegroundLinks_DoesNotFork keeps the foreground rebuild's link
// step in-process. It is user-initiated, latency-sensitive, and already covered
// by the stage gate's barge; routing it through a fork would add process
// startup to a path a human is waiting on.
func TestDaemonForegroundLinks_DoesNotFork(t *testing.T) {
	t.Setenv(daemon.EnvRoot, t.TempDir())

	forked := false
	prev := subprocessLinksRunner
	subprocessLinksRunner = func(context.Context, string, *slog.Logger) error {
		forked = true
		return nil
	}
	t.Cleanup(func() { subprocessLinksRunner = prev })

	// The group does not exist, so the in-process pass errors out early. The
	// error is irrelevant — what matters is which path it took.
	_ = daemonForegroundLinks(context.Background(), "no-such-group-5954")
	if forked {
		t.Fatal("the foreground rebuild's link step forked a child — it must stay in-process")
	}
}

// TestLinksInternalIsOnTheGCPacingAllowList — the child is a background batch
// process with nobody waiting on it, so it gets the same GOGC=50 pacing the
// other two children have. group-algo needed a follow-up PR for its half of
// this; the link child gets it in the same commit that creates it.
func TestLinksInternalIsOnTheGCPacingAllowList(t *testing.T) {
	if !isBackgroundGCPercentCommand(backgroundLinksCommand) {
		t.Fatalf("%q is not on the background GC-pacing allow-list", backgroundLinksCommand)
	}
	pct, source := indexGCPercentDecision(backgroundLinksCommand, "", "")
	if pct != indexGCPercentDefault {
		t.Fatalf("GC percent for %q = %d (%s), want the background default %d",
			backgroundLinksCommand, pct, source, indexGCPercentDefault)
	}
}
