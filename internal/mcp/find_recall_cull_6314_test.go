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
	"fmt"
	"strings"
	"testing"

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

// TestFindRecall_CullTracksMaxResults_6314 asserts the per-repo candidate pool
// follows max_results: lowering max_results below the target's rank legitimately
// drops it, raising it above the rank must bring it back. This pins the cull to
// the caller-visible argument rather than to any particular constant.
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
