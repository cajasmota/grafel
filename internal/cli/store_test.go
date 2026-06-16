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
	if err := runStoreGC(&buf, storeGCOpts{prune: false}, known); err != nil {
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
	if err := runStoreGC(&buf, storeGCOpts{prune: true}, known); err != nil {
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

// TestRunStoreGC_5268_OptInYesGating drives the operator command's opt-in reap:
// an undeterminable-gone OLD root is reaped only with --include-undeterminable
// + --older-than + --yes; without --yes it stays dry-run; a newer root and a
// live-path root are always kept; default mode (no flag) keeps undeterminable.
func TestRunStoreGC_5268_OptInYesGating(t *testing.T) {
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	// Old undeterminable-gone root (no manifest, not in known set). Age its
	// graph.fb to 30d so it is older than the 7d bound below.
	oldDir := filepath.Join(t.TempDir(), "old-gone")
	oldRoot := makeRoot(t, oldDir)
	thirtyDaysAgo := time.Now().Add(-30 * 24 * time.Hour)
	for _, ref := range []string{"main"} {
		fb := filepath.Join(daemon.StateDirForRepoRef(oldDir, ref), "graph.fb")
		if err := os.Chtimes(fb, thirtyDaysAgo, thirtyDaysAgo); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(oldDir); err != nil {
		t.Fatal(err)
	}

	// Live-path root (exists).
	liveDir := t.TempDir()
	liveRoot := makeRoot(t, liveDir)

	// Forward map knows ONLY the live path → oldRoot is undeterminable.
	known := func() []string { return []string{liveDir} }
	sevenDays := 7 * 24 * time.Hour

	// (1) default mode (no opt-in): undeterminable old root KEPT.
	var buf bytes.Buffer
	if err := runStoreGC(&buf, storeGCOpts{prune: true}, known); err != nil {
		t.Fatalf("default prune: %v", err)
	}
	if !isDir(oldRoot) {
		t.Errorf("default mode reaped an undeterminable root — fail-closed broken")
	}

	// (2) opt-in but WITHOUT --yes (yesWithheld): stays dry-run, nothing reaped.
	buf.Reset()
	err := runStoreGC(&buf, storeGCOpts{
		prune: false, includeUndeterminable: true, reapOlderThan: sevenDays, yesWithheld: true,
	}, known)
	if err != nil {
		t.Fatalf("opt-in no-yes: %v", err)
	}
	if !isDir(oldRoot) {
		t.Errorf("opt-in without --yes reaped a root — must stay dry-run")
	}
	if !strings.Contains(buf.String(), "--yes") {
		t.Errorf("opt-in no-yes output missing --yes hint:\n%s", buf.String())
	}

	// (3) opt-in WITH --yes (prune true): old root reaped, live kept.
	buf.Reset()
	if err := runStoreGC(&buf, storeGCOpts{
		prune: true, includeUndeterminable: true, reapOlderThan: sevenDays,
	}, known); err != nil {
		t.Fatalf("opt-in prune: %v", err)
	}
	if isDir(oldRoot) {
		t.Errorf("opt-in --yes did not reap old undeterminable root %s", oldRoot)
	}
	if !isDir(liveRoot) {
		t.Errorf("opt-in --yes wrongly reaped live-path root %s", liveRoot)
	}
	if !strings.Contains(buf.String(), "re-index") {
		t.Errorf("opt-in prune output missing re-index note:\n%s", buf.String())
	}
}

// TestParseDurationWithDays verifies the day-suffix duration parsing.
func TestParseDurationWithDays(t *testing.T) {
	cases := map[string]time.Duration{
		"7d":   7 * 24 * time.Hour,
		"30d":  30 * 24 * time.Hour,
		"168h": 168 * time.Hour,
		"90m":  90 * time.Minute,
	}
	for in, want := range cases {
		got, err := parseDurationWithDays(in)
		if err != nil {
			t.Errorf("parseDurationWithDays(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseDurationWithDays(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := parseDurationWithDays("notaduration"); err == nil {
		t.Errorf("parseDurationWithDays(garbage): want error, got nil")
	}
}
