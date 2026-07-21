package mcp

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// Frozen pre-#5871 reference implementation (float64, per-doc tf maps).
//
// This is a verbatim snapshot of the BM25Index that existed BEFORE the L1
// compaction (postings fold-in). It is the independent oracle for the
// search-parity guard: the new float32 postings-based Search MUST rank the
// same entity IDs in the same order for every query. Keeping the whole old
// index+build+search here means the parity test does not depend on the new
// struct sharing any fields with the old one.
// ---------------------------------------------------------------------------

type oldDocTerms struct {
	tf     map[string]float64
	length float64
}

type oldBM25Index struct {
	docs      []oldDocTerms
	entities  []*graph.Entity
	df        map[string]int
	avgLen    float64
	totalDocs int
	postings  map[string][]int32
}

func oldBuildBM25(doc *graph.Document) *oldBM25Index {
	idx := &oldBM25Index{
		docs:     make([]oldDocTerms, len(doc.Entities)),
		entities: make([]*graph.Entity, len(doc.Entities)),
		df:       make(map[string]int),
		postings: make(map[string][]int32),
	}
	totalLen := 0.0
	for i := range doc.Entities {
		e := &doc.Entities[i]
		idx.entities[i] = e
		d := oldBuildDocTerms(e)
		idx.docs[i] = d
		totalLen += d.length
		for term := range d.tf {
			idx.df[term]++
			idx.postings[term] = append(idx.postings[term], int32(i))
		}
	}
	idx.totalDocs = len(idx.entities)
	if idx.totalDocs > 0 {
		idx.avgLen = totalLen / float64(idx.totalDocs)
	}
	return idx
}

// oldBuildDocTerms mirrors buildDocTerms but writes into oldDocTerms. It reuses
// the package tokenizers/weights (those are unchanged by #5871).
func oldBuildDocTerms(e *graph.Entity) oldDocTerms {
	d := oldDocTerms{tf: map[string]float64{}}
	add := func(s string, weight float64, isDocstring bool) {
		for _, t := range tokenize(s) {
			if isDocstring && stopWords[t] {
				continue
			}
			d.tf[t] += weight
			d.length += weight
		}
	}
	addIdentifier := func(name string) {
		toks := tokenizeIdentifier(name)
		if len(toks) == 0 {
			return
		}
		full := toks[0]
		d.tf[full] += weightLabel
		d.length += weightLabel
		subW := weightLabel * subtokenWeight
		for _, sub := range toks[1:] {
			d.tf[sub] += subW
			d.length += subW
		}
	}
	addIdentifier(e.Name)
	if e.SourceFile != "" {
		stem := strings.TrimSuffix(filepath.Base(e.SourceFile), filepath.Ext(e.SourceFile))
		add(stem, weightFileStem, false)
		dir := filepath.Dir(e.SourceFile)
		dirs := []string{}
		for i := 0; i < 2 && dir != "." && dir != "/" && dir != ""; i++ {
			dirs = append(dirs, filepath.Base(dir))
			dir = filepath.Dir(dir)
		}
		add(strings.Join(dirs, " "), weightPathDirs, false)
	}
	if e.PropLen() > 0 {
		if ds, ok := e.PropLookup("docstring"); ok && ds != "" {
			if len(ds) > docstringLimitChars {
				ds = ds[:docstringLimitChars]
			}
			add(ds, weightDocstring, true)
		}
		if pairs, ok := e.PropLookup("discriminators"); ok && pairs != "" {
			for _, pair := range strings.Split(pairs, ",") {
				eq := strings.IndexByte(pair, '=')
				if eq <= 0 || eq >= len(pair)-1 {
					continue
				}
				varName := pair[:eq]
				literal := pair[eq+1:]
				for _, t := range tokenizeIdentifier(varName) {
					d.tf[t] += weightDiscriminator
					d.length += weightDiscriminator
				}
				for _, t := range tokenize(literal) {
					d.tf[t] += weightDiscriminator
					d.length += weightDiscriminator
				}
				if literal != "" {
					raw := strings.ToLower(literal)
					d.tf[raw] += weightDiscriminator
					d.length += weightDiscriminator
				}
			}
		}
	}
	if e.Kind == string(types.EntityKindChannelBinding) {
		add("channel binding", weightDocstring, false)
		add(e.PropGet("direction"), weightDocstring, false)
		add(e.PropGet("topic"), weightDocstring, false)
		add(e.PropGet("connector"), weightDocstring, false)
	}
	return d
}

func (b *oldBM25Index) Search(query string, limit int) []Hit {
	if b == nil || b.totalDocs == 0 {
		return nil
	}
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	scoreByDoc := make(map[int32]float64)
	for _, t := range terms {
		plist := b.postings[t]
		if len(plist) == 0 {
			continue
		}
		df := b.df[t]
		if df == 0 {
			continue
		}
		idf := math.Log(1.0 + (float64(b.totalDocs)-float64(df)+0.5)/(float64(df)+0.5))
		for _, di := range plist {
			d := b.docs[di]
			tf := d.tf[t]
			lenNorm := 1.0
			if b.avgLen > 0 {
				lenNorm = 1 - bm25B + bm25B*(d.length/b.avgLen)
			}
			scoreByDoc[di] += idf * (tf * (bm25K1 + 1)) / (tf + bm25K1*lenNorm)
		}
	}
	type scoredDoc struct {
		di    int32
		score float64
	}
	scored := make([]scoredDoc, 0, len(scoreByDoc))
	for di, score := range scoreByDoc {
		if score > 0 {
			scored = append(scored, scoredDoc{di: di, score: score})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].di < scored[j].di
	})
	hits := make([]Hit, len(scored))
	for i, sd := range scored {
		hits[i] = Hit{Entity: b.entities[sd.di], Score: sd.score}
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// ---------------------------------------------------------------------------
// Load-bearing test: search parity between the frozen old float64 index and
// the new float32 postings-compacted index. The RANKED ORDER of entity IDs
// must be identical; scores are compared with a float32-scale tolerance
// because tf/length are now stored as float32.
// ---------------------------------------------------------------------------

func TestBM25L1SearchParity(t *testing.T) {
	doc := buildSyntheticDoc(1500)
	oldIdx := oldBuildBM25(doc)
	newIdx := BuildBM25(doc)

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
			got := newIdx.Search(q, lim)
			want := oldIdx.Search(q, lim)
			if len(got) != len(want) {
				t.Fatalf("q=%q lim=%d length mismatch: new=%d old=%d", q, lim, len(got), len(want))
			}
			for i := range got {
				// Ranked order (entity identity) MUST be bit-identical.
				if got[i].Entity.ID != want[i].Entity.ID {
					t.Fatalf("q=%q lim=%d pos=%d ranked-order mismatch: new=%s old=%s",
						q, lim, i, got[i].Entity.ID, want[i].Entity.ID)
				}
				// Score may differ by float32 rounding of tf/length; assert a
				// tight relative tolerance so we still catch algorithmic drift.
				tol := 1e-4 * (1 + math.Abs(want[i].Score))
				if math.Abs(got[i].Score-want[i].Score) > tol {
					t.Fatalf("q=%q lim=%d pos=%d score drift beyond float32 tol: new=%v old=%v (tol=%v)",
						q, lim, i, got[i].Score, want[i].Score, tol)
				}
			}
		}
	}
}

// TestBM25L1StructuralShape asserts the new resident structure: postings carry
// the weighted tf inline (no per-doc tf map probe), df == len(postings[t]), and
// docLen holds a positive length per document. This is the compaction
// invariant that banks the memory win.
func TestBM25L1StructuralShape(t *testing.T) {
	doc := buildSyntheticDoc(500)
	idx := BuildBM25(doc)

	if len(idx.docLen) != idx.totalDocs {
		t.Fatalf("docLen len=%d, want totalDocs=%d", len(idx.docLen), idx.totalDocs)
	}
	for i, l := range idx.docLen {
		if l <= 0 {
			t.Fatalf("docLen[%d] = %v, want > 0", i, l)
		}
	}

	// Cross-check against the frozen oracle: df(term) == len(postings[id]) and
	// each posting's tf matches the old per-doc tf for that (term, doc). #5871
	// L2: postings is now indexed by the interned term ID, so we resolve each
	// term's ID via idx.terms before looking up its postings list.
	old := oldBuildBM25(doc)
	for term, id := range idx.terms {
		plist := idx.postings[id]
		if len(plist) != old.df[term] {
			t.Fatalf("term %q: len(postings)=%d != old df=%d", term, len(plist), old.df[term])
		}
		// Postings must stay sorted by doc index (Search relies on this for the
		// ascending tie-break).
		for i := 1; i < len(plist); i++ {
			if plist[i].doc <= plist[i-1].doc {
				t.Fatalf("term %q: postings not strictly ascending at %d", term, i)
			}
		}
		for _, p := range plist {
			want := float32(old.docs[p.doc].tf[term])
			if p.tf != want {
				t.Fatalf("term %q doc %d: posting tf=%v != float32(old tf)=%v", term, p.doc, p.tf, want)
			}
		}
	}
}
