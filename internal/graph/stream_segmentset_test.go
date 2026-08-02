package graph_test

// Segment-set coverage for the streaming prior-graph reader (#5954 item 16).
//
// OpenGraphStream has a dedicated branch for GraphSegmentSet, and until this
// file existed that branch was executed by nothing: a mutant that opened only
// desc.Segments[0] instead of the whole segment set survived the entire
// internal/graph AND cmd/grafel suites. On a segment-set graph that mutant
// carries forward only the records that happen to live in segment 0 and drops
// everything anchored in later segments — a #6085-class silent relationship
// loss on a real path.
//
// The assertion is the same one the single-file fixture makes: MULTISET parity
// in BOTH directions against LoadGraphFromDir over the same dir, so a stream
// that returns a subset reports LOST rather than quietly passing. Plus an
// explicit non-vacuity gate — a fixture whose records all land in one segment
// would assert nothing about the segment-set branch at all.

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// splitDocIntoSegments partitions doc's records into nSeg segment documents.
//
// Entities are sorted by ID and cut into contiguous, disjoint runs (the
// FlatBuffers `(key)` requirement the segment writer imposes); relationships
// are dealt round-robin so no segment holds them all. The result is a segment
// set in which EVERY segment carries both entity and relationship rows, which
// is what makes the "reads only segment 0" mutation observable.
func splitDocIntoSegments(doc *graph.Document, nSeg int) []*graph.Document {
	ents := append([]graph.Entity(nil), doc.Entities...)
	sort.Slice(ents, func(i, j int) bool { return ents[i].ID < ents[j].ID })

	segs := make([]*graph.Document, nSeg)
	for i := range segs {
		segs[i] = &graph.Document{Version: doc.Version, Repo: doc.Repo}
	}
	per := (len(ents) + nSeg - 1) / nSeg
	for i := 0; i < nSeg; i++ {
		lo := i * per
		if lo > len(ents) {
			lo = len(ents)
		}
		hi := lo + per
		if hi > len(ents) {
			hi = len(ents)
		}
		segs[i].Entities = ents[lo:hi]
	}
	for i, r := range doc.Relationships {
		s := segs[i%nSeg]
		s.Relationships = append(s.Relationships, r)
	}
	return segs
}

// writeSegmentedStreamFixture lays the shared stream fixture down as an
// nSeg-segment generation and returns the state dir. It fails the test if the
// fixture degenerates to a single populated segment.
func writeSegmentedStreamFixture(t *testing.T, nSeg int) string {
	t.Helper()
	dir := t.TempDir()
	segs := splitDocIntoSegments(streamFixtureDoc(), nSeg)

	populatedEnt, populatedRel := 0, 0
	for _, s := range segs {
		if len(s.Entities) > 0 {
			populatedEnt++
		}
		if len(s.Relationships) > 0 {
			populatedRel++
		}
	}
	if populatedEnt < 2 || populatedRel < 2 {
		t.Fatalf("fixture is vacuous for a segment-set test: %d/%d segments carry entities/relationships, need >=2 of each",
			populatedEnt, populatedRel)
	}

	genDir := writeSegmentSet(t, dir, 7, segs)

	// Sanity: the descriptor must actually resolve to a segment set, or this
	// test exercises the single-file branch and proves nothing.
	desc, err := graph.CurrentGraphDescriptor(dir)
	if err != nil {
		t.Fatalf("CurrentGraphDescriptor: %v", err)
	}
	if desc.Kind != graph.GraphSegmentSet {
		t.Fatalf("descriptor Kind = %v, want GraphSegmentSet (gen dir %s)", desc.Kind, genDir)
	}
	if got := len(desc.Manifest.Segments); got != nSeg {
		t.Fatalf("manifest has %d segments, want %d", got, nSeg)
	}
	if desc.GenDir != filepath.Clean(genDir) {
		t.Fatalf("descriptor GenDir = %q, want %q", desc.GenDir, genDir)
	}
	return dir
}

// TestGraphStream_SegmentSetRecordParity is the mutation gate on the
// GraphSegmentSet branch of OpenGraphStream: streaming a multi-segment graph
// must yield exactly the record multiset LoadGraphFromDir materialises from the
// same dir. Opening fewer segments than the manifest names shows up here as
// LOST entities and LOST relationships.
func TestGraphStream_SegmentSetRecordParity(t *testing.T) {
	t.Parallel()
	dir := writeSegmentedStreamFixture(t, 3)

	want, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	if len(want.Entities) == 0 || len(want.Relationships) == 0 {
		t.Fatalf("segment-set load produced %d entities / %d relationships — the parity check would be vacuous",
			len(want.Entities), len(want.Relationships))
	}

	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream: %v", err)
	}
	defer s.Close()

	var gotEnts, gotRels []string
	s.EachEntity(func(e graph.Entity) bool { gotEnts = append(gotEnts, entityKeyForTest(e)); return true })
	s.EachRelationship(func(r graph.Relationship) bool { gotRels = append(gotRels, relKeyForTest(r)); return true })

	var wantEnts, wantRels []string
	for _, e := range want.Entities {
		wantEnts = append(wantEnts, entityKeyForTest(e))
	}
	for _, r := range want.Relationships {
		wantRels = append(wantRels, relKeyForTest(r))
	}

	if lost, invented := diffMultisets(wantEnts, gotEnts); len(lost) > 0 || len(invented) > 0 {
		t.Errorf("segment-set entity parity broken: stream LOST %d, INVENTED %d\n lost=%v\n invented=%v",
			len(lost), len(invented), first(lost, 5), first(invented, 5))
	}
	if lost, invented := diffMultisets(wantRels, gotRels); len(lost) > 0 || len(invented) > 0 {
		t.Errorf("segment-set relationship parity broken: stream LOST %d, INVENTED %d\n lost=%v\n invented=%v",
			len(lost), len(invented), first(lost, 5), first(invented, 5))
	}

	if s.EntityCount() != len(want.Entities) {
		t.Errorf("EntityCount()=%d want %d", s.EntityCount(), len(want.Entities))
	}
	if s.RelationshipCount() != len(want.Relationships) {
		t.Errorf("RelationshipCount()=%d want %d", s.RelationshipCount(), len(want.Relationships))
	}
}

// A segment-set stream must be re-iterable exactly like the single-file one —
// the merge walks entities twice.
func TestGraphStream_SegmentSetIsReIterable(t *testing.T) {
	t.Parallel()
	dir := writeSegmentedStreamFixture(t, 3)
	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream: %v", err)
	}
	defer s.Close()

	collect := func() ([]string, []string) {
		var e, r []string
		s.EachEntity(func(x graph.Entity) bool { e = append(e, entityKeyForTest(x)); return true })
		s.EachRelationship(func(x graph.Relationship) bool { r = append(r, relKeyForTest(x)); return true })
		return e, r
	}
	e1, r1 := collect()
	e2, r2 := collect()
	if len(e1) == 0 || len(r1) == 0 {
		t.Fatal("segment-set fixture produced no records — the re-iteration check is vacuous")
	}
	if lost, invented := diffMultisets(e1, e2); len(lost) > 0 || len(invented) > 0 {
		t.Errorf("entity pass 2 diverged: lost=%d invented=%d", len(lost), len(invented))
	}
	if lost, invented := diffMultisets(r1, r2); len(lost) > 0 || len(invented) > 0 {
		t.Errorf("relationship pass 2 diverged: lost=%d invented=%d", len(lost), len(invented))
	}
}

// GraphStream is exported, so use-after-Close must not panic. Close nils the
// view; every accessor has to notice rather than dereference it.
func TestGraphStream_UseAfterCloseDoesNotPanic(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		open func(t *testing.T) string
	}{
		{"single-file", func(t *testing.T) string { return writeStreamFixture(t) }},
		{"segment-set", func(t *testing.T) string { return writeSegmentedStreamFixture(t, 3) }},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := graph.OpenGraphStream(tc.open(t))
			if err != nil {
				t.Fatalf("OpenGraphStream: %v", err)
			}
			if s.EntityCount() == 0 {
				t.Fatal("fixture is empty; the post-close comparison would be vacuous")
			}
			if err := s.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			// Each of these dereferenced a nil view before the guard landed.
			if got := s.EntityCount(); got != 0 {
				t.Errorf("EntityCount() after Close = %d, want 0", got)
			}
			if got := s.RelationshipCount(); got != 0 {
				t.Errorf("RelationshipCount() after Close = %d, want 0", got)
			}
			n := 0
			s.EachEntity(func(graph.Entity) bool { n++; return true })
			s.EachRelationship(func(graph.Relationship) bool { n++; return true })
			if n != 0 {
				t.Errorf("walked %d records after Close, want 0", n)
			}
			if err := s.Close(); err != nil {
				t.Errorf("second Close: %v", err)
			}
		})
	}
}
