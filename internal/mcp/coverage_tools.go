// coverage_tools.go — MCP tool for test-coverage analysis (issue #1323).
//
// Tool: grafel_test_coverage
//
//	Returns per-entity and per-directory test coverage statistics for the
//	resolved group. Identifies production entities that have no TESTS edge
//	inbound (untested) and ranks them by severity.
//
// When entity_id is provided (#1774), returns a single-record focused answer:
// "is THIS entity tested?" — skips the full dump-all path entirely.
//
// Severity rules:
//
//	high   — HTTP endpoint without tests (unprotected surface area)
//	medium — exported Function / Method without tests
//	low    — other in-scope entities without tests
package mcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// handleTestCoverage implements grafel_test_coverage.
func (s *Server) handleTestCoverage(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, toolErr := s.resolveAndGroup(req)
	if toolErr != nil {
		return toolErr, nil
	}

	// ── entity_id fast-path (#1774) ───────────────────────────────────────────
	// When entity_id is provided, return a focused single-record answer instead
	// of the full dump. This avoids the O(all-entities) output rendering and
	// answers "is THIS entity tested?" in < 500 bytes.
	entityID := argString(req, "entity_id", "")
	if entityID != "" {
		return s.handleTestCoverageEntity(lg, entityID)
	}

	repoFilter := argStringSlice(req, "repo_filter")
	severityFilter := argString(req, "severity", "") // high | medium | low | ""
	limit := argInt(req, "limit", 100)
	topDir := argBool(req, "top_directories", false)

	// ── accumulate across repos ───────────────────────────────────────────────
	type dirAccum struct{ total, covered int }
	dirAcc := make(map[string]*dirAccum)

	totalProd := 0
	coveredProd := 0
	totalTests := 0
	totalEdges := 0
	var allUncovered []graph.UncoveredEntity

	severityOrder := map[string]int{"high": 0, "medium": 1, "low": 2}

	// Iterate over repos in sorted order for deterministic output.
	repoNames := make([]string, 0, len(lg.Repos))
	for name := range lg.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	for _, name := range repoNames {
		rd := lg.Repos[name]
		if rd == nil || rd.Doc == nil {
			continue
		}
		if len(repoFilter) > 0 && !repoMatchesSlice(rd.Repo, repoFilter) {
			continue
		}

		// #5870 PR7a: feed a transient Document built from the Reader-served
		// materialize seam instead of the raw rd.Doc (ComputeCoverage reads only
		// Entities/Relationships). Byte-identical; survives a future slice-drop.
		report := graph.ComputeCoverage(materializedDoc(rd))
		totalProd += report.TotalProduction
		coveredProd += report.CoveredProduction
		totalTests += report.TotalTests
		totalEdges += report.TotalTestsEdges

		for _, u := range report.UncoveredEntities {
			if severityFilter != "" && u.Severity != severityFilter {
				continue
			}
			allUncovered = append(allUncovered, u)
		}

		for _, d := range report.ByDirectory {
			if _, ok := dirAcc[d.Dir]; !ok {
				dirAcc[d.Dir] = &dirAccum{}
			}
			dirAcc[d.Dir].total += d.Total
			dirAcc[d.Dir].covered += d.Covered
		}
	}

	// ── compute group-level percentage ────────────────────────────────────────
	covPct := 0.0
	if totalProd > 0 {
		covPct = 100.0 * float64(coveredProd) / float64(totalProd)
	}

	// ── sort + cap uncovered list ─────────────────────────────────────────────
	sort.SliceStable(allUncovered, func(i, j int) bool {
		si := severityOrder[allUncovered[i].Severity]
		sj := severityOrder[allUncovered[j].Severity]
		if si != sj {
			return si < sj
		}
		if allUncovered[i].SourceFile != allUncovered[j].SourceFile {
			return allUncovered[i].SourceFile < allUncovered[j].SourceFile
		}
		return allUncovered[i].Name < allUncovered[j].Name
	})
	if len(allUncovered) > limit {
		allUncovered = allUncovered[:limit]
	}

	// ── build top-directory breakdown ─────────────────────────────────────────
	type dirRow struct {
		dir         string
		total       int
		covered     int
		coveragePct float64
	}
	var dirRows []dirRow
	if topDir {
		for d, acc := range dirAcc {
			p := 0.0
			if acc.total > 0 {
				p = 100.0 * float64(acc.covered) / float64(acc.total)
			}
			dirRows = append(dirRows, dirRow{
				dir:         d,
				total:       acc.total,
				covered:     acc.covered,
				coveragePct: p,
			})
		}
		// Sort least-covered first for actionability.
		sort.Slice(dirRows, func(i, j int) bool {
			if dirRows[i].coveragePct != dirRows[j].coveragePct {
				return dirRows[i].coveragePct < dirRows[j].coveragePct
			}
			return dirRows[i].dir < dirRows[j].dir
		})
		if len(dirRows) > 20 {
			dirRows = dirRows[:20]
		}
	}

	// ── render text output ────────────────────────────────────────────────────
	out := fmt.Sprintf(
		"## Test Coverage — group %q\n\n"+
			"Production entities : %d\n"+
			"Covered             : %d (%.1f%%)\n"+
			"Test entities       : %d\n"+
			"TESTS edges (total) : %d\n",
		lg.Name,
		totalProd, coveredProd, covPct,
		totalTests, totalEdges,
	)

	out += fmt.Sprintf("\n### Untested entities (%d shown", len(allUncovered))
	if severityFilter != "" {
		out += ", severity=" + severityFilter
	}
	out += ")\n\n"
	if len(allUncovered) == 0 {
		out += "_No untested entities found._\n"
	}
	for _, u := range allUncovered {
		out += fmt.Sprintf("- [%s] %s  %s:%d\n",
			u.Severity, u.Name, u.SourceFile, u.StartLine)
	}

	if topDir && len(dirRows) > 0 {
		out += "\n### Directories by coverage (lowest first)\n\n"
		out += fmt.Sprintf("%-8s %-6s %-6s  %s\n", "Covered%", "Cvrd", "Total", "Dir")
		for _, d := range dirRows {
			out += fmt.Sprintf("%-8.1f %-6d %-6d  %s\n", d.coveragePct, d.covered, d.total, d.dir)
		}
	}

	// Freshness signal (#5068): is any ingested line-coverage measurement stale
	// relative to the latest index? Degrades honestly when nothing is ingested.
	out += renderCoverageFreshness(computeCoverageFreshness(lg))

	return mcpapi.NewToolResultText(out), nil
}

// handleTestCoverageEntity is the entity_id fast-path for handleTestCoverage
// (#1774). It searches all repos in the loaded group for the entity, runs the
// two-phase coverage algorithm restricted to that single entity, and returns a
// compact single-record result.
func (s *Server) handleTestCoverageEntity(lg *LoadedGroup, entityID string) (*mcpapi.CallToolResult, error) {
	// Search repos in sorted order for deterministic behaviour.
	repoNames := make([]string, 0, len(lg.Repos))
	for name := range lg.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	for _, name := range repoNames {
		rd := lg.Repos[name]
		if rd == nil || rd.Doc == nil {
			continue
		}

		// #5870 PR7a: transient Document from the Reader-served materialize seam.
		result, found := graph.ComputeEntityCoverage(materializedDoc(rd), entityID)
		if !found {
			continue
		}

		// Render compact single-record output.
		testedStr := "no"
		if result.Tested {
			testedStr = "yes"
		}
		out := fmt.Sprintf(
			"## Test Coverage — entity %q\n\n"+
				"entity_id         : %s\n"+
				"name              : %s\n"+
				"kind              : %s\n"+
				"source_file       : %s (line %d)\n"+
				"severity          : %s\n"+
				"tested            : %s\n"+
				"coverage_fraction : %.2f\n"+
				"covering_tests    : %d\n",
			entityID,
			result.EntityID,
			result.Name,
			result.Kind,
			result.SourceFile, result.StartLine,
			result.Severity,
			testedStr,
			result.CoverageFraction,
			len(result.CoveringTests),
		)
		if len(result.CoveringTests) > 0 {
			out += "\n### Covering test entity IDs\n\n"
			for _, tid := range result.CoveringTests {
				out += "- " + tid + "\n"
			}
		}
		return mcpapi.NewToolResultText(out), nil
	}

	// Entity not found in any repo.
	return mcpapi.NewToolResultText(
		fmt.Sprintf("entity not found: %q — check the entity_id is correct and the group is fully loaded.", entityID),
	), nil
}

// repoMatchesSlice returns true when slug matches any entry in filter.
func repoMatchesSlice(slug string, filter []string) bool {
	for _, f := range filter {
		if f == slug {
			return true
		}
	}
	return false
}
