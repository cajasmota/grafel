package dashboard

// handlers_fitness.go — Architectural fitness functions HTTP surface (#1345).
//
// Route:
//
//	GET /api/fitness/{group}
//	    Evaluates .archigraph/fitness.yaml rules for every repo in the group
//	    and returns per-repo results plus aggregate violation counts.
//
// Query parameters:
//
//	repo   — (optional) restrict evaluation to a single slug within the group
//
// Response shape: FitnessGroupReport (JSON)

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/quality/fitness"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// FitnessRepoResult is the per-repo evaluation result returned by the API.
type FitnessRepoResult struct {
	Slug        string                  `json:"slug"`
	Path        string                  `json:"path"`
	HasConfig   bool                    `json:"has_config"`
	TotalRules  int                     `json:"total_rules"`
	PassedRules int                     `json:"passed_rules"`
	FailedRules int                     `json:"failed_rules"`
	ErrorCount  int                     `json:"error_count"`
	WarnCount   int                     `json:"warn_count"`
	InfoCount   int                     `json:"info_count"`
	Results     []fitness.RuleResult    `json:"results"`
	Suggested   []fitness.SuggestedRule `json:"suggested_rules,omitempty"`
}

// FitnessGroupReport is the wire shape for GET /api/fitness/{group}.
type FitnessGroupReport struct {
	Group       string              `json:"group"`
	TotalErrors int                 `json:"total_errors"`
	TotalWarns  int                 `json:"total_warns"`
	TotalInfos  int                 `json:"total_infos"`
	Repos       []FitnessRepoResult `json:"repos"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleFitness(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	if groupName == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	repoPaths, err := repoPathsForGroup(groupName)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q: %v", groupName, err))
		return
	}
	if len(repoPaths) == 0 {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q has no repos", groupName))
		return
	}

	// Optional single-repo filter.
	filterSlug := r.URL.Query().Get("repo")

	report := FitnessGroupReport{Group: groupName}

	// S8 (#2159): use the cached group to avoid per-request LoadGraphFromDir.
	cachedGrpFit, _ := s.graphs.GetGroupCached(groupName)

	for _, rp := range repoPaths {
		if filterSlug != "" && rp.Slug != filterSlug {
			continue
		}

		stateDir := filepath.Join(rp.Path, ".archigraph")

		// Load fitness config (empty config if file absent).
		fitCfg, cfgErr := fitness.LoadConfig(stateDir)
		if cfgErr != nil {
			// Config parse error: surface as a single error finding.
			report.Repos = append(report.Repos, FitnessRepoResult{
				Slug:      rp.Slug,
				Path:      rp.Path,
				HasConfig: true,
				Results: []fitness.RuleResult{{
					Passed: false,
					Violations: []fitness.Violation{{
						RuleName: "__config_parse_error__",
						Severity: "error",
						Kind:     "parse",
						Message:  cfgErr.Error(),
					}},
				}},
				ErrorCount:  1,
				FailedRules: 1,
			})
			report.TotalErrors++
			continue
		}

		hasConfig := len(fitCfg.Rules) > 0

		// Load the graph document — prefer the cached DashRepo.Doc (S8, #2159).
		var doc *graph.Document
		if cachedGrpFit != nil {
			if dr, ok := cachedGrpFit.Repos[rp.Slug]; ok && dr != nil {
				doc = dr.Doc
			}
		}
		if doc == nil {
			var docErr error
			doc, docErr = graph.LoadGraphFromDir(stateDir)
			if docErr != nil {
				// Graph not indexed yet — skip silently (consistent with other handlers).
				continue
			}
		}

		evalResult := fitness.Evaluate(fitCfg, doc)

		repoResult := FitnessRepoResult{
			Slug:        rp.Slug,
			Path:        rp.Path,
			HasConfig:   hasConfig,
			TotalRules:  evalResult.TotalRules,
			PassedRules: evalResult.PassedRules,
			FailedRules: evalResult.FailedRules,
			ErrorCount:  evalResult.ErrorCount,
			WarnCount:   evalResult.WarnCount,
			InfoCount:   evalResult.InfoCount,
			Results:     evalResult.Results,
			Suggested:   evalResult.SuggestedRules,
		}

		report.Repos = append(report.Repos, repoResult)
		report.TotalErrors += evalResult.ErrorCount
		report.TotalWarns += evalResult.WarnCount
		report.TotalInfos += evalResult.InfoCount
	}

	writeJSON(w, http.StatusOK, report)
}
