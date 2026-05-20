package dashboard

// handlers_graph.go — LoD-aware graph endpoints
//
//	GET /api/graph/{group}?lod=centroids|mid|dense|full&filter_kind=&filter_repo=&repos=slug1,slug2
//	GET /api/graph/{group}/entity/{id}
//
// The LoD tiers:
//   - centroids : one centroid object per community (~50–200 nodes)
//   - mid       : centroids + top-50 god-nodes per community (~150 nodes)
//   - dense     : top-500 by pagerank per repo — default tier (issue #1000)
//   - full      : all nodes up to 20 000 hard cap
//
// Default lod is "dense" (issue #1000: user wants to SEE the graph).
// "repos" param accepts comma-separated repo slugs for multi-select filtering.

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

const fullNodeCap = 20_000
const denseNodeLimit = 500 // per-repo limit for the "dense" tier

// handleGraph — GET /api/graph/{group}
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	lod := r.URL.Query().Get("lod")
	if lod == "" {
		lod = "dense" // #1000: default to dense tier so the graph feels populated
	}
	filterKind := r.URL.Query().Get("filter_kind")
	filterRepo := r.URL.Query().Get("filter_repo")
	reposParam := r.URL.Query().Get("repos") // comma-separated list of repo slugs (#1000)

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

	// Multi-repo filter — ?repos=slug1,slug2 (#1000)
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

	switch lod {
	case "centroids":
		s.serveGraphCentroids(w, group, repos)
	case "mid":
		s.serveGraphMid(w, group, repos, filterKind)
	case "dense":
		s.serveGraphDense(w, group, repos, filterKind)
	default: // "full"
		s.serveGraphFull(w, group, repos, filterKind)
	}
}

// serveGraphCentroids returns one centroid per community (zoom-out tier).
//
// Each centroid is emitted as a GraphNode-shaped object (id, label, kind,
// repo, is_centroid=true, centroid_size) so the force-graph renderer can
// draw it without additional client-side normalization.
//
// Inter-community edges are derived by counting how many relationships cross
// community boundaries; edges with weight ≥ 1 are emitted so the layout
// engine has structure to work with (without edges the force simulation
// produces a uniform blob with no visible separation).
func (s *Server) serveGraphCentroids(w http.ResponseWriter, group string, repos []*DashRepo) {
	nodes := []map[string]any{}
	communities := []map[string]any{}

	// centroidID builds a stable, unique node ID for a community centroid.
	centroidID := func(repoSlug string, communityID int) string {
		return fmt.Sprintf("%s::community::%d", repoSlug, communityID)
	}

	// Build entity→communityID lookup for edge derivation.
	type entityKey struct{ repo, id string }
	entityCommunity := map[entityKey]int{}

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

			name := c.AutoName
			if c.AgentName != "" {
				name = c.AgentName
			}
			if name == "" {
				name = fmt.Sprintf("Community %d", c.ID)
			}

			nid := centroidID(r.Slug, c.ID)
			node := map[string]any{
				"id":            nid,
				"label":         name,
				"kind":          "Community",
				"repo":          r.Slug,
				"is_centroid":   true,
				"centroid_size": c.Size,
				"community_id":  c.ID,
				"top_entity_ids": prefixed,
			}
			nodes = append(nodes, node)

			cm := map[string]any{
				"id":           c.ID,
				"size":         c.Size,
				"auto_name":    c.AutoName,
				"repo":         r.Slug,
				"top_entities": prefixed,
				"centroid_node_id": nid,
			}
			if c.AgentName != "" {
				cm["agent_name"] = c.AgentName
			}
			communities = append(communities, cm)

			// Populate entity→community lookup for all members.
			for i := range r.Doc.Entities {
				e := &r.Doc.Entities[i]
				if e.CommunityID != nil && *e.CommunityID == c.ID {
					entityCommunity[entityKey{r.Slug, e.ID}] = c.ID
				}
			}
		}
	}

	// Derive inter-community edges: count cross-boundary relationships.
	type edgeKey struct {
		fromRepo string
		fromCID  int
		toRepo   string
		toCID    int
	}
	edgeWeights := map[edgeKey]int{}
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for _, rel := range r.Doc.Relationships {
			fromCID, okFrom := entityCommunity[entityKey{r.Slug, rel.FromID}]
			toCID, okTo := entityCommunity[entityKey{r.Slug, rel.ToID}]
			if !okFrom || !okTo {
				continue
			}
			if fromCID == toCID {
				continue // intra-community — skip
			}
			// Normalise direction so A→B and B→A collapse to one key.
			k := edgeKey{r.Slug, fromCID, r.Slug, toCID}
			if fromCID > toCID {
				k = edgeKey{r.Slug, toCID, r.Slug, fromCID}
			}
			edgeWeights[k]++
		}
	}

	edges := []map[string]any{}
	for k, weight := range edgeWeights {
		fromID := centroidID(k.fromRepo, k.fromCID)
		toID := centroidID(k.toRepo, k.toCID)
		edges = append(edges, map[string]any{
			"from_id": fromID,
			"to_id":   toID,
			"kind":    "COMMUNITY_LINK",
			"weight":  weight,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":       nodes,
		"edges":       edges,
		"communities": communities,
		"lod_level":   "zoom-out",
		"total_nodes": len(nodes),
	})
}

// serveGraphMid returns centroids + top god-nodes (mid-zoom tier).
func (s *Server) serveGraphMid(w http.ResponseWriter, group string, repos []*DashRepo, filterKind string) {
	nodes := []map[string]any{}
	edges := []map[string]any{}
	communities := []map[string]any{}
	visible := map[string]bool{}

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		// Add community centroids.
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

		// Collect god-nodes: top-50 by PageRank per repo (or centrality).
		type scored struct {
			e  *graph.Entity
			pr float64
		}
		var godCandidates []scored
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if filterKind != "" && dashStripScopePrefix(e.Kind) != filterKind {
				continue
			}
			pr := 0.0
			if e.PageRank != nil {
				pr = *e.PageRank
			}
			if e.IsGodNode || pr > 0 {
				godCandidates = append(godCandidates, scored{e: e, pr: pr})
			}
		}
		sort.Slice(godCandidates, func(i, j int) bool {
			return godCandidates[i].pr > godCandidates[j].pr
		})
		limit := 50
		if len(godCandidates) > limit {
			godCandidates = godCandidates[:limit]
		}
		for _, sc := range godCandidates {
			pid := dashPrefixedID(r.Slug, sc.e.ID)
			if visible[pid] {
				continue
			}
			visible[pid] = true
			nodes = append(nodes, serializeEntity(r.Slug, sc.e))
		}
	}

	// Include edges where both endpoints are visible.
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

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":       nodes,
		"edges":       edges,
		"communities": communities,
		"lod_level":   "mid",
		"total_nodes": len(nodes),
	})
}

// serveGraphDense returns top-N nodes by PageRank per repo — the default tier (#1000).
// Gives a much richer view than "mid" (50/repo) while staying bounded below full.
func (s *Server) serveGraphDense(w http.ResponseWriter, group string, repos []*DashRepo, filterKind string) {
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

		type scored struct {
			e  *graph.Entity
			pr float64
		}
		var candidates []scored
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if filterKind != "" && dashStripScopePrefix(e.Kind) != filterKind {
				continue
			}
			pr := 0.0
			if e.PageRank != nil {
				pr = *e.PageRank
			}
			candidates = append(candidates, scored{e: e, pr: pr})
		}
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].pr > candidates[j].pr
		})
		limit := denseNodeLimit
		if len(candidates) < limit {
			limit = len(candidates)
		}
		for _, sc := range candidates[:limit] {
			pid := dashPrefixedID(r.Slug, sc.e.ID)
			if visible[pid] {
				continue
			}
			visible[pid] = true
			nodes = append(nodes, serializeEntity(r.Slug, sc.e))
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

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":       nodes,
		"edges":       edges,
		"communities": communities,
		"lod_level":   "zoom-in",
		"total_nodes": len(nodes),
	})
}

// serveGraphFull returns all nodes up to the hard cap.
func (s *Server) serveGraphFull(w http.ResponseWriter, group string, repos []*DashRepo, filterKind string) {
	nodes := []map[string]any{}
	edges := []map[string]any{}
	communities := []map[string]any{}
	visible := map[string]bool{}

	totalEntities := 0
	for _, r := range repos {
		if r.Doc != nil {
			totalEntities += len(r.Doc.Entities)
		}
	}

	// Hard cap: if unfiltered count > 20k, return empty with "blocked" signal.
	if filterKind == "" && totalEntities > fullNodeCap {
		writeJSON(w, http.StatusOK, map[string]any{
			"nodes":       []any{},
			"edges":       []any{},
			"communities": []any{},
			"lod_level":   "blocked",
			"total_nodes": totalEntities,
		})
		return
	}

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
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if filterKind != "" && dashStripScopePrefix(e.Kind) != filterKind {
				continue
			}
			pid := dashPrefixedID(r.Slug, e.ID)
			visible[pid] = true
			nodes = append(nodes, serializeEntity(r.Slug, e))
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

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":       nodes,
		"edges":       edges,
		"communities": communities,
		"lod_level":   "full",
		"total_nodes": len(nodes),
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
