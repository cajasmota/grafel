// pr_impact_tools.go — MCP tool: grafel_pr_impact (issue #4292).
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
// graph.DiffDocs — the exact engine behind grafel_diff_refs — so the git
// diff logic is reused, not duplicated.
package mcp

import (
	"context"
	"fmt"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// noCommunityDataHint is appended to every "community data unavailable" message
// so the answer is actionable rather than merely negative.
const noCommunityDataHint = "the group-algo overlay (<group>-algo.json) is missing or stale — " +
	"run/await a group index so community detection produces it, then retry"

// handlePRImpact implements grafel_pr_impact.
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

	// Community data is NOT in graph.fb (see newGroupCommunityStamper): resolve
	// the group overlay once and stamp it onto each ref graph we analyse.
	stamper := newGroupCommunityStamper(groupName)

	// ── Conflicts mode: refs supplied ────────────────────────────────────────
	if conflictsMode {
		return s.prImpactConflicts(groupName, repoSlug, repoPath, base, refs, opts, stamper)
	}

	// ── Single mode: base/head ───────────────────────────────────────────────
	res, errRes := s.prImpactSingle(repoPath, base, head, opts, stamper)
	if errRes != nil {
		return errRes, nil
	}
	out := map[string]any{
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
		// #6006: the blast radius is valid regardless, but impacted_communities
		// is only meaningful when this is true. Without the flag an empty
		// impacted_communities is indistinguishable from "nothing was computed".
		"community_data_available": res.CommunityDataAvailable,
	}
	if !res.CommunityDataAvailable {
		out["community_data_unavailable_reason"] = noCommunityDataHint
	}
	return jsonResult(out), nil
}

// groupCommunityStamper supplies the community assignments grafel_pr_impact
// needs, from the ONE place they actually live (#6006).
//
// Why the overlay and not graph.fb: the per-repo Pass-4 algorithm pass was
// removed when the group-scope pass replaced it (cmd/grafel/index.go — the
// graph.fb per-entity algo fields are retained but "left at their schema
// sentinels (-2 / 0 / false) rather than recomputed per-repo"). A graph loaded
// straight off disk therefore carries NO community ids at all, permanently, on
// every invocation. Restamping graph.fb instead would mean reviving a per-repo
// Louvain whose partition is not the group partition every other grafel surface
// reports — two disagreeing community numberings, and merge risk computed
// against the wrong one.
//
// Absence-tolerance mirrors applyGroupAlgoOverlay: an absent, corrupt, stale or
// version-mismatched overlay yields the zero stamper, which stamps nothing. The
// difference is what the CALLER then does — here that state is reported, not
// silently rendered as "no conflicts".
type groupCommunityStamper struct {
	ov *groupalgo.Overlay // nil ⇒ no community data available
}

func newGroupCommunityStamper(group string) groupCommunityStamper {
	path, err := groupalgo.OverlayPath(group)
	if err != nil || path == "" {
		return groupCommunityStamper{}
	}
	cur, err := groupalgo.CurrentSourceMtimes(group)
	if err != nil {
		return groupCommunityStamper{}
	}
	ov, ok := groupalgo.ReadOverlay(path, cur)
	if !ok {
		return groupCommunityStamper{}
	}
	return groupCommunityStamper{ov: ov}
}

// stamp copies the overlay's CommunityID onto doc's entities in place, by id.
// Entities absent from the overlay (e.g. an entity that exists only on a feature
// ref and so was never part of the indexed group union) are left ungrouped.
func (s groupCommunityStamper) stamp(doc *graph.Document) {
	if s.ov == nil || doc == nil {
		return
	}
	for i := range doc.Entities {
		eo, ok := s.ov.Results[doc.Entities[i].ID]
		if !ok {
			continue
		}
		cid := eo.CommunityID
		doc.Entities[i].CommunityID = &cid
	}
}

// prImpactSingle loads base+head graphs, diffs them (reusing graph.DiffDocs),
// and runs the impact analysis on the head graph.
func (s *Server) prImpactSingle(repoPath, base, head string, opts graph.PRImpactOptions, stamper groupCommunityStamper) (graph.PRImpactResult, *mcpapi.CallToolResult) {
	headDoc, err := loadRefGraph(repoPath, head)
	if err != nil {
		return graph.PRImpactResult{}, mcpapi.NewToolResultError(err.Error())
	}
	stamper.stamp(headDoc)
	change, errRes := diffChangeSet(repoPath, base, head)
	if errRes != nil {
		return graph.PRImpactResult{}, errRes
	}
	return graph.AnalyzePRImpact(headDoc.Entities, headDoc.Relationships, change, opts), nil
}

// prImpactConflicts diffs each ref against the base (defaulting to the first
// ref) to derive its change set, runs impact analysis to get its impacted
// communities, then triages pairwise overlaps via graph.AnalyzeMergeRisk.
func (s *Server) prImpactConflicts(groupName, repoSlug, repoPath, base string, refs []string, opts graph.PRImpactOptions, stamper groupCommunityStamper) (*mcpapi.CallToolResult, error) {
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
		stamper.stamp(headDoc)
		change, errRes := diffChangeSet(repoPath, base, ref)
		if errRes != nil {
			return errRes, nil
		}
		res := graph.AnalyzePRImpact(headDoc.Entities, headDoc.Relationships, change, opts)
		comms := res.ImpactedCommunityIDs()
		impacts = append(impacts, graph.ChangeImpact{
			Ref:                    ref,
			Communities:            comms,
			CommunityDataAvailable: res.CommunityDataAvailable,
		})
		perRef = append(perRef, map[string]any{
			"ref":                  ref,
			"changed_count":        res.ChangedCount,
			"impacted_communities": comms,
			"blast_radius_count":   res.BlastRadiusCount,
		})
	}

	risk := graph.AnalyzeMergeRisk(impacts)
	// #6006 — merge risk is a SAFETY question, and an empty risky-pair list is
	// read as "safe to merge". When the community data the intersection is
	// computed from is missing, there is no analysis to report: returning the
	// zero-risk payload would be a false negative in the one direction that
	// causes harm. Fail loudly instead — an error result is the only shape an MCP
	// client cannot mistake for an answer.
	if !risk.CommunityDataAvailable {
		return mcpapi.NewToolResultError(fmt.Sprintf(
			"merge-risk analysis did NOT run: no community assignments are available for ref(s) %v, "+
				"so grafel cannot tell whether these refs conflict. This is NOT a \"no conflicts\" result. "+
				"Cause: %s.",
			risk.RefsWithoutCommunityData, noCommunityDataHint)), nil
	}
	return jsonResult(map[string]any{
		"mode":                     "conflicts",
		"group":                    groupName,
		"repo":                     repoSlug,
		"base":                     base,
		"per_ref":                  perRef,
		"risk_pairs":               risk.Pairs,
		"ref_count":                risk.RefCount,
		"risky_pair_count":         risk.RiskyPairs,
		"community_data_available": risk.CommunityDataAvailable,
	}), nil
}

// loadRefGraph loads an indexed graph for a single ref from disk, using the same
// StateDirForRepoRef path diff_refs and other ref-aware tools use.
func loadRefGraph(repoPath, ref string) (*graph.Document, error) {
	dir := daemon.StateDirForRepoRef(repoPath, ref)
	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		return nil, fmt.Errorf("could not load graph for ref %q: %v (run `grafel index` on that branch first)", ref, err)
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
