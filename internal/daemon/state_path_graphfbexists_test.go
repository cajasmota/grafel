package daemon

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeSegmentSetFixtureForExistsTest hand-builds a 1-segment gen dir
// (graph.<gen>/ with seg-0000.fb + manifest.json) under stateDir and points
// `current` at it, mirroring the fixture builders already used at
// internal/graph/segmentset_test.go and
// internal/daemon/mcp/graph_cache_segmentset_test.go.
func writeSegmentSetFixtureForExistsTest(t *testing.T, stateDir string, gen uint64) {
	t.Helper()
	doc := &graph.Document{Repo: "seg", Entities: []graph.Entity{
		{ID: "aa1", QualifiedName: "p.A", Kind: "function", Name: "A"},
	}}
	genDir := filepath.Join(stateDir, graph.GenDirName(gen))
	name := graph.SegmentFileName(0)
	if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	ids := []string{"aa1"}
	sort.Strings(ids)
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion, Segments: []graph.SegmentMeta{{
		File: name, Kind: graph.SegmentEntities, EntityCount: 1,
		MinKey: ids[0], MaxKey: ids[0],
	}}}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current: %v", err)
	}
}

// TestGraphFBExistsForRepo_SegmentSet is the RED test for #5915 J2 P2 slice 1:
// a segment-set repo (graph.<gen>/ dir + manifest.json, no flat graph.fb)
// must be reported as HAVING an FB graph. The old call site
// (os.Stat(GraphFBPathForRepo(repoPath))) would find nothing.
func TestGraphFBExistsForRepo_SegmentSet(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	repoPath := t.TempDir()
	stateDir := StateDirForRepo(repoPath)
	writeSegmentSetFixtureForExistsTest(t, stateDir, 4)

	if !GraphFBExistsForRepo(repoPath) {
		t.Fatal("GraphFBExistsForRepo(segment-set repo) = false, want true")
	}
}

// TestGraphFBExistsForRepo_SingleFileParity guards that the single-gen-file
// (and legacy flat) case is byte-identical to the pre-fix os.Stat behavior.
func TestGraphFBExistsForRepo_SingleFileParity(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	repoPath := t.TempDir()
	stateDir := StateDirForRepo(repoPath)
	doc := &graph.Document{Repo: "single", Entities: []graph.Entity{
		{ID: "aaaa0001", QualifiedName: "p.A", Kind: "function", Name: "A"},
	}}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, graph.GenFileName(1)), doc); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(stateDir, graph.GenFileName(1)); err != nil {
		t.Fatal(err)
	}
	if !GraphFBExistsForRepo(repoPath) {
		t.Fatal("GraphFBExistsForRepo(single-file repo) = false, want true (parity)")
	}
}

// TestGraphFBExistsForRepo_Absent guards a never-indexed repo still reads
// absent.
func TestGraphFBExistsForRepo_Absent(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	repoPath := t.TempDir()
	if GraphFBExistsForRepo(repoPath) {
		t.Fatal("GraphFBExistsForRepo(never-indexed repo) = true, want false")
	}
}
