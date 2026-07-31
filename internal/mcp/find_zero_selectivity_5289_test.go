// find_zero_selectivity_5289_test.go — regression tests for #5289.
//
// Bug: grafel_find on a zero-selectivity query (terms that match NO entity
// name) returned the WHOLE repo at score 0.00, then BFS-expanded (depth=3) over
// a 6915-node densely-connected graph and built a 2.6M-edge summary → ~113s.
//
// Root cause: the min_score cull had a "preserve at least one hit" guard that
// kept the ENTIRE sub-threshold candidate list when nothing cleared the 0.15
// floor; those near-zero seeds then drove an UNBOUNDED BFS (bfs() passed
// maxNodes=0) + an unbounded edge-summary.
//
// Fix: enforce the min_score floor honestly (drop every sub-floor hit, even if
// that empties the list), return the single always-1 fallback with a
// low-confidence hint WITHOUT expansion, and bound BFS/edge work defensively.
package mcp

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// buildDenseRepo builds a single densely-connected repo of n entities whose
// names share NO tokens with the zero-selectivity query below. Every node links
// to the next ~k nodes so a depth-3 BFS from any seed (before the #5289 caps)
// would reach essentially the whole graph and emit a huge edge summary.
func buildDenseRepo(n, k int) *graph.Document {
	doc := &graph.Document{Repo: "dense"}
	for i := 0; i < n; i++ {
		// Names use a private token namespace ("zqx…") so no query in these
		// tests BM25-matches them by accident.
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:         fmt.Sprintf("n%d", i),
			Name:       fmt.Sprintf("zqxNode%d", i),
			Kind:       "SCOPE.Function",
			SourceFile: fmt.Sprintf("src/zqx/node%d.ts", i),
			StartLine:  1,
			PageRank:   pr(0.01),
		})
	}
	for i := 0; i < n; i++ {
		for j := 1; j <= k; j++ {
			t := (i + j) % n
			doc.Relationships = append(doc.Relationships, graph.Relationship{
				ID:     fmt.Sprintf("e%d_%d", i, t),
				FromID: fmt.Sprintf("n%d", i),
				ToID:   fmt.Sprintf("n%d", t),
				Kind:   "CALLS",
			})
		}
	}
	return doc
}

// TestFind_ZeroSelectivity_NoWholeRepoDump_5289 is the core regression: a query
// whose terms match nothing must NOT return the whole repo, must be fast, and
// must carry the low-confidence hint.
func TestFind_ZeroSelectivity_NoWholeRepoDump_5289(t *testing.T) {
	const n = 6000
	doc := buildDenseRepo(n, 8) // 6000 nodes, ~48k edges, depth-3 reaches all
	srv := newTestServer(t, doc)

	start := time.Now()
	res := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group": "test",
		"query": "main features screens dashboard inspection building",
	})
	elapsed := time.Since(start)

	if res == "" {
		t.Fatal("expected a (low-confidence) result, got empty")
	}

	// 1) Must NOT dump the whole repo. The compact output names visible nodes;
	//    a whole-repo dump would mention thousands of zqxNode names. Count them.
	mentions := strings.Count(res, "zqxNode")
	if mentions > findMaxVisibleNodes {
		t.Fatalf("whole-repo dump: %d node mentions (cap %d). The min_score floor "+
			"or expansion bound is not enforced.\n%.500s", mentions, findMaxVisibleNodes, res)
	}
	// For a true zero-selectivity query we expect just the single fallback node.
	if mentions > 2 {
		t.Errorf("expected ~1 fallback node for a zero-selectivity query, got %d node mentions", mentions)
	}

	// 2) Must carry the honest low-confidence hint (not a real subgraph).
	if !strings.Contains(res, "no strong matches above min_score") {
		t.Errorf("expected low-confidence hint; got:\n%s", res)
	}

	// 3) Liveness only. The real guard for #5289 is the `mentions` cap in (1)
	//    above, not this bound.
	//
	//    Do NOT read the 20s as "5.6x below the 113s unbounded path". The ~113s
	//    in #5289 was measured on a LIVE graph; this 6000-node fixture cannot
	//    reach it. Reverting the whole #5289 fix (restore the preserve-one-hit
	//    guard, drop `&& !lowConfidence`, raise all three limits to 1<<30) makes
	//    this query take 146ms, not 113s — and the run still fails, on the
	//    `mentions` assertion above, which is the assertion that actually
	//    encodes the regression.
	//
	//    So this line is a hang guard and nothing more: it catches the query
	//    never returning. It is 20s rather than the original 2s because 2s on a
	//    sub-second operation measures the runner, and failing here would point
	//    a maintainer at a regression that the mentions check would have named
	//    precisely.
	if elapsed > 20*time.Second {
		t.Fatalf("zero-selectivity query took %v — the unbounded whole-graph scan is back (bounded path is sub-second)", elapsed)
	}
	t.Logf("zero-selectivity query: %d node mentions, elapsed=%v", mentions, elapsed)
}

// TestFind_MinScoreFloorEnforced_5289 asserts the floor itself: with the default
// min_score, sub-threshold hits are dropped (full=true exposes the raw match
// list). A zero-selectivity query yields only the low-confidence fallback.
func TestFind_MinScoreFloorEnforced_5289(t *testing.T) {
	doc := buildDenseRepo(500, 4)
	srv := newTestServer(t, doc)

	out := callEndpointTool(t, srv.handleQueryGraph, map[string]any{
		"group": "test",
		"query": "main features screens dashboard inspection building",
		"full":  true,
	})

	matches, _ := out["matches"].([]any)
	if len(matches) > 1 {
		t.Fatalf("min_score floor not enforced: expected <=1 (fallback only) match for a "+
			"zero-selectivity query, got %d", len(matches))
	}
	if lc, _ := out["low_confidence"].(bool); !lc {
		t.Errorf("expected low_confidence=true for zero-selectivity query; got %#v", out["low_confidence"])
	}
}

// TestFind_SelectiveQuery_StillExpands_5289 guards against regression: a real
// match above the floor must still be returned and BFS-expand its neighbours.
func TestFind_SelectiveQuery_StillExpands_5289(t *testing.T) {
	doc := &graph.Document{Repo: "app"}
	// A clearly-named target plus a small neighbourhood reachable by CALLS.
	doc.Entities = []graph.Entity{
		{ID: "page", Name: "InspectionResultsPage", Kind: "SCOPE.Component",
			SourceFile: "src/pages/InspectionResultsPage.tsx", StartLine: 1, PageRank: pr(0.2)},
		{ID: "hook", Name: "useInspectionResults", Kind: "SCOPE.Function",
			SourceFile: "src/hooks/useInspectionResults.ts", StartLine: 1, PageRank: pr(0.1)},
		{ID: "api", Name: "fetchInspectionResults", Kind: "SCOPE.Function",
			SourceFile: "src/api/inspection.ts", StartLine: 1, PageRank: pr(0.05)},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "r1", FromID: "page", ToID: "hook", Kind: "CALLS"},
		{ID: "r2", FromID: "hook", ToID: "api", Kind: "CALLS"},
	}
	srv := newTestServer(t, doc)

	res := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group": "test",
		"query": "InspectionResultsPage",
	})

	if !strings.Contains(res, "InspectionResultsPage") {
		t.Fatalf("selective query lost its primary match:\n%s", res)
	}
	// The neighbourhood must still expand (regression: don't over-prune).
	if !strings.Contains(res, "useInspectionResults") {
		t.Errorf("selective query did not BFS-expand neighbours:\n%s", res)
	}
	// And it must NOT carry the low-confidence hint.
	if strings.Contains(res, "no strong matches above min_score") {
		t.Errorf("selective query wrongly flagged low-confidence:\n%s", res)
	}
}
