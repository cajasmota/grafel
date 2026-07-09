package daemon

// warming.go — issue #5690.
//
// WarmingSnapshot is a tiny, read-only projection of the scheduler's state
// used to answer one question the MCP surface previously could not: "is this
// group still warming (post-index enrichment in flight), or is a slow query
// just slow?". It carries no scheduling authority — it is observability only.
//
// The type lives in internal/daemon (not internal/mcp) so the daemon can hand
// a closure over its private *sched.Scheduler out to cmd/grafel WITHOUT the
// daemon importing internal/mcp. internal/mcp already imports internal/daemon
// (for on-disk layout helpers), so mcp.State can consume daemon.WarmingSnapshot
// directly — no import cycle is introduced (the daemon never imports mcp).

// WarmingSnapshot is the read-only warming/readiness projection surfaced to the
// MCP tools. All fields are additive and safe to extend.
type WarmingSnapshot struct {
	// IndexInFlight is true when at least one repo is actively being indexed.
	IndexInFlight bool
	// PendingAlgo is the number of repos with a post-index algorithm
	// (PageRank/Louvain/…) enrichment pass still queued.
	PendingAlgo int
	// PendingLinks is the number of groups with a cross-repo link pass still
	// queued.
	PendingLinks int
}

// Warming reports whether the group is still warming — i.e. an index is in
// flight OR a post-index enrichment (algo/links) pass is pending. This is the
// single boolean the MCP surface exposes as `warming`.
func (w WarmingSnapshot) Warming() bool {
	return w.IndexInFlight || w.PendingAlgo > 0 || w.PendingLinks > 0
}
