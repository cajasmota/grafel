// diff.go — differential validator (#5349 A4, epic #5350).
//
// The trust gate for Part A's per-repo->group migration. It runs BOTH passes
// over a group's assembled union and reports how the group-scope result differs
// from the OLD per-repo result:
//
//   - OLD: graph.RunAlgorithms run on EACH repo's graph independently (the
//     production per-repo Pass-4 was removed in A3, so the harness re-derives it
//     locally for comparison — it does NOT depend on any per-repo pass still
//     running).
//   - NEW: groupalgo.RunGroupAlgorithms over the union (the A1 deliverable).
//
// Reported deltas:
//   - CommunityChanged: # entities whose community_id changed (per-repo->group).
//   - PageRankRankChurn: how the top-N PageRank importance ordering shifts.
//   - ModularityDelta:  group modularity - mean per-repo modularity.
//   - CrossRepoRankRegressions: the CORE THESIS sanity assertion. Every entity
//     that is called cross-repo (receives a phantom CALLS edge from another
//     repo) must rank >= its per-repo PageRank rank at group scope — cross-repo
//     hubs should gain importance, never lose it. Any regression is a failure
//     and is listed here.
//
// The whole report marshals to JSON (DiffReport) so CI / the upvate baseline
// re-run can consume it.
package groupalgo

import (
	"sort"

	"github.com/cajasmota/grafel/internal/graph"
)

// RankChurnRow records, for one entity in the group top-N by PageRank, where it
// ranked per-repo vs group (1-based; 0 = "outside the compared top-N / absent").
type RankChurnRow struct {
	EntityID    string `json:"entity_id"`
	Repo        string `json:"repo"`
	GroupRank   int    `json:"group_rank"`
	PerRepoRank int    `json:"per_repo_rank"`
	// RankDelta = PerRepoRank - GroupRank. Positive = the entity rose (better
	// rank number) at group scope; negative = it fell.
	RankDelta float64 `json:"rank_delta"`
}

// RankRegression is a cross-repo-called entity whose PageRank RANK got worse
// (numerically larger) at group scope — a violation of the core thesis.
type RankRegression struct {
	EntityID    string `json:"entity_id"`
	Repo        string `json:"repo"`
	PerRepoRank int    `json:"per_repo_rank"`
	GroupRank   int    `json:"group_rank"`
}

// DiffReport is the machine-readable output of the differential validator.
type DiffReport struct {
	Group              string `json:"group"`
	NumRepos           int    `json:"num_repos"`
	NumEntities        int    `json:"num_entities"`
	NumRels            int    `json:"num_relationships"`
	TopN               int    `json:"top_n"`
	BetweennessSampled bool   `json:"betweenness_sampled"`

	// Community churn.
	CommunityChanged int `json:"community_changed"`

	// Modularity.
	GroupModularity       float64 `json:"group_modularity"`
	MeanPerRepoModularity float64 `json:"mean_per_repo_modularity"`
	ModularityDelta       float64 `json:"modularity_delta"`

	// PageRank rank churn over the group top-N.
	TopRankChurn []RankChurnRow `json:"top_rank_churn"`

	// The core thesis assertion. CrossRepoEntities is the count of entities
	// that receive a cross-repo phantom CALLS edge. CrossRepoRankRegressions
	// lists any that LOST PageRank rank at group scope (must be empty).
	CrossRepoEntities        int              `json:"cross_repo_entities"`
	CrossRepoRankRegressions []RankRegression `json:"cross_repo_rank_regressions"`
	// CrossRepoRankNonDecreasing is the pass/fail of the assertion: true iff
	// CrossRepoRankRegressions is empty.
	CrossRepoRankNonDecreasing bool `json:"cross_repo_rank_non_decreasing"`
}

// rankMap turns a PageRank score map into 1-based ranks (rank 1 = highest
// score). Ties break by entity ID for determinism. Entities absent from the
// score map are not in the result.
func rankMap(pr map[string]float64) map[string]int {
	type row struct {
		id string
		pr float64
	}
	rows := make([]row, 0, len(pr))
	for id, v := range pr {
		rows = append(rows, row{id, v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].pr != rows[j].pr {
			return rows[i].pr > rows[j].pr
		}
		return rows[i].id < rows[j].id
	})
	out := make(map[string]int, len(rows))
	for i, r := range rows {
		out[r.id] = i + 1
	}
	return out
}

// crossRepoCalledEntities returns the set of entity IDs that receive a CALLS
// edge whose source is in a DIFFERENT repo (the phantom cross-repo edges). This
// is the population the core thesis is about: their inbound cross-repo edges are
// only visible at group scope, so their rank must not regress.
func crossRepoCalledEntities(rels []graph.Relationship, entityRepo map[string]string) map[string]bool {
	out := map[string]bool{}
	for _, r := range rels {
		if r.Kind != "CALLS" {
			continue
		}
		fromRepo, okF := entityRepo[r.FromID]
		toRepo, okT := entityRepo[r.ToID]
		if !okF || !okT {
			continue
		}
		if fromRepo != toRepo {
			out[r.ToID] = true
		}
	}
	return out
}

// DiffGroup runs the differential validator for a group: it assembles the union
// once, runs the OLD per-repo pass on each repo independently, runs the NEW
// group pass over the union, and returns a DiffReport. topN bounds the PageRank
// rank-churn table (<=0 defaults to 25).
func DiffGroup(group string, topN int) (*DiffReport, error) {
	if topN <= 0 {
		topN = 25
	}

	// NEW group pass (also gives us the union + per-repo attribution).
	groupRes, err := RunGroupAlgorithms(group)
	if err != nil {
		return nil, err
	}
	ents, rels, entityRepo, _, err := AssembleGroupGraph(group)
	if err != nil {
		return nil, err
	}

	// OLD per-repo pass: partition the union back into per-repo slices and run
	// graph.RunAlgorithms on each independently (re-derives the pre-A3 pass).
	repoEnts := map[string][]graph.Entity{}
	for _, e := range ents {
		slug := entityRepo[e.ID]
		repoEnts[slug] = append(repoEnts[slug], e)
	}
	repoRels := map[string][]graph.Relationship{}
	for _, r := range rels {
		fromRepo := entityRepo[r.FromID]
		toRepo := entityRepo[r.ToID]
		// A relationship belongs to the per-repo pass only when BOTH endpoints
		// are in the same repo (cross-repo phantom edges did not exist in the
		// old per-repo world).
		if fromRepo != "" && fromRepo == toRepo {
			repoRels[fromRepo] = append(repoRels[fromRepo], r)
		}
	}

	perRepoPR := map[string]float64{} // entity id -> per-repo pagerank
	perRepoComm := map[string]int{}   // entity id -> per-repo community id
	var modularities []float64
	commBaseNext := 0
	// Stable repo order for deterministic community-id namespacing.
	repoSlugs := make([]string, 0, len(repoEnts))
	for slug := range repoEnts {
		repoSlugs = append(repoSlugs, slug)
	}
	sort.Strings(repoSlugs)
	for _, slug := range repoSlugs {
		res := graph.RunAlgorithms(repoEnts[slug], repoRels[slug])
		if res == nil {
			continue
		}
		modularities = append(modularities, res.Stats.LouvainModularity)
		base := commBaseNext
		maxCid := -1
		for id, pr := range res.PageRank {
			perRepoPR[id] = pr
		}
		for id, cid := range res.CommunityID {
			if cid < 0 {
				perRepoComm[id] = cid // keep sentinel as-is
				continue
			}
			perRepoComm[id] = base + cid
			if cid > maxCid {
				maxCid = cid
			}
		}
		commBaseNext = base + maxCid + 1
	}

	groupPR := groupRes.Results.PageRank
	groupComm := groupRes.Results.CommunityID

	// Community churn: count entities whose community id changed. Because the
	// numeric ids are not comparable across the two passes, we compare via a
	// canonical signature: two entities are "co-clustered the same" iff their
	// (this-entity, partner) co-membership is preserved. A cheaper, well-defined
	// proxy used here: an entity "changed" if the SET of repos in its community
	// differs — at group scope cross-repo entities join a multi-repo community
	// they could never be in per-repo. We compute the simplest robust signal:
	// an entity changed iff its group community spans repos its per-repo
	// community did not (i.e. it gained cross-repo co-members).
	communityChanged := communityChurn(groupComm, perRepoComm, entityRepo)

	// PageRank rank maps.
	groupRanks := rankMap(groupPR)
	perRepoRanks := rankMap(perRepoPR)

	// Top-N group entities -> churn rows.
	type prRow struct {
		id string
		pr float64
	}
	grows := make([]prRow, 0, len(groupPR))
	for id, v := range groupPR {
		grows = append(grows, prRow{id, v})
	}
	sort.Slice(grows, func(i, j int) bool {
		if grows[i].pr != grows[j].pr {
			return grows[i].pr > grows[j].pr
		}
		return grows[i].id < grows[j].id
	})
	n := topN
	if len(grows) < n {
		n = len(grows)
	}
	churn := make([]RankChurnRow, 0, n)
	for i := 0; i < n; i++ {
		id := grows[i].id
		gr := groupRanks[id]
		pr := perRepoRanks[id]
		churn = append(churn, RankChurnRow{
			EntityID:    id,
			Repo:        entityRepo[id],
			GroupRank:   gr,
			PerRepoRank: pr,
			RankDelta:   float64(pr - gr),
		})
	}

	// Core thesis assertion: cross-repo-called entities must not lose rank.
	xrepo := crossRepoCalledEntities(rels, entityRepo)
	var regressions []RankRegression
	for id := range xrepo {
		gr, okG := groupRanks[id]
		pr, okR := perRepoRanks[id]
		if !okG || !okR {
			continue // not ranked in one pass — skip (can't compare)
		}
		// A larger rank number = worse importance. Regression iff group rank is
		// strictly worse than per-repo rank.
		if gr > pr {
			regressions = append(regressions, RankRegression{
				EntityID:    id,
				Repo:        entityRepo[id],
				PerRepoRank: pr,
				GroupRank:   gr,
			})
		}
	}
	sort.Slice(regressions, func(i, j int) bool {
		return regressions[i].EntityID < regressions[j].EntityID
	})

	meanMod := 0.0
	if len(modularities) > 0 {
		sum := 0.0
		for _, m := range modularities {
			sum += m
		}
		meanMod = sum / float64(len(modularities))
	}
	groupMod := groupRes.Results.Stats.LouvainModularity

	return &DiffReport{
		Group:                      group,
		NumRepos:                   groupRes.NumRepos,
		NumEntities:                groupRes.NumEntities,
		NumRels:                    groupRes.NumRels,
		TopN:                       n,
		BetweennessSampled:         groupRes.NumEntities > betweennessSampleThresholdValueExported(),
		CommunityChanged:           communityChanged,
		GroupModularity:            groupMod,
		MeanPerRepoModularity:      meanMod,
		ModularityDelta:            groupMod - meanMod,
		TopRankChurn:               churn,
		CrossRepoEntities:          len(xrepo),
		CrossRepoRankRegressions:   regressions,
		CrossRepoRankNonDecreasing: len(regressions) == 0,
	}, nil
}

// communityChurn counts entities whose community co-membership gained cross-repo
// members at group scope (i.e. their group community spans more repos than they
// could ever be co-clustered with per-repo). This is a stable, id-namespacing-
// independent measure of "the community assignment changed because of the union".
func communityChurn(groupComm map[string]int, perRepoComm map[string]int, entityRepo map[string]string) int {
	// repos present in each GROUP community.
	groupCommRepos := map[int]map[string]struct{}{}
	for id, cid := range groupComm {
		if cid < 0 {
			continue
		}
		if groupCommRepos[cid] == nil {
			groupCommRepos[cid] = map[string]struct{}{}
		}
		if repo := entityRepo[id]; repo != "" {
			groupCommRepos[cid][repo] = struct{}{}
		}
	}
	changed := 0
	for id, gcid := range groupComm {
		if gcid < 0 {
			continue
		}
		// Per-repo, an entity's community is by construction single-repo. If its
		// GROUP community spans >1 repo, its assignment materially changed.
		if len(groupCommRepos[gcid]) > 1 {
			changed++
			continue
		}
		// Single-repo group community: it changed iff the entity was ungrouped
		// per-repo but grouped now (or vice-versa).
		pr, ok := perRepoComm[id]
		if !ok || pr < 0 {
			changed++
		}
	}
	return changed
}

// betweennessSampleThresholdValueExported bridges to the package-private
// threshold in the graph package via a small indirection (the diff report only
// needs to know whether the group was large enough to sample). It mirrors
// graph.betweennessSampleThresholdValue() — kept here so groupalgo does not have
// to export internals from the graph package. Default matches graph's default.
func betweennessSampleThresholdValueExported() int {
	return graph.BetweennessSampleThreshold()
}
