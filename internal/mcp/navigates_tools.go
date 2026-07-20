// navigates_tools.go — MCP handler for the grafel_navigates tool (#2658).
//
// grafel_navigates traverses NAVIGATES_TO edges in the loaded graph,
// supporting filter-by-route, filter-by-param, direction, and a multi-hop
// flow mode.
//
// Filters:
//   - route=<string>       — exact or prefix match on Properties["route"]
//   - with_param=<string>  — match edges whose Properties["params"] contains
//     the given param key name
//   - repo_filter          — standard per-repo scope restriction
//   - direction=outgoing|incoming — outgoing: find what X navigates TO;
//     incoming: find what navigates TO a given entity
//   - mode=list|flow       — list (default): flat edge list;
//     flow: multi-hop BFS following NAVIGATES_TO chains
//
// Return shape:
//
//	{
//	  "count": N,
//	  "total": N,
//	  "edges": [
//	    {
//	      "from_id":    "...",
//	      "from_name":  "...",
//	      "from_repo":  "...",
//	      "to_id":      "route:/foo",
//	      "route":      "/foo",
//	      "params":     "id, type",
//	      "line":       42,
//	      "source_file":"...",
//	      "hop":        0   // only present in flow mode
//	    }, ...
//	  ],
//	  "truncated": false,
//	  "mode": "list",
//	  "direction": "outgoing"
//	}
package mcp

import (
	"context"
	"sort"
	"strconv"
	"strings"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// kindNAVIGATES_TO is the relationship kind emitted by the JS/TS navigation
// extractor (internal/extractors/javascript/navigation.go, #2655).
const kindNAVIGATES_TO = "NAVIGATES_TO"

// navigatesEntityMeta holds the minimal entity attributes needed by navigates
// query helpers.
type navigatesEntityMeta struct {
	name       string
	sourceFile string
	repo       string
}

// navigatesEdgeItem is the wire shape for a single NAVIGATES_TO edge returned
// by grafel_navigates.
type navigatesEdgeItem struct {
	FromID     string `json:"from_id"`
	FromName   string `json:"from_name,omitempty"`
	FromRepo   string `json:"from_repo"`
	ToID       string `json:"to_id"`
	Route      string `json:"route,omitempty"`
	Params     string `json:"params,omitempty"`
	Line       int    `json:"line,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
	Hop        int    `json:"hop,omitempty"` // flow mode only
}

// handleNavigates is the handler for the grafel_navigates MCP tool (#2658).
// It queries NAVIGATES_TO edges with optional route / param / direction filters.
// When mode=flow it performs a multi-hop BFS following NAVIGATES_TO chains.
func (s *Server) handleNavigates(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}

	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	routeFilter := argString(req, "route", "")
	withParam := argString(req, "with_param", "")
	direction := strings.ToLower(argString(req, "direction", "outgoing"))
	mode := strings.ToLower(argString(req, "mode", "list"))
	entityID := argString(req, "entity_id", "")
	limit := argInt(req, "limit", 100)
	if limit <= 0 {
		limit = 100
	}

	// Validate direction.
	if direction != "outgoing" && direction != "incoming" {
		return mcpapi.NewToolResultError(
			"invalid direction " + direction + " (allowed: outgoing, incoming)",
		), nil
	}

	// Validate mode.
	if mode != "list" && mode != "flow" {
		return mcpapi.NewToolResultError(
			"invalid mode " + mode + " (allowed: list, flow)",
		), nil
	}

	// Build entity ID lookup maps (local ID → entity) for name resolution.
	// We also build a prefixed-entity-ID lookup for fast from_name resolution.
	entityByPrefixed := make(map[string]navigatesEntityMeta)
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			pid := prefixedID(r.Repo, e.ID)
			entityByPrefixed[pid] = navigatesEntityMeta{name: e.Name, sourceFile: e.SourceFile, repo: r.Repo}
			entityByPrefixed[e.ID] = navigatesEntityMeta{name: e.Name, sourceFile: e.SourceFile, repo: r.Repo}
		}
	}

	var edges []navigatesEdgeItem

	switch mode {
	case "list":
		edges = collectNavigatesEdges(repos, entityByPrefixed, routeFilter, withParam, direction, entityID)

	case "flow":
		// Multi-hop BFS: start from entityID (or all NAVIGATES_TO sources if
		// entity_id is unset) and follow NAVIGATES_TO chains up to max_depth hops.
		maxDepth := argInt(req, "max_depth", 5)
		if maxDepth <= 0 {
			maxDepth = 5
		}
		edges = collectNavigatesFlow(repos, entityByPrefixed, routeFilter, withParam, entityID, maxDepth)
	}

	// Sort: by from_repo, then from_id, then to_id for determinism.
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].FromRepo != edges[j].FromRepo {
			return edges[i].FromRepo < edges[j].FromRepo
		}
		if edges[i].FromID != edges[j].FromID {
			return edges[i].FromID < edges[j].FromID
		}
		return edges[i].ToID < edges[j].ToID
	})

	total := len(edges)
	if limit > 0 && len(edges) > limit {
		edges = edges[:limit]
	}
	truncated := len(edges) < total

	// Always return a non-nil slice so the JSON encodes as [] not null.
	if edges == nil {
		edges = []navigatesEdgeItem{}
	}

	return jsonResult(map[string]any{
		"count":     len(edges),
		"total":     total,
		"truncated": truncated,
		"mode":      mode,
		"direction": direction,
		"edges":     edges,
	}), nil
}

// collectNavigatesEdges scans all NAVIGATES_TO relationships across repos and
// returns those matching the given filters. direction="outgoing" returns edges
// FROM entities that navigate somewhere; direction="incoming" returns edges TO
// the entity (i.e. who navigates to a given route).
func collectNavigatesEdges(
	repos []*LoadedRepo,
	entityByPrefixed map[string]navigatesEntityMeta,
	routeFilter, withParam, direction, entityID string,
) []navigatesEdgeItem {
	var out []navigatesEdgeItem

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if !strings.EqualFold(rel.Kind, kindNAVIGATES_TO) {
				continue
			}

			// Apply entity_id filter (direction-aware).
			if entityID != "" {
				switch direction {
				case "outgoing":
					// entity_id is the FROM entity; match by local or prefixed ID.
					if rel.FromID != entityID && prefixedID(r.Repo, rel.FromID) != entityID {
						continue
					}
				case "incoming":
					// entity_id is the TO route / destination entity.
					if rel.ToID != entityID {
						continue
					}
				}
			}

			route := ""
			params := ""
			if rel.PropLen() > 0 {
				route = rel.PropGet("route")
				params = rel.PropGet("params")
			}

			// Apply route filter (contains, case-insensitive).
			if routeFilter != "" && !strings.Contains(strings.ToLower(route), strings.ToLower(routeFilter)) {
				continue
			}

			// Apply with_param filter: params is comma-separated key names.
			if withParam != "" {
				found := false
				for _, p := range strings.Split(params, ",") {
					if strings.TrimSpace(p) == withParam {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			line := 0
			if rel.PropLen() > 0 {
				if ls, ok := rel.PropLookup("line"); ok {
					line, _ = strconv.Atoi(ls)
				}
			}

			pid := prefixedID(r.Repo, rel.FromID)
			meta := entityByPrefixed[pid]
			if meta.name == "" {
				meta = entityByPrefixed[rel.FromID]
			}

			out = append(out, navigatesEdgeItem{
				FromID:     pid,
				FromName:   meta.name,
				FromRepo:   r.Repo,
				ToID:       rel.ToID,
				Route:      route,
				Params:     params,
				Line:       line,
				SourceFile: meta.sourceFile,
			})
		}
	}
	return out
}

// collectNavigatesFlow performs a multi-hop BFS following NAVIGATES_TO edges
// starting from entityID (all sources when entity_id is empty). Each hop is
// annotated with its BFS depth. Useful for tracing navigation chains like:
// HomeScreen → /dashboard → DashboardScreen → /profile.
func collectNavigatesFlow(
	repos []*LoadedRepo,
	entityByPrefixed map[string]navigatesEntityMeta,
	routeFilter, withParam, startEntityID string,
	maxDepth int,
) []navigatesEdgeItem {
	type queueItem struct {
		entityID string
		repo     string
		hop      int
	}

	// Build a per-repo forward NAVIGATES_TO adjacency: fromID → list of edges.
	type navEdge struct {
		toID   string
		route  string
		params string
		line   int
		srcRel int // index into r.Doc.Relationships
		repo   string
	}
	navAdj := make(map[string][]navEdge) // keyed by prefixedID(repo, fromID)
	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Relationships {
			rel := &r.Doc.Relationships[i]
			if !strings.EqualFold(rel.Kind, kindNAVIGATES_TO) {
				continue
			}
			route, params, line := "", "", 0
			if rel.PropLen() > 0 {
				route = rel.PropGet("route")
				params = rel.PropGet("params")
				if ls, ok := rel.PropLookup("line"); ok {
					line, _ = strconv.Atoi(ls)
				}
			}
			pid := prefixedID(r.Repo, rel.FromID)
			ne := navEdge{
				toID:   rel.ToID,
				route:  route,
				params: params,
				line:   line,
				repo:   r.Repo,
			}
			navAdj[pid] = append(navAdj[pid], ne)
			// Also key by bare ID so that ToID lookups (which may be bare)
			// can find subsequent hops.
			navAdj[rel.FromID] = append(navAdj[rel.FromID], ne)
		}
	}

	// Determine BFS start set.
	var frontier []queueItem
	seen := make(map[string]bool) // visited from-entity IDs

	if startEntityID != "" {
		// Resolve to a prefixed ID if needed.
		for _, r := range repos {
			if r.Doc == nil {
				continue
			}
			pid := prefixedID(r.Repo, startEntityID)
			if _, ok := navAdj[pid]; ok {
				frontier = append(frontier, queueItem{entityID: pid, repo: r.Repo, hop: 0})
				seen[pid] = true
			}
			// Also try bare ID directly.
			if _, ok := navAdj[startEntityID]; ok && !seen[startEntityID] {
				frontier = append(frontier, queueItem{entityID: startEntityID, repo: r.Repo, hop: 0})
				seen[startEntityID] = true
			}
		}
	} else {
		// Start from all entities that have outgoing NAVIGATES_TO edges.
		for pid := range navAdj {
			frontier = append(frontier, queueItem{entityID: pid, repo: "", hop: 0})
			seen[pid] = true
		}
	}

	var out []navigatesEdgeItem
	visited := make(map[string]bool) // visited (from→to) edge keys to avoid duplicates

	for len(frontier) > 0 {
		curr := frontier[0]
		frontier = frontier[1:]

		if curr.hop >= maxDepth {
			continue
		}

		for _, ne := range navAdj[curr.entityID] {
			edgeKey := curr.entityID + "→" + ne.toID
			if visited[edgeKey] {
				continue
			}

			// Apply route filter.
			if routeFilter != "" && !strings.Contains(strings.ToLower(ne.route), strings.ToLower(routeFilter)) {
				continue
			}
			// Apply with_param filter.
			if withParam != "" {
				found := false
				for _, p := range strings.Split(ne.params, ",") {
					if strings.TrimSpace(p) == withParam {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			visited[edgeKey] = true

			meta := entityByPrefixed[curr.entityID]
			out = append(out, navigatesEdgeItem{
				FromID:     curr.entityID,
				FromName:   meta.name,
				FromRepo:   ne.repo,
				ToID:       ne.toID,
				Route:      ne.route,
				Params:     ne.params,
				Line:       ne.line,
				SourceFile: meta.sourceFile,
				Hop:        curr.hop,
			})

			// Enqueue destination if it is itself a navigation source.
			// Try both bare and prefixed forms of the toID so that
			// cross-ID-format chains (bare ToID → prefixed navAdj key) are traversed.
			nextHop := curr.hop + 1
			nextIDs := []string{ne.toID, prefixedID(ne.repo, ne.toID)}
			for _, nid := range nextIDs {
				if !seen[nid] && len(navAdj[nid]) > 0 {
					seen[nid] = true
					frontier = append(frontier, queueItem{entityID: nid, repo: ne.repo, hop: nextHop})
				}
			}
		}
	}

	return out
}
