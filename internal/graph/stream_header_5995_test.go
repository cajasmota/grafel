package graph_test

// Parity tests for the header/kind accessors GraphStream grew for #5995
// (`grafel status` reads a graph's header and entity kinds without
// materialising it).
//
// The contract is the same one the rest of stream_test.go locks down: what the
// stream reports must equal what LoadGraphFromDir would have put on the
// Document. A status command that streams a DIFFERENT ref, sha or kind tally
// than the loader reported is worse than a slow one.

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// writeSegmentedHeaderFixture lays headerFixtureDoc down as an nSeg-segment
// generation, stamping the header metadata onto EVERY segment (the MultiReader
// sources the header from segment 0; stamping all of them means the test
// cannot pass by accident of which segment is read).
//
// Segment-set coverage matters specifically for EachEntityKind: it indexes
// s.view.EntityAt(i) by GLOBAL index across a key-routed multi-segment view,
// which is the #6080 / task-16 failure class — an accessor that reads only
// segment 0, or mis-routes the index, silently undercounts and `grafel status`
// would report a smaller endpoint/flow tally with no error anywhere.
func writeSegmentedHeaderFixture(t *testing.T, nSeg int) (dir string, doc *graph.Document) {
	t.Helper()
	dir = t.TempDir()
	doc = headerFixtureDoc()

	segs := splitDocIntoSegments(doc, nSeg)
	populated := 0
	for _, s := range segs {
		if len(s.Entities) > 0 {
			populated++
		}
		s.GeneratedAt = doc.GeneratedAt
		s.IndexedRef = doc.IndexedRef
		s.IndexedSHA = doc.IndexedSHA
		s.IsWorktree = doc.IsWorktree
		s.CoverageStatus = doc.CoverageStatus
	}
	if populated < 2 {
		t.Fatalf("fixture is vacuous: only %d/%d segments carry entities", populated, nSeg)
	}
	writeSegmentSet(t, dir, 7, segs)

	desc, err := graph.CurrentGraphDescriptor(dir)
	if err != nil {
		t.Fatalf("CurrentGraphDescriptor: %v", err)
	}
	if desc.Kind != graph.GraphSegmentSet {
		t.Fatalf("descriptor Kind = %v, want GraphSegmentSet — the test would exercise the single-file branch", desc.Kind)
	}
	return dir, doc
}

// TestGraphStream_SegmentSetHeaderAndKinds is the segment-set gate on the
// accessors #5995 added. Both must agree with LoadGraphFromDir over the SAME
// multi-segment dir: an accessor that reads one segment shows up here as a
// short kind tally, and one that misses the header shows up as empty metadata.
func TestGraphStream_SegmentSetHeaderAndKinds(t *testing.T) {
	t.Parallel()
	dir, _ := writeSegmentedHeaderFixture(t, 3)

	loaded, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	want := map[string]int{}
	for _, e := range loaded.Entities {
		want[e.Kind]++
	}

	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream: %v", err)
	}
	defer s.Close()

	h := s.Header()
	if h.IndexedRef != loaded.IndexedRef || h.IndexedSHA != loaded.IndexedSHA ||
		h.IsWorktree != loaded.IsWorktree || h.RepoTag != loaded.Repo ||
		h.CoverageStatus != loaded.CoverageStatus {
		t.Fatalf("segment-set header = %+v, loader ref=%q sha=%q worktree=%v repo=%q coverage=%q",
			h, loaded.IndexedRef, loaded.IndexedSHA, loaded.IsWorktree, loaded.Repo, loaded.CoverageStatus)
	}

	got := map[string]int{}
	s.EachEntityKind(func(k string) bool { got[k]++; return true })
	if len(got) != len(want) {
		t.Fatalf("segment-set kind sets differ: stream %v, loader %v", got, want)
	}
	total := 0
	for k, n := range want {
		if got[k] != n {
			t.Errorf("segment-set kind %q: stream %d, loader %d", k, got[k], n)
		}
		total += n
	}
	if total != len(loaded.Entities) || total == 0 {
		t.Fatalf("loader tally %d does not cover its %d entities", total, len(loaded.Entities))
	}

	// DocStats must sum across every segment, not report segment 0's counts.
	if st := s.DocStats(); st.Entities != loaded.Stats.Entities {
		t.Errorf("segment-set DocStats.Entities = %d, loader Stats.Entities = %d", st.Entities, loaded.Stats.Entities)
	}
}

// TestGraphStream_TwoSegmentKindTallyIsNotSegmentZero is the explicit
// non-vacuity gate: with two segments, the tally must exceed what segment 0
// alone holds, so an accessor that stopped at the first segment cannot pass.
func TestGraphStream_TwoSegmentKindTallyIsNotSegmentZero(t *testing.T) {
	t.Parallel()
	dir, doc := writeSegmentedHeaderFixture(t, 2)

	segs := splitDocIntoSegments(doc, 2)
	seg0 := len(segs[0].Entities)
	if seg0 == 0 || seg0 == len(doc.Entities) {
		t.Fatalf("split is vacuous: segment 0 holds %d of %d entities", seg0, len(doc.Entities))
	}

	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream: %v", err)
	}
	defer s.Close()

	n := 0
	s.EachEntityKind(func(string) bool { n++; return true })
	if n != len(doc.Entities) {
		t.Fatalf("EachEntityKind visited %d kinds, want %d (segment 0 alone holds %d)", n, len(doc.Entities), seg0)
	}
}

// headerFixtureDoc is streamFixtureDoc plus populated header metadata and a
// kind mix (the loader's fixture is single-kind, which cannot distinguish a
// correct tally from a constant).
func headerFixtureDoc() *graph.Document {
	doc := streamFixtureDoc()
	doc.Repo = "acme/widgets"
	doc.IndexedRef = "feature/streaming"
	doc.IndexedSHA = "0123456789ab"
	doc.IsWorktree = true
	doc.CoverageStatus = "partial"
	doc.Entities = append(doc.Entities,
		graph.Entity{ID: "h1", Name: "GetUser", Kind: "http_endpoint_definition", SourceFile: "h.go"},
		graph.Entity{ID: "h2", Name: "CallUser", Kind: "http_endpoint_call", SourceFile: "h.go"},
		graph.Entity{ID: "h3", Name: "Legacy", Kind: "http_endpoint", SourceFile: "h.go"},
		graph.Entity{ID: "p1", Name: "Ingest", Kind: "process", SourceFile: "p.go"},
		graph.Entity{ID: "p2", Name: "Batch", Kind: "SCOPE.ProcessFlow", SourceFile: "p.go"},
	)
	return doc
}

func writeHeaderFixture(t *testing.T) (dir string, doc *graph.Document) {
	t.Helper()
	dir = t.TempDir()
	doc = headerFixtureDoc()
	if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
	return dir, doc
}

// TestGraphStream_HeaderMatchesLoadedDocument: Header() must report exactly the
// git/repo metadata LoadGraphFromDir puts on the Document.
func TestGraphStream_HeaderMatchesLoadedDocument(t *testing.T) {
	t.Parallel()
	dir, _ := writeHeaderFixture(t)

	loaded, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}

	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream: %v", err)
	}
	defer s.Close()
	h := s.Header()

	if h.IndexedRef != loaded.IndexedRef {
		t.Errorf("IndexedRef = %q, loader = %q", h.IndexedRef, loaded.IndexedRef)
	}
	if h.IndexedSHA != loaded.IndexedSHA {
		t.Errorf("IndexedSHA = %q, loader = %q", h.IndexedSHA, loaded.IndexedSHA)
	}
	if h.IsWorktree != loaded.IsWorktree {
		t.Errorf("IsWorktree = %v, loader = %v", h.IsWorktree, loaded.IsWorktree)
	}
	if h.RepoTag != loaded.Repo {
		t.Errorf("RepoTag = %q, loader Repo = %q", h.RepoTag, loaded.Repo)
	}
	if h.CoverageStatus != loaded.CoverageStatus {
		t.Errorf("CoverageStatus = %q, loader = %q", h.CoverageStatus, loaded.CoverageStatus)
	}
	// The fixture sets every field non-zero, so an accessor that returned a
	// zero GraphMeta would be caught above only if the loader also zeroed it.
	if h.IndexedRef == "" || h.IndexedSHA == "" {
		t.Fatalf("header came back empty: %+v", h)
	}
}

// TestGraphStream_HeaderJSONFallback: the graph.json path has no mmap header,
// so Header() reconstitutes from the materialised Document. It must agree with
// the .fb path on the same content.
func TestGraphStream_HeaderJSONFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := headerFixtureDoc()
	if err := writeJSONGraphForTest(t, filepath.Join(dir, "graph.json"), doc); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream (json): %v", err)
	}
	defer s.Close()
	h := s.Header()
	if h.IndexedRef != doc.IndexedRef || h.IndexedSHA != doc.IndexedSHA ||
		h.IsWorktree != doc.IsWorktree || h.RepoTag != doc.Repo ||
		h.CoverageStatus != doc.CoverageStatus {
		t.Fatalf("json fallback header = %+v, want ref=%q sha=%q worktree=%v repo=%q coverage=%q",
			h, doc.IndexedRef, doc.IndexedSHA, doc.IsWorktree, doc.Repo, doc.CoverageStatus)
	}
}

// TestGraphStream_EachEntityKindMatchesFullWalk: the kind-only walk must visit
// the same kinds, in the same multiplicity, as the full entity walk — and
// therefore as the loaded Document.
func TestGraphStream_EachEntityKindMatchesFullWalk(t *testing.T) {
	t.Parallel()
	dir, _ := writeHeaderFixture(t)

	loaded, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	want := map[string]int{}
	for _, e := range loaded.Entities {
		want[e.Kind]++
	}

	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream: %v", err)
	}
	defer s.Close()

	got := map[string]int{}
	s.EachEntityKind(func(k string) bool { got[k]++; return true })

	if len(got) != len(want) {
		t.Fatalf("kind sets differ: stream %v, loader %v", got, want)
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("kind %q: stream %d, loader %d", k, got[k], n)
		}
	}
	// Guard against a walk that visits nothing (which would satisfy a
	// one-sided comparison against an empty map).
	if got["http_endpoint_definition"] != 1 || got["SCOPE.ProcessFlow"] != 1 {
		t.Fatalf("expected the seeded status kinds to be visited, got %v", got)
	}
}

// TestGraphStream_EachEntityKindEarlyReturn: returning false stops the walk.
func TestGraphStream_EachEntityKindEarlyReturn(t *testing.T) {
	t.Parallel()
	dir, _ := writeHeaderFixture(t)
	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream: %v", err)
	}
	defer s.Close()
	n := 0
	s.EachEntityKind(func(string) bool { n++; return n < 3 })
	if n != 3 {
		t.Fatalf("early return visited %d kinds, want 3", n)
	}
	if s.EntityCount() <= 3 {
		t.Fatalf("fixture too small (%d entities) to prove early return", s.EntityCount())
	}
}

// TestGraphStream_DocStatsMatchesLoadedDocument: DocStats must reproduce the
// loader's Stats block, INCLUDING the fact that the .fb formats carry no file
// count (Stats.Files is zero there) while graph.json does.
func TestGraphStream_DocStatsMatchesLoadedDocument(t *testing.T) {
	t.Parallel()

	// .fb path.
	dir, _ := writeHeaderFixture(t)
	loaded, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	s, err := graph.OpenGraphStream(dir)
	if err != nil {
		t.Fatalf("OpenGraphStream: %v", err)
	}
	st := s.DocStats()
	s.Close()
	if st.Files != loaded.Stats.Files {
		t.Errorf("fb Stats.Files = %d, loader = %d", st.Files, loaded.Stats.Files)
	}
	if st.Entities != loaded.Stats.Entities || st.Relationships != loaded.Stats.Relationships {
		t.Errorf("fb Stats counts = %d/%d, loader = %d/%d",
			st.Entities, st.Relationships, loaded.Stats.Entities, loaded.Stats.Relationships)
	}

	// graph.json path: a persisted file count must survive.
	jdir := t.TempDir()
	jdoc := headerFixtureDoc()
	jdoc.Stats = graph.Stats{Files: 41, Entities: len(jdoc.Entities), Relationships: len(jdoc.Relationships)}
	if err := writeJSONGraphForTest(t, filepath.Join(jdir, "graph.json"), jdoc); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
	js, err := graph.OpenGraphStream(jdir)
	if err != nil {
		t.Fatalf("OpenGraphStream (json): %v", err)
	}
	defer js.Close()
	if got := js.DocStats().Files; got != 41 {
		t.Fatalf("json Stats.Files = %d, want 41", got)
	}
}
