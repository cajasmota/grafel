package mcp

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// ADR-0027 Cutover PR1: the primitive-only serve-side index builds
// (adjacency, calls-adjacency, step-adjacency, top-K PageRank, TESTS-edge
// count) were re-sourced to read off the resident mmap fbreader.Reader instead
// of the materialized graph.Document. Because the Reader holds the same rows in
// the same vector order the Document loader produces, the Reader-sourced build
// MUST be byte-identical (reflect.DeepEqual) to the Document-sourced one. These
// tests build BOTH over one real graph.fb and assert equality.

func prPtr(v float64) *float64 { return &v }

// parityIndexDoc exercises every kind the re-sourced builds branch on: CALLS
// (calls-adjacency), STEP_IN_PROCESS with step_index (step-adjacency), TESTS
// (edge count), plus the count/weight properties feeding edgeWeight and entity
// pagerank scalars (present and absent) for the top-K build.
func parityIndexDoc() *graph.Document {
	mkEnt := func(id, name, kind string, pr *float64) graph.Entity {
		return graph.Entity{
			ID: id, Name: name, QualifiedName: "pkg." + name, Kind: kind,
			SourceFile: "pkg/" + name + ".go", Language: "go", PageRank: pr,
		}
	}
	ents := []graph.Entity{
		mkEnt("id::a", "A", "FUNCTION", prPtr(0.9)),
		mkEnt("id::b", "B", "FUNCTION", prPtr(0.1)),
		mkEnt("id::c", "C", "FUNCTION", nil),
		mkEnt("id::d", "D", "FUNCTION", prPtr(0.5)),
		mkEnt("id::proc", "Proc", "PROCESS", nil),
		mkEnt("id::t", "T", "TEST", nil),
	}

	mkRel := func(from, to, kind string, props map[string]string) graph.Relationship {
		r := graph.Relationship{FromID: from, ToID: to, Kind: kind}
		if props != nil {
			r.PropsReplace(props)
		}
		return r
	}
	rels := []graph.Relationship{
		mkRel("id::a", "id::b", "CALLS", map[string]string{"count": "3"}),
		mkRel("id::a", "id::c", "CALLS", nil),
		mkRel("id::b", "id::c", "CALLS", map[string]string{"weight": "2.5"}),
		// BOTH count and weight, DIFFERENT values — pins edgeWeightFB's
		// count-before-weight precedence (weight must resolve to 7, not 9). A
		// precedence swap in edgeWeightFB makes the Reader-sourced edge weight
		// diverge here and fails TestAdjacencyReaderParity_PR1.
		mkRel("id::b", "id::d", "CALLS", map[string]string{"count": "7", "weight": "9"}),
		mkRel("id::a", "id::d", "REFERENCES", map[string]string{"count": "0"}), // count<=0 -> weight 1.0
		mkRel("id::proc", "id::a", "STEP_IN_PROCESS", map[string]string{"step_index": "1"}),
		mkRel("id::proc", "id::b", "STEP_IN_PROCESS", map[string]string{"step_index": "0"}),
		mkRel("id::t", "id::a", "TESTS", nil),
		mkRel("id::t", "id::b", "TESTS", nil),
	}
	return &graph.Document{Entities: ents, Relationships: rels}
}

// loadParityIndexFixture writes parityIndexDoc() to a temp graph.fb and returns
// both the loader-materialized Document and an open Reader over the same file.
func loadParityIndexFixture(t *testing.T) (*graph.Document, *fbreader.Reader) {
	t.Helper()
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, parityIndexDoc()); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}
	r, err := fbreader.Open(fbPath)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return doc, r
}

func TestAdjacencyReaderParity_PR1(t *testing.T) {
	t.Parallel()
	doc, r := loadParityIndexFixture(t)
	want := buildAdjacency(doc, "repo")
	got := buildAdjacencyFromReader(r, "repo")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("adjacency Reader-sourced != Document-sourced\n got=%#v\nwant=%#v", got, want)
	}
}

func TestCallsAdjacencyReaderParity_PR1(t *testing.T) {
	t.Parallel()
	doc, r := loadParityIndexFixture(t)
	want := buildCallsAdjacency(doc)
	got := buildCallsAdjacencyFromReader(r)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callsAdjacency Reader-sourced != Document-sourced\n got=%#v\nwant=%#v", got, want)
	}
	// Spot-check the public Get contract too (sorted callee ids).
	for _, id := range []string{"id::a", "id::b", "id::c"} {
		if !reflect.DeepEqual(got.Get(id), want.Get(id)) {
			t.Errorf("Get(%q): %v != %v", id, got.Get(id), want.Get(id))
		}
	}
}

func TestStepAdjacencyReaderParity_PR1(t *testing.T) {
	t.Parallel()
	doc, r := loadParityIndexFixture(t)
	want := buildStepAdjacency(doc)
	got := buildStepAdjacencyFromReader(r)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stepAdjacency Reader-sourced != Document-sourced\n got=%#v\nwant=%#v", got, want)
	}
}

// TestTopKPageRankReaderParity_PR1 proves buildTopKPageRankFromReader is
// byte-identical to buildTopKPageRank for whatever PageRank values are baked
// directly into this fixture's graph.fb — it does NOT prove production
// parity, because getTopKPageRank no longer calls the Reader-sourced builder
// (see buildTopKPageRankFromReader's doc comment and the post-PR1 regression
// fix in getTopKPageRank / state.go). In production the FB Pagerank() scalar
// is a permanent sentinel; real PageRank only ever reaches lr.Doc via the
// group-algo overlay. TestGetTopKPageRank_OverlayOrder_NotReaderSentinel
// (topk_pagerank_overlay_test.go) covers that overlay-aware production path.
func TestTopKPageRankReaderParity_PR1(t *testing.T) {
	t.Parallel()
	doc, r := loadParityIndexFixture(t)
	for _, k := range []int{1, 3, 64} {
		want := buildTopKPageRank(doc, k)
		got := buildTopKPageRankFromReader(r, k)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("topKPageRank(k=%d) Reader-sourced != Document-sourced\n got=%v\nwant=%v", k, got, want)
		}
	}
}

// TestTestsEdgeCountReaderParity_PR1 mirrors the reload-loop count: TESTS-kind
// edges tallied off the Reader must equal the Document tally.
func TestTestsEdgeCountReaderParity_PR1(t *testing.T) {
	t.Parallel()
	doc, r := loadParityIndexFixture(t)
	want := 0
	for i := range doc.Relationships {
		if doc.Relationships[i].Kind == "TESTS" {
			want++
		}
	}
	got := 0
	r.IterateRelationships(func(rel *fb.Relationship) bool {
		if string(rel.Kind()) == "TESTS" {
			got++
		}
		return true
	})
	if got != want {
		t.Fatalf("TESTS-edge count Reader=%d Document=%d", got, want)
	}
}
