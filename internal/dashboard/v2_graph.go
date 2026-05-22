// v2_graph.go — GET /api/v2/graph/{group}
//
// The graph payload for WebUI v2's hero surface. It returns the full
// dependency graph (nodes + edges + communities + repos) wrapped in the
// standard v2 envelope (see v2_helpers.go / API_V2.md):
//
//	{ "ok": true, "data": { nodes, edges, communities, repos, total_node_count } }
//
// This reuses the proven v1 dense-graph build logic (handlers_graph.go) but
// emits a v2-shaped payload that carries the fields the cosmos.gl WebGL canvas
// needs that the v1 tier-1 payload omitted:
//
//   - pagerank      (drives log-scaled node radius)
//   - source_file   (drives the "module" group-by dimension on the client)
//   - communities[] (id + colorIndex + size + label) for the legend / focus
//   - repos[]       (slug + language) for the repo filter + per-repo islands
//
// Like v1, it honours ?repos=, ?filter_kind=, ?include_external=, ?view=modules
// and reuses the server-side payload cache + strong ETag + 304 path. gzip is
// applied at the mux level (server.go withGzip), so large payloads compress
// transparently for clients sending Accept-Encoding: gzip.
//
// Latitude decision (documented per the ticket): a NEW /api/v2/graph endpoint
// is added rather than reusing the v1 /api/graph one. Rationale — the v1 tier-1
// payload deliberately omits pagerank + source_file to keep its wire shape
// tight, but the cosmos.gl renderer needs both for node sizing and the
// module group-by. Bolting those onto v1 would change its contract and risk
// the legacy dashboard; a clean v2 endpoint keeps both UIs independent (the
// ARCHITECTURE.md hard rule "do not touch dashboard/").

package dashboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/module"
)

// ── v2 wire structs ──────────────────────────────────────────────────────────

// v2GraphNode is the per-node shape consumed by the cosmos.gl canvas.
type v2GraphNode struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Kind        string  `json:"kind"`
	Repo        string  `json:"repo"`
	Degree      int     `json:"degree"`
	PageRank    float64 `json:"pagerank"`
	CommunityID *int    `json:"community_id,omitempty"`
	SourceFile  string  `json:"source_file,omitempty"`
}

// v2GraphEdge is the per-edge shape (compact).
type v2GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

// v2GraphCommunity is the community summary for the legend / focus popover.
// ColorIndex is a 1-based index into the pastel categorical scale so the
// frontend can pick a stable token color without knowing the palette size.
type v2GraphCommunity struct {
	ID         int    `json:"id"`
	Label      string `json:"label"`
	Repo       string `json:"repo"`
	Size       int    `json:"size"`
	ColorIndex int    `json:"color_index"`
}

// v2GraphRepo is one repo in the group (drives the repo filter + islands).
type v2GraphRepo struct {
	ID         string `json:"id"`
	Language   string `json:"language"`
	ColorIndex int    `json:"color_index"`
}

// v2GraphResponse is the data payload inside the v2 envelope.
type v2GraphResponse struct {
	Nodes          []v2GraphNode      `json:"nodes"`
	Edges          []v2GraphEdge      `json:"edges"`
	Communities    []v2GraphCommunity `json:"communities"`
	Repos          []v2GraphRepo      `json:"repos"`
	TotalNodeCount int                `json:"total_node_count"`
}

// handleV2Graph — GET /api/v2/graph/{group}
func (s *Server) handleV2Graph(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	filterKind := r.URL.Query().Get("filter_kind")
	reposParam := r.URL.Query().Get("repos")
	includeExternal := r.URL.Query().Get("include_external") == "true"
	includeModules := r.URL.Query().Get("view") == "modules" ||
		r.URL.Query().Get("include") == "modules"
	lodParam := r.URL.Query().Get("lod")

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	// Payload cache + strong ETag/304. A "v2:" prefix keeps the v2 payload
	// cache entries distinct from v1's for the same (group, params) tuple.
	// The lod suffix is appended so each LoD level has its own cache entry.
	cacheKey := "v2:" + payloadCacheKey(group, filterKind, "", reposParam, includeExternal, includeModules) + ":lod=" + lodParam
	if entry, hit := s.graphs.Payloads.Get(cacheKey); hit {
		w.Header().Set("ETag", entry.etag)
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Header.Get("If-None-Match") == entry.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(entry.body)
		return
	}

	repos := sortedRepos(grp)
	if reposParam != "" {
		slugSet := map[string]bool{}
		for _, sl := range strings.Split(reposParam, ",") {
			slugSet[strings.TrimSpace(sl)] = true
		}
		var filtered []*DashRepo
		for _, rp := range repos {
			if slugSet[rp.Slug] {
				filtered = append(filtered, rp)
			}
		}
		repos = filtered
	}

	resp := s.buildV2Graph(repos, grp, filterKind, includeExternal, includeModules)

	// Apply LoD thinning: cap nodes by pagerank, then drop orphaned edges.
	// total_node_count is preserved as the un-thinned count so the UI badge
	// can read "500 / 12 000" even after thinning.
	totalBeforeThin := resp.TotalNodeCount
	nodeCap := lodNodeCap(lodParam)
	if nodeCap > 0 && len(resp.Nodes) > nodeCap {
		resp.Nodes = thinByPagerankConnected(resp.Nodes, resp.Edges, nodeCap)
		keptIDs := make(map[string]bool, len(resp.Nodes))
		for _, n := range resp.Nodes {
			keptIDs[n.ID] = true
		}
		pruned := resp.Edges[:0]
		for _, e := range resp.Edges {
			if keptIDs[e.Source] && keptIDs[e.Target] {
				pruned = append(pruned, e)
			}
		}
		resp.Edges = pruned
		// Degree must reflect the post-thin edge set, not the full payload.
		recomputeServedDegree(resp.Nodes, resp.Edges)
	}
	resp.TotalNodeCount = totalBeforeThin

	if resp.TotalNodeCount > softNodeWarnThreshold {
		w.Header().Set("X-Graph-Warning", "large-graph: node count exceeds 50k; consider filtering by repo or kind")
	}

	env := v2OK(resp)
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(env); err != nil {
		writeV2JSON(w, http.StatusOK, env)
		return
	}
	body := buf.Bytes()
	sum := sha256.Sum256(body)
	etag := fmt.Sprintf(`"%x"`, sum[:8])
	s.graphs.Payloads.Set(cacheKey, body, etag)

	w.Header().Set("ETag", etag)
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// buildV2Graph walks the loaded repos and assembles the v2 graph payload.
// Mirrors serveGraphDense's visibility + filter rules so v1 and v2 agree on
// which nodes/edges exist; adds pagerank + source_file + repo/community color
// indices that the cosmos.gl canvas needs.
func (s *Server) buildV2Graph(repos []*DashRepo, grp *DashGroup, filterKind string, includeExternal, includeModules bool) v2GraphResponse {
	totalEntities, totalRels, totalCommunities := 0, 0, 0
	for _, rp := range repos {
		if rp.Doc == nil {
			continue
		}
		totalEntities += len(rp.Doc.Entities)
		totalRels += len(rp.Doc.Relationships)
		totalCommunities += len(rp.Doc.Communities)
	}

	nodes := make([]v2GraphNode, 0, totalEntities)
	edges := make([]v2GraphEdge, 0, totalRels)
	communities := make([]v2GraphCommunity, 0, totalCommunities)
	reposOut := make([]v2GraphRepo, 0, len(repos))
	visible := make(map[string]bool, totalEntities)

	// Stable 1-based repo color index (alphabetical, matching sortedRepos order).
	repoColorIdx := make(map[string]int, len(repos))
	for i, rp := range repos {
		repoColorIdx[rp.Slug] = (i % pastelScaleSize) + 1
	}

	for _, rp := range repos {
		if rp.Doc == nil {
			continue
		}
		lang := ""
		// Use the most common entity language as the repo's primary language.
		if len(rp.Doc.Entities) > 0 {
			lang = dominantLanguage(rp.Doc.Entities)
		}
		reposOut = append(reposOut, v2GraphRepo{
			ID:         rp.Slug,
			Language:   lang,
			ColorIndex: repoColorIdx[rp.Slug],
		})

		for _, c := range rp.Doc.Communities {
			label := c.AutoName
			if c.AgentName != "" {
				label = c.AgentName
			}
			communities = append(communities, v2GraphCommunity{
				ID:         c.ID,
				Label:      label,
				Repo:       rp.Slug,
				Size:       c.Size,
				ColorIndex: communityColorIndex(c.ID),
			})
		}

		for i := range rp.Doc.Entities {
			e := &rp.Doc.Entities[i]
			strippedKind := dashStripScopePrefix(e.Kind)
			if !includeExternal && strippedKind == externalKindSuffix {
				continue
			}
			if !includeModules && strippedKind == module.KindModule {
				continue
			}
			if filterKind != "" && strippedKind != filterKind {
				continue
			}
			pid := dashPrefixedID(rp.Slug, e.ID)
			if visible[pid] {
				continue
			}
			visible[pid] = true
			pr := 0.0
			if e.PageRank != nil {
				pr = *e.PageRank
			}
			node := v2GraphNode{
				ID:    pid,
				Label: entityLabel(e),
				Kind:  strippedKind,
				Repo:  rp.Slug,
				// Degree is recomputed from the SERVED edges by
				// recomputeServedDegree below, so the field matches what the
				// canvas actually renders. The full-graph degreeMap counted
				// edges to neighbours that get filtered out of the payload
				// (External, modules, or a CONTAINS parent that is not
				// visible), which made it claim every node had degree>=1 while
				// ~24% rendered as isolated dots (Issue #1597).
				Degree:     0,
				PageRank:   pr,
				SourceFile: e.SourceFile,
			}
			if e.CommunityID != nil {
				node.CommunityID = e.CommunityID
			}
			nodes = append(nodes, node)
		}
	}

	for _, rp := range repos {
		if rp.Doc == nil {
			continue
		}
		for _, rel := range rp.Doc.Relationships {
			from := dashPrefixedID(rp.Slug, rel.FromID)
			to := dashPrefixedID(rp.Slug, rel.ToID)
			if visible[from] && visible[to] {
				edges = append(edges, v2GraphEdge{Source: from, Target: to, Kind: rel.Kind})
			}
		}
	}
	for _, l := range grp.Links {
		if visible[l.Source] && visible[l.Target] {
			edges = append(edges, v2GraphEdge{Source: l.Source, Target: l.Target, Kind: l.Kind})
		}
	}

	recomputeServedDegree(nodes, edges)

	return v2GraphResponse{
		Nodes:          nodes,
		Edges:          edges,
		Communities:    communities,
		Repos:          reposOut,
		TotalNodeCount: len(nodes),
	}
}

// recomputeServedDegree rewrites every node's Degree field to count only the
// edges actually present in edges (the served payload). This keeps node.degree
// consistent with what the canvas renders, so a node that shows as an isolated
// dot reports degree 0 instead of inheriting a full-graph degree that included
// edges to neighbours filtered out of the payload (Issue #1597).
func recomputeServedDegree(nodes []v2GraphNode, edges []v2GraphEdge) {
	deg := make(map[string]int, len(nodes))
	for _, e := range edges {
		deg[e.Source]++
		deg[e.Target]++
	}
	for i := range nodes {
		nodes[i].Degree = deg[nodes[i].ID]
	}
}

// pastelScaleSize is the number of pastel categorical slots in tokens.css
// (--pastel-1 … --pastel-10). Color indices wrap within this range.
const pastelScaleSize = 10

// communityColorIndex maps a community id to a stable 1-based pastel slot.
// id = -1 (ungrouped/denoised) maps to slot 1; negatives are normalised.
func communityColorIndex(id int) int {
	if id < 0 {
		return 1
	}
	return (id % pastelScaleSize) + 1
}

// ── LoD helpers ──────────────────────────────────────────────────────────────

// lodNodeCap maps a ?lod= query value to a node budget (0 = unlimited).
// Canonical names: overview|normal|full.
// Legacy frontend LodLevel strings: low|mid|high are also accepted.
func lodNodeCap(lod string) int {
	switch lod {
	case "overview", "low":
		return 500
	case "full", "high":
		return 0 // unlimited
	default:
		// "normal", "mid", "" (no param), and unknown values all default to 3000.
		return 3000
	}
}

// thinByPagerank returns at most cap nodes from nodes, keeping those with
// the highest pagerank (ties broken by degree). If cap == 0 or
// cap >= len(nodes) it returns nodes unchanged. The returned slice is a
// new allocation sorted descending by pagerank.
func thinByPagerank(nodes []v2GraphNode, cap int) []v2GraphNode {
	if cap == 0 || len(nodes) <= cap {
		return nodes
	}
	sorted := make([]v2GraphNode, len(nodes))
	copy(sorted, nodes)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].PageRank != sorted[j].PageRank {
			return sorted[i].PageRank > sorted[j].PageRank
		}
		return sorted[i].Degree > sorted[j].Degree
	})
	return sorted[:cap]
}

// thinByPagerankConnected caps the node set to cap nodes while keeping the
// result connectivity-preserving: among the cap nodes it selects, every node
// that has at least one real edge in the full payload retains at least one of
// those edges (its neighbour is also kept), so a connected node never renders
// as an isolated dot purely because of LoD thinning (Issue #1597).
//
// Strategy: take the top-cap nodes by pagerank as the seed set, then run a
// repair pass — for each seed node that would be edge-less under the seed set,
// pull in its highest-pagerank neighbour by evicting the lowest-pagerank seed
// node that is itself still connected. This keeps the node budget fixed while
// maximising the number of seed nodes that keep an edge. Genuinely isolated
// nodes in the full graph (true orphans) stay isolated — that is correct.
func thinByPagerankConnected(nodes []v2GraphNode, edges []v2GraphEdge, cap int) []v2GraphNode {
	if cap == 0 || len(nodes) <= cap {
		return nodes
	}

	// Rank all nodes by pagerank (ties → degree), highest first.
	sorted := make([]v2GraphNode, len(nodes))
	copy(sorted, nodes)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].PageRank != sorted[j].PageRank {
			return sorted[i].PageRank > sorted[j].PageRank
		}
		return sorted[i].Degree > sorted[j].Degree
	})

	// Adjacency over the full (pre-thin) edge set.
	adj := make(map[string][]string, len(nodes))
	for _, e := range edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
	}

	// Seed = top-cap by pagerank.
	kept := make(map[string]bool, cap)
	for i := 0; i < cap; i++ {
		kept[sorted[i].ID] = true
	}

	// connectedWithin reports whether id has at least one neighbour in kept.
	connectedWithin := func(id string) bool {
		for _, nb := range adj[id] {
			if kept[nb] {
				return true
			}
		}
		return false
	}

	// Repair pass: for each kept node that has real edges but none survive,
	// bring in its best neighbour by evicting the weakest evictable seed
	// (lowest pagerank, still connected, not the node we are repairing).
	// rank position lookup for picking the weakest seed to evict.
	pos := make(map[string]int, len(sorted))
	for i := range sorted {
		pos[sorted[i].ID] = i
	}
	for i := 0; i < cap; i++ {
		id := sorted[i].ID
		if !kept[id] {
			continue
		}
		if len(adj[id]) == 0 {
			continue // genuine orphan in the full graph; leave it isolated
		}
		if connectedWithin(id) {
			continue
		}
		// Pick the highest-pagerank neighbour not already kept.
		best := ""
		bestPos := -1
		for _, nb := range adj[id] {
			if kept[nb] {
				continue
			}
			if p, ok := pos[nb]; ok && (bestPos == -1 || p < bestPos) {
				best, bestPos = nb, p
			}
		}
		if best == "" {
			continue
		}
		// Find the weakest evictable seed: lowest pagerank, not id/best, and
		// removing it does not strand another node we already repaired.
		victim := ""
		for j := cap - 1; j >= 0; j-- {
			cand := sorted[j].ID
			if !kept[cand] || cand == id || cand == best {
				continue
			}
			victim = cand
			break
		}
		if victim == "" {
			continue
		}
		delete(kept, victim)
		kept[best] = true
	}

	out := make([]v2GraphNode, 0, cap)
	for i := range sorted {
		if kept[sorted[i].ID] {
			out = append(out, sorted[i])
		}
	}
	return out
}

// dominantLanguage returns the most frequent non-empty Language across the
// repo's entities — used as the repo's primary language label.
func dominantLanguage(entities []graph.Entity) string {
	counts := map[string]int{}
	for i := range entities {
		if lang := entities[i].Language; lang != "" {
			counts[lang]++
		}
	}
	best := ""
	bestN := 0
	// Deterministic: iterate sorted keys so ties resolve stably.
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if counts[k] > bestN {
			bestN = counts[k]
			best = k
		}
	}
	return best
}
