package dashboard

// handlers_nplus1.go — N+1 query anti-pattern detection HTTP surface.
//
// Route registered in server.go:
//
//	GET  /api/quality/anti-patterns/{group}  — list N+1 findings for a group
//
// The handler loads each repo's graph document within the group (using the
// shared repoPathsForGroup helper and LoadGraphFromDir), runs
// graph.DetectNPlusOne against each document, and aggregates the results
// into a single GroupNPlusOneReport.
//
// Query params:
//
//	orm=django|sqlalchemy|activerecord|…  — filter by ORM framework
//	file=<path>                            — filter by source file substring
//
// Wire format is JSON (application/json).

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// GroupNPlusOneReport is the wire shape for
// GET /api/quality/anti-patterns/{group}.
type GroupNPlusOneReport struct {
	Group           string                  `json:"group"`
	TotalFindings   int                     `json:"total_findings"`
	EntitiesScanned int                     `json:"entities_scanned"`
	RelsScanned     int                     `json:"rels_scanned"`
	ByORM           map[string]int          `json:"by_orm"`
	ByLanguage      map[string]int          `json:"by_language"`
	Findings        []graph.NPlusOneFinding `json:"findings"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler
// ─────────────────────────────────────────────────────────────────────────────

// handleNPlusOne serves GET /api/quality/anti-patterns/{group}.
//
// For each repo in the group it loads the indexed graph document and runs the
// N+1 detector. Results are merged and returned as a GroupNPlusOneReport.
func (s *Server) handleNPlusOne(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	if groupName == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	// Resolve the group's repo paths from the registry (shared helper
	// defined in handlers_quality.go).
	repoPaths, err := repoPathsForGroup(groupName)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q: %v", groupName, err))
		return
	}
	if len(repoPaths) == 0 {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q has no repos", groupName))
		return
	}

	// ── query-string filters ──────────────────────────────────────────────────
	q := r.URL.Query()
	filterORM := strings.ToLower(q.Get("orm"))
	filterFile := q.Get("file")

	// ── aggregate across repos ────────────────────────────────────────────────
	result := GroupNPlusOneReport{
		Group:      groupName,
		ByORM:      make(map[string]int),
		ByLanguage: make(map[string]int),
	}

	// S8 (#2159): use the cached group to avoid per-request LoadGraphFromDir.
	cachedGrp, _ := s.graphs.GetGroupCached(groupName)

	for _, rp := range repoPaths {
		var doc *graph.Document
		if cachedGrp != nil {
			if dr, ok := cachedGrp.Repos[rp.Slug]; ok && dr != nil {
				doc = dr.Doc
			}
		}
		if doc == nil {
			// Resolve the per-repo state dir (external store; #1626).
			stateDir := daemon.StateDirForRepo(rp.Path)
			var loadErr error
			doc, loadErr = graph.LoadGraphFromDir(stateDir)
			if loadErr != nil {
				// Repo not yet indexed — skip silently.
				continue
			}
		}

		report := graph.DetectNPlusOne(doc)
		result.EntitiesScanned += report.EntitiesScanned
		result.RelsScanned += report.RelationshipsScanned

		for _, f := range report.Findings {
			// Apply optional filters.
			if filterORM != "" && strings.ToLower(f.ORM) != filterORM {
				continue
			}
			if filterFile != "" && !strings.Contains(f.QueryFile, filterFile) {
				continue
			}
			result.Findings = append(result.Findings, f)
			if f.ORM != "" {
				result.ByORM[f.ORM]++
			}
			if f.Language != "" {
				result.ByLanguage[f.Language]++
			}
		}
	}

	// Sort findings by file+line for deterministic output.
	sort.SliceStable(result.Findings, func(i, j int) bool {
		fi, fj := result.Findings[i], result.Findings[j]
		if fi.QueryFile != fj.QueryFile {
			return fi.QueryFile < fj.QueryFile
		}
		return fi.QueryLine < fj.QueryLine
	})

	result.TotalFindings = len(result.Findings)

	writeReportJSON(w, result)
}
