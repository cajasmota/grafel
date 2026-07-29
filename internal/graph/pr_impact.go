// Package graph — pr_impact.go computes diff/PR-scoped impact analysis and
// cross-change merge-risk triage (issue #4292).
//
// This file is pure-Go and MCP-free, mirroring AnalyzeOrientation (#4290): the
// thin MCP handler loads the per-ref graphs and supplies the changed-entity set
// (reusing the same DiffDocs machinery that backs grafel_diff_refs), then
// calls into these functions. Keeping the analysis pure makes it unit-testable
// on an in-memory graph with no daemon, no registry, and no git.
//
// Two analyses:
//
//  1. AnalyzePRImpact — single change. Given the HEAD graph (entities +
//     relationships) and the set of entity IDs the diff touched, it resolves
//     which communities those entities belong to (Entity.CommunityID from the
//     Pass-4 algorithm pass) and walks the INBOUND dependency graph to find the
//     downstream blast radius — every entity that transitively depends on a
//     changed entity. This is the same inbound-BFS reachability used by
//     grafel_impact_radius, generalised to a *set* of seeds.
//
//  2. AnalyzeMergeRisk — multiple changes. Given each change's impacted-
//     community set, it intersects them pairwise; any two changes whose
//     impacted communities overlap are a merge-order / conflict risk. Pairs are
//     ranked by shared-community count (descending), with the shared community
//     ids listed.
//
// All outputs are deterministic with ID/index tiebreaks, per the #481 contract.
package graph

import "sort"

// ---------------------------------------------------------------------------
// Single-change impact
// ---------------------------------------------------------------------------

// ChangedEntity is a slim record of an entity the diff touched, annotated with
// its community and a change-class (added | removed | modified).
type ChangedEntity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	SourceFile  string `json:"source_file,omitempty"`
	Change      string `json:"change"`       // added | removed | modified
	CommunityID int    `json:"community_id"` // -1 when ungrouped/unknown

	// CommunitySource says HOW CommunityID was arrived at (#6042):
	// "overlay" (measured — the group-algo partition placed it), "inferred"
	// (deduced from placed neighbours, see pr_impact_infer.go), or "none" (not
	// placed at all, CommunityID is -1).
	//
	// A caller that ignores this field is reading a guess as a measurement, which
	// is the same defect class as #6006 — hence no omitempty: the label is always
	// on the wire.
	CommunitySource string `json:"community_source"`
}

// ImpactedCommunity is a community touched by the change, with how many changed
// entities and how many blast-radius entities fall inside it.
type ImpactedCommunity struct {
	CommunityID    int `json:"community_id"`
	ChangedCount   int `json:"changed_count"`    // changed entities in this community
	BlastRadiusHit int `json:"blast_radius_hit"` // downstream entities in this community
	// InferredChangedCount is how many of ChangedCount were placed here by
	// INFERENCE rather than by the overlay (#6042). Equal to ChangedCount means
	// this community's involvement is entirely deduced.
	InferredChangedCount int `json:"inferred_changed_count,omitempty"`
}

// BlastEntity is a downstream entity that transitively depends on a changed
// entity, annotated with the BFS hop distance from the nearest changed seed.
type BlastEntity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	SourceFile  string `json:"source_file,omitempty"`
	CommunityID int    `json:"community_id"`
	HopDistance int    `json:"hop_distance"`
}

// PRImpactResult is the structured output of AnalyzePRImpact.
type PRImpactResult struct {
	ChangedEntities     []ChangedEntity     `json:"changed_entities"`
	ImpactedCommunities []ImpactedCommunity `json:"impacted_communities"`
	BlastRadius         []BlastEntity       `json:"blast_radius"`

	// Aggregate counts for quick triage.
	ChangedCount     int  `json:"changed_count"`
	CommunityCount   int  `json:"impacted_community_count"`
	BlastRadiusCount int  `json:"blast_radius_count"`
	Truncated        bool `json:"truncated,omitempty"`

	// CommunityDataAvailable is the validity flag for every community-derived
	// field in this struct (ImpactedCommunities, CommunityCount,
	// ImpactedCommunityIDs, and therefore the whole merge-risk analysis built on
	// top of them). Issue #6006.
	//
	// It exists because the per-repo Pass-4 algorithm pass was removed when the
	// group-scope algo pass replaced it: entities loaded straight out of graph.fb
	// carry the -2/nil sentinel, so every community-derived output collapses to
	// empty. Without this flag "no impacted communities" is indistinguishable
	// from "communities were never computed" — and on a merge-risk question that
	// reads as "safe to merge", which is the worst direction to be wrong in.
	//
	// MEASURED OVER THE CHANGED SET, NOT THE ENTITY SET. The first cut of this
	// flag asked "does any entity anywhere in the graph carry a community?", and
	// that is the wrong question: the group-algo overlay is computed from the
	// INDEXED GROUP UNION, so entities that exist only on a feature ref — every
	// entity a PR ADDS — are absent from it by construction. A pure add-only PR
	// therefore has a fully-covered repo and a completely uncovered change set,
	// and the graph-wide flag reported `community_data_available: true` next to
	// `risky_pair_count: 0` — an affirmative all-clear on a change nothing was
	// known about. Coverage of the entities we are actually reasoning about is
	// the only coverage that makes the answer valid.
	//
	// An EMPTY change set is vacuously available: nothing changed, so nothing can
	// conflict, and that is a real answer rather than a missing one. (This is the
	// live default-base case — conflicts mode diffs refs[0] against itself.)
	CommunityDataAvailable bool `json:"community_data_available"`
	// ChangedWithoutCommunity counts changed entities carrying no community, i.e.
	// the entities this analysis could not place — neither by the overlay nor by
	// inference. Non-zero with CommunityDataAvailable=true is PARTIAL coverage:
	// the verdict stands on the entities that were placed, but this many were
	// invisible to it.
	ChangedWithoutCommunity int `json:"changed_entities_without_community"`

	// ── #6042: measured vs deduced ──────────────────────────────────────────
	//
	// ChangedWithOverlayCommunity + ChangedWithInferredCommunity +
	// ChangedWithoutCommunity == ChangedCount, always.

	// ChangedWithOverlayCommunity counts changed entities the group-algo overlay
	// placed directly. These are MEASURED.
	ChangedWithOverlayCommunity int `json:"changed_entities_with_overlay_community"`
	// ChangedWithInferredCommunity counts changed entities placed by inference
	// from their placed neighbours (pr_impact_infer.go). These are DEDUCED — good
	// enough to triage with, not good enough to present as measured.
	ChangedWithInferredCommunity int `json:"changed_entities_with_inferred_community"`
	// CommunityDataInferredOnly is the verdict-level confidence marker: the
	// analysis ran, but EVERY placement behind it was inferred. An agent should
	// weigh such a verdict differently from one the overlay measured. False when
	// nothing was placed at all — that case is a decline
	// (CommunityDataAvailable=false), not a low-confidence answer.
	CommunityDataInferredOnly bool `json:"community_data_inferred_only"`
}

// PRImpactOptions bounds the analysis.
type PRImpactOptions struct {
	Hops          int // downstream BFS depth (default 3, clamped [1,6])
	MaxBlastNodes int // cap on blast_radius entries returned (default 500)
}

// DefaultPRImpactOptions returns the production caps.
func DefaultPRImpactOptions() PRImpactOptions {
	return PRImpactOptions{Hops: 3, MaxBlastNodes: 500}
}

func (o PRImpactOptions) normalized() PRImpactOptions {
	if o.Hops <= 0 {
		o.Hops = 3
	}
	if o.Hops > 6 {
		o.Hops = 6
	}
	if o.MaxBlastNodes <= 0 {
		o.MaxBlastNodes = 500
	}
	return o
}

// ChangeSet is the diff-derived input to AnalyzePRImpact: the entity IDs the
// diff classified as added / removed / modified between base and head. The MCP
// handler fills this from DiffDocs (the diff_refs engine), so this package does
// not duplicate the git-diff logic.
type ChangeSet struct {
	Added    []DiffEntityEntry
	Removed  []DiffEntityEntry
	Modified []DiffEntityEntry
}

// ChangedIDs returns the union of changed entity IDs (added+removed+modified),
// deduplicated and sorted for determinism. This is the seed set for both the
// community resolution and the downstream BFS.
func (c ChangeSet) ChangedIDs() []string {
	seen := map[string]struct{}{}
	for _, group := range [][]DiffEntityEntry{c.Added, c.Removed, c.Modified} {
		for _, e := range group {
			if e.ID != "" {
				seen[e.ID] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// AnalyzePRImpact computes the single-change impact view: changed entities ->
// impacted communities -> downstream blast radius.
//
// `entities`/`rels` are the HEAD graph (added & modified entities live here;
// removed entities won't, which is fine — they have no downstream in HEAD).
// `change` is the diff-derived change set. The function:
//
//  1. annotates each changed entity with its community,
//  2. walks the INBOUND graph from the changed seeds (callers-of-callers,
//     bounded by opts.Hops) to find every downstream dependent, and
//  3. rolls up per-community changed/blast counts.
func AnalyzePRImpact(entities []Entity, rels []Relationship, change ChangeSet, opts PRImpactOptions) PRImpactResult {
	opts = opts.normalized()

	byID := make(map[string]Entity, len(entities))
	for i := range entities {
		byID[entities[i].ID] = entities[i]
	}

	// ── Part 1: changed entities + their communities ─────────────────────────
	classOf := map[string]string{}
	for _, e := range change.Removed {
		classOf[e.ID] = "removed"
	}
	for _, e := range change.Added {
		classOf[e.ID] = "added"
	}
	for _, e := range change.Modified {
		classOf[e.ID] = "modified"
	}

	changedIDs := change.ChangedIDs()
	seedSet := make(map[string]struct{}, len(changedIDs))
	// #6042: the changed entities the overlay could NOT place, and which are
	// present in the head graph so they have neighbours to read. Computed before
	// the edge pass below so that pass can capture their edges in one sweep
	// instead of building a whole outbound adjacency for the graph.
	unplacedChanged := make(map[string]struct{})
	for _, id := range changedIDs {
		seedSet[id] = struct{}{}
		if e, ok := byID[id]; ok && communityOf(e) < 0 {
			unplacedChanged[id] = struct{}{}
		}
	}

	// Inbound adjacency: in[X] = entities that depend on X (callers). Restricted
	// to edges whose both endpoints are present in the entity set, matching the
	// edge-filtering contract used elsewhere.
	//
	// The same sweep collects the #6042 inference inputs — outbound targets and
	// CONTAINS parents — but ONLY for unplaced changed entities, so the extra
	// memory is O(deg(changed)) rather than O(E).
	in := make(map[string][]string, len(entities))
	var inferParents, inferTargets map[string][]string
	if len(unplacedChanged) > 0 {
		inferParents = make(map[string][]string, len(unplacedChanged))
		inferTargets = make(map[string][]string, len(unplacedChanged))
	}
	for _, r := range rels {
		if r.FromID == "" || r.ToID == "" || r.FromID == r.ToID {
			continue
		}
		if _, ok := byID[r.FromID]; !ok {
			continue
		}
		if _, ok := byID[r.ToID]; !ok {
			continue
		}
		// r.FromID depends on r.ToID, so FromID is a downstream dependent of ToID.
		in[r.ToID] = append(in[r.ToID], r.FromID)

		if len(unplacedChanged) > 0 {
			if _, ok := unplacedChanged[r.FromID]; ok {
				inferTargets[r.FromID] = append(inferTargets[r.FromID], r.ToID)
			}
			if r.Kind == "CONTAINS" {
				if _, ok := unplacedChanged[r.ToID]; ok {
					inferParents[r.ToID] = append(inferParents[r.ToID], r.FromID)
				}
			}
		}
	}
	inferrer := newCommunityInferrer(entities, byID, unplacedChanged, inferParents, inferTargets)

	changed := make([]ChangedEntity, 0, len(changedIDs))
	// communityChanged[community] = #changed entities in it; communityInferred is
	// the inferred subset of that, so a caller can see how much of a community's
	// involvement was deduced rather than measured.
	communityChanged := map[int]int{}
	communityInferred := map[int]int{}
	// #6006: how many changed entities we could NOT place in a community. This,
	// not the graph-wide entity set, decides whether the community-derived output
	// below means anything — see PRImpactResult.CommunityDataAvailable.
	changedWithoutCommunity := 0
	// #6042: the measured/deduced split behind the verdict.
	changedFromOverlay, changedFromInference := 0, 0
	for _, id := range changedIDs {
		comm := -1
		source := CommunitySourceNone
		var name, kind, src string
		if e, ok := byID[id]; ok {
			comm = communityOf(e)
			if comm >= 0 {
				source = CommunitySourceOverlay
			} else if c, inferred := inferrer.infer(id); inferred {
				comm, source = c, CommunitySourceInferred
			}
			name, kind, src = e.Name, e.Kind, e.SourceFile
		} else {
			// Removed entity (gone from HEAD) — fall back to the diff record.
			for _, group := range [][]DiffEntityEntry{change.Removed, change.Added, change.Modified} {
				for _, de := range group {
					if de.ID == id {
						name, kind, src = de.Name, de.Kind, de.SourceFile
					}
				}
			}
		}
		changed = append(changed, ChangedEntity{
			ID:              id,
			Name:            name,
			Kind:            kind,
			SourceFile:      src,
			Change:          classOf[id],
			CommunityID:     comm,
			CommunitySource: source,
		})
		communityChanged[comm]++
		switch source {
		case CommunitySourceOverlay:
			changedFromOverlay++
		case CommunitySourceInferred:
			changedFromInference++
			communityInferred[comm]++
		default:
			changedWithoutCommunity++
		}
	}
	// Vacuously available when nothing changed; otherwise at least one changed
	// entity must have been placed — by the overlay or, since #6042, by inference
	// — for the community verdict to mean anything.
	communityDataAvailable := len(changedIDs) == 0 || changedWithoutCommunity < len(changedIDs)
	// #6042: the analysis ran, but every placement behind it is a deduction. Not
	// set when nothing was placed at all — that is a decline, not a soft answer.
	inferredOnly := communityDataAvailable && changedFromInference > 0 && changedFromOverlay == 0

	// ── Part 2: downstream blast radius (inbound BFS from all seeds) ──────────
	// Multi-source BFS: distance is hops from the nearest changed seed.
	dist := make(map[string]int, len(changedIDs))
	frontier := make([]string, 0, len(changedIDs))
	for id := range seedSet {
		// Only seeds present in HEAD can have downstream dependents.
		if _, ok := byID[id]; ok {
			dist[id] = 0
			frontier = append(frontier, id)
		}
	}
	sort.Strings(frontier) // deterministic BFS expansion order
	for d := 0; d < opts.Hops && len(frontier) > 0; d++ {
		var next []string
		for _, n := range frontier {
			deps := in[n]
			sort.Strings(deps)
			for _, dep := range deps {
				if _, seen := dist[dep]; seen {
					continue
				}
				dist[dep] = d + 1
				next = append(next, dep)
			}
		}
		sort.Strings(next)
		frontier = next
	}

	// Blast radius = reached entities that are not themselves changed seeds.
	communityBlast := map[int]int{}
	blast := make([]BlastEntity, 0, len(dist))
	for id, d := range dist {
		if _, isSeed := seedSet[id]; isSeed {
			continue
		}
		e, ok := byID[id]
		if !ok {
			continue
		}
		comm := communityOf(e)
		communityBlast[comm]++
		blast = append(blast, BlastEntity{
			ID:          id,
			Name:        e.Name,
			Kind:        e.Kind,
			SourceFile:  e.SourceFile,
			CommunityID: comm,
			HopDistance: d,
		})
	}
	// Deterministic order: nearest first, then ID.
	sort.SliceStable(blast, func(i, j int) bool {
		if blast[i].HopDistance != blast[j].HopDistance {
			return blast[i].HopDistance < blast[j].HopDistance
		}
		return blast[i].ID < blast[j].ID
	})
	blastTotal := len(blast)
	truncated := false
	if len(blast) > opts.MaxBlastNodes {
		blast = blast[:opts.MaxBlastNodes]
		truncated = true
	}

	// ── Part 3: impacted communities roll-up ─────────────────────────────────
	commIDs := map[int]struct{}{}
	for c := range communityChanged {
		commIDs[c] = struct{}{}
	}
	for c := range communityBlast {
		commIDs[c] = struct{}{}
	}
	impacted := make([]ImpactedCommunity, 0, len(commIDs))
	for c := range commIDs {
		// #6006: align this filter with ImpactedCommunityIDs and AnalyzeMergeRisk,
		// both of which already drop c < 0. Without it the payload reported
		// impacted_community_count: 1 with a single community_id: -1 — the
		// "ungrouped" bucket dressed up as a real community, which reads as a
		// genuine (and singular) impact signal. Entities with no community are
		// still fully reported in changed_entities / blast_radius, each carrying
		// community_id: -1; they just do not manufacture a community here.
		if c < 0 {
			continue
		}
		impacted = append(impacted, ImpactedCommunity{
			CommunityID:          c,
			ChangedCount:         communityChanged[c],
			BlastRadiusHit:       communityBlast[c],
			InferredChangedCount: communityInferred[c],
		})
	}
	// Rank by total touch (changed+blast) desc, then community id asc.
	sort.SliceStable(impacted, func(i, j int) bool {
		ti := impacted[i].ChangedCount + impacted[i].BlastRadiusHit
		tj := impacted[j].ChangedCount + impacted[j].BlastRadiusHit
		if ti != tj {
			return ti > tj
		}
		return impacted[i].CommunityID < impacted[j].CommunityID
	})

	return PRImpactResult{
		ChangedEntities:     changed,
		ImpactedCommunities: impacted,
		BlastRadius:         blast,
		ChangedCount:        len(changed),
		CommunityCount:      len(impacted),
		BlastRadiusCount:    blastTotal,
		Truncated:           truncated,

		CommunityDataAvailable:  communityDataAvailable,
		ChangedWithoutCommunity: changedWithoutCommunity,

		ChangedWithOverlayCommunity:  changedFromOverlay,
		ChangedWithInferredCommunity: changedFromInference,
		CommunityDataInferredOnly:    inferredOnly,
	}
}

// ImpactedCommunityIDs returns the sorted set of (grouped) community ids a
// PRImpactResult touches — used as the merge-risk overlap key. Ungrouped (-1)
// is excluded: "everything not in a community" is not a meaningful conflict
// signal, and including it would make every unrelated pair appear to overlap.
func (r PRImpactResult) ImpactedCommunityIDs() []int {
	out := make([]int, 0, len(r.ImpactedCommunities))
	for _, c := range r.ImpactedCommunities {
		if c.CommunityID < 0 {
			continue
		}
		out = append(out, c.CommunityID)
	}
	sort.Ints(out)
	return out
}

// ---------------------------------------------------------------------------
// Cross-change merge-risk
// ---------------------------------------------------------------------------

// ChangeImpact pairs a ref label with its impacted-community set. The MCP
// handler builds one per input ref (running AnalyzePRImpact for each), then
// hands the slice to AnalyzeMergeRisk.
type ChangeImpact struct {
	Ref         string // ref / branch / PR label
	Communities []int  // impacted community ids (grouped only)

	// CommunityDataAvailable carries PRImpactResult.CommunityDataAvailable for
	// this ref (#6006). An empty Communities slice means "this change touched no
	// community" only when this is true; when it is false the slice is empty
	// because nothing was computed, and no conclusion about merge safety can be
	// drawn from this ref at all.
	CommunityDataAvailable bool

	// OverlayEntityCount / InferredEntityCount carry this ref's measured/deduced
	// split (#6042), so the merge-risk verdict can say whether it rests on the
	// group-algo partition or on inference from placed neighbours.
	OverlayEntityCount  int
	InferredEntityCount int
}

// MergeRiskPair is two refs whose impacted-community sets overlap.
type MergeRiskPair struct {
	RefA              string `json:"ref_a"`
	RefB              string `json:"ref_b"`
	SharedCount       int    `json:"shared_community_count"`
	SharedCommunities []int  `json:"shared_communities"`
}

// MergeRiskResult is the ranked triage output of AnalyzeMergeRisk.
type MergeRiskResult struct {
	Pairs      []MergeRiskPair `json:"risk_pairs"`
	RefCount   int             `json:"ref_count"`
	RiskyPairs int             `json:"risky_pair_count"`

	// CommunityDataAvailable is false when ANY input ref lacked community data
	// (#6006). A zero risky_pair_count is only a "these refs do not conflict"
	// answer when this is true; when it is false the analysis did not run and the
	// empty pair list carries no safety information whatsoever.
	//
	// Deliberately conservative: one uncovered ref taints the whole result,
	// because merge risk is a property of the pair set, not of a single ref.
	CommunityDataAvailable bool `json:"community_data_available"`
	// RefsWithoutCommunityData names the refs that had no community data, sorted.
	RefsWithoutCommunityData []string `json:"refs_without_community_data,omitempty"`

	// ── #6042: how much of this verdict is deduced ──────────────────────────

	// InferredEntityCount totals the changed entities across all refs that were
	// placed by INFERENCE rather than by the group-algo overlay.
	InferredEntityCount int `json:"inferred_entity_count"`
	// CommunityDataInferredOnly is true when the analysis ran but NO ref
	// contributed a single overlay-measured entity — the add-only PR shape #6042
	// exists for. The pairs below are then a reasoned guess at what Louvain would
	// have said, not a reading of what it did say. False when the data was
	// unavailable altogether: that is a decline, not a low-confidence answer.
	CommunityDataInferredOnly bool `json:"community_data_inferred_only"`
	// RefsWithInferredCommunityData names the refs that contributed at least one
	// inferred placement, sorted.
	RefsWithInferredCommunityData []string `json:"refs_with_inferred_community_data,omitempty"`
}

// AnalyzeMergeRisk intersects every change's impacted-community set pairwise and
// returns the pairs that overlap, ranked by shared-community count descending.
// Refs with disjoint community sets are safe to merge in any order and are
// omitted from the result.
//
// Determinism: input refs are sorted by label; pairs are emitted with RefA<RefB
// and ranked by (sharedCount desc, RefA asc, RefB asc).
func AnalyzeMergeRisk(impacts []ChangeImpact) MergeRiskResult {
	// Normalise: sort each community set + dedupe, and sort refs by label.
	norm := make([]ChangeImpact, len(impacts))
	copy(norm, impacts)
	sort.SliceStable(norm, func(i, j int) bool { return norm[i].Ref < norm[j].Ref })
	var missing, inferredRefs []string
	totalInferred, totalOverlay := 0, 0
	for _, ci := range norm {
		if !ci.CommunityDataAvailable {
			missing = append(missing, ci.Ref)
		}
		totalInferred += ci.InferredEntityCount
		totalOverlay += ci.OverlayEntityCount
		if ci.InferredEntityCount > 0 {
			inferredRefs = append(inferredRefs, ci.Ref)
		}
	}
	sets := make([]map[int]struct{}, len(norm))
	for i, ci := range norm {
		s := make(map[int]struct{}, len(ci.Communities))
		for _, c := range ci.Communities {
			if c >= 0 {
				s[c] = struct{}{}
			}
		}
		sets[i] = s
	}

	var pairs []MergeRiskPair
	for i := 0; i < len(norm); i++ {
		for j := i + 1; j < len(norm); j++ {
			shared := intersectSets(sets[i], sets[j])
			if len(shared) == 0 {
				continue
			}
			sort.Ints(shared)
			pairs = append(pairs, MergeRiskPair{
				RefA:              norm[i].Ref,
				RefB:              norm[j].Ref,
				SharedCount:       len(shared),
				SharedCommunities: shared,
			})
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].SharedCount != pairs[j].SharedCount {
			return pairs[i].SharedCount > pairs[j].SharedCount
		}
		if pairs[i].RefA != pairs[j].RefA {
			return pairs[i].RefA < pairs[j].RefA
		}
		return pairs[i].RefB < pairs[j].RefB
	})

	return MergeRiskResult{
		Pairs:      pairs,
		RefCount:   len(norm),
		RiskyPairs: len(pairs),

		CommunityDataAvailable:   len(missing) == 0,
		RefsWithoutCommunityData: missing,

		InferredEntityCount: totalInferred,
		// Available, something was inferred, and nothing at all was measured.
		CommunityDataInferredOnly:     len(missing) == 0 && totalInferred > 0 && totalOverlay == 0,
		RefsWithInferredCommunityData: inferredRefs,
	}
}

func intersectSets(a, b map[int]struct{}) []int {
	// Iterate the smaller set for efficiency.
	if len(b) < len(a) {
		a, b = b, a
	}
	var out []int
	for k := range a {
		if _, ok := b[k]; ok {
			out = append(out, k)
		}
	}
	return out
}
