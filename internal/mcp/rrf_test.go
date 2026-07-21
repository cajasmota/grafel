package mcp

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/embed"
	"github.com/cajasmota/grafel/internal/graph"
)

// fakeBE is a deterministic token-bag backend used by the RRF integration
// test. It mirrors the fakeBackend used in internal/embed but is duplicated
// here because that one is in `_test.go` and not exported.
type fakeBE struct{ dims int }

func (f *fakeBE) Dims() int    { return f.dims }
func (f *fakeBE) Name() string { return "fake" }
func (f *fakeBE) Close() error { return nil }
func (f *fakeBE) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, f.dims)
		for _, tok := range strings.Fields(strings.ToLower(t)) {
			h := sha1.Sum([]byte(tok))
			idx := binary.LittleEndian.Uint32(h[:4]) % uint32(f.dims)
			v[idx] += 1.0
		}
		out[i] = l2NormalizeF(v)
	}
	return out, nil
}

func l2NormalizeF(v []float32) []float32 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return v
	}
	inv := float32(1.0 / sqrt(s))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}

func sqrt(x float64) float64 {
	// std math import already used elsewhere — local copy avoids the import here
	z := x
	for i := 0; i < 16; i++ {
		z = (z + x/z) / 2
	}
	return z
}

func TestFuseRRF_SemanticHitWithoutKeywordOverlap(t *testing.T) {
	t.Parallel()
	// Two entities, neither shares any token with the query
	// "where do we handle authentication" -- but `verifyBearer` is semantically
	// close (shares the token "verify" and "bearer" through its docstring).
	// BM25 returns 0 hits (no overlap with name/path/docstring after stop-words).
	// Semantic returns it via the docstring-token overlap captured in the
	// embed text. RRF fusion should put the auth entity first.
	authEntity :=

		graph.EntityPtr(graph.Entity{
			ID: "auth-1", Name: "verifyBearer", Kind: "function",
			SourceFile: "internal/identity/sessions.go",
		}.WithProperties(map[string]string{
			"docstring": "Verify a bearer token and create an authentication session for the caller.",
		},
		))
	mathEntity :=

		graph.EntityPtr(graph.Entity{
			ID: "math-1", Name: "sumInts", Kind: "function",
			SourceFile: "internal/math/sum.go",
		}.WithProperties(map[string]string{"docstring": "Compute the sum of two integers."}))
	doc := &graph.Document{Entities: []graph.Entity{*authEntity, *mathEntity}}

	// Build BM25 over the doc.
	bm := BuildBM25(doc)
	query := "where do we handle authentication"
	bm25Hits := bm.Search(query, 50)

	// Embed entities and the query via the fake backend.
	be := &fakeBE{dims: 64}
	stateDir := t.TempDir()
	store, _, err := embed.EmbedDocument(context.Background(), doc, "", stateDir, be)
	if err != nil {
		t.Fatalf("EmbedDocument: %v", err)
	}
	qVecs, err := be.Embed(context.Background(), []string{query})
	if err != nil {
		t.Fatal(err)
	}

	semIDs := store.Search(qVecs[0], 10)
	if len(semIDs) == 0 {
		t.Fatal("expected semantic hits, got none")
	}
	// Map IDs back to entities (the way handleQueryGraph does).
	byID := map[string]*graph.Entity{authEntity.ID: &doc.Entities[0], mathEntity.ID: &doc.Entities[1]}
	semHits := make([]Hit, 0, len(semIDs))
	for _, s := range semIDs {
		if e, ok := byID[s.ID]; ok {
			semHits = append(semHits, Hit{Entity: e, Score: s.Score, Source: "semantic"})
		}
	}
	if len(semHits) == 0 || semHits[0].Entity.ID != "auth-1" {
		t.Fatalf("semantic top hit should be auth-1, got %+v", semHits)
	}

	fused := FuseRRF(bm25Hits, semHits)
	if len(fused) == 0 || fused[0].Entity.ID != "auth-1" {
		t.Fatalf("RRF top hit should be auth-1, got %+v", fused)
	}
	if !strings.Contains(fused[0].Source, "semantic") {
		t.Fatalf("RRF top hit Source should record semantic, got %q", fused[0].Source)
	}
	t.Logf("semantic-only RRF hit example: id=%s name=%s score=%.4f source=%s (BM25 returned %d hits for this query)",
		fused[0].Entity.ID, fused[0].Entity.Name, fused[0].Score, fused[0].Source, len(bm25Hits))
}

func TestFuseRRF_BothSourcesRankFusedHigher(t *testing.T) {
	t.Parallel()
	a := &graph.Entity{ID: "a", Name: "AuthCheck"}
	b := &graph.Entity{ID: "b", Name: "AuthVerify"}
	c := &graph.Entity{ID: "c", Name: "LogoutHandler"}
	bm25 := []Hit{{Entity: a, Score: 5}, {Entity: c, Score: 1}}
	sem := []Hit{{Entity: b, Score: 0.9}, {Entity: a, Score: 0.5}}
	fused := FuseRRF(bm25, sem)
	if fused[0].Entity != a {
		t.Fatalf("a should rank first (top in both lists), got %s", fused[0].Entity.ID)
	}
	if fused[0].Source != "bm25+semantic" {
		t.Fatalf("a Source should be bm25+semantic, got %q", fused[0].Source)
	}
}

// TestFuseRRF_DistinctPointersSameID_FuseIntoOne proves the ID-keyed re-key
// (ADR-0027 mmap zero-copy cutover): once ranker hits are materialized
// independently, BM25 and semantic search can hand back two *graph.Entity
// values that are separate allocations (distinct pointers) but describe the
// same logical entity (same ID). A pointer-keyed byEntity map treats those as
// two different results and never fuses them — this test fails against that
// old impl. Keying by Entity.ID instead correctly folds them into one Hit
// with the summed RRF contribution from both rankers.
func TestFuseRRF_DistinctPointersSameID_FuseIntoOne(t *testing.T) {
	t.Parallel()
	// Two independently-allocated *graph.Entity values sharing ID "shared-1".
	// Pointer identity differs (p1 != p2); logical identity (ID) is the same.
	p1 := &graph.Entity{ID: "shared-1", Name: "HandleAuth"}
	p2 := &graph.Entity{ID: "shared-1", Name: "HandleAuth"}
	if p1 == p2 {
		t.Fatal("test setup invalid: p1 and p2 must be distinct pointers")
	}
	other := &graph.Entity{ID: "other-1", Name: "Unrelated"}

	bm25 := []Hit{{Entity: p1, Score: 1}, {Entity: other, Score: 0.1}}
	sem := []Hit{{Entity: p2, Score: 1}}

	fused := FuseRRF(bm25, sem)

	var shared *Hit
	count := 0
	for i := range fused {
		if fused[i].Entity.ID == "shared-1" {
			shared = &fused[i]
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one fused hit for ID shared-1 (distinct pointers, same ID), got %d in %+v", count, fused)
	}
	if shared.Source != "bm25+semantic" {
		t.Fatalf("shared-1 should be sourced from both rankers, got %q", shared.Source)
	}
	wantScore := 1.0/(rrfK+1) + 1.0/(rrfK+1) // rank 0 in both bm25 and semantic
	if diff := shared.Score - wantScore; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("shared-1 fused score = %v, want %v (summed RRF contribution)", shared.Score, wantScore)
	}
}

// TestFuseRRF_OrderingAndScoreParity locks down the exact fused order and
// scores for a normal multi-ranker fixture (all pointers pre-materialized
// once and shared across ranker inputs, as today), so the ID-keyed re-key is
// verified byte-identical to the prior pointer-keyed behavior in the common
// case.
func TestFuseRRF_OrderingAndScoreParity(t *testing.T) {
	t.Parallel()
	a := &graph.Entity{ID: "a", Name: "AuthCheck"}
	b := &graph.Entity{ID: "b", Name: "AuthVerify"}
	c := &graph.Entity{ID: "c", Name: "LogoutHandler"}
	d := &graph.Entity{ID: "d", Name: "SessionStore"}

	bm25 := []Hit{{Entity: a, Score: 5}, {Entity: c, Score: 3}, {Entity: d, Score: 1}}
	sem := []Hit{{Entity: b, Score: 0.9}, {Entity: a, Score: 0.6}, {Entity: c, Score: 0.2}}

	fused := FuseRRF(bm25, sem)

	wantIDs := []string{"a", "c", "b", "d"}
	if len(fused) != len(wantIDs) {
		t.Fatalf("fused length = %d, want %d (%+v)", len(fused), len(wantIDs), fused)
	}
	for i, id := range wantIDs {
		if fused[i].Entity.ID != id {
			t.Fatalf("fused[%d].Entity.ID = %q, want %q (full: %+v)", i, fused[i].Entity.ID, id, fused)
		}
	}

	wantScores := map[string]float64{
		"a": 1.0/(rrfK+1) + 1.0/(rrfK+2), // rank 1 in bm25, rank 2 in semantic
		"c": 1.0/(rrfK+2) + 1.0/(rrfK+3), // rank 2 in bm25, rank 3 in semantic
		"b": 1.0 / (rrfK + 1),            // rank 1 in semantic only
		"d": 1.0 / (rrfK + 3),            // rank 3 in bm25 only
	}
	wantSources := map[string]string{
		"a": "bm25+semantic",
		"c": "bm25+semantic",
		"b": "semantic",
		"d": "bm25",
	}
	for _, h := range fused {
		if diff := h.Score - wantScores[h.Entity.ID]; diff > 1e-9 || diff < -1e-9 {
			t.Fatalf("%s score = %v, want %v", h.Entity.ID, h.Score, wantScores[h.Entity.ID])
		}
		if h.Source != wantSources[h.Entity.ID] {
			t.Fatalf("%s source = %q, want %q", h.Entity.ID, h.Source, wantSources[h.Entity.ID])
		}
	}
}
