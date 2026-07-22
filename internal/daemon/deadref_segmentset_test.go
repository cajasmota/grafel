package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeSegmentSetRefStore hand-builds a 1-segment gen dir (graph.<gen>/ with
// seg-0000.fb + manifest.json) under <refsRoot>/<refSafe>/, points `current`
// at it, and stamps manifest.json (the segment-set's atomic commit point)
// with mtime. Mirrors writeRefStore (the flat-graph.fb fixture already used
// in this package) and the fixture builders at
// internal/daemon/state_path_graphfbexists_test.go /
// internal/graph/segmentset_test.go.
func writeSegmentSetRefStore(t *testing.T, refsRoot, refSafe string, gen uint64, mtime time.Time) string {
	t.Helper()
	refDir := filepath.Join(refsRoot, refSafe)
	genDir := filepath.Join(refDir, graph.GenDirName(gen))
	doc := &graph.Document{Repo: "seg", Entities: []graph.Entity{
		{ID: "aa1", QualifiedName: "p.A", Kind: "function", Name: "A"},
	}}
	name := graph.SegmentFileName(0)
	if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion, Segments: []graph.SegmentMeta{{
		File: name, Kind: graph.SegmentEntities, EntityCount: 1,
		MinKey: "aa1", MaxKey: "aa1",
	}}}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manifestPath := filepath.Join(genDir, graph.ManifestFileName)
	if err := os.Chtimes(manifestPath, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointerRaw(refDir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current: %v", err)
	}
	return refDir
}

// TestRecentlyIndexed_SegmentSet is the RED unit test for #5915 J2 slice-3
// (DATA LOSS fix): a segment-set ref (graph.<gen>/ dir + manifest.json, no
// flat graph.fb) whose manifest.json mtime is inside the grace cutoff must be
// reported recentlyIndexed==true. Before the fix, recentlyIndexed resolved
// only graph.CurrentGraphPath(refDir) (always absent for a segment-set ref),
// so this read false and the ref fell outside grace protection.
func TestRecentlyIndexed_SegmentSet(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	refDir := writeSegmentSetRefStore(t, root, "wip-seg", 3, now.Add(-1*time.Hour))

	if !recentlyIndexed(refDir, now.Add(-24*time.Hour)) {
		t.Fatal("recentlyIndexed(segment-set ref, within grace) = false, want true")
	}
	if recentlyIndexed(refDir, now.Add(1*time.Hour)) {
		t.Fatal("recentlyIndexed(segment-set ref, cutoff in the future) = true, want false")
	}
}

// TestRefGraphMtime_SegmentSet guards refGraphMtime resolves the manifest.json
// mtime for a segment-set ref instead of reporting the zero time.
func TestRefGraphMtime_SegmentSet(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	mtime := now.Add(-1 * time.Hour)
	refDir := writeSegmentSetRefStore(t, root, "wip-seg", 3, mtime)

	got := refGraphMtime(refDir)
	if !got.Equal(mtime) {
		t.Fatalf("refGraphMtime(segment-set ref) = %v, want %v", got, mtime)
	}
}

// TestDeadRef_segmentSetRefSurvivesGraceWindow is the load-bearing end-to-end
// RED test for #5915 J2 slice-3's DATA LOSS fix: a segment-set ref that is
// dead-in-git but was indexed inside the grace window must survive a full GC
// pass (Sweep) — it must NOT be reaped. Before the fix, recentlyIndexed
// resolved mtime-zero for this ref (no flat graph.fb ever existed), so the
// grace guard never engaged and the ref was reaped despite being fresh.
func TestDeadRef_segmentSetRefSurvivesGraceWindow(t *testing.T) {
	repo := mkLiveRepo(t)
	refsRoot := filepath.Join(t.TempDir(), "refs")
	now := time.Unix(1_700_000_000, 0)

	// Primary ref, single-file, indexed long ago — present so the fixture
	// mirrors a real store; survives purely on the primary guard.
	writeRefStore(t, refsRoot, "main", 1000, now.Add(-100*time.Hour))
	// A dead-in-git SEGMENT-SET ref, indexed within the grace window.
	segDir := writeSegmentSetRefStore(t, refsRoot, "wip-seg", 3, now.Add(-1*time.Hour))

	ff := &fakeRefForgetter{held: map[[2]string]bool{}}
	var dropped [][2]string
	// git lists NOTHING for either ref — only the grace window can protect
	// wip-seg (main is separately protected by the primary guard).
	live := map[string]struct{}{}
	s := NewDeadRefSweeper(DeadRefConfig{
		TrackedRepos:   func() []string { return []string{repo} },
		LiveRefs:       func(string) (map[string]struct{}, bool) { return live, true },
		PrimaryRef:     func(string) string { return "main" },
		RefsDirForRepo: func(string) string { return refsRoot },
		Tier:           ff,
		DropReader:     func(rp, ref string) { dropped = append(dropped, [2]string{rp, ref}) },
		GraceWindow:    24 * time.Hour,
		Now:            func() time.Time { return now },
	})

	res := s.Sweep()

	if res.RefsReaped != 0 {
		t.Fatalf("RefsReaped=%d want 0 (segment-set ref must survive the grace window)", res.RefsReaped)
	}
	if _, err := os.Stat(segDir); err != nil {
		t.Errorf("segment-set ref dir was reaped despite grace window: %v", err)
	}
	if len(dropped) != 0 {
		t.Errorf("DropReader called unexpectedly: %v", dropped)
	}
}

// TestRecentlyIndexed_SingleFileParity guards that the single-gen-file (and
// legacy flat) resolution is byte-identical to the pre-fix behavior.
func TestRecentlyIndexed_SingleFileParity(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	refDir := writeRefStore(t, root, "wip", 1000, now.Add(-1*time.Hour))

	if !recentlyIndexed(refDir, now.Add(-24*time.Hour)) {
		t.Fatal("recentlyIndexed(single-file ref, within grace) = false, want true (parity)")
	}
	if recentlyIndexed(refDir, now.Add(1*time.Hour)) {
		t.Fatal("recentlyIndexed(single-file ref, cutoff in the future) = true, want false (parity)")
	}
}
