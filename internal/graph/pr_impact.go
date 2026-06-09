// Package graph — pr_impact.go computes diff/PR-scoped impact analysis and
// cross-change merge-risk triage (issue #4292).
//
// This file is pure-Go and MCP-free, mirroring AnalyzeOrientation (#4290): the
// thin MCP handler loads the per-ref graphs and supplies the changed-entity set
// (reusing the same DiffDocs machinery that backs archigraph_diff_refs), then
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
//     archigraph_impact_radius, generalised to a *set* of seeds.
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
}

// ImpactedCommunity is a community touched by the change, with how many changed
// entities and how many blast-radius entities fall inside it.
type ImpactedCommunity struct {
	CommunityID    int `json:"community_id"`
	ChangedCount   int `json:"changed_count"`    // changed entities in this community
	BlastRadiusHit int `json:"blast_radius_hit"` // downstream entities in this community
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

	// Inbound adjacency: in[X] = entities that depend on X (callers). Restricted
	// to edges whose both endpoints are present in the entity set, matching the
	// edge-filtering contract used elsewhere.
	in := make(map[string][]string, len(entities))
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
	changed := make([]ChangedEntity, 0, len(changedIDs))
	// communityChanged[community] = #changed entities in it.
	communityChanged := map[int]int{}
	seedSet := make(map[string]struct{}, len(changedIDs))
	for _, id := range changedIDs {
		seedSet[id] = struct{}{}
		comm := -1
		var name, kind, src string
		if e, ok := byID[id]; ok {
			comm = communityOf(e)
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
			ID:          id,
			Name:        name,
			Kind:        kind,
			SourceFile:  src,
			Change:      classOf[id],
			CommunityID: comm,
		})
		communityChanged[comm]++
	}

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
		impacted = append(impacted, ImpactedCommunity{
			CommunityID:    c,
			ChangedCount:   communityChanged[c],
			BlastRadiusHit: communityBlast[c],
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
