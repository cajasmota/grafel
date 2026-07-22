package graph_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// TestCurrentGraphMtime_SegmentSet is the RED test for #5915 J2 slice-3: a
// segment-set repo (graph.<gen>/ dir + manifest.json, no flat graph.fb) must
// report the manifest.json mtime — the pre-fix os.Stat(CurrentGraphPath(dir))
// pattern this helper replaces would report ok=false (mtime absent) because
// CurrentGraphPath only ever names a flat .fb path.
func TestCurrentGraphMtime_SegmentSet(t *testing.T) {
	dir := t.TempDir()
	genDir := writeSegmentSet(t, dir, 7, threeSegDocs())

	mt, ok := graph.CurrentGraphMtime(dir)
	if !ok {
		t.Fatal("CurrentGraphMtime(segment-set dir) ok = false, want true")
	}
	manifestInfo, err := os.Stat(filepath.Join(genDir, graph.ManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !mt.Equal(manifestInfo.ModTime()) {
		t.Fatalf("CurrentGraphMtime = %v, want manifest mtime %v", mt, manifestInfo.ModTime())
	}
}

// TestCurrentGraphMtime_SingleFileParity guards that the single-gen-file (and
// legacy flat) case reports the resolved .fb file's own mtime — byte-identical
// to the pre-fix os.Stat(CurrentGraphPath(dir)) behavior.
func TestCurrentGraphMtime_SingleFileParity(t *testing.T) {
	dir := t.TempDir()
	flat := filepath.Join(dir, "graph.fb")
	if err := os.WriteFile(flat, []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	mt, ok := graph.CurrentGraphMtime(dir)
	if !ok {
		t.Fatal("CurrentGraphMtime(flat) ok = false, want true")
	}
	fi, err := os.Stat(flat)
	if err != nil {
		t.Fatal(err)
	}
	if !mt.Equal(fi.ModTime()) {
		t.Fatalf("CurrentGraphMtime = %v, want %v", mt, fi.ModTime())
	}
}

// TestCurrentGraphMtime_Absent guards a never-indexed repo reports ok=false.
func TestCurrentGraphMtime_Absent(t *testing.T) {
	dir := t.TempDir()
	if _, ok := graph.CurrentGraphMtime(dir); ok {
		t.Fatal("CurrentGraphMtime(never-indexed dir) ok = true, want false")
	}
}
