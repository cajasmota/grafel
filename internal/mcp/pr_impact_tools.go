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
	"os"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

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
		"community_data_available":           res.CommunityDataAvailable,
		"changed_entities_without_community": res.ChangedWithoutCommunity,
		// #6042: each changed_entities row carries community_source
		// (overlay|inferred|none); these are the aggregates. An inferred placement
		// is a deduction from placed neighbours, not a reading of the group
		// partition, and a caller must be able to tell the two apart.
		"changed_entities_with_overlay_community":  res.ChangedWithOverlayCommunity,
		"changed_entities_with_inferred_community": res.ChangedWithInferredCommunity,
		"community_data_inferred_only":             res.CommunityDataInferredOnly,
	}
	if res.CommunityDataInferredOnly {
		out["community_data_note"] = inferredOnlyNote
	}
	stamper.describeInto(out)
	if !res.CommunityDataAvailable {
		out["community_data_unavailable_reason"] = stamper.unavailableCause(groupName)
	}
	return jsonResult(out), nil
}

// describeInto adds the overlay-provenance fields every pr_impact payload
// carries, so a caller can tell a current partition from a 50-minute-old one
// without the tool having to refuse the older one.
func (s groupCommunityStamper) describeInto(out map[string]any) {
	out["community_data_stale"] = s.ov != nil && s.stale
	if s.ov != nil && !s.ov.ComputedAt.IsZero() {
		out["overlay_computed_at"] = s.ov.ComputedAt.UTC().Format(time.RFC3339)
	}
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
// An absent, corrupt or version-mismatched overlay yields the zero stamper,
// which stamps nothing — and the CALLER reports that state instead of silently
// rendering it as "no conflicts".
//
// STALENESS IS TOLERATED, NOT REFUSED (see groupalgo.ReadOverlayAnyAge). The
// staleness key is the graph.fb of whichever ref is currently CHECKED OUT, so
// refusing on stale meant that standing on a feature branch — the only posture
// from which anyone asks a merge-risk question — turned conflicts mode into a
// hard error, as did any reindex of any repo in the group for the ~50 minutes
// until group-algo caught up. Merge risk is a community-level verdict; a
// 50-minute-old partition is overwhelmingly the same partition. So a stale
// overlay is applied and the staleness is REPORTED (community_data_stale +
// overlay_computed_at) rather than withheld.
type groupCommunityStamper struct {
	ov    *groupalgo.Overlay // nil ⇒ no community data at all
	stale bool               // overlay applied, but older than the current graphs
}

func newGroupCommunityStamper(group string) groupCommunityStamper {
	path, err := groupalgo.OverlayPath(group)
	if err != nil || path == "" {
		return groupCommunityStamper{}
	}
	ov, ok := cachedOverlayAnyAge(path)
	if !ok {
		return groupCommunityStamper{}
	}
	// Staleness is computed per call (cheap: a registry read + one stat per repo)
	// and deliberately NOT part of the cache key — the parsed overlay is the
	// expensive part and does not change when a graph.fb mtime moves.
	stale := true
	if cur, mtErr := groupalgo.CurrentSourceMtimes(group); mtErr == nil {
		stale = groupalgo.IsOverlayStale(ov, cur)
	}
	return groupCommunityStamper{ov: ov, stale: stale}
}

// unavailableCause explains WHY no community data reached the changed entities.
// The two causes need different remedies, and telling a user to "run a group
// index" when they have just done so is worse than saying nothing.
func (s groupCommunityStamper) unavailableCause(group string) string {
	if s.ov == nil {
		return fmt.Sprintf("no usable group-algo overlay exists for group %q "+
			"(%s-algo.json is absent, corrupt, or was produced by a different algorithm "+
			"version) — run or await a group index so community detection produces it", group, group)
	}
	return "the group-algo overlay exists but does not cover the changed entities, and no " +
		"community could be INFERRED for them either (#6042: grafel tries the containing file, " +
		"the module, and the placed entities they call). The overlay is computed from the " +
		"INDEXED group union, so entities that exist only on a feature ref are absent from it by " +
		"construction — this is the expected shape for a change that only ADDS entities. " +
		"Inference then failed because those entities have no overlay-placed neighbours at all, " +
		"or because their signals disagreed. Reindex the group with those refs' code present, " +
		"or fall back to single mode and triage by blast radius"
}

// inferredOnlyNote is attached to any payload whose every placement was
// inferred (#6042). The structured flags are the contract; this is the sentence
// that stops an agent reading the verdict as a measurement.
const inferredOnlyNote = "every community placement behind this verdict was INFERRED from " +
	"placed neighbours (containing file / module / outbound call targets), not read from the " +
	"group-algo partition — the changed entities are new, so the partition has never seen them. " +
	"Treat this as a well-founded estimate of what community detection would say, not as a " +
	"measurement. Per-entity provenance is in changed_entities[].community_source."

// ── overlay read cache (one entry) ───────────────────────────────────────────
//
// The overlay is read and unmarshalled on every grafel_pr_impact call, in both
// modes. On the live corpus that is a 31.9 MB JSON file: ~326 ms and ~76 MB of
// transient heap PER CALL. The latency is acceptable (RSS over latency is the
// standing trade for MCP agents) but the transient heap is not — concurrent MCP
// agents on a 16 GB box are precisely the workload this costs the most.
//
// One entry is the right size: a session works one group at a time, and
// concurrent agents share it. Keyed on path + mtime + size so an atomic overlay
// swap (os.Rename, see overlay.go) invalidates it. The cached *Overlay is
// treated as IMMUTABLE — stamp() only reads it and writes to the caller's
// Document — so sharing it across concurrent calls needs no copy.
var (
	overlayCacheMu   sync.Mutex
	overlayCacheKey  string
	overlayCacheVal  *groupalgo.Overlay
	overlayCacheHits int // test-visible; not reported anywhere
)

func cachedOverlayAnyAge(path string) (*groupalgo.Overlay, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	key := fmt.Sprintf("%s\x00%d\x00%d", path, fi.ModTime().UnixNano(), fi.Size())

	overlayCacheMu.Lock()
	defer overlayCacheMu.Unlock()
	if overlayCacheKey == key && overlayCacheVal != nil {
		overlayCacheHits++
		return overlayCacheVal, true
	}
	ov, ok := groupalgo.ReadOverlayAnyAge(path)
	if !ok {
		// Do not cache misses: an absent/corrupt overlay is usually transient
		// (mid-write, or not computed yet) and re-reading it is cheap.
		overlayCacheKey, overlayCacheVal = "", nil
		return nil, false
	}
	overlayCacheKey, overlayCacheVal = key, ov
	return ov, true
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
			OverlayEntityCount:     res.ChangedWithOverlayCommunity,
			InferredEntityCount:    res.ChangedWithInferredCommunity,
		})
		perRef = append(perRef, map[string]any{
			"ref":                  ref,
			"changed_count":        res.ChangedCount,
			"impacted_communities": comms,
			"blast_radius_count":   res.BlastRadiusCount,
			// #6006 D1: partial coverage is visible per ref, so a caller can see
			// that (say) 3 of 4 changed entities were placed even when the overall
			// verdict stands.
			"changed_entities_without_community": res.ChangedWithoutCommunity,
			// #6042: and how much of this ref's placement was measured vs deduced.
			"changed_entities_with_overlay_community":  res.ChangedWithOverlayCommunity,
			"changed_entities_with_inferred_community": res.ChangedWithInferredCommunity,
			"community_data_inferred_only":             res.CommunityDataInferredOnly,
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
			"merge-risk analysis did NOT run: none of the changed entities on ref(s) %v could be "+
				"placed in a community, so grafel cannot tell whether these refs conflict. "+
				"This is NOT a \"no conflicts\" result. Cause: %s.",
			risk.RefsWithoutCommunityData, stamper.unavailableCause(groupName))), nil
	}
	out := map[string]any{
		"mode":                     "conflicts",
		"group":                    groupName,
		"repo":                     repoSlug,
		"base":                     base,
		"per_ref":                  perRef,
		"risk_pairs":               risk.Pairs,
		"ref_count":                risk.RefCount,
		"risky_pair_count":         risk.RiskyPairs,
		"community_data_available": risk.CommunityDataAvailable,
		// #6042 — the verdict-level confidence marker. `inferred_only` means the
		// pairs above are grafel's best reconstruction of what the group partition
		// WOULD have said about entities it has never seen, not a reading of what
		// it did say. Zero risky pairs under that flag is a weaker all-clear.
		"inferred_entity_count":        risk.InferredEntityCount,
		"community_data_inferred_only": risk.CommunityDataInferredOnly,
	}
	if len(risk.RefsWithInferredCommunityData) > 0 {
		out["refs_with_inferred_community_data"] = risk.RefsWithInferredCommunityData
	}
	if risk.CommunityDataInferredOnly {
		out["community_data_note"] = inferredOnlyNote
	}
	stamper.describeInto(out)
	return jsonResult(out), nil
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
