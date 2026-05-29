package engine

import (
	"testing"
)

// TestSynth_Aiohttp covers the three aiohttp server-routing idioms:
// `app.router.add_get(...)`, `app.router.add_route("GET", ...)`, and the
// `@routes.get(...)` RouteTableDef decorator — all with FastAPI-style
// `{name}` curly-brace path params. #2979.
func TestSynth_Aiohttp(t *testing.T) {
	src := `from aiohttp import web

routes = web.RouteTableDef()

@routes.get("/users/{user_id}")
async def get_user(request):
    return web.json_response({})

@routes.post("/items")
async def create_item(request):
    return web.json_response({})

async def list_health(request):
    return web.Response(text="ok")

async def update_item(request):
    return web.Response(text="ok")

app = web.Application()
app.add_routes(routes)
app.router.add_get("/health", list_health)
app.router.add_route("PUT", "/items/{item_id}", update_item)
`
	got, res := runDetect(t, "python", "server.py", src)
	want := []string{
		"http:GET:/users/{user_id}",
		"http:POST:/items",
		"http:GET:/health",
		"http:PUT:/items/{item_id}",
	}
	requireContains(t, got, want, "aiohttp")

	// Framework label + handler attribution + def-line stamping proof for the
	// RouteTableDef decorator form.
	e := findSynthDef(res, "http:GET:/users/{user_id}")
	if e == nil {
		t.Fatalf("aiohttp: missing http:GET:/users/{user_id}")
	}
	if e.Properties["framework"] != "aiohttp" {
		t.Errorf("aiohttp: framework = %q, want aiohttp", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "Controller:get_user" {
		t.Errorf("aiohttp: source_handler = %q, want Controller:get_user", e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("aiohttp: StartLine not stamped on get_user endpoint")
	}

	// add_route generic form: verb taken from the first string argument; the
	// handler reference is the third positional arg.
	r := findSynthDef(res, "http:PUT:/items/{item_id}")
	if r == nil {
		t.Fatalf("aiohttp: missing http:PUT:/items/{item_id}")
	}
	if r.Properties["framework"] != "aiohttp" {
		t.Errorf("aiohttp: add_route framework = %q, want aiohttp", r.Properties["framework"])
	}
	if r.Properties["source_handler"] != "Controller:update_item" {
		t.Errorf("aiohttp: add_route source_handler = %q, want Controller:update_item", r.Properties["source_handler"])
	}
}

// TestSynth_Aiohttp_ClientOnlyNoOp asserts that a file using aiohttp purely as
// an HTTP client (`ClientSession`) — with no server-routing signal — does NOT
// synthesize any endpoint. Guards the dual-use skip the aiohttp.yaml rule pack
// calls out. #2979.
func TestSynth_Aiohttp_ClientOnlyNoOp(t *testing.T) {
	src := `import aiohttp

async def fetch(url):
    async with aiohttp.ClientSession() as session:
        async with session.get(url) as resp:
            return await resp.json()
`
	_, res := runDetect(t, "python", "client.py", src)
	for i := range res.Entities {
		e := &res.Entities[i]
		if e.Properties != nil && e.Properties["framework"] == "aiohttp" {
			t.Errorf("aiohttp: client-only file synthesized an endpoint %q (must no-op)", e.ID)
		}
	}
}

// TestSynth_Bottle covers `@route` (default GET + explicit method= string /
// list), `@get`, and `@post` decorators with Flask-style `<name>` /
// `<name:filter>` angle-bracket path params. #2979.
func TestSynth_Bottle(t *testing.T) {
	src := `from bottle import route, get, post, run

@route("/")
def index():
    return "home"

@get("/users/<id:int>")
def get_user(id):
    return {}

@post("/items")
def create_item():
    return {}

@route("/legacy", method="DELETE")
def legacy():
    return ""

@route("/multi", method=["GET", "POST"])
def multi():
    return ""

run(host="localhost", port=8080)
`
	got, res := runDetect(t, "python", "bottle_app.py", src)
	want := []string{
		"http:GET:/",
		"http:GET:/users/{id}",
		"http:POST:/items",
		"http:DELETE:/legacy",
		"http:GET:/multi",
		"http:POST:/multi",
	}
	requireContains(t, got, want, "Bottle")

	// Framework label + handler attribution + def-line stamping proof.
	e := findSynthDef(res, "http:GET:/users/{id}")
	if e == nil {
		t.Fatalf("Bottle: missing http:GET:/users/{id}")
	}
	if e.Properties["framework"] != "bottle" {
		t.Errorf("Bottle: framework = %q, want bottle", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "Controller:get_user" {
		t.Errorf("Bottle: source_handler = %q, want Controller:get_user", e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("Bottle: StartLine not stamped on get_user endpoint")
	}

	// Generic @route with method=[...] list composes both verbs.
	if findSynthDef(res, "http:POST:/multi") == nil {
		t.Errorf("Bottle: method=[...] list form did not synthesize POST verb")
	}
}

// TestSynth_AiohttpBottle_NoOpOnFlask asserts the aiohttp/Bottle synthesizers
// do not fire on a plain Flask file (no aiohttp/bottle marker), so the endpoint
// is labelled flask. Guards the framework-label correctness that the dispatch
// ordering depends on. #2979.
func TestSynth_AiohttpBottle_NoOpOnFlask(t *testing.T) {
	src := `from flask import Flask

app = Flask(__name__)

@app.route("/ping")
def ping():
    return "pong"
`
	_, res := runDetect(t, "python", "flask_app.py", src)
	e := findSynthDef(res, "http:GET:/ping")
	if e == nil {
		t.Fatalf("expected http:GET:/ping endpoint")
	}
	if e.Properties["framework"] != "flask" {
		t.Errorf("framework = %q, want flask (aiohttp/Bottle synthesizers must no-op without their markers)", e.Properties["framework"])
	}
}
