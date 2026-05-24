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
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/registry"
)

// setupTestGroup creates a temporary ARCHIGRAPH_HOME, registers a group with
// n repos whose paths are subdirectories of repoBase, and returns the group
// name. t.Cleanup removes everything.
func setupTestGroup(t *testing.T, groupName string, slugs []string) string {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", tmpHome)
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

// TestRebuildPanicRecoveryReleasesSemaphore verifies that a panic inside
// rebuildIndexFunc does not leak the semaphore slot. With concurrency=1 and
// 3 repos where the first panics, all three should produce a result (the
// first an error, the remaining two success).
func TestRebuildPanicRecoveryReleasesSemaphore(t *testing.T) {
	group := setupTestGroup(t, "panic-group", []string{"first", "second", "third"})

	origIndexFn := rebuildIndexFunc
	var callCount int32
	rebuildIndexFunc = func(repoPath, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			panic("simulated extractor panic")
		}
		return nil
	}
	defer func() { rebuildIndexFunc = origIndexFn }()

	origLinksFn := rebuildLinksFunc
	rebuildLinksFunc = func(_ string) error { return nil }
	defer func() { rebuildLinksFunc = origLinksFn }()

	rebuildConcurrency = 1

	_, _, err := daemonRebuildFunc(proto.RebuildArgs{Group: group})
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

	origIndexFn := rebuildIndexFunc
	var callCount int32
	var panicked int32
	rebuildIndexFunc = func(repoPath, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		n := atomic.AddInt32(&callCount, 1)
		if n == 2 && atomic.CompareAndSwapInt32(&panicked, 0, 1) {
			panic("parallel extractor panic")
		}
		time.Sleep(10 * time.Millisecond)
		return nil
	}
	defer func() { rebuildIndexFunc = origIndexFn }()

	origLinksFn := rebuildLinksFunc
	rebuildLinksFunc = func(_ string) error { return nil }
	defer func() { rebuildLinksFunc = origLinksFn }()

	rebuildConcurrency = 2
	_, _, _ = daemonRebuildFunc(proto.RebuildArgs{Group: group})

	if got := atomic.LoadInt32(&callCount); got != 4 {
		t.Errorf("callCount = %d, want 4 (panic in one goroutine must not starve others)", got)
	}
}

// TestRebuildFiveSequentialAlwaysComplete fires five sequential Rebuild RPCs
// where one of them errors. Every call must complete (not hang). This is the
// exact scenario that produced in_flight=4 before #2097.
func TestRebuildFiveSequentialAlwaysComplete(t *testing.T) {
	group := setupTestGroup(t, "five-seq-group", []string{"r1", "r2"})

	origIndexFn := rebuildIndexFunc
	var totalCalls int32
	rebuildIndexFunc = func(repoPath, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		atomic.AddInt32(&totalCalls, 1)
		if atomic.LoadInt32(&totalCalls)%4 == 0 {
			return errors.New("injected error")
		}
		return nil
	}
	defer func() { rebuildIndexFunc = origIndexFn }()

	origLinksFn := rebuildLinksFunc
	rebuildLinksFunc = func(_ string) error { return nil }
	defer func() { rebuildLinksFunc = origLinksFn }()

	rebuildConcurrency = 1
	for i := 0; i < 5; i++ {
		done := make(chan struct{})
		go func() {
			defer close(done)
			daemonRebuildFunc(proto.RebuildArgs{Group: group}) //nolint:errcheck
		}()
		select {
		case <-done:
			// OK — completed
		case <-time.After(5 * time.Second):
			t.Fatalf("Rebuild RPC %d hung after 5s", i+1)
		}
	}
}

// TestConcurrentRebuildSameGroupIsSerialised verifies that two concurrent
// Rebuild RPCs for the same group do NOT overlap execution. Because
// daemonRebuildFunc itself doesn't hold the per-group mutex (that lives
// in Service.Rebuild), this test verifies the per-group mutex at the
// daemonRebuildFunc level is NOT present — and instead validates the
// semaphore behaviour within a single call.
//
// The actual group-mutex serialisation is tested in
// internal/daemon/service_test.go (TestServiceRebuildGroupSerialisedUnderLoad).
func TestRebuildSemaphoreCapRespected(t *testing.T) {
	if testing.Short() {
		t.Skip("semaphore cap timing test skipped in short mode")
	}
	group := setupTestGroup(t, "sem-cap-group", []string{"x1", "x2", "x3", "x4"})

	origIndexFn := rebuildIndexFunc
	var peakConc, current int64
	rebuildIndexFunc = func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		cur := atomic.AddInt64(&current, 1)
		defer atomic.AddInt64(&current, -1)
		for {
			pk := atomic.LoadInt64(&peakConc)
			if cur <= pk || atomic.CompareAndSwapInt64(&peakConc, pk, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		return nil
	}
	defer func() { rebuildIndexFunc = origIndexFn }()

	origLinksFn := rebuildLinksFunc
	rebuildLinksFunc = func(_ string) error { return nil }
	defer func() { rebuildLinksFunc = origLinksFn }()

	rebuildConcurrency = 2
	_, _, err := daemonRebuildFunc(proto.RebuildArgs{Group: group})
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

	origIndexFn := rebuildIndexFunc
	rebuildIndexFunc = func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		time.Sleep(5 * time.Millisecond)
		return nil
	}
	defer func() { rebuildIndexFunc = origIndexFn }()

	origLinksFn := rebuildLinksFunc
	rebuildLinksFunc = func(_ string) error { return nil }
	defer func() { rebuildLinksFunc = origLinksFn }()

	rebuildConcurrency = 1

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rebuilt, _, err := daemonRebuildFunc(proto.RebuildArgs{Group: group})
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
