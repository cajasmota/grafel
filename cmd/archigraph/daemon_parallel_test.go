package main

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/registry"
)

// TestDaemonRebuildParallel verifies that daemonRebuildFuncCore respects
// the concurrency parameter: with concurrency=2 and 4 repos that each sleep
// briefly, the parallel run should complete in meaningfully less wall
// time than the serial run, and peak observed concurrency should be ≥2.
func TestDaemonRebuildParallel(t *testing.T) {
	if testing.Short() {
		t.Skip("parallel-rebuild timing test skipped in short mode")
	}

	// Build a temporary registry with a synthetic group containing 4 repos.
	tmpHome := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", tmpHome)

	repoBase := t.TempDir()
	var repos []registry.Repo
	for _, name := range []string{"alpha", "beta", "gamma", "delta"} {
		p := repoBase + "/" + name
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		repos = append(repos, registry.Repo{Slug: name, Path: p})
	}

	cfgPath := tmpHome + "/test-group.fleet.json"
	cfg := &registry.GroupConfig{
		Name:  "test-group",
		Repos: repos,
	}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup("test-group", cfgPath); err != nil {
		t.Fatal(err)
	}

	// Track peak concurrency across all stub Index calls.
	var currentConc, peakConc int64

	// mockIndexFn sleeps and records concurrency.
	mockIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		cur := atomic.AddInt64(&currentConc, 1)
		defer atomic.AddInt64(&currentConc, -1)
		for {
			pk := atomic.LoadInt64(&peakConc)
			if cur <= pk || atomic.CompareAndSwapInt64(&peakConc, pk, cur) {
				break
			}
		}
		time.Sleep(60 * time.Millisecond)
		return nil
	}
	mockLinksFn := func(_ string) error { return nil }

	// --- Serial run (concurrency=1) ---
	t0 := time.Now()
	rebuilt, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: "test-group"}, mockIndexFn, mockLinksFn)
	serialDur := time.Since(t0)
	if err != nil {
		t.Fatalf("serial rebuild: %v", err)
	}
	if len(rebuilt) != 4 {
		t.Fatalf("serial: want 4 rebuilt repos, got %d", len(rebuilt))
	}

	// Reset counters.
	atomic.StoreInt64(&peakConc, 0)
	atomic.StoreInt64(&currentConc, 0)

	// --- Parallel run (concurrency=2) ---
	t1 := time.Now()
	rebuilt2, _, err2 := daemonRebuildFuncCore(2, proto.RebuildArgs{Group: "test-group"}, mockIndexFn, mockLinksFn)
	parallelDur := time.Since(t1)
	if err2 != nil {
		t.Fatalf("parallel rebuild: %v", err2)
	}
	if len(rebuilt2) != 4 {
		t.Fatalf("parallel: want 4 rebuilt repos, got %d", len(rebuilt2))
	}

	peak := atomic.LoadInt64(&peakConc)
	speedup := float64(serialDur) / float64(parallelDur)
	t.Logf("serial=%s parallel=%s speedup=%.2fx peakConc=%d",
		serialDur.Truncate(time.Millisecond),
		parallelDur.Truncate(time.Millisecond),
		speedup, peak)

	if peak < 2 {
		t.Errorf("peak concurrency = %d, want ≥2", peak)
	}
	if speedup < 1.3 {
		t.Errorf("parallel speedup = %.2f×, want ≥1.3× vs serial", speedup)
	}
}

// TestDaemonRebuildSerial verifies the serial path (concurrency=1)
// indexes all repos without error and produces one result per repo.
func TestDaemonRebuildSerial(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", tmpHome)

	repoBase := t.TempDir()
	var repos []registry.Repo
	for _, name := range []string{"repo-a", "repo-b"} {
		p := repoBase + "/" + name
		_ = os.MkdirAll(p, 0o755)
		repos = append(repos, registry.Repo{Slug: name, Path: p})
	}

	cfgPath := tmpHome + "/serial-group.fleet.json"
	cfg := &registry.GroupConfig{Name: "serial-group", Repos: repos}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup("serial-group", cfgPath); err != nil {
		t.Fatal(err)
	}

	mockIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error { return nil }
	mockLinksFn := func(_ string) error { return nil }

	rebuilt, warning, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: "serial-group"}, mockIndexFn, mockLinksFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if warning != "" {
		t.Errorf("unexpected warning: %s", warning)
	}
	if len(rebuilt) != 2 {
		t.Errorf("got %d rebuilt repos, want 2", len(rebuilt))
	}
}

// TestDaemonRebuildFailureIsolation confirms that a failing repo does not
// prevent other repos in the group from completing.
func TestDaemonRebuildFailureIsolation(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", tmpHome)

	repoBase := t.TempDir()
	var repos []registry.Repo
	for _, name := range []string{"ok-repo", "bad-repo", "also-ok"} {
		p := repoBase + "/" + name
		_ = os.MkdirAll(p, 0o755)
		repos = append(repos, registry.Repo{Slug: name, Path: p})
	}

	cfgPath := tmpHome + "/mixed-group.fleet.json"
	cfg := &registry.GroupConfig{Name: "mixed-group", Repos: repos}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup("mixed-group", cfgPath); err != nil {
		t.Fatal(err)
	}

	mockIndexFn := func(repoPath, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		if repoPath == repoBase+"/bad-repo" {
			return os.ErrPermission
		}
		return nil
	}
	mockLinksFn := func(_ string) error { return nil }

	// Both serial and parallel should return partial results + an error.
	for _, conc := range []int{1, 2} {
		rebuilt, _, err := daemonRebuildFuncCore(conc, proto.RebuildArgs{Group: "mixed-group"}, mockIndexFn, mockLinksFn)
		if err == nil {
			t.Errorf("conc=%d: expected error for bad-repo, got nil", conc)
		}
		if len(rebuilt) != 2 {
			t.Errorf("conc=%d: got %d rebuilt repos, want 2 (ok ones)", conc, len(rebuilt))
		}
	}
}
