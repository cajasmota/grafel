package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"
)

// #4383 — PROGRAMMATIC route registration in Python web frameworks (the handler
// is passed as a VALUE to a registration method, not via a decorator) was not
// extracted at all, so the endpoint was MISSING entirely. This generalises the
// #4324 value-handler fix to:
//
//	Flask     app.add_url_rule('/users', view_func=list_users)
//	          app.add_url_rule('/users', 'users', UserView.as_view('users'))
//	          app.add_url_rule('/x', view_func=lambda: ...)
//	FastAPI   app.add_api_route('/items', get_items, methods=['GET'])
//	          router.add_api_route('/items', get_items)
//	Starlette app.add_route('/x', handler)
//	          Route('/x', endpoint=lambda: ...)
//
// Handler shape is resolved agnostically: a NAMED ref → named bridge (#4319) to
// the real def; a `lambda` → synthesized inline-handler stand-in + bridge
// (#4324). Either way the endpoint must be present AND handler-linked.

// operationByName returns the SCOPE.Operation handler entity with the given
// Name, or nil.
func operationByName(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		e := ents[i]
		if e.Kind != "SCOPE.Operation" {
			continue
		}
		if e.Name == name {
			return &ents[i]
		}
	}
	return nil
}

// withHandlerOps appends faithful SCOPE.Operation handler entities for the named
// top-level defs in `file`. The single-file engine Detect pass used by
// detectInline does not run the Python class/function extractor that, in the
// full buildDocument pipeline, lands each `def` as a SCOPE.Operation; we add
// them here so the in-pipeline merge+central-resolve can bind the #4319
// synthesis-time structural bridge to the real handler symbol — exactly as it
// would on the live graph.
func withHandlerOps(ents []types.EntityRecord, file string, names ...string) []types.EntityRecord {
	for _, n := range names {
		ents = append(ents, types.EntityRecord{
			Name:          n,
			QualifiedName: n,
			Kind:          "SCOPE.Operation",
			SourceFile:    file,
			Language:      "python",
		})
	}
	return ents
}

// assertNamedEndpointBridged proves the (verb, path) endpoint exists AND has a
// resolved inbound IMPLEMENTS edge from a NAMED handler operation (not an inline
// stand-in). It mirrors buildDocument: http-endpoint resolve → ComputeID →
// central resolve over synthesis relationships.
func assertNamedEndpointBridged(t *testing.T, ents []types.EntityRecord, rels []types.RelationshipRecord, verb, path, framework, handlerName string) {
	t.Helper()

	if endpointByVerbPath(ents, verb, path) == nil {
		t.Fatalf("[%s] endpoint %s %s NOT emitted (programmatic route dropped)", framework, verb, path)
	}
	// A named ref must NOT synthesize an inline stand-in.
	if inlineHandlerEntity(ents, verb, path) != nil {
		t.Errorf("[%s] named handler %s %s wrongly synthesized an inline stand-in", framework, verb, path)
	}

	merged, _ := ResolveHTTPEndpointHandlers(ents)
	for i := range merged {
		merged[i].ID = merged[i].ComputeID()
	}
	ep := endpointByVerbPath(merged, verb, path)
	h := operationByName(merged, handlerName)
	if ep == nil {
		t.Fatalf("[%s] endpoint lost after http-endpoint resolve pass", framework)
	}
	if h == nil {
		t.Fatalf("[%s] named handler operation %q not found", framework, handlerName)
	}

	idx := resolve.BuildIndex(merged)
	resolve.References(rels, idx)

	for _, r := range rels {
		if r.Kind == implementsEdgeKind && r.ToID == ep.ID && r.FromID == h.ID {
			return
		}
	}
	t.Fatalf("[%s] ISLAND: endpoint %s %s has no resolved IMPLEMENTS edge from named handler %q (#4383)", framework, verb, path, handlerName)
}

// --- Flask add_url_rule ------------------------------------------------------

func TestProg4383_FlaskAddURLRule_NamedAndLambda(t *testing.T) {
	src := `from flask import Flask, Blueprint
from flask.views import MethodView

app = Flask(__name__)
bp = Blueprint("api", __name__)

def list_users():
    return []

def create_user():
    return {}, 201

class UserView(MethodView):
    def get(self, user_id):
        return {}

app.add_url_rule('/users', view_func=list_users)
app.add_url_rule('/users', 'create_user', create_user, methods=['POST'])
app.add_url_rule('/users/<int:user_id>', 'user', UserView.as_view('user'))
bp.add_url_rule('/health', view_func=lambda: 'ok')
`
	ents, rels := detectInline(t, "python", "app/routes.py", src)
	ents = withHandlerOps(ents, "app/routes.py", "list_users", "create_user", "UserView")

	// Named handlers — present + named-bridged.
	assertNamedEndpointBridged(t, ents, rels, "GET", "/users", "flask", "list_users")
	assertNamedEndpointBridged(t, ents, rels, "POST", "/users", "flask", "create_user")

	// Class-based view (as_view) — endpoint present; bridged to the view class
	// name (named ref, not inline).
	if endpointByVerbPath(ents, "GET", "/users/{user_id}") == nil {
		t.Errorf("flask CBV add_url_rule as_view: endpoint missing")
	}
	if inlineHandlerEntity(ents, "GET", "/users/{user_id}") != nil {
		t.Errorf("flask CBV as_view must not synthesize an inline stand-in")
	}

	// Lambda handler — present + inline-bridged.
	assertInlineEndpointBridged(t, ents, rels, "GET", "/health", "flask")
}

// --- FastAPI add_api_route ---------------------------------------------------

func TestProg4383_FastAPIAddAPIRoute_NamedAndLambda(t *testing.T) {
	src := `from fastapi import FastAPI, APIRouter

app = FastAPI()
router = APIRouter()

def get_items():
    return []

def make_item():
    return {}

app.add_api_route('/items', get_items, methods=['GET'])
router.add_api_route('/items', make_item, methods=['POST'])
app.add_api_route('/ping', endpoint=lambda: 'pong')
`
	ents, rels := detectInline(t, "python", "app/api.py", src)
	ents = withHandlerOps(ents, "app/api.py", "get_items", "make_item")

	assertNamedEndpointBridged(t, ents, rels, "GET", "/items", "fastapi", "get_items")
	assertNamedEndpointBridged(t, ents, rels, "POST", "/items", "fastapi", "make_item")
	assertInlineEndpointBridged(t, ents, rels, "GET", "/ping", "fastapi")
}

// add_api_route defaults to GET when methods= is omitted.
func TestProg4383_FastAPIAddAPIRoute_DefaultGET(t *testing.T) {
	src := `from fastapi import APIRouter
router = APIRouter()

def health():
    return {}

router.add_api_route('/health', health)
`
	ents, rels := detectInline(t, "python", "app/health.py", src)
	ents = withHandlerOps(ents, "app/health.py", "health")
	assertNamedEndpointBridged(t, ents, rels, "GET", "/health", "fastapi", "health")
}

// --- Starlette add_route + Route(endpoint=lambda) ----------------------------

func TestProg4383_StarletteAddRoute_NamedAndLambda(t *testing.T) {
	src := `from starlette.applications import Starlette
from starlette.routing import Route

app = Starlette()

async def homepage(request):
    return None

async def submit(request):
    return None

app.add_route('/', homepage)
app.add_route('/submit', submit, methods=['POST'])
`
	ents, rels := detectInline(t, "python", "app/main.py", src)
	ents = withHandlerOps(ents, "app/main.py", "homepage", "submit")
	assertNamedEndpointBridged(t, ents, rels, "GET", "/", "starlette", "homepage")
	assertNamedEndpointBridged(t, ents, rels, "POST", "/submit", "starlette", "submit")
}

func TestProg4383_StarletteRouteLambdaEndpoint(t *testing.T) {
	src := `from starlette.routing import Route

routes = [
    Route('/ping', endpoint=lambda request: None),
]
`
	ents, rels := detectInline(t, "python", "app/routes.py", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/ping", "starlette")
}
