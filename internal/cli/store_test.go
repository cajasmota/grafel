package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
)

// makeRoot creates a synthetic store root (with a graph.fb whose mtime is set
// well outside the grace window) for sourcePath under the active
// GRAFEL_DAEMON_ROOT and returns the root dir.
func makeRoot(t *testing.T, sourcePath string) string {
	t.Helper()
	refDir := daemon.StateDirForRepoRef(sourcePath, "main")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fb := filepath.Join(refDir, "graph.fb")
	if err := os.WriteFile(fb, []byte("g"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(fb, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	return daemon.RepoBaseDir(sourcePath)
}

// TestRunStoreGC_DryRunThenPrune drives the operator command's wrapper: a
// dry-run must report the orphan + reclaim estimate WITHOUT touching disk, then
// --prune must reap exactly the orphan.
func TestRunStoreGC_DryRunThenPrune(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	liveDir := t.TempDir()
	liveRoot := makeRoot(t, liveDir)

	goneDir := filepath.Join(t.TempDir(), "gone")
	orphanRoot := makeRoot(t, goneDir)
	if err := os.RemoveAll(goneDir); err != nil {
		t.Fatal(err)
	}

	known := func() []string { return []string{liveDir, goneDir} }

	// --- dry-run -------------------------------------------------------------
	var buf bytes.Buffer
	if err := runStoreGC(&buf, false, known); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ORPHAN") {
		t.Errorf("dry-run output missing ORPHAN verdict:\n%s", out)
	}
	if !strings.Contains(out, "1 orphan(s)") {
		t.Errorf("dry-run output missing orphan count:\n%s", out)
	}
	if !strings.Contains(out, "--prune") {
		t.Errorf("dry-run output missing prune hint:\n%s", out)
	}
	// Disk untouched.
	if !isDir(orphanRoot) || !isDir(liveRoot) {
		t.Fatalf("dry-run removed a root (orphan=%v live=%v)", isDir(orphanRoot), isDir(liveRoot))
	}

	// --- prune ---------------------------------------------------------------
	buf.Reset()
	if err := runStoreGC(&buf, true, known); err != nil {
		t.Fatalf("prune: %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, "Pruned 1 orphan") {
		t.Errorf("prune output missing reclamation summary:\n%s", out)
	}
	if isDir(orphanRoot) {
		t.Errorf("prune did not remove orphan root %s", orphanRoot)
	}
	if !isDir(liveRoot) {
		t.Errorf("prune wrongly removed live root %s", liveRoot)
	}
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
