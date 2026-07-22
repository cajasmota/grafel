// Package graph — reader_open.go is the segment-aware reader-open seam for the
// #5890 gen-dir read substrate (#5901). It turns a state dir (or a resolved
// gen dir) into an fbreader.GraphView WITHOUT the caller needing to know
// whether the graph is a single mmap'd file or N segment mmaps:
//
//   - single-file / legacy flat / absent → fbreader.Open (today's *Reader,
//     byte-identical — the common path is completely unchanged).
//   - segment-set → fbreader.OpenSegmentsWithRanges (a *MultiReader), threading
//     the manifest's per-segment [MinKey,MaxKey] so LookupEntityByID skips
//     out-of-range segments (decision 4).
//
// The reader-open does NOT itself enforce the fbversion gate — callers that
// need it (load.go, graph_cache.go) compose it with their existing
// FormatVersionError check, so behaviour for an old-flat-below-fbversion graph
// is unchanged (no fbversion bump in this slice).
package graph

import (
	"path/filepath"

	"github.com/cajasmota/grafel/internal/graph/fbreader"
)

// segmentOpenArgs builds the absolute segment paths + parallel key ranges for
// a validated manifest under genDir, ready to hand to
// fbreader.OpenSegmentsWithRanges. Segment order follows manifest order.
func segmentOpenArgs(m *Manifest, genDir string) (paths []string, ranges []fbreader.KeyRange) {
	paths = make([]string, 0, len(m.Segments))
	ranges = make([]fbreader.KeyRange, 0, len(m.Segments))
	for _, s := range m.Segments {
		// s.File is validated (Manifest.validate) to be a bare seg-NNNN.fb with
		// no separator or "..", so this join can never escape genDir.
		paths = append(paths, filepath.Join(genDir, s.File))
		ranges = append(ranges, fbreader.KeyRange{
			HasEntities: s.EntityCount > 0,
			Min:         s.MinKey,
			Max:         s.MaxKey,
		})
	}
	return paths, ranges
}

// OpenSegmentReader opens the multi-segment graph in genDir (which MUST contain
// a valid manifest.json) as a unified fbreader.GraphView with cross-segment key
// routing. A malformed/hostile manifest, or any segment failing to mmap, yields
// a non-nil error and a nil view. Callers own the returned view's Close.
//
// This is the path-based entry point (for callers that already hold the gen
// dir, e.g. the daemon graph cache keyed by resolved path). ReaderForDir is the
// state-dir-based entry point that resolves single-file vs segment-set first.
func OpenSegmentReader(genDir string) (fbreader.GraphView, error) {
	m, err := ReadManifest(genDir)
	if err != nil {
		return nil, err
	}
	paths, ranges := segmentOpenArgs(m, genDir)
	return fbreader.OpenSegmentsWithRanges(paths, ranges)
}

// ReaderForDir resolves dir's active graph and opens it as an fbreader.GraphView:
//
//   - GraphSingleFile / GraphAbsent → fbreader.Open(desc.Path). This is the
//     UNCHANGED common path — the returned view is the same *Reader over the
//     same file today's fbreader.Open(CurrentGraphPath(dir)) returns, so
//     single-file callers are byte-identical (an absent flat path returns
//     fbreader.Open's usual open error, exactly as before).
//   - GraphSegmentSet → OpenSegmentReader(desc.GenDir), a *MultiReader with the
//     manifest key ranges attached for LookupEntityByID pruning.
//
// A corrupt segment-set manifest surfaces the descriptor's error. This does NOT
// apply the fbversion gate; callers compose it (see loadFBDoc / graph_cache).
func ReaderForDir(dir string) (fbreader.GraphView, error) {
	desc, err := CurrentGraphDescriptor(dir)
	if err != nil {
		return nil, err
	}
	if desc.Kind == GraphSegmentSet {
		paths, ranges := segmentOpenArgs(desc.Manifest, desc.GenDir)
		return fbreader.OpenSegmentsWithRanges(paths, ranges)
	}
	return fbreader.Open(desc.Path)
}
