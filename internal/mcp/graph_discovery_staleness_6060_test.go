// graph_discovery_staleness_6060_test.go — issue #6060.
//
// lr.GraphFile is the anchor for everything per-repo: the Document, the
// embeddings sidecar, and (after this issue) the description side-table. The
// #2550 short-circuit in reloadAllLocked PINS it — once set, reload stats that
// exact file and, as long as it still exists, never re-runs discovery. So a repo
// indexed at "main" whose HEAD then moves to "feature" AND gets reindexed keeps
// serving refs/main's graph forever, while every other resolver in the process
// (which re-derives per call) has moved on to refs/feature.
//
// That is a staleness bug on its own, and it is also the precondition for the
// side-table pairing: "the directory the graph was discovered in" is only a
// correct target if discovery is CURRENT.
package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// gitRepoForDiscovery creates a real git repo with one commit on branch "main".
func gitRepoForDiscovery(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

func writeGraphInto(t *testing.T, dir string, doc *graph.Document) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb into %s: %v", dir, err)
	}
}

// A reindex under the NEW HEAD ref must be picked up by the next reload.
//
// This is the default install shape: watchers ON, so a branch switch is followed
// by an index of the new ref. Before the fix, lr.GraphFile stayed pinned at
// refs/main/graph.fb — the file still exists and stats fine, so the #2550
// short-circuit never falls through to discovery — and the daemon served the old
// ref's graph indefinitely.
//
// Proof the fixture can fail: it does fail with the #2550 short-circuit intact,
// reporting refs/main where refs/feature is expected.
func TestReload_RediscoversGraphAfterHeadMovesAndIsReindexed(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)

	repoDir := gitRepoForDiscovery(t)
	mainDir := daemon.StateDirForRepoRef(repoDir, "main")
	featureDir := daemon.StateDirForRepoRef(repoDir, "feature")
	if mainDir == featureDir {
		t.Fatal("fixture degenerate: both refs resolve to the same state dir")
	}

	// Indexed at main.
	writeGraphInto(t, mainDir, lazyTestDoc())

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}

	lr := st.Group("test").Repos["r"]
	if lr == nil {
		t.Fatal("repo not loaded")
	}
	if got := filepath.Dir(lr.GraphFile); got != mainDir {
		t.Fatalf("initial discovery resolved %s, want %s", got, mainDir)
	}

	// HEAD moves, and the new ref is indexed (watchers ON — the default).
	cmd := exec.Command("git", "checkout", "-b", "feature")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}
	writeGraphInto(t, featureDir, lazyTestDoc())

	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reload after head move: %v", err)
	}

	if got := filepath.Dir(lr.GraphFile); got != featureDir {
		t.Errorf("reload kept serving a stale ref's graph:\n  lr.GraphFile dir: %s\n  want:             %s\n"+
			"  (the #2550 short-circuit pinned discovery; every other resolver in the\n"+
			"   process has already moved to the new ref)", got, featureDir)
	}
}

// The reader's anchor and the writer's resolver must name the same directory in
// the population above. The writer (dashboard enrichment write-back) re-derives
// through FindGraphFileAnyRef on every call; the reader uses lr.GraphFile. If
// discovery is pinned they diverge and the description write is silently lost —
// same signature as the AnyRef case, but in the DEFAULT population.
//
// Proof the fixture can fail: with the #2550 short-circuit intact this reports
// readerDir=refs/main writerDir=refs/feature.
func TestReload_ReaderAndWriterAgreeAfterHeadMoveAndReindex(t *testing.T) {
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)

	repoDir := gitRepoForDiscovery(t)
	writeGraphInto(t, daemon.StateDirForRepoRef(repoDir, "main"), lazyTestDoc())

	reg := &Registry{Groups: map[string]RegistryGroup{
		"test": {Repos: map[string]RegistryRepo{"r": {Path: repoDir}}},
	}}
	st := NewState(reg)
	t.Cleanup(st.Close)
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}

	cmd := exec.Command("git", "checkout", "-b", "feature")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}
	writeGraphInto(t, daemon.StateDirForRepoRef(repoDir, "feature"), lazyTestDoc())
	if _, _, err := st.reloadLocked(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	lr := st.Group("test").Repos["r"]
	readerDir := repoStateDir(lr)

	// What the dashboard write-back resolves (enrichmentStateDir's resolution).
	discovered, _ := daemon.FindGraphFileAnyRef(repoDir)
	writerDir := filepath.Dir(discovered)

	if readerDir != writerDir {
		t.Errorf("side-table reader and writer disagree — the description write would be\n"+
			"silently lost on the next reload:\n  readerDir=%s\n  writerDir=%s", readerDir, writerDir)
	}
}
