package indexstate

import (
	"testing"
	"time"
)

// parse_foreground_5970_test.go — the FOREGROUND exemption on the in-process
// parse gate (#5970).
//
// The daemon installs a BACKGROUND cap at startup (25% of the box). But the
// same process also runs user-awaited in-process indexing: the synchronous
// `grafel index` RPC, and the rebuild path when the subprocess indexer is
// opted out. Those must not be throttled — the standing policy caps background
// work only. Since the gate is a single process-wide semaphore, the exemption
// has to be a scoped, refcounted lift rather than a second cap.

// TestBeginForegroundParse_LiftsTheCapWhileHeld pins the core contract: while a
// foreground hold is live the gate is unbounded, and the configured background
// cap is restored — not lost — when the hold releases.
func TestBeginForegroundParse_LiftsTheCapWhileHeld(t *testing.T) {
	t.Cleanup(func() { SetParseConcurrency(0) })
	SetParseConcurrency(3)

	if got := EffectiveParseConcurrencyCap(); got != 3 {
		t.Fatalf("effective cap before hold = %d, want 3", got)
	}
	release := BeginForegroundParse()
	if got := EffectiveParseConcurrencyCap(); got != 0 {
		t.Fatalf("effective cap during foreground hold = %d, want 0 (unbounded)", got)
	}
	// The CONFIGURED cap is untouched: ensureParseConcurrencyDefault and the
	// status plane both read it and must still see the daemon's real ceiling.
	if got := ParseConcurrencyCap(); got != 3 {
		t.Fatalf("configured cap during hold = %d, want 3 (unchanged)", got)
	}
	release()
	if got := EffectiveParseConcurrencyCap(); got != 3 {
		t.Fatalf("effective cap after release = %d, want 3 (background cap restored)", got)
	}
}

// TestBeginForegroundParse_Refcounted proves nested/concurrent foreground units
// of work do not un-lift each other: the cap returns only when the LAST hold
// releases, and a double release is inert.
func TestBeginForegroundParse_Refcounted(t *testing.T) {
	t.Cleanup(func() { SetParseConcurrency(0) })
	SetParseConcurrency(2)

	r1 := BeginForegroundParse()
	r2 := BeginForegroundParse()
	r1()
	if got := EffectiveParseConcurrencyCap(); got != 0 {
		t.Fatalf("effective cap with one hold still live = %d, want 0", got)
	}
	r1() // idempotent: must not drop r2's hold
	if got := EffectiveParseConcurrencyCap(); got != 0 {
		t.Fatalf("effective cap after double-release of r1 = %d, want 0", got)
	}
	r2()
	if got := EffectiveParseConcurrencyCap(); got != 2 {
		t.Fatalf("effective cap after last release = %d, want 2", got)
	}
}

// TestBeginForegroundParse_UnblocksQueuedWaiters is the behavioural half: a
// parse queued behind a full background gate must proceed the moment foreground
// work lifts the cap, not sit blocked until the in-flight parse finishes.
func TestBeginForegroundParse_UnblocksQueuedWaiters(t *testing.T) {
	t.Cleanup(func() { SetParseConcurrency(0) })
	SetParseConcurrency(1)

	AcquireParseSlot() // occupy the single background slot
	defer ReleaseParseSlot()

	got := make(chan struct{})
	go func() {
		AcquireParseSlot()
		close(got)
		ReleaseParseSlot()
	}()

	select {
	case <-got:
		t.Fatal("second parse acquired a slot while the cap-1 gate was full")
	case <-time.After(50 * time.Millisecond):
	}

	release := BeginForegroundParse()
	defer release()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("foreground lift did not wake the queued parse")
	}
}
