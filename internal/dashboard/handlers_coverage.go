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
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/coverage"
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
	// ContractCoveredOnly / ContractCoveredPct expose the secondary
	// contract-covered band (#4662): endpoints whose shape is asserted by an
	// offline contract spec but which no test executes. They are NEVER folded
	// into CoveredProduction/CoveragePct, which stay pure reach-coverage.
	ContractCoveredOnly int     `json:"contract_covered_only"`
	ContractCoveredPct  float64 `json:"contract_covered_pct"`
	TotalTests          int     `json:"total_tests"`
	TotalTestsEdges     int     `json:"total_tests_edges"`
	Repos               int     `json:"repos"`
	// UncoveredEntities is sorted by severity (high first) and capped by
	// the ?limit query parameter (default 200).
	UncoveredEntities []graph.UncoveredEntity `json:"uncovered_entities"`
	ByDirectory       []graph.DirCoverage     `json:"by_directory"`
	// ByFile is the per-file breakdown (deepest grouping). Directory rollups in
	// ByDirectory are sums of their files; the frontend nests files under their
	// directory using the shared path segments.
	ByFile   []graph.FileCoverage   `json:"by_file"`
	ByModule []graph.ModuleCoverage `json:"by_module"`
	// ByFileUncovered (#4636) nests uncovered entities under their owning file
	// (keyed by the forward-slash source path, matching ByFile.File) so the
	// frontend can render them as leaf children of each file node in the
	// coverage tree: directory → file → entity. Severity sorted (high first)
	// and capped per file (PerFileUncoveredCap) with a per-file overflow count.
	ByFileUncovered map[string]FileUncovered `json:"by_file_uncovered,omitempty"`

	// LineCoverage (#5066) surfaces the REAL ingested line coverage (#5036)
	// when a coverage report (LCOV/Cobertura/JaCoCo) was ingested and stamped
	// onto entities at index time (#5061). It is distinct from CoveragePct,
	// which is graph-derived reach coverage (static test-reachability), not a
	// measured line %. Nil when no entity in the group carries the stamped
	// `coverage_source` prop — the common case until a report is ingested — so
	// the provenance banner degrades cleanly to reachability/capability.
	LineCoverage *LineCoverageSummary `json:"line_coverage,omitempty"`
}

// LineCoverageSummary is the group-level roll-up of the per-entity ingested
// line-coverage props (#5036) stamped at index time (#5061). It is the
// authoritative, executed line % — never conflated with reach coverage.
//
// Presence of this object is itself the "report ingestion ran for this group"
// signal the provenance banner (#5038) keys off: an entity only carries
// `coverage_source` if a report was actually ingested.
type LineCoverageSummary struct {
	// Source is the coverage_source prop, e.g. "lcov" (the first non-empty
	// source seen; v1 honors a single ingestor per group).
	Source string `json:"source"`
	// CoveredLines / TotalLines are the group-wide sums over file-scope
	// entities (whole-file roll-ups), so the percentage is a true line ratio
	// and not double-counted across nested span-bearing entities.
	CoveredLines int `json:"covered_lines"`
	TotalLines   int `json:"total_lines"`
	// CoveragePct is 100*CoveredLines/TotalLines, or 0 when TotalLines==0.
	CoveragePct float64 `json:"coverage_pct"`
	// MeasuredAt is the coverage_measured_at prop (RFC3339), the latest seen
	// across entities. Empty when the ingestor did not stamp a timestamp.
	MeasuredAt string `json:"measured_at,omitempty"`
	// Entities is how many entities carried a stamped coverage_source prop —
	// distinguishes a real (if zero-line) ingestion from no ingestion at all.
	Entities int `json:"entities"`
}

// lineCovAccumulator folds stamped ingested line-coverage props (#5036/#5061)
// from one or more documents into a group-level roll-up. It is keyed by the
// forward-slash source path so the line ratio is a true per-file roll-up and
// not double-counted across the nested span-bearing entities that share a file:
// for each file it keeps the widest (whole-file-scope) total_lines stamp seen.
//
// Kept as a small pure helper (no Server/IO) so the aggregation policy is
// directly unit-testable (#5066).
type lineCovAccumulator struct {
	byFile     map[string]lineCovFile // source path → widest file stamp
	source     string                 // first non-empty coverage_source seen
	measuredAt string                 // latest coverage_measured_at seen
	entities   int                    // count of entities carrying coverage_source
}

type lineCovFile struct{ covered, total int }

// accumulate folds every entity in doc that carries a coverage_source prop.
func (a *lineCovAccumulator) accumulate(doc *graph.Document) {
	if doc == nil {
		return
	}
	for ei := range doc.Entities {
		ent := &doc.Entities[ei]
		if len(ent.Properties) == 0 {
			continue
		}
		src := ent.Properties[coverage.PropCoverageSource]
		if src == "" {
			continue
		}
		a.entities++
		if a.source == "" {
			a.source = src
		}
		// RFC3339 timestamps sort lexicographically, so a string compare picks
		// the most recent measurement without parsing.
		if at := ent.Properties[coverage.PropCoverageMeasAt]; at > a.measuredAt {
			a.measuredAt = at
		}
		covered, cErr := strconv.Atoi(ent.Properties[coverage.PropCoveredLines])
		total, tErr := strconv.Atoi(ent.Properties[coverage.PropTotalLines])
		if cErr != nil || tErr != nil || total <= 0 {
			continue
		}
		key := filepath.ToSlash(ent.SourceFile)
		if key == "" {
			key = ent.ID
		}
		if a.byFile == nil {
			a.byFile = make(map[string]lineCovFile)
		}
		// A file-scope entity reports the full file's total_lines; a span
		// entity a subset. Keeping the max total (with its covered) avoids both
		// double-counting and undercounting.
		if cur, ok := a.byFile[key]; !ok || total > cur.total {
			a.byFile[key] = lineCovFile{covered: covered, total: total}
		}
	}
}

// summarize renders the wire shape, or nil when no report was ingested (no
// entity carried coverage_source) so the provenance banner degrades cleanly.
func (a *lineCovAccumulator) summarize() *LineCoverageSummary {
	if a.entities == 0 {
		return nil
	}
	s := &LineCoverageSummary{
		Source:     a.source,
		MeasuredAt: a.measuredAt,
		Entities:   a.entities,
	}
	for _, f := range a.byFile {
		s.CoveredLines += f.covered
		s.TotalLines += f.total
	}
	if s.TotalLines > 0 {
		s.CoveragePct = 100.0 * float64(s.CoveredLines) / float64(s.TotalLines)
	}
	return s
}

// PerFileUncoveredCap bounds how many uncovered-entity leaves a single file
// node carries in the payload, so a pathological file can't bloat the response.
// The overflow is reported as FileUncovered.More.
const PerFileUncoveredCap = 50

// FileUncovered is the per-file slice of uncovered entities for the tree leaves.
type FileUncovered struct {
	// Entities are this file's uncovered entities, severity-sorted (high first),
	// capped at PerFileUncoveredCap.
	Entities []graph.UncoveredEntity `json:"entities"`
	// More is the number of additional uncovered entities beyond the cap, shown
	// as a "+N more" affordance in the UI.
	More int `json:"more,omitempty"`
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
	type dirAccum struct{ total, covered, contractOnly int }
	type modAccum struct{ total, covered int }
	type fileAccum struct {
		dir                          string
		total, covered, contractOnly int
	}
	dirAcc := make(map[string]*dirAccum)
	modAcc := make(map[string]*modAccum)
	fileAcc := make(map[string]*fileAccum)

	// Ingested line-coverage roll-up (#5066). The #5036 props are stamped onto
	// entities at index time (#5061); accumulateLineCoverage folds each doc's
	// stamped entities into lineCov, which summarize() turns into the wire
	// shape once all repos are scanned.
	var lineCov lineCovAccumulator

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
		result.ContractCoveredOnly += report.ContractCoveredOnly
		result.TotalTests += report.TotalTests
		result.TotalTestsEdges += report.TotalTestsEdges

		// Merge uncovered entities, stamping the owning repo slug so the UI can
		// resolve each entity's source through the correct repo root in a
		// multi-repo group (#4551). ComputeCoverage runs per-document and does
		// not know the slug, so it is the aggregator's job to attach it here.
		for i := range report.UncoveredEntities {
			report.UncoveredEntities[i].Repo = rp.Slug
		}
		result.UncoveredEntities = append(result.UncoveredEntities, report.UncoveredEntities...)

		// Merge per-directory stats.
		for _, d := range report.ByDirectory {
			if _, ok := dirAcc[d.Dir]; !ok {
				dirAcc[d.Dir] = &dirAccum{}
			}
			dirAcc[d.Dir].total += d.Total
			dirAcc[d.Dir].covered += d.Covered
			dirAcc[d.Dir].contractOnly += d.ContractOnly
		}

		// Merge per-file stats.
		for _, f := range report.ByFile {
			if _, ok := fileAcc[f.File]; !ok {
				fileAcc[f.File] = &fileAccum{dir: f.Dir}
			}
			fileAcc[f.File].total += f.Total
			fileAcc[f.File].covered += f.Covered
			fileAcc[f.File].contractOnly += f.ContractOnly
		}

		// Merge per-module stats.
		for _, m := range report.ByModule {
			if _, ok := modAcc[m.Module]; !ok {
				modAcc[m.Module] = &modAccum{}
			}
			modAcc[m.Module].total += m.Total
			modAcc[m.Module].covered += m.Covered
		}

		// Fold this repo's stamped ingested line-coverage props (#5066).
		lineCov.accumulate(doc)
	}

	// Presence of any stamped entity ⇒ a report was ingested for this group, so
	// we emit the authoritative line % (distinct from reach CoveragePct). When
	// nothing was stamped, LineCoverage stays nil and the banner degrades.
	result.LineCoverage = lineCov.summarize()

	// ── compute group-level coverage % ───────────────────────────────────────
	// CoveragePct stays pure reach-coverage; ContractCoveredPct is the union
	// band (reach + contract-covered-only) the UI renders behind it (#4662).
	if result.TotalProduction > 0 {
		result.CoveragePct = 100.0 * float64(result.CoveredProduction) / float64(result.TotalProduction)
		result.ContractCoveredPct = 100.0 *
			float64(result.CoveredProduction+result.ContractCoveredOnly) / float64(result.TotalProduction)
	}

	// ── apply severity filter and cap UncoveredEntities ──────────────────────
	severityOrder := map[string]int{"high": 0, "medium": 1, "low": 2}
	minSev := 2 // default: include all
	if v, ok := severityOrder[filterSeverity]; ok {
		minSev = v
	}

	// ── nest uncovered entities under their file (#4636) ─────────────────────
	// Build the per-file map from the FULL uncovered set (before the global
	// severity filter and the flat-list cap) so each file node in the tree
	// carries all of its uncovered leaves; the frontend severity filter then
	// narrows which leaves render. Keyed by the forward-slash source path to
	// match ByFile.File.
	byFileUncovered := make(map[string]FileUncovered)
	{
		grouped := make(map[string][]graph.UncoveredEntity)
		for _, u := range result.UncoveredEntities {
			key := filepath.ToSlash(u.SourceFile)
			if key == "" {
				continue
			}
			grouped[key] = append(grouped[key], u)
		}
		for key, ents := range grouped {
			sort.SliceStable(ents, func(i, j int) bool {
				si := severityOrder[ents[i].Severity]
				sj := severityOrder[ents[j].Severity]
				if si != sj {
					return si < sj
				}
				if ents[i].StartLine != ents[j].StartLine {
					return ents[i].StartLine < ents[j].StartLine
				}
				return ents[i].Name < ents[j].Name
			})
			fu := FileUncovered{}
			if len(ents) > PerFileUncoveredCap {
				fu.More = len(ents) - PerFileUncoveredCap
				ents = ents[:PerFileUncoveredCap]
			}
			fu.Entities = ents
			byFileUncovered[key] = fu
		}
	}
	result.ByFileUncovered = byFileUncovered

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
			Dir:          d,
			Total:        acc.total,
			Covered:      acc.covered,
			ContractOnly: acc.contractOnly,
			CoveragePct:  covPct,
		})
	}
	sort.Slice(result.ByDirectory, func(i, j int) bool {
		return result.ByDirectory[i].Dir < result.ByDirectory[j].Dir
	})

	// ── build ByFile with the same optional dir prefix filter ─────────────────
	for f, acc := range fileAcc {
		if filterDir != "" && !strings.HasPrefix(acc.dir, filterDir) {
			continue
		}
		covPct := 0.0
		if acc.total > 0 {
			covPct = 100.0 * float64(acc.covered) / float64(acc.total)
		}
		result.ByFile = append(result.ByFile, graph.FileCoverage{
			File:         f,
			Dir:          acc.dir,
			Total:        acc.total,
			Covered:      acc.covered,
			ContractOnly: acc.contractOnly,
			CoveragePct:  covPct,
		})
	}
	sort.Slice(result.ByFile, func(i, j int) bool {
		return result.ByFile[i].File < result.ByFile[j].File
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

	writeReportJSON(w, result)
}
