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
//     GUARDED, NOT UNCONDITIONAL (#6047 review round 3 — an unguarded `||`
//     shipped in round 2 was wrong): GroupAlgoInFlight is a global COUNT
//     across every group the engine happens to be running group-algo for
//     right now (indexstate.go's backing field is a bare atomic.Int64, not
//     keyed by group) — it does not name groups. In MONOLITH mode
//     GroupAlgoRunning is populated too, and is exact
//     (service.go:453 sets GroupAlgoInFlight = len(snap.GroupAlgoRunning) —
//     the two fields describe the SAME snapshot, one as names, one as a
//     count). An unguarded `|| GroupAlgoInFlight > 0` therefore let the
//     imprecise count override the precise list in the one mode where the
//     precise list exists and is correct: group A finishes (drained,
//     genuinely absent from GroupAlgoRunning) while unrelated group B's pass
//     is executing, and the wizard reported A as outstanding — exactly the
//     direction the issue forbids ("must not report an outstanding stage
//     that has already drained"). outstandingCaveat therefore only falls
//     back to the count when the list is EMPTY
//     (len(st.GroupAlgoRunning) == 0 && st.GroupAlgoInFlight > 0): empty
//     means split mode (structurally always empty there) or a genuinely idle
//     monolith (nothing running, so a stray positive count would itself be a
//     bug elsewhere) — never "monolith with an unrelated group's pass mid-
//     flight", which is exactly the case a non-empty list correctly rules
//     group out of.
//
//     GroupAlgoInFlight COVERS MORE THAN THE COMMENT ORIGINALLY CLAIMED: its
//     two increment sites are scheduler.go:2391 (the pass begins actually
//     executing) and scheduler.go:2222 (markGroupAlgoDeferredLocked — the
//     pass was ARMED but got turned away at the gate, i.e. gate-deferred).
//     So in split mode the guarded fallback also catches the gate-deferred
//     state, not just "actively executing" — which is precisely the state
//     the issue's own repro captured (`stage_gate: ... deferred=group-algo:
//     <group>,links:<group>`). It still does NOT fire at timer-arm time,
//     before the pass has reached the gate at all — that debounce-armed
//     window remains open (see below).
//
//     GroupAlgoInFlight does NOT close the debounce-armed window (a
//     group-algo pass armed but not yet having reached the gate at all, so
//     neither executing nor deferred, so not yet counted "in flight") — that
//     narrower gap is a separate, pre-existing, accepted plumbing limit, not
//     something this file's scope covers.

import (
	"fmt"
	"slices"
	"strings"

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
// group-algo only, and ONLY when GroupAlgoRunning is empty (see this file's
// top doc for why an unguarded check is wrong) — the split-mode-native
// fallback signal GroupAlgoInFlight > 0. analytics counts as outstanding if
// StageGateBarging names "analytics:<group>" (the quality-metrics history
// scan's foreground barge registration — see cmd/grafel/rebuild_history.go).
//
// Returns "" when nothing is outstanding for group — the caller (see
// attachOutstandingWith) leaves wiztui.IndexOutcome.Outstanding "" in that
// case, and indexView renders nothing extra (a caveat, not decoration).
func outstandingCaveat(st proto.StatusReply, group string) string {
	algoRunning := slices.Contains(st.GroupAlgoRunning, group) ||
		st.StageGateHolder == "group-algo:"+group ||
		// Fall back to the coarse (unnamed, global) count ONLY when the
		// precise list is empty — a non-empty GroupAlgoRunning that omits
		// group means group's pass has genuinely drained, and the count must
		// not override that (#6047 review round 3).
		(len(st.GroupAlgoRunning) == 0 && st.GroupAlgoInFlight > 0)
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
// attachOutstandingWith is tested against without a live daemon (#6047
// review round 2: an earlier attachOutstanding(c *client.Client, group
// string) took the concrete client type, so nothing but its own trivial
// nil-check was exercised by any test — a mutation that bypassed the whole
// computation and returned "" still left the suite green. #6047 review round
// 3 closed that seam: toIndexOutcome (wizard_tui_run.go) now takes a
// statusFetcher directly instead of a *client.Client, and passes c.Status —
// which already has this exact signature — at all three of its call sites.
// wizard_split_progress_test.go's toIndexOutcome tests pass a fake
// statusFetcher for the same reason, so production and tests now go through
// the identical seam).
type statusFetcher func() (proto.StatusReply, error)

// attachOutstandingWith calls fetch and folds the result through
// outstandingCaveat for group, or returns "" on ANY failure — a nil fetcher
// (--no-index / daemon-down paths that never dialed a client to take
// c.Status from) or an RPC error. This is the wizard's ONLY entry point into
// this file's completion-honesty caveat — best-effort, unit-testable with a
// fake fetcher.
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
