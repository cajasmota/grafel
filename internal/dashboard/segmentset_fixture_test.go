// segmentset_fixture_test.go — shared segment-set fixture builder for the
// #5915 J2 slice-2 dashboard existence/freshness gate tests.
//
// A SEGMENT-SET repo has NO flat graph.fb: instead `current` points at a
// graph.<gen>/ dir holding seg-NNNN.fb files + manifest.json. The pre-slice-2
// dashboard gates hardcoded os.Stat(graph.CurrentGraphPath(dir)), which only
// ever resolves a flat .fb path, so they silently reported such a repo as
// "never indexed" / mtime-zero. Mirrors the fixture builders already used at
// internal/daemon/state_path_graphfbexists_test.go,
// internal/daemon/deadref_segmentset_test.go and internal/graph/segmentset_test.go.
package dashboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeDashboardSegmentSetFixture hand-builds a 1-segment gen dir
// (graph.<gen>/ with seg-0000.fb + manifest.json) under dir, points
// `current` at it, and stamps manifest.json (the segment-set's atomic commit
// point) with mtime.
func writeDashboardSegmentSetFixture(t *testing.T, dir string, gen uint64, mtime time.Time) {
	t.Helper()
	genDir := filepath.Join(dir, graph.GenDirName(gen))
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
	if !mtime.IsZero() {
		if err := os.Chtimes(manifestPath, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if err := graph.WriteCurrentPointerRaw(dir, graph.GenDirName(gen)); err != nil {
		t.Fatalf("write current: %v", err)
	}
}
