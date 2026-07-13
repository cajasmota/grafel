package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestParentDeathWatchdog_FiresOnReparent verifies that when the observed
// parent pid diverges from the pid recorded at startup (the engine has been
// reparented — its original serve parent died uncleanly: SIGKILL, crash,
// OOM), the watchdog invokes onParentDeath exactly once (ADR-0024 orphan-
// engine hardening, epic #5729).
func TestParentDeathWatchdog_FiresOnReparent(t *testing.T) {
	const originalParent = 4242
	var current int32 = originalParent

	var fired int32
	fireCh := make(chan struct{})
	onParentDeath := func() {
		if atomic.AddInt32(&fired, 1) == 1 {
			close(fireCh)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := startParentDeathWatchdog(ctx, originalParent, func() int {
		return int(atomic.LoadInt32(&current))
	}, time.Millisecond, onParentDeath, nil)

	// Simulate reparenting: the original parent is gone, init (or a
	// substitute) has adopted this process.
	atomic.StoreInt32(&current, 1)

	select {
	case <-fireCh:
	case <-time.After(2 * time.Second):
		t.Fatal("onParentDeath was not called after the observed parent pid diverged")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog goroutine did not exit after firing")
	}

	if got := atomic.LoadInt32(&fired); got != 1 {
		t.Errorf("onParentDeath called %d times, want exactly 1", got)
	}
}

// TestParentDeathWatchdog_NoFireWhileParentUnchanged verifies the watchdog
// never fires while the observed parent pid keeps matching the original —
// the common case (serve is alive and well) must never trigger a spurious
// self-termination.
func TestParentDeathWatchdog_NoFireWhileParentUnchanged(t *testing.T) {
	const originalParent = 777

	var fired int32
	onParentDeath := func() { atomic.AddInt32(&fired, 1) }

	ctx, cancel := context.WithCancel(context.Background())

	done := startParentDeathWatchdog(ctx, originalParent, func() int {
		return originalParent
	}, time.Millisecond, onParentDeath, nil)

	// Let several poll intervals elapse with the parent unchanged.
	time.Sleep(50 * time.Millisecond)

	if got := atomic.LoadInt32(&fired); got != 0 {
		t.Fatalf("onParentDeath called %d times while parent was unchanged, want 0", got)
	}

	// Normal shutdown: cancel ctx — the goroutine must exit cleanly (no
	// leak) WITHOUT ever having called onParentDeath.
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog goroutine did not exit after ctx cancellation")
	}

	if got := atomic.LoadInt32(&fired); got != 0 {
		t.Errorf("onParentDeath called %d times after ctx cancel, want 0 (must not fire on normal shutdown)", got)
	}
}

// TestParentDeathWatchdog_ExitsOnCtxCancel_NoLeak is a focused leak check:
// the watchdog goroutine must terminate promptly when ctx is cancelled, even
// if the parent pid never diverges — this is the "normal engine shutdown"
// path and must never leave a goroutine running.
func TestParentDeathWatchdog_ExitsOnCtxCancel_NoLeak(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	done := startParentDeathWatchdog(ctx, 1234, func() int { return 1234 }, time.Millisecond, func() {}, nil)

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog goroutine leaked past ctx cancellation")
	}
}
