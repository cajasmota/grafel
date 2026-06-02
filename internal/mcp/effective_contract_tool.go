package mcp

// effective_contract_tool.go — archigraph_effective_contract MCP tool (epic
// #3829, ticket #3836 — T6).
//
// This is the thin SERVING / GROUPING layer over T5's computation
// (effective_contract.go projectEffectiveContract + the engine-stamped
// effective_* props). Given a ViewSet/controller (or a single route/endpoint),
// it gathers that ViewSet's router-expanded routes, projects each into the
// structured per-verb contract, and groups them under the owning ViewSet:
//
//	{ class, framework, handlers: [ {verb, path, kind, source_class,
//	  default_status, error_statuses, serializer, pagination, permissions,
//	  auth_required, behaviour, handler}, ... ] }
//
// It is the single artifact that lets the rewrite agent answer "what is the
// full contract of every verb on this ViewSet?" in one call — preventing the
// #278 defect class (an INHERITED create surfacing kind:inherited,
// source_class:CreateModelMixin, default_status:201, error_statuses:[400] even
// though the ViewSet body is empty).
//
// HONEST-PARTIAL: a verb whose backing route carries no resolvable contract
// field simply omits that field (projectEffectiveContract leaves it zero/empty)
// — nothing is fabricated. A ViewSet with no router-expanded routes in the
// index returns an empty handlers list with a note, not an error.

import (
	"context"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// effectiveContractGroup is the per-ViewSet wire shape returned by
// archigraph_effective_contract: the owning class, its framework, and the
// per-verb effective contracts of its router-expanded routes.
type effectiveContractGroup struct {
	// Class is the owning ViewSet/controller leaf name (the grouping key).
	Class string `json:"class"`
	// Framework is the route framework ("django" for DRF), when known.
	Framework string `json:"framework,omitempty"`
	// Repo is the repo the routes were found in, for cross-repo locating.
	Repo string `json:"repo,omitempty"`
	// Handlers are the per-verb effective contracts, sorted by path then verb.
	Handlers []effectiveContract `json:"handlers"`
}

// effectiveContractResult is the top-level envelope. Groups is one entry per
// owning ViewSet (usually one, but a route/endpoint input could match across
// repos). Note carries the honest-partial / empty-result explanation.
type effectiveContractResult struct {
	Target string                   `json:"target"`
	Groups []effectiveContractGroup `json:"groups"`
	Note   string                   `json:"note,omitempty"`
}

// viewSetNameForRoute returns the owning ViewSet leaf name a router-expanded
// route belongs to, derived from its drf_view_method prop ("ViewSet.method" for
// a verb route, or just "ViewSet" for the ANY catch-all). Empty when the route
// carries no ViewSet attribution.
func viewSetNameForRoute(e *graph.Entity) string {
	dvm := e.Properties["drf_view_method"]
	if dvm == "" {
		return ""
	}
	if owning := prefixBeforeDot(dvm); owning != "" {
		return leafAfterDot(owning)
	}
	// No dot: the ANY catch-all records just the ViewSet name.
	return leafAfterDot(dvm)
}

// resolveEffectiveContractTarget determines the ViewSet leaf name to group by
// from the request's entity_id / qualified_name. Resolution order:
//
//  1. The arg matches a router-expanded route entity → derive its ViewSet.
//  2. The arg matches a class/component entity → use its leaf name.
//  3. The arg matches no entity → treat the raw string's leaf as the ViewSet
//     name (lets callers pass a bare class name that isn't itself indexed as a
//     standalone entity, e.g. when only the routes carry it).
//
// Returns the lower-cased ViewSet leaf to match routes against, plus the
// resolved entity (may be nil for case 3).
func resolveEffectiveContractTarget(lg *LoadedGroup, arg string) (string, *graph.Entity) {
	for _, r := range reposToConsider(lg, nil) {
		if r.LabelIndex == nil {
			continue
		}
		for _, e := range r.LabelIndex.LookupAll(arg) {
			if isRouterExpandedRoute(e) {
				if vs := viewSetNameForRoute(e); vs != "" {
					return strings.ToLower(vs), e
				}
			}
			if isClassEntity(e) {
				return strings.ToLower(leafAfterDot(e.Name)), e
			}
		}
	}
	// Case 3: no entity matched — fall back to the raw leaf.
	return strings.ToLower(leafAfterDot(arg)), nil
}

// handleEffectiveContract serves archigraph_effective_contract. It resolves the
// target ViewSet, gathers its router-expanded routes across the group, projects
// each into a per-verb effective contract, and groups them by owning ViewSet.
func (s *Server) handleEffectiveContract(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	target := argString(req, "entity_id", "")
	if target == "" {
		target = argString(req, "qualified_name", "")
	}
	if target == "" {
		return mcpapi.NewToolResultError("entity_id (or qualified_name) is required"), nil
	}

	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}

	wantVS, _ := resolveEffectiveContractTarget(lg, target)

	// Gather, per (repo, ViewSet), the projected per-verb contracts of every
	// router-expanded route owned by the target ViewSet. Keyed by repo so a
	// same-named ViewSet in two repos stays distinct.
	type groupKey struct{ repo, class string }
	groups := map[groupKey]*effectiveContractGroup{}
	var order []groupKey

	for _, r := range reposToConsider(lg, nil) {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isRouterExpandedRoute(e) {
				continue
			}
			vs := viewSetNameForRoute(e)
			if vs == "" || strings.ToLower(vs) != wantVS {
				continue
			}
			c, ok := projectEffectiveContract(e)
			if !ok {
				continue
			}
			key := groupKey{repo: r.Repo, class: vs}
			g, exists := groups[key]
			if !exists {
				g = &effectiveContractGroup{
					Class:     vs,
					Framework: e.Properties["framework"],
					Repo:      r.Repo,
				}
				groups[key] = g
				order = append(order, key)
			}
			g.Handlers = append(g.Handlers, c)
		}
	}

	out := effectiveContractResult{Target: target}
	// Deterministic group order: repo, then class.
	sort.Slice(order, func(i, j int) bool {
		if order[i].repo != order[j].repo {
			return order[i].repo < order[j].repo
		}
		return order[i].class < order[j].class
	})
	for _, key := range order {
		g := groups[key]
		sortEffectiveContracts(g.Handlers)
		out.Groups = append(out.Groups, *g)
	}

	if len(out.Groups) == 0 {
		out.Note = "no router-expanded routes found for this ViewSet. " +
			"The effective contract is stamped by the DRF expansion pass (T5 #3964); " +
			"if the index predates that stamping a reindex is required to populate it."
	}
	return jsonResult(out), nil
}

// sortEffectiveContracts orders per-verb contracts deterministically by path
// then verb so the output is stable across calls.
func sortEffectiveContracts(cs []effectiveContract) {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].Path != cs[j].Path {
			return cs[i].Path < cs[j].Path
		}
		return cs[i].Verb < cs[j].Verb
	})
}
