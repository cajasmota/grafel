package mcp

// archigraph_apply_docgen_repairs — MCP tool for the docgen→graph repair
// feedback loop (#1659).
//
// The tool reads docgen-repairs.jsonl from each repo in the resolved group,
// partitions by confidence (≥0.8 → apply immediately, <0.8 → pending queue),
// and returns a per-repo summary with fidelity before/after.
//
// This is the apply-path companion to the skill-side emission described in
// skills/generate-docs/SKILL.md § "Docgen Repair Feedback Contract".

import (
	"context"
	"fmt"
	"sort"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/enrichment"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// handleApplyDocgenRepairs implements archigraph_apply_docgen_repairs.
//
// Parameters (all optional):
//
//	repo_filter []string — restrict to named repos; empty/["*"] = all
//	dry_run bool — when true, report what would be applied but do not write
func (s *Server) handleApplyDocgenRepairs(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}

	dryRun := argBool(req, "dry_run", false)
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	type repoResult struct {
		Repo           string                   `json:"repo"`
		Applied        int                      `json:"applied"`
		Queued         int                      `json:"queued"`
		Skipped        int                      `json:"skipped"`
		FidelityBefore *float64                 `json:"fidelity_before,omitempty"`
		FidelityAfter  *float64                 `json:"fidelity_after,omitempty"`
		FidelityDelta  *float64                 `json:"fidelity_delta,omitempty"`
		RepairsApplied []enrichment.DocgenRepairCandidate `json:"repairs_applied,omitempty"`
		RepairsQueued  []enrichment.DocgenRepairCandidate `json:"repairs_queued,omitempty"`
		Error          string                   `json:"error,omitempty"`
	}

	results := make([]repoResult, 0, len(repos))
	totalApplied, totalQueued := 0, 0

	// Sort repos for deterministic output.
	sort.Slice(repos, func(i, j int) bool { return repos[i].Repo < repos[j].Repo })

	for _, r := range repos {
		res := repoResult{Repo: r.Repo}

		stateDir := daemon.StateDirForRepo(r.Path)

		// Count total / bug edges from the loaded document for fidelity calc.
		totalEdges, bugEdges := countEdgesForFidelity(r)

		if dryRun {
			// Preview mode: read candidates but do not write.
			cands, skipped, err := enrichment.ReadDocgenRepairs(stateDir)
			if err != nil {
				res.Error = fmt.Sprintf("read docgen-repairs.jsonl: %v", err)
				results = append(results, res)
				continue
			}
			res.Skipped = skipped
			for _, c := range cands {
				if c.Confidence >= enrichment.HighConfidenceThreshold {
					res.Applied++
					res.RepairsApplied = append(res.RepairsApplied, c)
				} else {
					res.Queued++
					res.RepairsQueued = append(res.RepairsQueued, c)
				}
			}
			if totalEdges > 0 {
				before := 1.0 - float64(bugEdges)/float64(totalEdges)
				res.FidelityBefore = &before
				lift := countEdgeRepairsFromCandidates(res.RepairsApplied)
				adj := bugEdges - lift
				if adj < 0 {
					adj = 0
				}
				after := 1.0 - float64(adj)/float64(totalEdges)
				res.FidelityAfter = &after
				delta := after - before
				res.FidelityDelta = &delta
			}
		} else {
			// Live apply.
			stats, err := enrichment.ApplyDocgenRepairsToResolutions(stateDir, totalEdges, bugEdges)
			if err != nil {
				res.Error = fmt.Sprintf("apply: %v", err)
				results = append(results, res)
				continue
			}
			res.Applied = stats.Applied
			res.Queued = stats.Queued
			res.Skipped = stats.Skipped
			res.FidelityBefore = stats.FidelityBefore
			res.FidelityAfter = stats.FidelityAfter
			res.RepairsApplied = stats.RepairsApplied
			res.RepairsQueued = stats.RepairsQueued
			if stats.FidelityBefore != nil && stats.FidelityAfter != nil {
				delta := *stats.FidelityAfter - *stats.FidelityBefore
				res.FidelityDelta = &delta
			}
		}

		totalApplied += res.Applied
		totalQueued += res.Queued
		results = append(results, res)
	}

	return jsonResult(map[string]any{
		"dry_run":       dryRun,
		"repos":         results,
		"total_applied": totalApplied,
		"total_queued":  totalQueued,
		"threshold":     enrichment.HighConfidenceThreshold,
	}), nil
}

// countEdgesForFidelity returns (totalEdges, bugEdges) from the loaded repo
// document for use in fidelity calculations. Bug edges are those whose ToID
// contains the "bug-" or "BugExtractor" disposition markers written by the
// resolver (i.e. unresolved CALLS/IMPORTS that could not be bound).
func countEdgesForFidelity(r *LoadedRepo) (total, bugs int) {
	if r == nil || r.Doc == nil {
		return 0, 0
	}
	for i := range r.Doc.Relationships {
		rel := &r.Doc.Relationships[i]
		// Count all CALLS / IMPORTS as the "import edge" universe (same scope
		// as the dashboard fidelity calculation).
		if rel.Kind != "CALLS" && rel.Kind != "IMPORTS" && rel.Kind != "REFERENCES" {
			continue
		}
		total++
		// A relationship is "buggy" (unresolved) when its ToID still looks like
		// a raw stub — it was not bound to a hex entity ID or ext:-qualified
		// external. The resolver writes these as bug:* prefixes on failed
		// resolution, or the ToID remains a bare name/qname string.
		if isBugEdgeToID(rel.ToID) {
			bugs++
		}
	}
	return total, bugs
}

// isBugEdgeToID returns true for ToID values that represent unresolved stubs.
// Mirrors the heuristic used by internal/resolve (DispositionBugExtractor):
// a hex ID (16 chars) or an ext:-prefixed ID are both resolved; everything
// else is unresolved.
func isBugEdgeToID(toID string) bool {
	if toID == "" {
		return false
	}
	// Resolved: hex entity ID (16 lowercase hex chars).
	if len(toID) == 16 && isHexString(toID) {
		return false
	}
	// Resolved: ext:-qualified external.
	if len(toID) > 4 && toID[:4] == "ext:" {
		return false
	}
	// Everything else is a raw stub → unresolved → bug.
	return true
}

// isHexString returns true if every byte in s is a lowercase hex digit.
func isHexString(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// countEdgeRepairsFromCandidates returns the number of candidates that
// directly reduce the unresolved-edge count (resolve_ref, add_edge,
// label_external).
func countEdgeRepairsFromCandidates(repairs []enrichment.DocgenRepairCandidate) int {
	n := 0
	for _, r := range repairs {
		switch r.Type {
		case enrichment.DocgenRepairResolveRef,
			enrichment.DocgenRepairAddEdge,
			enrichment.DocgenRepairLabelExternal:
			n++
		}
	}
	return n
}
