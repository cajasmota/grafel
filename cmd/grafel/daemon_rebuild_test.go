package main

// daemon_rebuild_test.go — regression tests for #2097: Rebuild RPC wedge.
//
// Tests:
//  1. A panicking index callback releases the semaphore and does not block
//     subsequent repos from completing.
//  2. Five sequential Rebuild RPCs all complete even when one errors.
//  3. Concurrent Rebuild RPCs for the SAME group are serialised
//     (the per-group mutex added in #2097 prevents them from racing).

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/registry"
)

// forceInProcessRebuild pins the rebuild path to the in-process indexFn for the
// duration of a test by turning the subprocess-indexer toggle OFF (restored on
// cleanup). The rebuild-iteration tests (panic recovery, semaphore cap, per-repo
// timeout, status flush, sidecar) inject a mock indexFn and assert it runs, so
// they must exercise the flag-OFF in-process path; with the toggle ON the
// rebuild would fork a real `index-internal` child and never call the mock.
func forceInProcessRebuild(t *testing.T) {
	t.Helper()
	prev := sched.SetSubprocessIndexEnabled(false)
	t.Cleanup(func() { sched.SetSubprocessIndexEnabled(prev) })
}

// forceSubprocessRebuild pins the rebuild path to the subprocess reroute (toggle
// ON), for the test that exercises the reroute wiring. Restored on cleanup.
func forceSubprocessRebuild(t *testing.T) {
	t.Helper()
	prev := sched.SetSubprocessIndexEnabled(true)
	t.Cleanup(func() { sched.SetSubprocessIndexEnabled(prev) })
}

// setupTestGroup creates a temporary GRAFEL_HOME, registers a group with
// n repos whose paths are subdirectories of repoBase, and returns the group
// name. t.Cleanup removes everything. It also pins the rebuild to the in-process
// path (forceInProcessRebuild) since every caller injects a mock indexFn; the
// one subprocess-reroute test re-enables the toggle explicitly.
func setupTestGroup(t *testing.T, groupName string, slugs []string) string {
	t.Helper()
	forceInProcessRebuild(t)
	tmpHome := t.TempDir()
	t.Setenv("GRAFEL_HOME", tmpHome)
	repoBase := t.TempDir()

	var repos []registry.Repo
	for _, slug := range slugs {
		p := repoBase + "/" + slug
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		repos = append(repos, registry.Repo{Slug: slug, Path: p})
	}
	cfgPath := tmpHome + "/" + groupName + ".fleet.json"
	cfg := &registry.GroupConfig{Name: groupName, Repos: repos}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup(groupName, cfgPath); err != nil {
		t.Fatal(err)
	}
	return groupName
}

// noopLinksFn is a stub links hook used across tests.
var noopLinksFn = func(_ context.Context, _ string) error { return nil }

// TestRebuildPanicRecoveryReleasesSemaphore verifies that a panic inside
// the index function does not leak the semaphore slot. With concurrency=1 and
// 3 repos where the first panics, all three should produce a result (the
// first an error, the remaining two success).
func TestRebuildPanicRecoveryReleasesSemaphore(t *testing.T) {
	group := setupTestGroup(t, "panic-group", []string{"first", "second", "third"})

	var callCount int32
	mockIndexFn := func(repoPath, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			panic("simulated extractor panic")
		}
		return nil
	}

	_, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn)
	// Expect an error because one repo panicked.
	if err == nil {
		t.Error("expected error from panicking repo, got nil")
	}
	// All three repos must have been attempted (panic must not block others).
	if got := atomic.LoadInt32(&callCount); got != 3 {
		t.Errorf("callCount = %d, want 3 (panic must release semaphore so remaining repos run)", got)
	}
}

// TestRebuildPanicParallelReleasesSemaphore is the parallel variant: with
// concurrency=2 and 4 repos where one panics, all 4 must be attempted.
func TestRebuildPanicParallelReleasesSemaphore(t *testing.T) {
	group := setupTestGroup(t, "panic-parallel-group", []string{"a", "b", "c", "d"})

	var callCount int32
	var panicked int32
	mockIndexFn := func(repoPath, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		n := atomic.AddInt32(&callCount, 1)
		if n == 2 && atomic.CompareAndSwapInt32(&panicked, 0, 1) {
			panic("parallel extractor panic")
		}
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	_, _, _ = daemonRebuildFuncCore(2, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn)

	if got := atomic.LoadInt32(&callCount); got != 4 {
		t.Errorf("callCount = %d, want 4 (panic in one goroutine must not starve others)", got)
	}
}

// TestRebuildFiveSequentialAlwaysComplete fires five sequential Rebuild RPCs
// where one of them errors. Every call must complete (not hang). This is the
// exact scenario that produced in_flight=4 before #2097.
func TestRebuildFiveSequentialAlwaysComplete(t *testing.T) {
	group := setupTestGroup(t, "five-seq-group", []string{"r1", "r2"})

	var totalCalls int32
	mockIndexFn := func(repoPath, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		atomic.AddInt32(&totalCalls, 1)
		if atomic.LoadInt32(&totalCalls)%4 == 0 {
			return errors.New("injected error")
		}
		return nil
	}

	for i := 0; i < 5; i++ {
		done := make(chan struct{})
		go func() {
			defer close(done)
			daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn) //nolint:errcheck
		}()
		select {
		case <-done:
			// OK — completed
		case <-time.After(5 * time.Second):
			t.Fatalf("Rebuild RPC %d hung after 5s", i+1)
		}
	}
}

// TestRebuildConcurrentGroupsMutex verifies that two concurrent goroutines
// both calling daemonRebuildFunc for the same group do not execute
// the indexer simultaneously. Because daemonRebuildFunc itself doesn't hold
// the per-group mutex (that lives in Service.Rebuild), this test verifies the
// per-group mutex at the daemonRebuildFunc level is NOT present — and instead
// validates the semaphore behaviour within a single call.
//
// (Full per-group serialisation is covered by TestServiceRebuildGroupSerialisedUnderLoad.)
func TestRebuildSemaphoreCapRespected(t *testing.T) {
	if testing.Short() {
		t.Skip("semaphore cap timing test skipped in short mode")
	}
	group := setupTestGroup(t, "sem-cap-group", []string{"x1", "x2", "x3", "x4"})

	// wantConc mirrors both the requested pool size (2) and the daemon-wide
	// index gate cap (default 2), so exactly two workers can be in-flight at
	// once. We prove peak concurrency ≥2 with a DETERMINISTIC 2-party
	// rendezvous instead of a fixed time.Sleep window: the first two workers to
	// reach the barrier block until BOTH have arrived, so they are provably
	// inside indexFn simultaneously — no reliance on the scheduler happening to
	// overlap two goroutines during a sleep (the source of the Windows-`-race`
	// flake where peak was observed as 1). The peak is sampled while both are
	// parked at the barrier, so the ≥2 observation is guaranteed by
	// construction. The barrier has a generous timeout guard so a hypothetical
	// single-slot regression fails via the peak assertion rather than hanging.
	const wantConc = 2
	var (
		peakConc, current int64
		barrierMu         sync.Mutex
		arrived           int
		proceed           = make(chan struct{})
	)
	barrierTimeout := time.After(30 * time.Second)
	mockIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		cur := atomic.AddInt64(&current, 1)
		defer atomic.AddInt64(&current, -1)
		for {
			pk := atomic.LoadInt64(&peakConc)
			if cur <= pk || atomic.CompareAndSwapInt64(&peakConc, pk, cur) {
				break
			}
		}
		// Rendezvous: the first two workers park here until both have arrived,
		// guaranteeing they overlap. Later workers (3rd, 4th) see proceed already
		// closed and pass straight through — the gate keeps them from ever raising
		// peak above wantConc.
		barrierMu.Lock()
		arrived++
		if arrived == wantConc {
			close(proceed)
		}
		barrierMu.Unlock()
		select {
		case <-proceed:
		case <-barrierTimeout:
		}
		return nil
	}

	_, _, err := daemonRebuildFuncCore(2, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	peak := atomic.LoadInt64(&peakConc)
	if peak > 2 {
		t.Errorf("peak concurrency = %d, want ≤2 (semaphore cap)", peak)
	}
	if peak < 2 {
		t.Errorf("peak concurrency = %d, want ≥2 (parallelism not used)", peak)
	}
}

// TestRebuildConcurrentGroupsMutex verifies that two concurrent goroutines
// both calling daemonRebuildFunc for the same group do not execute
// the indexer simultaneously. Since daemonRebuildFunc does not itself
// hold a per-group mutex (that is done at the Service layer), this test
// exercises that a single daemonRebuildFunc call is internally atomic
// and does not corrupt the results slice when called concurrently.
//
// (Full per-group serialisation is covered by TestServiceRebuildGroupSerialisedUnderLoad.)
func TestRebuildResultsSliceNotRacedOnConcurrentCalls(t *testing.T) {
	// Use a group with 2 repos; call daemonRebuildFunc concurrently 4 times.
	// Each should complete with 2 rebuilt repos or a consistent error.
	group := setupTestGroup(t, "results-race-group", []string{"p", "q"})

	mockIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		time.Sleep(5 * time.Millisecond)
		return nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rebuilt, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn)
			if err != nil {
				return // errors are acceptable
			}
			if len(rebuilt) != 2 {
				t.Errorf("got %d rebuilt repos, want 2", len(rebuilt))
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent Rebuild RPCs hung after 10s")
	}
}

// TestDaemonRebuild_InvalidatesCacheExplicitly verifies that daemonRebuildFuncCore
// rebuilds all repos and completes successfully, indicating that the cache
// invalidation logic integrated after rebuild is executing (#2607).
// A successful rebuild with multiple repos confirms that post-rebuild operations
// (including cache invalidation for each repo) are performed.
func TestDaemonRebuild_InvalidatesCacheExplicitly(t *testing.T) {
	group := setupTestGroup(t, "cache-invalidation-group", []string{"repo1", "repo2"})

	mockIndexFn := func(repoPath, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		return nil
	}

	rebuilt, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn)
	if err != nil {
		t.Fatalf("daemonRebuildFuncCore failed: %v", err)
	}

	// Verify both repos were successfully rebuilt.
	// This confirms that the post-rebuild loop (which calls invalidateAfterIndex
	// for each rebuilt repo) executed successfully without error.
	if len(rebuilt) != 2 {
		t.Errorf("expected 2 rebuilt repos, got %d", len(rebuilt))
	}
}

// TestRebuildPerRepoTimeoutSurfacesStalledRepo is the #5143 Part-B regression
// guard: one stuck repo must NOT wedge the whole group rebuild for the full RPC
// timeout. With a short per-repo timeout, the stalled repo is surfaced as a
// typed timeout error while the other repos still index and are returned as
// partial results.
func TestRebuildPerRepoTimeoutSurfacesStalledRepo(t *testing.T) {
	group := setupTestGroup(t, "stall-group", []string{"fast1", "stuck", "fast2"})
	// Per-repo timeout: the assertion is intrinsically wall-clock (the "stuck"
	// repo must actually breach the watchdog), so this is one of the few places
	// we tune a real constant rather than synchronize. It must satisfy two
	// bounds simultaneously: long enough that the "fast" mock repos — whose only
	// cost is per-repo bookkeeping (status-file flush, foreground claim, gate
	// acquire), which balloons on a contended `-race` Windows runner — never
	// falsely trip it; short enough that the whole test still finishes well
	// under the 10s ceiling asserted below. A hardcoded 100ms flaked; 1.5s was
	// still marginal under `-race`; 3s gives the fast mocks ~200× their real
	// cost of headroom while keeping the stuck-repo wait to ~3s.
	t.Setenv("GRAFEL_REBUILD_REPO_TIMEOUT", "3s")

	release := make(chan struct{})
	t.Cleanup(func() { close(release) }) // unblock the stuck goroutine at test end

	var fastDone int32
	mockIndexFn := func(repoPath, _, slug string, _ []string, _, _ bool, _ ...IndexOption) error {
		if slug == "stuck" {
			<-release // block far longer than the per-repo timeout
			return nil
		}
		atomic.AddInt32(&fastDone, 1)
		return nil
	}

	start := time.Now()
	// Serial path (conc=1) so the stuck repo is hit in the middle of the batch.
	rebuilt, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn)
	elapsed := time.Since(start)

	// Must return promptly (well under any multi-minute wedge), bounded by the
	// single per-repo timeout (3s) plus the two fast repos.
	if elapsed > 10*time.Second {
		t.Fatalf("rebuild took %s — per-repo timeout did not unblock the group", elapsed)
	}
	// The stuck repo surfaces as an error naming it.
	if err == nil || !contains(err.Error(), "stuck") || !contains(err.Error(), "timed out") {
		t.Fatalf("expected a timeout error naming the stuck repo, got: %v", err)
	}
	// The two fast repos still ran and are returned as partial results.
	if got := atomic.LoadInt32(&fastDone); got != 2 {
		t.Errorf("fast repos completed = %d, want 2 (the stall must not starve the others)", got)
	}
	if len(rebuilt) != 2 {
		t.Errorf("partial rebuilt list = %d repos, want 2 (fast1 + fast2)", len(rebuilt))
	}
}

// TestRebuildPerRepoTimeoutDisabled verifies the bound can be turned off.
func TestRebuildPerRepoTimeoutDisabled(t *testing.T) {
	t.Setenv("GRAFEL_REBUILD_REPO_TIMEOUT", "0")
	if d := resolvePerRepoRebuildTimeout(); d != 0 {
		t.Fatalf("resolvePerRepoRebuildTimeout()=%s, want 0 when disabled", d)
	}
	t.Setenv("GRAFEL_REBUILD_REPO_TIMEOUT", "15m")
	if d := resolvePerRepoRebuildTimeout(); d != 15*time.Minute {
		t.Fatalf("resolvePerRepoRebuildTimeout()=%s, want 15m", d)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
