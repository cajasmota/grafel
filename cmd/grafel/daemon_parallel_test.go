package main

import (
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/registry"
)

// TestDaemonRebuildParallel verifies that daemonRebuildFuncCore respects
// the concurrency parameter: with concurrency=2 and 4 repos that each sleep
// briefly, the parallel run should complete in meaningfully less wall
// time than the serial run, and peak observed concurrency should be ≥2.
func TestDaemonRebuildParallel(t *testing.T) {
	if testing.Short() {
		t.Skip("parallel-rebuild timing test skipped in short mode")
	}
	forceInProcessRebuild(t)

	// Build a temporary registry with a synthetic group containing 4 repos.
	tmpHome := t.TempDir()
	t.Setenv("GRAFEL_HOME", tmpHome)

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

	// perRepoSleep is bumped to 200ms (was 60ms): the index sleep must dominate
	// the fixed per-rebuild overhead so the serial/parallel RATIO reflects real
	// parallelism, not setup cost. daemonRebuildFuncCore does per-repo status
	// flushes + cache invalidations TWICE now — once at the "graph queryable"
	// point before linksFn (the wizard's early-completion signal) and once after
	// linksFn — which is real serial work that grew the fixed overhead; a
	// too-short sleep let that overhead swamp the ratio (peakConc, the actual
	// parallelism check, is unaffected).
	const perRepoSleep = 200 * time.Millisecond

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
		time.Sleep(perRepoSleep)
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
	// The wall-clock speedup ratio is fundamentally unassertable on shared CI
	// runners: GitHub's hosted runners (including windows-latest, which reports
	// 4 cores) share CPU/IO with other tenants and suffer AV-scanning and slow
	// disk, so two 60ms sleeps can make the "parallel" run SLOWER than serial
	// (observed speedup=0.46×) even though real parallelism happened (peak≥2,
	// asserted above and valid everywhere). A core-count gate doesn't help —
	// the 4-core runner clears it yet still fails. Gate the ratio assertion to
	// non-CI hosts; GitHub Actions always sets CI=true. The parallelism itself
	// is still covered everywhere via the peakConc check (#4285).
	//
	// The same contention corrupts the ratio on a heavily-loaded LOCAL dev host
	// (observed load-average ~10), not just CI. Detect that generically: with 4
	// repos and concurrency 1/2 the wall-clock FLOOR is 4×sleep serial and 2×sleep
	// parallel; when EITHER run overruns 2× its floor the host is CPU-starved and
	// the ratio is noise (a starved parallel run can even finish slower than
	// serial). Skip the ratio there too — parallelism itself is still proven by
	// peakConc≥2 above, which holds on every host.
	serialFloor := time.Duration(len(repos)) * perRepoSleep
	parallelFloor := time.Duration((len(repos)+1)/2) * perRepoSleep
	contended := serialDur > 2*serialFloor || parallelDur > 2*parallelFloor
	switch {
	case os.Getenv("CI") != "":
		t.Logf("skipping speedup-ratio assertion under CI (CI=%s, #4285); "+
			"parallelism still verified via peakConc=%d (%d-core host)",
			os.Getenv("CI"), peak, runtime.NumCPU())
	case contended:
		t.Logf("skipping speedup-ratio assertion on a CPU-contended host "+
			"(serial=%s floor=%s, parallel=%s floor=%s); parallelism still "+
			"verified via peakConc=%d (%d-core host)",
			serialDur.Truncate(time.Millisecond), serialFloor,
			parallelDur.Truncate(time.Millisecond), parallelFloor,
			peak, runtime.NumCPU())
	case speedup < 1.3:
		t.Errorf("parallel speedup = %.2f×, want ≥1.3× vs serial", speedup)
	}
}

// TestDaemonRebuildSerial verifies the serial path (concurrency=1)
// indexes all repos without error and produces one result per repo.
func TestDaemonRebuildSerial(t *testing.T) {
	forceInProcessRebuild(t)
	tmpHome := t.TempDir()
	t.Setenv("GRAFEL_HOME", tmpHome)

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
	forceInProcessRebuild(t)
	tmpHome := t.TempDir()
	t.Setenv("GRAFEL_HOME", tmpHome)

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
