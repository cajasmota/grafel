// algo_demand.go — on-demand algorithm-result provider for rank-sensitive MCP
// tools (S2 of Silent Daemon, #2152).
//
// When ARCHIGRAPH_EAGER_ALGO is not set (the default), the daemon skips the
// post-reindex algorithm pass so the watcher is CPU-silent after a file save.
// This file provides ensureAlgoResults, which rank-sensitive tool handlers
// call before they need PageRank / community / articulation data:
//
//  1. Check if algo_results.fb exists for the repo's current state dir and is
//     fresher than graph.fb (cache hit → return immediately).
//  2. On cache miss, run graph.RunAlgorithms on the already-loaded entities
//     and relationships (no full reindex), persist the result, and return.
//
// This gives the first algo-needing query a ≤30s latency on a 60k-entity
// corpus, and subsequent queries <100ms (cache hit).
package mcp

import (
	"context"
	"fmt"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/daemon/algo"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/gitmeta"
)

// globalAlgoCache is the process-wide on-demand algo cache. It is
// initialised once at package init time and shared across all MCP Server
// instances (there is only one per daemon process).
var globalAlgoCache = algo.New(algoComputeFn)

// algoComputeFn is the ComputeFn wired into globalAlgoCache. It locates the
// graph.fb for (repoPath, ref) via the daemon state-path resolution, loads
// the Document, runs RunAlgorithms, and returns an algo.Results.
//
// Note: we load the graph fresh from disk rather than re-using the in-memory
// LoadedRepo.Doc to avoid coupling the cache lifecycle to the mtime-based
// reload cycle. On a 60k entity corpus this takes <30s; the cache then serves
// subsequent requests in <100ms.
func algoComputeFn(ctx context.Context, repoPath, ref string) (*algo.Results, error) {
	var stateDir string
	if ref != "" {
		stateDir = daemon.StateDirForRepoRef(repoPath, ref)
	} else {
		stateDir = daemon.StateDirForRepo(repoPath)
	}

	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		return nil, fmt.Errorf("load graph for algo pass: %w", err)
	}
	if doc == nil || len(doc.Entities) == 0 {
		return &algo.Results{
			PageRank:           map[string]float64{},
			Centrality:         map[string]float64{},
			CommunityID:        map[string]int{},
			GodNodes:           map[string]bool{},
			ArticulationPoints: map[string]bool{},
			SurpriseEndpoints:  map[string]bool{},
		}, nil
	}

	res := graph.RunAlgorithms(doc.Entities, doc.Relationships)
	return &algo.Results{
		PageRank:           res.PageRank,
		Centrality:         res.Centrality,
		CommunityID:        res.CommunityID,
		GodNodes:           res.GodNodes,
		ArticulationPoints: res.ArticulationPoints,
		SurpriseEndpoints:  res.SurpriseEndpoints,
	}, nil
}

// ensureAlgoResults returns cached or freshly-computed algorithm results for
// the given LoadedRepo. Callers that need ranking data (PageRank, community,
// articulation) should call this before using those fields.
//
// The stateDir is derived from lr.Path using the daemon path-resolution
// helpers (same logic as StateDirForRepo).
func ensureAlgoResults(ctx context.Context, lr *LoadedRepo) (*algo.Results, error) {
	if lr == nil || lr.Path == "" {
		return nil, fmt.Errorf("ensureAlgoResults: nil or empty-path LoadedRepo")
	}
	meta := gitmeta.Capture(lr.Path)
	stateDir := daemon.StateDirForRepoRef(lr.Path, meta.Ref)
	return globalAlgoCache.Get(ctx, stateDir, lr.Path, meta.Ref)
}
