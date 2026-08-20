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

	"github.com/cajasmota/grafel/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// kindNAVIGATES_TO is the relationship kind emitted by the JS/TS navigation
// extractor (internal/extractors/javascript/navigation.go, #2655).
const kindNAVIGATES_TO = "NAVIGATES_TO"

// navigationEdgeKinds is the kind set for the dedicated grafel_navigates tool:
// router navigation and nothing else.
var navigationEdgeKinds = []string{kindNAVIGATES_TO}

// usageEdgeKinds is the kind set behind grafel_related direction=uses /
// used_by (#6314).
//
// The route used to pin the traversal to NAVIGATES_TO alone, so USES,
// USES_HOOK, REFERENCES and CALLS — all of which the extractors genuinely
// emit — were unreachable from a parameter literally named "uses". Property
// and enum-member access, the symptom in the report, is modelled as
// REFERENCES/USES: there is no distinct ACCESSES kind in the model.
//
// What is deliberately NOT in the set:
//   - CONTAINS / IMPORTS / DEPENDS_ON — containment and module-level
//     dependency, not one entity using another.
//   - EXTENDS / IMPLEMENTS — type hierarchy; grafel_related has no "uses"
//     claim over inheritance, and folding it in would make "who uses this"
//     unanswerable separately from "who subclasses this".
//   - INJECTED_INTO — its direction is inverted relative to usage (the
//     provider points at its consumer), so including it would report the
//     opposite of what the caller asked for.
//   - INSTANTIATES — the only emitter is the HCL module_instantiation
//     extractor, and its ToID is not an entity at all: it is the synthetic
//     "tfmodule-def:<dir>" directory marker the resolver classifies as
//     Dynamic. Including it would make direction=uses report marker strings
//     as usages, and no used_by query could ever match one, so it is cost
//     without a reachable answer.
//
// RENDERS IS in the set: it is the component-uses-component edge of the
// JS/TS corpus (emitted by the javascript, angular, svelte, vue, astro,
// rescript and fsharp-elmish extractors), so without it direction=used_by on
// a React/Vue/Svelte component returns nothing for every parent that renders
// it — the same silently-incomplete answer #6314 is about, one kind over.
//
// NAVIGATES_TO stays in the set: widening must not remove what worked.
var usageEdgeKinds = []string{
	"CALLS",
	"USES",
	"USES_HOOK",
	"REFERENCES",
	"RENDERS",
	kindNAVIGATES_TO,
}

// matchesKind reports whether rel.Kind is in the (case-insensitive) kind set.
func matchesKind(kinds []string, kind string) bool {
	for _, k := range kinds {
		if strings.EqualFold(kind, k) {
			return true
		}
	}
	return false
}

// matchesEntityID reports whether relID (a repo-local relationship endpoint in
// repo) is the entity the caller named.
//
// Two forms are accepted, and which ones are live depends on what the caller
// supplied. A prefixed "repo::id" is unambiguous, so only the prefixed compare
// runs: the bare compare carries no repo, and leaving it live would let a
// foreign repo whose bare endpoint literally spells another repo's prefixed ID
// union its edges into the answer. A bare entity_id can only be compared
// bare — route destinations ("route:/foo") are synthetic and never prefixed,
// which is what makes grafel_navigates direction=incoming entity_id=/route and
// the navigation_route fallback work at all.
func matchesEntityID(entityID, repo, relID string) bool {
	if strings.Contains(entityID, "::") {
		return prefixedID(repo, relID) == entityID
	}
	return relID == entityID
}

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
	Kind       string `json:"kind,omitempty"`
	Route      string `json:"route,omitempty"`
	Params     string `json:"params,omitempty"`
	Line       int    `json:"line,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
	Hop        int    `json:"hop,omitempty"` // flow mode only
}

// handleNavigates is the handler for the grafel_navigates MCP tool (#2658).
// It queries NAVIGATES_TO edges with optional route / param / direction filters.
// When mode=flow it performs a multi-hop BFS following NAVIGATES_TO chains.
func (s *Server) handleNavigates(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	return s.handleNavigatesKinds(ctx, req, navigationEdgeKinds)
}

// handleNavigatesKinds is the traversal body shared by grafel_navigates
// (navigation edges only) and grafel_related direction=uses/used_by (the
// usage kind set, #6314). Everything except the kind predicate — filters,
// direction, list/flow modes, sorting, limit — is identical.
func (s *Server) handleNavigatesKinds(_ context.Context, req mcpapi.CallToolRequest, kinds []string) (*mcpapi.CallToolResult, error) {
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
		r.forEachEntity(func(e *graph.Entity) bool {
			pid := prefixedID(r.Repo, e.ID)
			entityByPrefixed[pid] = navigatesEntityMeta{name: e.Name, sourceFile: e.SourceFile, repo: r.Repo}
			entityByPrefixed[e.ID] = navigatesEntityMeta{name: e.Name, sourceFile: e.SourceFile, repo: r.Repo}
			return true
		})
	}

	var edges []navigatesEdgeItem

	switch mode {
	case "list":
		edges = collectNavigatesEdges(repos, entityByPrefixed, kinds, routeFilter, withParam, direction, entityID)

	case "flow":
		// Multi-hop BFS: start from entityID (or all NAVIGATES_TO sources if
		// entity_id is unset) and follow NAVIGATES_TO chains up to max_depth hops.
		maxDepth := argInt(req, "max_depth", 5)
		if maxDepth <= 0 {
			maxDepth = 5
		}
		edges = collectNavigatesFlow(repos, entityByPrefixed, kinds, routeFilter, withParam, entityID, maxDepth)
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
		"kinds":     kinds,
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
	kinds []string,
	routeFilter, withParam, direction, entityID string,
) []navigatesEdgeItem {
	var out []navigatesEdgeItem

	for _, r := range repos {
		if r.Doc == nil {
			continue
		}
		r.forEachRelationship(func(rel *graph.Relationship) bool {
			if !matchesKind(kinds, rel.Kind) {
				return true
			}

			// Apply entity_id filter (direction-aware).
			if entityID != "" {
				switch direction {
				case "outgoing":
					// entity_id is the FROM entity; match by local or prefixed ID.
					if !matchesEntityID(entityID, r.Repo, rel.FromID) {
						return true
					}
				case "incoming":
					// entity_id is the TO route / destination entity. Route
					// destinations carry a synthetic bare ID ("route:/foo"),
					// but a usage edge points at a real entity whose ID the
					// caller will have in prefixed form, so accept either
					// (#6314).
					if !matchesEntityID(entityID, r.Repo, rel.ToID) {
						return true
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
				return true
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
					return true
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
				Kind:       rel.Kind,
				Route:      route,
				Params:     params,
				Line:       line,
				SourceFile: meta.sourceFile,
			})
			return true
		})
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
	kinds []string,
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
		kind   string
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
		r.forEachRelationship(func(rel *graph.Relationship) bool {
			if !matchesKind(kinds, rel.Kind) {
				return true
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
				kind:   rel.Kind,
				route:  route,
				params: params,
				line:   line,
				repo:   r.Repo,
			}
			navAdj[pid] = append(navAdj[pid], ne)
			// Also key by bare ID so that ToID lookups (which may be bare)
			// can find subsequent hops.
			navAdj[rel.FromID] = append(navAdj[rel.FromID], ne)
			return true
		})
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
			// Key by kind as well as endpoints: with more than one usage kind
			// in the set (#6314) two entities are routinely joined by parallel
			// edges of different kinds, and an endpoints-only key would drop all
			// but the first — reporting one kind where several apply.
			edgeKey := curr.entityID + "→" + ne.kind + "→" + ne.toID
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
				Kind:       ne.kind,
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
