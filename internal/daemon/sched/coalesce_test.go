package sched

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestCoalesce_OneInFlightPlusSingleFollowUp proves the #5138 invariant:
// N concurrent reindex requests for ONE repo while a reindex is in-flight
// collapse into exactly ONE in-flight run plus AT MOST ONE coalesced
// follow-up — never N stacked jobs.
//
// Deterministic synchronisation (no sleeps): the fake Index func signals
// when it has entered the in-flight run (so the duplicate enqueues are
// guaranteed to arrive while inflight[repo] is set), then blocks on a gate
// the test releases. We assert the Index func is invoked at most twice.
func TestCoalesce_OneInFlightPlusSingleFollowUp(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{}, 1) // signalled each time Index enters
	release := make(chan struct{})    // test releases the in-flight run(s)

	s := New(Config{
		Workers: 4, // plenty of workers — coalescing must NOT rely on a 1-worker pool
		Index: func(_ context.Context, _ string, _ string) error {
			calls.Add(1)
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	const repo = "/repo-a"

	// Kick off the first reindex and wait until it is provably in-flight.
	s.Enqueue(repo)
	<-entered

	// Fire 10 more reindex requests for the SAME repo while the first is
	// held in-flight. These must all coalesce into a single dirty marker.
	for i := 0; i < 10; i++ {
		s.Enqueue(repo)
	}

	// Wait until the scheduler has observed at least one of those enqueues
	// as a dirty mark (deterministic: poll the guarded map, not a sleep).
	waitFor(t, time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.dirty[repo]
	})

	// Release all runs. The in-flight run completes and, because the repo
	// is dirty, schedules exactly ONE follow-up. The follow-up also reads
	// from the (now-closed) release channel, so it proceeds immediately.
	close(release)

	// The follow-up run must enter exactly once.
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("expected a single coalesced follow-up run, none occurred")
	}

	// Deterministically settle: wait until the scheduler has no in-flight,
	// no pending, and no dirty work for this repo — i.e. everything drained.
	waitFor(t, 2*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		_, running := s.inflight[repo]
		return !running && !s.dirty[repo] && !s.pendingIndex[repo]
	})

	if got := calls.Load(); got != 2 {
		t.Fatalf("coalescing failed: Index invoked %d times, want exactly 2 "+
			"(1 in-flight + 1 coalesced follow-up)", got)
	}
}

// TestCoalesce_NoLostUpdate proves requirement #3: a change that lands
// DURING an in-flight reindex must still be captured by the single
// follow-up — the marker is set after the run snapshots its input. We model
// "the later change" by having the test enqueue strictly after the run has
// entered (so the run's snapshot predates it) and asserting a follow-up runs.
func TestCoalesce_NoLostUpdate(t *testing.T) {
	var runs atomic.Int32
	entered := make(chan struct{}, 8)
	gate1 := make(chan struct{})

	var mu sync.Mutex
	var gate2 chan struct{}

	s := New(Config{
		Workers: 2,
		Index: func(_ context.Context, _ string, _ string) error {
			n := runs.Add(1)
			entered <- struct{}{}
			if n == 1 {
				<-gate1 // hold the first run open
			} else {
				mu.Lock()
				g := gate2
				mu.Unlock()
				if g != nil {
					<-g
				}
			}
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	const repo = "/repo-b"
	mu.Lock()
	gate2 = make(chan struct{})
	mu.Unlock()

	// Run 1 starts and is held open (snapshot taken).
	s.Enqueue(repo)
	<-entered

	// The "later change" lands while run 1 is still in-flight.
	s.Enqueue(repo)
	waitFor(t, time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.dirty[repo]
	})

	// Complete run 1 — the follow-up MUST run because dirty was set after
	// the snapshot. If the marker were lost, no second run would ever start.
	close(gate1)

	select {
	case <-entered:
		// follow-up run started — no lost update. Good.
	case <-time.After(2 * time.Second):
		t.Fatal("no-lost-update violated: change during in-flight run did not trigger a follow-up")
	}

	mu.Lock()
	close(gate2)
	mu.Unlock()

	waitFor(t, time.Second, func() bool { return runs.Load() == 2 })
	if got := runs.Load(); got != 2 {
		t.Fatalf("expected exactly 2 runs (in-flight + single follow-up), got %d", got)
	}
}

// TestCoalesce_PerRepoNotGlobal proves requirement #4: coalescing is
// per-repo. Two DIFFERENT repos enqueued concurrently must BOTH reindex at
// the same time — the in-flight guard for repo A must not block repo B.
func TestCoalesce_PerRepoNotGlobal(t *testing.T) {
	bothIn := make(chan string, 2)
	release := make(chan struct{})

	s := New(Config{
		Workers: 2,
		Index: func(_ context.Context, repoPath string, _ string) error {
			bothIn <- repoPath
			<-release
			return nil
		},
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/repo-x")
	s.Enqueue("/repo-y")

	// Both must be in-flight concurrently. Collect two DISTINCT repos before
	// releasing either — if coalescing were global this would deadlock and
	// the test times out.
	seen := map[string]bool{}
	timeout := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case r := <-bothIn:
			seen[r] = true
		case <-timeout:
			t.Fatalf("per-repo concurrency violated: only %d/2 repos ran concurrently (seen=%v)", len(seen), seen)
		}
	}
	close(release)

	if !seen["/repo-x"] || !seen["/repo-y"] {
		t.Fatalf("expected both /repo-x and /repo-y to run concurrently, saw %v", seen)
	}
}

// waitFor polls cond until true or the deadline elapses (deterministic
// convergence on a guarded predicate — not a fixed sleep). It fails the
// test on timeout.
func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if !cond() {
		t.Fatalf("waitFor: condition not met within %s", d)
	}
}
