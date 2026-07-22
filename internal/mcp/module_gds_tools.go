// module_gds_tools.go — MCP handler for the module-LEVEL graph data-science
// surface (issue #1384, part of epic #1380).
//
// Single tool with action=cycles|centrality|all over the aggregated module
// graph. Bundled (rather than split into two tools as initially planned) to
// stay under the ≤3k handshake-token ceiling (#1639) — splitting added +196
// tokens; the action-bundle pattern is already used by patterns/topology/flows.
//
// Outputs mirror the entity-level surfaces:
//
//   - cycles      ↔ grafel_quality_cycles (entity-level IMPORTS SCC)
//   - centrality  ↔ inline PageRank/centrality in grafel_stats
//   - all         returns both in one envelope (default; useful for the
//     dashboard / docgen consumer).
package mcp

import (
	"context"
	"sort"

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// moduleAnalysisResult is the shared per-repo computation cache for both
// tools. The MCP layer caches per request; the graph algorithm package
// (RunModuleAlgorithms) is pure and fast on real corpora (≤ thousands of
// module nodes), so we recompute rather than memoise across requests — this
// keeps the surface trivially correct under daemon reload.
type moduleAnalysisResult struct {
	Repo string
	Res  *graph.ModuleAlgorithmResults
}

// computeModuleAnalysis runs RunModuleAlgorithms over every repo in `repos`
// and returns the per-repo results. Empty repos (no graph loaded) are skipped.
func computeModuleAnalysis(repos []*LoadedRepo) []moduleAnalysisResult {
	out := make([]moduleAnalysisResult, 0, len(repos))
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		// #5870 PR7a: Reader-served materialize seam (transient copy) instead of
		// the raw lr.Doc.* slices — heavy/rare pass, transient copy acceptable.
		res := graph.RunModuleAlgorithms(materializeAllEntities(r), materializeAllRelationships(r))
		out = append(out, moduleAnalysisResult{Repo: r.Repo, Res: res})
	}
	return out
}

// handleModuleAnalysis dispatches on action=cycles|centrality|all.
func (s *Server) handleModuleAnalysis(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	action := argString(req, "action", "all")
	switch action {
	case "cycles":
		return s.handleModuleCycles(ctx, req)
	case "centrality":
		return s.handleModuleCentrality(ctx, req)
	case "all", "":
		return s.handleModuleCombined(ctx, req)
	}
	return jsonResult(map[string]any{
		"error":  "unknown action",
		"action": action,
		"valid":  []string{"cycles", "centrality", "all"},
	}), nil
}

// handleModuleCombined returns both cycles and centrality in one envelope.
func (s *Server) handleModuleCombined(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	topN := argInt(req, "top_n", 10)
	if topN <= 0 {
		topN = 10
	}
	limit := argInt(req, "limit", 50)

	type centOut struct {
		Repo        string  `json:"repo"`
		ModuleID    string  `json:"module_id"`
		ModuleName  string  `json:"module_name"`
		PageRank    float64 `json:"pagerank"`
		Betweenness float64 `json:"betweenness"`
		InDegree    int     `json:"in_degree"`
		OutDegree   int     `json:"out_degree"`
		InCycle     bool    `json:"in_cycle"`
	}
	type sccOut struct {
		Repo        string             `json:"repo"`
		ID          int                `json:"id"`
		Size        int                `json:"size"`
		Members     []string           `json:"members"`
		MemberNames []string           `json:"member_names"`
		Edges       []graph.ModuleEdge `json:"edges"`
	}
	type repoSummary struct {
		Repo           string `json:"repo"`
		NumModules     int    `json:"num_modules"`
		NumModuleEdges int    `json:"num_module_edges"`
		NumSCCs        int    `json:"num_sccs"`
		LargestSCCSize int    `json:"largest_scc_size"`
		ModulesInCycle int    `json:"modules_in_cycle"`
	}

	var sccs []sccOut
	var topPR []centOut
	var topBet []centOut
	var summaries []repoSummary

	for _, ra := range computeModuleAnalysis(repos) {
		summaries = append(summaries, repoSummary{
			Repo:           ra.Repo,
			NumModules:     ra.Res.Stats.NumModules,
			NumModuleEdges: ra.Res.Stats.NumModuleEdges,
			NumSCCs:        ra.Res.Stats.NumSCCs,
			LargestSCCSize: ra.Res.Stats.LargestSCCSize,
			ModulesInCycle: ra.Res.Stats.NumModulesInCycle,
		})
		for _, c := range ra.Res.SCCs {
			members := make([]string, len(c.Members))
			for i, m := range c.Members {
				members[i] = prefixedID(ra.Repo, m)
			}
			edges := make([]graph.ModuleEdge, len(c.Edges))
			for i, e := range c.Edges {
				edges[i] = graph.ModuleEdge{
					FromModule: prefixedID(ra.Repo, e.FromModule),
					ToModule:   prefixedID(ra.Repo, e.ToModule),
					Weight:     e.Weight,
				}
			}
			sccs = append(sccs, sccOut{
				Repo:        ra.Repo,
				ID:          c.ID,
				Size:        c.Size,
				Members:     members,
				MemberNames: append([]string{}, c.MemberNames...),
				Edges:       edges,
			})
		}
		toCent := func(c graph.ModuleCentrality) centOut {
			return centOut{
				Repo:        ra.Repo,
				ModuleID:    prefixedID(ra.Repo, c.ModuleID),
				ModuleName:  c.ModuleName,
				PageRank:    c.PageRank,
				Betweenness: c.Betweenness,
				InDegree:    c.InDegree,
				OutDegree:   c.OutDegree,
				InCycle:     ra.Res.SCCOf[c.ModuleID] >= 0,
			}
		}
		for _, c := range graph.TopByPageRank(ra.Res.Centrality, topN) {
			topPR = append(topPR, toCent(c))
		}
		for _, c := range graph.TopByBetweenness(ra.Res.Centrality, topN) {
			topBet = append(topBet, toCent(c))
		}
	}

	sort.SliceStable(sccs, func(i, j int) bool {
		if sccs[i].Size != sccs[j].Size {
			return sccs[i].Size > sccs[j].Size
		}
		return sccs[i].Repo < sccs[j].Repo
	})
	if limit > 0 && len(sccs) > limit {
		sccs = sccs[:limit]
	}
	sort.SliceStable(topPR, func(i, j int) bool {
		if topPR[i].PageRank != topPR[j].PageRank {
			return topPR[i].PageRank > topPR[j].PageRank
		}
		return topPR[i].ModuleID < topPR[j].ModuleID
	})
	sort.SliceStable(topBet, func(i, j int) bool {
		if topBet[i].Betweenness != topBet[j].Betweenness {
			return topBet[i].Betweenness > topBet[j].Betweenness
		}
		return topBet[i].ModuleID < topBet[j].ModuleID
	})
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Repo < summaries[j].Repo })

	return jsonResult(map[string]any{
		"sccs":            sccs,
		"top_pagerank":    topPR,
		"top_betweenness": topBet,
		"summaries":       summaries,
	}), nil
}

// handleModuleCycles is the cycles-only response. Returns every module-level
// SCC of size >= min_size (default 2) in the resolved group.
func (s *Server) handleModuleCycles(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	limit := argInt(req, "limit", 50)
	minSize := argInt(req, "min_size", 2)
	if minSize < 2 {
		minSize = 2
	}

	type memberOut struct {
		ModuleID   string `json:"module_id"`
		ModuleName string `json:"module_name"`
	}
	type cycleOut struct {
		Repo        string             `json:"repo"`
		Size        int                `json:"size"`
		Members     []memberOut        `json:"members"`
		Edges       []graph.ModuleEdge `json:"edges"`
		MemberNames []string           `json:"member_names"`
	}

	var all []cycleOut
	totalRepos := 0
	totalSCCs := 0

	for _, ra := range computeModuleAnalysis(repos) {
		totalRepos++
		for _, c := range ra.Res.SCCs {
			if c.Size < minSize {
				continue
			}
			totalSCCs++
			members := make([]memberOut, 0, len(c.Members))
			for i, mid := range c.Members {
				name := mid
				if i < len(c.MemberNames) {
					name = c.MemberNames[i]
				}
				members = append(members, memberOut{
					ModuleID:   prefixedID(ra.Repo, mid),
					ModuleName: name,
				})
			}
			// Prefix edge IDs too — module IDs are scoped per repo.
			edges := make([]graph.ModuleEdge, len(c.Edges))
			for i, e := range c.Edges {
				edges[i] = graph.ModuleEdge{
					FromModule: prefixedID(ra.Repo, e.FromModule),
					ToModule:   prefixedID(ra.Repo, e.ToModule),
					Weight:     e.Weight,
				}
			}
			all = append(all, cycleOut{
				Repo:        ra.Repo,
				Size:        c.Size,
				Members:     members,
				Edges:       edges,
				MemberNames: append([]string{}, c.MemberNames...),
			})
		}
	}

	// Sort: descending size, ascending repo, ascending first member name.
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Size != all[j].Size {
			return all[i].Size > all[j].Size
		}
		if all[i].Repo != all[j].Repo {
			return all[i].Repo < all[j].Repo
		}
		mi := ""
		if len(all[i].Members) > 0 {
			mi = all[i].Members[0].ModuleName
		}
		mj := ""
		if len(all[j].Members) > 0 {
			mj = all[j].Members[0].ModuleName
		}
		return mi < mj
	})
	total := len(all)
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return jsonResult(map[string]any{
		"cycles":        all,
		"count":         len(all),
		"total":         total,
		"truncated":     total > len(all),
		"repos_scanned": totalRepos,
	}), nil
}

// handleModuleCentrality is the MCP handler for grafel_module_centrality.
// Returns top-N modules by PageRank and top-N by betweenness, per repo.
func (s *Server) handleModuleCentrality(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	topN := argInt(req, "top_n", 10)
	if topN <= 0 {
		topN = 10
	}

	type centOut struct {
		Repo        string  `json:"repo"`
		ModuleID    string  `json:"module_id"`
		ModuleName  string  `json:"module_name"`
		PageRank    float64 `json:"pagerank"`
		Betweenness float64 `json:"betweenness"`
		InDegree    int     `json:"in_degree"`
		OutDegree   int     `json:"out_degree"`
		InCycle     bool    `json:"in_cycle"`
	}
	type repoOut struct {
		Repo           string    `json:"repo"`
		NumModules     int       `json:"num_modules"`
		NumModuleEdges int       `json:"num_module_edges"`
		NumSCCs        int       `json:"num_sccs"`
		LargestSCCSize int       `json:"largest_scc_size"`
		ModulesInCycle int       `json:"modules_in_cycle"`
		TopPageRank    []centOut `json:"top_pagerank"`
		TopBetweenness []centOut `json:"top_betweenness"`
	}

	out := make([]repoOut, 0, len(repos))

	for _, ra := range computeModuleAnalysis(repos) {
		toCent := func(c graph.ModuleCentrality) centOut {
			return centOut{
				Repo:        ra.Repo,
				ModuleID:    prefixedID(ra.Repo, c.ModuleID),
				ModuleName:  c.ModuleName,
				PageRank:    c.PageRank,
				Betweenness: c.Betweenness,
				InDegree:    c.InDegree,
				OutDegree:   c.OutDegree,
				InCycle:     ra.Res.SCCOf[c.ModuleID] >= 0,
			}
		}
		topPR := graph.TopByPageRank(ra.Res.Centrality, topN)
		topBet := graph.TopByBetweenness(ra.Res.Centrality, topN)
		ro := repoOut{
			Repo:           ra.Repo,
			NumModules:     ra.Res.Stats.NumModules,
			NumModuleEdges: ra.Res.Stats.NumModuleEdges,
			NumSCCs:        ra.Res.Stats.NumSCCs,
			LargestSCCSize: ra.Res.Stats.LargestSCCSize,
			ModulesInCycle: ra.Res.Stats.NumModulesInCycle,
			TopPageRank:    make([]centOut, 0, len(topPR)),
			TopBetweenness: make([]centOut, 0, len(topBet)),
		}
		for _, c := range topPR {
			ro.TopPageRank = append(ro.TopPageRank, toCent(c))
		}
		for _, c := range topBet {
			ro.TopBetweenness = append(ro.TopBetweenness, toCent(c))
		}
		out = append(out, ro)
	}

	// Sort repos alphabetically for stable output.
	sort.Slice(out, func(i, j int) bool { return out[i].Repo < out[j].Repo })

	return jsonResult(map[string]any{
		"repos": out,
		"count": len(out),
	}), nil
}
