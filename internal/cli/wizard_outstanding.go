package cli

// wizard_outstanding.go — the wizard index screen's completion-honesty caveat
// (#6047).
//
// THE BUG THIS FIXES: in split mode the wizard's "Done" fires the instant the
// engine drains+acks our rebuild request (see wizard_split_progress.go's
// awaitSplitCompletion) — i.e. once extraction + the cross-repo link merge
// have written graph.fb. The graph genuinely IS queryable at that point (every
// structural MCP tool works), but the community/pagerank/centrality overlay
// (group-algo) and, in some orderings, the cross-repo link pass itself are
// stage-gated to run AFTER that ack, in the background, via the SAME
// stage-gate `grafel status` already reports through its annotation/
// stage_gate lines (see internal/cli/status_annotation.go and
// status_daemon_detail.go). A user or agent that starts querying the instant
// the wizard exits gets an overlay that is silently not current, with no
// signal from the wizard that anything is outstanding — the same defect class
// `grafel status` already had to fix for itself.
//
// THE FIX: do NOT block wizard completion on the gate releasing (that would
// turn a ~16-minute run into a ~26-minute one for a large group, for no user
// benefit — the graph is already queryable). Instead, query the daemon's
// status ONE more time the instant the terminal outcome is available and, if
// group-algo or links is still running/pending for OUR group, attach a caveat
// naming it — in the SAME vocabulary status_annotation.go's
// "communities/pagerank/centrality overlay not yet current; the graph itself
// is queryable" line already uses — to wiztui.IndexOutcome.Outstanding. Silent
// (empty string) when nothing is outstanding, which is the common case (a
// small group, or an already-warm overlay).

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// outstandingOverlayCaveat is the exact phrase status_annotation.go's
// printAnnotationStatus already prints for a running/pending group-algo pass —
// reused verbatim here so the wizard and `grafel status` describe the same
// state in the same words.
const outstandingOverlayCaveat = "communities/pagerank/centrality overlay not yet current; the graph itself is queryable"

// outstandingCaveat derives the wizard's completion caveat naming which
// post-extraction stage(s) — group-algo, links, or both — are still running
// or pending for group, purely from a proto.StatusReply snapshot. Pure and
// side-effect-free so it is unit-testable without a daemon.
//
// A stage counts as OUTSTANDING for group if either the coarse
// running/pending lists name it (GroupAlgoRunning/PendingAlgo/PendingLinks —
// the same fields printAnnotationStatus and printDaemonDetail's scheduler
// line read) or the fine-grained stage gate (StageGateHolder/
// StageGateDeferred) names "group-algo:<group>" / "links:<group>" — the exact
// holder/deferred naming scheme documented on
// internal/daemon/sched/scheduler.go's StageHolder/StageDeferred fields.
// Checking both catches every mode: split-mode's status snapshot populates
// the coarse lists (Service.Status' snap.PendingAlgo / snap.GroupAlgoRunning
// branch), monolith's populates the stage gate directly (the `g.Holder` /
// `g.Deferred` branch) — see internal/daemon/service.go.
//
// Returns "" when neither stage is outstanding for group — the caller (see
// attachOutstanding) leaves wiztui.IndexOutcome.Outstanding "" in that case,
// and indexView renders nothing extra (a caveat, not decoration).
func outstandingCaveat(st proto.StatusReply, group string) string {
	algoRunning := slices.Contains(st.GroupAlgoRunning, group) || st.StageGateHolder == "group-algo:"+group
	algoPending := slices.Contains(st.PendingAlgo, group) || slices.Contains(st.StageGateDeferred, "group-algo:"+group)
	linksRunning := st.StageGateHolder == "links:"+group
	linksPending := slices.Contains(st.PendingLinks, group) || slices.Contains(st.StageGateDeferred, "links:"+group)

	var stages []string
	switch {
	case algoRunning:
		stages = append(stages, "group-algo")
	case algoPending:
		stages = append(stages, "group-algo (pending)")
	}
	switch {
	case linksRunning:
		stages = append(stages, "links")
	case linksPending:
		stages = append(stages, "links (pending)")
	}
	if len(stages) == 0 {
		return ""
	}
	return fmt.Sprintf("still running: %s  (%s)", strings.Join(stages, ", "), outstandingOverlayCaveat)
}

// attachOutstanding queries the daemon's current status and returns the
// completion caveat for group (see outstandingCaveat), or "" on ANY failure —
// a dead/unreachable client, an RPC error, or nothing outstanding. This is a
// best-effort, non-blocking honesty check on an already-successful
// completion, never a reason to fail or delay it: a status query that errors
// degrades to "no caveat shown" exactly like an absent rssMB reading degrades
// to "no readout shown" (see wizard_metrics.go's engineMetricsFuncWith).
func attachOutstanding(c *client.Client, group string) string {
	if c == nil {
		return ""
	}
	st, err := c.Status()
	if err != nil {
		return ""
	}
	return outstandingCaveat(st, group)
}
