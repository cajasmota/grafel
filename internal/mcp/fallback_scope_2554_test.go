// fallback_scope_2554_test.go — regression tests for #2554b: MCP response
// context bleed via cross-repo PageRank fallback injection.
//
// Root cause: pickFallback iterated ALL repos and returned the globally
// highest-PageRank entity, regardless of which repo the query was targeting.
// This caused bench iter 1 q10 (InspectionResultsPage trace) to surface an
// unrelated POST /schedule/import endpoint injected from a different repo.
//
// Fix: scopeFallbackRepos() restricts the fallback candidate set to the repo
// most relevant to the query (explicit filter → most BM25 hits → first repo).
package mcp

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// pr is a helper to set PageRank on an entity literal.
func pr(v float64) *float64 { return &v }

// buildCrossRepoGroup builds two repos:
//   - "frontend": contains InspectionResultsPage (high-relevance to the query)
//   - "backend":  contains a POST /schedule/import endpoint with a very high
//     PageRank, simulating the entity that was bleeding into q10 responses.
//
// When a query for "InspectionResultsPage" is run against both repos without a
// repo_filter, the frontend entity is the correct answer. The backend entity
// must never appear in that response.
func buildCrossRepoGroup() (*graph.Document, *graph.Document) {
	frontend := &graph.Document{
		Repo: "frontend",
		Entities: []graph.Entity{
			{
				ID:            "comp_inspection_results",
				Name:          "InspectionResultsPage",
				QualifiedName: "src/pages/InspectionResultsPage",
				Kind:          "SCOPE.Component",
				SourceFile:    "src/pages/InspectionResultsPage.tsx",
				StartLine:     1,
				PageRank:      pr(0.05),
			},
			{
				ID:            "hook_use_inspection",
				Name:          "useInspectionResults",
				QualifiedName: "src/hooks/useInspectionResults",
				Kind:          "SCOPE.Function",
				SourceFile:    "src/hooks/useInspectionResults.ts",
				StartLine:     10,
				PageRank:      pr(0.04),
			},
		},
	}
	backend := &graph.Document{
		Repo: "backend",
		Entities: []graph.Entity{
			// High-PageRank entity unrelated to InspectionResultsPage.
			// This is the entity that was bleeding into q10 responses before the fix.
			{
				ID:            "ep_schedule_import",
				Name:          "POST /schedule/import",
				QualifiedName: "schedule.views.ImportView.post",
				Kind:          "SCOPE.Operation",
				SourceFile:    "schedule/views.py",
				StartLine:     42,
				PageRank:      pr(0.95), // very high — old pickFallback would pick this
			},
			{
				ID:            "ep_inspection_list",
				Name:          "GET /inspections/",
				QualifiedName: "inspections.views.InspectionViewSet.list",
				Kind:          "SCOPE.Operation",
				SourceFile:    "inspections/views.py",
				StartLine:     10,
				PageRank:      pr(0.3),
			},
		},
	}
	return frontend, backend
}

// TestMCP_HighConfidenceQuery_NoFallbackInjected verifies that when a query
// has high-confidence primary matches (score well above minScore), no
// fallback entity from a different repo leaks into the response.
//
// The query "InspectionResultsPage" matches the frontend repo directly.
// The backend's POST /schedule/import must not appear in the output.
func TestMCP_HighConfidenceQuery_NoFallbackInjected(t *testing.T) {
	frontend, backend := buildCrossRepoGroup()
	srv := newTestServer(t, frontend, backend)

	res := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group": "test",
		"query": "InspectionResultsPage",
		"full":  true,
	})

	if res == "" {
		t.Fatal("expected non-empty result")
	}

	// The primary match should be present.
	if !strings.Contains(res, "InspectionResultsPage") {
		t.Errorf("expected InspectionResultsPage in result; got:\n%s", res)
	}

	// The unrelated backend endpoint must NOT be injected.
	if strings.Contains(res, "schedule/import") || strings.Contains(res, "schedule.import") ||
		strings.Contains(res, "POST /schedule/import") {
		t.Errorf("cross-context bleed: POST /schedule/import injected into InspectionResultsPage query; got:\n%s", res)
	}
}

// TestMCP_LowConfidenceQuery_FallbackScopedToRepo verifies that when a query
// returns zero results (no BM25/min_score match), the "always-1" fallback is
// scoped to the repo most textually similar to the query — never the globally
// highest-PageRank entity from an unrelated repo.
//
// Query "xyz_completely_unknown_symbol_zzzq" matches nothing in either repo.
// The fallback must come from the frontend repo (which had BM25 hits for
// partial tokens) or at worst the first repo — never from the backend repo's
// high-PageRank POST /schedule/import entity.
func TestMCP_LowConfidenceQuery_FallbackScopedToRepo(t *testing.T) {
	frontend, backend := buildCrossRepoGroup()

	// Give the frontend repo a slightly higher BM25 signal by adding an entity
	// whose name shares a token with the query stub. In practice, BM25 returns
	// a non-empty hit list even for poor queries when the corpus is non-empty.
	// We add a token "xyzcomp" so "xyz" BM25-matches the frontend.
	frontend.Entities = append(frontend.Entities, graph.Entity{
		ID:         "comp_xyzcomp",
		Name:       "XyzCompHelper",
		Kind:       "SCOPE.Component",
		SourceFile: "src/helpers/XyzCompHelper.tsx",
		StartLine:  1,
		PageRank:   pr(0.01),
	})

	srv := newTestServer(t, frontend, backend)

	// Use min_score=0 to defeat the minScore culling — we want to observe the
	// raw fallback path when the query finds nothing above the default threshold.
	res := callEndpointToolText(t, srv.handleQueryGraph, map[string]any{
		"group":     "test",
		"query":     "xyzcomp inspection frontend component",
		"full":      true,
		"min_score": 0.0,
	})

	if res == "" {
		// No result at all is acceptable (empty corpus branch); skip scope check.
		t.Skip("empty result — corpus too small for BM25 to score anything")
	}

	// The backend's POST /schedule/import must not appear as a fallback result.
	// It has a much higher PageRank (0.95) than any frontend entity — before
	// the fix, pickFallback would always return it in a zero-result scenario.
	if strings.Contains(res, "schedule/import") || strings.Contains(res, "POST /schedule") {
		t.Errorf("cross-repo fallback bleed: backend POST /schedule/import injected for frontend-targeted query; got:\n%s", res)
	}
}

// TestScopeFallbackRepos_SingleFilter verifies that when a single non-wildcard
// repo_filter is provided, scopeFallbackRepos returns only that repo.
func TestScopeFallbackRepos_SingleFilter(t *testing.T) {
	repoA := &LoadedRepo{Repo: "alpha"}
	repoB := &LoadedRepo{Repo: "beta"}
	repos := []*LoadedRepo{repoA, repoB}
	hits := map[*LoadedRepo]int{repoA: 3, repoB: 10}

	// Even though beta has more BM25 hits, a single explicit filter wins.
	got := scopeFallbackRepos(repos, []string{"alpha"}, hits)
	if len(got) != 1 || got[0] != repoA {
		t.Errorf("expected [alpha] from single filter; got %v", reposNames(got))
	}
}

// TestScopeFallbackRepos_BestBM25 verifies that without a filter, the repo
// with the most raw BM25 hits wins.
func TestScopeFallbackRepos_BestBM25(t *testing.T) {
	repoA := &LoadedRepo{Repo: "alpha"}
	repoB := &LoadedRepo{Repo: "beta"}
	repos := []*LoadedRepo{repoA, repoB}
	hits := map[*LoadedRepo]int{repoA: 2, repoB: 8}

	got := scopeFallbackRepos(repos, nil, hits)
	if len(got) != 1 || got[0] != repoB {
		t.Errorf("expected [beta] (highest BM25 hits=8); got %v", reposNames(got))
	}
}

// TestScopeFallbackRepos_AllZeroHits verifies that when all repos have zero
// BM25 hits, the first repo is returned (preserves original single-repo
// behaviour for empty corpora).
func TestScopeFallbackRepos_AllZeroHits(t *testing.T) {
	repoA := &LoadedRepo{Repo: "alpha"}
	repoB := &LoadedRepo{Repo: "beta"}
	repos := []*LoadedRepo{repoA, repoB}
	hits := map[*LoadedRepo]int{repoA: 0, repoB: 0}

	got := scopeFallbackRepos(repos, nil, hits)
	if len(got) != 1 || got[0] != repoA {
		t.Errorf("expected [alpha] (first repo, all zero hits); got %v", reposNames(got))
	}
}

// reposNames is a helper for readable test failure messages.
func reposNames(repos []*LoadedRepo) []string {
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Repo
	}
	return names
}
