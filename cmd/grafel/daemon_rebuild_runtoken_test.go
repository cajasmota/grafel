package main

// daemon_rebuild_runtoken_test.go — #5937 chunk 1: RebuildArgs.ProgressToken
// must thread all the way into every progress.Event this rebuild emits, on
// BOTH the in-process indexFn path and the subprocess (`grafel index-internal`
// child) reroute, so a subscriber can key events to THIS run rather than a
// stale prior run.

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// TestRebuild_InProcess_ForwardsProgressToken verifies that args.ProgressToken
// reaches the in-process indexFn's IndexOption list as WithRunToken, so the
// Indexer's Tracker stamps RunToken on every event it emits.
func TestRebuild_InProcess_ForwardsProgressToken(t *testing.T) {
	group := setupTestGroup(t, "runtoken-inprocess-group", []string{"r1"})

	var gotRunToken string
	var calls int32
	inProcessIndexFn := func(_, _, _ string, _ []string, _, _ bool, opts ...IndexOption) error {
		atomic.AddInt32(&calls, 1)
		var i Indexer
		for _, opt := range opts {
			opt(&i)
		}
		gotRunToken = i.runToken
		return nil
	}
	linksFn := func(_ context.Context, _ string) error { return nil }

	const tok = "wizard-run-tok-1"
	_, warning, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, ProgressToken: tok}, inProcessIndexFn, linksFn)
	if err != nil {
		t.Fatalf("rebuild: %v (warning=%q)", err, warning)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("indexFn called %d times, want 1", calls)
	}
	if gotRunToken != tok {
		t.Errorf("Indexer.runToken = %q, want %q", gotRunToken, tok)
	}
}

// TestRebuild_InProcess_EmptyProgressTokenUnaffected verifies a rebuild with no
// ProgressToken (the background/watcher case) leaves the Indexer's runToken
// empty — additive only, no behaviour change for untokened runs.
func TestRebuild_InProcess_EmptyProgressTokenUnaffected(t *testing.T) {
	group := setupTestGroup(t, "runtoken-inprocess-empty-group", []string{"r1"})

	var gotRunToken string
	sawRunToken := false
	inProcessIndexFn := func(_, _, _ string, _ []string, _, _ bool, opts ...IndexOption) error {
		var i Indexer
		for _, opt := range opts {
			opt(&i)
		}
		gotRunToken = i.runToken
		sawRunToken = true
		return nil
	}
	linksFn := func(_ context.Context, _ string) error { return nil }

	_, warning, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group}, inProcessIndexFn, linksFn)
	if err != nil {
		t.Fatalf("rebuild: %v (warning=%q)", err, warning)
	}
	if !sawRunToken {
		t.Fatal("indexFn was never called")
	}
	if gotRunToken != "" {
		t.Errorf("Indexer.runToken = %q, want empty for a rebuild with no ProgressToken", gotRunToken)
	}
}

// TestRebuild_SubprocessReroute_ForwardsProgressToken verifies that
// args.ProgressToken reaches rebuildSubprocessParams.RunToken (and from there
// SubprocessIndexOptions.RunToken / the child's --run-token flag — covered
// byte-faithfully by the sched package's own round-trip coverage) on the
// subprocess reroute.
func TestRebuild_SubprocessReroute_ForwardsProgressToken(t *testing.T) {
	group := setupTestGroup(t, "runtoken-subproc-group", []string{"r1"})
	forceSubprocessRebuild(t)

	calls := stubChildSpawn(t)

	inProcessIndexFn := failIfCalledIndexFn(t)
	linksFn := func(_ context.Context, _ string) error { return nil }

	const tok = "wizard-run-tok-2"
	_, warning, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, ProgressToken: tok}, inProcessIndexFn, linksFn)
	if err != nil {
		t.Fatalf("rebuild: %v (warning=%q)", err, warning)
	}
	if len(*calls) != 1 {
		t.Fatalf("child spawned %d times, want 1", len(*calls))
	}
	if got := (*calls)[0].RunToken; got != tok {
		t.Errorf("rebuildSubprocessParams.RunToken = %q, want %q", got, tok)
	}
}
