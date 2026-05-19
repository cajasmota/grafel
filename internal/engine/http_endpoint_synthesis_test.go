package engine

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// runDetect is a small test helper that loads all framework YAML rules
// and runs the detector against a single file. It returns the synthetic
// http_endpoint IDs emitted on that file, sorted for stable assertions.
func runDetect(t *testing.T, language, path, content string) ([]string, *DetectResult) {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(content),
		Language: language,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	var ids []string
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind {
			ids = append(ids, e.ID)
		}
	}
	sort.Strings(ids)
	return ids, res
}

// requireContains asserts every wanted ID is present in got. The remainder
// is logged for diagnostic value but does not fail (the synthesis pass
// may legitimately emit additional endpoints for incidental @-pattern
// matches in the fixture).
func requireContains(t *testing.T, got, want []string, label string) {
	t.Helper()
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: missing synthetic %q (got: %v)", label, w, got)
		}
	}
}

// TestSynth_Flask covers @app.route(methods=["GET","POST"]), @bp.get(),
// and Flask path converters.
func TestSynth_Flask(t *testing.T) {
	src := `from flask import Flask, Blueprint

app = Flask(__name__)
bp = Blueprint("api", __name__)

@app.route("/users/<int:user_id>", methods=["GET", "POST"])
def get_user(user_id):
    return {}

@bp.get("/users/<int:user_id>/posts")
def list_posts(user_id):
    return []

@bp.delete("/users/<int:user_id>")
def delete_user(user_id):
    return ""

@app.route("/health")
def health():
    return "ok"
`
	got, _ := runDetect(t, "python", "app.py", src)
	want := []string{
		"http:DELETE:/users/{user_id}",
		"http:GET:/health",
		"http:GET:/users/{user_id}",
		"http:GET:/users/{user_id}/posts",
		"http:POST:/users/{user_id}",
	}
	requireContains(t, got, want, "Flask")
}

// TestSynth_FastAPI covers @app.get / @router.post including curly-brace
// path params with regex constraints.
func TestSynth_FastAPI(t *testing.T) {
	src := `from fastapi import FastAPI, APIRouter

app = FastAPI()
router = APIRouter(prefix="/v1")

@app.get("/users/{user_id}")
async def get_user(user_id: int):
    return {}

@router.post("/items")
def create_item():
    return {}

@app.delete("/users/{user_id}")
def delete_user(user_id: int):
    return None
`
	got, _ := runDetect(t, "python", "main.py", src)
	want := []string{
		"http:DELETE:/users/{user_id}",
		"http:GET:/users/{user_id}",
		"http:POST:/items",
	}
	requireContains(t, got, want, "FastAPI")
}

// TestSynth_Express covers app.get / router.post and the bare path-only
// form with an inline arrow handler.
func TestSynth_Express(t *testing.T) {
	src := "const express = require('express');\n" +
		"const app = express();\n" +
		"const router = express.Router();\n" +
		"\n" +
		"app.get('/users/:id', getUser);\n" +
		"router.post('/items', createItem);\n" +
		"app.delete('/users/:id', (req, res) => res.send(''));\n" +
		"app.all('/health', healthCheck);\n"
	got, _ := runDetect(t, "javascript", "app.js", src)
	want := []string{
		"http:ANY:/health",
		"http:DELETE:/users/{id}",
		"http:GET:/users/{id}",
		"http:POST:/items",
	}
	requireContains(t, got, want, "Express")
}

// TestSynth_Express_FalsePositiveBlocklist verifies that non-HTTP API calls
// that share the same method names as Express routes do NOT produce
// http_endpoint entities. This covers the confirmed false-positive patterns
// from audit report 2026-05-19 (issue #653).
//
// Blacklisted receivers tested here:
//   - formData.delete(key)        — FormData API (browser)
//   - urlSearchParams.get(key)    — URLSearchParams API (browser)
//   - searchParams.get(key)       — URLSearchParams alias (common)
//   - headers.delete(key)         — Headers API (browser fetch)
//   - Dimensions.get('window')    — React Native screen dimensions
//   - localStorage.getItem(key)   — Web Storage API
//   - sessionStorage.getItem(key) — Web Storage API
//   - cache.delete(key)           — Cache API (service-worker)
//   - map.get(key)                — ES6 Map
//   - set.delete(key)             — ES6 Set
func TestSynth_Express_FalsePositiveBlocklist(t *testing.T) {
	src := "// All of the following look like express verbs but are NOT HTTP routes.\n" +
		"formData.delete('cronjob_opt_in');\n" +
		"formData.delete('deficiency_proposal_pricing');\n" +
		"urlSearchParams.get('segment');\n" +
		"searchParams.get('session_expired');\n" +
		"headers.delete('Authorization');\n" +
		"Dimensions.get('window');\n" +
		"localStorage.getItem('token');\n" +
		"sessionStorage.getItem('user');\n" +
		"cache.delete('my-cache-key');\n" +
		"map.get('some-key');\n" +
		"set.delete('some-key');\n" +
		"params.get('id');\n" +
		"query.get('filter');\n"
	got, _ := runDetect(t, "javascript", "components/ui/Form.jsx", src)
	if len(got) != 0 {
		t.Errorf("expected zero http_endpoint entities from non-HTTP calls, got %v", got)
	}
}

// TestSynth_Express_ReceiverAllowlist verifies that the receiver-shape gate
// allows known Express receiver names through while blocking unknown or
// ambiguous names.
func TestSynth_Express_ReceiverAllowlist(t *testing.T) {
	// Should match: these look like Express app/router variables.
	allowed := []struct {
		receiver string
		verb     string
		path     string
	}{
		{"app", "get", "/users"},
		{"router", "post", "/items"},
		{"r", "delete", "/things/:id"},
		{"srv", "get", "/health"},
		{"server", "put", "/profile"},
		{"apiRouter", "get", "/api/v1/orders"},
		{"myApp", "post", "/submit"},
		{"httpServer", "get", "/ping"},
		{"userRouter", "delete", "/users/:id"},
	}

	for _, tc := range allowed {
		line := tc.receiver + "." + tc.verb + "('" + tc.path + "', handler);\n"
		got, _ := runDetect(t, "javascript", "server.js", line)
		if len(got) == 0 {
			t.Errorf("receiver %q with path %q: expected an http_endpoint entity, got none", tc.receiver, tc.path)
		}
	}
}

// TestSynth_Express_ReceiverBlocklistUnknown verifies that arbitrary unknown
// receiver names without any express-like suffix are NOT emitted as endpoints.
func TestSynth_Express_ReceiverBlocklistUnknown(t *testing.T) {
	// These should NOT emit HTTP endpoints even though the method name looks
	// like an Express verb — the receiver is not Express-shaped.
	blocked := []struct {
		receiver string
		verb     string
		path     string
	}{
		{"myObj", "get", "/users"},
		{"someService", "post", "/items"},
		{"helper", "delete", "/things"},
		{"config", "get", "/settings"},
	}

	for _, tc := range blocked {
		line := tc.receiver + "." + tc.verb + "('" + tc.path + "', handler);\n"
		got, _ := runDetect(t, "javascript", "utils/helper.js", line)
		if len(got) != 0 {
			t.Errorf("receiver %q: expected zero http_endpoint entities, got %v", tc.receiver, got)
		}
	}
}

// TestSynth_Express_PathGate verifies the path-shape gate: a receiver that
// looks Express-shaped but passes a non-path string (no leading `/`) must not
// emit an endpoint. This is the belt-and-suspenders layer on top of the
// receiver gate (issue #653 strategy C).
func TestSynth_Express_PathGate(t *testing.T) {
	// Even if the receiver were allowlisted, these string args are not HTTP paths.
	src := "app.get('window', handler);\n" +
		"app.delete('key', handler);\n" +
		"router.get('cronjob_opt_in', handler);\n"
	got, _ := runDetect(t, "javascript", "server.js", src)
	if len(got) != 0 {
		t.Errorf("expected zero http_endpoint entities for non-path args, got %v", got)
	}
}

// TestSynth_Express_ReactNativeDimensions specifically reproduces the
// fixture-c false positive: Dimensions.get('window') in React Native files.
func TestSynth_Express_ReactNativeDimensions(t *testing.T) {
	src := "import { Dimensions } from 'react-native';\n" +
		"const { width, height } = Dimensions.get('window');\n" +
		"const windowWidth = Dimensions.get('window').width;\n"
	got, _ := runDetect(t, "typescript", "components/ui/drawer/index.tsx", src)
	if len(got) != 0 {
		t.Errorf("Dimensions.get('window') should not produce http_endpoint entities, got %v", got)
	}
}

// TestSynth_JAXRS exercises a class-level @Path with method-level @GET +
// @Path and bare @POST without a method-level path.
func TestSynth_JAXRS(t *testing.T) {
	src := `package com.example;

import jakarta.ws.rs.GET;
import jakarta.ws.rs.POST;
import jakarta.ws.rs.Path;
import jakarta.ws.rs.PathParam;

@Path("/users")
public class UserResource {

    @GET
    @Path("/{id}")
    public User get(@PathParam("id") long id) {
        return null;
    }

    @POST
    public User create(User u) {
        return u;
    }

    @GET
    @Path("/{id}/posts")
    public List<Post> posts(@PathParam("id") long id) {
        return null;
    }
}
`
	got, _ := runDetect(t, "java", "src/main/java/com/example/UserResource.java", src)
	want := []string{
		"http:GET:/users/{id}",
		"http:GET:/users/{id}/posts",
		"http:POST:/users",
	}
	requireContains(t, got, want, "JAX-RS")
}

// TestSynth_SpringMVC verifies the synthesis pass picks up the composed
// Route entities emitted by spring_routes.go and reuses their http_method
// property to set the correct verb on the synthetic.
func TestSynth_SpringMVC(t *testing.T) {
	got, _ := runDetect(t, "java", "src/main/java/com/example/api/OrderController.java", sampleSpringController)
	want := []string{
		"http:GET:/api/orders",  // from @GetMapping
		"http:POST:/api/orders", // from @PostMapping
		"http:PUT:/api/orders/{id}",
		"http:DELETE:/api/orders/{id}",
		"http:PATCH:/api/orders/{id}",
		"http:ANY:/api/legacy", // @RequestMapping with method= kwarg → spring_routes labels ANY
	}
	requireContains(t, got, want, "Spring MVC")
}

// TestSynth_EndToEnd verifies a JAX-RS Java file and a Flask Python file
// in the same run both emit the same `http:GET:/users/{id}` synthetic ID,
// which is the precondition for cross-repo matching to work in phase 2.
func TestSynth_EndToEnd_SharedID(t *testing.T) {
	javaSrc := `package com.example;
import jakarta.ws.rs.GET;
import jakarta.ws.rs.Path;
import jakarta.ws.rs.PathParam;

@Path("/users")
public class UserResource {
    @GET
    @Path("/{id}")
    public User get(@PathParam("id") long id) { return null; }
}
`
	pySrc := `from flask import Flask
app = Flask(__name__)

@app.route("/users/<int:id>", methods=["GET"])
def get_user(id):
    return {}
`
	javaIDs, _ := runDetect(t, "java", "Java.java", javaSrc)
	pyIDs, _ := runDetect(t, "python", "py.py", pySrc)

	target := "http:GET:/users/{id}"
	if !contains(javaIDs, target) {
		t.Errorf("java side did not emit %q; got %v", target, javaIDs)
	}
	if !contains(pyIDs, target) {
		t.Errorf("python side did not emit %q; got %v", target, pyIDs)
	}
}

// TestSynth_NoOpForUnrelatedFiles ensures the pass adds nothing to files
// that contain no HTTP framework signals (regression guard for the
// bug-rate floor).
func TestSynth_NoOpForUnrelatedFiles(t *testing.T) {
	src := `package main

func main() {
	println("no http here")
}
`
	got, res := runDetect(t, "go", "main.go", src)
	if len(got) != 0 {
		t.Errorf("expected zero http_endpoint entities, got %v", got)
	}
	for _, r := range res.Relationships {
		if r.Kind == servesEdgeKind || r.Kind == implementsEdgeKind {
			t.Errorf("unexpected SERVED_BY/IMPLEMENTS edge: %+v", r)
		}
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// TestSynth_RecordsHandlerInProperty asserts that the handler reference
// is captured as a `source_handler` property on the synthetic entity.
// Phase 1 deliberately does NOT emit graph edges from the synthetic to
// the handler — emitting unresolved edges would inflate bug-rate
// because the resolver counts every dangling target as a resolution
// failure. A follow-up pass will lift `source_handler` into proper edges
// once the AST extractors emit stable controller IDs.
func TestSynth_RecordsHandlerInProperty(t *testing.T) {
	src := `from flask import Flask
app = Flask(__name__)

@app.route("/things/<int:id>", methods=["GET"])
def get_thing(id):
    return {}
`
	ids, res := runDetect(t, "python", "app.py", src)
	_ = ids

	// No synthesis edges should be emitted (phase 1 contract).
	for _, r := range res.Relationships {
		if r.Properties != nil && r.Properties["pattern_type"] == "http_endpoint_synthesis" {
			t.Errorf("phase 1 must not emit edges; saw %s -> %s (%s)", r.FromID, r.ToID, r.Kind)
		}
	}

	// The synthetic http_endpoint entity must carry the handler in its
	// `source_handler` property.
	var sawHandler bool
	for _, e := range res.Entities {
		if e.Kind != "http_endpoint" || e.ID != "http:GET:/things/{id}" {
			continue
		}
		if e.Properties != nil && strings.HasSuffix(e.Properties["source_handler"], ":get_thing") {
			sawHandler = true
		}
	}
	if !sawHandler {
		t.Error("expected synthetic http:GET:/things/{id} to carry source_handler=Controller:get_thing")
	}
}
