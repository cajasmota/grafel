// Package fbreader — multireader.go adds a segment-aware reader that
// presents N FlatBuffers segment files as one unified graph view (issue
// #5900, part (a) of the #5890 bounded-memory streaming-write epic).
//
// This is pure additive plumbing: it commits to NO on-disk-format
// decision. There is no producer that writes multiple segments yet and
// no gen-dir layout — those are later slices of #5890. MultiReader simply
// takes a set of paths, each of which is a normal finalized graph.fb file
// (same shape single-file Reader.Open already understands, with its
// entity vector sorted-by-key per the FlatBuffers `(key)` attribute), and
// fans reads out across all of them.
//
// Relationship endpoints (from_id/to_id) are stable strings (see
// graph.fbs), so cross-segment resolution needs no forward-ref
// bookkeeping: LookupEntityByID just tries each segment's own
// EntitiesByKey binary search in turn.
package fbreader

import (
	"fmt"

	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
)

// MultiReader opens a fixed set of segment files and exposes the same
// read surface as Reader, transparently fanning operations out across
// every segment:
//
//   - EntityCount / RelationshipCount sum across segments.
//   - LookupEntityByID tries each segment's own sorted-by-key binary
//     search in turn and returns the first hit (O(S·logN) for S
//     segments) — this is how a relationship in segment 0 whose to_id
//     entity lives in segment 2 gets resolved.
//   - IterateEntities / IterateRelationships / FilterEntitiesByKind /
//     IterateRelationshipsFromID / IterateRelationshipsToID chain the
//     per-segment scans in segment order, preserving each segment's own
//     iteration order.
//
// A MultiReader opened over exactly one segment behaves identically to
// the single-file Reader over that same file (same counts, same lookup
// results, same iteration order) — see TestMultiReaderSingleSegmentParity.
type MultiReader struct {
	segs []*Reader
	// ranges, when non-nil, is parallel to segs and carries each segment's
	// entity-ID key range from the gen-dir manifest. LookupEntityByID uses it
	// to SKIP segments that cannot contain the looked-up key, avoiding the O(S)
	// fan-out (decision 4 of #5901). nil ⇒ no routing info ⇒ every segment is
	// searched (the OpenSegments legacy behaviour, unchanged).
	ranges []KeyRange
}

// KeyRange is the entity-ID key coverage of one segment, as recorded in the
// gen-dir manifest (SegmentMeta MinKey/MaxKey + a HasEntities flag derived
// from EntityCount>0). LookupEntityByID consults it to skip segments that
// provably cannot hold a key.
//
// Semantics:
//   - HasEntities==false ⇒ the segment carries NO entities (a pure-relationship
//     segment) ⇒ every entity lookup skips it.
//   - HasEntities==true with Min=="" and Max=="" ⇒ range unknown ⇒ never skip
//     (safe fallback: search it).
//   - Otherwise the (lexicographic, inclusive) [Min,Max] window gates the key.
type KeyRange struct {
	HasEntities bool
	Min         string
	Max         string
}

// contains reports whether id could live in a segment with this key range.
func (kr KeyRange) contains(id string) bool {
	if !kr.HasEntities {
		return false
	}
	if kr.Min == "" && kr.Max == "" {
		return true // unknown bounds — cannot safely skip
	}
	if kr.Min != "" && id < kr.Min {
		return false
	}
	if kr.Max != "" && id > kr.Max {
		return false
	}
	return true
}

// SegmentSearchHook, when non-nil, is invoked with the segment index each time
// LookupEntityByID actually performs a binary search on that segment (i.e. NOT
// skipped by key routing). It is a test/observability seam — nil in production,
// where it adds a single nil-check on the lookup path — set by the routing
// tests to assert out-of-range segments are pruned. Not safe for concurrent
// mutation; set it only from single-threaded test setup.
var SegmentSearchHook func(seg int)

// OpenSegments memory-maps every path in paths (in the given order) and
// returns a MultiReader over all of them. paths must be non-empty. If any
// segment fails to open, every already-opened segment is closed before
// the error is returned (no leaked mappings on a partial failure).
//
// This constructor attaches NO key ranges, so LookupEntityByID fans out across
// every segment (O(S)). Use OpenSegmentsWithRanges to enable manifest-driven
// segment skipping.
func OpenSegments(paths []string) (*MultiReader, error) {
	return OpenSegmentsWithRanges(paths, nil)
}

// OpenSegmentsWithRanges is OpenSegments plus per-segment key ranges (from the
// gen-dir manifest) used to prune LookupEntityByID. When ranges is non-nil it
// MUST have the same length as paths (one range per segment, same order);
// a length mismatch is an error. A nil ranges disables routing (== OpenSegments).
func OpenSegmentsWithRanges(paths []string, ranges []KeyRange) (*MultiReader, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("fbreader: OpenSegments requires at least one path")
	}
	if ranges != nil && len(ranges) != len(paths) {
		return nil, fmt.Errorf("fbreader: OpenSegmentsWithRanges: %d ranges for %d paths", len(ranges), len(paths))
	}
	segs := make([]*Reader, 0, len(paths))
	for _, p := range paths {
		r, err := Open(p)
		if err != nil {
			for _, s := range segs {
				_ = s.Close()
			}
			return nil, fmt.Errorf("fbreader: open segment %s: %w", p, err)
		}
		segs = append(segs, r)
	}
	return &MultiReader{segs: segs, ranges: ranges}, nil
}

// Close releases every segment's underlying mmap. It closes all segments
// even if one returns an error, and reports the first error encountered
// (mirrors Reader.Close's contract of "no field/string may be accessed
// after Close returns" — extended to every segment mapping this
// MultiReader owns).
func (m *MultiReader) Close() error {
	if m == nil {
		return nil
	}
	var firstErr error
	for _, s := range m.segs {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	m.segs = nil
	return firstErr
}

// SegmentCount returns the number of segments backing this reader.
func (m *MultiReader) SegmentCount() int {
	if m == nil {
		return 0
	}
	return len(m.segs)
}

// EntityCount returns the total number of entities across all segments.
func (m *MultiReader) EntityCount() int {
	if m == nil {
		return 0
	}
	n := 0
	for _, s := range m.segs {
		n += s.EntityCount()
	}
	return n
}

// RelationshipCount returns the total number of relationships across all
// segments.
func (m *MultiReader) RelationshipCount() int {
	if m == nil {
		return 0
	}
	n := 0
	for _, s := range m.segs {
		n += s.RelationshipCount()
	}
	return n
}

// LookupEntityByID returns the entity with the given id, or nil. Each
// segment's own FlatBuffers `(key)` binary search is tried in segment
// order; the first hit wins. Callers on a genuinely partitioned corpus
// see O(S·logN) for S (small) segments — this is how a relationship
// whose endpoint lives in a different segment than the relationship
// itself still resolves.
func (m *MultiReader) LookupEntityByID(id string) *fb.Entity {
	if m == nil {
		return nil
	}
	for i, s := range m.segs {
		// Decision 4: when manifest key ranges are attached, skip any segment
		// whose [Min,Max] window cannot contain id (or that holds no entities).
		// This turns the O(S) fan-out into "search only plausible segments".
		if m.ranges != nil && !m.ranges[i].contains(id) {
			continue
		}
		if SegmentSearchHook != nil {
			SegmentSearchHook(i)
		}
		if e := s.LookupEntityByID(id); e != nil {
			return e
		}
	}
	return nil
}

// FindEntityByID is the (value, ok) idiom counterpart to
// LookupEntityByID, mirroring Reader.FindEntityByID.
func (m *MultiReader) FindEntityByID(id string) (*fb.Entity, bool) {
	e := m.LookupEntityByID(id)
	return e, e != nil
}

// EntityAt returns the i-th entity across the concatenation of every
// segment's entity vector, in segment order (segment 0's entities first,
// then segment 1's, etc). Mirrors Reader.EntityAt for the single-segment
// case.
func (m *MultiReader) EntityAt(i int) *fb.Entity {
	if m == nil || i < 0 {
		return nil
	}
	for _, s := range m.segs {
		if i < s.EntityCount() {
			return s.EntityAt(i)
		}
		i -= s.EntityCount()
	}
	return nil
}

// RelationshipAt returns the i-th relationship across the concatenation
// of every segment's relationship vector, in segment order. Mirrors
// Reader.RelationshipAt for the single-segment case.
func (m *MultiReader) RelationshipAt(i int) *fb.Relationship {
	if m == nil || i < 0 {
		return nil
	}
	for _, s := range m.segs {
		if i < s.RelationshipCount() {
			return s.RelationshipAt(i)
		}
		i -= s.RelationshipCount()
	}
	return nil
}

// IterateEntities calls visit for every entity across every segment, in
// segment order (each segment's own vector order preserved). If visit
// returns false, iteration stops early across the whole chain — mirrors
// Reader.IterateEntities.
func (m *MultiReader) IterateEntities(visit func(e *fb.Entity) bool) {
	if m == nil {
		return
	}
	stopped := false
	for _, s := range m.segs {
		if stopped {
			return
		}
		s.IterateEntities(func(e *fb.Entity) bool {
			if !visit(e) {
				stopped = true
				return false
			}
			return true
		})
	}
}

// IterateRelationships calls visit for every relationship across every
// segment, in segment order. If visit returns false, iteration stops
// early across the whole chain — mirrors Reader.IterateRelationships.
func (m *MultiReader) IterateRelationships(visit func(rel *fb.Relationship) bool) {
	if m == nil {
		return
	}
	stopped := false
	for _, s := range m.segs {
		if stopped {
			return
		}
		s.IterateRelationships(func(rel *fb.Relationship) bool {
			if !visit(rel) {
				stopped = true
				return false
			}
			return true
		})
	}
}

// IterateRelationshipsFromID chains each segment's IterateRelationshipsFromID
// scan, in segment order, into a single slice.
func (m *MultiReader) IterateRelationshipsFromID(id string) []*fb.Relationship {
	if m == nil {
		return nil
	}
	out := make([]*fb.Relationship, 0, 8)
	for _, s := range m.segs {
		out = append(out, s.IterateRelationshipsFromID(id)...)
	}
	return out
}

// IterateRelationshipsToID chains each segment's IterateRelationshipsToID
// scan, in segment order, into a single slice.
func (m *MultiReader) IterateRelationshipsToID(id string) []*fb.Relationship {
	if m == nil {
		return nil
	}
	out := make([]*fb.Relationship, 0, 8)
	for _, s := range m.segs {
		out = append(out, s.IterateRelationshipsToID(id)...)
	}
	return out
}

// FilterEntitiesByKind chains each segment's FilterEntitiesByKind scan,
// in segment order, into a single slice.
func (m *MultiReader) FilterEntitiesByKind(kind string) []*fb.Entity {
	if m == nil {
		return nil
	}
	out := make([]*fb.Entity, 0, 8)
	for _, s := range m.segs {
		out = append(out, s.FilterEntitiesByKind(kind)...)
	}
	return out
}

// CommunityCount, CommunityAt, Version, LoadGraphMeta, and LoadAlgoStats
// are Graph-root HEADER fields (schema version, computed-at timestamp,
// Louvain communities, Pass-4 aggregates, ...). Multi-segment corpora do
// not yet have a defined "which segment owns the header" story — that is
// a gen-dir layout decision for a later #5890 slice. Until then these
// delegate to the FIRST segment only, which is exactly correct for the
// single-segment case this issue guarantees parity for, and is a
// reasonable placeholder otherwise (call sites that need true multi-segment
// header semantics are not wired to MultiReader yet).

// CommunityCount returns the number of communities recorded on the first
// segment's header. See the header-fields note above.
func (m *MultiReader) CommunityCount() int {
	if m == nil || len(m.segs) == 0 {
		return 0
	}
	return m.segs[0].CommunityCount()
}

// CommunityAt returns the i-th community from the first segment's
// header. See the header-fields note above.
func (m *MultiReader) CommunityAt(i int) *fb.Community {
	if m == nil || len(m.segs) == 0 {
		return nil
	}
	return m.segs[0].CommunityAt(i)
}

// Version returns the schema version recorded on the first segment's
// header. See the header-fields note above.
func (m *MultiReader) Version() int {
	if m == nil || len(m.segs) == 0 {
		return 0
	}
	return m.segs[0].Version()
}

// LoadGraphMeta returns the header fields of the first segment. See the
// header-fields note above.
func (m *MultiReader) LoadGraphMeta() GraphMeta {
	if m == nil || len(m.segs) == 0 {
		return GraphMeta{}
	}
	return m.segs[0].LoadGraphMeta()
}

// LoadAlgoStats returns the Pass-4 corpus-level aggregates recorded on
// the first segment's header. See the header-fields note above.
func (m *MultiReader) LoadAlgoStats() AlgoStats {
	if m == nil || len(m.segs) == 0 {
		return AlgoStats{}
	}
	return m.segs[0].LoadAlgoStats()
}
