package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/requests"
	"github.com/cajasmota/grafel/internal/daemon/sched"
)

// TestDiscoverRequestsDirs_FindsPerRefRequestsDirs is the engine-side
// discovery regression (ADR-0024 PR4, epic #5729): the engine has no a
// priori list of repos, so it must find every `requests/` directory under
// the store's `<slug>-<hash>/refs/<ref>/` layout by globbing, not by asking
// a specific repo for its state dir.
func TestDiscoverRequestsDirs_FindsPerRefRequestsDirs(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	repoPath := t.TempDir()
	dir := requestsDirForRepo(repoPath)
	if _, err := requests.Write(dir, requests.Record{Kind: requests.KindReindex, RepoPath: repoPath}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	dirs, err := discoverRequestsDirs(requestsRoot())
	if err != nil {
		t.Fatalf("discoverRequestsDirs: %v", err)
	}
	found := false
	for _, d := range dirs {
		if d == dir {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %q among discovered dirs, got %v", dir, dirs)
	}
}

// TestDrainRequestsOnce_ReindexEnqueuesOntoScheduler is the round-trip
// regression for the MOST IMPORTANT producer/consumer pair named in epic
// #5729: a KindReindex request dropped by serve's Service.Index (simulated
// here directly via requests.Write, mirroring what service.go's Index method
// does when SplitModeEnabled()) is drained by the engine and turned into a
// real scheduler.Enqueue call — the same call the monolith/engine makes
// in-process today.
func TestDrainRequestsOnce_ReindexEnqueuesOntoScheduler(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	repoPath := t.TempDir()
	dir := requestsDirForRepo(repoPath)
	id, err := requests.Write(dir, requests.Record{Kind: requests.KindReindex, RepoPath: repoPath})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	indexed := make(chan string, 1)
	sc := sched.New(sched.Config{
		Index: func(ctx context.Context, repo, ref string) error {
			indexed <- repo
			return nil
		},
	})
	sc.Start()
	defer sc.Stop()

	if err := drainRequestsOnce(requestsRoot(), sc, nil, nil); err != nil {
		t.Fatalf("drainRequestsOnce: %v", err)
	}

	select {
	case repo := <-indexed:
		if repo != repoPath {
			t.Fatalf("indexed wrong repo: got %q want %q", repo, repoPath)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for scheduler to index the enqueued repo")
	}

	// Ack-GC (PR6 prerequisite gap #2, epic #5729): ApplyAndAck now deletes
	// the ack file immediately after a successful apply+request-delete —
	// its only purpose was guarding the crash window between "ack written"
	// and "request deleted", which is closed the instant the request file
	// is confirmed gone. So a fully-succeeded drain leaves NEITHER the
	// request NOR the ack behind (see requests.TestApplyAndAck_DeletesAckAfterSuccess
	// for the focused regression); this is exactly what we assert here.
	if _, ok, err := requests.ReadAck(dir, id); err != nil {
		t.Fatalf("ReadAck: %v", err)
	} else if ok {
		t.Fatal("expected the ack to have been GC'd after a successful drain, but it still exists")
	}

	recs, err := requests.ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected the request to be consumed, still pending: %+v", recs)
	}
}

// TestDrainRequestsOnce_SecondDrainDoesNotDoubleEnqueue is the
// exactly-once / idempotency regression: draining twice in a row (as the
// periodic loop does) must not enqueue the same already-consumed request a
// second time.
func TestDrainRequestsOnce_SecondDrainDoesNotDoubleEnqueue(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	repoPath := t.TempDir()
	dir := requestsDirForRepo(repoPath)
	if _, err := requests.Write(dir, requests.Record{Kind: requests.KindReindex, RepoPath: repoPath}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var enqueueCount int
	indexed := make(chan string, 4)
	sc := sched.New(sched.Config{
		Index: func(ctx context.Context, repo, ref string) error {
			indexed <- repo
			return nil
		},
	})
	sc.Start()
	defer sc.Stop()

	if err := drainRequestsOnce(requestsRoot(), sc, nil, nil); err != nil {
		t.Fatalf("drainRequestsOnce (1st): %v", err)
	}
	if err := drainRequestsOnce(requestsRoot(), sc, nil, nil); err != nil {
		t.Fatalf("drainRequestsOnce (2nd): %v", err)
	}

	select {
	case <-indexed:
		enqueueCount++
	case <-time.After(2 * time.Second):
		t.Fatal("expected at least one index call")
	}
	// Drain any further (unexpected) sends without blocking.
	select {
	case <-indexed:
		enqueueCount++
	case <-time.After(300 * time.Millisecond):
	}
	if enqueueCount != 1 {
		t.Fatalf("expected exactly 1 index call across both drains, got %d", enqueueCount)
	}
}
