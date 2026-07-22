package dashboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// TestStatGraphMtime_SegmentSet is the RED test for #5915 J2 slice-2:
// statGraphMtime (the freshness signal behind DashGroup.diskUnchanged's
// per-repo mtime-keyed cache, #50) must resolve a SEGMENT-SET stateDir
// (graph.<gen>/ dir + manifest.json, no flat graph.fb) to the manifest.json
// mtime, not the zero time. The pre-fix os.Stat(graph.CurrentGraphPath(dir))
// gate only ever resolves a flat .fb path.
func TestStatGraphMtime_SegmentSet(t *testing.T) {
	stateDir := t.TempDir()
	mtime := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	writeDashboardSegmentSetFixture(t, stateDir, 5, mtime)

	got := statGraphMtime(stateDir)
	if got.IsZero() {
		t.Fatal("statGraphMtime(segment-set dir) = zero time, want the manifest.json mtime")
	}
	if !got.Equal(mtime) {
		t.Fatalf("statGraphMtime(segment-set dir) = %v, want %v", got, mtime)
	}
}

// TestStatGraphMtime_SingleFileParity guards the single-gen-file (and legacy
// flat) case stays byte-identical to the pre-fix os.Stat behavior.
func TestStatGraphMtime_SingleFileParity(t *testing.T) {
	stateDir := t.TempDir()
	doc := &graph.Document{Repo: "single", Entities: []graph.Entity{
		{ID: "aaaa0001", QualifiedName: "p.A", Kind: "function", Name: "A"},
	}}
	fbPath := filepath.Join(stateDir, graph.GenFileName(1))
	if err := fbwriter.WriteAtomic(fbPath, doc); err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteCurrentPointer(stateDir, graph.GenFileName(1)); err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(fbPath)
	if err != nil {
		t.Fatal(err)
	}

	got := statGraphMtime(stateDir)
	if got.IsZero() {
		t.Fatal("statGraphMtime(single-file dir) = zero time, want the .fb mtime (parity)")
	}
	if !got.Equal(want.ModTime()) {
		t.Fatalf("statGraphMtime(single-file dir) = %v, want %v (parity)", got, want.ModTime())
	}
}

// TestStatGraphMtime_Absent guards a never-indexed dir still reads zero.
func TestStatGraphMtime_Absent(t *testing.T) {
	stateDir := t.TempDir()
	if got := statGraphMtime(stateDir); !got.IsZero() {
		t.Fatalf("statGraphMtime(never-indexed dir) = %v, want zero", got)
	}
}
