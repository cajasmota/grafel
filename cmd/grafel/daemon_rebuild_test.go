package main

// daemon_rebuild_test.go — regression tests for #2097: Rebuild RPC wedge.
//
// Tests:
//  1. A panicking index callback releases the semaphore and does not block
//     subsequent repos from completing.
//  2. Five sequential Rebuild RPCs all complete even when one errors.
//  3. Four concurrent Rebuild RPCs for the SAME group all return, contending
//     the process-global state around the rebuild on the way. This entry used
//     to say they are "serialised (the per-group mutex added in #2097 prevents
//     them from racing)"; there is no per-group mutex at this layer —
//     daemonRebuildFuncCore holds none, as the test's own doc comment already
//     said — and what the concurrent calls actually do to one another is
//     CANCEL each other through daemon.GroupRebuildContext's per-group-NAME
//     entry. Service-layer serialisation is covered elsewhere, by
//     TestServiceRebuildGroupSerialisedUnderLoad.

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

	// rpcHangGuard bounds ONE Rebuild RPC. It is a hang guard, not a
	// performance assertion: the substantive claim of this test is that every
	// RPC completes, so the only thing a larger constant can weaken is how
	// long the suite takes to report a genuine wedge. It was 5s and went red
	// on `test (windows-latest)` as "Rebuild RPC 3 hung after 5s" with
	// ubuntu-latest green on the same run, across unrelated PRs (#7298).
	//
	// 30s is a CHOSEN budget, not a derived bound, and the distinction is
	// load-bearing because nothing here measures the Windows cost. What this
	// file does record — on TestRebuildPerRepoTimeoutSurfacesStalledRepo
	// below — is a per-repo ceiling arrived at empirically: "a hardcoded
	// 100ms flaked; 1.5s was still marginal under `-race`; 3s gives the fast
	// mocks ~200× their real cost of headroom". Read literally, that sentence
	// puts the MEASURED per-repo bookkeeping cost (status-file flush,
	// foreground claim, gate acquire) at roughly 3s/200 ≈ 15ms, and puts 3s
	// at a ceiling someone chose well above it. So 2 × 3s is not a cost bound
	// either; 3s is reused here only as the largest per-repo figure this file
	// has ever had to pay for, taken twice for the 2 repos one RPC indexes at
	// concurrency=1 (6s), then multiplied by 5.
	//
	// What made the old 5s go red is NOT established: this change did not
	// profile the failing runs, and "5s < 6s" is arithmetic relating two
	// chosen numbers, not a cause. The only claim being made is the weak one
	// a hang guard needs — 30s sits far enough above any per-repo cost this
	// file has ever recorded that a red result is far more likely to be a
	// real wedge than a slow runner, and over-sizing it costs nothing but the
	// time taken to report that wedge.
	//
	// The RUNNER REGIME is deliberately left unqualified rather than
	// asserted. The quoted figures come from a `-race` context, but
	// .github/workflows/test.yml runs the automatic `pull_request`
	// windows-latest leg WITHOUT `-race` (the `-race` branches of its Test
	// step match tag pushes, workflow_dispatch and the `ci:full` label only),
	// and which regime the #7298 census failures ran under has not been
	// checked. The budget is sized for the slower of the two.
	const rpcHangGuard = 30 * time.Second

	for i := 0; i < 5; i++ {
		done := make(chan struct{})
		go func() {
			defer close(done)
			daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn) //nolint:errcheck
		}()
		select {
		case <-done:
			// OK — completed
		case <-time.After(rpcHangGuard):
			t.Fatalf("Rebuild RPC %d hung after %s", i+1, rpcHangGuard)
		}
	}
}

// TestRebuildSemaphoreCapRespected checks the semaphore behaviour WITHIN a
// single daemonRebuildFuncCore call: with a pool size of 2 and 4 repos, peak
// in-flight index calls must be exactly 2.
//
// (This doc block was titled TestRebuildConcurrentGroupsMutex — a test that
// does not exist in this file — and opened on concurrent goroutines and a
// per-group mutex, neither of which this test has. Corrected with the same
// stale title one function below, #7298; no assertion changed.)
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

// TestRebuildConcurrentSameGroupCallsContendGlobalRegistries fires four
// concurrent daemonRebuildFuncCore calls at the SAME group and asserts what
// that contention is actually allowed to produce.
//
// RENAMED, and the rename grades nothing new (#7298). The old name —
// TestRebuildResultsSliceNotRacedOnConcurrentCalls — claimed a property this
// test cannot falsify: there is no results slice shared between calls.
// `results` is local to rebuildWorkerPool (cmd/grafel/daemon.go:1885) and
// written at distinct indices; `rebuilt` is function-local, declared at
// daemon.go:2378 and appended by one sequential loop; and at concurrency=1
// there is not even intra-call concurrency to race. The doc comment that used
// to sit here was additionally titled after a different test entirely
// (TestRebuildConcurrentGroupsMutex) and asserted a per-group mutex its own
// next sentence denied. The rename and this rewrite fix what the test SAYS;
// the assertions below are what it grades, and they are unchanged by it.
//
// What the four calls genuinely share is the process-global state around a
// rebuild: daemon.GroupRebuildContext's per-group-name registry,
// repolock.DefaultRegistry, the daemon index gate, and the per-repo status
// files. Under `-race` (tag / dispatch / `ci:full` legs only — the automatic
// PR gate runs without it) that contention is also checked by the detector.
//
// The assertions are: (a) all four calls return inside concurrentHangGuard;
// (b) at least one call is not cancelled by a sibling and returns both repos;
// (c) every error that IS returned has the sibling-cancellation shape.
//
// (Service-layer per-group serialisation is covered by
// TestServiceRebuildGroupSerialisedUnderLoad.)
func TestRebuildConcurrentSameGroupCallsContendGlobalRegistries(t *testing.T) {
	// A group with 2 repos, rebuilt concurrently 4 times.
	group := setupTestGroup(t, "results-race-group", []string{"p", "q"})

	mockIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		time.Sleep(5 * time.Millisecond)
		return nil
	}

	const concurrentCalls = 4

	// #7298, second defect — vacuity, independent of any deadline. This
	// goroutine used to `return` on a non-nil error under a bare "errors are
	// acceptable", which meant a run in which ALL FOUR calls errored executed
	// no assertion whatsoever and still passed — green on "everything
	// failed".
	// Record the outcome of every call instead, and require below that at
	// least one of them actually reached the rebuilt-length check.
	var succeeded int32
	var errMu sync.Mutex
	var callErrs []error

	var wg sync.WaitGroup
	for i := 0; i < concurrentCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rebuilt, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn)
			if err != nil {
				// A per-call error is recorded and tolerated rather than
				// failed outright, and that tolerance is not a hedge: with
				// this mock the observed outcome is 3 of the 4 calls returning
				// "index p: rebuild cancelled (group deleted): context
				// cancelled". daemon.GroupRebuildContext registers ONE
				// cancellable entry per GROUP NAME and cancels whatever
				// predecessor it finds under that name, so four concurrent
				// rebuilds of one group necessarily cancel each other. Failing
				// on any error would therefore make this test permanently red
				// rather than stricter.
				//
				// That same mechanism is why the floor below is exactly one
				// and not four: registrations are serialised under the
				// registry mutex and each registrant cancels only its
				// predecessor, so the LAST registrant is never cancelled by a
				// sibling — at least one call must get through.
				errMu.Lock()
				callErrs = append(callErrs, err)
				errMu.Unlock()
				return
			}
			atomic.AddInt32(&succeeded, 1)
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

	// concurrentHangGuard is the same kind of chosen budget as rpcHangGuard in
	// TestRebuildFiveSequentialAlwaysComplete above — see that comment for why
	// the 3s per-repo figure is a chosen ceiling rather than a measured cost,
	// and for why the runner regime is left unqualified. It was 10s and went
	// red on `test (windows-latest)` as "concurrent Rebuild RPCs hung after
	// 10s" with ubuntu-latest green on the same run (#7298).
	//
	// The per-repo work here is far SMALLER than the call count suggests, and
	// sizing this off 4 calls × 2 repos = 8 bookkeeping units would be a false
	// statement about what runs. Three of the four calls are cancelled by a
	// successor before they index anything at all: indexOne short-circuits on
	// `groupCtx.Err() != nil` (cmd/grafel/daemon.go:2322) and returns a
	// cancelled result WITHOUT reaching ClaimForeground or the index closure,
	// which is why a cancelled call emits no per-repo "rebuild <slug> took"
	// line. Only the surviving call does per-repo work — 2 units back-to-back
	// at concurrency=1, i.e. exactly the shape of one RPC above, so the same
	// 2 × 3s = 6s figure.
	//
	// 60s is then 2× the 30s that figure bought above, because this guard
	// covers all FOUR calls draining rather than one: on top of the survivor's
	// 2 units, the three cancelled calls still pay their non-indexing overhead
	// (context registration, the foreground barge and group mark, progress
	// sidecar setup and teardown), which nothing here measures.
	const concurrentHangGuard = 60 * time.Second

	select {
	case <-done:
	case <-time.After(concurrentHangGuard):
		t.Fatalf("concurrent Rebuild RPCs hung after %s", concurrentHangGuard)
	}

	errMu.Lock()
	errs := append([]error(nil), callErrs...)
	errMu.Unlock()
	if n := atomic.LoadInt32(&succeeded); n == 0 {
		t.Fatalf("all %d concurrent Rebuild calls errored, so the rebuilt-length check never ran and this test asserted nothing; errors: %v",
			concurrentCalls, errs)
	}

	// (c) The floor above only COUNTS the failures; this checks what they
	// failed WITH. Sibling cancellation is the one failure these fixtures can
	// legitimately produce — the mock returns nil, the group exists, and its
	// config loads — so without this loop a regression in which three calls
	// start failing with "unknown group", a gate error or a panic-recovered
	// index error, while one still succeeds, is entirely invisible here.
	//
	// The discriminator is the message TEXT and not errors.Is, because
	// daemonRebuildFuncCore flattens its per-repo failures with
	// fmt.Errorf("%s", strings.Join(errs, "; ")) at cmd/grafel/daemon.go:2458.
	// That does not wrap, so the context.Canceled carried by "rebuild
	// cancelled (group deleted): %w" (daemon.go:2326) does not survive to the
	// caller and errors.Is(err, context.Canceled) would be false for every
	// error this test can legitimately see.
	const cancelShape = "rebuild cancelled (group deleted)"
	for _, e := range errs {
		if !strings.Contains(e.Error(), cancelShape) {
			t.Errorf("concurrent Rebuild call failed with an unexpected error shape: %v; want a message containing %q", e, cancelShape)
			continue
		}
		t.Logf("concurrent Rebuild call returned the expected sibling-cancellation error: %v", e)
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
