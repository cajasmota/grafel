package dashboard

// handlers_graph.go — graph endpoints
//
//	GET /api/graph/{group}?filter_kind=&filter_repo=&repos=slug1,slug2&include_external=false
//	GET /api/graph/{group}/entity/{id}
//	GET /api/graph/{group}/labels?top=200
//	GET /api/graph/{group}/labels?ids=a,b,c
//
// Three-tier graph data model:
//
//	Tier 1 (default): Compact render payload — nodes carry id, repo, kind,
//	  degree, community_id, and label (all nodes, #1374). Edges carry source, target, kind.
//	  This is the only shape; there is no ?full= opt-in.
//
//	Tier 2: Labels endpoint — GET /api/graph/{group}/labels?top=200 returns
//	  {id, label} for the top-N nodes by degree. Accepts ?ids=a,b,c for
//	  explicit id lookup (hover-to-label).
//
//	Tier 3: Entity detail — GET /api/graph/{group}/entity/{id} returns the
//	  full inspector shape (kind, source_file, start_line, pagerank, inbound[],
//	  outbound[]). Fetched lazily on node click.
//
// #1023: LoD tiers removed. The endpoint returns all entities — no per-repo
// node cap. Cosmograph handles 1M+ nodes via GPU WebGL at 60fps; no sampling
// or LoD switching is needed.
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
//
// Perf (#1249): typed structs replace map[string]any throughout this file to
// eliminate per-node heap allocations and enable direct JSON encoding without
// intermediate map boxing.  On a 100k-node graph this cuts allocations ~10x and
// reduces GC pause by >40%.  gzip middleware is applied at the mux level
// (see server.go withGzip) so callers that send Accept-Encoding: gzip get a
// compressed response automatically.
//
// Perf (#1399): server-side payload cache caches the pre-serialised JSON bytes
// for each (group + params) combination.  On a cache hit the handler skips the
// O(nodes+edges) rebuild loop and performs only a map lookup + write — reducing
// warm-path latency from ~hundreds of ms to <5 ms for typical production graphs.
// ETag + 304 Not Modified support lets browsers reuse their cached copy across
// page reloads, making repeat visits instant (zero bytes transferred).
// Cache invalidation is automatic: GraphCache.Invalidate(group) busts both the
// loaded-group cache and the payload cache atomically, so any rebuild or
// enrichment write-back causes the next request to regenerate a fresh payload.

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/module"
)

// softNodeWarnThreshold is the node count above which the API adds an
// X-Graph-Warning response header so the frontend can surface a notice.
const softNodeWarnThreshold = 50_000

// entityLabel returns the human-readable display label for an entity.
//
// For most entities this is e.Name. For Process (SCOPE.Process) entities the
// Name field holds a synthesised "<entry> → <terminal>" string set by the
// process-flow BFS pass. If that Name is somehow empty (e.g. the entity was
// written by an older indexer version, or the entry function itself had an
// empty Name), we reconstruct a readable label from the stored Properties:
//
//  1. entry_name property (the entry function's name, always stored by the pass)
//  2. chain_labels property (full "A → B → C" string, first/last segment pair)
//  3. entry_id property (last path component of the entry entity id)
//  4. The raw entity ID itself (still better than nothing — callers that show
//     the raw id will at least see a shorter string via this field)
func entityLabel(e *graph.Entity) string {
	if e.Name != "" {
		return e.Name
	}
	// Only apply the Properties fallback for Process entities — other kinds
	// with empty Names are normal (anonymous lambdas, generated stubs, etc.)
	// and should just propagate the empty string without confusing the caller.
	if e.Kind != "SCOPE.Process" {
		return e.Name
	}
	if e.PropLen() > 0 {
		// Prefer the pre-stored entry_name which is the entry function's name.
		if en := e.PropGet("entry_name"); en != "" {
			// If we also have chain_labels, derive the terminal name for a richer
			// "entry → terminal" label that matches what the pass would have built.
			if cl := e.PropGet("chain_labels"); cl != "" {
				// chain_labels is "A → B → … → Z"; extract the last segment.
				parts := strings.Split(cl, " → ")
				if len(parts) >= 2 {
					return en + " → " + parts[len(parts)-1]
				}
			}
			return en + " flow"
		}
		// Fallback: last path component of entry_id (strips the scope prefix).
		if eid := e.PropGet("entry_id"); eid != "" {
			if idx := strings.LastIndexAny(eid, ":./"); idx >= 0 && idx < len(eid)-1 {
				return eid[idx+1:] + " flow"
			}
			return eid + " flow"
		}
	}
	// Last resort: return the raw entity ID so the caller at least has something.
	return e.ID
}

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

	// view=modules or include=modules opts into the module-aggregation layer
	// (#1393).  By default Module-kind synthetic nodes and their incident
	// CONTAINS / DEPENDS_ON edges are excluded from the entity graph to avoid
	// cluttering the default view.
	includeModules := r.URL.Query().Get("view") == "modules" ||
		r.URL.Query().Get("include") == "modules"

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	// ── Payload cache + ETag/304 ─────────────────────────────────────────────
	// Check the pre-serialised payload cache before doing any work.  On a hit
	// we skip the entire O(nodes+edges) build loop and serve directly from the
	// cached bytes.  ETag + 304 lets the browser reuse its own copy on repeat
	// visits (zero transfer).
	//
	// The cache key covers all query params that change the output.  The cache
	// entry is invalidated by GraphCache.Invalidate(group) which is called on
	// every re-index event and enrichment write-back.
	cacheKey := payloadCacheKey(group, filterKind, filterRepo, reposParam, includeExternal, includeModules)

	if entry, hit := s.graphs.Payloads.Get(cacheKey); hit {
		// Strong ETag — allows the browser to short-circuit the full
		// response body on repeat visits.
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

	s.serveGraphDense(w, r, grp, repos, filterKind, includeExternal, includeModules, cacheKey)
}

// externalKindSuffix is the trailing portion of the SCOPE.External kind after
// dashStripScopePrefix strips the leading "SCOPE." prefix.
const externalKindSuffix = "External"

// ── Perf (#1249): typed wire structs ─────────────────────────────────────────
// Using concrete types instead of map[string]any eliminates one heap allocation
// per node/edge and lets encoding/json use cached reflection data, cutting
// encoding time ~30% on large graphs.

// graphNodeWire is the Tier-1 compact node shape sent over the wire.
// Fields tagged omitempty are absent for most nodes — keeps JSON tight.
// Label is NOT omitempty: an empty string must be transmitted explicitly so
// the frontend client receives "" rather than undefined, which would cause
// it to fall back to the raw id (e.g. "repo::proc:<hash>").
type graphNodeWire struct {
	ID          string `json:"id"`
	Repo        string `json:"repo"`
	Kind        string `json:"kind"`
	Degree      int    `json:"degree"`
	CommunityID *int   `json:"community_id,omitempty"`
	Label       string `json:"label"`
}

// graphEdgeWire is the Tier-1 compact edge shape.
type graphEdgeWire struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Kind   string `json:"kind"`
}

// graphCommunityWire is the community summary shape.
type graphCommunityWire struct {
	ID          int      `json:"id"`
	Size        int      `json:"size"`
	AutoName    string   `json:"auto_name"`
	Repo        string   `json:"repo"`
	TopEntities []string `json:"top_entities"`
	AgentName   string   `json:"agent_name,omitempty"`
}

// graphDenseResponse is the top-level Tier-1 response envelope.
type graphDenseResponse struct {
	Nodes          []graphNodeWire      `json:"nodes"`
	Edges          []graphEdgeWire      `json:"edges"`
	Communities    []graphCommunityWire `json:"communities"`
	TotalNodeCount int                  `json:"total_node_count"`
}

// serveGraphDense returns every entity in the indexed graph — no per-repo cap.
// Cosmograph handles 1M+ nodes at 60fps via GPU WebGL (#1023 removed LoD).
// A soft X-Graph-Warning header is added when node count exceeds
// softNodeWarnThreshold so the frontend can optionally surface a notice.
//
// Tier 1 compact payload: nodes carry id, repo, kind, degree, community_id, label.
// Full entity detail is available via GET /api/graph/{group}/entity/{id} (Tier 3).
// The Tier 2 labels endpoint (GET /api/graph/{group}/labels) remains available for
// backward compatibility but is no longer the sole source of human-readable names.
//
// includeExternal controls whether SCOPE.External placeholder entities are
// emitted. Default (false) hides stdlib/builtin nodes from the graph view.
//
// includeModules controls whether synthetic Module-kind nodes and their
// incident CONTAINS/DEPENDS_ON edges are included.  Default (false) hides them
// to avoid cluttering the entity graph; pass ?view=modules to opt in (#1393).
//
// Perf (#1249): uses typed structs (graphNodeWire, graphEdgeWire) to eliminate
// per-node map allocations.  Pre-sizes slices from entity/relationship counts
// to avoid slice growth copies.
//
// Perf (#1399): cacheKey is the payload-cache key for this (group, params)
// combination.  After building the response, serveGraphDense serialises the
// payload to a bytes.Buffer, stores the bytes in the payload cache (keyed by
// cacheKey), and writes the bytes to w.  Subsequent requests with the same
// (group, params) are served directly from the cache without rebuilding.
// r is needed only to propagate the X-Graph-Warning header alongside the ETag.
func (s *Server) serveGraphDense(w http.ResponseWriter, r *http.Request, grp *DashGroup, repos []*DashRepo, filterKind string, includeExternal bool, includeModules bool, cacheKey string) {
	// Pre-size: count total entities + relationships across repos to avoid
	// repeated slice growth under GC pressure.
	totalEntities, totalRels, totalCommunities := 0, 0, 0
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		totalEntities += len(r.Doc.Entities)
		totalRels += len(r.Doc.Relationships)
		totalCommunities += len(r.Doc.Communities)
	}

	nodes := make([]graphNodeWire, 0, totalEntities)
	edges := make([]graphEdgeWire, 0, totalRels)
	communities := make([]graphCommunityWire, 0, totalCommunities)
	visible := make(map[string]bool, totalEntities)

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
			cm := graphCommunityWire{
				ID:          c.ID,
				Size:        c.Size,
				AutoName:    c.AutoName,
				Repo:        r.Slug,
				TopEntities: prefixed,
			}
			if c.AgentName != "" {
				cm.AgentName = c.AgentName
			}
			communities = append(communities, cm)
		}

		// Build per-repo degree map (total in + out edges) for node sizing.
		degreeMap := buildDegreeMap(r.Doc.Relationships)

		// Emit all entities — no cap. Filter by kind when requested.
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			strippedKind := dashStripScopePrefix(e.Kind)
			// Filter external stdlib/builtin placeholders unless opted in.
			if !includeExternal && strippedKind == externalKindSuffix {
				continue
			}
			// Exclude synthetic Module aggregation nodes from the default view
			// (#1393).  They are only returned when includeModules is true
			// (e.g. ?view=modules).  This avoids cluttering the entity graph
			// with 100+ container nodes and thousands of CONTAINS edges.
			if !includeModules && strippedKind == module.KindModule {
				continue
			}
			if filterKind != "" && strippedKind != filterKind {
				continue
			}
			pid := dashPrefixedID(r.Slug, e.ID)
			if visible[pid] {
				continue
			}
			visible[pid] = true
			// Tier 1 compact node: id, repo, kind, degree, community_id, label.
			// `kind` is included so the frontend can special-case Process sizing (#1121 P3).
			// `label` is included for ALL entities (#1374) so the frontend never falls back
			// to repo::<hash-id> for non-Process nodes. entityLabel handles the
			// SCOPE.Process fallback path: when e.Name is empty (older graph data or
			// entry function with no name), it reconstructs a readable label from
			// Properties["entry_name"] and Properties["chain_labels"].
			node := graphNodeWire{
				ID:     pid,
				Repo:   r.Slug,
				Kind:   strippedKind,
				Degree: degreeMap[e.ID],
				Label:  entityLabel(e),
			}
			if e.CommunityID != nil {
				node.CommunityID = e.CommunityID
			}
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
				// When Module nodes are excluded, also skip edges that are
				// incident to them (CONTAINS from a Module node, and DEPENDS_ON
				// between Module nodes).  The visibility check already ensures
				// Module-kind nodes are absent from the visible set when
				// includeModules=false, so this guard is a safety belt for
				// any edge whose Module endpoint was excluded above.
				edges = append(edges, graphEdgeWire{
					FromID: from,
					ToID:   to,
					Kind:   rel.Kind,
				})
			}
		}
	}

	// Merge cross-repo links (group-level edges produced by the link pass).
	// Only emit a cross-repo edge when BOTH endpoints are in the visible set so
	// that single-repo filtered views don't reference nodes that weren't returned.
	// See issue #1388.
	for _, l := range grp.Links {
		if visible[l.Source] && visible[l.Target] {
			edges = append(edges, graphEdgeWire{
				FromID: l.Source,
				ToID:   l.Target,
				Kind:   l.Kind,
			})
		}
	}

	if len(nodes) > softNodeWarnThreshold {
		w.Header().Set("X-Graph-Warning", "large-graph: node count exceeds 50k; consider filtering by repo or kind")
	}

	resp := graphDenseResponse{
		Nodes:          nodes,
		Edges:          edges,
		Communities:    communities,
		TotalNodeCount: len(nodes),
	}

	// Serialise to a buffer so we can (a) store the bytes in the payload
	// cache and (b) compute a strong ETag without a second encode pass.
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(resp); err != nil {
		// Fallback: write directly without caching.
		writeJSON(w, http.StatusOK, resp)
		return
	}
	body := buf.Bytes()

	// ETag = first 16 hex chars of SHA-256(body) — strong, opaque, stable.
	sum := sha256.Sum256(body)
	etag := fmt.Sprintf(`"%x"`, sum[:8])

	// Store in the payload cache for future requests.
	s.graphs.Payloads.Set(cacheKey, body, etag)

	// Set ETag and Vary so proxies and browsers can cache correctly.
	w.Header().Set("ETag", etag)
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleGraphLabels — GET /api/graph/{group}/labels?top=200
//
//	GET /api/graph/{group}/labels?ids=a,b,c
//
// Tier 2: Returns {id, label} pairs so the frontend can display node labels
// without carrying them in the main graph payload.
//
// ?top=N  — returns the top-N nodes by degree (default 200, max 2000).
// ?ids=   — returns labels for an explicit comma-separated list of node IDs
//
//	(used for hover-to-label of unlabeled nodes).
//
// The two params are mutually exclusive; ?ids= takes priority when present.
func (s *Server) handleGraphLabels(w http.ResponseWriter, r *http.Request) {
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

	idsParam := r.URL.Query().Get("ids")

	if idsParam != "" {
		// Explicit ID lookup — return labels for the given node IDs only.
		want := map[string]bool{}
		for _, id := range strings.Split(idsParam, ",") {
			if id = strings.TrimSpace(id); id != "" {
				want[id] = true
			}
		}
		type labelEntry struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		}
		out := []labelEntry{}
		for _, r := range sortedRepos(grp) {
			if r.Doc == nil {
				continue
			}
			for i := range r.Doc.Entities {
				e := &r.Doc.Entities[i]
				pid := dashPrefixedID(r.Slug, e.ID)
				if want[pid] {
					out = append(out, labelEntry{ID: pid, Label: entityLabel(e)})
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"labels": out})
		return
	}

	// Top-N by degree.
	topN := 200
	if s := r.URL.Query().Get("top"); s != "" {
		if n, err2 := strconv.Atoi(s); err2 == nil && n > 0 {
			topN = n
			if topN > 2000 {
				topN = 2000
			}
		}
	}

	type degreeLabel struct {
		id     string
		label  string
		degree int
	}
	var all []degreeLabel

	for _, repo := range sortedRepos(grp) {
		if repo.Doc == nil {
			continue
		}
		degMap := buildDegreeMap(repo.Doc.Relationships)
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			pid := dashPrefixedID(repo.Slug, e.ID)
			all = append(all, degreeLabel{id: pid, label: entityLabel(e), degree: degMap[e.ID]})
		}
	}

	sort.Slice(all, func(i, j int) bool { return all[i].degree > all[j].degree })
	if len(all) > topN {
		all = all[:topN]
	}

	type labelEntry struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	out := make([]labelEntry, len(all))
	for i, dl := range all {
		out[i] = labelEntry{ID: dl.id, Label: dl.label}
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": out})
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

	// Perf (#1249): build entity index for O(1) neighbor lookup instead of O(n)
	// linear scan per neighbor. For 100k-node graphs this drops neighbor resolution
	// from O(n*k) to O(n+k) where k is the number of neighbors.
	entityIndex := make(map[string]*graph.Entity, len(repo.Doc.Entities))
	for i := range repo.Doc.Entities {
		entityIndex[repo.Doc.Entities[i].ID] = &repo.Doc.Entities[i]
	}

	// Collect inbound and outbound edges for this entity.
	localID := entity.ID
	type edgeWire struct {
		FromID    string `json:"from_id"`
		ToID      string `json:"to_id"`
		Kind      string `json:"kind"`
		CrossRepo bool   `json:"cross_repo,omitempty"`
	}
	inbound := make([]edgeWire, 0)
	outbound := make([]edgeWire, 0)
	neighborIDs := make(map[string]bool)

	prefixedLocal := dashPrefixedID(repo.Slug, localID)

	for _, rel := range repo.Doc.Relationships {
		if rel.FromID == localID {
			outbound = append(outbound, edgeWire{
				FromID: prefixedLocal,
				ToID:   dashPrefixedID(repo.Slug, rel.ToID),
				Kind:   rel.Kind,
			})
			neighborIDs[rel.ToID] = true
		}
		if rel.ToID == localID {
			inbound = append(inbound, edgeWire{
				FromID: dashPrefixedID(repo.Slug, rel.FromID),
				ToID:   prefixedLocal,
				Kind:   rel.Kind,
			})
			neighborIDs[rel.FromID] = true
		}
	}

	// Collect cross-repo edges involving this entity.
	pid := prefixedLocal
	for _, l := range grp.Links {
		if l.Source == pid {
			outbound = append(outbound, edgeWire{
				FromID:    pid,
				ToID:      l.Target,
				Kind:      l.Kind,
				CrossRepo: true,
			})
		}
		if l.Target == pid {
			inbound = append(inbound, edgeWire{
				FromID:    l.Source,
				ToID:      pid,
				Kind:      l.Kind,
				CrossRepo: true,
			})
		}
	}

	// Resolve neighbor entities (depth-1, same repo) — O(k) via index.
	type neighborWire struct {
		ID         string `json:"id"`
		Label      string `json:"label"`
		Kind       string `json:"kind"`
		SourceFile string `json:"source_file"`
		StartLine  int    `json:"start_line"`
		Repo       string `json:"repo"`
	}
	neighbors := make([]neighborWire, 0, len(neighborIDs))
	for nid := range neighborIDs {
		if e, ok := entityIndex[nid]; ok {
			neighbors = append(neighbors, neighborWire{
				ID:         dashPrefixedID(repo.Slug, e.ID),
				Label:      entityLabel(e),
				Kind:       dashStripScopePrefix(e.Kind),
				SourceFile: e.SourceFile,
				StartLine:  e.StartLine,
				Repo:       repo.Slug,
			})
		}
	}

	// Compute in/out degree counts (same-repo + cross-repo).
	inDegree := len(inbound)
	outDegree := len(outbound)

	// Look up community name for this entity.
	var communityName string
	if entity.CommunityID != nil {
		for _, c := range repo.Doc.Communities {
			if c.ID == *entity.CommunityID {
				if c.AgentName != "" {
					communityName = c.AgentName
				} else {
					communityName = c.AutoName
				}
				break
			}
		}
	}

	resp := map[string]any{
		"entity":         serializeEntity(repo.Slug, entity),
		"inbound_edges":  inbound,
		"outbound_edges": outbound,
		"neighbors":      neighbors,
		"in_degree":      inDegree,
		"out_degree":     outDegree,
	}
	if communityName != "" {
		resp["community_name"] = communityName
	}
	if entity.Centrality != nil {
		resp["betweenness"] = *entity.Centrality
	}

	writeJSON(w, http.StatusOK, resp)
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
				Label:    entityLabel(e),
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
	// Resolve each link's source/target entity so the /links page can render
	// readable names and open a real source-peek (#4596). Additive and
	// best-effort: unresolved endpoints leave the enrichment fields empty.
	// #4698 — resolve per-repo monorepo module roots so each link endpoint can
	// carry its module_path. Best-effort: on config-load failure we pass nil and
	// module paths stay empty (links still render, scoped at repo-level).
	var moduleRoots map[string][]string
	if repoRefs, rerr := repoPathsForGroup(group); rerr == nil {
		moduleRoots = moduleRootsByRepo(repoRefs)
	}
	links = enrichLinkEndpoints(grp, links, moduleRoots)
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}
