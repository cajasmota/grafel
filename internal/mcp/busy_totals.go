package mcp

import (
	"time"

	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// applyBusyTotals fills the grafel_stats `totals` block with the daemon's live
// busy signal. Extracted from handleStats so the EXTRACTION vs ENRICHMENT
// distinction — the thing every agent polls to decide whether the index is
// usable yet — is unit-testable without standing up a whole *Server.
//
// THE DISTINCTION, AND WHY IT IS NOT COSMETIC.
//
// Two different stages can make the daemon busy:
//
//   - INDEXING (extraction + the cross-repo link pass) writes graph.fb. Until it
//     finishes, the graph a consumer would read is incomplete. "Wait."
//   - ENHANCING (the group-algo annotation pass) writes a SEPARATE overlay file
//     holding communities/pagerank/centrality. It never touches graph.fb, and
//     none of the structural tools — find, expand, find_callers, traces,
//     cross_links, get_source — read it. The graph is fully queryable the whole
//     time it runs. "Go ahead; clusters/ranking may be missing for now."
//
// The engine-liveness branch used to report `is_indexing: true` for BOTH, and
// omitted is_enhancing entirely. On the reference corpus the annotation pass is
// hours long, so an agent (or the dashboard) polling grafel_stats saw
// "still indexing" for hours after the graph had been ready for ten minutes —
// i.e. the user-awaited rebuild never reported completion on the surface people
// actually watch. That is the bug this function fixes: the two branches now
// answer identically, and `is_indexing` means extraction only.
//
// engineFile/engineFresh come from daemon.EngineLivenessStatus. When the sidecar
// is missing or stale (no engine plane has run, or it is down/starting) the
// function falls back to the process-global indexstate record, preserving the
// pre-#5729-PR3 behavior for an in-process scheduler.
// Overlay states published in grafel_stats' `communities_overlay` field.
const (
	// overlayStateCurrent — an overlay is loaded; community_id / pagerank /
	// centrality are real values.
	overlayStateCurrent = "current"
	// overlayStateComputing — no overlay is loaded and the annotation pass is
	// running RIGHT NOW. Clusters/ranking will appear when it finishes; the
	// graph itself is already queryable.
	overlayStateComputing = "computing"
	// overlayStatePending — no overlay is loaded and no pass is running. Either
	// one has never been computed for this group, or the last one was
	// cancelled/failed. The scheduler's overlay sweep re-arms a pass for stale
	// groups, so this is a "not yet", not a permanent verdict — but it is the
	// state to report when someone asks why grafel_clusters is empty.
	overlayStatePending = "pending"
)

// publishOverlayState writes grafel_stats' `communities_overlay` field.
//
// The community/pagerank/centrality overlay is produced by the group-algo
// annotation pass AFTER a rebuild reports completion, so an empty `communities`
// count is ambiguous on its own — it means either "this group genuinely has no
// communities" or "the pass has not run yet". Every consumer degrades SILENTLY
// when the overlay is absent (nil community pointers, empty cluster lists), so
// the state is published explicitly rather than left for callers to infer.
//
// PRESENCE, NOT CONTENT. The signal is grp.algoApplied — "a present,
// non-stale, current-algorithm-version overlay was applied" — and deliberately
// NOT len(grp.Communities). A group whose overlay legitimately contains zero
// communities (tiny group, everything ungrouped) is fully computed; reporting
// it as `pending` would reintroduce, for exactly that group, the ambiguity this
// field exists to remove, and would tell an agent to keep retrying something
// that is already finished.
//
// The `is_enhancing` source is load-bearing: it must be the ENRICHMENT signal,
// not `is_indexing`. An absent overlay while extraction happens to be running
// is still `pending` — a running index does not mean the annotation pass is on
// its way. Only a running group-algo pass justifies telling a caller to retry.
func publishOverlayState(totals map[string]any, grp *LoadedGroup) {
	loaded := grp != nil && grp.algoApplied
	totals["communities_overlay"] = overlayStateFor(loaded, totals["is_enhancing"] == true)
}

// overlayStateFor maps (overlay loaded?, annotation pass running?) onto the
// published state string. Split out so the mapping is testable and so the two
// "absent" cases stay distinguishable — an agent that sees `computing` should
// retry, one that sees `pending` should not spin.
func overlayStateFor(loaded, enhancing bool) string {
	switch {
	case loaded:
		return overlayStateCurrent
	case enhancing:
		return overlayStateComputing
	default:
		return overlayStatePending
	}
}

func applyBusyTotals(totals map[string]any, engineFile *statusfile.File, engineFresh bool) {
	if engineFresh && engineFile != nil {
		isIndexing := engineFile.EngineInFlight > 0
		isEnhancing := engineFile.EngineGroupAlgoInFlight > 0
		totals["is_indexing"] = isIndexing
		totals["is_enhancing"] = isEnhancing
		if isIndexing {
			totals["indexing_in_flight"] = engineFile.EngineInFlight
		}
		// #5349 A3: surface an in-flight group-scope algorithm pass so a
		// coordinator can tell the daemon is busy recomputing communities /
		// centrality over the union, not just reindexing a repo.
		if isEnhancing {
			totals["group_algo_in_flight"] = engineFile.EngineGroupAlgoInFlight
		}
		if (isIndexing || isEnhancing) && !engineFile.EngineBusyStartedAt.IsZero() {
			totals["indexing_started_at"] = engineFile.EngineBusyStartedAt.UTC().Format(time.RFC3339)
		}
		return
	}
	// No fresh engine-liveness sidecar exists yet — either no engine plane has
	// ever run against this daemon root (e.g. this *mcp.Server was constructed
	// directly in a test harness with no daemon/heartbeat goroutine), or the
	// engine is down/starting/degraded. Fall back to the process-global
	// indexstate record so an in-process scheduler (true legacy monolith, or a
	// test that drives indexstate directly) is still reflected immediately
	// rather than waiting on the periodic heartbeat.
	ix := indexstate.Get()
	totals["is_indexing"] = ix.IsIndexing
	totals["is_enhancing"] = ix.IsEnhancing
	if ix.IsIndexing {
		totals["indexing_in_flight"] = ix.InFlight
	}
	if ix.IsEnhancing {
		totals["group_algo_in_flight"] = ix.GroupAlgoInFlight
	}
	if (ix.IsIndexing || ix.IsEnhancing) && !ix.StartedAt.IsZero() {
		totals["indexing_started_at"] = ix.StartedAt.UTC().Format(time.RFC3339)
	}
}
