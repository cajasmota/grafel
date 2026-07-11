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
// These tests drive the REAL production trigger path (Enqueue → RefCapture),
// which always yields a CONSTANT branch name across same-branch commits and a
// SHA that changes per commit. That is exactly why the breaker must key on the
// commit SHA, not the branch ref: a fix commit on the same branch keeps the
// name but changes the SHA (breaker must reset), and a detached HEAD has an
// empty name but a valid SHA (breaker must still gate).

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestReindexCircuitBreaker_SameCommitSkips_NewCommitResets drives the
// production Enqueue→RefCapture flow: the branch NAME is constant ("main")
// throughout; only the commit SHA changes. This is the case the branch-name
// keyed breaker got wrong — a same-branch fix commit would be skipped until
// the backoff expired.
func TestReindexCircuitBreaker_SameCommitSkips_NewCommitResets(t *testing.T) {
	var calls atomic.Int32

	var mu sync.Mutex
	ref := "main" // CONSTANT branch name — never changes, exactly as production
	sha := "sha-aaaaaaaaaaaa"

	s := New(Config{
		Workers: 1,
		RefCapture: func(_ string) (string, string) {
			mu.Lock()
			defer mu.Unlock()
			return ref, sha
		},
		Index: func(_ context.Context, _ string, _ string) error {
			calls.Add(1)
			return errBoom5726
		},
	})
	s.Start()
	defer s.Stop()

	// First trigger at commit sha-aaa: a real attempt, which fails and arms
	// the breaker for that commit.
	s.Enqueue("/big")
	time.Sleep(150 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 real attempt for the first trigger, got %d", got)
	}

	// N rapid re-triggers at the SAME commit while the failure is fresh (well
	// inside the 30s base backoff window). Without the breaker, every one of
	// these re-attempts the doomed marshal — the #5726 hot loop. With the
	// breaker, they must all be skipped: calls stays at 1.
	for i := 0; i < 10; i++ {
		s.Enqueue("/big")
		time.Sleep(30 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("breaker did not hold: expected calls to stay at 1 after 10 same-commit re-triggers, got %d (hot loop not bounded)", got)
	}

	// A NEW commit on the SAME branch (developer commits a fix; RefCapture
	// still reports ref="main" but a fresh SHA) must get a real attempt — the
	// breaker resets per-commit, not per-branch. A branch-name-keyed breaker
	// would wrongly skip this until the backoff window expired.
	mu.Lock()
	sha = "sha-bbbbbbbbbbbb"
	mu.Unlock()
	s.Enqueue("/big")
	time.Sleep(150 * time.Millisecond)
	if got := calls.Load(); got != 2 {
		t.Errorf("expected a new commit on the same branch to reset the breaker and trigger exactly 1 more real attempt, got %d total calls", got)
	}
}

// TestReindexCircuitBreaker_DetachedHeadGatesBySHA covers the gap the
// branch-name keyed breaker left wide open: a detached HEAD (or any state
// where RefCapture yields an EMPTY ref but a valid SHA). The old breaker
// required a non-empty ref to gate, so the hot loop recurred unbounded — the
// exact failure mode this PR exists to prevent. Keying on the SHA closes it.
func TestReindexCircuitBreaker_DetachedHeadGatesBySHA(t *testing.T) {
	var calls atomic.Int32

	s := New(Config{
		Workers: 1,
		RefCapture: func(_ string) (string, string) {
			// Detached HEAD: no branch name, but a perfectly good commit SHA.
			return "", "detached-sha-1"
		},
		Index: func(_ context.Context, _ string, _ string) error {
			calls.Add(1)
			return errBoom5726
		},
	})
	s.Start()
	defer s.Stop()

	// First real attempt fails and arms the breaker keyed on the SHA.
	s.Enqueue("/detached")
	time.Sleep(150 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 real attempt for the first trigger, got %d", got)
	}

	// Rapid re-triggers with the same (empty ref, same SHA) must be skipped.
	for i := 0; i < 10; i++ {
		s.Enqueue("/detached")
		time.Sleep(30 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("detached-HEAD breaker did not gate by SHA: expected calls to stay at 1, got %d (empty ref must not disable the breaker)", got)
	}
}

var errBoom5726 = &staticErr{"fbwriter: graph too large to serialize: simulated 2-GiB marshal panic (#5726)"}

type staticErr struct{ msg string }

func (e *staticErr) Error() string { return e.msg }
