// cycles_tools.go — MCP handler for archigraph_quality_cycles (#1312).
//
// Exposes import-cycle detection via Tarjan SCC over IMPORTS edges. Each
// reported cycle includes the participating files/entities, the weakest-link
// edge (lowest-PageRank source — easiest to sever), and a suggested extraction
// target (highest-PageRank member — best candidate for a shared module).
package mcp

import (
	"context"
	"sort"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// handleQualityCycles is the MCP handler for archigraph_quality_cycles.
// It accepts an optional repo_filter and optional limit, runs Tarjan SCC
// over the IMPORTS sub-graph of each loaded repo, and returns all detected
// cycles sorted by descending size.
func (s *Server) handleQualityCycles(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))
	limit := argInt(req, "limit", 100)
	repoFilter := argStringSlice(req, "repo_filter")

	type memberDetail struct {
		EntityID   string  `json:"entity_id"`
		EntityName string  `json:"name"`
		Kind       string  `json:"kind"`
		SourceFile string  `json:"source_file,omitempty"`
		PageRank   float64 `json:"pagerank,omitempty"`
	}

	type cycleOut struct {
		Repo                    string            `json:"repo"`
		Size                    int               `json:"size"`
		Members                 []memberDetail    `json:"members"`
		Edges                   []graph.CycleEdge `json:"edges"`
		WeakestLinkFromID       string            `json:"weakest_link_from_id"`
		WeakestLinkFromName     string            `json:"weakest_link_from_name"`
		WeakestLinkToID         string            `json:"weakest_link_to_id"`
		WeakestLinkToName       string            `json:"weakest_link_to_name"`
		SuggestedExtractionID   string            `json:"suggested_extraction_id"`
		SuggestedExtractionName string            `json:"suggested_extraction_name"`
	}

	var all []cycleOut

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}

		// Build PageRank map from entity attributes (Pass 4 output).
		pr := buildPageRankMap(r.Doc)

		cycles := graph.FindImportCycles(r.Doc.Entities, r.Doc.Relationships, pr)
		if len(cycles) == 0 {
			continue
		}

		// Index entities by ID for name lookup.
		byID := r.getByID()

		for _, c := range cycles {
			members := make([]memberDetail, 0, len(c.Members))
			for _, id := range c.Members {
				e := byID[id]
				if e == nil {
					members = append(members, memberDetail{
						EntityID: prefixedID(r.Repo, id),
					})
					continue
				}
				mem := memberDetail{
					EntityID:   prefixedID(r.Repo, e.ID),
					EntityName: e.Name,
					Kind:       e.Kind,
					SourceFile: e.SourceFile,
				}
				if e.PageRank != nil {
					mem.PageRank = *e.PageRank
				}
				members = append(members, mem)
			}

			// Prefix edge IDs.
			edges := make([]graph.CycleEdge, len(c.Edges))
			for i, edge := range c.Edges {
				edges[i] = graph.CycleEdge{
					FromID: prefixedID(r.Repo, edge.FromID),
					ToID:   prefixedID(r.Repo, edge.ToID),
				}
			}

			weakFromName := entityName(byID, c.WeakestLinkFromID)
			weakToName := entityName(byID, c.WeakestLinkToID)
			extractName := entityName(byID, c.SuggestedExtractionID)

			all = append(all, cycleOut{
				Repo:                    r.Repo,
				Size:                    c.Size,
				Members:                 members,
				Edges:                   edges,
				WeakestLinkFromID:       prefixedID(r.Repo, c.WeakestLinkFromID),
				WeakestLinkFromName:     weakFromName,
				WeakestLinkToID:         prefixedID(r.Repo, c.WeakestLinkToID),
				WeakestLinkToName:       weakToName,
				SuggestedExtractionID:   prefixedID(r.Repo, c.SuggestedExtractionID),
				SuggestedExtractionName: extractName,
			})
		}
	}

	// Sort: descending cycle size, then repo, then first member name.
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Size != all[j].Size {
			return all[i].Size > all[j].Size
		}
		if all[i].Repo != all[j].Repo {
			return all[i].Repo < all[j].Repo
		}
		mi := ""
		if len(all[i].Members) > 0 {
			mi = all[i].Members[0].EntityName
		}
		mj := ""
		if len(all[j].Members) > 0 {
			mj = all[j].Members[0].EntityName
		}
		return mi < mj
	})

	total := len(all)
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}

	// ── Service-level SCC detection (#1502) ──────────────────────────────────
	// Per-repo IMPORTS cycles above cannot see cross-service coupling that flows
	// through HTTP-endpoint / topic synthetic nodes. Aggregate the group's
	// resolved cross-repo links into a directed service graph and run Tarjan SCC
	// over it so e.g. orders↔payments (REST one way, Kafka the other) surfaces.
	serviceCycles := buildServiceCycles(lg, repoFilter)

	return jsonResult(map[string]any{
		"cycles":         all,
		"count":          len(all),
		"total":          total,
		"truncated":      total > len(all),
		"service_cycles": serviceCycles,
		"service_count":  len(serviceCycles),
	}), nil
}

// buildServiceCycles aggregates a group's cross-repo links into a directed
// service graph and returns every strongly-connected component of size >= 2.
//
// Only directed-dependency relations (calls / imports / publishes_to) feed the
// graph; undirected co-occurrence relations (shared_label, string_match) are
// excluded so they cannot manufacture spurious cycles. When repoFilter is set,
// the graph is restricted to links whose BOTH endpoints are in the filter.
func buildServiceCycles(lg *LoadedGroup, repoFilter []string) []graph.ServiceCycle {
	if lg == nil || len(lg.Links) == 0 {
		return nil
	}
	keep := map[string]bool{}
	for _, r := range repoFilter {
		keep[r] = true
	}
	inFilter := func(repo string) bool {
		if len(keep) == 0 {
			return true
		}
		return keep[repo]
	}

	links := make([]graph.ServiceLink, 0, len(lg.Links))
	for _, l := range lg.Links {
		rel := l.EffectiveKind()
		if !graph.IsDirectedServiceRelation(rel) {
			continue
		}
		fromRepo, _ := splitPrefixed(l.Source)
		toRepo, _ := splitPrefixed(l.Target)
		if fromRepo == "" || toRepo == "" {
			continue
		}
		if !inFilter(fromRepo) || !inFilter(toRepo) {
			continue
		}
		links = append(links, graph.ServiceLink{
			FromService: fromRepo,
			ToService:   toRepo,
			Relation:    rel,
		})
	}
	return graph.FindServiceCycles(links)
}

// buildPageRankMap extracts per-entity PageRank scores from a loaded Document.
// Entities without a PageRank attribute (pre-Pass-4 graphs) return an empty map.
func buildPageRankMap(doc *graph.Document) map[string]float64 {
	pr := make(map[string]float64, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.PageRank != nil {
			pr[e.ID] = *e.PageRank
		}
	}
	return pr
}

// entityName returns the Name of the entity with the given local ID, or ""
// when not found.
func entityName(byID map[string]*graph.Entity, id string) string {
	if e := byID[id]; e != nil {
		return e.Name
	}
	return ""
}
