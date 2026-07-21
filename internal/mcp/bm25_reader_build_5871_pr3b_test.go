package mcp

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// Memory epic #5850, Path P PR3b / issue #5871 L4: BuildBM25FromReader builds
// the (already L1-compacted) BM25 index by iterating the resident mmap Reader
// instead of lr.Doc.Entities, and the index retains NO *graph.Entity — only the
// int32 vector index per doc, resolved to an entity on demand at Search-return
// time. This is the FLIP PREREQUISITE: post-flip lr.Doc is emptied, so
// BuildBM25(lr.Doc) would build an EMPTY index; the Reader still holds every row.
//
// These tests build BOTH the Document-sourced and Reader-sourced indexes over
// one real graph.fb and prove they are byte-equal (postings / docLen / tokens)
// and produce the same ranked search results.

// loadBM25RichFixture writes a BM25-rich synthetic Document (names, source
// files, docstrings, discriminators) to a graph.fb, loads it back, and opens a
// Reader over the SAME file — so the Document rows and the Reader rows are
// byte-identical and in the same vector order.
func loadBM25RichFixture(t *testing.T, nEnt int) (*graph.Document, *fbreader.Reader) {
	t.Helper()
	dir := t.TempDir()
	fbPath := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(fbPath, buildSyntheticDoc(nEnt)); err != nil {
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

// TestBM25ReaderBuildParity_PR3b is the load-bearing parity test: the index
// built from the Reader is byte-equal to the index built from the Document
// (same postings / docLen / avgLen / totalDocs), and yields the SAME ranked
// search results (entity-ID order + scores) on the same fixture.
func TestBM25ReaderBuildParity_PR3b(t *testing.T) {
	doc, r := loadBM25RichFixture(t, 1500)

	idxDoc := BuildBM25(doc)         // flag-OFF / baseline path
	idxRdr := BuildBM25FromReader(r) // flag-ON build path

	// --- Structural byte-parity: postings / docLen / avgLen / totalDocs ---
	if idxRdr.totalDocs != idxDoc.totalDocs {
		t.Fatalf("totalDocs: reader=%d doc=%d", idxRdr.totalDocs, idxDoc.totalDocs)
	}
	if idxRdr.avgLen != idxDoc.avgLen {
		t.Fatalf("avgLen: reader=%v doc=%v", idxRdr.avgLen, idxDoc.avgLen)
	}
	if !reflect.DeepEqual(idxRdr.docLen, idxDoc.docLen) {
		t.Fatalf("docLen slices differ between reader-build and doc-build")
	}
	if !reflect.DeepEqual(idxRdr.entities, idxDoc.entities) {
		t.Fatalf("entities (vector-index) slices differ between reader-build and doc-build")
	}
	// terms dict + postings slice: same term->ID assignment and, for each ID,
	// the same {doc, tf} list in the same order (both are appended in
	// ascending doc order). This is the tokens + weighted-tf byte-parity
	// assertion, extended to the interned structure (#5871 L2): the deterministic
	// sorted-key interning order in foldDocTerms means both builds assign the
	// SAME term->ID mapping given the same entities in the same vector order,
	// so terms and postings must be byte-equal, not just equal-as-sets.
	if !reflect.DeepEqual(idxRdr.terms, idxDoc.terms) {
		t.Fatalf("terms dict (term->ID) differs between reader-build and doc-build")
	}
	if !reflect.DeepEqual(idxRdr.postings, idxDoc.postings) {
		// Narrow down the first divergent term for a useful failure message.
		for term, id := range idxDoc.terms {
			dlist := idxDoc.postings[id]
			rlist := idxRdr.postings[id]
			if !reflect.DeepEqual(rlist, dlist) {
				t.Fatalf("term %q (id=%d) postings differ: reader=%v doc=%v", term, id, rlist, dlist)
			}
		}
		t.Fatalf("postings slices differ (length or term-ID assignment mismatch)")
	}

	// --- Search parity: same ranked entity IDs + scores ---
	// Wire the reader-built index's resolver to a Reader-sourced LabelIndex.at
	// (the production flag-ON resolution path); the doc-built index already has
	// its live-Document resolver from BuildBM25.
	li := BuildLabelIndexFromReader(r, doc)
	idxRdr.resolve = func(vi int32) *graph.Entity { return li.at(vi) }

	queries := []string{
		"handleOrderRequest customer processor payload",
		"order kafka fulfilment pipeline",
		"premium checklistType 2",
		"validating persisting entity",
		"handleOrderRequestForCustomer42Processor",
		"1234",
		"nonexistent zzzz",
		"order order order",
		"orders service handler go",
		"downstream event asynchronously",
	}
	for _, q := range queries {
		for _, lim := range []int{0, 5, 10, 50} {
			gotR := idxRdr.Search(q, lim)
			gotD := idxDoc.Search(q, lim)
			if len(gotR) != len(gotD) {
				t.Fatalf("q=%q lim=%d length: reader=%d doc=%d", q, lim, len(gotR), len(gotD))
			}
			for i := range gotR {
				if gotR[i].Entity.ID != gotD[i].Entity.ID {
					t.Fatalf("q=%q lim=%d pos=%d ranked-order mismatch: reader=%s doc=%s",
						q, lim, i, gotR[i].Entity.ID, gotD[i].Entity.ID)
				}
				if gotR[i].Score != gotD[i].Score {
					t.Fatalf("q=%q lim=%d pos=%d score mismatch: reader=%v doc=%v",
						q, lim, i, gotR[i].Score, gotD[i].Score)
				}
			}
		}
	}
}

// TestBM25FromReaderNoEntityRetention_PR3b is the no-retention assertion: the
// resident BM25Index holds only []int32 vector indices — NOT []*graph.Entity.
// Retaining 427k live entity pointers here would re-pin ~the whole Document
// (~608 MB on the corpus) and defeat the ADR-0027 mmap flip; the materialized
// entities in BuildBM25FromReader must be transient (tokenize, then GC'd).
func TestBM25FromReaderNoEntityRetention_PR3b(t *testing.T) {
	_, r := loadBM25RichFixture(t, 400)
	idx := BuildBM25FromReader(r)

	// The entities field is []int32 (vector indices), not []*graph.Entity.
	if _, ok := interface{}(idx.entities).([]int32); !ok {
		t.Fatalf("entities is %T, want []int32", idx.entities)
	}

	// Structural sweep: no field of the resident index may be a []*graph.Entity
	// (or any slice/array/map of *graph.Entity) — the assertion that the index
	// retains no materialized entities.
	entityPtrType := reflect.TypeOf((*graph.Entity)(nil))
	v := reflect.ValueOf(idx).Elem()
	for i := 0; i < v.NumField(); i++ {
		ft := v.Type().Field(i).Type
		switch ft.Kind() {
		case reflect.Slice, reflect.Array:
			if ft.Elem() == entityPtrType {
				t.Fatalf("field %q is a slice of *graph.Entity — the index must not retain entities", v.Type().Field(i).Name)
			}
		case reflect.Map:
			if ft.Elem() == entityPtrType {
				t.Fatalf("field %q is a map to *graph.Entity — the index must not retain entities", v.Type().Field(i).Name)
			}
		}
	}

	// Sanity: the index still resolves entities on demand once a resolver is set
	// (it is not simply empty). entities length must equal the reader row count.
	if len(idx.entities) != r.EntityCount() {
		t.Fatalf("entities len=%d, want reader EntityCount=%d", len(idx.entities), r.EntityCount())
	}
}

// newReaderBackedRepo builds a LoadedRepo wired for the flag-ON path: a resident
// Reader, a Reader-sourced LabelIndex with readerMu wired (so at() takes the
// SIGBUS-safety mutex and materializes on demand), and a live Doc. handle is
// left nil (a fresh, never-retired generation), which the getBM25 flag-ON gate
// admits (h == nil || !h.readRetired).
func newReaderBackedRepo(t *testing.T, nEnt int) *LoadedRepo {
	t.Helper()
	doc, r := loadBM25RichFixture(t, nEnt)
	lr := &LoadedRepo{Repo: "corpus", Doc: doc, Reader: r}
	li := BuildLabelIndexFromReader(r, doc)
	li.readerMu = &lr.readerMu
	lr.LabelIndex = li
	return lr
}

// TestBM25EvictionRebuild_BothFlags_PR3b proves getBM25's idle-eviction +
// transparent rebuild works on BOTH paths: flag-OFF (rebuild from the Doc) and
// flag-ON (rebuild from the Reader). Results must be byte-identical across the
// warm build and the post-eviction rebuild, and the flag-ON rebuild must source
// from the Reader (not silently collapse to the flag-OFF Doc path).
func TestBM25EvictionRebuild_BothFlags_PR3b(t *testing.T) {
	queries := []string{
		"handleOrderRequest customer processor payload",
		"order kafka fulfilment pipeline",
		"premium checklistType 2",
		"validating persisting entity",
	}

	run := func(t *testing.T, lr *LoadedRepo, flagOn bool) {
		warm := map[string][]string{}
		for _, q := range queries {
			warm[q] = hitKeys(lr.getBM25().Search(q, 10))
		}
		// On the flag-ON path the built index must have a resolver and one
		// vector-index slot per Reader row (proves it built FROM the Reader).
		if flagOn {
			if lr.BM25 == nil || lr.BM25.resolve == nil {
				t.Fatal("flag-ON getBM25 produced no resolver")
			}
			if len(lr.BM25.entities) != lr.Reader.EntityCount() {
				t.Fatalf("flag-ON index entities len=%d, want reader EntityCount=%d",
					len(lr.BM25.entities), lr.Reader.EntityCount())
			}
		}
		// Evict (force idle) and rebuild.
		if !lr.evictBM25IfIdle(time.Minute, lr.bm25LastUse.Add(2*time.Minute)) {
			t.Fatal("expected eviction")
		}
		if lr.BM25 != nil {
			t.Fatal("evicted index field must be nil")
		}
		for _, q := range queries {
			got := hitKeys(lr.getBM25().Search(q, 10))
			want := warm[q]
			if len(got) != len(want) {
				t.Fatalf("q=%q result count changed after rebuild: got %d want %d", q, len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("q=%q rank %d rebuilt result differs\n got=%s\nwant=%s", q, i, got[i], want[i])
				}
			}
		}
	}

	t.Run("flag-off/doc", func(t *testing.T) {
		withServeFromMMap(t, false)
		doc, _ := loadBM25RichFixture(t, 1000)
		lr := &LoadedRepo{Repo: "corpus", Doc: doc}
		run(t, lr, false)
	})

	t.Run("flag-on/reader", func(t *testing.T) {
		withServeFromMMap(t, true)
		lr := newReaderBackedRepo(t, 1000)
		run(t, lr, true)
	})
}

// TestBM25FlagOnReaderMatchesFlagOffDoc_PR3b proves the end-to-end getBM25
// results are identical whether served from the Reader (flag-ON) or the Doc
// (flag-OFF) — the search-parity property at the getter level, not just the
// builder level.
func TestBM25FlagOnReaderMatchesFlagOffDoc_PR3b(t *testing.T) {
	queries := []string{
		"handleOrderRequest customer processor payload",
		"order kafka fulfilment pipeline",
		"premium checklistType 2",
		"orders service handler go",
	}

	// Flag-OFF, Doc-backed repo.
	docWant := func() map[string][]string {
		withServeFromMMap(t, false)
		doc, _ := loadBM25RichFixture(t, 1200)
		lr := &LoadedRepo{Repo: "corpus", Doc: doc}
		out := map[string][]string{}
		for _, q := range queries {
			out[q] = hitKeys(lr.getBM25().Search(q, 10))
		}
		return out
	}()

	// Flag-ON, Reader-backed repo.
	withServeFromMMap(t, true)
	lr := newReaderBackedRepo(t, 1200)
	for _, q := range queries {
		got := hitKeys(lr.getBM25().Search(q, 10))
		want := docWant[q]
		if len(got) != len(want) {
			t.Fatalf("q=%q length: reader=%d doc=%d", q, len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("q=%q rank %d: reader=%s doc=%s", q, i, got[i], want[i])
			}
		}
	}
}
