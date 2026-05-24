package sched

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEnqueueDedups verifies that two enqueues for the same repo while
// the first is still pending result in a single index call.
func TestEnqueueDedups(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	gate := make(chan struct{})
	s := New(Config{
		Workers: 1,
		Index: func(_ context.Context, _ string, _ string) error {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-gate
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/a")
	// Block until the first index actually entered runIndex, so the
	// inflight flag is set before the duplicate enqueues arrive.
	<-started
	// Small sleep to ensure dedupLoop has drained any in-channel
	// enqueue before we add more.
	time.Sleep(50 * time.Millisecond)
	s.Enqueue("/a")
	s.Enqueue("/a")
	// Let dedupLoop process the duplicates while the first is held.
	time.Sleep(100 * time.Millisecond)
	close(gate)
	time.Sleep(200 * time.Millisecond)
	// The duplicates either dropped (because inflight=true at the
	// moment they arrived) or queued as pendingIndex=true. The
	// dedupLoop allows a single pending slot per repo: so the worst
	// case is 2 total runs (the original + one dedup'd re-run that
	// the worker found via the pendingIndex slot). Anything above 2
	// indicates a real dedup leak.
	if got := calls.Load(); got > 2 {
		t.Errorf("dedup leaked: got %d calls, want ≤ 2", got)
	}
}

// TestAlgoDebounceCancelOnWrite verifies that a new Enqueue during the
// algo debounce window cancels the pending algo pass.
func TestAlgoDebounceCancelOnWrite(t *testing.T) {
	var indexCalls atomic.Int32
	var algoCalls atomic.Int32
	s := New(Config{
		Workers:      1,
		AlgoDebounce: 500 * time.Millisecond,
		Index: func(_ context.Context, _ string, _ string) error {
			indexCalls.Add(1)
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
	// Wait for the first index to complete and schedule the algo pass.
	time.Sleep(50 * time.Millisecond)
	if got := indexCalls.Load(); got < 1 {
		t.Fatalf("expected first index by now, got %d", got)
	}
	// Trigger another write 200ms into the 500ms algo window.
	time.Sleep(200 * time.Millisecond)
	s.Enqueue("/repo")
	// The original algo timer would have fired at ~500ms — make sure
	// it does NOT, because the second Enqueue cancelled it.
	time.Sleep(400 * time.Millisecond) // now ~650ms past first index
	if got := algoCalls.Load(); got != 0 {
		t.Errorf("algo should be cancelled by mid-window write, got %d calls", got)
	}
	// Wait for the rescheduled algo to fire (500ms after 2nd index;
	// 2nd index ran instantly after 2nd Enqueue at ~250ms mark, so
	// the rescheduled algo fires at ~750ms after first index).
	time.Sleep(500 * time.Millisecond)
	if got := algoCalls.Load(); got != 1 {
		t.Errorf("expected rescheduled algo to fire exactly once, got %d", got)
	}
}

// TestLinksDebouncePerGroup verifies that two repos in the same group
// trigger a single link recompute (one per group, debounced).
func TestLinksDebouncePerGroup(t *testing.T) {
	var (
		mu        sync.Mutex
		linkCalls []string
	)
	groupsFor := func(p string) []string {
		switch p {
		case "/a", "/b":
			return []string{"shared"}
		default:
			return nil
		}
	}
	s := New(Config{
		Workers:      2,
		LinkDebounce: 100 * time.Millisecond,
		Index: func(_ context.Context, _ string, _ string) error {
			return nil
		},
		Links: func(_ context.Context, g string) error {
			mu.Lock()
			linkCalls = append(linkCalls, g)
			mu.Unlock()
			return nil
		},
		GroupsForRepo: groupsFor,
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/a")
	s.Enqueue("/b")
	time.Sleep(400 * time.Millisecond)
	mu.Lock()
	got := append([]string(nil), linkCalls...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "shared" {
		t.Errorf("expected single coalesced links call for 'shared', got %v", got)
	}
}

// TestIndexErrorRecorded verifies an index error is captured into the
// snapshot for /status reporting.
func TestIndexErrorRecorded(t *testing.T) {
	s := New(Config{
		Workers: 1,
		Index: func(_ context.Context, _ string, _ string) error {
			return errors.New("boom")
		},
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/x")
	time.Sleep(150 * time.Millisecond)
	snap := s.Snapshot()
	found := false
	for _, r := range snap.IndexedRepos {
		if r.Path == "/x" && r.LastErr == "boom" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected snapshot to record error for /x, got %+v", snap.IndexedRepos)
	}
}
