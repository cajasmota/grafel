package daemon

// Mutual-exclusion regression for ADR-0024 PR4 (epic #5729): Service.Index's
// async fast path must use EXACTLY ONE of the two reindex-trigger paths —
// never both, and never neither — depending on GRAFEL_SPLIT_MODE:
//
//   - flag OFF (monolith, the default): calls s.scheduler.Enqueue directly,
//     in-process, exactly as before this PR. No requests/ file is written.
//   - flag ON (split mode): writes a KindReindex request file instead of
//     touching the scheduler at all (the scheduler is not even consulted —
//     serve has no scheduler in real split-mode deployments; the field being
//     non-nil here is only because the test harness wires one to prove it is
//     NOT called).

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/requests"
	"github.com/cajasmota/grafel/internal/daemon/sched"
)

func TestIndexAsync_SplitModeOff_UsesSchedulerDirectly_NoRequestFile(t *testing.T) {
	t.Setenv(SplitModeEnvVar, "")
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	repoPath := t.TempDir()
	enqueued := make(chan string, 1)
	svc, cleanup := newAsyncTestService(t, func(args proto.IndexArgs) (string, string, error) {
		t.Fatal("synchronous index entrypoint must not run on the async fast path")
		return "", "", nil
	}, func(ctx context.Context, repo, ref string) error {
		enqueued <- repo
		return nil
	})
	defer cleanup()

	var reply proto.IndexReply
	if err := svc.Index(&proto.IndexArgs{RepoPath: repoPath, Async: true}, &reply); err != nil {
		t.Fatalf("Index: %v", err)
	}

	select {
	case repo := <-enqueued:
		if repo != repoPath {
			t.Fatalf("enqueued wrong repo: got %q want %q", repo, repoPath)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the scheduler to be enqueued directly (monolith path)")
	}

	dir := requestsDirForRepo(repoPath)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("split mode OFF must not create a requests/ dir; stat=%v", err)
	}
}

func TestIndexAsync_SplitModeOn_WritesRequestFile_NeverTouchesScheduler(t *testing.T) {
	t.Setenv(SplitModeEnvVar, "1")
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	repoPath := t.TempDir()
	enqueued := make(chan string, 1)
	svc, cleanup := newAsyncTestService(t, func(args proto.IndexArgs) (string, string, error) {
		t.Fatal("synchronous index entrypoint must not run on the async fast path")
		return "", "", nil
	}, func(ctx context.Context, repo, ref string) error {
		enqueued <- repo
		return nil
	})
	defer cleanup()

	var reply proto.IndexReply
	if err := svc.Index(&proto.IndexArgs{RepoPath: repoPath, Async: true}, &reply); err != nil {
		t.Fatalf("Index: %v", err)
	}

	select {
	case repo := <-enqueued:
		t.Fatalf("split mode ON must NOT call the scheduler directly, but got enqueue for %q", repo)
	case <-time.After(300 * time.Millisecond):
		// Expected: no direct enqueue.
	}

	dir := requestsDirForRepo(repoPath)
	recs, err := requests.ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 queued reindex request, got %d", len(recs))
	}
	if recs[0].Kind != requests.KindReindex || recs[0].RepoPath != repoPath {
		t.Fatalf("unexpected record: %+v", recs[0])
	}

	// Mutual exclusion, end to end: draining it now must reach the SAME
	// scheduler this test proved was never touched directly.
	if err := drainRequestsOnce(requestsRoot(), extractSchedulerForTest(svc), nil); err != nil {
		t.Fatalf("drainRequestsOnce: %v", err)
	}
	select {
	case repo := <-enqueued:
		if repo != repoPath {
			t.Fatalf("drained enqueue for wrong repo: got %q want %q", repo, repoPath)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the drained request to reach the scheduler")
	}
}

// extractSchedulerForTest exposes svc.scheduler (unexported) to this
// same-package test file without adding a production-facing accessor.
func extractSchedulerForTest(svc *Service) *sched.Scheduler {
	return svc.scheduler
}
