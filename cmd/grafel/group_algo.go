package main

// group_algo.go — hidden `grafel group-algo <group> [--dry-run|--write]`
// subcommand (#5349 A1/A2, epic #5350).
//
// Assembles the union of a group's per-repo graphs and runs the graph
// algorithm pass (Louvain communities + PageRank/Betweenness centrality) ONCE
// at group scope, then prints stats. With --dry-run (the A1 default) it writes
// NO files and mutates no state — a pure read + compute + report. With --write
// (A2) it additionally persists the result as the <group>-algo.json overlay via
// an atomic temp+rename swap (groupalgo.WriteOverlayFromResult); A3's scheduler
// will be the real trigger for this path.
//
// Not part of the public command surface; intercepted before cobra dispatch in
// main.go (mirrors the xrepo-verify / index-internal hidden harnesses). The
// scheduling (A3) lands in a follow-up PR.
//
//	grafel group-algo <group> [--dry-run|--write]

import (
	"fmt"
	"os"
	"sort"

	"github.com/cajasmota/grafel/internal/graph/groupalgo"
)

func runGroupAlgo(args []string) int {
	dryRun := false
	write := false
	var positional []string
	for _, a := range args {
		switch a {
		case "--dry-run":
			dryRun = true
		case "--write":
			write = true
		default:
			positional = append(positional, a)
		}
	}
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "usage: grafel group-algo <group> [--dry-run|--write]")
		return 2
	}
	if write && dryRun {
		fmt.Fprintln(os.Stderr, "grafel group-algo: --write and --dry-run are mutually exclusive")
		return 2
	}
	// Default (no flag) stays dry — A1 behavior is preserved.
	if !write {
		dryRun = true
	}
	group := positional[0]

	res, err := groupalgo.RunGroupAlgorithms(group)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grafel group-algo: %v\n", err)
		return 1
	}

	if write {
		if werr := groupalgo.WriteOverlayFromResult(res); werr != nil {
			fmt.Fprintf(os.Stderr, "grafel group-algo: write overlay: %v\n", werr)
			return 1
		}
	}

	printGroupAlgoStats(os.Stdout, res, dryRun)
	return 0
}

// printGroupAlgoStats reports the union size, community spread (how many
// communities span more than one repo — the whole point of group scope),
// modularity, and the top-10 PageRank entities with their source repo.
func printGroupAlgoStats(w *os.File, res *groupalgo.GroupAlgoResult, dryRun bool) {
	r := res.Results

	fmt.Fprintf(w, "group-algo: %s", res.Group)
	if dryRun {
		fmt.Fprint(w, " (dry-run — no files written)")
	} else {
		fmt.Fprint(w, " (overlay written)")
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  repos:          %d\n", res.NumRepos)
	fmt.Fprintf(w, "  union entities: %d\n", res.NumEntities)
	fmt.Fprintf(w, "  union rels:     %d\n", res.NumRels)

	if r == nil || len(r.CommunityID) == 0 {
		fmt.Fprintln(w, "  (empty group — no algorithm output)")
		return
	}

	// Count communities and how many SPAN more than one repo.
	reposPerCommunity := map[int]map[string]struct{}{}
	for entityID, cid := range r.CommunityID {
		if cid < 0 {
			continue // -1 ungrouped / -2 not-computed sentinels
		}
		repo := res.EntityRepo[entityID]
		if reposPerCommunity[cid] == nil {
			reposPerCommunity[cid] = map[string]struct{}{}
		}
		if repo != "" {
			reposPerCommunity[cid][repo] = struct{}{}
		}
	}
	spanning := 0
	for _, repos := range reposPerCommunity {
		if len(repos) > 1 {
			spanning++
		}
	}

	fmt.Fprintf(w, "  communities:    %d (%d span >1 repo)\n", r.Stats.NumCommunities, spanning)
	fmt.Fprintf(w, "  modularity:     %.4f\n", r.Stats.LouvainModularity)
	fmt.Fprintf(w, "  god nodes:      %d\n", r.Stats.NumGodNodes)
	fmt.Fprintf(w, "  articulation:   %d\n", r.Stats.NumArticulationPts)

	// Top-10 PageRank entities with their repo.
	type prRow struct {
		id   string
		pr   float64
		repo string
	}
	rows := make([]prRow, 0, len(r.PageRank))
	for id, pr := range r.PageRank {
		rows = append(rows, prRow{id: id, pr: pr, repo: res.EntityRepo[id]})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].pr != rows[j].pr {
			return rows[i].pr > rows[j].pr
		}
		return rows[i].id < rows[j].id
	})
	n := 10
	if len(rows) < n {
		n = len(rows)
	}
	fmt.Fprintln(w, "  top-10 pagerank:")
	for i := 0; i < n; i++ {
		repo := rows[i].repo
		if repo == "" {
			repo = "?"
		}
		fmt.Fprintf(w, "    %2d. %-10s  pr=%.6f  %s\n", i+1, repo, rows[i].pr, rows[i].id)
	}
}
