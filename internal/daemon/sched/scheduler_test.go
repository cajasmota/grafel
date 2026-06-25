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

// (former TestAlgoDebounceCancelOnWrite — replaced by the group-algo tests
// below after the per-repo algo pass was removed in #5349 A3.)
//
// TestGroupAlgoCoalescesRepoReindexes verifies that a new Enqueue during the
// algo debounce window cancels the pending algo pass.
// TestGroupAlgoCoalescesRepoReindexes is the #5349 A3 debounce/coalesce
// acceptance: 5 rapid repo reindexes in one group collapse to exactly ONE link
// pass and ONE group-algo pass. The group-algo timer is armed off the link
// pass's success path, so the link debounce coalesces the reindex burst and the
// group-algo debounce coalesces on top of that.
func TestGroupAlgoCoalescesRepoReindexes(t *testing.T) {
	var (
		mu             sync.Mutex
		linkCalls      int
		groupAlgoCalls int
	)
	groupsFor := func(p string) []string {
		switch p {
		case "/a", "/b", "/c", "/d", "/e":
			return []string{"shared"}
		default:
			return nil
		}
	}
	s := New(Config{
		Workers:           5,
		LinkDebounce:      80 * time.Millisecond,
		GroupAlgoDebounce: 80 * time.Millisecond,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links: func(_ context.Context, g string) error {
			mu.Lock()
			linkCalls++
			mu.Unlock()
			return nil
		},
		GroupAlgo: func(_ context.Context, g string) error {
			if g != "shared" {
				t.Errorf("unexpected group %q", g)
			}
			mu.Lock()
			groupAlgoCalls++
			mu.Unlock()
			return nil
		},
		GroupsForRepo: groupsFor,
	})
	s.Start()
	defer s.Stop()

	// 5 rapid reindexes across the group's repos.
	for _, r := range []string{"/a", "/b", "/c", "/d", "/e"} {
		s.Enqueue(r)
	}

	// Converge, then settle: poll until both coalesced passes have run (index
	// burst → link debounce → group-algo debounce), then confirm the counts
	// hold steady past two further debounce windows. Polling on the guarded
	// counters (not a fixed sleep) removes the wall-clock straddle that made
	// this flake under slow CI: a slow scheduler just delays convergence, it
	// can never split the burst into a second pass.
	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return linkCalls == 1 && groupAlgoCalls == 1
	})
	// Settle window: well past LinkDebounce+GroupAlgoDebounce (80+80ms) to
	// catch any erroneous second pass.
	time.Sleep(400 * time.Millisecond)

	mu.Lock()
	gotLinks, gotAlgo := linkCalls, groupAlgoCalls
	mu.Unlock()
	if gotLinks != 1 {
		t.Errorf("expected exactly 1 coalesced link pass, got %d", gotLinks)
	}
	if gotAlgo != 1 {
		t.Errorf("expected exactly 1 coalesced group-algo pass, got %d", gotAlgo)
	}
}

// TestGroupAlgoReArmsOnNewLinkCompletion verifies the group-algo timer is
// cancelled+rescheduled when a fresh link pass completes mid-debounce, so two
// separated reindex bursts still produce a single (latest) group-algo pass per
// settled window rather than a stale one.
func TestGroupAlgoReArmsOnNewLinkCompletion(t *testing.T) {
	var (
		mu             sync.Mutex
		groupAlgoCalls int
	)
	groupsFor := func(p string) []string {
		if p == "/a" || p == "/b" {
			return []string{"shared"}
		}
		return nil
	}
	s := New(Config{
		Workers:           2,
		LinkDebounce:      30 * time.Millisecond,
		GroupAlgoDebounce: 200 * time.Millisecond,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, _ string) error {
			mu.Lock()
			groupAlgoCalls++
			mu.Unlock()
			return nil
		},
		GroupsForRepo: groupsFor,
	})
	s.Start()
	defer s.Stop()

	// Timing here is structural, not wall-clock-fragile: the group-algo timer
	// is *re-armed* (old timer cancelled) when /b's link pass completes, so the
	// only way group-algo can fire is GroupAlgoDebounce (200ms) after the LAST
	// link completion. The mid-window check below proves it did NOT fire early;
	// the final check converges on the single eventual pass. The sleeps are
	// kept SMALL relative to the 200ms debounce (re-arm happens at ~100ms, well
	// before the first 200ms timer could elapse) so even a slow CI cannot make
	// the first timer fire before the re-arm cancels it.
	s.Enqueue("/a")
	time.Sleep(100 * time.Millisecond) // first link pass done, group-algo armed (200ms)
	s.Enqueue("/b")                    // second burst → re-arms the group-algo timer
	time.Sleep(150 * time.Millisecond) // first group-algo timer would have fired (~at 100+200) — but re-armed
	mu.Lock()
	mid := groupAlgoCalls
	mu.Unlock()
	if mid != 0 {
		t.Errorf("group-algo fired before the re-armed window settled, got %d", mid)
	}
	// Converge on the single eventual pass with a generous deadline (slow CI
	// just takes longer to reach the re-armed 200ms window), then settle to
	// confirm no extra pass follows.
	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return groupAlgoCalls == 1
	})
	time.Sleep(300 * time.Millisecond) // settle past another GroupAlgoDebounce
	mu.Lock()
	got := groupAlgoCalls
	mu.Unlock()
	if got != 1 {
		t.Errorf("expected exactly 1 group-algo pass after settle, got %d", got)
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
	// Converge on the single coalesced link pass, then settle to prove no
	// second pass follows. Poll the guarded slice rather than asserting on a
	// fixed sleep so slow CI can't straddle the 100ms LinkDebounce window.
	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(linkCalls) == 1
	})
	time.Sleep(300 * time.Millisecond) // settle: 3× LinkDebounce
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

// TestGroupAlgoCapLimitsConcurrency verifies the group-algo pass acquires the
// algoSem cap (#5349 A3): with AlgoCap=1, two concurrently-armed group passes
// (different groups) never overlap. We drive each group via its own repo and
// gate the GroupAlgo hook so both would-be passes are live at once if uncapped.
func TestGroupAlgoCapLimitsConcurrency(t *testing.T) {
	const cap = 1
	var (
		mu        sync.Mutex
		active    int
		maxActive int
	)
	gate := make(chan struct{})
	started := make(chan struct{}, 4)

	groupsFor := func(p string) []string {
		switch p {
		case "/g1repo":
			return []string{"g1"}
		case "/g2repo":
			return []string{"g2"}
		default:
			return nil
		}
	}
	s := New(Config{
		Workers:           4,
		LinkDebounce:      10 * time.Millisecond,
		GroupAlgoDebounce: 10 * time.Millisecond,
		AlgoCap:           cap,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(ctx context.Context, _ string) error {
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()
			started <- struct{}{}
			select {
			case <-gate:
			case <-ctx.Done():
			}
			mu.Lock()
			active--
			mu.Unlock()
			return nil
		},
		GroupsForRepo: groupsFor,
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/g1repo")
	s.Enqueue("/g2repo")

	// Wait for the first group-algo pass to enter (the second is blocked on the
	// cap), then release.
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("no group-algo pass started")
	}
	time.Sleep(100 * time.Millisecond) // give a second pass a chance to (wrongly) start
	close(gate)

	mu.Lock()
	got := maxActive
	mu.Unlock()
	if got > cap {
		t.Errorf("group-algo cap violated: max concurrent = %d, want ≤ %d", got, cap)
	}
	if got < 1 {
		t.Errorf("no group-algo pass ran; test misconfigured")
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

// TestGroupAlgoCancelledOnStop is the #5349 A3 cancel acceptance: a daemon
// Stop() (SIGTERM) mid group-algo pass cancels the pass's ctx cleanly, the
// hook observes ctx.Done(), and Stop() returns without a goroutine leak.
func TestGroupAlgoCancelledOnStop(t *testing.T) {
	algoStarted := make(chan struct{})
	algoCtxCancelled := make(chan struct{})

	s := New(Config{
		Workers:           1,
		LinkDebounce:      10 * time.Millisecond,
		GroupAlgoDebounce: 10 * time.Millisecond,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(ctx context.Context, _ string) error {
			close(algoStarted)
			select {
			case <-ctx.Done():
				close(algoCtxCancelled)
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		},
		GroupsForRepo: func(_ string) []string { return []string{"g"} },
	})
	s.Start()
	s.Enqueue("/repo")

	select {
	case <-algoStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("group-algo callback never started")
	}

	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-algoCtxCancelled:
		// correct: shutdownCtx cancellation reached the group-algo callback.
	case <-time.After(2 * time.Second):
		t.Error("Stop() did not cancel the group-algo callback context")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Stop() did not return after group-algo callback exited")
	}
}

// TestSkipEnqueue_dropsLinkedWorktree asserts the #3680 guard: when
// Config.SkipEnqueue returns true for a path, that enqueue is dropped before
// it can become a root index job — so the indexer never runs for it. A
// non-matching path is still indexed normally (negative case).
func TestSkipEnqueue_dropsLinkedWorktree(t *testing.T) {
	const worktreePath = "/repos/primary/.worktrees/agent-7"
	const realRepo = "/repos/primary"

	var indexed sync.Map // repoPath -> struct{}
	done := make(chan string, 4)
	s := New(Config{
		Workers: 1,
		// Gate: treat the worktree path as a linked worktree of an indexed
		// primary; everything else is a normal root repo.
		SkipEnqueue: func(repoPath string) bool {
			return repoPath == worktreePath
		},
		Index: func(_ context.Context, repoPath string, _ string) error {
			indexed.Store(repoPath, struct{}{})
			done <- repoPath
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	// Gated: must NOT be indexed.
	s.Enqueue(worktreePath)
	// Ungated: must be indexed (proves the gate is selective, not a kill-switch).
	s.Enqueue(realRepo)

	select {
	case got := <-done:
		if got != realRepo {
			t.Fatalf("indexed unexpected repo %q; the gated worktree must not be indexed", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("real repo was never indexed — gate must not block non-worktree paths")
	}

	// Give any (erroneously) admitted worktree job time to run, then assert
	// it never did.
	time.Sleep(150 * time.Millisecond)
	if _, ok := indexed.Load(worktreePath); ok {
		t.Fatalf("linked-worktree path %q was cold-indexed despite the #3680 gate", worktreePath)
	}

	// The skip must be recorded in the recent-log telemetry for observability.
	snap := s.Snapshot()
	var sawSkip bool
	for _, e := range snap.RecentLog {
		if e.Kind == "enqueue_skipped" && e.Repo == worktreePath {
			sawSkip = true
		}
	}
	if !sawSkip {
		t.Errorf("expected an enqueue_skipped log entry for the gated worktree")
	}
}
