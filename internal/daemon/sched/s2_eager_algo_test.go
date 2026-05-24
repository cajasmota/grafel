package sched

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// TestS2AlgoNotFiredByDefault verifies that a post-reindex algo pass does NOT
// fire when ARCHIGRAPH_EAGER_ALGO is unset (the S2 default). The goroutine
// counter for Algorithms must remain zero after the index completes.
func TestS2AlgoNotFiredByDefault(t *testing.T) {
	// Ensure the env var is not set for this test.
	os.Unsetenv("ARCHIGRAPH_EAGER_ALGO") //nolint:errcheck

	indexed := make(chan struct{})
	var algoCalls atomic.Int32

	s := New(Config{
		Workers:      1,
		AlgoDebounce: 20 * time.Millisecond, // short so any errant pass fires fast
		Index: func(_ context.Context, _ string, _ string) error {
			close(indexed)
			return nil
		},
		Algorithms: func(_ context.Context, _ string) error {
			algoCalls.Add(1)
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/repo")
	select {
	case <-indexed:
	case <-time.After(5 * time.Second):
		t.Fatal("index did not complete within timeout")
	}

	// Wait well beyond AlgoDebounce to give any errant pass a chance to fire.
	time.Sleep(100 * time.Millisecond)

	if n := algoCalls.Load(); n != 0 {
		t.Errorf("S2: expected 0 algo calls with ARCHIGRAPH_EAGER_ALGO unset, got %d", n)
	}
}

// TestS2EagerAlgoEnvRestoresPreS2Behavior verifies that setting
// ARCHIGRAPH_EAGER_ALGO=true causes the automatic post-reindex algo pass to
// fire, matching pre-S2 behaviour.
func TestS2EagerAlgoEnvRestoresPreS2Behavior(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EAGER_ALGO", "true")

	indexed := make(chan struct{}, 1)
	algoDone := make(chan struct{})
	var algoCalls atomic.Int32

	s := New(Config{
		Workers:      1,
		AlgoDebounce: 20 * time.Millisecond,
		Index: func(_ context.Context, _ string, _ string) error {
			indexed <- struct{}{}
			return nil
		},
		Algorithms: func(_ context.Context, _ string) error {
			if algoCalls.Add(1) == 1 {
				close(algoDone)
			}
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/repo")
	select {
	case <-indexed:
	case <-time.After(5 * time.Second):
		t.Fatal("index did not complete")
	}

	select {
	case <-algoDone:
		// pass
	case <-time.After(5 * time.Second):
		t.Fatal("algo pass did not fire with ARCHIGRAPH_EAGER_ALGO=true")
	}

	if n := algoCalls.Load(); n < 1 {
		t.Errorf("expected ≥1 algo call with ARCHIGRAPH_EAGER_ALGO=true, got %d", n)
	}
}

// TestEagerAlgoEnabled verifies the env-var parsing helper covers all
// accepted truth values.
func TestEagerAlgoEnabled(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"1", true},
		{"true", true},
		{"yes", true},
	}
	for _, tc := range cases {
		t.Run(tc.val, func(t *testing.T) {
			if tc.val == "" {
				os.Unsetenv("ARCHIGRAPH_EAGER_ALGO") //nolint:errcheck
			} else {
				t.Setenv("ARCHIGRAPH_EAGER_ALGO", tc.val)
			}
			if got := eagerAlgoEnabled(); got != tc.want {
				t.Errorf("eagerAlgoEnabled() with %q = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}
