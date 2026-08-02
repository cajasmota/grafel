package graph

// stream.go — the streaming prior-graph reader (#5954 item 16).
//
// LoadGraphFromDir materialises EVERY entity and relationship of a graph.fb
// into Go objects before its caller can look at a single one. On the
// incremental path that is paid for a graph the caller then almost entirely
// copies forward, so the prior graph exists TWICE at the merge point: once as
// the loaded Document's []Entity/[]Relationship backing arrays, and again as
// the merged Document's. GraphStream removes the first copy: rows are decoded
// one at a time straight off the mmap, handed to the caller, and forgotten.
//
// SCOPE, stated plainly so it is not over-read: this makes the prior graph
// STREAMABLE, it does not make the merged document lazy. Rows the merge keeps
// still land on the heap inside the merged Document — they have to, because
// Pass 4 (graph algorithms), external synthesis and the writer all consume
// doc.Entities/doc.Relationships as slices. What is eliminated is the
// DUPLICATE: the prior graph's own backing arrays, plus every row the merge
// drops, which under LoadGraphFromDir was decoded only to be thrown away.
//
// Parity with LoadGraphFromDir is structural, not asserted: EachEntity and
// EachRelationship call the SAME fbEntityToGraphEntity / fbRelToGraphRel
// converters materializeGraphView calls, over the same fbreader.GraphView,
// sharing one stringInterner across both walks exactly as materializeGraphView
// shares one across its two loops. The reserved "\x00id" relationship-identity
// slot (#6085) is therefore lifted out identically, and the mmap view in
// internal/mcp is unaffected — this adds a reader, it changes none.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cajasmota/grafel/internal/graph/fbreader"
)

// GraphStream is a re-iterable, non-materialising read over a persisted graph.
//
// It holds the mmap open for its lifetime, so Close MUST be called. Every
// string handed to a visitor is COPIED out of the mapping (the shared
// converters do that), so records outlive the stream safely.
//
// The zero value is not usable; construct with OpenGraphStream.
type GraphStream struct {
	view fbreader.GraphView

	// si is shared across entity and relationship walks AND across repeated
	// walks, so a relationship endpoint string canonicalises to the same
	// backing array as the entity ID it references — the identical sharing
	// materializeGraphView gets from its single per-load interner. Without
	// this the streamed rows would use MORE memory than the materialised
	// ones, not less.
	si *stringInterner

	// doc is non-nil ONLY for the graph.json fallback, which has no mmap to
	// stream from. Iteration then walks the already-materialised slices. This
	// path wins nothing on memory; it exists so callers get one contract.
	doc *Document
}

// OpenGraphStream opens dir's active graph for streaming.
//
// Resolution mirrors LoadGraphFromDir exactly — segment-set, then single-file
// graph.fb, then graph.json — so a dir that loads today streams today, and a
// dir that fails to load still fails (the incremental path depends on that
// error to fall back to a full reindex rather than persisting a truncated
// graph).
func OpenGraphStream(dir string) (*GraphStream, error) {
	desc, derr := CurrentGraphDescriptor(dir)
	if derr != nil {
		return nil, derr
	}

	if desc.Kind == GraphSegmentSet {
		// Open from desc.Manifest — the manifest CurrentGraphDescriptor already
		// read and validated — rather than via OpenSegmentReader, which would
		// re-read manifest.json off disk. Re-reading opens a window in which a
		// concurrent generation flip lands between the descriptor resolve and
		// the manifest read, yielding a stream over a DIFFERENT generation than
		// the descriptor named. loadSegmentSetDocument (load.go) uses
		// desc.Manifest for exactly this reason; match it.
		paths, ranges := segmentOpenArgs(desc.Manifest, desc.GenDir)
		v, err := fbreader.OpenSegmentsWithRanges(paths, ranges)
		if err != nil {
			return nil, fmt.Errorf("graph.OpenGraphStream: open %s: %w", desc.GenDir, err)
		}
		if err := checkStreamVersion(v); err != nil {
			v.Close()
			return nil, err
		}
		return &GraphStream{view: v, si: newStringInterner()}, nil
	}

	fbPath := CurrentGraphPath(dir)
	if _, err := os.Stat(fbPath); err == nil {
		v, oerr := fbreader.Open(fbPath)
		if oerr != nil {
			return nil, fmt.Errorf("graph.OpenGraphStream: open %s: %w", fbPath, oerr)
		}
		if verr := checkStreamVersion(v); verr != nil {
			v.Close()
			return nil, verr
		}
		return &GraphStream{view: v, si: newStringInterner()}, nil
	}

	jsonPath := filepath.Join(dir, "graph.json")
	if _, err := os.Stat(jsonPath); err == nil {
		doc, jerr := loadJSONDocument(jsonPath)
		if jerr != nil {
			return nil, jerr
		}
		return &GraphStream{doc: doc}, nil
	}

	return nil, fmt.Errorf("graph.OpenGraphStream: neither graph.fb nor graph.json found in %s", dir)
}

// checkStreamVersion applies the same format gate materializeGraphView applies,
// so an incompatible graph.fb is refused at open rather than yielding a stream
// of garbage records.
func checkStreamVersion(v fbreader.GraphView) error {
	if got := v.Version(); got < minSupportedFBFormatVersion {
		return fmt.Errorf("graph.OpenGraphStream: %w",
			&FormatVersionError{Found: got, Required: minSupportedFBFormatVersion})
	}
	return nil
}

// EntityCount reports the number of entity rows the stream will visit.
func (s *GraphStream) EntityCount() int {
	if s == nil {
		return 0
	}
	if s.doc != nil {
		return len(s.doc.Entities)
	}
	if s.view == nil {
		// Closed (Close nils the view). An exported type must not panic on
		// use-after-Close; report an empty stream instead.
		return 0
	}
	return s.view.EntityCount()
}

// RelationshipCount reports the number of relationship rows the stream will
// visit.
func (s *GraphStream) RelationshipCount() int {
	if s == nil {
		return 0
	}
	if s.doc != nil {
		return len(s.doc.Relationships)
	}
	if s.view == nil {
		return 0
	}
	return s.view.RelationshipCount()
}

// EachEntity decodes and visits every entity row in file order. Return false
// from visit to stop early; a later walk still starts from the beginning.
//
// The decoded Entity is byte-identical to the one LoadGraphFromDir would have
// placed at the same index — same converter, same interner discipline.
func (s *GraphStream) EachEntity(visit func(Entity) bool) {
	if s == nil || visit == nil {
		return
	}
	if s.doc != nil {
		for i := range s.doc.Entities {
			if !visit(s.doc.Entities[i]) {
				return
			}
		}
		return
	}
	if s.view == nil {
		// Closed: walk nothing rather than deref a nil view.
		return
	}
	// NOT memoized, deliberately. The indexer walks entities twice (once early
	// to seed the resolver index, once at the merge), so caching the decoded
	// rows to avoid the second decode looks like an obvious win — it was
	// MEASURED as a loss: 7000-file fixture, incremental peak live heap
	// 282.7 MB streaming vs 302.2 MB with an entity cache. The cache array
	// costs more than the duplicated Name/property copies it saves, and it
	// reinstates exactly the prior-graph entity vector this type exists to
	// avoid holding.
	n := s.view.EntityCount()
	for i := 0; i < n; i++ {
		fbEnt := s.view.EntityAt(i)
		if fbEnt == nil {
			// materializeGraphView skips a nil vector slot rather than
			// emitting a zero row; match it so the record SETS agree.
			continue
		}
		if !visit(fbEntityToGraphEntity(fbEnt, s.si)) {
			return
		}
	}
}

// EachRelationship decodes and visits every relationship row in file order.
// Return false from visit to stop early.
func (s *GraphStream) EachRelationship(visit func(Relationship) bool) {
	if s == nil || visit == nil {
		return
	}
	if s.doc != nil {
		for i := range s.doc.Relationships {
			if !visit(s.doc.Relationships[i]) {
				return
			}
		}
		return
	}
	if s.view == nil {
		return
	}
	n := s.view.RelationshipCount()
	for i := 0; i < n; i++ {
		fbRel := s.view.RelationshipAt(i)
		if fbRel == nil {
			continue
		}
		if !visit(fbRelToGraphRel(fbRel, s.si)) {
			return
		}
	}
}

// Close releases the mmap. Safe on a nil receiver and safe to call twice.
func (s *GraphStream) Close() error {
	if s == nil || s.view == nil {
		return nil
	}
	v := s.view
	s.view = nil
	return v.Close()
}
