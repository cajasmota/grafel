package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildRoot creates a synthetic store root for sourcePath under an isolated
// GRAFEL_DAEMON_ROOT, with one ref dir holding a graph.fb of the given mtime.
// Returns the root dir.
func buildRoot(t *testing.T, sourcePath, ref string, mtime time.Time) string {
	t.Helper()
	refDir := StateDirForRepoRef(sourcePath, ref)
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatalf("mkdir refdir: %v", err)
	}
	fb := filepath.Join(refDir, "graph.fb")
	if err := os.WriteFile(fb, []byte("synthetic-graph"), 0o644); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	if err := os.Chtimes(fb, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	return RepoBaseDir(sourcePath)
}

// orphanFixture wires an isolated daemon root + a frozen clock so grace-window
// math is deterministic.
type orphanFixture struct {
	root string // GRAFEL_DAEMON_ROOT
	now  time.Time
}

func newOrphanFixture(t *testing.T) *orphanFixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	return &orphanFixture{root: root, now: time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)}
}

func (f *orphanFixture) sweeper(known func() []string) *OrphanRootSweeper {
	return NewOrphanRootSweeper(OrphanRootConfig{
		KnownSourcePaths: known,
		Now:              func() time.Time { return f.now },
		// Default 24h grace.
	})
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// TestOrphanRoot_AttributionAndPrune exercises every safety branch with one
// synthetic store: (a) orphan → pruned, (b) live-path → kept, (c) live-group
// primary → kept, (d) undeterminable → kept (fail-closed), (e) orphan within
// grace → kept.
func TestOrphanRoot_AttributionAndPrune(t *testing.T) {
	f := newOrphanFixture(t)

	old := f.now.Add(-72 * time.Hour) // outside grace
	fresh := f.now.Add(-1 * time.Hour) // inside grace

	// (b) live-path root: source dir exists on disk.
	liveDir := t.TempDir()
	liveRoot := buildRoot(t, liveDir, "main", old)

	// (c) live-group/primary root: also a real, existing path (a registered
	// group repo is, from the sweeper's POV, just a known path that exists).
	primaryDir := t.TempDir()
	primaryRoot := buildRoot(t, primaryDir, "main", old)

	// (a) orphan root: known path, but its dir is GONE, old artifact.
	goneDir := filepath.Join(t.TempDir(), "deleted-worktree")
	orphanRoot := buildRoot(t, goneDir, "feature/x", old)
	if err := os.RemoveAll(goneDir); err != nil {
		t.Fatalf("rm goneDir: %v", err)
	}

	// (e) orphan within grace: known path gone but artifact is fresh.
	graceDir := filepath.Join(t.TempDir(), "just-deleted")
	graceRoot := buildRoot(t, graceDir, "main", fresh)
	if err := os.RemoveAll(graceDir); err != nil {
		t.Fatalf("rm graceDir: %v", err)
	}

	// (d) undeterminable root: build it for a path NOT in the known set, then
	// delete the path. The sweeper cannot attribute it → fail-closed KEEP.
	unknownDir := filepath.Join(t.TempDir(), "never-registered")
	unknownRoot := buildRoot(t, unknownDir, "main", old)
	if err := os.RemoveAll(unknownDir); err != nil {
		t.Fatalf("rm unknownDir: %v", err)
	}

	known := func() []string {
		// Note: unknownDir is intentionally OMITTED.
		return []string{liveDir, primaryDir, goneDir, graceDir}
	}

	s := f.sweeper(known)

	// --- dry-run attribution -------------------------------------------------
	byRoot := map[string]OrphanRootVerdict{}
	for _, v := range s.Attribute() {
		byRoot[filepath.Clean(v.Root)] = v
	}

	check := func(name, root, wantVerdict string, wantOrphan bool) {
		t.Helper()
		v, ok := byRoot[filepath.Clean(root)]
		if !ok {
			t.Fatalf("%s: root %s not attributed", name, root)
		}
		if v.Verdict != wantVerdict {
			t.Errorf("%s: verdict=%q want %q (reason=%q)", name, v.Verdict, wantVerdict, v.Reason)
		}
		if v.IsOrphan() != wantOrphan {
			t.Errorf("%s: IsOrphan=%v want %v", name, v.IsOrphan(), wantOrphan)
		}
	}

	check("(b) live-path", liveRoot, "KEEP", false)
	check("(c) live-primary", primaryRoot, "KEEP", false)
	check("(a) orphan", orphanRoot, "ORPHAN", true)
	check("(e) grace", graceRoot, "KEEP", false)
	check("(d) undeterminable", unknownRoot, "KEEP", false)

	// The orphan verdict must carry size accounting > 0 (it has a graph.fb).
	if byRoot[filepath.Clean(orphanRoot)].SizeBytes <= 0 {
		t.Errorf("orphan SizeBytes should be > 0, got %d", byRoot[filepath.Clean(orphanRoot)].SizeBytes)
	}
	// Undeterminable must say so in the reason.
	if got := byRoot[filepath.Clean(unknownRoot)].Reason; got == "" || !strings.Contains(got, "undeterminable") {
		t.Errorf("undeterminable reason = %q, want mention of 'undeterminable'", got)
	}

	// Dry-run must NOT remove anything.
	for _, root := range []string{liveRoot, primaryRoot, orphanRoot, graceRoot, unknownRoot} {
		if !dirExists(root) {
			t.Errorf("dry-run removed %s — must not touch disk", root)
		}
	}

	// --- prune ---------------------------------------------------------------
	wantFreed := dirSizeMust(t, orphanRoot)
	res := s.Sweep()

	if res.RootsReaped != 1 {
		t.Fatalf("RootsReaped=%d want 1", res.RootsReaped)
	}
	if res.FreedBytes != wantFreed {
		t.Errorf("FreedBytes=%d want %d", res.FreedBytes, wantFreed)
	}
	if res.Kept < 4 {
		t.Errorf("Kept=%d want >=4 (live, primary, grace, undeterminable)", res.Kept)
	}

	// Only the orphan is gone; everything else survives.
	if dirExists(orphanRoot) {
		t.Errorf("orphan root NOT removed by prune: %s", orphanRoot)
	}
	for name, root := range map[string]string{
		"live": liveRoot, "primary": primaryRoot, "grace": graceRoot, "undeterminable": unknownRoot,
	} {
		if !dirExists(root) {
			t.Errorf("prune wrongly removed %s root %s", name, root)
		}
	}
}

// TestOrphanRoot_NoKnownPaths_KeepsEverything: with an empty known set every
// root is undeterminable → nothing is reaped (fail-closed).
func TestOrphanRoot_NoKnownPaths_KeepsEverything(t *testing.T) {
	f := newOrphanFixture(t)
	gone := filepath.Join(t.TempDir(), "x")
	root := buildRoot(t, gone, "main", f.now.Add(-72*time.Hour))
	_ = os.RemoveAll(gone)

	s := f.sweeper(func() []string { return nil })
	res := s.Sweep()
	if res.RootsReaped != 0 {
		t.Fatalf("RootsReaped=%d want 0 (fail-closed)", res.RootsReaped)
	}
	if !dirExists(root) {
		t.Errorf("fail-closed sweep removed undeterminable root %s", root)
	}
}

// TestOrphanRoot_GraceDisabledReapsFresh: a negative grace window disables the
// grace guard, so even a freshly-indexed-but-gone root is reaped.
func TestOrphanRoot_GraceDisabledReapsFresh(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	now := time.Now()
	gone := filepath.Join(t.TempDir(), "fresh-gone")
	storeRoot := buildRoot(t, gone, "main", now)
	_ = os.RemoveAll(gone)

	s := NewOrphanRootSweeper(OrphanRootConfig{
		KnownSourcePaths: func() []string { return []string{gone} },
		GraceWindow:      -1, // disable grace
		Now:              func() time.Time { return now },
	})
	res := s.Sweep()
	if res.RootsReaped != 1 {
		t.Fatalf("RootsReaped=%d want 1 (grace disabled)", res.RootsReaped)
	}
	if dirExists(storeRoot) {
		t.Errorf("root not reaped with grace disabled: %s", storeRoot)
	}
}

func dirSizeMust(t *testing.T, dir string) int64 {
	t.Helper()
	sz, err := dirSizeHygiene(dir)
	if err != nil {
		t.Fatalf("dirSize %s: %v", dir, err)
	}
	return sz
}
