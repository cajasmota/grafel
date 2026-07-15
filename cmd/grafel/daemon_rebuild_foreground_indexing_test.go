package main

// daemon_rebuild_foreground_indexing_test.go — regression for the
// foreground-indexing status-plane gap: writeRepoStatusFile's Indexing bit is
// computed purely from indexstate.RepoStates(), which the FOREGROUND
// rebuild/subprocess path (daemonRebuildFuncCore) never populates (it
// deliberately bypasses the scheduler — see internal/repolock's package doc).
// Without the repolock.HasForegroundClaim OR-in (internal/daemon/statuswriter.go)
// and the start-of-index flush added alongside it (this file's ClaimForeground
// call site in daemonRebuildFuncCore), a statusline widget or the wizard TUI
// live-progress feature can never observe Indexing=true during a foreground
// rebuild.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// TestDaemonRebuild_StartFlushSetsIndexingTrue verifies that by the time the
// index function for a repo is actually running (i.e. after
// repolock.ClaimForeground has been acquired and the start-of-index
// FlushRepoStatusFile call has run), the repo's status-plane sidecar reports
// Indexing=true — and that once the rebuild returns (the claim released, the
// terminal flush run), it reports Indexing=false again.
func TestDaemonRebuild_StartFlushSetsIndexingTrue(t *testing.T) {
	group := setupTestGroup(t, "fg-indexing-group", []string{"alpha"})

	var sawIndexingDuringRun bool
	var repoPathSeen string
	mockIndexFn := func(repoPath, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		repoPathSeen = repoPath
		// The start-of-index flush (right after ClaimForeground acquires, in
		// daemonRebuildFuncCore) must have already run by the time this
		// function — which only runs INSIDE the claim — executes.
		if f, ok := daemon.RepoStatusFile(repoPath); ok && f != nil {
			sawIndexingDuringRun = f.Indexing
		}

		sd := daemon.StateDirForRepo(repoPath)
		if err := os.MkdirAll(sd, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(sd, "graph.fb"), []byte("fb"), 0o644)
	}

	rebuilt, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn)
	if err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}
	if len(rebuilt) != 1 {
		t.Fatalf("rebuilt %d repos, want 1", len(rebuilt))
	}
	if repoPathSeen == "" {
		t.Fatal("mockIndexFn never ran")
	}
	if !sawIndexingDuringRun {
		t.Error("Indexing should have been true during the index run — the start-of-index flush should have set it via repolock.HasForegroundClaim")
	}

	f, ok := daemon.RepoStatusFile(rebuilt[0])
	if !ok || f == nil {
		t.Fatalf("no status file for %s after rebuild returned", rebuilt[0])
	}
	if f.Indexing {
		t.Errorf("Indexing = true after rebuild returned; want false (claim released, terminal flush ran)")
	}
}
