// reachability_tools.go — MCP tool for static test-reachability (#5060).
//
// Tool: grafel_test_reachability
//
//	Surfaces the static test-reachability signal computed by #5037 and stamped
//	onto production entities at index time by the #5061 enrichment pass. Unlike
//	grafel_test_coverage (which only checks for a *direct* inbound TESTS
//	edge), this tool reflects transitive reachability over TESTS+CALLS edges:
//	"is there ANY test path that reaches this function/endpoint?" — with the
//	reaching tests and minimum hop depth.
//
// The KEY use case is orphan discovery: which endpoints / handlers / functions
// have NO test path reaching them at all (the parity-rewrite risk surface).
//
// This tool does NOT recompute anything. It reads the properties the indexer
// already stamped (coverage.PropTestReachable / PropReachingTests /
// PropReachingTestCount / PropReachDepth) directly off the loaded graph, the
// same way the other read-only MCP tools read entities. When those properties
// are absent for a group (indexed before #5061, or with no test edges), it says
// so explicitly rather than returning a misleading all-zero result.
package mcp

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/cajasmota/grafel/internal/coverage"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// reachRow is one production entity's reachability verdict, projected from its
// stamped properties.
type reachRow struct {
	id          string
	name        string
	kind        string
	sourceFile  string
	startLine   int
	isEndpoint  bool
	reachable   bool
	depth       int
	reachCount  int    // distinct reaching tests (uncapped)
	reaching    string // comma-joined capped list, as stamped
	crossSignal coverage.CrossSignalVerdict
}

// handleTestReachability implements grafel_test_reachability.
func (s *Server) handleTestReachability(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, toolErr := s.resolveAndGroup(req)
	if toolErr != nil {
		return toolErr, nil
	}

	entityID := argString(req, "entity_id", "")
	repoFilter := argStringSlice(req, "repo_filter")
	untestedOnly := argBool(req, "untested_only", false)
	endpointsOnly := argBool(req, "endpoints_only", false)
	limit := argInt(req, "limit", 100)

	// ── collect projected rows across the (filtered) repos ────────────────────
	repoNames := make([]string, 0, len(lg.Repos))
	for name := range lg.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	var rows []reachRow
	stampedSeen := false // did ANY entity carry the reachability prop?
	modAcc := map[string]*coverage.RollUp{}

	for _, name := range repoNames {
		lr := lg.Repos[name]
		if lr == nil || lr.Doc == nil {
			continue
		}
		if len(repoFilter) > 0 && !repoMatchesSlice(lr.Repo, repoFilter) {
			continue
		}
		lr.forEachEntity(func(e *graph.Entity) bool {
			if e.PropLen() == 0 {
				return true
			}
			val, ok := e.PropLookup(coverage.PropTestReachable)
			if !ok {
				return true // not a reachability-considered production entity
			}
			stampedSeen = true

			row := projectReachRow(e, val)

			// module roll-up (over all stamped production entities, pre-filter).
			mod := moduleOf(e)
			acc := modAcc[mod]
			if acc == nil {
				acc = &coverage.RollUp{}
				modAcc[mod] = acc
			}
			acc.Total++
			if row.reachable {
				acc.Reachable++
			}

			// row-level filters.
			if entityID != "" && row.id != entityID {
				return true
			}
			if endpointsOnly && !row.isEndpoint {
				return true
			}
			if untestedOnly && row.reachable {
				return true
			}
			rows = append(rows, row)
			return true
		})
	}

	// ── honesty gate: nothing stamped → tell the agent to reindex ─────────────
	if !stampedSeen {
		return mcpapi.NewToolResultText(fmt.Sprintf(
			"## Test reachability — group %q\n\n"+
				"_Reachability not computed for this group._ No entity carries the "+
				"`%s` property, which means the group was indexed before the #5061 "+
				"reachability enrichment pass (or has no test/call edges to traverse).\n\n"+
				"**Reindex the group** to populate the static test-reachability signal, "+
				"then re-run this tool.\n",
			lg.Name, coverage.PropTestReachable,
		)), nil
	}

	// ── group + endpoint roll-ups ─────────────────────────────────────────────
	var grpTotal, grpReach, epTotal, epReach int
	for _, a := range modAcc {
		grpTotal += a.Total
		grpReach += a.Reachable
	}
	// Endpoint roll-up is derived from rows we projected as endpoints; but
	// because row-level filters may have excluded some, recompute from the docs
	// is overkill — instead count endpoints across the *unfiltered* stamped set.
	epTotal, epReach = endpointRollup(lg, repoFilter)

	// ── sort rows: orphans first, then most-reaching, then location ───────────
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].reachable != rows[j].reachable {
			return !rows[i].reachable // unreachable (orphans) first
		}
		if rows[i].isEndpoint != rows[j].isEndpoint {
			return rows[i].isEndpoint // endpoints before plain functions
		}
		if rows[i].sourceFile != rows[j].sourceFile {
			return rows[i].sourceFile < rows[j].sourceFile
		}
		return rows[i].startLine < rows[j].startLine
	})
	shown := rows
	if len(shown) > limit {
		shown = shown[:limit]
	}

	// ── render ────────────────────────────────────────────────────────────────
	out := fmt.Sprintf("## Test reachability — group %q\n\n", lg.Name)
	out += fmt.Sprintf(
		"Production entities : %d\n"+
			"Test-reachable      : %d (%s)\n"+
			"Orphans (untested)  : %d\n",
		grpTotal, grpReach, pct(grpReach, grpTotal),
		grpTotal-grpReach,
	)
	if epTotal > 0 {
		out += fmt.Sprintf(
			"Endpoints           : %d\n"+
				"Endpoints reachable : %d (%s)\n"+
				"Endpoint orphans    : %d\n",
			epTotal, epReach, pct(epReach, epTotal), epTotal-epReach,
		)
	}

	// per-module breakdown, least-reachable first.
	if len(modAcc) > 0 {
		type mr struct {
			mod   string
			total int
			reach int
		}
		mods := make([]mr, 0, len(modAcc))
		for m, a := range modAcc {
			mods = append(mods, mr{m, a.Total, a.Reachable})
		}
		sort.Slice(mods, func(i, j int) bool {
			pi := ratio(mods[i].reach, mods[i].total)
			pj := ratio(mods[j].reach, mods[j].total)
			if pi != pj {
				return pi < pj
			}
			return mods[i].mod < mods[j].mod
		})
		if len(mods) > 20 {
			mods = mods[:20]
		}
		out += "\n### Modules by reachability (lowest first)\n\n"
		out += fmt.Sprintf("%-9s %-6s %-6s  %s\n", "Reach%", "Rch", "Total", "Module")
		for _, m := range mods {
			out += fmt.Sprintf("%-9s %-6d %-6d  %s\n", pct(m.reach, m.total), m.reach, m.total, m.mod)
		}
	}

	// the row listing — defaults to orphans-first so "what has no test path" is
	// answered at the top.
	heading := "Entities"
	if untestedOnly {
		heading = "Orphans (no test path)"
	} else if endpointsOnly {
		heading = "Endpoints"
	}
	out += fmt.Sprintf("\n### %s (%d shown of %d)\n\n", heading, len(shown), len(rows))
	if len(shown) == 0 {
		out += "_No matching entities._\n"
	}
	for _, r := range shown {
		marker := "ORPHAN"
		detail := ""
		if r.reachable {
			marker = "reachable"
			detail = fmt.Sprintf("  depth=%d tests=%d", r.depth, r.reachCount)
			if r.crossSignal == coverage.CrossSignalReachableNoLines {
				detail += "  [reachable-but-0%-lines]"
			}
		}
		tag := ""
		if r.isEndpoint {
			tag = "endpoint "
		}
		out += fmt.Sprintf("- [%s] %s%s  %s:%d%s\n",
			marker, tag, r.name, r.sourceFile, r.startLine, detail)
		if r.reachable && r.reaching != "" && (entityID != "" || len(shown) <= 25) {
			out += "    reaching_tests: " + r.reaching + "\n"
		}
	}

	// Freshness signal (#5068): any reachable-but-0%-lines cross-signal above is
	// only meaningful against a CURRENT coverage report — surface whether the
	// ingested measurement is stale relative to the latest index.
	out += renderCoverageFreshness(computeCoverageFreshness(lg))

	return mcpapi.NewToolResultText(out), nil
}

// projectReachRow reads the stamped reachability properties off a loaded entity
// into a reachRow. reachableStr is the already-fetched PropTestReachable value.
func projectReachRow(e *graph.Entity, reachableStr string) reachRow {
	reachable, _ := strconv.ParseBool(reachableStr)
	row := reachRow{
		id:          e.ID,
		name:        e.Name,
		kind:        e.Kind,
		sourceFile:  e.SourceFile,
		startLine:   e.StartLine,
		isEndpoint:  isEndpointKind(e.Kind),
		reachable:   reachable,
		crossSignal: coverage.CrossSignal(e.PropsSnapshot()),
	}
	if reachable {
		row.depth, _ = strconv.Atoi(e.PropGet(coverage.PropReachDepth))
		row.reachCount, _ = strconv.Atoi(e.PropGet(coverage.PropReachingTestCount))
		row.reaching = e.PropGet(coverage.PropReachingTests)
	}
	return row
}

// endpointRollup counts stamped endpoint entities and how many are reachable,
// across the (filtered) repos. Separate from the row projection so the endpoint
// summary is independent of row-level filters.
func endpointRollup(lg *LoadedGroup, repoFilter []string) (total, reachable int) {
	for _, lr := range lg.Repos {
		if lr == nil || lr.Doc == nil {
			continue
		}
		if len(repoFilter) > 0 && !repoMatchesSlice(lr.Repo, repoFilter) {
			continue
		}
		lr.forEachEntity(func(e *graph.Entity) bool {
			if e.PropLen() == 0 || !isEndpointKind(e.Kind) {
				return true
			}
			val, ok := e.PropLookup(coverage.PropTestReachable)
			if !ok {
				return true
			}
			total++
			if r, _ := strconv.ParseBool(val); r {
				reachable++
			}
			return true
		})
	}
	return total, reachable
}

// isEndpointKind reports whether a kind is an HTTP endpoint surface (both the
// SCOPE.Endpoint scope kind and the http_endpoint_definition cross-link kind).
func isEndpointKind(kind string) bool {
	switch types.EntityKind(kind) {
	case types.EntityKindEndpoint,
		types.EntityKindRoute,
		types.EntityKindHTTPEndpointDefinition:
		return true
	}
	return false
}

// moduleOf returns an entity's module bucket: the stamped Properties["module"]
// when present, else the containing directory of its source file.
func moduleOf(e *graph.Entity) string {
	if e.PropLen() > 0 {
		if m := e.PropGet("module"); m != "" {
			return m
		}
	}
	src := e.SourceFile
	if src == "" {
		return "."
	}
	for i := len(src) - 1; i >= 0; i-- {
		if src[i] == '/' || src[i] == '\\' {
			return src[:i]
		}
	}
	return "."
}

// pct formats reach/total as a percentage string; "n/a" when total is zero.
func pct(reach, total int) string {
	if total == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100.0*float64(reach)/float64(total))
}

// ratio returns reach/total in [0,1]; 0 when total is zero (sort helper).
func ratio(reach, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(reach) / float64(total)
}
