package sched

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// #5450: the group-algo overlay must be recomputed at the END of a member-repo
// reindex, and a busy daemon that re-indexes back-to-back must not be able to
// starve that recompute indefinitely by continuously re-arming the (long)
// group-algo debounce. These tests drive the scheduler's testable units so no
// real wall-clock dependence is needed for the burst-coalescing / scoping
// assertions; the max-wait test uses tiny durations.

// newAlgoChainScheduler builds a scheduler that maps every repo to a single
// fixed group and counts group-algo invocations. Short link/algo debounces keep
// the end-to-end chain (reindex → links → group-algo) fast for tests.
func newAlgoChainScheduler(t *testing.T, group string, algoCalls *atomic.Int32, algoGroups *sync.Map) *Scheduler {
	t.Helper()
	return New(Config{
		Workers:           2,
		LinkDebounce:      5 * time.Millisecond,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour, // not exercised here; keep coalescing intact
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, g string) error {
			algoCalls.Add(1)
			algoGroups.Store(g, true)
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return []string{group} },
		MemReleaseDisabled: true,
	})
}

// TestEndOfReindex_TriggersGroupAlgo: after a member-repo reindex completes, a
// group-algo recompute is triggered for that repo's group (#5450). This is the
// reindex → scheduleLinks → runLinks → scheduleGroupAlgo chain.
func TestEndOfReindex_TriggersGroupAlgo(t *testing.T) {
	var calls atomic.Int32
	var groups sync.Map
	s := newAlgoChainScheduler(t, "acme", &calls, &groups)
	s.Start()
	defer s.Stop()

	s.Enqueue("/repo-a")

	waitFor(t, 2*time.Second, func() bool { return calls.Load() >= 1 })
	if _, ok := groups.Load("acme"); !ok {
		t.Fatal("expected the affected group's group-algo to run after reindex")
	}
}

// TestEndOfReindex_BurstCoalescesToOnePass: a burst of repo reindexes in ONE
// group coalesces into a SINGLE group-algo pass, not N (#5450 debounce/coalesce).
func TestEndOfReindex_BurstCoalescesToOnePass(t *testing.T) {
	var calls atomic.Int32
	var groups sync.Map
	// Slightly larger debounce so the whole burst lands inside one window.
	s := New(Config{
		Workers:           4,
		LinkDebounce:      40 * time.Millisecond,
		GroupAlgoDebounce: 40 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, g string) error {
			calls.Add(1)
			groups.Store(g, true)
			return nil
		},
		GroupsForRepo:      func(_ string) []string { return []string{"acme"} },
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	// Five distinct repos in the same group re-index back-to-back.
	for _, r := range []string{"/r1", "/r2", "/r3", "/r4", "/r5"} {
		s.Enqueue(r)
	}

	// Wait for exactly one pass to settle, then ensure no second pass fires.
	waitFor(t, 2*time.Second, func() bool { return calls.Load() >= 1 })
	time.Sleep(150 * time.Millisecond) // well past the debounce window
	if n := calls.Load(); n != 1 {
		t.Fatalf("burst of 5 reindexes in one group should coalesce to ONE group-algo pass, got %d", n)
	}
}

// TestEndOfReindex_OnlyAffectedGroupRefreshed: a reindex of a repo belonging to
// group A must not trigger a group-algo pass for unrelated group B (#5450 /
// #5403 scoping). The scheduler only schedules passes for GroupsForRepo(repo).
func TestEndOfReindex_OnlyAffectedGroupRefreshed(t *testing.T) {
	var seen sync.Map
	var calls atomic.Int32
	s := New(Config{
		Workers:           2,
		LinkDebounce:      5 * time.Millisecond,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  time.Hour,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, g string) error {
			calls.Add(1)
			seen.Store(g, true)
			return nil
		},
		// repo-a → groupA only; groupB has no reindexing repo.
		GroupsForRepo: func(repo string) []string {
			if repo == "/repo-a" {
				return []string{"groupA"}
			}
			return nil
		},
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	s.Enqueue("/repo-a")
	waitFor(t, 2*time.Second, func() bool { return calls.Load() >= 1 })
	time.Sleep(50 * time.Millisecond)

	if _, ok := seen.Load("groupA"); !ok {
		t.Fatal("affected group (groupA) should have been refreshed")
	}
	if _, ok := seen.Load("groupB"); ok {
		t.Fatal("unaffected group (groupB) must NOT be refreshed by an unrelated reindex")
	}
}

// TestGroupAlgoMaxWait_ForcesPromptFireUnderChurn: the core #5450 fix. With a
// short max-wait and a long debounce, continuously re-arming the debounce (as a
// busy daemon does on every link completion) must NOT starve the pass — once the
// window exceeds the max-wait, the next re-arm fires promptly. Without the
// max-wait cap, the long debounce would keep resetting and the pass would never
// run within the test window.
func TestGroupAlgoMaxWait_ForcesPromptFireUnderChurn(t *testing.T) {
	var fired atomic.Int32
	// debounce 25ms, max-wait 90ms. Re-arming every 10ms keeps resetting the
	// 25ms debounce so it would NEVER fire on its own — but the 90ms max-wait is
	// a firm ceiling. (Production uses 180s/300s; the same relationship.)
	s := New(Config{
		Workers:           1,
		GroupAlgoDebounce: 25 * time.Millisecond,
		GroupAlgoMaxWait:  90 * time.Millisecond,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, _ string) error {
			fired.Add(1)
			return nil
		},
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	// Re-arm the group-algo debounce repeatedly (every 10ms < the 25ms debounce)
	// — emulating back-to-back link completions on a busy daemon. The debounce
	// alone would be perpetually reset; only the max-wait ceiling lets it fire.
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				s.scheduleGroupAlgo("acme")
			}
		}
	}()

	// Despite continuous re-arming, the max-wait must force a fire well within
	// the window. If the cap were absent, the debounce keeps resetting → fired=0.
	waitFor(t, 2*time.Second, func() bool { return fired.Load() >= 1 })
	close(done)
	<-stopped
}

// TestGroupAlgoMaxWait_DefaultClampedAboveDebounce: a max-wait configured below
// the debounce is clamped up to the debounce so coalescing is never defeated.
func TestGroupAlgoMaxWait_DefaultClampedAboveDebounce(t *testing.T) {
	s := New(Config{
		Workers:           1,
		GroupAlgoDebounce: 30 * time.Second,
		GroupAlgoMaxWait:  1 * time.Second, // below debounce → must be clamped up
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo:         func(_ context.Context, _ string) error { return nil },
	})
	if s.cfg.GroupAlgoMaxWait < s.cfg.GroupAlgoDebounce {
		t.Fatalf("max-wait (%s) must be clamped >= debounce (%s)", s.cfg.GroupAlgoMaxWait, s.cfg.GroupAlgoDebounce)
	}
}

// TestGroupAlgoMaxWait_WindowResetsAfterFire: after a pass fires (window closes),
// a subsequent arm starts a FRESH max-wait budget — i.e. groupAlgoArmedAt is
// cleared. This guards against a stuck window forcing perpetual prompt fires.
func TestGroupAlgoMaxWait_WindowResetsAfterFire(t *testing.T) {
	gateRelease := make(chan struct{})
	var firstEntered sync.Once
	entered := make(chan struct{}, 1)
	s := New(Config{
		Workers:           1,
		GroupAlgoDebounce: 5 * time.Millisecond,
		GroupAlgoMaxWait:  10 * time.Millisecond,
		Index:             func(_ context.Context, _ string, _ string) error { return nil },
		Links:             func(_ context.Context, _ string) error { return nil },
		GroupAlgo: func(_ context.Context, _ string) error {
			firstEntered.Do(func() { entered <- struct{}{} })
			<-gateRelease
			return nil
		},
		MemReleaseDisabled: true,
	})
	s.Start()
	defer s.Stop()

	s.scheduleGroupAlgo("acme")
	<-entered // pass is now in-flight; its window was cleared on fire

	s.mu.Lock()
	_, hasWindow := s.groupAlgoArmedAt["acme"]
	s.mu.Unlock()
	if hasWindow {
		t.Fatal("max-wait window should be cleared once the pass fires")
	}
	close(gateRelease)
}
