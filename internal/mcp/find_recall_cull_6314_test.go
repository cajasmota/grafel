// find_recall_cull_6314_test.go — retrieval-recall regression for #6314.
//
// The bm25 branch of grafel_find culled each repo to a hard top-10 before RRF
// fusion, de-noise, kind filtering, re-ranking, min_score and max_results ever
// ran. Rank 11+ in a repo was therefore ABSENT from the result set, not merely
// low-ranked, and no caller argument could reach it: max_results (default 50,
// ceiling 200) gates only the returned set at the end of the pipeline.
//
// Nothing in this package measured retrieval recall before this file. The
// nearest test (find_ranking_1747_test.go) uses a 7-entity fixture — smaller
// than the cap — so the cull can never fire in it.
package mcp

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/embed"
	"github.com/cajasmota/grafel/internal/graph"
)

// buildCrowdedRecallDoc builds a single-repo corpus in which many entities
// share the query token "checkout" via their FILE STEM (weight 1.5, the
// heaviest indexed field) while exactly one entity carries it as its own
// NAME (weight 1.0). The name-match is therefore out-scored by every stem
// match and lands past rank 10 in its own repo.
//
// decoys controls the crowd size; with decoys >= 10 the target cannot fit in
// a hard top-10 cull.
func buildCrowdedRecallDoc(decoys int) *graph.Document {
	ents := make([]graph.Entity, 0, decoys+1)
	for i := 0; i < decoys; i++ {
		ents = append(ents, graph.Entity{
			ID:         fmt.Sprintf("decoy_%02d", i),
			Name:       fmt.Sprintf("RenderStepPanel%02d", i),
			Kind:       "SCOPE.Function",
			SourceFile: fmt.Sprintf("src/step%02d/checkout.ts", i),
			StartLine:  10 + i,
		})
	}
	// The entity the caller is actually looking for: its NAME is exactly the
	// query, but its file lives in an unrelated-looking directory.
	ents = append(ents, graph.Entity{
		ID:         "target_checkout",
		Name:       "Checkout",
		Kind:       "SCOPE.Function",
		SourceFile: "src/promo/banner.ts",
		StartLine:  7,
	})
	return &graph.Document{Repo: "shop", Entities: ents}
}

// TestFindRecall_ExactNameSurvivesCrowdedRepo_6314 asserts that an entity whose
// NAME is exactly the query is PRESENT in the results when more than ten
// same-repo entities share that query token through their file stems.
//
// min_score=0 and full=true are passed so that the only stage able to remove
// the target is the per-repo cull itself — this isolates recall from scoring.
func TestFindRecall_ExactNameSurvivesCrowdedRepo_6314(t *testing.T) {
	const decoys = 14
	doc := buildCrowdedRecallDoc(decoys)
	srv := newTestServer(t, doc)

	// Sanity: the raw index must actually contain the target and rank it past
	// rank 10, otherwise this fixture would not exercise the cull at all.
	lr := srv.State.groups["test"].Repos["shop"]
	unlimited := lr.getBM25().Search("Checkout", 0)
	rank := -1
	for i, h := range unlimited {
		if h.Entity != nil && h.Entity.ID == "target_checkout" {
			rank = i + 1
			break
		}
	}
	if rank == -1 {
		t.Fatalf("fixture invalid: target absent from the unlimited BM25 result set (%d hits)", len(unlimited))
	}
	if rank <= 10 {
		t.Fatalf("fixture invalid: target ranks %d (<=10), so a top-10 cull would not drop it", rank)
	}
	t.Logf("target ranks %d of %d in the raw BM25 index", rank, len(unlimited))

	res := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group":     "test",
		"question":  "Checkout",
		"full":      true,
		"min_score": 0.0,
	})

	if !strings.Contains(res, `"Checkout"`) {
		t.Errorf("entity named exactly the query %q is ABSENT from results in a crowded repo\n"+
			"(%d competing entities share the token via their file stems; raw BM25 rank %d)\ngot:\n%s",
			"Checkout", decoys, rank, res)
	}
}

// TestFindRecall_CullTracksMaxResults_6314 asserts two things, and deliberately
// claims no more than they carry:
//
//   - the default max_results reaches past rank 10, i.e. the cull is wide
//     enough that a crowded repo no longer hides an exact-name match;
//   - max_results still gates the RETURNED set, so widening the pool did not
//     change the output contract.
//
// It does NOT show that the pool is derived from max_results — a hardcoded
// cull satisfies both assertions. That property is pinned separately by
// TestFindRecall_PoolTracksMaxResultsNotAConstant_6314.
func TestFindRecall_CullTracksMaxResults_6314(t *testing.T) {
	doc := buildCrowdedRecallDoc(14)
	srv := newTestServer(t, doc)

	call := func(maxResults int) string {
		return callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
			"group":       "test",
			"question":    "Checkout",
			"full":        true,
			"min_score":   0.0,
			"max_results": maxResults,
		})
	}

	if got := call(50); !strings.Contains(got, `"Checkout"`) {
		t.Errorf("max_results=50 must reach past rank 10 in a 15-entity repo; got:\n%s", got)
	}
	// max_results still gates the RETURNED set: a tight cap must not return
	// more rows than asked for.
	tight := call(3)
	if n := strings.Count(tight, `"name"`); n > 3 {
		t.Errorf("max_results=3 must gate the returned set; got %d rows:\n%s", n, tight)
	}
}

// ---------------------------------------------------------------------------
// The SEMANTIC half of the same cull (#6314).
//
// tools.go caps the vector index identically to BM25. A recall test that only
// crowds the BM25 index leaves that second call site free to be reverted to a
// hard 10 by a later edit with the suite still green — so the semantic side is
// crowded here on its own terms.
// ---------------------------------------------------------------------------

// fixedVecBackend is a query embedder that always returns the same vector.
// The fixture controls semantic RANK entirely through the stored entity
// vectors (see buildSemanticCrowdedRepo), so the query side only has to be
// deterministic and dimensionally compatible.
type fixedVecBackend struct{ vec []float32 }

func (f *fixedVecBackend) Dims() int    { return len(f.vec) }
func (f *fixedVecBackend) Name() string { return "fixedvec-test" }
func (f *fixedVecBackend) Close() error { return nil }
func (f *fixedVecBackend) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = append([]float32(nil), f.vec...)
	}
	return out, nil
}

// installQueryEmbedder points the package-level query embedder at be for the
// duration of the test.
//
// This deliberately does NOT call t.Parallel: Go defers every top-level
// parallel test until the serial ones have finished, so a serial test owns the
// global outright and no concurrent reader can observe the swap.
func installQueryEmbedder(t *testing.T, be embed.Backend) {
	t.Helper()
	qe := &queryEmbedder{backend: be}
	qe.once.Do(func() {}) // burn the once so get() never re-initialises from config
	prev := globalQueryEmbedder
	globalQueryEmbedder = qe
	t.Cleanup(func() { globalQueryEmbedder = prev })
}

// buildSemanticCrowdedRepo builds a repo in which the target entity is
// invisible to BM25 (its name, path and docstring share no token with the
// query) and reachable ONLY through the vector index, where `decoys` entities
// out-rank it. Vectors are 2-D unit vectors; cosine against the query vector
// [1,0] decreases monotonically down the list, so the target sits at semantic
// rank decoys+1.
func buildSemanticCrowdedRepo(t *testing.T, decoys int) (*Server, string) {
	t.Helper()
	ents := make([]graph.Entity, 0, decoys+1)
	for i := 0; i < decoys; i++ {
		ents = append(ents, graph.Entity{
			ID:         fmt.Sprintf("sdecoy_%02d", i),
			Name:       fmt.Sprintf("CheckoutStepPanel%02d", i),
			Kind:       "SCOPE.Function",
			SourceFile: fmt.Sprintf("src/step%02d/checkout.ts", i),
			StartLine:  10 + i,
		})
	}
	const targetID = "starget_promo"
	ents = append(ents, graph.Entity{
		ID:         targetID,
		Name:       "PromoBanner",
		Kind:       "SCOPE.Function",
		SourceFile: "src/promo/banner.ts",
		StartLine:  7,
	})
	doc := &graph.Document{Repo: "shop", Entities: ents}
	srv := newTestServer(t, doc)

	// Stored vectors: angle grows with list position, so cosine against [1,0]
	// falls monotonically and the target (last) is the weakest semantic match
	// that still scores above zero.
	store := embed.NewStore(2, "fixedvec-test")
	for i, e := range ents {
		theta := float64(i+1) * 0.05 // stays well inside the first quadrant
		store.Put(embed.Record{
			ID:     e.ID,
			Hash:   e.ID,
			Vector: []float32{float32(math.Cos(theta)), float32(math.Sin(theta))},
		})
	}
	srv.State.groups["test"].Repos["shop"].Semantic = store
	installQueryEmbedder(t, &fixedVecBackend{vec: []float32{1, 0}})
	return srv, targetID
}

// TestSemanticRecall_VectorHitSurvivesCrowdedRepo_6314 pins the SECOND call
// site changed for #6314: the vector index is culled per repo before RRF
// fusion, so an entity at vector rank 11+ was absent from the answer even
// though BM25 never had a chance to surface it either.
func TestSemanticRecall_VectorHitSurvivesCrowdedRepo_6314(t *testing.T) {
	const decoys = 14
	srv, targetID := buildSemanticCrowdedRepo(t, decoys)

	lr := srv.State.groups["test"].Repos["shop"]

	// Fixture validity 1: the target must be invisible to BM25, so that only
	// the semantic cull can decide whether it reaches the answer.
	for _, h := range lr.getBM25().Search("checkout", 0) {
		if h.Entity != nil && h.Entity.ID == targetID {
			t.Fatalf("fixture invalid: target is reachable via BM25; the semantic cull would not be the deciding stage")
		}
	}
	// Fixture validity 2: the target must rank past 10 in the vector index.
	rank := -1
	for i, h := range lr.Semantic.Search([]float32{1, 0}, 0) {
		if h.ID == targetID {
			rank = i + 1
			break
		}
	}
	if rank <= 10 {
		t.Fatalf("fixture invalid: target ranks %d in the vector index (<=10), so a top-10 cull would not drop it", rank)
	}
	t.Logf("target ranks %d in the vector index and is absent from BM25", rank)

	res := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group":     "test",
		"question":  "checkout",
		"full":      true,
		"min_score": 0.0,
	})

	if !strings.Contains(res, `"PromoBanner"`) {
		t.Errorf("entity reachable only through the vector index at rank %d is ABSENT from results\n"+
			"(%d entities out-rank it semantically); got:\n%s", rank, decoys, res)
	}
}

// buildWideRecallDoc builds a single-repo corpus far larger than the
// max_results ceiling, every entity sharing the query token through both its
// name and its file stem.
func buildWideRecallDoc(n int) *graph.Document {
	ents := make([]graph.Entity, 0, n)
	for i := 0; i < n; i++ {
		ents = append(ents, graph.Entity{
			ID:         fmt.Sprintf("wide_%03d", i),
			Name:       fmt.Sprintf("CheckoutStepPanel%03d", i),
			Kind:       "SCOPE.Function",
			SourceFile: fmt.Sprintf("src/step%03d/checkout.ts", i),
			StartLine:  1 + i,
		})
	}
	return &graph.Document{Repo: "shop", Entities: ents}
}

// callWideFind queries the wide corpus with an explicit max_results.
func callWideFind(t *testing.T, srv *Server, maxResults int) string {
	t.Helper()
	return callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group":       "test",
		"question":    "checkout",
		"full":        true,
		"min_score":   0.0,
		"max_results": maxResults,
	})
}

// TestFindRecall_PoolTracksMaxResultsNotAConstant_6314 kills the entire
// `perRepoCull := <constant>` family — including `perRepoCull := 200`, which
// survived every other test in this file.
//
// A constant cull satisfies "the relevant entity is present" and "no more rows
// than max_results are returned", so neither of those observations can tell a
// derived pool from a fixed one. What separates them is a TIGHT max_results:
// with the pool derived from max_results, asking for 3 admits 3 candidates and
// nothing is discarded; with any constant >= 4 the pool admits far more than
// can be returned and the surplus is dropped after the fact.
//
// `truncation_note` is emitted iff the pre-cap pool exceeded max_results, so
// its ABSENCE at max_results=3 over a 260-entity corpus is the caller-visible
// witness that the pool followed the argument down.
//
// This also retires a claim that previously had no test behind it: the older
// doc comment asserted that lowering max_results below the target's rank
// legitimately drops it, while only the returned row count was ever checked.
func TestFindRecall_PoolTracksMaxResultsNotAConstant_6314(t *testing.T) {
	const corpus = 260
	srv := newTestServer(t, buildWideRecallDoc(corpus))

	res := callWideFind(t, srv, 3)

	// Report the note, not the payload.
	if i := strings.Index(res, "truncation_note"); i >= 0 {
		t.Errorf("the per-repo pool did not follow max_results down to 3 in a %d-entity repo:\n"+
			"a truncation_note means the pool admitted more candidates than max_results can return, "+
			"which is what a hardcoded cull (e.g. perRepoCull := 200) does and a derived one does not.\nnote: %s",
			corpus, res[i:])
	}
	if n := strings.Count(res, `"name"`); n > 3 {
		t.Errorf("max_results=3 must gate the returned set; got %d rows", n)
	}
}

// TestFindRecall_ClampBoundsPoolAndRows_6314 covers the opposite end: a caller
// asking for more than the ceiling.
//
// The two assertions carry DIFFERENT properties, and the distinction matters
// because it is easy to credit the wrong one:
//
//   - the absence of `truncation_note` carries only "pool <= max_results". It
//     says nothing about the absolute size of the pool, so it cannot observe
//     the 200 clamp on its own.
//   - the row-count assertion is what carries "<= 200". Deleting the clamp at
//     tools.go:606 is caught HERE, by the row count (260 rows returned), not
//     by the note — with the clamp gone, max_results is 1000 and a 260-entity
//     pool never exceeds it, so no note is emitted.
//
// Together they bound the pre-filter pool at min(max_results, 200) per repo.
func TestFindRecall_ClampBoundsPoolAndRows_6314(t *testing.T) {
	const ceiling = 200 // tools.go:606 — the documented max_results ceiling
	const corpus = 260  // comfortably larger than the ceiling
	srv := newTestServer(t, buildWideRecallDoc(corpus))

	res := callWideFind(t, srv, 1000)

	// Property: pool <= max_results (i.e. the pool is not widened past what
	// the caller can receive). Does NOT by itself observe the 200 clamp.
	if i := strings.Index(res, "truncation_note"); i >= 0 {
		t.Errorf("candidate pool exceeded max_results in a %d-entity repo: "+
			"more candidates were admitted than could be returned, which is "+
			"unbounded pre-filter growth in a package whose RSS is already contended.\nnote: %s",
			corpus, res[i:])
	}
	// Property: <= 200. This is the assertion that observes the clamp itself.
	if n := strings.Count(res, `"name"`); n > ceiling {
		t.Errorf("returned %d rows, above the max_results ceiling of %d (the clamp at tools.go:606 is not holding)", n, ceiling)
	}
}

// Known limits of this file, stated rather than implied:
//
//   - Every fixture here is SINGLE-REPO. The real pre-filter pool is
//     len(repos) x perRepoCull, so a 20-repo group still admits up to 4,000
//     candidates per request. Nothing in this file observes that multiplier;
//     the per-repo bound is what is pinned.
//   - `truncation_note` is one-directional. It witnesses a pool that is wide
//     relative to max_results; it cannot witness one that is wide in absolute
//     terms while max_results is wider still. The clamp assertion above covers
//     that direction, and only at the ceiling.
