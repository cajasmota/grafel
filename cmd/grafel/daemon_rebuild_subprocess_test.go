package main

// daemon_rebuild_subprocess_test.go — #5729 follow-up: the rebuild path routes
// per-repo indexing through the subprocess indexer when the toggle is ON, WHILE
// preserving the wizard's per-module progress bars, the status-before-ack flush,
// the group-level link pass, and the repolock claim.
//
// The child spawn (runRebuildSubprocess) is stubbed so the test does not exec a
// real binary (in a `go test` process os.Executable is the test binary, not
// grafel). The cross-process IPC fidelity itself is covered byte-faithfully by
// the sched package's round-trip test; here we assert the daemon-side WIRING:
// the reroute is taken, the child receives the right params, its republished
// progress reaches the split-mode sidecar bridge, and the parent still owns the
// completion/ack path.

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/progress"
)

// errChildIndex simulates a child index_error propagated by RunSubprocessIndex.
var errChildIndex = errors.New("index bad: child exit: extractor failed")

// failIfCalledIndexFn is an in-process indexFn that fails the test if invoked —
// the subprocess reroute must never fall through to it.
func failIfCalledIndexFn(t *testing.T) func(string, string, string, []string, bool, bool, ...IndexOption) error {
	return func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		t.Errorf("in-process indexFn must not be called on the subprocess reroute")
		return nil
	}
}

// stubChildSpawn replaces runRebuildSubprocess for the duration of a test,
// recording the params it was called with and simulating the child: it
// republishes two per-module progress events into the parent's publisher and
// writes a graph.fb so the status flush observes a fresh mtime. Restored on
// cleanup.
func stubChildSpawn(t *testing.T) *[]rebuildSubprocessParams {
	t.Helper()
	orig := runRebuildSubprocess
	t.Cleanup(func() { runRebuildSubprocess = orig })

	var mu sync.Mutex
	calls := &[]rebuildSubprocessParams{}
	runRebuildSubprocess = func(_ context.Context, p rebuildSubprocessParams) error {
		mu.Lock()
		*calls = append(*calls, p)
		mu.Unlock()
		// Simulate the child republishing per-module progress over stdout — the
		// parent handed us its real progressPub (broker / split-mode tee).
		if p.ProgressPub != nil {
			now := time.Now().UnixMilli()
			p.ProgressPub.Publish(progress.Event{
				GroupSlug: p.GroupSlug, RepoSlug: p.RepoSlug, Module: "mod-a",
				Phase: progress.PhaseExtractAST, FilesDone: 1, FilesTotal: 2, TS: now,
			})
			p.ProgressPub.Publish(progress.Event{
				GroupSlug: p.GroupSlug, RepoSlug: p.RepoSlug, Module: "mod-b",
				Phase: progress.PhaseExtractAST, FilesDone: 2, FilesTotal: 2, TS: now,
			})
		}
		// Simulate the child writing graph.fb so FlushRepoStatusFile sees a fresh
		// mtime, as a real index-internal child would.
		writeStubGraphFB(t, p.RepoPath)
		return nil
	}
	return calls
}

func writeStubGraphFB(t *testing.T, repoPath string) {
	t.Helper()
	sd := daemon.StateDirForRepo(repoPath)
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(sd+"/graph.fb", []byte("fb"), 0o644); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
}

// TestRebuild_SubprocessReroute_RepublishesProgressAndCompletes drives the whole
// rebuild through the subprocess path (toggle ON) in split mode and asserts:
//   - the child is invoked once per repo with the correct slugs + interactive flag
//   - the child's per-module progress reaches the split-mode sidecar bridge
//   - each repo's status is flushed fresh (Indexing=false) before return
//   - the group-level link pass runs exactly once
//   - the in-process indexFn is NEVER called (the reroute bypasses it)
func TestRebuild_SubprocessReroute_RepublishesProgressAndCompletes(t *testing.T) {
	t.Setenv(daemon.SplitModeEnvVar, "1") // split ON so the sidecar bridge exists
	group := setupTestGroup(t, "subproc-group", []string{"r1", "r2"})
	forceSubprocessRebuild(t) // override setupTestGroup's in-process pin

	calls := stubChildSpawn(t)

	var indexFnCalls int32
	inProcessIndexFn := func(_, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		atomic.AddInt32(&indexFnCalls, 1)
		return nil
	}
	var linkCalls int32
	linksFn := func(_ string) error { atomic.AddInt32(&linkCalls, 1); return nil }

	rebuilt, warning, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, Interactive: true}, inProcessIndexFn, linksFn)
	if err != nil {
		t.Fatalf("rebuild: %v (warning=%q)", err, warning)
	}
	if len(rebuilt) != 2 {
		t.Fatalf("rebuilt %d repos, want 2", len(rebuilt))
	}
	if got := atomic.LoadInt32(&indexFnCalls); got != 0 {
		t.Errorf("in-process indexFn called %d times; the subprocess reroute must bypass it", got)
	}
	if got := atomic.LoadInt32(&linkCalls); got != 1 {
		t.Errorf("linksFn ran %d times, want exactly 1 (group-level, after all repos)", got)
	}

	// Child invoked once per repo, with the rebuild's slugs + interactive flag and
	// a live publisher.
	if len(*calls) != 2 {
		t.Fatalf("child spawned %d times, want 2", len(*calls))
	}
	for _, c := range *calls {
		if c.GroupSlug != group {
			t.Errorf("child GroupSlug = %q, want %q", c.GroupSlug, group)
		}
		if c.RepoSlug != "r1" && c.RepoSlug != "r2" {
			t.Errorf("child RepoSlug = %q, want r1|r2", c.RepoSlug)
		}
		if !c.Interactive {
			t.Errorf("child Interactive = false; a human-awaited rebuild must forward the foreground cap")
		}
		if c.ProgressPub == nil {
			t.Errorf("child ProgressPub is nil; per-module bars would be lost")
		}
	}

	// The child's republished per-module progress must reach the split-mode
	// sidecar (the wizard's SSE bridge).
	r, err := progress.NewSidecarReader(group)
	if err != nil {
		t.Fatalf("sidecar reader: %v", err)
	}
	events, err := r.ReadAll()
	if err != nil {
		t.Fatalf("sidecar readall: %v", err)
	}
	sawModA, sawModB := false, false
	for _, e := range events {
		if e.Module == "mod-a" {
			sawModA = true
		}
		if e.Module == "mod-b" {
			sawModB = true
		}
	}
	if !sawModA || !sawModB {
		t.Errorf("per-module republished progress missing from sidecar (mod-a=%v mod-b=%v); events=%+v",
			sawModA, sawModB, events)
	}

	// Status flushed fresh before return (status-before-ack), for each repo.
	for _, rp := range rebuilt {
		f, ok := daemon.RepoStatusFile(rp)
		if !ok || f == nil {
			t.Fatalf("no status file flushed for %s (drain would ack before any status write)", rp)
		}
		if f.GraphFBMtime <= 0 {
			t.Errorf("%s: GraphFBMtime = %d, want fresh (>0) before return", rp, f.GraphFBMtime)
		}
		if f.Indexing {
			t.Errorf("%s: Indexing = true, want false after completion", rp)
		}
	}
}

// TestRebuild_SubprocessReroute_TimeoutCancelsChild is the leak guard: a child
// that would outlive the per-repo deadline must be cancelled (its ctx killed) so
// the parent goroutine unblocks and the repolock claim releases, instead of
// leaking a live subprocess with the parent stuck in Wait. The stub stands in
// for a wedged child that only returns when its ctx is cancelled — proving the
// reroute threads a cancellable, timeout-bounded ctx into the child (not
// context.Background()).
func TestRebuild_SubprocessReroute_TimeoutCancelsChild(t *testing.T) {
	group := setupTestGroup(t, "subproc-timeout-group", []string{"wedged"})
	forceSubprocessRebuild(t)
	t.Setenv("GRAFEL_REBUILD_REPO_TIMEOUT", "100ms") // tiny per-repo deadline

	orig := runRebuildSubprocess
	t.Cleanup(func() { runRebuildSubprocess = orig })

	childReturned := make(chan struct{})
	var sawCancel bool
	runRebuildSubprocess = func(ctx context.Context, _ rebuildSubprocessParams) error {
		<-ctx.Done() // a wedged child: only unblocks when the ctx is cancelled/killed
		sawCancel = ctx.Err() != nil
		close(childReturned)
		return ctx.Err()
	}

	// The rebuild must return promptly (bounded by the 100ms per-repo timeout),
	// never blocking on the wedged child.
	rebuildDone := make(chan error, 1)
	go func() {
		_, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group, Interactive: true}, failIfCalledIndexFn(t), noopLinksFn)
		rebuildDone <- err
	}()

	select {
	case err := <-rebuildDone:
		if err == nil || !contains(err.Error(), "wedged") || !contains(err.Error(), "timed out") {
			t.Fatalf("expected a timeout error naming the wedged repo, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rebuild did not return — the wedged child was not cancelled on timeout (leak)")
	}

	// The child goroutine must have been released via ctx cancellation (not left
	// leaked): its ctx was cancelled and it returned.
	select {
	case <-childReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("child goroutine never returned — ctx was not cancelled (process/claim leak)")
	}
	if !sawCancel {
		t.Error("child observed ctx.Err()==nil; the reroute must pass a cancellable ctx, not context.Background()")
	}
}

// TestRebuild_SubprocessReroute_ChildErrorIsPerRepoFailure verifies a child
// index_error surfaces as that repo's failure with the same partial-failure
// semantics as an in-process error: the group returns an error naming the repo,
// the healthy repo is still rebuilt, and successful repos' status is flushed.
func TestRebuild_SubprocessReroute_ChildErrorIsPerRepoFailure(t *testing.T) {
	group := setupTestGroup(t, "subproc-fail-group", []string{"good", "bad"})
	forceSubprocessRebuild(t)

	orig := runRebuildSubprocess
	t.Cleanup(func() { runRebuildSubprocess = orig })
	runRebuildSubprocess = func(_ context.Context, p rebuildSubprocessParams) error {
		if p.RepoSlug == "bad" {
			return errChildIndex
		}
		writeStubGraphFB(t, p.RepoPath)
		return nil
	}

	rebuilt, _, err := daemonRebuildFuncCore(
		1, proto.RebuildArgs{Group: group, Interactive: true}, failIfCalledIndexFn(t), noopLinksFn)
	if err == nil {
		t.Fatal("expected an error because one child failed, got nil")
	}
	if !contains(err.Error(), "bad") {
		t.Errorf("error %q should name the failed repo 'bad'", err.Error())
	}
	if len(rebuilt) != 1 || rebuilt[0] == "" {
		t.Fatalf("partial rebuilt = %v, want exactly the healthy repo", rebuilt)
	}
	// The healthy repo's status must still be flushed on the error-return path.
	f, ok := daemon.RepoStatusFile(rebuilt[0])
	if !ok || f == nil || f.GraphFBMtime <= 0 {
		t.Errorf("healthy repo status not flushed before the (partial-failure) return")
	}
}
