package dashboard

// handlers_coverage.go — Test-coverage HTTP surface (issue #1323).
//
// Route registered in server.go:
//
//	GET  /api/quality/coverage/{group}
//
// The handler loads each repo's indexed graph document, runs
// graph.ComputeCoverage, and aggregates the per-repo results into a
// single GroupCoverageReport.
//
// Query params:
//
//	dir=<path>     — filter ByDirectory entries by directory prefix
//	module=<name>  — filter ByModule entries by module name substring
//	severity=high|medium|low — filter UncoveredEntities by minimum severity
//	limit=<n>      — cap UncoveredEntities to n items (default 200)

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// GroupCoverageReport is the wire shape for GET /api/quality/coverage/{group}.
type GroupCoverageReport struct {
	Group             string  `json:"group"`
	TotalProduction   int     `json:"total_production"`
	CoveredProduction int     `json:"covered_production"`
	CoveragePct       float64 `json:"coverage_pct"`
	TotalTests        int     `json:"total_tests"`
	TotalTestsEdges   int     `json:"total_tests_edges"`
	Repos             int     `json:"repos"`
	// UncoveredEntities is sorted by severity (high first) and capped by
	// the ?limit query parameter (default 200).
	UncoveredEntities []graph.UncoveredEntity `json:"uncovered_entities"`
	ByDirectory       []graph.DirCoverage     `json:"by_directory"`
	ByModule          []graph.ModuleCoverage  `json:"by_module"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler
// ─────────────────────────────────────────────────────────────────────────────

// handleQualityCoverage serves GET /api/quality/coverage/{group}.
func (s *Server) handleQualityCoverage(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	if groupName == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	// Resolve repo paths using the shared helper in handlers_quality.go.
	repoPaths, err := repoPathsForGroup(groupName)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q: %v", groupName, err))
		return
	}
	if len(repoPaths) == 0 {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q has no repos", groupName))
		return
	}

	// ── query-string options ──────────────────────────────────────────────────
	q := r.URL.Query()
	filterDir := q.Get("dir")
	filterModule := q.Get("module")
	filterSeverity := strings.ToLower(q.Get("severity"))
	limit := 200
	if lStr := q.Get("limit"); lStr != "" {
		if n, pErr := strconv.Atoi(lStr); pErr == nil && n > 0 {
			limit = n
		}
	}

	// ── aggregate across repos ────────────────────────────────────────────────
	result := GroupCoverageReport{Group: groupName}

	// Accumulate per-directory and per-module maps for aggregation.
	type dirAccum struct{ total, covered int }
	type modAccum struct{ total, covered int }
	dirAcc := make(map[string]*dirAccum)
	modAcc := make(map[string]*modAccum)

	// S8 (#2159): use the cached group to avoid per-request LoadGraphFromDir.
	cachedGrpCov, _ := s.graphs.GetGroupCached(groupName)

	for _, rp := range repoPaths {
		var doc *graph.Document
		if cachedGrpCov != nil {
			if dr, ok := cachedGrpCov.Repos[rp.Slug]; ok && dr != nil {
				doc = dr.Doc
			}
		}
		if doc == nil {
			stateDir := filepath.Join(rp.Path, ".archigraph")
			var loadErr error
			doc, loadErr = graph.LoadGraphFromDir(stateDir)
			if loadErr != nil {
				// Repo not yet indexed — skip silently.
				continue
			}
		}

		report := graph.ComputeCoverage(doc)
		result.Repos++
		result.TotalProduction += report.TotalProduction
		result.CoveredProduction += report.CoveredProduction
		result.TotalTests += report.TotalTests
		result.TotalTestsEdges += report.TotalTestsEdges

		// Merge uncovered entities.
		result.UncoveredEntities = append(result.UncoveredEntities, report.UncoveredEntities...)

		// Merge per-directory stats.
		for _, d := range report.ByDirectory {
			if _, ok := dirAcc[d.Dir]; !ok {
				dirAcc[d.Dir] = &dirAccum{}
			}
			dirAcc[d.Dir].total += d.Total
			dirAcc[d.Dir].covered += d.Covered
		}

		// Merge per-module stats.
		for _, m := range report.ByModule {
			if _, ok := modAcc[m.Module]; !ok {
				modAcc[m.Module] = &modAccum{}
			}
			modAcc[m.Module].total += m.Total
			modAcc[m.Module].covered += m.Covered
		}
	}

	// ── compute group-level coverage % ───────────────────────────────────────
	if result.TotalProduction > 0 {
		result.CoveragePct = 100.0 * float64(result.CoveredProduction) / float64(result.TotalProduction)
	}

	// ── apply severity filter and cap UncoveredEntities ──────────────────────
	severityOrder := map[string]int{"high": 0, "medium": 1, "low": 2}
	minSev := 2 // default: include all
	if v, ok := severityOrder[filterSeverity]; ok {
		minSev = v
	}

	filtered := result.UncoveredEntities[:0]
	for _, u := range result.UncoveredEntities {
		if severityOrder[u.Severity] <= minSev {
			filtered = append(filtered, u)
		}
	}
	// Re-sort: severity then file then name.
	sort.SliceStable(filtered, func(i, j int) bool {
		si := severityOrder[filtered[i].Severity]
		sj := severityOrder[filtered[j].Severity]
		if si != sj {
			return si < sj
		}
		if filtered[i].SourceFile != filtered[j].SourceFile {
			return filtered[i].SourceFile < filtered[j].SourceFile
		}
		return filtered[i].Name < filtered[j].Name
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	result.UncoveredEntities = filtered

	// ── build ByDirectory with optional prefix filter ─────────────────────────
	for d, acc := range dirAcc {
		if filterDir != "" && !strings.HasPrefix(d, filterDir) {
			continue
		}
		covPct := 0.0
		if acc.total > 0 {
			covPct = 100.0 * float64(acc.covered) / float64(acc.total)
		}
		result.ByDirectory = append(result.ByDirectory, graph.DirCoverage{
			Dir:         d,
			Total:       acc.total,
			Covered:     acc.covered,
			CoveragePct: covPct,
		})
	}
	sort.Slice(result.ByDirectory, func(i, j int) bool {
		return result.ByDirectory[i].Dir < result.ByDirectory[j].Dir
	})

	// ── build ByModule with optional name filter ──────────────────────────────
	for m, acc := range modAcc {
		if filterModule != "" && !strings.Contains(m, filterModule) {
			continue
		}
		covPct := 0.0
		if acc.total > 0 {
			covPct = 100.0 * float64(acc.covered) / float64(acc.total)
		}
		result.ByModule = append(result.ByModule, graph.ModuleCoverage{
			Module:      m,
			Total:       acc.total,
			Covered:     acc.covered,
			CoveragePct: covPct,
		})
	}
	sort.Slice(result.ByModule, func(i, j int) bool {
		return result.ByModule[i].Module < result.ByModule[j].Module
	})

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}
