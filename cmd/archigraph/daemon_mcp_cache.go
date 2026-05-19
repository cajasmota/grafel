package main

import (
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/daemon"
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
// new file. Safe to call on the error path too — the cache simply
// returns false when there's nothing to evict.
func invalidateAfterIndex(repoPath string) {
	daemonMCPCache.Invalidate(repoGraphFBPath(repoPath))
}
