package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// findSynthDef returns the http_endpoint_definition entity with the given
// synthetic ID, or nil. Used by the ASGI synthesis tests to assert per-endpoint
// properties (framework, source_handler, start line) beyond mere ID presence.
func findSynthDef(res *DetectResult, id string) *types.EntityRecord {
	for i := range res.Entities {
		e := &res.Entities[i]
		if e.ID == id && (e.Kind == httpEndpointDefinitionKind || e.Kind == httpEndpointKind) {
			return e
		}
	}
	return nil
}

// TestSynth_Sanic covers @app.get / @app.route(methods=[...]) with Flask-style
// <converter:name> path params, plus Blueprint url_prefix composition. #2980.
func TestSynth_Sanic(t *testing.T) {
	src := `from sanic import Sanic, Blueprint

app = Sanic("app")
bp = Blueprint("v1", url_prefix="/v1")

@app.get("/users/<int:user_id>")
async def get_user(request, user_id):
    return {}

@app.route("/items", methods=["GET", "POST"])
async def items(request):
    return {}

@bp.get("/resource")
async def list_resource(request):
    return []

@app.route("/health")
async def health(request):
    return "ok"
`
	got, res := runDetect(t, "python", "server.py", src)
	want := []string{
		"http:GET:/users/{user_id}",
		"http:GET:/items",
		"http:POST:/items",
		"http:GET:/v1/resource",
		"http:GET:/health",
	}
	requireContains(t, got, want, "Sanic")

	// Framework label + handler attribution proof.
	e := findSynthDef(res, "http:GET:/users/{user_id}")
	if e == nil {
		t.Fatalf("Sanic: missing http:GET:/users/{user_id}")
	}
	if e.Properties["framework"] != "sanic" {
		t.Errorf("Sanic: framework = %q, want sanic", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "Controller:get_user" {
		t.Errorf("Sanic: source_handler = %q, want Controller:get_user", e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("Sanic: StartLine not stamped on get_user endpoint")
	}
}

// TestSynth_Litestar covers bare @get/@post route handlers, Controller `path`
// composition, and Router(path=...) prefix composition with {name:int} params. #2980.
func TestSynth_Litestar(t *testing.T) {
	src := `from litestar import Litestar, get, post
from litestar.controller import Controller
from litestar.router import Router

class UserController(Controller):
    path = "/users"

    @get("/{user_id:int}")
    async def get_user(self, user_id: int) -> dict:
        return {}

    @post()
    async def create_user(self, data: dict) -> dict:
        return {}

@get("/health")
async def health() -> str:
    return "ok"

router = Router(path="/api", route_handlers=[UserController])
`
	got, res := runDetect(t, "python", "app.py", src)
	want := []string{
		"http:GET:/api/users/{user_id}",
		"http:POST:/api/users",
		"http:GET:/api/health",
	}
	requireContains(t, got, want, "Litestar")

	e := findSynthDef(res, "http:GET:/api/users/{user_id}")
	if e == nil {
		t.Fatalf("Litestar: missing http:GET:/api/users/{user_id}")
	}
	if e.Properties["framework"] != "litestar" {
		t.Errorf("Litestar: framework = %q, want litestar", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:get_user" {
		t.Errorf("Litestar: source_handler = %q, want SCOPE.Operation:get_user", e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("Litestar: StartLine not stamped on get_user endpoint")
	}
}

// TestSynth_Robyn covers @app.get/@app.post bound to a Robyn(__file__) instance
// with Express-style :name path params. #2980.
func TestSynth_Robyn(t *testing.T) {
	src := `from robyn import Robyn

app = Robyn(__file__)

@app.get("/users/:id")
async def get_user(request):
    return {}

@app.post("/users")
async def create_user(request):
    return {}

app.start(port=8080)
`
	got, res := runDetect(t, "python", "app.py", src)
	want := []string{
		"http:GET:/users/{id}",
		"http:POST:/users",
	}
	requireContains(t, got, want, "Robyn")

	e := findSynthDef(res, "http:GET:/users/{id}")
	if e == nil {
		t.Fatalf("Robyn: missing http:GET:/users/{id}")
	}
	if e.Properties["framework"] != "robyn" {
		t.Errorf("Robyn: framework = %q, want robyn", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "Controller:get_user" {
		t.Errorf("Robyn: source_handler = %q, want Controller:get_user", e.Properties["source_handler"])
	}
}

// TestSynth_ASGI_NoOpOnFlask asserts the Sanic/Robyn synthesizers do not fire
// on a plain Flask file (no Sanic/Robyn marker), so the endpoint is labelled
// flask, not sanic/robyn. Guards the framework-label correctness that the
// dispatch ordering depends on. #2980.
func TestSynth_ASGI_NoOpOnFlask(t *testing.T) {
	src := `from flask import Flask

app = Flask(__name__)

@app.get("/ping")
def ping():
    return "pong"
`
	_, res := runDetect(t, "python", "flask_app.py", src)
	e := findSynthDef(res, "http:GET:/ping")
	if e == nil {
		t.Fatalf("expected http:GET:/ping endpoint")
	}
	if e.Properties["framework"] != "flask" {
		t.Errorf("framework = %q, want flask (ASGI synthesizers must no-op without their markers)", e.Properties["framework"])
	}
}
