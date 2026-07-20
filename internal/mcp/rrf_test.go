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
