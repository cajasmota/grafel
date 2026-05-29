package engine

import (
	"testing"
)

// TestSynth_CherryPy covers @cherrypy.expose decorated methods. #3065.
// The synthesizer derives the URL from the method name: `index` → `/`,
// any other method name → `/<method>`.
func TestSynth_CherryPy(t *testing.T) {
	src := `import cherrypy

class Root:
    @cherrypy.expose
    def index(self):
        return "Hello World!"

    @cherrypy.expose
    def users(self):
        return "[]"

    @cherrypy.expose()
    def items(self):
        return "[]"

cherrypy.quickstart(Root())
`
	got, res := runDetect(t, "python", "server.py", src)
	want := []string{
		"http:ANY:/",
		"http:ANY:/users",
		"http:ANY:/items",
	}
	requireContains(t, got, want, "CherryPy")

	// Framework label + handler attribution + def-line stamping.
	e := findSynthDef(res, "http:ANY:/users")
	if e == nil {
		t.Fatalf("CherryPy: missing http:ANY:/users")
	}
	if e.Properties["framework"] != "cherrypy" {
		t.Errorf("CherryPy: framework = %q, want cherrypy", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "Controller:users" {
		t.Errorf("CherryPy: source_handler = %q, want Controller:users", e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("CherryPy: StartLine not stamped on users handler")
	}

	// index → /
	idx := findSynthDef(res, "http:ANY:/")
	if idx == nil {
		t.Fatalf("CherryPy: missing http:ANY:/ (index method)")
	}
	if idx.Properties["framework"] != "cherrypy" {
		t.Errorf("CherryPy: index framework = %q, want cherrypy", idx.Properties["framework"])
	}
}

// TestSynth_CherryPy_NoOpOnFlask asserts the CherryPy synthesizer does not
// fire on a plain Flask file that shares no CherryPy markers.
func TestSynth_CherryPy_NoOpOnFlask(t *testing.T) {
	src := `from flask import Flask

app = Flask(__name__)

@app.route("/ping")
def ping():
    return "pong"
`
	_, res := runDetect(t, "python", "flask_app.py", src)
	e := findSynthDef(res, "http:GET:/ping")
	if e == nil {
		t.Fatalf("expected http:GET:/ping endpoint from Flask")
	}
	if e.Properties["framework"] != "flask" {
		t.Errorf("framework = %q, want flask (CherryPy synthesizer must no-op)", e.Properties["framework"])
	}
}

// TestSynth_Falcon covers app.add_route + on_get/on_post responder methods. #3065.
func TestSynth_Falcon(t *testing.T) {
	src := `import falcon

class UserResource:
    def on_get(self, req, resp, user_id):
        resp.media = {}

    def on_post(self, req, resp):
        resp.media = {}

class ItemResource:
    def on_get(self, req, resp):
        resp.media = []

app = falcon.App()
app.add_route('/users/{user_id}', UserResource())
app.add_route('/items', ItemResource())
`
	got, res := runDetect(t, "python", "api.py", src)
	want := []string{
		"http:GET:/users/{user_id}",
		"http:POST:/users/{user_id}",
		"http:GET:/items",
	}
	requireContains(t, got, want, "Falcon")

	// Framework label + handler attribution + def-line stamping for the
	// on_get responder on UserResource.
	e := findSynthDef(res, "http:GET:/users/{user_id}")
	if e == nil {
		t.Fatalf("Falcon: missing http:GET:/users/{user_id}")
	}
	if e.Properties["framework"] != "falcon" {
		t.Errorf("Falcon: framework = %q, want falcon", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "SCOPE.Operation:UserResource.on_get" {
		t.Errorf("Falcon: source_handler = %q, want SCOPE.Operation:UserResource.on_get", e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("Falcon: StartLine not stamped on on_get handler")
	}

	// POST on the same resource.
	p := findSynthDef(res, "http:POST:/users/{user_id}")
	if p == nil {
		t.Fatalf("Falcon: missing http:POST:/users/{user_id}")
	}
	if p.Properties["framework"] != "falcon" {
		t.Errorf("Falcon: POST framework = %q, want falcon", p.Properties["framework"])
	}
}

// TestSynth_Falcon_CrossFileResource asserts that when the Resource class is
// not defined in the same file an ANY synthetic is still emitted. #3065.
func TestSynth_Falcon_CrossFileResource(t *testing.T) {
	src := `import falcon
from resources import OrderResource

app = falcon.App()
app.add_route('/orders', OrderResource())
`
	got, _ := runDetect(t, "python", "main.py", src)
	want := []string{"http:ANY:/orders"}
	requireContains(t, got, want, "Falcon cross-file")
}

// TestSynth_Hug covers @hug.get / @hug.post decorator routing. #3065.
func TestSynth_Hug(t *testing.T) {
	src := `import hug

@hug.get('/users/{user_id}')
def get_user(user_id: int):
    return {}

@hug.post('/items')
def create_item(body):
    return {}

@hug.put('/items/{item_id}')
def update_item(item_id: int, body):
    return {}

@hug.delete('/items/{item_id}')
def delete_item(item_id: int):
    pass
`
	got, res := runDetect(t, "python", "api.py", src)
	want := []string{
		"http:GET:/users/{user_id}",
		"http:POST:/items",
		"http:PUT:/items/{item_id}",
		"http:DELETE:/items/{item_id}",
	}
	requireContains(t, got, want, "Hug")

	// Framework label + handler attribution + def-line stamping.
	e := findSynthDef(res, "http:GET:/users/{user_id}")
	if e == nil {
		t.Fatalf("Hug: missing http:GET:/users/{user_id}")
	}
	if e.Properties["framework"] != "hug" {
		t.Errorf("Hug: framework = %q, want hug", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "Controller:get_user" {
		t.Errorf("Hug: source_handler = %q, want Controller:get_user", e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("Hug: StartLine not stamped on get_user handler")
	}
}

// TestSynth_Hug_NoOpOnFlask asserts Hug synthesizer does not fire on Flask. #3065.
func TestSynth_Hug_NoOpOnFlask(t *testing.T) {
	src := `from flask import Flask

app = Flask(__name__)

@app.get("/ping")
def ping():
    return "pong"
`
	_, res := runDetect(t, "python", "flask_app.py", src)
	e := findSynthDef(res, "http:GET:/ping")
	if e == nil {
		t.Fatalf("expected http:GET:/ping endpoint from Flask")
	}
	if e.Properties["framework"] != "flask" {
		t.Errorf("framework = %q, want flask (Hug synthesizer must no-op on Flask files)", e.Properties["framework"])
	}
}

// TestSynth_Quart covers @app.route(methods=...), @app.get(), @app.post()
// decorators — the async-Flask API. #3065.
func TestSynth_Quart(t *testing.T) {
	src := `from quart import Quart, Blueprint

app = Quart(__name__)
bp = Blueprint("api", __name__)

@app.route("/users/<int:user_id>", methods=["GET", "POST"])
async def get_user(user_id):
    return {}

@bp.get("/users/<int:user_id>/posts")
async def list_posts(user_id):
    return []

@bp.delete("/users/<int:user_id>")
async def delete_user(user_id):
    pass

@app.route("/health")
async def health():
    return "ok"
`
	got, res := runDetect(t, "python", "app.py", src)
	want := []string{
		"http:GET:/users/{user_id}",
		"http:POST:/users/{user_id}",
		"http:GET:/users/{user_id}/posts",
		"http:DELETE:/users/{user_id}",
		"http:GET:/health",
	}
	requireContains(t, got, want, "Quart")

	// Framework label + handler attribution + def-line stamping proof for
	// the @app.get shorthand form.
	e := findSynthDef(res, "http:GET:/users/{user_id}/posts")
	if e == nil {
		t.Fatalf("Quart: missing http:GET:/users/{user_id}/posts")
	}
	if e.Properties["framework"] != "quart" {
		t.Errorf("Quart: framework = %q, want quart", e.Properties["framework"])
	}
	if e.Properties["source_handler"] != "Controller:list_posts" {
		t.Errorf("Quart: source_handler = %q, want Controller:list_posts", e.Properties["source_handler"])
	}
	if e.StartLine == 0 {
		t.Errorf("Quart: StartLine not stamped on list_posts handler")
	}

	// methods=[...] list should produce both GET and POST synthetics.
	if findSynthDef(res, "http:POST:/users/{user_id}") == nil {
		t.Errorf("Quart: methods=[GET,POST] did not synthesize POST verb")
	}
}

// TestSynth_Quart_NoOpOnFlask asserts the Quart synthesizer does not fire on
// a plain Flask file (no quart import). #3065.
func TestSynth_Quart_NoOpOnFlask(t *testing.T) {
	src := `from flask import Flask

app = Flask(__name__)

@app.route("/ping")
def ping():
    return "pong"
`
	_, res := runDetect(t, "python", "flask_app.py", src)
	e := findSynthDef(res, "http:GET:/ping")
	if e == nil {
		t.Fatalf("expected http:GET:/ping endpoint from Flask")
	}
	if e.Properties["framework"] != "flask" {
		t.Errorf("framework = %q, want flask (Quart synthesizer must no-op on pure Flask files)", e.Properties["framework"])
	}
}
