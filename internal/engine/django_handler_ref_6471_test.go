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

// ---------------------------------------------------------------------------
// #6484 round 2 — the retraction's own guards
// ---------------------------------------------------------------------------

// django6484DualCherryPy is one Python file that is BOTH a CherryPy app and a
// DRF urls.py. CherryPy's `@cherrypy.expose def users` claims (ANY, /users)
// first and gets a legitimate same-file bridge; the DRF router then composes
// the SAME (ANY, /users) id, so makeEmit's side-scoped dedup makes the Django
// emit a NO-OP. Contrived as a single file, but it is the minimal witness for
// "an earlier producer already claimed this id" — which on the corpus happens
// across any Python file whose routes two synthesizers both recognise.
const django6484DualCherryPy = `import cherrypy

from rest_framework import routers
from django.urls import path, include

from api.views import UserViewSet


class Root:
    @cherrypy.expose
    def users(self):
        return "ok"


router = routers.DefaultRouter()
router.register(r'users', UserViewSet)


urlpatterns = [
    path('', include(router.urls)),
]
`

// django6484CherryPyOtherPath pairs a CherryPy handler on ONE path with a DRF
// registration on ANOTHER, in the attribute (`views.X`) form so the Django
// synthesizer emits the historic ("Route", raw) shape — which appends no
// bridge of its own. The retraction therefore runs with the PREVIOUS route's
// bridge sitting at the tail.
const django6484CherryPyOtherPath = `import cherrypy

from rest_framework import routers
from django.urls import path, include

from api import views


class Root:
    @cherrypy.expose
    def health(self):
        return "ok"


router = routers.DefaultRouter()
router.register(r'users', views.UserViewSet)


urlpatterns = [
    path('', include(router.urls)),
]
`

// django6484FlaskTwoVerbs gives two Flask routes on the SAME path under
// DIFFERENT verbs — two bridges whose `path` property is identical — before a
// DRF registration on that path under verb ANY. The Django retraction must
// remove exactly the ONE bridge its own emit() just appended; a second
// retraction would eat `POST /users`'s.
const django6484FlaskTwoVerbs = `from flask import Flask

from rest_framework import routers
from django.urls import path, include

from api.views import UserViewSet

app = Flask(__name__)


@app.route('/users', methods=['GET'])
def list_users():
    return ""


@app.route('/users', methods=['POST'])
def create_user():
    return ""


router = routers.DefaultRouter()
router.register(r'users', UserViewSet)


urlpatterns = [
    path('', include(router.urls)),
]
`

// synthesisBridges returns every synthesis-time bridge edge as "verb path"
// keyed on the edge's FromID, for order-independent assertions.
func synthesisBridges(rels []types.RelationshipRecord) map[string]string {
	out := map[string]string{}
	for _, r := range rels {
		if r.Kind != implementsEdgeKind ||
			r.Properties.Get("pattern_type") != "http_endpoint_synthesis_time_bridge" {
			continue
		}
		out[r.FromID] = r.Properties.Get("verb") + " " + r.Properties.Get("path")
	}
	return out
}

// TestDjango6484_DedupNoOpKeepsEarlierProducersBridge is the regression probe
// for the guard emitDjangoComposed was missing. emitFile / emitResource — the
// emitters this wrapper copies — both `return` on lastEndpointIdx < 0 BEFORE
// retracting, precisely so a dedup no-op cannot retract someone else's edge.
// Without that guard the Django pass silently deletes the bridge of whichever
// producer claimed the (ANY, path) id first, re-opening the #753 dedup-ordering
// hazard in the bridge dimension.
func TestDjango6484_DedupNoOpKeepsEarlierProducersBridge(t *testing.T) {
	_, rels := detectInline(t, "python", django6471Path, django6484DualCherryPy)

	bridges := synthesisBridges(rels)
	var found bool
	for from, vp := range bridges {
		if vp == "ANY /users" && strings.Contains(from, "users") {
			found = true
		}
	}
	if !found {
		t.Errorf("#6484: CherryPy's own bridge for ANY /users was retracted by the Django pass, whose emit() was a dedup NO-OP; bridges=%v", bridges)
	}
}

// TestDjango6484_RetractionSparesOtherPathsBridge kills the widening mutant that
// drops `path == canonicalPath` from dropTrailingSynthesisTimeBridge. The Django
// ("Route", raw) shape appends no bridge, so the retraction sees the PREVIOUS
// route's bridge at the tail — a different path, which must survive.
func TestDjango6484_RetractionSparesOtherPathsBridge(t *testing.T) {
	_, rels := detectInline(t, "python", django6471Path, django6484CherryPyOtherPath)

	bridges := synthesisBridges(rels)
	var found bool
	for _, vp := range bridges {
		if vp == "ANY /health" {
			found = true
		}
	}
	if !found {
		t.Errorf("#6484: the Django retraction removed the trailing bridge for ANY /health, a DIFFERENT path than the route it was retracting; bridges=%v", bridges)
	}
}

// TestDjango6484_RetractionRemovesOnlyItsOwnBridge kills the "call
// dropTrailing... twice" mutant. Two Flask routes on /users (GET, POST) leave
// two bridges whose `path` property is identical, so the path guard alone does
// not stop a second retraction — only doing it once does.
func TestDjango6484_RetractionRemovesOnlyItsOwnBridge(t *testing.T) {
	_, rels := detectInline(t, "python", django6471Path, django6484FlaskTwoVerbs)

	bridges := synthesisBridges(rels)
	got := map[string]bool{}
	for _, vp := range bridges {
		got[vp] = true
	}
	for _, want := range []string{"GET /users", "POST /users"} {
		if !got[want] {
			t.Errorf("#6484: Flask's bridge for %s was retracted by the Django pass, which may only retract the ONE bridge its own emit() appended; bridges=%v", want, bridges)
		}
	}
	if got["ANY /users"] {
		t.Errorf("#6484: the Django cross-file bridge for ANY /users was NOT retracted; bridges=%v", bridges)
	}
}

// TestDropTrailingSynthesisTimeBridge_IsSurgical pins both halves of the
// "matches on pattern_type + path at the tail" claim the retraction rests on.
func TestDropTrailingSynthesisTimeBridge_IsSurgical(t *testing.T) {
	bridge := func(patternType, path string) types.RelationshipRecord {
		return types.RelationshipRecord{
			FromID: "scope:operation:method:python:api/urls.py:h",
			ToID:   httpEndpointDefinitionKind + ":http:ANY:" + path,
			Kind:   implementsEdgeKind,
			Properties: types.Props{
				{K: "path", V: path},
				{K: "pattern_type", V: patternType},
			},
		}
	}
	for _, tc := range []struct {
		name string
		tail types.RelationshipRecord
		drop bool
	}{
		{"matching bridge", bridge("http_endpoint_synthesis_time_bridge", "/users"), true},
		{"other path", bridge("http_endpoint_synthesis_time_bridge", "/orders"), false},
		{"other pattern_type", bridge("http_endpoint_handler_resolution", "/users"), false},
		{"other edge kind", func() types.RelationshipRecord {
			r := bridge("http_endpoint_synthesis_time_bridge", "/users")
			r.Kind = "CALLS"
			return r
		}(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keep := bridge("http_endpoint_synthesis_time_bridge", "/keep")
			in := []types.RelationshipRecord{keep, tc.tail}
			out := dropTrailingSynthesisTimeBridge(in, "/users")
			wantLen := 2
			if tc.drop {
				wantLen = 1
			}
			if len(out) != wantLen {
				t.Fatalf("dropTrailingSynthesisTimeBridge(%s) len = %d, want %d", tc.name, len(out), wantLen)
			}
			if out[0].Properties.Get("path") != "/keep" {
				t.Errorf("dropTrailingSynthesisTimeBridge ate a non-trailing edge: %v", out[0].Properties)
			}
		})
	}
}

// TestSynthesisHandlerStructuralRef_RejectsRoute pins the premise the Django
// retraction is justified on: of the three shapes synthesizeDjangoFromComposed
// emits, only ("Controller", <ViewSet>) ever appends a bridge, because
// synthesisHandlerStructuralRef rejects refKind "Route" (the Spring/Django
// path placeholder) and an empty refName outright. If "Route" were accepted,
// the historic ("Route", <own path>) shape would start minting same-file
// bridges to a symbol that is a URL, not a method.
func TestSynthesisHandlerStructuralRef_RejectsRoute(t *testing.T) {
	const file = "api/urls.py"
	for _, tc := range []struct {
		name, refKind, refName string
		want                   bool // want a non-empty ref
	}{
		{"Route placeholder", "Route", "/api/users", false},
		{"Route with empty name", "Route", "", false},
		{"empty refName", "Controller", "", false},
		{"dotted qualified name", "Controller", "views.UserViewSet", false},
		{"Controller", "Controller", "UserViewSet", true},
		{"View", "View", "UserViewSet", true},
		{"SCOPE.Operation", "SCOPE.Operation", "list_users", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := synthesisHandlerStructuralRef("python", file, tc.refKind, tc.refName)
			if (got != "") != tc.want {
				t.Errorf("synthesisHandlerStructuralRef(python, %s, %q, %q) = %q, want non-empty=%v",
					file, tc.refKind, tc.refName, got, tc.want)
			}
		})
	}
}

// django6484NestedRouter mounts a DRF router under a PARAMETERISED prefix, so
// the composed ast_driven Route's canonical path already carries a `{...}`
// placeholder (`/orgs/{org_id}/items`) by the time the #703 detail-route
// section sees it. This is the only shape that reaches that guard: a plain
// `path('items/<int:pk>/', ...)` lands as a yaml_driven Route and is dropped by
// the ast_driven gate at the top of the loop, far above it.
const django6484NestedRouter = `from django.urls import path, include
from rest_framework import routers

from api.views import ItemViewSet


router = routers.DefaultRouter()
router.register(r'items', ItemViewSet)


urlpatterns = [
    path('orgs/<int:org_id>/', include(router.urls)),
]
`

// TestDjango6484_PlaceholderPathSkipsDetailSynthesis pins #703's
// `strings.Contains(canonical, "{")` guard, which no test reached
// (TestDjango6471_DetailRouteStaysHandlerless exercises the guard's FALSE arm
// only). This is a CHARACTERIZATION pin, not an endorsement: for a router
// nested under a parameterised prefix DRF does still generate a detail route,
// so #703's stated rationale ("those routes are path()-based and already encode
// their parameter") does not actually hold for this shape. Whether the guard
// should narrow to path()-composed routes is its own measured change; until
// then this locks the behaviour so any such change is deliberate rather than
// an accident of a passing suite.
func TestDjango6484_PlaceholderPathSkipsDetailSynthesis(t *testing.T) {
	ents, _ := detectInline(t, "python", django6471Path, django6484NestedRouter)

	if ep := endpointByVerbPathAnyKind(ents, "ANY", "/orgs/{org_id}/items"); ep == nil {
		t.Fatalf("#6484: nested-router list endpoint ANY /orgs/{org_id}/items not emitted at all")
	}
	if ep := endpointByVerbPathAnyKind(ents, "ANY", "/orgs/{org_id}/items/{pk}"); ep != nil {
		t.Errorf("#6484: #703 detail synthesis fired on a canonical path that already carries a {...} placeholder, producing the two-placeholder route %q", ep.Properties["path"])
	}
}
