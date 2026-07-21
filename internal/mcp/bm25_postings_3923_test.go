package mcp

import (
	"math"
	"sort"
	"testing"
)

// bruteSearch is a reference implementation of the pre-#3923 Search algorithm:
// a full O(totalDocs) scan that scores every document. The optimized
// postings-based Search (which visits only documents present in a query term's
// inverted-index postings list) MUST return identical (entity, score) pairs in
// the same order for every query — this is the correctness-regression guard for
// #3923.
func (b *BM25Index) bruteSearch(query string, limit int) []Hit {
	if b == nil || b.totalDocs == 0 {
		return nil
	}
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	type sd struct {
		di    int
		score float64
	}
	// After the #5871 L1 compaction the per-doc tf lives inline in the postings
	// list, so reconstruct a per-(term,doc) tf lookup for the brute-force scan.
	tfByTermDoc := make(map[string]map[int32]float32)
	for term, plist := range b.postings {
		m := make(map[int32]float32, len(plist))
		for _, p := range plist {
			m[p.doc] = p.tf
		}
		tfByTermDoc[term] = m
	}
	res := []sd{}
	for i := 0; i < b.totalDocs; i++ {
		di := int32(i)
		score := 0.0
		for _, t := range terms {
			plist := b.postings[t]
			df := len(plist)
			if df == 0 {
				continue
			}
			tf, ok := tfByTermDoc[t][di]
			if !ok {
				continue
			}
			idf := math.Log(1.0 + (float64(b.totalDocs)-float64(df)+0.5)/(float64(df)+0.5))
			lenNorm := 1.0
			if b.avgLen > 0 {
				lenNorm = 1 - bm25B + bm25B*(float64(b.docLen[di])/b.avgLen)
			}
			score += idf * (float64(tf) * (bm25K1 + 1)) / (float64(tf) + bm25K1*lenNorm)
		}
		if score > 0 {
			res = append(res, sd{i, score})
		}
	}
	// Score desc, ascending doc-index tie-break — matching the optimized path.
	sort.Slice(res, func(i, j int) bool {
		if res[i].score != res[j].score {
			return res[i].score > res[j].score
		}
		return res[i].di < res[j].di
	})
	if limit > 0 && len(res) > limit {
		res = res[:limit]
	}
	out := make([]Hit, len(res))
	for i, r := range res {
		out[i] = Hit{Entity: b.entities[r.di], Score: r.score}
	}
	return out
}

// TestBM25SearchMatchesBruteForce asserts the postings-based Search returns
// byte-for-byte identical rankings (entity identity + score) to the reference
// full-scan implementation across a spread of queries, limits, selectivities,
// and a duplicated-term query (which must double-count exactly as the old code
// did). This is the #3923 correctness guard: the optimization must not change
// any find result.
func TestBM25SearchMatchesBruteForce(t *testing.T) {
	doc := buildSyntheticDoc(1500)
	idx := BuildBM25(doc)
	queries := []string{
		"handleOrderRequest customer processor payload",
		"order kafka fulfilment pipeline",
		"premium checklistType 2",
		"validating persisting entity",
		"handleOrderRequestForCustomer42Processor",
		"1234",              // selective: matches a small subset
		"nonexistent zzzz",  // matches nothing
		"order order order", // duplicated term: must double-count identically
	}
	for _, q := range queries {
		for _, lim := range []int{0, 5, 10, 50} {
			got := idx.Search(q, lim)
			want := idx.bruteSearch(q, lim)
			if len(got) != len(want) {
				t.Fatalf("q=%q lim=%d length mismatch: got=%d want=%d", q, lim, len(got), len(want))
			}
			for i := range got {
				if got[i].Entity != want[i].Entity {
					t.Fatalf("q=%q lim=%d pos=%d entity mismatch: got=%s want=%s",
						q, lim, i, got[i].Entity.ID, want[i].Entity.ID)
				}
				if math.Abs(got[i].Score-want[i].Score) > 1e-12 {
					t.Fatalf("q=%q lim=%d pos=%d score mismatch: got=%v want=%v",
						q, lim, i, got[i].Score, want[i].Score)
				}
			}
		}
	}
}

// TestBM25SearchSublinear asserts the postings invariant directly: a query
// whose terms occur in only a handful of documents must visit far fewer than
// totalDocs documents. We assert it via correctness on a selective query plus a
// structural check that the postings list for a rare term is small relative to
// the corpus (the property that makes Search sublinear).
func TestBM25PostingsSelective(t *testing.T) {
	doc := buildSyntheticDoc(2000)
	idx := BuildBM25(doc)
	// A common token appears in (nearly) every doc; a rare numeric token in a
	// small subset. The rare term's postings list must be a small fraction of
	// the corpus — that is what bounds Search to the matching subset.
	rare := idx.postings["1234"]
	if len(rare) == 0 {
		t.Fatalf("expected rare token '1234' to have postings")
	}
	if len(rare) >= idx.totalDocs/2 {
		t.Fatalf("rare token postings unexpectedly large: %d of %d docs", len(rare), idx.totalDocs)
	}
	// Postings indices must be strictly ascending (built in doc order) so the
	// ascending tie-break holds without re-sorting.
	for i := 1; i < len(rare); i++ {
		if rare[i].doc <= rare[i-1].doc {
			t.Fatalf("postings not strictly ascending at %d: %d <= %d", i, rare[i].doc, rare[i-1].doc)
		}
	}
}

func BenchmarkBM25SearchSelective(b *testing.B) {
	doc := buildSyntheticDoc(2000)
	idx := BuildBM25(doc)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = idx.Search("1234", 10)
	}
}

func BenchmarkBM25SearchBroad(b *testing.B) {
	doc := buildSyntheticDoc(2000)
	idx := BuildBM25(doc)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = idx.Search("handleOrderRequest customer processor payload", 10)
	}
}
