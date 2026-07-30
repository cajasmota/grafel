package cli

// wizard_outstanding.go — the wizard index screen's completion-honesty caveat
// (#6047).
//
// THE BUG THIS FIXES: in split mode the wizard's "Done" fires the instant the
// engine drains+acks our rebuild request (see wizard_split_progress.go's
// awaitSplitCompletion) — i.e. once extraction + the cross-repo link merge
// have written graph.fb. The graph genuinely IS queryable at that point (every
// structural MCP tool works), but the community/pagerank/centrality overlay
// (group-algo), the cross-repo link pass itself (in some orderings), and the
// post-rebuild quality-metrics scan (cmd/grafel/rebuild_history.go's
// "analytics:<group>" barge, unrelated to the overlay but still real
// outstanding work) are all stage-gated to run AFTER that ack, in the
// background, via the SAME stage-gate `grafel status` already reports through
// its annotation/stage_gate lines (see internal/cli/status_annotation.go and
// status_daemon_detail.go). A user or agent that starts querying the instant
// the wizard exits gets an overlay that is silently not current, with no
// signal from the wizard that anything is outstanding — the same defect class
// `grafel status` already had to fix for itself.
//
// THE FIX: do NOT block wizard completion on the gate releasing (that would
// turn a ~16-minute run into a ~26-minute one for a large group, for no user
// benefit — the graph is already queryable). Instead, query the daemon's
// status ONE more time the instant the terminal outcome is available and, if
// group-algo, links, or analytics is still running/pending for OUR group,
// attach a caveat naming it — reusing status_annotation.go's
// "communities/pagerank/centrality overlay not yet current; the graph itself
// is queryable" wording where it actually applies (group-algo/links) — to
// wiztui.IndexOutcome.Outstanding. Silent (empty string) when nothing is
// outstanding, which is the common case (a small group, or an already-warm
// overlay).
//
// #6047 REVIEW ROUND 2 — SPLIT VS. MONOLITH, CORRECTED. An earlier version of
// this file's doc had split/monolith backwards. The actual split:
//   - MONOLITH (s.scheduler != nil in Service.Status, service.go:394-453): the
//     scheduler lives in the SAME process answering the RPC, so the FULL
//     snapshot is available — StageGateHolder/Deferred/Barging AND the coarse
//     GroupAlgoRunning/PendingAlgo/PendingLinks lists (which name the exact
//     groups) are all populated directly from it.
//   - SPLIT (the DEFAULT — service.go:454-495, the `else if` fallback): the
//     scheduler lives in the ENGINE process, not the one answering this RPC.
//     GroupAlgoRunning/PendingAlgo/PendingLinks are structurally NEVER
//     populated here — nothing in the tree ever assigns them in this branch.
//     Only StageGateHolder/Deferred/Barging (relayed verbatim from the
//     engine's liveness sidecar, StageGateFromStatusFile) and the numeric
//     GroupAlgoInFlight (GroupAlgoInFlightFromStatusFile, also sidecar-backed)
//     are available. Since split mode is the default AND the mode #6047 was
//     measured in, outstandingCaveat must not depend on the coarse lists to
//     do anything useful — it keeps checking them (harmless in split mode,
//     where they're always empty; load-bearing in monolith mode/tests), but
//     ALSO checks GroupAlgoInFlight, the one split-mode-native "is an overlay
//     recompute happening somewhere" signal (see its doc: "the pass writes
//     the ... overlay AFTER the rebuild has already reported completion, so
//     without this the only observable symptom ... is that clusters come back
//     empty" — literally #6047's failure mode, from the commit that added the
//     signal).
//
//     KNOWN IMPRECISION, ACCEPTED: GroupAlgoInFlight is a global COUNT across
//     every group the engine happens to be running group-algo for right now —
//     it does not name groups, so a wizard finishing group A while an
//     unrelated group B's overlay pass happens to be executing will
//     (rarely, and only when the operator runs two groups concurrently)
//     over-report A as outstanding. Given the alternative in split mode is
//     ALWAYS reporting nothing (silently repeating the bug this file fixes),
//     this tradeoff is deliberate, not an oversight.
//
//     GroupAlgoInFlight also does NOT close the debounce-armed window (a
//     group-algo pass armed but not yet actually executing, so not yet
//     counted "in flight" by indexstate.Get().GroupAlgoInFlight, which backs
//     the sidecar field) — that gap is a separate, pre-existing, accepted
//     plumbing limit, not something this file's scope covers.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// graphQueryableCaveat is the exact clause status_annotation.go's
// printAnnotationStatus already prints — reused verbatim (both standalone,
// for a non-overlay stage like analytics, and as the tail of
// outstandingOverlayCaveat below) so the wizard and `grafel status` describe
// the same state in the same words.
const graphQueryableCaveat = "the graph itself is queryable"

// outstandingOverlayCaveat is the exact phrase status_annotation.go's
// printAnnotationStatus already prints for a running/pending group-algo
// pass. Used only when an OVERLAY-affecting stage (group-algo and/or links)
// is actually outstanding — never for analytics alone, which has nothing to
// do with the community/pagerank/centrality overlay (see
// cmd/grafel/rebuild_history.go's appendRebuildHistory: it is a
// quality-metrics history scan, a completely different post-rebuild batch
// that merely happens to share the stage-gate mechanism).
const outstandingOverlayCaveat = "communities/pagerank/centrality overlay not yet current; " + graphQueryableCaveat

// outstandingCaveat derives the wizard's completion caveat naming which
// post-extraction stage(s) — group-algo, links, analytics, or some
// combination — are still running or pending for group, purely from a
// proto.StatusReply snapshot. Pure and side-effect-free so it is
// unit-testable without a daemon.
//
// group-algo/links count as OUTSTANDING for group if either the coarse
// running/pending lists name it (GroupAlgoRunning/PendingAlgo/PendingLinks —
// populated only in MONOLITH mode, see this file's top doc) or the
// fine-grained stage gate (StageGateHolder/StageGateDeferred) names
// "group-algo:<group>" / "links:<group>" (populated in BOTH modes) or — for
// group-algo only, the split-mode-native signal — GroupAlgoInFlight > 0 (see
// this file's top doc for its known imprecision). analytics counts as
// outstanding if StageGateBarging names "analytics:<group>" (the
// quality-metrics history scan's foreground barge registration — see
// cmd/grafel/rebuild_history.go).
//
// Returns "" when nothing is outstanding for group — the caller (see
// attachOutstanding) leaves wiztui.IndexOutcome.Outstanding "" in that case,
// and indexView renders nothing extra (a caveat, not decoration).
func outstandingCaveat(st proto.StatusReply, group string) string {
	algoRunning := slices.Contains(st.GroupAlgoRunning, group) ||
		st.StageGateHolder == "group-algo:"+group ||
		st.GroupAlgoInFlight > 0
	algoPending := slices.Contains(st.PendingAlgo, group) || slices.Contains(st.StageGateDeferred, "group-algo:"+group)
	linksRunning := st.StageGateHolder == "links:"+group
	linksPending := slices.Contains(st.PendingLinks, group) || slices.Contains(st.StageGateDeferred, "links:"+group)
	analyticsBarging := slices.Contains(st.StageGateBarging, "analytics:"+group)

	var overlayStages []string
	switch {
	case algoRunning:
		overlayStages = append(overlayStages, "group-algo")
	case algoPending:
		overlayStages = append(overlayStages, "group-algo (pending)")
	}
	switch {
	case linksRunning:
		overlayStages = append(overlayStages, "links")
	case linksPending:
		overlayStages = append(overlayStages, "links (pending)")
	}

	var otherStages []string
	if analyticsBarging {
		otherStages = append(otherStages, "analytics")
	}

	allStages := append(append([]string{}, overlayStages...), otherStages...)
	if len(allStages) == 0 {
		return ""
	}

	// Only claim the overlay is not current when an overlay-affecting stage
	// (group-algo/links) is actually in the list — analytics alone must never
	// carry that clause, since it has nothing to do with the overlay.
	caveat := graphQueryableCaveat
	if len(overlayStages) > 0 {
		caveat = outstandingOverlayCaveat
	}
	return fmt.Sprintf("still running: %s  (%s)", strings.Join(allStages, ", "), caveat)
}

// statusFetcher mirrors client.Client.Status's signature — the seam
// attachOutstandingWith is tested against without a live daemon (#6047 review
// round 2: the production wiring from attachOutstanding through
// outstandingCaveat had no test proving the daemon is actually queried and
// its answer actually used — replacing attachOutstanding's body with "query,
// discard, return ”" left the whole ./internal/cli suite green). Production
// passes c.Status directly, which already has this exact signature.
type statusFetcher func() (proto.StatusReply, error)

// attachOutstandingWith calls fetch and folds the result through
// outstandingCaveat for group, or returns "" on ANY failure — a nil fetcher
// or an RPC error. This is the side-effect-free half of attachOutstanding,
// unit-testable with a fake fetcher.
func attachOutstandingWith(fetch statusFetcher, group string) string {
	if fetch == nil {
		return ""
	}
	st, err := fetch()
	if err != nil {
		return ""
	}
	return outstandingCaveat(st, group)
}

// attachOutstanding queries the daemon's current status (via c.Status,
// production's statusFetcher) and returns the completion caveat for group —
// best-effort, see attachOutstandingWith's doc; never attempted when c is
// nil (--no-index / daemon-down paths that never dialed).
func attachOutstanding(c *client.Client, group string) string {
	if c == nil {
		return ""
	}
	return attachOutstandingWith(c.Status, group)
}
