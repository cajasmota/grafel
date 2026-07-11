package sched

// Issue #5726 / epic #5729 — reindex circuit breaker.
//
// A repo genuinely over the FlatBuffers 2-GiB builder cap fails the SAME way
// on every attempt at the SAME target commit: fbwriter's fail-soft path
// (internal/graph/fbwriter/streaming.go) recovers the marshal panic and
// leaves last-good graph.fb intact, but the scheduler's trigger conditions
// (watcher fs events, git-HEAD poll) are input-driven and unaware of the
// failure — any further churn at the same commit re-fires a doomed reindex
// immediately. The original #5726 report showed the panic logged 74x in
// daemon.err, evidence of exactly this hot loop.
//
// This test proves the breaker: N rapid same-ref re-triggers after a failure
// collapse into 1 real attempt (the rest are skipped, serving last-good,
// during the backoff window), and a target-commit CHANGE resets the guard so
// the new commit always gets a real attempt.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestReindexCircuitBreaker_SameRefSkipsRepeatedFailures(t *testing.T) {
	var calls atomic.Int32
	s := New(Config{
		Workers: 1,
		Index: func(_ context.Context, _ string, _ string) error {
			calls.Add(1)
			return errBoom5726
		},
	})
	s.Start()
	defer s.Stop()

	// First trigger at commit "sha1": a real attempt, which fails and arms
	// the breaker for ("/big", "sha1").
	s.EnqueueRef("/big", "sha1")
	time.Sleep(150 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 real attempt for the first trigger, got %d", got)
	}

	// N rapid re-triggers at the SAME ref while the failure is fresh (well
	// inside the 30s base backoff window). Without the breaker, every one of
	// these re-attempts the doomed marshal — the #5726 hot loop. With the
	// breaker, they must all be skipped: calls stays at 1.
	for i := 0; i < 10; i++ {
		s.EnqueueRef("/big", "sha1")
		time.Sleep(30 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("breaker did not hold: expected calls to stay at 1 after 10 same-ref re-triggers, got %d (hot loop not bounded)", got)
	}

	// A trigger at a DIFFERENT ref (new commit / legitimately new content)
	// must always get a fresh real attempt — the breaker resets per-commit,
	// it does not permanently wedge the repo.
	s.EnqueueRef("/big", "sha2")
	time.Sleep(150 * time.Millisecond)
	if got := calls.Load(); got != 2 {
		t.Errorf("expected a commit change to reset the breaker and trigger exactly 1 more real attempt, got %d total calls", got)
	}
}

var errBoom5726 = &staticErr{"fbwriter: graph too large to serialize: simulated 2-GiB marshal panic (#5726)"}

type staticErr struct{ msg string }

func (e *staticErr) Error() string { return e.msg }
