package dashboard

// paths_handler_resolve.go — endpoint-definition → handler resolution (#1646).
//
// DRF (and Spring/NestJS) producers emit a synthetic `http_endpoint_definition`
// entity per (verb, path) at the route-registration site (e.g. `routers.py:0`).
// That synthetic carries NO body edges: the real CALLS / QUERIES / REFERENCES /
// ACCESSES_TABLE / TESTS edges all live on the *handler* method (the ViewSet
// method, controller action, …). The handler is linked to the definition by an
// inbound `IMPLEMENTS` edge:
//
//	handler  --IMPLEMENTS-->  http_endpoint_definition
//	(also: definition --ROUTES_TO/SERVES--> handler  for some frameworks)
//
// Before #1646 the Paths detail traversed edges off the *definition* entity, so
// Called-by / Downstream / Side-effects / Tests were always empty, and the left
// tree grouped by the route-definition file (routers.py) instead of the owning
// viewset/module. This file resolves the handler and classifies its edges so the
// detail sections and grouping reflect the real handler.

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// repoEntityIndex is a per-repo lookup built once per request so the
// handler-resolution passes don't re-scan the relationship slice O(n²).
type repoEntityIndex struct {
	repo *DashRepo
	// byID maps local entity ID → entity.
	byID map[string]*graph.Entity
	// implementsTo maps a definition's local ID → the handler local IDs that
	// IMPLEMENT (or ROUTES_TO / SERVES) it.
	handlerOf map[string][]string
}

// handlerResolveEdgeKinds are the producer-side edge kinds that link a backend
// handler to an http_endpoint_definition. IMPLEMENTS is the DRF/Nest shape;
// ROUTES_TO / SERVES cover Spring and other frameworks where the definition
// points at the handler directly.
var handlerResolveEdgeKinds = map[string]bool{
	"IMPLEMENTS": true,
	"ROUTES_TO":  true,
	"SERVES":     true,
}

// buildRepoEntityIndex builds an entity index + handler-resolution map for a
// single repo document. The handlerOf map is keyed by the definition entity ID
// and resolves in BOTH directions:
//
//	handler --IMPLEMENTS--> def   (DRF/Nest: FromID is the handler)
//	def --ROUTES_TO--> handler    (Spring: ToID is the handler)
//
// so the caller can ask "who handles this definition?" regardless of framework.
func buildRepoEntityIndex(repo *DashRepo) *repoEntityIndex {
	idx := &repoEntityIndex{
		repo:      repo,
		byID:      make(map[string]*graph.Entity, len(repo.Doc.Entities)),
		handlerOf: make(map[string][]string),
	}
	for i := range repo.Doc.Entities {
		e := &repo.Doc.Entities[i]
		idx.byID[e.ID] = e
	}
	isDef := func(id string) bool {
		e := idx.byID[id]
		return e != nil && strings.EqualFold(dashStripScopePrefix(e.Kind), httpEndpointDefinitionKind)
	}
	isHandlerKind := func(id string) bool {
		e := idx.byID[id]
		if e == nil {
			return false
		}
		// A handler is anything that is NOT itself an endpoint definition: a
		// viewset method (Operation), controller action, function, etc.
		return !strings.EqualFold(dashStripScopePrefix(e.Kind), httpEndpointDefinitionKind)
	}
	for i := range repo.Doc.Relationships {
		r := &repo.Doc.Relationships[i]
		if !handlerResolveEdgeKinds[r.Kind] {
			continue
		}
		switch {
		// handler --IMPLEMENTS/SERVES--> definition  (FromID = handler)
		case isDef(r.ToID) && isHandlerKind(r.FromID):
			idx.handlerOf[r.ToID] = appendUnique(idx.handlerOf[r.ToID], r.FromID)
		// definition --ROUTES_TO/SERVES--> handler  (ToID = handler)
		case isDef(r.FromID) && isHandlerKind(r.ToID):
			idx.handlerOf[r.FromID] = appendUnique(idx.handlerOf[r.FromID], r.ToID)
		}
	}
	return idx
}

// resolveHandlers returns the handler entities for a given endpoint-definition
// entity. If the entity is not a synthetic definition (e.g. a framework that
// records the route directly on a real function) it is returned as its own
// handler so callers always get a non-empty handler to traverse.
func (idx *repoEntityIndex) resolveHandlers(defEntity *graph.Entity) []*graph.Entity {
	if defEntity == nil {
		return nil
	}
	isDef := strings.EqualFold(dashStripScopePrefix(defEntity.Kind), httpEndpointDefinitionKind)
	if !isDef {
		// Already a concrete handler / real route entity.
		return []*graph.Entity{defEntity}
	}
	ids := idx.handlerOf[defEntity.ID]
	if len(ids) == 0 {
		// Unresolved — fall back to the definition itself so existing
		// definition-level edges (e.g. retargeted FETCHES) still surface.
		return []*graph.Entity{defEntity}
	}
	out := make([]*graph.Entity, 0, len(ids))
	for _, id := range ids {
		if h := idx.byID[id]; h != nil {
			out = append(out, h)
		}
	}
	if len(out) == 0 {
		return []*graph.Entity{defEntity}
	}
	return out
}

// handlerGroupKey derives a stable viewset/module grouping key for a resolved
// handler. Preference order:
//
//  1. The class/viewset the handler belongs to ("RoleViewSet.retrieve" →
//     "RoleViewSet"; "pkg::OrderViewSet" → "OrderViewSet").
//  2. The handler's own name when it carries no class scope (a bare function
//     view, e.g. "health_check").
//
// This is the unit a developer thinks in (the viewset/controller), NOT the
// route-registration file. Returns "" when no handler name is available.
func handlerGroupKey(handler *graph.Entity) string {
	if handler == nil {
		return ""
	}
	name := handler.Name
	// Strip a leading scope prefix kind ("SCOPE.Operation:RoleViewSet.retrieve"
	// arrives here already as Name="RoleViewSet.retrieve", but be defensive).
	if i := strings.LastIndex(name, ":"); i >= 0 && i < len(name)-1 {
		// only strip when the prefix looks like a kind tag, not a package path
		if !strings.Contains(name[:i], "/") {
			name = name[i+1:]
		}
	}
	// "RoleViewSet.retrieve" → "RoleViewSet"
	if dot := strings.LastIndex(name, "."); dot > 0 {
		return name[:dot]
	}
	// "pkg::OrderViewSet" → "OrderViewSet"
	if sc := strings.LastIndex(name, "::"); sc >= 0 && sc < len(name)-2 {
		return name[sc+2:]
	}
	return name
}

// classifiedHandlerEdges holds a resolved handler's edges, split into the four
// Paths-detail sections. IDs are repo-prefixed (dashPrefixedID) so the group
// resolver can look them up cross-repo.
type classifiedHandlerEdges struct {
	calledBy    []string // inbound CALLS + inbound FETCHES (callers)
	downstream  []string // outbound CALLS to other operations/services
	sideEffects []string // DB writes / model mutation / pub-sub / external API
	tests       []string // inbound TESTS edges
}

// sideEffectEdgeKinds are outbound edge kinds that represent a mutation or
// external interaction performed by the handler.
var sideEffectEdgeKinds = map[string]bool{
	"QUERIES":       true,
	"ACCESSES_TABLE": true,
	"EMITS":         true,
	"PUBLISHES_TO":  true,
	"SUBSCRIBES_TO": true,
	"TRIGGERS":      true,
	"MAPS_TO":       true,
}

// classifyHandlerEdges walks the repo's relationships once and bucketises every
// edge touching the given handler IDs. defIDs are the endpoint-definition IDs
// the handlers implement: inbound FETCHES land on the *definition* (the
// resolver retargets caller fetches there), so they are collected as callers
// too. The handler set and definition set are evaluated against local IDs.
func classifyHandlerEdges(idx *repoEntityIndex, handlerIDs, defIDs []string) classifiedHandlerEdges {
	handlerSet := make(map[string]bool, len(handlerIDs))
	for _, id := range handlerIDs {
		handlerSet[id] = true
	}
	defSet := make(map[string]bool, len(defIDs))
	for _, id := range defIDs {
		defSet[id] = true
	}
	slug := idx.repo.Slug
	var out classifiedHandlerEdges
	for i := range idx.repo.Doc.Relationships {
		r := &idx.repo.Doc.Relationships[i]
		// Outbound from a handler.
		if handlerSet[r.FromID] {
			switch {
			case r.Kind == "CALLS":
				// Skip external stubs (Response/save/get…) — they're noise; keep
				// only edges that resolve to a real intra-graph entity.
				if t := idx.byID[r.ToID]; t != nil && !isExternalStub(t) {
					out.downstream = append(out.downstream, dashPrefixedID(slug, r.ToID))
				}
			case sideEffectEdgeKinds[r.Kind]:
				out.sideEffects = append(out.sideEffects, dashPrefixedID(slug, r.ToID))
			case r.Kind == "REFERENCES":
				// REFERENCES to a Model/datastore is a DB touch; ignore plain
				// symbol references to keep the section signal-dense.
				if t := idx.byID[r.ToID]; t != nil && isDataEntity(t) {
					out.sideEffects = append(out.sideEffects, dashPrefixedID(slug, r.ToID))
				}
			}
		}
		// Inbound to a handler.
		if handlerSet[r.ToID] {
			switch r.Kind {
			case "CALLS":
				out.calledBy = append(out.calledBy, dashPrefixedID(slug, r.FromID))
			case "TESTS":
				out.tests = append(out.tests, dashPrefixedID(slug, r.FromID))
			}
		}
		// Inbound to the definition: retargeted caller FETCHES + TESTS.
		if defSet[r.ToID] {
			switch r.Kind {
			case "FETCHES":
				out.calledBy = append(out.calledBy, dashPrefixedID(slug, r.FromID))
			case "TESTS":
				out.tests = append(out.tests, dashPrefixedID(slug, r.FromID))
			}
		}
	}
	out.calledBy = dedupStrings(out.calledBy)
	out.downstream = dedupStrings(out.downstream)
	out.sideEffects = dedupStrings(out.sideEffects)
	out.tests = dedupStrings(out.tests)
	return out
}

// isExternalStub reports whether an entity is a synthetic external-call stub
// (SCOPE.External:save, builtin Response, etc.) that adds noise rather than
// signal to the Downstream section.
func isExternalStub(e *graph.Entity) bool {
	k := strings.ToLower(dashStripScopePrefix(e.Kind))
	if strings.Contains(k, "external") {
		// External *services* (real third-party APIs) are signal; bare external
		// call stubs with no source file are noise.
		return e.SourceFile == ""
	}
	return false
}

// isDataEntity reports whether an entity is a persistence target (DB model,
// table, datastore) — i.e. a REFERENCES/QUERIES to it is a real side effect.
func isDataEntity(e *graph.Entity) bool {
	k := strings.ToLower(dashStripScopePrefix(e.Kind))
	switch {
	case strings.Contains(k, "model"),
		strings.Contains(k, "table"),
		strings.Contains(k, "datastore"),
		strings.Contains(k, "dataaccess"),
		strings.Contains(k, "database"),
		strings.Contains(k, "collection"):
		return true
	}
	return false
}

// appendUnique appends s to sl only if not already present.
func appendUnique(sl []string, s string) []string {
	for _, v := range sl {
		if v == s {
			return sl
		}
	}
	return append(sl, s)
}

