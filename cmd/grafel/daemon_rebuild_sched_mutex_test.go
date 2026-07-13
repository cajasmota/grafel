package main

// daemon_rebuild_sched_mutex_test.go — regression for the split-mode rebuild ⇄
// scheduler concurrency bug: a wizard-triggered FOREGROUND group rebuild and
// the engine SCHEDULER both indexed the SAME repo at once, unserialized, both
// rewriting <store>/<repo>/refs/<ref>/graph.fb. The rebuild's post-index
// cross-repo link pass then never converged against a file being rewritten
// beneath it, so the rebuild never returned and its KindRebuild request was
// never acked — runaway re-indexing that only stopped on a manual daemon kill.
//
// The rebuild path (daemonRebuildFuncCore → indexFn → Index) never touches the
// scheduler, so the scheduler's per-repo in-flight guard did not know the repo
// was being rebuilt. This test wires a REAL sched.Scheduler whose Index hook
// shares a max-concurrency counter with the rebuild's indexFn, enqueues repo R
// on the scheduler while running the rebuild against the SAME R, and asserts
// the two never overlap (max concurrent index of R == 1).

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/registry"
)

// setupSingleRepoGroup registers a group with exactly one repo and returns the
// group name and that repo's on-disk path (the key both the rebuild and the
// scheduler index against).
func setupSingleRepoGroup(t *testing.T, groupName, slug string) (group, repoPath string) {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("GRAFEL_HOME", tmpHome)
	repoBase := t.TempDir()
	repoPath = repoBase + "/" + slug
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := tmpHome + "/" + groupName + ".fleet.json"
	cfg := &registry.GroupConfig{Name: groupName, Repos: []registry.Repo{{Slug: slug, Path: repoPath}}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup(groupName, cfgPath); err != nil {
		t.Fatal(err)
	}
	return groupName, repoPath
}

func TestRebuildAndSchedulerDoNotIndexSameRepoConcurrently(t *testing.T) {
	group, repoPath := setupSingleRepoGroup(t, "mutex-group", "only")

	const hold = 250 * time.Millisecond

	// Shared concurrency tracker: every index of repoPath (from EITHER the
	// scheduler hook or the rebuild indexFn) enters, records the running-max,
	// holds briefly so any second concurrent entry is observable, then leaves.
	var active, maxActive int32
	enterLeave := func() {
		cur := atomic.AddInt32(&active, 1)
		for {
			m := atomic.LoadInt32(&maxActive)
			if cur <= m || atomic.CompareAndSwapInt32(&maxActive, m, cur) {
				break
			}
		}
		time.Sleep(hold)
		atomic.AddInt32(&active, -1)
	}

	// Real scheduler whose Index hook shares the tracker.
	schedRan := make(chan struct{}, 8)
	scheduler := sched.New(sched.Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Index: func(_ context.Context, rp, _ string) error {
			if rp == repoPath {
				enterLeave()
			}
			schedRan <- struct{}{}
			return nil
		},
	})
	scheduler.Start()
	defer scheduler.Stop()

	// Rebuild indexFn shares the SAME tracker for the SAME repo.
	rebuildIndexFn := func(rp, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		if rp == repoPath {
			enterLeave()
		}
		return nil
	}

	// Kick off the FOREGROUND rebuild first so it registers its claim, then
	// enqueue the SAME repo onto the scheduler so the two race for graph.fb.
	rebuildDone := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, _ = daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group, Interactive: true}, rebuildIndexFn, noopLinksFn)
		close(rebuildDone)
	}()

	// Let the rebuild reach its index hook, then enqueue the scheduler reindex
	// of the same repo — the exact concurrent-write race from the live daemon.
	time.Sleep(30 * time.Millisecond)
	scheduler.Enqueue(repoPath)

	<-rebuildDone
	// Grace for any concurrently-admitted scheduler run to have recorded itself.
	time.Sleep(50 * time.Millisecond)

	if got := atomic.LoadInt32(&maxActive); got != 1 {
		t.Fatalf("rebuild and scheduler indexed the same repo concurrently: maxActive=%d, want 1", got)
	}

	// Guard against a false pass where maxActive stayed 1 only because the
	// scheduler never admitted the repo at all: on the fixed code it yields
	// during the rebuild (no Index call) and retries after yieldRetryDelay, so
	// its Index hook must fire shortly after the rebuild releases its claim.
	select {
	case <-schedRan:
	case <-time.After(6 * time.Second):
		t.Fatal("scheduler never admitted/ran the enqueued repo (no yield-retry observed)")
	}
	wg.Wait()
}
