package dashboard

// handlers_coverage_reach.go — static test-reachability surfacing (#5062).
//
// #5037 computes, without executing anything, which production endpoints are
// reachable from at least one test (the reachability pass in
// internal/coverage/reachability.go stamps test_reachable / reaching_tests /
// reach_depth onto entity Properties at index time, #5061).
//
// This file folds those stamped props into the GET /api/quality/coverage/{group}
// payload as a ReachabilitySummary: an endpoint-level tested/untested roll-up
// plus the high-value ORPHAN list — endpoints with NO test reaching their
// handler. The dashboard CoverageTab renders the orphan surface so an agent
// doing a parity rewrite can see exactly which architectural surfaces have zero
// test exercising them.
//
// Degradation: if NO endpoint in the group carries a test_reachable prop, the
// pass was not run (pre-#5061 index). In that case Computed is false and the UI
// shows "reachability not computed — reindex" rather than implying every
// endpoint is untested.

import (
	"sort"
	"strconv"

	"github.com/cajasmota/grafel/internal/coverage"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// ReachabilitySummary is the wire shape for the static test-reachability signal
// (#5037) surfaced on the coverage report. It is intentionally distinct from
// LineCoverageSummary (executed line %) and from the reach CoveragePct band:
// this is the endpoint-level "has any test path reaching it" signal.
type ReachabilitySummary struct {
	// Computed reports whether the reachability pass ran for this group. When
	// false (no endpoint carried a test_reachable prop — a pre-#5061 index),
	// the counts are meaningless and the UI must show "not computed — reindex"
	// instead of implying everything is untested.
	Computed bool `json:"computed"`

	// TotalEndpoints / TestedEndpoints / OrphanEndpoints are the endpoint-level
	// roll-up: how many HTTP endpoint surfaces have >=1 test reaching their
	// handler vs none. Orphan = Total - Tested.
	TotalEndpoints  int `json:"total_endpoints"`
	TestedEndpoints int `json:"tested_endpoints"`
	OrphanEndpoints int `json:"orphan_endpoints"`

	// ReachablePct is 100*Tested/Total in [0,100]; 0 when Total is zero.
	ReachablePct float64 `json:"reachable_pct"`

	// Orphans is the capped, sorted list of endpoints with no test path
	// reaching them — the actionable surface highlighted in the UI.
	Orphans []ReachOrphanEndpoint `json:"orphans"`

	// OrphansMore is the number of orphan endpoints elided beyond the cap.
	OrphansMore int `json:"orphans_more,omitempty"`
}

// ReachOrphanEndpoint is one untested endpoint row.
type ReachOrphanEndpoint struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	SourceFile string `json:"source_file,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	Repo       string `json:"repo,omitempty"`
}

// reachOrphanCap bounds the orphan list returned on the wire so a group with a
// large untested surface does not produce an unbounded payload. Matches the
// spirit of PerFileUncoveredCap.
const reachOrphanCap = 200

// reachAccumulator folds endpoint-level reachability props across a group's
// repo documents. Zero value is ready to use.
type reachAccumulator struct {
	computed bool
	total    int
	tested   int
	orphans  []ReachOrphanEndpoint
}

// accumulate scans one repo document for HTTP endpoint entities carrying the
// #5037 test_reachable prop, updating the roll-up and collecting orphans. repo
// is the owning repo slug, attached to each orphan so the UI can resolve its
// source through the correct repo root in a multi-repo group.
func (a *reachAccumulator) accumulate(doc *graph.Document, repo string) {
	if doc == nil {
		return
	}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.PropLen() == 0 || !isEndpointReachKind(e.Kind) {
			continue
		}
		val, ok := e.PropLookup(coverage.PropTestReachable)
		if !ok {
			// Endpoint exists but was never visited by the reachability pass
			// for this index — do not count it (avoids implying "untested").
			continue
		}
		a.computed = true
		a.total++
		reachable, _ := strconv.ParseBool(val)
		if reachable {
			a.tested++
			continue
		}
		a.orphans = append(a.orphans, ReachOrphanEndpoint{
			ID:         e.ID,
			Name:       e.Name,
			Kind:       e.Kind,
			SourceFile: e.SourceFile,
			StartLine:  e.StartLine,
			Repo:       repo,
		})
	}
}

// summarize turns the accumulated state into the wire shape, sorting and
// capping the orphan list. Returns nil only when no document was ever scanned;
// when scanned-but-not-stamped it returns a {Computed:false} summary so the UI
// can distinguish "not computed" from "no orphans".
func (a *reachAccumulator) summarize() *ReachabilitySummary {
	s := &ReachabilitySummary{
		Computed:        a.computed,
		TotalEndpoints:  a.total,
		TestedEndpoints: a.tested,
		OrphanEndpoints: a.total - a.tested,
	}
	if a.total > 0 {
		s.ReachablePct = 100.0 * float64(a.tested) / float64(a.total)
	}

	orphans := a.orphans
	sort.SliceStable(orphans, func(i, j int) bool {
		if orphans[i].SourceFile != orphans[j].SourceFile {
			return orphans[i].SourceFile < orphans[j].SourceFile
		}
		if orphans[i].StartLine != orphans[j].StartLine {
			return orphans[i].StartLine < orphans[j].StartLine
		}
		return orphans[i].Name < orphans[j].Name
	})
	if len(orphans) > reachOrphanCap {
		s.OrphansMore = len(orphans) - reachOrphanCap
		orphans = orphans[:reachOrphanCap]
	}
	s.Orphans = orphans
	return s
}

// isEndpointReachKind reports whether a kind is an HTTP endpoint surface for
// the reachability roll-up. Mirrors the MCP tool's isEndpointKind (#5060) so
// the dashboard and MCP agree on what counts as an endpoint.
func isEndpointReachKind(kind string) bool {
	switch types.EntityKind(kind) {
	case types.EntityKindEndpoint,
		types.EntityKindRoute,
		types.EntityKindHTTPEndpointDefinition:
		return true
	}
	return false
}
