package dashboard

// handlers_graph.go — graph endpoints
//
//	GET /api/graph/{group}?filter_kind=&filter_repo=&repos=slug1,slug2&include_external=false
//	GET /api/graph/{group}/entity/{id}
//
// #1023: LoD tiers removed. The endpoint returns all entities — no per-repo
// node cap. Cosmograph handles 1M+ nodes via GPU WebGL at 60fps; no sampling
// or LoD switching is needed.
//
// Removed functions: serveGraphCentroids, serveGraphMid, serveGraphFull.
// Removed: ?lod= param, COMMUNITY_LINK synthetic edges, centroid nodes,
//          denseNodeLimit (was 500/repo — legacy react-force-graph artifact).
//
// If the response exceeds 50,000 nodes, an X-Graph-Warning header is added so
// the frontend can optionally surface a notice to the user.
//
// "repos" param accepts comma-separated repo slugs for multi-select filtering.
//
// "include_external" (default "false") controls whether entities with kind
// "SCOPE.External" (stdlib/builtin placeholders) are included in the response.
// When false, those entities and any edges referencing only external nodes are
// excluded. Pass "include_external=true" to opt back in.

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// softNodeWarnThreshold is the node count above which the API adds an
// X-Graph-Warning response header so the frontend can surface a notice.
const softNodeWarnThreshold = 50_000

// buildDegreeMap returns a map from entity ID to total degree (in + out) for
// all relationships in a repo.  Used by the dense/mid samplers to rank nodes
// by connectivity rather than PageRank alone (#1020).
func buildDegreeMap(rels []graph.Relationship) map[string]int {
	deg := make(map[string]int, len(rels)*2)
	for _, r := range rels {
		deg[r.FromID]++
		deg[r.ToID]++
	}
	return deg
}

// handleGraph — GET /api/graph/{group}
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	filterKind := r.URL.Query().Get("filter_kind")
	filterRepo := r.URL.Query().Get("filter_repo")
	reposParam := r.URL.Query().Get("repos") // comma-separated list of repo slugs

	// include_external defaults to false: External stdlib/builtin placeholder
	// entities are excluded unless the caller explicitly opts in.
	includeExternal := r.URL.Query().Get("include_external") == "true"

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	repos := sortedRepos(grp)

	// Single-repo legacy filter
	if filterRepo != "" {
		var filtered []*DashRepo
		for _, r := range repos {
			if r.Slug == filterRepo {
				filtered = append(filtered, r)
			}
		}
		repos = filtered
	}

	// Multi-repo filter — ?repos=slug1,slug2
	if reposParam != "" {
		slugSet := map[string]bool{}
		for _, s := range strings.Split(reposParam, ",") {
			slugSet[strings.TrimSpace(s)] = true
		}
		var filtered []*DashRepo
		for _, r := range repos {
			if slugSet[r.Slug] {
				filtered = append(filtered, r)
			}
		}
		repos = filtered
	}

	s.serveGraphDense(w, group, repos, filterKind, includeExternal)
}

// externalKindSuffix is the trailing portion of the SCOPE.External kind after
// dashStripScopePrefix strips the leading "SCOPE." prefix.
const externalKindSuffix = "External"

// serveGraphAll returns every entity in the indexed graph — no per-repo cap.
// Cosmograph handles 1M+ nodes at 60fps via GPU WebGL (#1023 removed LoD).
// A soft X-Graph-Warning header is added when node count exceeds
// softNodeWarnThreshold so the frontend can optionally surface a notice.
//
// includeExternal controls whether SCOPE.External placeholder entities are
// emitted. Default (false) hides stdlib/builtin nodes from the graph view.
func (s *Server) serveGraphDense(w http.ResponseWriter, group string, repos []*DashRepo, filterKind string, includeExternal bool) {
	nodes := []map[string]any{}
	edges := []map[string]any{}
	communities := []map[string]any{}
	visible := map[string]bool{}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for _, c := range r.Doc.Communities {
			top := c.TopEntities
			if len(top) > 3 {
				top = top[:3]
			}
			prefixed := make([]string, len(top))
			for i, id := range top {
				prefixed[i] = dashPrefixedID(r.Slug, id)
			}
			cm := map[string]any{
				"id":           c.ID,
				"size":         c.Size,
				"auto_name":    c.AutoName,
				"repo":         r.Slug,
				"top_entities": prefixed,
			}
			if c.AgentName != "" {
				cm["agent_name"] = c.AgentName
			}
			communities = append(communities, cm)
		}

		// Build per-repo degree map (total in + out edges) for node sizing.
		degreeMap := buildDegreeMap(r.Doc.Relationships)

		// Emit all entities — no cap. Filter by kind when requested.
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			// Filter external stdlib/builtin placeholders unless opted in.
			if !includeExternal && dashStripScopePrefix(e.Kind) == externalKindSuffix {
				continue
			}
			if filterKind != "" && dashStripScopePrefix(e.Kind) != filterKind {
				continue
			}
			pid := dashPrefixedID(r.Slug, e.ID)
			if visible[pid] {
				continue
			}
			visible[pid] = true
			node := serializeEntity(r.Slug, e)
			node["degree"] = degreeMap[e.ID]
			nodes = append(nodes, node)
		}
	}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for _, rel := range r.Doc.Relationships {
			from := dashPrefixedID(r.Slug, rel.FromID)
			to := dashPrefixedID(r.Slug, rel.ToID)
			if visible[from] && visible[to] {
				edges = append(edges, map[string]any{
					"from_id": from,
					"to_id":   to,
					"kind":    rel.Kind,
				})
			}
		}
	}

	if len(nodes) > softNodeWarnThreshold {
		w.Header().Set("X-Graph-Warning", "large-graph: node count exceeds 50k; consider filtering by repo or kind")
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":            nodes,
		"edges":            edges,
		"communities":      communities,
		"total_node_count": len(nodes),
	})
}

// handleGraphEntity — GET /api/graph/{group}/entity/{id}
func (s *Server) handleGraphEntity(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	id := r.PathValue("id")
	if group == "" || id == "" {
		writeErr(w, http.StatusBadRequest, "group and id required")
		return
	}

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	repo, entity := findEntity(grp, id)
	if entity == nil {
		writeErr(w, http.StatusNotFound, "entity not found: "+id)
		return
	}

	// Collect inbound and outbound edges for this entity.
	localID := entity.ID
	inbound := []map[string]any{}
	outbound := []map[string]any{}
	neighborIDs := map[string]bool{}

	for _, rel := range repo.Doc.Relationships {
		if rel.FromID == localID {
			to := dashPrefixedID(repo.Slug, rel.ToID)
			outbound = append(outbound, map[string]any{
				"from_id": dashPrefixedID(repo.Slug, rel.FromID),
				"to_id":   to,
				"kind":    rel.Kind,
			})
			neighborIDs[rel.ToID] = true
		}
		if rel.ToID == localID {
			from := dashPrefixedID(repo.Slug, rel.FromID)
			inbound = append(inbound, map[string]any{
				"from_id": from,
				"to_id":   dashPrefixedID(repo.Slug, rel.ToID),
				"kind":    rel.Kind,
			})
			neighborIDs[rel.FromID] = true
		}
	}

	// Collect cross-repo edges involving this entity.
	pid := dashPrefixedID(repo.Slug, localID)
	for _, l := range grp.Links {
		if l.Source == pid {
			outbound = append(outbound, map[string]any{
				"from_id":    pid,
				"to_id":      l.Target,
				"kind":       l.Kind,
				"cross_repo": true,
			})
		}
		if l.Target == pid {
			inbound = append(inbound, map[string]any{
				"from_id":    l.Source,
				"to_id":      pid,
				"kind":       l.Kind,
				"cross_repo": true,
			})
		}
	}

	// Resolve neighbor entities (depth-1, same repo).
	neighbors := []map[string]any{}
	for nid := range neighborIDs {
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			if e.ID == nid {
				neighbors = append(neighbors, map[string]any{
					"id":          dashPrefixedID(repo.Slug, e.ID),
					"label":       e.Name,
					"kind":        dashStripScopePrefix(e.Kind),
					"source_file": e.SourceFile,
					"start_line":  e.StartLine,
					"repo":        repo.Slug,
				})
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entity":         serializeEntity(repo.Slug, entity),
		"inbound_edges":  inbound,
		"outbound_edges": outbound,
		"neighbors":      neighbors,
	})
}

// handleGroupCommunities — GET /api/groups/{group}/communities
func (s *Server) handleGroupCommunities(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	out := []map[string]any{}
	for _, r := range sortedRepos(grp) {
		for _, c := range r.Doc.Communities {
			top := c.TopEntities
			if len(top) > 3 {
				top = top[:3]
			}
			prefixed := make([]string, len(top))
			for i, id := range top {
				prefixed[i] = dashPrefixedID(r.Slug, id)
			}
			cm := map[string]any{
				"repo":         r.Slug,
				"id":           c.ID,
				"size":         c.Size,
				"modularity":   c.Modularity,
				"auto_name":    c.AutoName,
				"top_entities": prefixed,
			}
			if c.AgentName != "" {
				cm["agent_name"] = c.AgentName
			}
			out = append(out, cm)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"communities": out})
}

// handleGroupGodNodes — GET /api/groups/{group}/god-nodes
func (s *Server) handleGroupGodNodes(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	type godNode struct {
		ID       string  `json:"id"`
		Label    string  `json:"label"`
		Kind     string  `json:"kind"`
		Repo     string  `json:"repo"`
		PageRank float64 `json:"pagerank"`
	}
	var nodes []godNode
	for _, r := range sortedRepos(grp) {
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !e.IsGodNode {
				continue
			}
			pr := 0.0
			if e.PageRank != nil {
				pr = *e.PageRank
			}
			nodes = append(nodes, godNode{
				ID:       dashPrefixedID(r.Slug, e.ID),
				Label:    e.Name,
				Kind:     dashStripScopePrefix(e.Kind),
				Repo:     r.Slug,
				PageRank: pr,
			})
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].PageRank > nodes[j].PageRank })
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"god_nodes": nodes})
}

// handleGroupLinks — GET /api/groups/{group}/links
func (s *Server) handleGroupLinks(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	links := grp.Links
	if links == nil {
		links = []CrossRepoLink{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}
