package main

import (
	"github.com/cajasmota/grafel/internal/daemon"
	daemonalgo "github.com/cajasmota/grafel/internal/daemon/algo"
	daemonmcp "github.com/cajasmota/grafel/internal/daemon/mcp"
)

// daemonMCPCache is the process-wide lazy-mmap graph.fb cache used by
// every MCP query handler served from the daemon (ADR-0017 Phase D).
//
// The cache is constructed once at startup with the default capacity
// (10 mmap'd graph.fb handles). The scheduler-wrapping IndexFn hooks
// below call InvalidateDir after a successful index pass so the next MCP
// query reopens the freshly written generation.
var daemonMCPCache = daemonmcp.NewCache(daemonmcp.DefaultCapacity)

// invalidateAfterIndex is the post-index hook: it drops any cached
// reader for repoPath's graph.fb so the next MCP query re-mmap's the
// new file. It also invalidates the algo_results.fb sidecar (S2 of #2149)
// so the next rank-sensitive MCP query triggers a fresh algorithm pass.
// Safe to call on the error path too.
func invalidateAfterIndex(repoPath string) {
	// #5891: evict by state dir, not by a fixed graph.fb path. The reindex just
	// wrote a NEW graph.<gen>.fb and flipped the pointer, so the resident handle
	// is keyed by the superseded gen path; InvalidateDir drops it regardless of
	// gen number so the next MCP query re-mmaps the freshly written generation.
	stateDir := daemon.StateDirForRepo(repoPath)
	daemonMCPCache.InvalidateDir(stateDir)
	// S2: invalidate the on-demand algo cache so ranking tools recompute
	// against the newly written graph.fb on next query.
	_ = daemonalgo.Invalidate(stateDir) // non-fatal; log is inside Invalidate
}
