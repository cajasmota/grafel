// pr_impact_tools.go — MCP tool: archigraph_pr_impact (issue #4292).
//
// Diff/PR-scoped impact analysis with cross-change merge-risk detection. Two
// modes, selected by the arguments supplied:
//
//	single mode    {base, head}  -> changed entities -> impacted communities ->
//	                                downstream blast radius
//	conflicts mode {refs:[...]}   -> each ref's impacted-community set, intersected
//	                                pairwise -> ranked merge-order/conflict triage
//
// The core logic is pure and MCP-free (graph.AnalyzePRImpact / AnalyzeMergeRisk,
// see internal/graph/pr_impact.go). This handler is the thin shell that loads
// the per-ref graphs (the same StateDirForRepoRef path diff_refs uses) and feeds
// the diff-derived change set in. The change set itself is computed by
// graph.DiffDocs — the exact engine behind archigraph_diff_refs — so the git
// diff logic is reused, not duplicated.
package mcp

import (
	"context"
	"fmt"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// handlePRImpact implements archigraph_pr_impact.
//
// Arguments:
//   - group (string, optional) — inferred from cwd / registry when omitted
//   - repo  (string, required)
//   - base, head (string) — single mode: diff base..head, then impact-analyse head
//   - refs  (array of string) — conflicts mode: ≥2 refs, each diffed against `base`
//     (or against the first ref when base omitted) to derive its change set, then
//     pairwise community-overlap triage
//   - hops  (number, optional) — downstream blast-radius depth (default 3, [1,6])
func (s *Server) handlePRImpact(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	groupName := argString(req, "group", "")
	if groupName == "" {
		cwd := s.inferCWD(req)
		groupName, _ = groupFromRegistryWithCandidates(s.State, cwd)
	}
	if groupName == "" {
		return mcpapi.NewToolResultError("group is required; pass group= or run from inside a registered repo"), nil
	}
	repoSlug := argString(req, "repo", "")
	if repoSlug == "" {
		return mcpapi.NewToolResultError("repo is required"), nil
	}

	opts := graph.DefaultPRImpactOptions()
	opts.Hops = argInt(req, "hops", opts.Hops)

	refs := argStringSlice(req, "refs")
	base := argString(req, "base", "")
	head := argString(req, "head", "")

	// Validate mode arguments *before* touching the registry/disk so the error is
	// deterministic and independent of whether the repo is indexed.
	conflictsMode := len(refs) > 0
	if conflictsMode {
		if len(refs) < 2 {
			return mcpapi.NewToolResultError("conflicts mode requires at least 2 refs"), nil
		}
	} else if base == "" || head == "" {
		return mcpapi.NewToolResultError(
			"single mode requires both base= and head=; conflicts mode requires refs=[...]"), nil
	}

	repoPath, err := diffToolRepoPath(groupName, repoSlug)
	if err != nil {
		return mcpapi.NewToolResultError(fmt.Sprintf("repo lookup failed: %v", err)), nil
	}

	// ── Conflicts mode: refs supplied ────────────────────────────────────────
	if conflictsMode {
		return s.prImpactConflicts(groupName, repoSlug, repoPath, base, refs, opts)
	}

	// ── Single mode: base/head ───────────────────────────────────────────────
	res, errRes := s.prImpactSingle(repoPath, base, head, opts)
	if errRes != nil {
		return errRes, nil
	}
	return jsonResult(map[string]any{
		"mode":                     "single",
		"group":                    groupName,
		"repo":                     repoSlug,
		"base":                     base,
		"head":                     head,
		"changed_entities":         res.ChangedEntities,
		"impacted_communities":     res.ImpactedCommunities,
		"blast_radius":             res.BlastRadius,
		"changed_count":            res.ChangedCount,
		"impacted_community_count": res.CommunityCount,
		"blast_radius_count":       res.BlastRadiusCount,
		"truncated":                res.Truncated,
	}), nil
}

// prImpactSingle loads base+head graphs, diffs them (reusing graph.DiffDocs),
// and runs the impact analysis on the head graph.
func (s *Server) prImpactSingle(repoPath, base, head string, opts graph.PRImpactOptions) (graph.PRImpactResult, *mcpapi.CallToolResult) {
	headDoc, err := loadRefGraph(repoPath, head)
	if err != nil {
		return graph.PRImpactResult{}, mcpapi.NewToolResultError(err.Error())
	}
	change, errRes := diffChangeSet(repoPath, base, head)
	if errRes != nil {
		return graph.PRImpactResult{}, errRes
	}
	return graph.AnalyzePRImpact(headDoc.Entities, headDoc.Relationships, change, opts), nil
}

// prImpactConflicts diffs each ref against the base (defaulting to the first
// ref) to derive its change set, runs impact analysis to get its impacted
// communities, then triages pairwise overlaps via graph.AnalyzeMergeRisk.
func (s *Server) prImpactConflicts(groupName, repoSlug, repoPath, base string, refs []string, opts graph.PRImpactOptions) (*mcpapi.CallToolResult, error) {
	if len(refs) < 2 {
		return mcpapi.NewToolResultError("conflicts mode requires at least 2 refs"), nil
	}
	if base == "" {
		base = refs[0]
	}

	impacts := make([]graph.ChangeImpact, 0, len(refs))
	perRef := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		headDoc, err := loadRefGraph(repoPath, ref)
		if err != nil {
			return mcpapi.NewToolResultError(err.Error()), nil
		}
		change, errRes := diffChangeSet(repoPath, base, ref)
		if errRes != nil {
			return errRes, nil
		}
		res := graph.AnalyzePRImpact(headDoc.Entities, headDoc.Relationships, change, opts)
		comms := res.ImpactedCommunityIDs()
		impacts = append(impacts, graph.ChangeImpact{Ref: ref, Communities: comms})
		perRef = append(perRef, map[string]any{
			"ref":                  ref,
			"changed_count":        res.ChangedCount,
			"impacted_communities": comms,
			"blast_radius_count":   res.BlastRadiusCount,
		})
	}

	risk := graph.AnalyzeMergeRisk(impacts)
	return jsonResult(map[string]any{
		"mode":             "conflicts",
		"group":            groupName,
		"repo":             repoSlug,
		"base":             base,
		"per_ref":          perRef,
		"risk_pairs":       risk.Pairs,
		"ref_count":        risk.RefCount,
		"risky_pair_count": risk.RiskyPairs,
	}), nil
}

// loadRefGraph loads an indexed graph for a single ref from disk, using the same
// StateDirForRepoRef path diff_refs and other ref-aware tools use.
func loadRefGraph(repoPath, ref string) (*graph.Document, error) {
	dir := daemon.StateDirForRepoRef(repoPath, ref)
	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		return nil, fmt.Errorf("could not load graph for ref %q: %v (run `archigraph index` on that branch first)", ref, err)
	}
	return doc, nil
}

// diffChangeSet computes the diff-derived change set between base and head by
// loading both ref graphs and running graph.DiffDocs (the diff_refs engine).
// Same-ref is the empty change set.
func diffChangeSet(repoPath, base, head string) (graph.ChangeSet, *mcpapi.CallToolResult) {
	if base == head {
		return graph.ChangeSet{}, nil
	}
	baseDoc, err := loadRefGraph(repoPath, base)
	if err != nil {
		return graph.ChangeSet{}, mcpapi.NewToolResultError(err.Error())
	}
	headDoc, err := loadRefGraph(repoPath, head)
	if err != nil {
		return graph.ChangeSet{}, mcpapi.NewToolResultError(err.Error())
	}
	d := graph.DiffDocs(baseDoc, headDoc)
	return graph.ChangeSet{
		Added:    d.Entities.Added,
		Removed:  d.Entities.Removed,
		Modified: d.Entities.Modified,
	}, nil
}
