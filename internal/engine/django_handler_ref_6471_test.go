package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6471 / #6374 (Django's share) — the DRF-router route→handler hop must name
// the HANDLER, not the route's own path.
//
// Before this fix `synthesizeDjangoFromComposed` called
// `emit("ANY", canonical, "django", "Route", raw)` where `raw == e.Name ==
// the route PATH`. That stamps `source_handler = "Route:<own path>"`, and
// because `Route` entities ARE indexed by the phase-2 handler index
// (http_endpoint_resolve.go only excludes the three http_endpoint* kinds) the
// same-file `{Route, <path>, urls.py}` lookup found the very Route entity the
// synthesizer had been iterating — producing
//
//	Route:/api/users --IMPLEMENTS--> http_endpoint_definition:http:ANY:/api/users
//
// i.e. the graph asserting that route /api/users is implemented by route
// /api/users. This is the exact shape e15170110 (#6429) removed for Java
// Spring; that commit left "express/django's share of #6374" untouched.
//
// The port: django_routes.go stamps `handler_class` on every ast_driven Route
// it composes from a `router.register(prefix, ViewSet)` call, and the
// synthesizer emits `("Controller", <ViewSet>)`. `Controller` resolves through
// resolverKindEquivalents to `View`, the kind the Django YAML rules land a
// DRF ViewSet under.

const django6471URLs = `from rest_framework import routers
from django.urls import path, include

from api.views import UserViewSet, OrderViewSet


router = routers.DefaultRouter()
router.register(r'users', UserViewSet)
router.register(r'orders', OrderViewSet)


urlpatterns = [
    path('api/', include(router.urls)),
]
`

const django6471Path = "api/urls.py"

// django6471AttrURLs registers via an ATTRIBUTE expression (`views.UserViewSet`),
// which registerArgs deliberately does not parse — it accepts an `identifier`
// second positional only. This is the narrowing boundary of the fix: no handler
// name is in hand, so nothing may be invented for one.
const django6471AttrURLs = `from rest_framework import routers
from django.urls import path, include

from api import views


router = routers.DefaultRouter()
router.register(r'users', views.UserViewSet)


urlpatterns = [
    path('api/', include(router.urls)),
]
`

// composedRoute returns the ast_driven Route entity with the given Name.
func composedRoute(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == "Route" && e.Name == name && e.Properties != nil &&
			e.Properties["pattern_type"] == "ast_driven" {
			return e
		}
	}
	return nil
}

// endpointByVerbPathAnyKind is endpointByVerbPath widened to the legacy
// `http_endpoint` kind, which Detect still emits pre-resolve.
func endpointByVerbPathAnyKind(ents []types.EntityRecord, verb, path string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind != httpEndpointDefinitionKind && e.Kind != httpEndpointKind {
			continue
		}
		if e.Properties != nil && e.Properties["verb"] == verb && e.Properties["path"] == path {
			return e
		}
	}
	return nil
}

// TestDjango6471_ComposedRouteCarriesHandlerClass proves the AST pass carries
// the ViewSet identity forward on the Route entity, so the synthesizer — which
// only sees emitted Route records, never the AST — can stamp a handler ref.
func TestDjango6471_ComposedRouteCarriesHandlerClass(t *testing.T) {
	ents, _ := detectInline(t, "python", django6471Path, django6471URLs)

	for _, tc := range []struct{ route, viewSet string }{
		{"/api/users", "UserViewSet"},
		{"/api/orders", "OrderViewSet"},
	} {
		r := composedRoute(ents, tc.route)
		if r == nil {
			t.Fatalf("#6471: composed ast_driven Route %q not emitted", tc.route)
		}
		if got := r.Properties["handler_class"]; got != tc.viewSet {
			t.Errorf("#6471: Route %q handler_class = %q, want %q", tc.route, got, tc.viewSet)
		}
	}
}

// TestDjango6471_EndpointSourceHandlerIsController proves the synthesized
// http_endpoint names the ViewSet, not its own route path.
func TestDjango6471_EndpointSourceHandlerIsController(t *testing.T) {
	ents, _ := detectInline(t, "python", django6471Path, django6471URLs)

	for _, tc := range []struct{ path, want string }{
		{"/api/users", "Controller:UserViewSet"},
		{"/api/orders", "Controller:OrderViewSet"},
	} {
		ep := endpointByVerbPathAnyKind(ents, "ANY", tc.path)
		if ep == nil {
			t.Fatalf("#6471: endpoint ANY %s not emitted", tc.path)
		}
		got := ep.Properties["source_handler"]
		if strings.HasPrefix(got, "Route:") {
			t.Errorf("#6471: endpoint ANY %s stamps source_handler=%q — the endpoint points at its own route path, not a handler",
				tc.path, got)
		}
		if got != tc.want {
			t.Errorf("#6471: endpoint ANY %s source_handler = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// TestDjango6471_NoHandlerClassKeepsHistoricShape pins the NARROWING boundary.
// When registerArgs cannot name the ViewSet (attribute form), the fix must
// change nothing: inventing a handler ref out of the URL prefix would emit
// `Controller:users`, which resolves to no entity and — on the corpus resolve
// path, keepUnresolved=false — DELETES the endpoint rather than mis-labelling
// it. The historic ("Route", <path>) shape is the deliberate, measured-later
// residue, not an oversight.
func TestDjango6471_NoHandlerClassKeepsHistoricShape(t *testing.T) {
	ents, _ := detectInline(t, "python", django6471Path, django6471AttrURLs)

	r := composedRoute(ents, "/api/users")
	if r == nil {
		t.Fatalf("#6471: composed ast_driven Route /api/users not emitted for the attribute form")
	}
	if got, ok := r.Properties["handler_class"]; ok {
		t.Errorf("#6471: attribute-form register stamped handler_class=%q — the ViewSet name was never parsed, so any value here is invented", got)
	}
	ep := endpointByVerbPathAnyKind(ents, "ANY", "/api/users")
	if ep == nil {
		t.Fatalf("#6471: endpoint ANY /api/users not emitted for the attribute form")
	}
	if got := ep.Properties["source_handler"]; got != "Route:/api/users" {
		t.Errorf("#6471: attribute-form endpoint source_handler = %q, want the unchanged historic %q",
			got, "Route:/api/users")
	}
}

// TestDjango6471_DetailRouteStaysHandlerless pins the other boundary. #703's
// DRF auto-generated `/{pk}` detail synthetic is emitted with refName="" ON
// PURPOSE: it has no handler symbol in the source and must land in the
// NoHandlerProp keep-path. Handing it the list route's ViewSet would
// manufacture an IMPLEMENTS edge for a route no one wrote — the same class of
// confidently-wrong answer #6471 exists to remove.
func TestDjango6471_DetailRouteStaysHandlerless(t *testing.T) {
	ents, _ := detectInline(t, "python", django6471Path, django6471URLs)

	ep := endpointByVerbPathAnyKind(ents, "ANY", "/api/users/{pk}")
	if ep == nil {
		t.Fatalf("#6471: DRF detail synthetic ANY /api/users/{pk} not emitted")
	}
	if got := ep.Properties["source_handler"]; got != "" {
		t.Errorf("#6471: DRF auto-generated detail route stamps source_handler=%q; #703 requires it handler-less", got)
	}
}

// TestDjango6471_NoDanglingSameFileBridge guards the trade the fix could have
// made by accident: makeEmit's #4319 synthesis-time bridge keys its structural
// ref on the ROUTE's file, but a DRF ViewSet lives in views.py, not the urls.py
// holding `router.register`. Emitting refKind=Controller without retracting that
// bridge swaps a wrong edge for a permanently unresolvable one.
func TestDjango6471_NoDanglingSameFileBridge(t *testing.T) {
	_, rels := detectInline(t, "python", django6471Path, django6471URLs)

	for _, r := range rels {
		if r.Kind != implementsEdgeKind {
			continue
		}
		if r.Properties.Get("pattern_type") != "http_endpoint_synthesis_time_bridge" {
			continue
		}
		if strings.Contains(r.Properties.Get("path"), "/api/") {
			t.Errorf("#6471: same-file synthesis-time bridge %s -> %s survived for a cross-file DRF ViewSet handler",
				r.FromID, r.ToID)
		}
	}
}

// TestDjango6471_NoRouteSelfImplementsEdge is the graph-level gate: after the
// phase-2 resolve pass no IMPLEMENTS edge may originate at a Route entity, and
// the endpoint must instead bind to the ViewSet the Django rules land as
// `View:<name>` in views.py.
func TestDjango6471_NoRouteSelfImplementsEdge(t *testing.T) {
	ents, rels := detectInline(t, "python", django6471Path, django6471URLs)

	// Inject the ViewSet entities the Django YAML rules land from views.py.
	for _, vs := range []string{"UserViewSet", "OrderViewSet"} {
		ents = append(ents, types.EntityRecord{
			Kind:       "View",
			Name:       vs,
			SourceFile: "api/views.py",
			Language:   "python",
			Properties: map[string]string{"framework": "python"},
		})
	}

	merged, stats := ResolveHTTPEndpointHandlers(ents)
	for i := range merged {
		merged[i].ID = merged[i].ComputeID()
	}

	var all []types.RelationshipRecord
	all = append(all, rels...)
	for i := range merged {
		all = append(all, merged[i].Relationships...)
	}
	idx := resolve.BuildIndex(merged)
	resolve.References(all, idx)

	byID := map[string]*types.EntityRecord{}
	for i := range merged {
		byID[merged[i].ID] = &merged[i]
	}
	isEndpoint := func(e *types.EntityRecord) bool {
		return e != nil && (e.Kind == httpEndpointDefinitionKind ||
			e.Kind == httpEndpointCallKind || e.Kind == httpEndpointKind)
	}

	for _, r := range all {
		if r.Kind != implementsEdgeKind {
			continue
		}
		from, to := byID[r.FromID], byID[r.ToID]
		if !isEndpoint(to) || from == nil {
			continue
		}
		if from.Kind == "Route" {
			t.Errorf("#6471: IMPLEMENTS self-edge survived: Route:%s -> %s:%s",
				from.Name, to.Kind, to.Name)
		}
	}

	// And the positive direction: the ViewSet must actually bind.
	ep := endpointByVerbPathAnyKind(merged, "ANY", "/api/users")
	if ep == nil {
		t.Fatalf("#6471: endpoint ANY /api/users lost after resolve (resolved=%d dropped=%d nohandler=%d)",
			stats.HandlerResolved, stats.HandlerDropped, stats.NoHandlerProp)
	}
	var viewID string
	for i := range merged {
		if merged[i].Kind == "View" && merged[i].Name == "UserViewSet" {
			viewID = merged[i].ID
		}
	}
	if viewID == "" {
		t.Fatalf("#6471: injected View:UserViewSet missing from merged set")
	}
	found := false
	for _, r := range all {
		if r.Kind == implementsEdgeKind && r.FromID == viewID && r.ToID == ep.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("#6471: no resolved IMPLEMENTS edge View:UserViewSet -> ANY /api/users (resolved=%d dropped=%d nohandler=%d)",
			stats.HandlerResolved, stats.HandlerDropped, stats.NoHandlerProp)
	}
}
