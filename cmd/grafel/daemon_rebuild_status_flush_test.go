package main

// daemon_rebuild_status_flush_test.go — #5729 blocker #5 regression: the engine
// direct-rebuild path must leave a FRESH per-repo status sidecar (advanced
// graph_fb_mtime, Indexing=false) upon return, BEFORE the split-mode drain
// writes the request ack, so a wizard keying completion on the ack classifies
// the repo as indexed-OK instead of "produced no graph".

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// TestDaemonRebuild_FlushesFreshStatusBeforeReturn verifies daemonRebuildFuncCore
// synchronously writes each rebuilt repo's status file with a fresh graph_fb_mtime
// (> the pre-rebuild baseline, i.e. > 0 for a fresh group) and Indexing=false by
// the time it returns.
func TestDaemonRebuild_FlushesFreshStatusBeforeReturn(t *testing.T) {
	group := setupTestGroup(t, "status-flush-group", []string{"alpha", "beta"})

	// The index fn writes a real graph.fb into each repo's state dir (as the
	// real indexer does) so FindGraphFile — which the status flush stats — sees
	// a fresh mtime.
	mockIndexFn := func(repoPath, _, _ string, _ []string, _, _ bool, _ ...IndexOption) error {
		sd := daemon.StateDirForRepo(repoPath)
		if err := os.MkdirAll(sd, 0o755); err != nil {
			return err
		}
		// A <8-byte graph.fb: FindGraphFile still stats a fresh mtime, but the
		// fb reader rejects it gracefully (too short) instead of parsing garbage
		// — we only need the mtime to advance, not a valid graph.
		return os.WriteFile(filepath.Join(sd, "graph.fb"), []byte("fb"), 0o644)
	}

	rebuilt, _, err := daemonRebuildFuncCore(1, proto.RebuildArgs{Group: group}, mockIndexFn, noopLinksFn)
	if err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}
	if len(rebuilt) != 2 {
		t.Fatalf("rebuilt %d repos, want 2", len(rebuilt))
	}

	for _, rp := range rebuilt {
		f, ok := daemon.RepoStatusFile(rp)
		if !ok || f == nil {
			t.Fatalf("no status file flushed for %s (the drain would ack before any status write)", rp)
		}
		if f.GraphFBMtime <= 0 {
			t.Errorf("%s: GraphFBMtime = %d; want a fresh (>0) mtime flushed before return", rp, f.GraphFBMtime)
		}
		if f.Indexing {
			t.Errorf("%s: Indexing = true; want false after the rebuild completed", rp)
		}
	}
}
