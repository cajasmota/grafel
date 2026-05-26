package sched

import (
	"context"
	"errors"
	"runtime"
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
	// This test validates debounce behaviour which requires the eager algo path.
	t.Setenv("ARCHIGRAPH_EAGER_ALGO", "true")
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

// TestPerRepoIndexerMutex verifies that 10 simultaneous enqueues for the same
// repo collapse to at most 1 active + 1 pending indexer goroutine (#2141
// root-cause C / #2140 hyp-3). This is the per-repo dedup invariant.
func TestPerRepoIndexerMutex(t *testing.T) {
	var calls atomic.Int32
	gate := make(chan struct{})
	started := make(chan struct{}, 1)

	s := New(Config{
		Workers: 10, // unlimited workers so all enqueues are eligible
		Index: func(_ context.Context, _ string, _ string) error {
			if calls.Add(1) == 1 {
				select {
				case started <- struct{}{}:
				default:
				}
			}
			<-gate
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	// Wait until the first index is in-flight, then flood with enqueues.
	s.Enqueue("/same")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first enqueue never started")
	}

	// Send 9 more enqueues for the same repo while the first is blocked.
	for i := 0; i < 9; i++ {
		s.Enqueue("/same")
	}
	time.Sleep(100 * time.Millisecond)

	// Release the gate and let all deduped enqueues drain.
	close(gate)
	time.Sleep(300 * time.Millisecond)

	// At most 2: the original in-flight + one dedup'd pending. The other
	// 8 enqueues must have been collapsed by the pending-dedup logic.
	if got := calls.Load(); got > 2 {
		t.Errorf("per-repo dedup violated: %d index calls for same repo (want ≤ 2)", got)
	}
}

// TestAlgoCapLimitsConcurrency verifies that at most AlgoCap algorithm passes
// run simultaneously. We use 10 repos and cap at 2 — the concurrently-active
// count must never exceed 2.
func TestAlgoCapLimitsConcurrency(t *testing.T) {
	// Cap test requires the eager path so the scheduler actually fires passes.
	t.Setenv("ARCHIGRAPH_EAGER_ALGO", "true")
	const numRepos = 10
	const cap = 2

	var (
		mu            sync.Mutex
		activeAlgo    int
		maxActiveAlgo int
	)

	// Gate channel: algo passes block until the test releases them.
	gate := make(chan struct{})
	// Count down: when all algo passes have started, close startedAll.
	startedAll := make(chan struct{})
	var started atomic.Int32

	s := New(Config{
		Workers:      numRepos, // allow all indexers to run in parallel
		AlgoDebounce: 10 * time.Millisecond,
		AlgoCap:      cap,
		Index: func(_ context.Context, _ string, _ string) error {
			return nil
		},
		Algorithms: func(ctx context.Context, _ string) error {
			mu.Lock()
			activeAlgo++
			if activeAlgo > maxActiveAlgo {
				maxActiveAlgo = activeAlgo
			}
			mu.Unlock()

			if started.Add(1) == numRepos {
				close(startedAll)
			}

			// Block until the gate opens or the context is cancelled.
			select {
			case <-gate:
			case <-ctx.Done():
			}

			mu.Lock()
			activeAlgo--
			mu.Unlock()
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	repos := []string{
		"/repo1", "/repo2", "/repo3", "/repo4", "/repo5",
		"/repo6", "/repo7", "/repo8", "/repo9", "/repo10",
	}
	for _, r := range repos {
		s.Enqueue(r)
	}

	// Wait for all algo passes to at least begin, with a generous timeout.
	select {
	case <-startedAll:
	case <-time.After(5 * time.Second):
		// Not all passes started within timeout — still check the cap.
	}

	mu.Lock()
	got := maxActiveAlgo
	mu.Unlock()

	// Release the gate so the scheduler can drain.
	close(gate)

	if got > cap {
		t.Errorf("algo cap violated: max concurrent algo passes = %d, want ≤ %d", got, cap)
	}
	if got < 1 {
		t.Errorf("no algo passes ran; test may be misconfigured")
	}
}

// TestStopCancelsInFlightIndex verifies the fix for issue #2176: when
// Stop() is called while an IndexFn is running, the context passed to
// IndexFn is cancelled so any exec.CommandContext child process receives
// SIGTERM instead of surviving as a zombie.
func TestStopCancelsInFlightIndex(t *testing.T) {
	// indexStarted is closed when the index function is entered.
	indexStarted := make(chan struct{})
	// ctxCancelledDuring is closed when the index function observes ctx.Done().
	ctxCancelledDuring := make(chan struct{})

	s := New(Config{
		Workers: 1,
		Index: func(ctx context.Context, _ string, _ string) error {
			close(indexStarted)
			// Block until the context is cancelled (simulates a long-running
			// subprocess indexer that respects ctx cancellation).
			select {
			case <-ctx.Done():
				close(ctxCancelledDuring)
				return ctx.Err()
			case <-time.After(10 * time.Second):
				return nil // should never reach here in this test
			}
		},
	})
	s.Start()
	s.Enqueue("/repo")

	// Wait until the indexer is actually running.
	select {
	case <-indexStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("index function never started")
	}

	// Call Stop — this should cancel shutdownCtx, which propagates to
	// the IndexFn's jobCtx, which unblocks the select above.
	stopDone := make(chan struct{})
	go func() {
		s.Stop()
		close(stopDone)
	}()

	// The IndexFn must observe the cancellation within a short window.
	select {
	case <-ctxCancelledDuring:
		// correct: Stop() cancelled the in-flight job context
	case <-time.After(2 * time.Second):
		t.Error("Stop() did not cancel the in-flight IndexFn context (zombie leak still present)")
	}

	// Stop must also return (wg.Wait completes once IndexFn returns).
	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Error("Stop() did not return after IndexFn exited")
	}
}

// TestResolveAlgoCap verifies the auto-tune formula.
func TestResolveAlgoCap(t *testing.T) {
	// Explicit cap: use as-is.
	if got := resolveAlgoCap(4); got != 4 {
		t.Errorf("resolveAlgoCap(4) = %d, want 4", got)
	}
	if got := resolveAlgoCap(1); got != 1 {
		t.Errorf("resolveAlgoCap(1) = %d, want 1", got)
	}

	// Auto-tune: floor at 2.
	auto := resolveAlgoCap(0)
	expected := runtime.NumCPU() / 2
	if expected < 2 {
		expected = 2
	}
	if auto != expected {
		t.Errorf("resolveAlgoCap(0) = %d, want %d (NumCPU=%d)", auto, expected, runtime.NumCPU())
	}
}

// TestSchedulerStop_Idempotent verifies that calling Stop() twice does not
// panic. Prior to the sync.Once fix (issue #2494), the second call would
// panic with "close of closed channel".
func TestSchedulerStop_Idempotent(t *testing.T) {
	s := New(Config{
		Workers: 1,
		Index: func(_ context.Context, _ string, _ string) error {
			return nil
		},
	})
	s.Start()

	// First Stop — normal path.
	s.Stop()

	// Second Stop — must be a no-op and MUST NOT panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Stop() panicked on second call: %v", r)
		}
	}()
	s.Stop()
}

// TestRunLinksUsesShutdownCtx verifies that the Links callback receives a
// cancellable context derived from the scheduler's shutdownCtx, not a
// context.Background() orphan. When Stop() is called, any in-flight Links
// call should observe ctx.Done() — this is the fix for issue #2493.
func TestRunLinksUsesShutdownCtx(t *testing.T) {
	t.Helper()

	linkStarted := make(chan struct{})
	linkCtxCancelled := make(chan struct{})

	s := New(Config{
		Workers:      1,
		LinkDebounce: 10 * time.Millisecond,
		Index: func(_ context.Context, _ string, _ string) error {
			return nil
		},
		Links: func(ctx context.Context, _ string) error {
			close(linkStarted)
			select {
			case <-ctx.Done():
				close(linkCtxCancelled)
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil // should not reach here in this test
			}
		},
		GroupsForRepo: func(_ string) []string { return []string{"g"} },
	})
	s.Start()
	s.Enqueue("/repo")

	// Wait until the Links callback is running.
	select {
	case <-linkStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Links callback never started")
	}

	// Stop should cancel shutdownCtx, which must propagate to the Links ctx.
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-linkCtxCancelled:
		// correct: shutdownCtx cancellation reached the Links callback
	case <-time.After(2 * time.Second):
		t.Error("Stop() did not cancel the Links callback context (issue #2493 regression)")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Stop() did not return after Links callback exited")
	}
}
