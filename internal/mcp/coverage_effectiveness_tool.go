// coverage_effectiveness_tool.go — MCP tool for the reachability × line-coverage
// cross-product report (#5063).
//
// Tool: grafel_coverage_effectiveness
//
//	Crosses #5037 static test-reachability with #5036 ingested LCOV line
//	coverage — both stamped on entity Properties at index time by #5061 — and
//	classifies every production fn/endpoint into the meaningful quadrants:
//
//	  reachable + 0% lines  -> ineffective/tautological test  (HEADLINE, #4893)
//	  reachable + low%      -> weak coverage
//	  reachable + good%     -> covered
//	  reachable + no cov    -> measured-reachable, coverage cross unavailable
//	  unreachable           -> untested surface (#5037 orphans)
//
//	with per-module + group roll-ups, and surfaces the headline ineffective-test
//	list (a static test path reaches the entity yet not one production line
//	executed). This is the cheap, high-value surfacing path requested by #5063;
//	the dashboard surfacing is owned by #5062 / #5067.
//
// This tool does NOT recompute anything — it reads the stamped Properties off
// the loaded graph (the same way grafel_test_reachability does) and runs
// the pure coverage.ComputeEffectivenessReport over them. HONEST degradation:
// when a group/module has reachability but NO ingested line coverage, it says
// the line-coverage cross is unavailable rather than fabricating verdicts; when
// nothing is stamped at all, it tells the agent to reindex.
package mcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/cajasmota/grafel/internal/coverage"
	"github.com/cajasmota/grafel/internal/types"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// effEntity carries a stamped production entity plus its projected
// classification row, so the renderer can show name/location alongside the
// verdict without re-reading properties.
type effEntity struct {
	id         string
	name       string
	sourceFile string
	startLine  int
	isEndpoint bool
	row        coverage.EffectivenessRow
}

// handleCoverageEffectiveness implements grafel_coverage_effectiveness.
func (s *Server) handleCoverageEffectiveness(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, toolErr := s.resolveAndGroup(req)
	if toolErr != nil {
		return toolErr, nil
	}

	repoFilter := argStringSlice(req, "repo_filter")
	ineffectiveOnly := argBool(req, "ineffective_only", false)
	limit := argInt(req, "limit", 100)

	repoNames := make([]string, 0, len(lg.Repos))
	for name := range lg.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	// Collect the stamped production entities across the (filtered) repos,
	// preserving the meta we need to render, and feed them to the pure report.
	var ents []types.EntityRecord
	meta := map[string]effEntity{}
	stampedSeen := false
	for _, name := range repoNames {
		lr := lg.Repos[name]
		if lr == nil || lr.Doc == nil {
			continue
		}
		if len(repoFilter) > 0 && !repoMatchesSlice(lr.Repo, repoFilter) {
			continue
		}
		for i := range lr.Doc.Entities {
			e := &lr.Doc.Entities[i]
			if e.PropLen() == 0 {
				continue
			}
			if _, ok := e.PropLookup(coverage.PropTestReachable); !ok {
				continue
			}
			stampedSeen = true
			// The pure report reads only ID, SourceFile and Properties; build a
			// minimal record rather than depending on a full graph→record copy.
			ents = append(ents, types.EntityRecord{
				ID:         e.ID,
				Kind:       e.Kind,
				SourceFile: e.SourceFile,
				Properties: e.PropsSnapshot(),
			})
			meta[e.ID] = effEntity{
				id: e.ID, name: e.Name, sourceFile: e.SourceFile,
				startLine: e.StartLine, isEndpoint: isEndpointKind(e.Kind),
			}
		}
	}

	if !stampedSeen {
		return mcpapi.NewToolResultText(fmt.Sprintf(
			"## Coverage effectiveness — group %q\n\n"+
				"_Reachability not computed for this group._ No entity carries the "+
				"`%s` property, so the group was indexed before the #5061 enrichment "+
				"pass (or has no test/call edges).\n\n"+
				"**Reindex the group** to populate the static test-reachability signal "+
				"(and ingest LCOV for the line-coverage cross), then re-run this tool.\n",
			lg.Name, coverage.PropTestReachable,
		)), nil
	}

	rep := coverage.ComputeEffectivenessReport(ents)

	// Attach meta to rows for rendering.
	for i := range rep.Rows {
		if m, ok := meta[rep.Rows[i].EntityID]; ok {
			m.row = rep.Rows[i]
			meta[rep.Rows[i].EntityID] = m
		}
	}

	out := renderEffectiveness(lg.Name, rep, meta, ineffectiveOnly, limit)
	return mcpapi.NewToolResultText(out), nil
}

// renderEffectiveness builds the markdown report body.
func renderEffectiveness(group string, rep coverage.EffectivenessReport, meta map[string]effEntity, ineffectiveOnly bool, limit int) string {
	g := rep.Group
	out := fmt.Sprintf("## Coverage effectiveness — group %q\n\n", group)
	out += fmt.Sprintf(
		"Production entities (reachability-stamped) : %d\n"+
			"  reachable + 0%% lines (ineffective?)      : %d\n"+
			"  reachable + low coverage (<%.0f%%)         : %d\n"+
			"  reachable + covered                       : %d\n"+
			"  reachable + no line-coverage measurement  : %d\n"+
			"  unreachable (untested surface)            : %d\n",
		g.Total, g.ReachableNoLines, coverage.LowCoverageThreshold,
		g.ReachableLowCoverage, g.ReachableCovered, g.ReachableNoCoverage, g.Untested,
	)

	// Honest degradation note for the line-coverage cross.
	if !g.LineCrossAvailable() {
		out += "\n> **Line-coverage cross unavailable for this group** — no entity " +
			"carries an ingested LCOV measurement (`" + coverage.PropCoveragePct + "`). " +
			"Reporting reachability quadrants only; the ineffective-test (reachable-but-0%-lines) " +
			"signal cannot be computed without line coverage. Ingest LCOV (#5036) and reindex.\n"
	} else {
		out += fmt.Sprintf("\nLine coverage measured on %d of %d reachability-stamped entities.\n",
			g.CoverageMeasured, g.Total)
	}

	// Per-module breakdown — worst first (most ineffective+untested per total).
	if len(rep.Modules) > 0 {
		type mr struct {
			mod string
			c   coverage.EffectivenessCounts
		}
		mods := make([]mr, 0, len(rep.Modules))
		for m, c := range rep.Modules {
			mods = append(mods, mr{m, c})
		}
		sort.Slice(mods, func(i, j int) bool {
			ri := effBadRatio(mods[i].c)
			rj := effBadRatio(mods[j].c)
			if ri != rj {
				return ri > rj // worst (most ineffective/untested) first
			}
			return mods[i].mod < mods[j].mod
		})
		if len(mods) > 20 {
			mods = mods[:20]
		}
		out += "\n### Modules by ineffective+untested ratio (worst first)\n\n"
		out += fmt.Sprintf("%-6s %-6s %-6s %-6s  %s\n", "Inef", "Untst", "Cov", "Total", "Module")
		for _, m := range mods {
			out += fmt.Sprintf("%-6d %-6d %-6d %-6d  %s\n",
				m.c.ReachableNoLines, m.c.Untested, m.c.ReachableCovered, m.c.Total, m.mod)
		}
	}

	// Headline: the ineffective-test list (reachable-but-0%-lines).
	out += fmt.Sprintf("\n### Ineffective tests — reachable but 0%% lines (%d)\n\n", len(rep.Ineffective))
	if len(rep.Ineffective) == 0 {
		if g.LineCrossAvailable() {
			out += "_None — every reachable, line-measured entity executed at least one line._\n"
		} else {
			out += "_Not computable without ingested line coverage (see note above)._\n"
		}
	} else {
		out += "> A static test path reaches each of these, yet 0% of its lines ran — " +
			"likely an ineffective / tautological test. Cross-check with " +
			"`grafel_contract_test_effectiveness` (#4893).\n\n"
		shown := rep.Ineffective
		if len(shown) > limit {
			shown = shown[:limit]
		}
		for _, r := range shown {
			m := meta[r.EntityID]
			out += fmt.Sprintf("- %s  %s:%d  (reaching_tests=%d, depth=%d, lines=0%%)\n",
				m.name, m.sourceFile, m.startLine, r.ReachCount, r.ReachDepth)
		}
		if len(rep.Ineffective) > len(shown) {
			out += fmt.Sprintf("  …and %d more.\n", len(rep.Ineffective)-len(shown))
		}
	}

	if ineffectiveOnly {
		return out
	}

	// Full quadrant listing — worst quadrant first, capped at limit.
	rows := make([]coverage.EffectivenessRow, len(rep.Rows))
	copy(rows, rep.Rows)
	sort.SliceStable(rows, func(i, j int) bool {
		oi, oj := verdictOrder(rows[i].Verdict), verdictOrder(rows[j].Verdict)
		if oi != oj {
			return oi < oj
		}
		return meta[rows[i].EntityID].sourceFile < meta[rows[j].EntityID].sourceFile
	})
	shown := rows
	if len(shown) > limit {
		shown = shown[:limit]
	}
	out += fmt.Sprintf("\n### Entities by quadrant (%d shown of %d)\n\n", len(shown), len(rows))
	for _, r := range shown {
		m := meta[r.EntityID]
		detail := ""
		if r.HasCoverage {
			detail = fmt.Sprintf("  lines=%.1f%%", r.CoveragePct)
		}
		if r.Reachable {
			detail += fmt.Sprintf("  depth=%d tests=%d", r.ReachDepth, r.ReachCount)
		}
		tag := ""
		if m.isEndpoint {
			tag = "endpoint "
		}
		out += fmt.Sprintf("- [%s] %s%s  %s:%d%s\n",
			r.Verdict, tag, m.name, m.sourceFile, m.startLine, detail)
	}
	return out
}

// effBadRatio is the fraction of a bucket that is ineffective-or-untested — the
// sort key for "worst module first".
func effBadRatio(c coverage.EffectivenessCounts) float64 {
	if c.Total == 0 {
		return 0
	}
	return float64(c.ReachableNoLines+c.Untested) / float64(c.Total)
}

// verdictOrder ranks quadrants worst→best for the row listing.
func verdictOrder(v coverage.EffectivenessVerdict) int {
	switch v {
	case coverage.EffReachableNoLines:
		return 0
	case coverage.EffUntested:
		return 1
	case coverage.EffReachableLowCoverage:
		return 2
	case coverage.EffReachableNoCoverage:
		return 3
	case coverage.EffReachableCovered:
		return 4
	default:
		return 5
	}
}
