package audit

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeSegmentSetFixture hand-builds a 1-segment gen dir (graph.<gen>/ with
// seg-0000.fb + manifest.json) under stateDir and points `current` at it. This
// mirrors the fixture builders already used at internal/graph/segmentset_test.go
// and internal/daemon/mcp/graph_cache_segmentset_test.go (no producer emits
// this shape yet — #5901 dark read substrate — so the test constructs it
// directly).
func writeSegmentSetFixture(t *testing.T, stateDir string, gen uint64) string {
	t.Helper()
	doc := &graph.Document{Repo: "seg", Entities: []graph.Entity{
		{ID: "aa1", QualifiedName: "p.A", Kind: "function", Name: "A"},
		{ID: "aa2", QualifiedName: "p.B", Kind: "struct", Name: "B"},
	}, Relationships: []graph.Relationship{{FromID: "aa1", ToID: "aa2", Kind: "CALLS"}}}

	genDir := filepath.Join(stateDir, graph.GenDirName(gen))
	name := graph.SegmentFileName(0)
	if err := fbwriter.WriteAtomic(filepath.Join(genDir, name), doc); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	ids := make([]string, 0, len(doc.Entities))
	for _, e := range doc.Entities {
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)
	m := &graph.Manifest{FormatVersion: graph.ManifestFormatVersion, Segments: []graph.SegmentMeta{{
		File: name, Kind: graph.SegmentEntities,
		EntityCount: len(doc.Entities), RelCount: len(doc.Relationships),
		MinKey: ids[0], MaxKey: ids[len(ids)-1],
	}}}
	if err := graph.WriteManifest(genDir, m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := graph.WriteCurrentPointerRaw(stateDir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current: %v", err)
	}
	return genDir
}

// TestHasGraphJSON_SegmentSet is the RED test for #5915 J2 P2 slice 1: a
// segment-set repo (graph.<gen>/ dir + manifest.json, no flat graph.fb) must
// be seen as HAVING a graph. Before the fix, hasGraphJSON stat'd
// CurrentGraphPath(stateDir) — the flat .fb path, which is absent for a
// segment-set — so this reported false.
func TestHasGraphJSON_SegmentSet(t *testing.T) {
	repoDir := t.TempDir()
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)

	stateDir := daemon.StateDirForRepo(repoDir)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSegmentSetFixture(t, stateDir, 3)

	if !hasGraphJSON(repoDir) {
		t.Fatal("hasGraphJSON(segment-set repo) = false, want true")
	}
	if !HasGraph(repoDir) {
		t.Fatal("HasGraph(segment-set repo) = false, want true")
	}
}

// TestHasGraphJSON_SingleFileParity guards that the flat/single-gen-file case
// is byte-identical to before the fix.
func TestHasGraphJSON_SingleFileParity(t *testing.T) {
	repoDir := t.TempDir()
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)

	stateDir := daemon.StateDirForRepo(repoDir)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := &graph.Document{Repo: "single", Entities: []graph.Entity{
		{ID: "aaaa0001", QualifiedName: "p.A", Kind: "function", Name: "A"},
	}}
	if err := fbwriter.WriteAtomic(filepath.Join(stateDir, graph.GenFileName(1)), doc); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(stateDir, graph.GenFileName(1)); err != nil {
		t.Fatal(err)
	}
	if !hasGraphJSON(repoDir) {
		t.Fatal("hasGraphJSON(single-file repo) = false, want true (parity)")
	}
}

// TestHasGraphJSON_Absent guards that a never-indexed repo still reads absent.
func TestHasGraphJSON_Absent(t *testing.T) {
	repoDir := t.TempDir()
	root := t.TempDir()
	t.Setenv(daemon.EnvRoot, root)

	if hasGraphJSON(repoDir) {
		t.Fatal("hasGraphJSON(never-indexed repo) = true, want false")
	}
}
