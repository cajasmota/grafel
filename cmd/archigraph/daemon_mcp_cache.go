package main

import (
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/daemon"
	daemonalgo "github.com/cajasmota/archigraph/internal/daemon/algo"
	daemonmcp "github.com/cajasmota/archigraph/internal/daemon/mcp"
)

// daemonMCPCache is the process-wide lazy-mmap graph.fb cache used by
// every MCP query handler served from the daemon (ADR-0017 Phase D).
//
// The cache is constructed once at startup with the default capacity
// (10 mmap'd graph.fb handles). The scheduler-wrapping IndexFn hooks
// below call Invalidate after a successful index pass so the next MCP
// query reopens the freshly written file.
var daemonMCPCache = daemonmcp.NewCache(daemonmcp.DefaultCapacity)

// repoGraphFBPath is the canonical on-disk location of a repo's
// FlatBuffers graph, matching the path used in Index().
func repoGraphFBPath(repoPath string) string {
	return filepath.Join(daemon.StateDirForRepo(repoPath), "graph.fb")
}

// invalidateAfterIndex is the post-index hook: it drops any cached
// reader for repoPath's graph.fb so the next MCP query re-mmap's the
// new file. It also invalidates the algo_results.fb sidecar (S2 of #2149)
// so the next rank-sensitive MCP query triggers a fresh algorithm pass.
// Safe to call on the error path too.
func invalidateAfterIndex(repoPath string) {
	daemonMCPCache.Invalidate(repoGraphFBPath(repoPath))
	// S2: invalidate the on-demand algo cache so ranking tools recompute
	// against the newly written graph.fb on next query.
	stateDir := daemon.StateDirForRepo(repoPath)
	_ = daemonalgo.Invalidate(stateDir) // non-fatal; log is inside Invalidate
}
