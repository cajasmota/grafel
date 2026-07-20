package main

// daemon_rebuild_cancel_marker_test.go — review follow-up to #5822 sub-ask 3:
// the "last rebuild FAILED" marker must represent a GENUINE failure, not an
// intentional cancellation (group deleted mid-rebuild, daemon shutdown, or a
// rebuild superseded by a newer ref). Markers key purely by absolute
// repoPath, so recording one on an intentional cancellation risks a bogus
// FAILED line surfacing under a DIFFERENT, still-alive group that happens to
// share the same on-disk repo path.
//
// This guards against over-skipping too: a genuine per-repo watchdog timeout
// must still record the marker (see
// TestRebuildWatchdogFailure_PersistedAndSurfacedInStatus in
// daemon_rebuild_watchdog_visibility_test.go), since context.DeadlineExceeded
// must never be conflated with an intentional context.Canceled.

import (
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// assertNoFailureMarker fails the test if repoPath carries a persisted
// LastRebuildFailure. A missing status sidecar counts as "no marker" — the
// suppressed-cancellation path never calls RecordRebuildFailure, so it never
// creates/flushes the sidecar in the first place.
func assertNoFailureMarker(t *testing.T, label, repoPath string) {
	t.Helper()
	sf, err := statusfile.Read(repoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return // no sidecar written at all → certainly no marker
		}
		t.Fatalf("statusfile.Read(%s): %v", label, err)
	}
	if sf.LastRebuildFailure != nil {
		t.Errorf("%s: LastRebuildFailure = %+v, want nil — an intentional group-deleted cancellation must not be recorded as a failure",
			label, sf.LastRebuildFailure)
	}
}

// TestRebuild_GroupDeletedMidRebuild_DoesNotRecordFailureMarker is the
// regression RED test: simulate `grafel delete <group>` racing an in-flight
// rebuild by cancelling the group from inside the FIRST repo's indexFn. The
// other repo's indexOne then observes groupCtx.Err() != nil and short-circuits
// with the "rebuild cancelled (group deleted)" error (wrapping context.Canceled)
// WITHOUT ever calling indexFn. That intentional cancellation must NOT persist
// a "last rebuild FAILED" marker.
//
// Determinism: concurrency=1 means the pool's single semaphore slot serialises
// the two indexOne invocations (rebuildWorkerPool holds the slot across the
// whole workFn), so exactly one repo runs its indexFn at a time. We cancel on
// the FIRST indexFn call regardless of slug (sync.Once) rather than pinning it
// to a specific slug: which goroutine wins the slot is NOT FIFO-guaranteed, so
// the old "cancel only when slug==first" keying was flaky (~50%) whenever
// "second" acquired the slot first. Cancelling on whichever repo runs first
// guarantees the other repo's top-of-indexOne groupCtx.Err() check (which runs
// strictly after the first repo released the slot) sees the cancellation and
// short-circuits — exactly one indexFn call, deterministically.
func TestRebuild_GroupDeletedMidRebuild_DoesNotRecordFailureMarker(t *testing.T) {
	group := setupTestGroup(t, "cancel-marker-group", []string{"first", "second"})

	var calls int32
	var cancelOnce sync.Once
	mockIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		atomic.AddInt32(&calls, 1)
		// Cancel groupCtx on the first repo to actually run, so the other
		// repo's indexOne short-circuits before it ever runs its indexFn.
		cancelOnce.Do(func() { daemon.CancelGroupRebuild(group) })
		return nil
	}

	_, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn)
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected a 'rebuild cancelled' error for the short-circuited repo, got: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("indexFn called %d times; want 1 (only the first repo runs; the other must be short-circuited)", got)
	}

	// Neither repo may carry a failure marker: the one that ran succeeded, and
	// the one that was cancelled is an intentional stop, not a failure. Check
	// BOTH because which slug won the semaphore is nondeterministic.
	assertNoFailureMarker(t, "first", repoPathForSlug(t, group, "first"))
	assertNoFailureMarker(t, "second", repoPathForSlug(t, group, "second"))
}
