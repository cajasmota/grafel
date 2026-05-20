package engine

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
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

// ---------------------------------------------------------------------------
// Express false-positive guard tests — round 1 (#653) + round 2 (#684)
// ---------------------------------------------------------------------------

// TestSynth_Express_FalsePositiveBlocklist verifies that non-HTTP API calls
// that share the same method names as Express routes do NOT produce
// express-producer http_endpoint entities. This covers the confirmed
// false-positive patterns from issues #653 and #684.
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
	_, res := runDetect(t, "javascript", "components/ui/Form.jsx", src)
	if hasExpressProducer(res) {
		t.Errorf("expected zero express producer entities from non-HTTP calls, got %v", expressProducerIDs(res))
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
// receiver names without any express-like suffix are NOT emitted as Express
// producer endpoints.
func TestSynth_Express_ReceiverBlocklistUnknown(t *testing.T) {
	// These should NOT emit express producer entities even though the method
	// name looks like an Express verb — the receiver is not Express-shaped.
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
		_, res := runDetect(t, "javascript", "utils/helper.js", line)
		if hasExpressProducer(res) {
			t.Errorf("receiver %q: expected zero express producer entities, got %v", tc.receiver, expressProducerIDs(res))
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
	_, res := runDetect(t, "javascript", "server.js", src)
	if hasExpressProducer(res) {
		t.Errorf("expected zero express producer entities for non-path args, got %v", expressProducerIDs(res))
	}
}

// TestSynth_Express_ReactNativeDimensions specifically reproduces the
// fixture-c false positive: Dimensions.get('window') in React Native files.
func TestSynth_Express_ReactNativeDimensions(t *testing.T) {
	src := "import { Dimensions } from 'react-native';\n" +
		"const { width, height } = Dimensions.get('window');\n" +
		"const windowWidth = Dimensions.get('window').width;\n"
	_, res := runDetect(t, "typescript", "components/ui/drawer/index.tsx", src)
	if hasExpressProducer(res) {
		t.Errorf("Dimensions.get('window') must not produce express producer entities, got %v", expressProducerIDs(res))
	}
}

// ---------------------------------------------------------------------------
// Express false-positive guard tests — round 2 only (#684)
// Consumer HTTP-client wrapper variables ($http, *Client, axios.create)
// ---------------------------------------------------------------------------

// TestSynth_Express_DollarHttp_NotProducer verifies that `$http.get('/path')`
// — the Angular/Vue axios-instance pattern — is NOT emitted as an Express
// producer. This was the primary false-positive reported in fixture-e (#684).
// Note: the consumer-side synthesizer (synthesizeFetchAxios) may legitimately
// emit an http_endpoint_client_synthesis entity for the same call; we only
// assert that NO express producer entity is emitted.
func TestSynth_Express_DollarHttp_NotProducer(t *testing.T) {
	src := "import {$http} from '../../utils/http.utils';\n" +
		"\n" +
		"class BranchesService {\n" +
		"  allBranches = () => $http.get('/branches')\n" +
		"  assignedUsers = ({branchId}) => $http.get(`/branches/${branchId}/users`)\n" +
		"}\n"
	_, res := runDetect(t, "javascript", "src/store/branches/branches.service.js", src)
	if hasExpressProducer(res) {
		t.Errorf("$http.get('/branches') must NOT emit Express producer entity, got express producers: %v", expressProducerIDs(res))
	}
}

// TestSynth_Express_ApiClient_NotProducer verifies that `apiClient.post('/path')`
// is not classified as an Express route (#684).
// Note: synthesizeFetchAxios will emit a consumer-side entity for this call;
// we only assert that NO express producer entity is emitted.
func TestSynth_Express_ApiClient_NotProducer(t *testing.T) {
	src := "import apiClient from '../http';\n" +
		"\n" +
		"export function createOrder(body) {\n" +
		"  return apiClient.post('/orders', body);\n" +
		"}\n"
	_, res := runDetect(t, "javascript", "src/api/orders.js", src)
	if hasExpressProducer(res) {
		t.Errorf("apiClient.post('/orders') must NOT emit Express producer entity, got express producers: %v", expressProducerIDs(res))
	}
}

// TestSynth_Express_CustomClient_NotProducer verifies that a custom client
// variable named `myCustomClient` is not classified as an Express route (#684).
// Note: synthesizeFetchAxios will emit a consumer-side entity for this call;
// we only assert that NO express producer entity is emitted.
func TestSynth_Express_CustomClient_NotProducer(t *testing.T) {
	src := "const myCustomClient = new HttpClient();\n" +
		"myCustomClient.delete('/foo');\n"
	_, res := runDetect(t, "javascript", "src/services/foo.service.js", src)
	if hasExpressProducer(res) {
		t.Errorf("myCustomClient.delete('/foo') must NOT emit Express producer entity, got express producers: %v", expressProducerIDs(res))
	}
}

// TestSynth_Express_AxiosCreate_SymbolTable verifies the per-file symbol table
// check: `const $http = axios.create(...)` marks `$http` as a known HTTP
// client, so `$http.get('/path')` must not be classified as an Express producer
// even if the name would otherwise pass the allowlist (#684 fix C).
func TestSynth_Express_AxiosCreate_SymbolTable(t *testing.T) {
	src := "import axios from 'axios';\n" +
		"\n" +
		"const $http = axios.create({\n" +
		"  baseURL: process.env.API_URL,\n" +
		"});\n" +
		"\n" +
		"export const getUsers = () => $http.get('/users');\n" +
		"export const createUser = (data) => $http.post('/users', data);\n"
	_, res := runDetect(t, "javascript", "src/utils/http.utils.js", src)
	if hasExpressProducer(res) {
		t.Errorf("axios.create instance $http.get('/users') must NOT emit Express producer, got express producers: %v", expressProducerIDs(res))
	}
}

// TestSynth_Express_KyCreate_SymbolTable verifies symbol table detection
// also works for ky.create(...) instances (#684 fix C).
func TestSynth_Express_KyCreate_SymbolTable(t *testing.T) {
	src := "import ky from 'ky';\n" +
		"\n" +
		"const apiClient = ky.create({ prefixUrl: '/api' });\n" +
		"\n" +
		"export const fetchBranches = () => apiClient.get('/branches');\n"
	_, res := runDetect(t, "javascript", "src/api/branches.js", src)
	if hasExpressProducer(res) {
		t.Errorf("ky.create instance apiClient.get('/branches') must NOT emit Express producer, got express producers: %v", expressProducerIDs(res))
	}
}

// TestSynth_Express_RealRoutes_NotRejected ensures the round-2 blocklist
// additions did NOT accidentally break real Express route extraction.
// This is the regression guard for #684.
func TestSynth_Express_RealRoutes_NotRejected(t *testing.T) {
	src := "const express = require('express');\n" +
		"const app = express();\n" +
		"const router = express.Router();\n" +
		"\n" +
		"app.get('/route', handler);\n" +
		"router.post('/route', handler);\n" +
		"app.delete('/users/:id', deleteUser);\n" +
		"router.put('/users/:id', updateUser);\n"
	got, _ := runDetect(t, "javascript", "server.js", src)
	want := []string{
		"http:DELETE:/users/{id}",
		"http:GET:/route",
		"http:POST:/route",
		"http:PUT:/users/{id}",
	}
	requireContains(t, got, want, "Express real routes must survive round-2 blocklist")
}

// TestSynth_Express_ClientNamedHttp_NotProducer verifies that variables
// named `http`, `client`, `request`, `xhr` are not emitted as Express
// producers — these are generic consumer-client naming conventions (#684).
// Note: synthesizeFetchAxios may legitimately emit consumer-side entities;
// we only assert no express producer entities are emitted.
func TestSynth_Express_ClientNamedHttp_NotProducer(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"http", "http.get('/users');\n"},
		{"client", "client.post('/items');\n"},
		{"request", "request.delete('/things/1');\n"},
		{"xhr", "xhr.get('/data');\n"},
		{"api", "api.get('/api/v1/users');\n"},
	}
	for _, tc := range cases {
		_, res := runDetect(t, "javascript", "src/services/api.js", tc.content)
		if hasExpressProducer(res) {
			t.Errorf("consumer receiver %q: must NOT emit express producer entities, got %v", tc.name, expressProducerIDs(res))
		}
	}
}

// TestSynth_Express_ServiceSuffix_NotProducer verifies that variables ending
// in `Service` (e.g. `branchesService`, `userService`) are blocked — these
// are Angular/React service classes not Express routers (#684).
func TestSynth_Express_ServiceSuffix_NotProducer(t *testing.T) {
	src := "branchesService.get('/branches');\n" +
		"userService.post('/users', data);\n"
	_, res := runDetect(t, "javascript", "src/store/app.service.js", src)
	if hasExpressProducer(res) {
		t.Errorf("*Service suffix receivers must not emit Express producer entities, got %v", expressProducerIDs(res))
	}
}

// TestSynth_Express_BranchesService_FixtureE_Pattern reproduces the exact
// fixture-e false-positive pattern from the #684 audit: a BranchesService
// class importing $http from utils and calling $http.get('/branches').
// This must emit ZERO producer-side (framework=express) http_endpoint entities.
// Consumer-side entities from synthesizeFetchAxios are allowed and expected.
func TestSynth_Express_BranchesService_FixtureE_Pattern(t *testing.T) {
	src := "import {$http} from '../../utils/http.utils';\n" +
		"\n" +
		"class BranchesService {\n" +
		"  allBranches = () => $http.get('/branches')\n" +
		"  countBranches = () => $http.get('/branches/count')\n" +
		"  createBranch = (data) => $http.post('/branches', data)\n" +
		"  updateBranch = (id, data) => $http.put(`/branches/${id}`, data)\n" +
		"  deleteBranch = (id) => $http.delete(`/branches/${id}`)\n" +
		"}\n"
	_, res := runDetect(t, "javascript", "src/store/branches/branches.service.js", src)
	if hasExpressProducer(res) {
		t.Errorf("BranchesService $http calls must NOT be express producer; got express producer entities: %v", expressProducerIDs(res))
	}
}

// TestSynth_Express_buildClientSymbolTable unit-tests the symbol table builder
// directly, verifying it captures axios.create, ky.create, and got.extend
// variable names correctly without touching synthesizeExpress.
func TestSynth_Express_buildClientSymbolTable(t *testing.T) {
	content := "const $http = axios.create({ baseURL: '/api' });\n" +
		"const kyClient = ky.create({ prefixUrl: '/v1' });\n" +
		"const myHttp = got.extend({ prefixUrl: 'http://host' });\n" +
		"const normalVar = someOtherFunc();\n"
	symbols := buildExpressClientSymbolTable(content)
	for _, want := range []string{"$http", "kyClient", "myHttp"} {
		if !symbols[want] {
			t.Errorf("buildExpressClientSymbolTable: expected %q in symbol table, got %v", want, symbols)
		}
	}
	if symbols["normalVar"] {
		t.Error("buildExpressClientSymbolTable: normalVar must NOT be in symbol table")
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

// hasExpressProducer returns true if any http_endpoint entity in res was
// emitted with framework=express (i.e. was classified as an Express producer
// route). Consumer-side entities (framework=http_client, axios, fetch, etc.)
// are intentionally ignored — they come from synthesizeFetchAxios and are
// correct. Used by #684 fix-validation tests to assert zero false producers
// while allowing the consumer-side pass to run.
func hasExpressProducer(res *DetectResult) bool {
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind && e.Properties != nil && e.Properties["framework"] == "express" {
			return true
		}
	}
	return false
}

// expressProducerIDs returns the IDs of all http_endpoint entities emitted as
// Express producer routes (framework=express). Used in assertions that need to
// report which specific entities are the false positives.
func expressProducerIDs(res *DetectResult) []string {
	var out []string
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind && e.Properties != nil && e.Properties["framework"] == "express" {
			out = append(out, e.ID)
		}
	}
	return out
}

// TestSynth_DjangoComposed_SingleDetailPlaceholder verifies that
// synthesizeDjangoFromComposed emits exactly ONE {pk} detail-route
// variant per ast_driven list route — not the three-variant set
// ({pk}/{id}/{param}) that the pre-#730 workaround produced. The
// #704 byPath normalizer handles cross-placeholder matching at lookup
// time, so a single emission is sufficient.
func TestSynth_DjangoComposed_SingleDetailPlaceholder(t *testing.T) {
	// Simulate the entity slice that django_routes.go emits for a DRF
	// router.register(r"users", UserViewSet) where the AST pass composes
	// the parent path("api/v1/", include(...)) prefix.
	composedRoute := types.EntityRecord{
		ID:         "ast:Route:/api/v1/users",
		Name:       "/api/v1/users",
		Kind:       "Route",
		SourceFile: "api/urls.py",
		Language:   "python",
		Properties: map[string]string{
			"framework":    "python",
			"pattern_type": "ast_driven",
		},
	}

	var emitted []string
	emitFnCapture := func(method, canonicalPath, framework, refKind, refName string) {
		id := httproutes.SyntheticID(method, canonicalPath)
		emitted = append(emitted, id)
	}
	synthesizeDjangoFromComposed(
		[]types.EntityRecord{composedRoute},
		"api/urls.py",
		emitFnCapture,
	)

	// Must have the single {pk} detail-route variant.
	found := false
	for _, id := range emitted {
		if id == "http:ANY:/api/v1/users/{pk}" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected http:ANY:/api/v1/users/{pk} to be emitted; got %v", emitted)
	}

	// Must NOT have {id} or {param} variants — those were the pre-#730
	// multi-emit workaround and are no longer needed.
	for _, id := range emitted {
		if id == "http:ANY:/api/v1/users/{id}" {
			t.Errorf("unexpected {id} variant present (pre-#730 workaround must be removed): %v", emitted)
		}
		if id == "http:ANY:/api/v1/users/{param}" {
			t.Errorf("unexpected {param} variant present (pre-#730 workaround must be removed): %v", emitted)
		}
	}
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

// ---------------------------------------------------------------------------
// #748 — FastAPI / Django YAML rule collision regression tests
// ---------------------------------------------------------------------------

// TestSynth_FastAPI_VerbSpecific_NotANY is the primary regression test for
// #748. FastAPI endpoints declared with @app.get / @router.post / etc. MUST
// emit verb-specific http_endpoint IDs (http:GET:, http:POST:, …), NOT the
// catch-all http:ANY: that synthesizeDjangoFromComposed would have produced
// before the fix.
//
// Pre-fix behaviour: the Django YAML path() pattern matched any `path("...")`
// call in a Python file, producing yaml_driven Route entities with
// framework=python. synthesizeDjangoFromComposed consumed those entities
// (it previously accepted both ast_driven and yaml_driven) and emitted
// http:ANY:… synthetics. FastAPI synthesis ran afterwards but the dedup-by-ID
// `seen` map in applyHTTPEndpointSynthesis made it skip the already-claimed
// path. Result: FastAPI endpoints emerged as ANY-verb.
//
// Post-fix: synthesizeDjangoFromComposed skips yaml_driven routes. FastAPI
// synthesis claims all paths first with their proper verbs.
func TestSynth_FastAPI_VerbSpecific_NotANY(t *testing.T) {
	src := `from fastapi import FastAPI, APIRouter

app = FastAPI()
router = APIRouter(prefix="/v1")

@app.get("/users")
async def list_users():
    return []

@app.get("/users/{user_id}")
async def get_user(user_id: int):
    return {}

@router.post("/items")
def create_item():
    return {}

@app.delete("/users/{user_id}")
def delete_user(user_id: int):
    return None

@app.patch("/users/{user_id}")
def update_user(user_id: int):
    return {}
`
	got, res := runDetect(t, "python", "main.py", src)

	// All of these must be present — and with the specific verb.
	want := []string{
		"http:DELETE:/users/{user_id}",
		"http:GET:/users",
		"http:GET:/users/{user_id}",
		"http:PATCH:/users/{user_id}",
		"http:POST:/items",
	}
	requireContains(t, got, want, "FastAPI verb-specific")

	// None of the above paths may appear with ANY verb — that is the
	// specific regression from #748.
	anyVerbPaths := []string{
		"http:ANY:/users",
		"http:ANY:/users/{user_id}",
		"http:ANY:/items",
	}
	for _, forbidden := range anyVerbPaths {
		for _, id := range got {
			if id == forbidden {
				t.Errorf("FastAPI endpoint %q emitted as ANY-verb (Django yaml_driven collision regression #748)", forbidden)
			}
		}
	}

	// Verify the verb property is correct on each emitted entity.
	verbFor := map[string]string{}
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind {
			verbFor[e.ID] = e.Properties["verb"]
		}
	}
	for _, id := range want {
		// Extract expected verb from ID "http:VERB:path"
		parts := strings.SplitN(id, ":", 3)
		if len(parts) < 3 {
			continue
		}
		expectedVerb := parts[1]
		if verbFor[id] != expectedVerb {
			t.Errorf("entity %q has verb=%q, want %q", id, verbFor[id], expectedVerb)
		}
	}
}

// TestSynth_FastAPI_RouterVerbSpecific verifies that named routers other than
// `router` (e.g. `items_router`, `api_router`) also emit verb-specific
// endpoints, not ANY.
func TestSynth_FastAPI_RouterVerbSpecific(t *testing.T) {
	src := `from fastapi import FastAPI, APIRouter

app = FastAPI()
items_router = APIRouter(prefix="/items")
users_router = APIRouter(prefix="/users")

@items_router.get("/")
async def list_items():
    return []

@items_router.post("/")
async def create_item():
    return {}

@users_router.get("/{user_id}")
async def get_user(user_id: int):
    return {}
`
	got, _ := runDetect(t, "python", "routers/items.py", src)
	want := []string{
		"http:GET:/",
		"http:POST:/",
		"http:GET:/{user_id}",
	}
	requireContains(t, got, want, "FastAPI named router verb-specific")

	// No ANY-verb synthetics for these paths.
	for _, id := range got {
		if strings.HasPrefix(id, "http:ANY:") {
			t.Errorf("unexpected ANY-verb entity %q from FastAPI named router (#748)", id)
		}
	}
}

// TestSynth_FastAPI_YamlDrivenRouteNotSynthesized_Unit is a direct unit test
// of synthesizeDjangoFromComposed — it feeds it a yaml_driven Route entity
// (the kind that the Django YAML path() pattern would produce for a FastAPI
// file) and asserts that NO http_endpoint is emitted.
func TestSynth_FastAPI_YamlDrivenRouteNotSynthesized_Unit(t *testing.T) {
	// Simulate a yaml_driven Route that the Django YAML path() pattern
	// would produce when it fires on a FastAPI file containing path("...").
	yamlDrivenFastapiRoute := types.EntityRecord{
		ID:         "yaml:Route:/users",
		Name:       "/users",
		Kind:       "Route",
		SourceFile: "main.py",
		Language:   "python",
		Properties: map[string]string{
			"framework":    "python",
			"pattern_type": "yaml_driven", // <- the problematic case
		},
	}

	var emitted []string
	synthesizeDjangoFromComposed(
		[]types.EntityRecord{yamlDrivenFastapiRoute},
		"main.py",
		func(method, canonicalPath, framework, refKind, refName string) {
			id := httproutes.SyntheticID(method, canonicalPath)
			emitted = append(emitted, id)
		},
	)

	if len(emitted) != 0 {
		t.Errorf("#748 regression: synthesizeDjangoFromComposed emitted %v for a yaml_driven Route; expected zero emissions (yaml_driven routes must be skipped)", emitted)
	}
}

// TestSynth_Django_AstDriven_StillWorks verifies that the #748 fix does NOT
// regress real Django URL conf routes. ast_driven Route entities (produced by
// the Django AST composition passes) must still be synthesized as ANY-verb
// http_endpoints.
func TestSynth_Django_AstDriven_StillWorks(t *testing.T) {
	// Simulate entities that django_routes.go / django_urlconf_nested.go
	// would produce for a Django urls.py file.
	astDrivenRoutes := []types.EntityRecord{
		{
			ID:         "ast:Route:/api/v1/users",
			Name:       "/api/v1/users",
			Kind:       "Route",
			SourceFile: "api/urls.py",
			Language:   "python",
			Properties: map[string]string{
				"framework":    "python",
				"pattern_type": "ast_driven",
			},
		},
		{
			ID:         "ast:Route:/api/v1/orders",
			Name:       "/api/v1/orders",
			Kind:       "Route",
			SourceFile: "api/urls.py",
			Language:   "python",
			Properties: map[string]string{
				"framework":    "python",
				"pattern_type": "ast_driven",
			},
		},
	}

	var emitted []string
	synthesizeDjangoFromComposed(
		astDrivenRoutes,
		"api/urls.py",
		func(method, canonicalPath, framework, refKind, refName string) {
			id := httproutes.SyntheticID(method, canonicalPath)
			emitted = append(emitted, id)
		},
	)

	// Both list routes should be emitted as ANY.
	wantList := []string{
		"http:ANY:/api/v1/users",
		"http:ANY:/api/v1/orders",
	}
	for _, want := range wantList {
		found := false
		for _, id := range emitted {
			if id == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("#748 regression guard: ast_driven Django route %q not emitted; got %v", want, emitted)
		}
	}

	// Detail-route variants should also be present ({pk} suffix).
	wantDetail := []string{
		"http:ANY:/api/v1/users/{pk}",
		"http:ANY:/api/v1/orders/{pk}",
	}
	for _, want := range wantDetail {
		found := false
		for _, id := range emitted {
			if id == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("#748 regression guard: ast_driven Django detail route %q not emitted; got %v", want, emitted)
		}
	}
}

// ---------------------------------------------------------------------------
// Issue #708 — hasDynamicBaseURLPath unit test
// ---------------------------------------------------------------------------

// TestHasDynamicBaseURLPath verifies the path-classification helper used by
// makeEmit to tag leading-param consumer endpoints with dynamic_baseurl=true.
func TestHasDynamicBaseURLPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		// Leading placeholder — dynamic baseURL.
		{"/{tenantId}/contracts/{id}", true},
		{"{tenantId}/contracts/{id}", true},
		{"/{x}", true},
		{"{x}", true},
		// Non-leading placeholder — NOT dynamic baseURL.
		{"/api/{version}/users", false},
		{"/users/{id}", false},
		{"/api/v1/items", false},
		{"/", false},
		{"", false},
	}
	for _, tc := range cases {
		got := hasDynamicBaseURLPath(tc.path)
		if got != tc.want {
			t.Errorf("hasDynamicBaseURLPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
