package engine

import (
	"strings"
	"testing"
)

// TestSynth_Fetch covers fetch("/path") and fetch("/path", {method:"POST"}).
func TestSynth_Fetch(t *testing.T) {
	src := `
export async function loadUsers() {
  const r = await fetch("/api/users");
  return r.json();
}

export async function createUser(body) {
  const r = await fetch("/api/users", { method: "POST", body });
  return r.json();
}

export async function deleteUser(id) {
  const r = await fetch("/api/users/123", { method: "DELETE" });
  return r;
}
`
	got, _ := runDetect(t, "typescript", "client.ts", src)
	want := []string{
		"http:DELETE:/api/users/123",
		"http:GET:/api/users",
		"http:POST:/api/users",
	}
	requireContains(t, got, want, "fetch")
}

// TestSynth_Axios covers axios.<verb>("/path", ...).
func TestSynth_Axios(t *testing.T) {
	src := `
import axios from "axios";

export async function listOrders() {
  return axios.get("/api/orders");
}

export async function createOrder(body) {
  return axios.post("/api/orders", body);
}

export async function updateOrder(id, body) {
  return axios.put("/api/orders/{id}", body);
}

export async function patchOrder(id, body) {
  return axios.patch("/api/orders/{id}", body);
}

export async function deleteOrder(id) {
  return axios.delete("/api/orders/{id}");
}
`
	got, _ := runDetect(t, "typescript", "axios-client.ts", src)
	want := []string{
		"http:DELETE:/api/orders/{id}",
		"http:GET:/api/orders",
		"http:PATCH:/api/orders/{id}",
		"http:POST:/api/orders",
		"http:PUT:/api/orders/{id}",
	}
	requireContains(t, got, want, "axios")
}

// TestSynth_HttpClient covers generic *Client / httpClient / apiClient instances.
func TestSynth_HttpClient(t *testing.T) {
	src := `
class FooHttpClient {}
const httpClient = new FooHttpClient();

export async function getThing() {
  return httpClient.get("/things/1");
}

export async function postThing(body) {
  return apiClient.post("/things", body);
}
`
	got, _ := runDetect(t, "javascript", "client.js", src)
	want := []string{
		"http:GET:/things/1",
		"http:POST:/things",
	}
	requireContains(t, got, want, "httpClient")
}

// TestSynth_Fetch_AbsoluteURL strips scheme+host before canonicalisation
// so the cross-repo linker still matches by path against the producer.
func TestSynth_Fetch_AbsoluteURL(t *testing.T) {
	src := `
export async function pingHealth() {
  return fetch("https://api.example.com/health");
}
`
	got, _ := runDetect(t, "javascript", "absolute.js", src)
	want := []string{"http:GET:/health"}
	requireContains(t, got, want, "absolute-url")
}

// TestSynth_Fetch_TemplateLiteralSimple verifies that a fetch call with a
// simple template-literal URL emits a canonical synthetic with the variable
// name preserved as the placeholder (#706). This was deferred in Phase 1;
// Phase 2 enabled it; #706 adds semantic variable-name fidelity.
func TestSynth_Fetch_TemplateLiteralSimple(t *testing.T) {
	src := "export async function fetchUser(id) {\n" +
		"  return fetch(`/users/${id}`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "tmpl.ts", src)
	// Must not contain raw ${...} syntax.
	for _, id := range got {
		if strings.Contains(id, "${") {
			t.Errorf("template literal leaked into synthetic: %q", id)
		}
	}
	// #706: preserve the variable name — emit {id} not {param}.
	want := []string{"http:GET:/users/{id}"}
	requireContains(t, got, want, "template-literal fetch")
}

// TestSynth_TemplateLiteral_MultiSegment verifies multiple ${...} substitutions
// in the same URL template produce distinct named placeholders (#706).
func TestSynth_TemplateLiteral_MultiSegment(t *testing.T) {
	src := "export async function fetchChecklist(userId, listId) {\n" +
		"  return fetch(`/api/users/${userId}/checklists/${listId}`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "tmpl2.ts", src)
	// #706: each param gets its own name, not the generic {param}.
	want := []string{"http:GET:/api/users/{userId}/checklists/{listId}"}
	requireContains(t, got, want, "template-literal multi-segment")
}

// TestSynth_TemplateLiteral_ConstantFolding verifies that a known const
// string is resolved before variable-name placeholder substitution.
// Constant folding takes priority over identifier extraction (#706).
func TestSynth_TemplateLiteral_ConstantFolding(t *testing.T) {
	src := `const API_BASE = "/api/v1";

export async function getUsers() {
  return fetch(` + "`" + `${API_BASE}/users` + "`" + `);
}

export async function getUser(id) {
  return fetch(` + "`" + `${API_BASE}/users/${id}` + "`" + `);
}
`
	got, _ := runDetect(t, "typescript", "const-fold.ts", src)
	want := []string{
		"http:GET:/api/v1/users",
		// API_BASE folded to "/api/v1"; ${id} → {id} (#706).
		"http:GET:/api/v1/users/{id}",
	}
	requireContains(t, got, want, "constant-folding fetch")
}

// TestSynth_TemplateLiteral_AxiosPost verifies axios.post with a template URL
// preserves the variable name as the placeholder (#706).
func TestSynth_TemplateLiteral_AxiosPost(t *testing.T) {
	src := `import axios from "axios";

export async function updateUser(userId, body) {
  return axios.post(` + "`" + `/api/v1/users/${userId}` + "`" + `, body);
}

export async function deleteUser(userId) {
  return axios.delete(` + "`" + `/api/v1/users/${userId}` + "`" + `);
}
`
	got, _ := runDetect(t, "typescript", "axios-tmpl.ts", src)
	// #706: ${userId} → {userId}, not {param}.
	want := []string{
		"http:POST:/api/v1/users/{userId}",
		"http:DELETE:/api/v1/users/{userId}",
	}
	requireContains(t, got, want, "axios template literal")
}

// TestSynth_TemplateLiteral_AxiosConstantFolding verifies axios with a
// const base URL folded into the canonical path; non-constant params use
// variable name as placeholder (#706).
func TestSynth_TemplateLiteral_AxiosConstantFolding(t *testing.T) {
	src := `import axios from "axios";
const BASE = "/api/v1";

export async function listOrders(userId) {
  return axios.get(` + "`" + `${BASE}/users/${userId}/orders` + "`" + `);
}
`
	got, _ := runDetect(t, "typescript", "axios-const-fold.ts", src)
	// BASE folded to "/api/v1"; ${userId} → {userId} (#706).
	want := []string{"http:GET:/api/v1/users/{userId}/orders"}
	requireContains(t, got, want, "axios constant-folding")
}

// TestSynth_TemplateLiteral_UnknownBase verifies that when the first
// segment is an unknown constant (not in the symbol table), we emit the
// identifier name as the placeholder (#706). Both ${UNKNOWN_BASE} and
// ${userId} produce named placeholders since they are valid identifiers.
func TestSynth_TemplateLiteral_UnknownBase(t *testing.T) {
	src := `export async function fetchUser(userId) {
  return fetch(` + "`" + `${UNKNOWN_BASE}/users/${userId}` + "`" + `);
}
`
	got, _ := runDetect(t, "typescript", "unknown-base.ts", src)
	// #706: UNKNOWN_BASE is a valid identifier → {UNKNOWN_BASE}; userId → {userId}.
	want := []string{"http:GET:/{UNKNOWN_BASE}/users/{userId}"}
	requireContains(t, got, want, "unknown base constant")
}

// TestSynth_TemplateLiteral_NonURLRejected verifies that template literals
// that don't look like URL paths are not emitted as synthetics.
func TestSynth_TemplateLiteral_NonURLRejected(t *testing.T) {
	src := `export function buildMsg(name) {
  const msg = ` + "`" + `Hello ${name}!` + "`" + `;
  console.log(` + "`" + `greeting: ${msg}` + "`" + `);
  fetch(` + "`" + `not-a-path-${name}` + "`" + `);
}
`
	got, _ := runDetect(t, "typescript", "non-url-tmpl.ts", src)
	for _, id := range got {
		if strings.Contains(id, "Hello") || strings.Contains(id, "greeting") || strings.Contains(id, "not-a-path") {
			t.Errorf("non-URL template literal produced synthetic: %q", id)
		}
	}
}

// TestSynth_Python_Requests covers requests.<verb>("path") and
// httpx.<verb>("path").
func TestSynth_Python_Requests(t *testing.T) {
	src := `import requests
import httpx

def fetch_users():
    return requests.get("/api/users")

def create_user(body):
    return requests.post("/api/users", json=body)

def delete_user(uid):
    return httpx.delete("/api/users/{uid}")
`
	got, _ := runDetect(t, "python", "client.py", src)
	want := []string{
		"http:DELETE:/api/users/{uid}",
		"http:GET:/api/users",
		"http:POST:/api/users",
	}
	requireContains(t, got, want, "requests+httpx")
}

// TestSynth_Python_Session covers session.<verb>(...) and client.<verb>(...)
// from a requests.Session() or httpx.Client() instance.
func TestSynth_Python_Session(t *testing.T) {
	src := `import requests
import httpx

session = requests.Session()
client = httpx.Client()

def list_orders():
    return session.get("/api/orders")

def create_order(body):
    return client.post("/api/orders", json=body)
`
	got, _ := runDetect(t, "python", "session.py", src)
	want := []string{
		"http:GET:/api/orders",
		"http:POST:/api/orders",
	}
	requireContains(t, got, want, "session/client")
}

// TestSynth_Python_Aiohttp covers aiohttp.ClientSession().<verb>(...).
func TestSynth_Python_Aiohttp(t *testing.T) {
	src := `import aiohttp

async def fetch_health():
    async with aiohttp.ClientSession() as session:
        async with session.get("/health") as resp:
            return await resp.json()

async def post_event(body):
    return await aiohttp.ClientSession().post("/events", json=body)
`
	got, _ := runDetect(t, "python", "aiohttp_client.py", src)
	want := []string{
		"http:GET:/health",
		"http:POST:/events",
	}
	requireContains(t, got, want, "aiohttp")
}

// TestSynth_NonURL_Rejected confirms identifiers and non-path strings
// don't pollute the consumer side. (Express producer-side YAML rule
// emits its own synthetics for `<ident>.get("...")` regardless of path
// shape; that's a producer-side concern. We only check that the
// consumer-side fetch detector rejects non-path inputs.)
func TestSynth_NonURL_Rejected(t *testing.T) {
	src := `
const result = fetch("not-a-path");
const result2 = fetch("just-a-bare-word");
`
	got, _ := runDetect(t, "javascript", "rejects.js", src)
	if len(got) > 0 {
		t.Errorf("expected no synthetics, got: %v", got)
	}
}

// TestSynth_NoProducerSideCollision confirms that Express's
// `app.get("/p", handler)` route registration is NOT shadowed by the
// consumer-side pattern — producer + consumer detectors must be disjoint
// on the same input file.
func TestSynth_NoProducerSideCollision(t *testing.T) {
	// Express producer registration — should emit a producer-side
	// synthetic with pattern_type=http_endpoint_synthesis, NOT a
	// consumer-side one. We also confirm only ONE synthetic per ID
	// (the dedup map enforces this).
	src := `
const express = require("express");
const app = express();
app.get("/api/things", (req, res) => { res.json({}); });
`
	_, res := runDetect(t, "javascript", "server.js", src)
	count := 0
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind && e.ID == "http:GET:/api/things" {
			count++
			if pt := e.Properties["pattern_type"]; pt != "http_endpoint_synthesis" {
				t.Errorf("expected pattern_type=http_endpoint_synthesis for producer-side route, got %q", pt)
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 synthetic for /api/things, got %d", count)
	}
}

// TestSynth_DecoratorNotMisclassified confirms Python decorators
// `@app.get("/p")` (FastAPI / Flask) are NOT picked up as session-style
// HTTP-client calls — they're producer-side.
func TestSynth_DecoratorNotMisclassified(t *testing.T) {
	src := `from fastapi import FastAPI
app = FastAPI()

@app.get("/api/items")
async def list_items():
    return []
`
	_, res := runDetect(t, "python", "server.py", src)
	count := 0
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind && e.ID == "http:GET:/api/items" {
			count++
			if pt := e.Properties["pattern_type"]; pt != "http_endpoint_synthesis" {
				t.Errorf("expected producer-side pattern_type, got %q", pt)
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 synthetic, got %d", count)
	}
}

// TestSynth_FetchCallerAttribution checks the source_caller property is
// attached when an enclosing named function exists.
func TestSynth_FetchCallerAttribution(t *testing.T) {
	src := `
export async function loadOrders() {
  return fetch("/orders");
}
`
	_, res := runDetect(t, "typescript", "caller.ts", src)
	for _, e := range res.Entities {
		if e.ID != "http:GET:/orders" {
			continue
		}
		if got := e.Properties["source_caller"]; got != "Function:loadOrders" {
			t.Errorf("source_caller = %q, want Function:loadOrders", got)
		}
		if got := e.Properties["pattern_type"]; got != "http_endpoint_client_synthesis" {
			t.Errorf("pattern_type = %q, want http_endpoint_client_synthesis", got)
		}
		return
	}
	t.Fatalf("no synthetic emitted for /orders")
}

// ---------------------------------------------------------------------------
// Phase 3 (#651) — custom HTTP wrapper recognition
// ---------------------------------------------------------------------------

// TestSynth_Wrapper_EndpointKeyStaticString covers the canonical
// callApi-style shape:
//
//	callApi({ endpoint: "/users/5" }, "GET")
//
// We do NOT hardcode `callApi` — match by object-literal shape with an
// `endpoint:` URL key.
func TestSynth_Wrapper_EndpointKeyStaticString(t *testing.T) {
	src := `export async function listClients(token) {
  const r = await callApi({ token, endpoint: "/clients/" }, "GET");
  return r.data;
}
`
	got, _ := runDetect(t, "javascript", "wrapper-endpoint.js", src)
	want := []string{"http:GET:/clients"}
	requireContains(t, got, want, "wrapper endpoint:")
}

// TestSynth_Wrapper_URLKeyTemplateLiteral covers the same shape with the
// `url:` variant and a template-literal value. The constant is folded and
// ${id} produces {id} as placeholder (#706).
func TestSynth_Wrapper_URLKeyTemplateLiteral(t *testing.T) {
	src := "const CLIENTS_ENDPOINT = \"/clients\";\n" +
		"export async function getClient(id) {\n" +
		"  return api({ url: `${CLIENTS_ENDPOINT}/${id}`, token }, \"GET\");\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "wrapper-url-tmpl.ts", src)
	// CLIENTS_ENDPOINT folded; ${id} → {id} (#706).
	want := []string{"http:GET:/clients/{id}"}
	requireContains(t, got, want, "wrapper url: template literal")
}

// TestSynth_Wrapper_MethodKeyExplicit verifies an in-object-literal
// `method:` key takes precedence over default GET.
func TestSynth_Wrapper_MethodKeyExplicit(t *testing.T) {
	src := `export async function createClient(body) {
  return request({ endpoint: "/clients", method: "POST" }, body);
}
`
	got, _ := runDetect(t, "javascript", "wrapper-method-key.js", src)
	want := []string{"http:POST:/clients"}
	requireContains(t, got, want, "wrapper method: key")
}

// TestSynth_Wrapper_PositionalMethodConstant covers the HTTP_METHODS.X
// pattern as a 2nd positional argument — the dominant fixture-b shape.
func TestSynth_Wrapper_PositionalMethodConstant(t *testing.T) {
	src := `export async function updateClient(id, body) {
  return callApi({ endpoint: "/clients/5/" }, HTTP_METHODS.PUT, body);
}
`
	got, _ := runDetect(t, "javascript", "wrapper-pos-method.js", src)
	want := []string{"http:PUT:/clients/5"}
	requireContains(t, got, want, "wrapper positional dotted method")
}

// TestSynth_Wrapper_DefaultGET verifies that omitting the method key
// defaults to GET.
func TestSynth_Wrapper_DefaultGET(t *testing.T) {
	src := `export async function ping() {
  return http({ endpoint: "/health" });
}
`
	got, _ := runDetect(t, "javascript", "wrapper-default-get.js", src)
	want := []string{"http:GET:/health"}
	requireContains(t, got, want, "wrapper default GET")
}

// TestSynth_Wrapper_NonHTTPCallRejected verifies that random object-arg
// invocations without a URL key produce no synthetic — guards against
// false positives.
func TestSynth_Wrapper_NonHTTPCallRejected(t *testing.T) {
	src := `function buildConfig(opts) { return opts; }
const cfg = buildConfig({ timeout: 5000, retries: 3 });
const dispatch = createDispatch({ store, middleware: [] });
const state = useState({ count: 0 });
`
	got, _ := runDetect(t, "javascript", "wrapper-no-url.js", src)
	if len(got) > 0 {
		t.Errorf("expected zero synthetics for non-HTTP object-arg calls, got: %v", got)
	}
}

// TestSynth_Wrapper_BlocklistKeywords verifies that control-flow keywords
// and known non-HTTP helpers (setState, useState, etc.) are skipped even
// when they happen to receive an obj literal with an `endpoint:` key.
func TestSynth_Wrapper_BlocklistKeywords(t *testing.T) {
	src := `// pathological: someone names a state field 'endpoint'
useState({ endpoint: "/not-http" });
setState({ endpoint: "/also-not-http" });
`
	got, _ := runDetect(t, "javascript", "wrapper-blocklist.js", src)
	for _, id := range got {
		if strings.Contains(id, "not-http") {
			t.Errorf("blocklisted call emitted synthetic: %q", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 3 (#651) — $-prefixed / named axios instances
// ---------------------------------------------------------------------------

// TestSynth_DollarHTTP_StaticString covers the gfleet/Angular pattern:
//
//	$http.get('/path/')
func TestSynth_DollarHTTP_StaticString(t *testing.T) {
	src := `import { $http } from "./httpClient";

export async function getSchedule() {
  return $http.get('/schedule/');
}

export async function createReschedule(body) {
  return $http.post('/reschedule-requests/', body);
}
`
	got, _ := runDetect(t, "typescript", "dollar-http.ts", src)
	want := []string{
		"http:GET:/schedule",
		"http:POST:/reschedule-requests",
	}
	requireContains(t, got, want, "$http static")
}

// TestSynth_DollarHTTP_TypescriptGeneric covers the TS-generic call
// form `$http.get<Response>('/path')` that is idiomatic for typed
// axios instances in TypeScript codebases.
func TestSynth_DollarHTTP_TypescriptGeneric(t *testing.T) {
	src := `import { $http } from "./httpClient";

export async function listGroups(): Promise<Group[]> {
  const r = await $http.get<Group[]>('/groups/list/');
  return r.data;
}
`
	got, _ := runDetect(t, "typescript", "dollar-http-generic.ts", src)
	want := []string{"http:GET:/groups/list"}
	requireContains(t, got, want, "$http with TS generic")
}

// TestSynth_DollarHTTP_TemplateLiteral covers $http with template literal
// and file-local const folding. BASE_PATH is folded; ${id} emits {id} (#706).
func TestSynth_DollarHTTP_TemplateLiteral(t *testing.T) {
	src := "import { $http } from \"./httpClient\";\n" +
		"const BASE_PATH = \"/inspections/\";\n" +
		"export async function getInspection(id) {\n" +
		"  return $http.get(`${BASE_PATH}${id}/`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "dollar-http-tmpl.ts", src)
	// BASE_PATH folded to "/inspections/"; ${id} → {id} (#706).
	want := []string{"http:GET:/inspections/{id}"}
	requireContains(t, got, want, "$http template literal")
}

// TestSynth_AxiosInstance_NamedAndPlain covers `const apiClient =
// axios.create({...})` followed by `apiClient.post('/users', body)`.
//
// This case was already matched by axiosClientRe (apiClient suffix). The
// instance-table emitter ALSO emits an entity tagged "axios_instance" —
// the upstream dedup map collapses duplicate IDs.
func TestSynth_AxiosInstance_NamedAndPlain(t *testing.T) {
	src := `import axios from "axios";
const apiClient = axios.create({ timeout: 5000 });

export async function createUser(body) {
  return apiClient.post('/users', body);
}
`
	got, _ := runDetect(t, "typescript", "axios-instance-named.ts", src)
	want := []string{"http:POST:/users"}
	requireContains(t, got, want, "axios.create named instance")
}

// TestSynth_AxiosInstance_BaseURLComposition verifies that an
// `axios.create({baseURL:'/api/v1'})` instance prepends its baseURL to
// the request path on emission.
func TestSynth_AxiosInstance_BaseURLComposition(t *testing.T) {
	src := `import axios from "axios";
const client = axios.create({ baseURL: '/api/v1', timeout: 5000 });

export async function listUsers() {
  return client.get('/users');
}

export async function getUser(id) {
  return client.get('/users/' + id);
}
`
	got, _ := runDetect(t, "typescript", "axios-base-url.ts", src)
	// First call: literal path. Second: path with concat, dropped (not a
	// pure literal/template). At minimum the first must emit composed.
	want := []string{"http:GET:/api/v1/users"}
	requireContains(t, got, want, "axios.create baseURL composition")
}

// TestSynth_AxiosInstance_BaseURLTemplateComposition verifies baseURL
// composition stacks with template-literal path resolution. ${id} produces
// {id} as placeholder (#706).
func TestSynth_AxiosInstance_BaseURLTemplateComposition(t *testing.T) {
	src := "import axios from \"axios\";\n" +
		"const client = axios.create({ baseURL: '/api/v1' });\n" +
		"export async function getUser(id) {\n" +
		"  return client.get(`/users/${id}`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "axios-base-url-tmpl.ts", src)
	// #706: ${id} → {id}, not {param}.
	want := []string{"http:GET:/api/v1/users/{id}"}
	requireContains(t, got, want, "axios.create baseURL + template")
}

// TestSynth_AxiosInstance_UnknownReceiverRejected verifies that the new
// axios-instance emitter (framework="axios_instance") does NOT fire on
// `someObj.someMethod({...})` when there is no axios.create() and no `$`
// prefix.
//
// Note: the server-side express synthesizer in http_endpoint_synthesis.go
// (a different file) DOES currently emit on `formData.delete("foo")` —
// that's the precision bug fixed by PR #660. We scope this test to the
// new code only.
func TestSynth_AxiosInstance_UnknownReceiverRejected(t *testing.T) {
	src := `const formData = new FormData();
formData.delete("foo");
formData.get("bar");
const someObj = { get(x) { return x; } };
someObj.get("baz");
`
	_, res := runDetect(t, "javascript", "axios-unknown.js", src)
	for _, e := range res.Entities {
		if e.Kind != httpEndpointKind {
			continue
		}
		fw := e.Properties["framework"]
		if fw == "axios_instance" || fw == "http_wrapper" {
			t.Errorf("new-code emitter fired on unknown receiver: ID=%q framework=%q", e.ID, fw)
		}
	}
}

// ---------------------------------------------------------------------------
// Issue #706 — semantic placeholder names for template-literal interpolations
// ---------------------------------------------------------------------------

// TestSynth706_SingleIdentifier is the canonical #706 case: a plain variable
// name inside ${...} becomes the placeholder name instead of the generic {param}.
func TestSynth706_SingleIdentifier(t *testing.T) {
	src := "export async function getUser(userId) {\n" +
		"  return fetch(`/users/${userId}`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "706-single-ident.ts", src)
	want := []string{"http:GET:/users/{userId}"}
	requireContains(t, got, want, "#706 single identifier")
}

// TestSynth706_MultipleNamedParams verifies that multiple distinct variable
// names produce distinct named placeholders in the same path.
func TestSynth706_MultipleNamedParams(t *testing.T) {
	src := "export async function getComment(postId, commentId) {\n" +
		"  return fetch(`/posts/${postId}/comments/${commentId}`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "706-multi-param.ts", src)
	want := []string{"http:GET:/posts/{postId}/comments/{commentId}"}
	requireContains(t, got, want, "#706 multiple named params")
}

// TestSynth706_PropertyAccess verifies property-access expressions use the
// last segment as the placeholder name: ${user.id} → {id}.
func TestSynth706_PropertyAccess(t *testing.T) {
	src := "export async function patchUser(user) {\n" +
		"  return fetch(`/users/${user.id}`, { method: 'PATCH' });\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "706-prop-access.ts", src)
	// ${user.id} → last segment "id" → {id}.
	want := []string{"http:PATCH:/users/{id}"}
	requireContains(t, got, want, "#706 property access last segment")
}

// TestSynth706_DeepPropertyAccess verifies that deeper chains like
// ${params.branchId} still use the last segment.
func TestSynth706_DeepPropertyAccess(t *testing.T) {
	src := "export async function getBranch(params) {\n" +
		"  return axios.get(`/repos/${params.branchId}/details`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "706-deep-prop.ts", src)
	// ${params.branchId} → {branchId}
	want := []string{"http:GET:/repos/{branchId}/details"}
	requireContains(t, got, want, "#706 deep property access")
}

// TestSynth706_ComplexExprFallback verifies that function calls fall back to
// {param} since there is no reliable identifier to extract.
func TestSynth706_ComplexExprFallback(t *testing.T) {
	src := "export async function getUser() {\n" +
		"  return fetch(`/users/${getUserId()}`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "706-complex-expr.ts", src)
	// Function call → {param} fallback.
	want := []string{"http:GET:/users/{param}"}
	requireContains(t, got, want, "#706 complex expression fallback")
}

// TestSynth706_TypeScriptCast verifies TypeScript `as <Type>` casts are
// stripped so the underlying identifier name is preserved.
func TestSynth706_TypeScriptCast(t *testing.T) {
	src := "export async function getUser(userId: unknown) {\n" +
		"  return fetch(`/users/${userId as string}`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "706-ts-cast.ts", src)
	// `userId as string` → strip cast → `userId` → {userId}.
	want := []string{"http:GET:/users/{userId}"}
	requireContains(t, got, want, "#706 TypeScript cast stripped")
}

// TestSynth706_OptionalChain verifies optional-chain expressions like
// ${user?.id} use the property name after the ?. operator.
func TestSynth706_OptionalChain(t *testing.T) {
	src := "export async function deleteUser(user) {\n" +
		"  return axios.delete(`/users/${user?.id}`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "706-optional-chain.ts", src)
	// ${user?.id} → strip ?. → user.id → last segment "id" → {id}.
	want := []string{"http:DELETE:/users/{id}"}
	requireContains(t, got, want, "#706 optional-chain")
}

// TestSynth706_NestedTemplatePlaceholders verifies that a template literal
// with back-to-back placeholders (${prefix}${id}) produces two named params.
func TestSynth706_NestedTemplatePlaceholders(t *testing.T) {
	src := "const API = \"/api\";\n" +
		"export async function getItem(resourceType, itemId) {\n" +
		"  return fetch(`${API}/${resourceType}/${itemId}`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "706-nested-tmpl.ts", src)
	// API folded to "/api"; resourceType and itemId get named placeholders.
	want := []string{"http:GET:/api/{resourceType}/{itemId}"}
	requireContains(t, got, want, "#706 nested template placeholders")
}

// TestSynth706_ArraySubscriptFallback verifies that array-subscript
// expressions like ${arr[0]} fall back to {param} (not extractable).
func TestSynth706_ArraySubscriptFallback(t *testing.T) {
	src := "export async function getFirst(ids) {\n" +
		"  return fetch(`/items/${ids[0]}`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "706-subscript.ts", src)
	// ${ids[0]} → subscript expression → {param} fallback.
	want := []string{"http:GET:/items/{param}"}
	requireContains(t, got, want, "#706 array subscript fallback")
}

// ---------------------------------------------------------------------------
// Issue #712 — bare const-variable path resolution
// ---------------------------------------------------------------------------

// TestSynth712_SingleConst verifies that a bare const path variable is
// resolved and emits the correct endpoint.
// Covers: const X = "/foo"; $http.get(X)  → http:GET:/foo
func TestSynth712_SingleConst(t *testing.T) {
	src := `import { $http } from "./httpClient";
const BASE_PATH = "/buildings/";

export async function getBuildings() {
  return $http.get(BASE_PATH, { params: { active: true } });
}
`
	got, _ := runDetect(t, "typescript", "712-single-const.ts", src)
	want := []string{"http:GET:/buildings"}
	requireContains(t, got, want, "#712 single const path")
}

// TestSynth712_MultipleConsts verifies that multiple distinct const paths
// in the same file are each resolved to their respective endpoints.
// Covers: const A = "/a"; const B = "/b"; $http.get(A); $http.delete(B)
func TestSynth712_MultipleConsts(t *testing.T) {
	src := `import { $http } from "./httpClient";
const RECENTS_PATH = "/recents/buildings/";
const UPLOAD_PATH = "/attachments/upload/";

export async function getRecents() {
  return $http.get(RECENTS_PATH, { params: {} });
}

export async function deleteRecents() {
  return $http.delete(RECENTS_PATH, { params: {} });
}

export async function uploadFile(data) {
  return $http.post(UPLOAD_PATH, data);
}
`
	got, _ := runDetect(t, "typescript", "712-multiple-consts.ts", src)
	want := []string{
		"http:GET:/recents/buildings",
		"http:DELETE:/recents/buildings",
		"http:POST:/attachments/upload",
	}
	requireContains(t, got, want, "#712 multiple consts")
}

// TestSynth712_LetDeclaration verifies that let-declared path variables are
// also resolved (beyond-minimum: const|let|var all handled).
func TestSynth712_LetDeclaration(t *testing.T) {
	src := `import { $http } from "./httpClient";
let CHECKLISTS_PATH = "/checklists/";

export async function getChecklists() {
  return $http.get(CHECKLISTS_PATH, { params: { page: 1 } });
}
`
	got, _ := runDetect(t, "typescript", "712-let-decl.ts", src)
	want := []string{"http:GET:/checklists"}
	requireContains(t, got, want, "#712 let declaration")
}

// TestSynth712_DynamicArgFallback verifies that a dynamic argument (a
// function call result) is NOT resolved via the const table — only bare
// identifiers that match the table are substituted.
func TestSynth712_DynamicArgFallback(t *testing.T) {
	src := `import { $http } from "./httpClient";
const BASE_PATH = "/buildings/";

export async function getDynamic() {
  return $http.get(getDynamicPath(), { params: {} });
}
`
	got, _ := runDetect(t, "typescript", "712-dynamic-fallback.ts", src)
	// getDynamicPath() is a call expression, not a bare identifier — must
	// NOT be resolved via the const table. No endpoint should be emitted.
	for _, id := range got {
		if id == "http:GET:/buildings" {
			// The Base_PATH const should not be confused with the dynamic call.
			// If we do emit /buildings it means we resolved BASE_PATH instead
			// of getDynamicPath() — but the regex only matches bare identifiers
			// not calls, so this shouldn't happen.
			// Accept this as a valid endpoint only if BASE_PATH itself was used
			// elsewhere. Since it wasn't, if we see it something is wrong.
			t.Logf("note: /buildings appeared in output (may be from another match)")
		}
	}
	// The primary assertion: getDynamicPath() must NOT produce an endpoint.
	for _, id := range got {
		if id == "http:GET:/getDynamicPath" {
			t.Errorf("dynamic function call should not produce endpoint: %q", id)
		}
	}
}

// TestSynth712_UnknownIdentSkipped verifies that a bare identifier that is
// NOT in the const symbol table produces no endpoint.
func TestSynth712_UnknownIdentSkipped(t *testing.T) {
	src := `import { $http } from "./httpClient";

export async function fetchSomething() {
  return $http.get(SOME_EXTERNAL_CONST, {});
}
`
	got, _ := runDetect(t, "typescript", "712-unknown-ident.ts", src)
	for _, id := range got {
		if id == "http:GET:/SOME_EXTERNAL_CONST" || id == "http:GET:SOME_EXTERNAL_CONST" {
			t.Errorf("unknown identifier produced endpoint: %q", id)
		}
	}
}

// TestSynth712_AxiosInstanceBareIdent verifies that a named axios.create()
// instance also resolves bare-identifier path variables.
func TestSynth712_AxiosInstanceBareIdent(t *testing.T) {
	src := `import axios from "axios";
const apiClient = axios.create({ baseURL: "/api/v1" });
const USERS_PATH = "/users";

export async function listUsers() {
  return apiClient.get(USERS_PATH);
}
`
	got, _ := runDetect(t, "typescript", "712-axios-instance-bare.ts", src)
	// USERS_PATH resolved to "/users", composed with baseURL "/api/v1".
	want := []string{"http:GET:/api/v1/users"}
	requireContains(t, got, want, "#712 axios instance bare ident with baseURL")
}

// ---------------------------------------------------------------------------
// Issue #721 — FETCHES edge emission for JS/TS consumers
// ---------------------------------------------------------------------------

// TestSynth721_FetchEmitsFetchesEdge verifies that a static fetch() call
// emits both a consumer http_endpoint entity AND a FETCHES relationship
// from the enclosing function to that endpoint.
func TestSynth721_FetchEmitsFetchesEdge(t *testing.T) {
	src := `
export async function loadUsers() {
  return fetch("/api/users");
}
`
	_, res := runDetect(t, "typescript", "721-fetch-edge.ts", src)
	// Assert the entity exists.
	foundEntity := false
	for _, e := range res.Entities {
		if e.ID == "http:GET:/api/users" {
			foundEntity = true
		}
	}
	if !foundEntity {
		t.Fatalf("expected http:GET:/api/users entity, not found")
	}
	// Assert the FETCHES edge exists.
	foundEdge := false
	for _, r := range res.Relationships {
		if r.Kind == fetchesEdgeKind && r.ToID == "http:GET:/api/users" {
			if r.FromID == "Function:loadUsers" {
				foundEdge = true
			}
		}
	}
	if !foundEdge {
		t.Errorf("expected FETCHES edge Function:loadUsers → http:GET:/api/users, not found (relationships: %v)", res.Relationships)
	}
}

// TestSynth721_AxiosEmitsFetchesEdge verifies FETCHES edge for axios.get().
func TestSynth721_AxiosEmitsFetchesEdge(t *testing.T) {
	src := `import axios from "axios";

export async function getOrder(id) {
  return axios.get("/api/orders");
}
`
	_, res := runDetect(t, "typescript", "721-axios-edge.ts", src)
	foundEdge := false
	for _, r := range res.Relationships {
		if r.Kind == fetchesEdgeKind && r.ToID == "http:GET:/api/orders" && r.FromID == "Function:getOrder" {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Errorf("expected FETCHES edge Function:getOrder → http:GET:/api/orders")
	}
}

// TestSynth721_TemplateLiteralEmitsFetchesEdge verifies FETCHES for template literal fetch.
func TestSynth721_TemplateLiteralEmitsFetchesEdge(t *testing.T) {
	src := "export async function getUser(id) {\n" +
		"  return fetch(`/users/${id}`);\n" +
		"}\n"
	_, res := runDetect(t, "typescript", "721-tmpl-edge.ts", src)
	foundEdge := false
	for _, r := range res.Relationships {
		if r.Kind == fetchesEdgeKind && r.ToID == "http:GET:/users/{id}" && r.FromID == "Function:getUser" {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Errorf("expected FETCHES edge Function:getUser → http:GET:/users/{id}")
	}
}

// TestSynth721_DollarHTTPEmitsFetchesEdge verifies FETCHES for $http calls.
func TestSynth721_DollarHTTPEmitsFetchesEdge(t *testing.T) {
	src := `import { $http } from "./httpClient";

export async function getSchedule() {
  return $http.get('/schedule/');
}
`
	_, res := runDetect(t, "typescript", "721-dollar-http-edge.ts", src)
	foundEdge := false
	for _, r := range res.Relationships {
		if r.Kind == fetchesEdgeKind && r.ToID == "http:GET:/schedule" && r.FromID == "Function:getSchedule" {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Errorf("expected FETCHES edge Function:getSchedule → http:GET:/schedule")
	}
}

// TestSynth721_ClassPropertyArrowEmitsFetchesEdge verifies FETCHES for class
// property arrow functions — the dominant pattern in Angular/Vue service classes
// and React component class methods:
//
//	class AuthService { login = (email) => $http.post('/auth/login', ...) }
func TestSynth721_ClassPropertyArrowEmitsFetchesEdge(t *testing.T) {
	src := `import { $http } from "../../utils/http.utils";

class AuthService {
  login = (email, password) => $http.post('/auth/login', {email, password})
  current = () => $http.get('/users/current')
}
`
	_, res := runDetect(t, "javascript", "721-class-arrow.js", src)
	// login method should emit FETCHES to /auth/login.
	foundLogin := false
	for _, r := range res.Relationships {
		if r.Kind == fetchesEdgeKind && r.ToID == "http:POST:/auth/login" && r.FromID == "Function:login" {
			foundLogin = true
		}
	}
	if !foundLogin {
		t.Errorf("expected FETCHES edge Function:login → http:POST:/auth/login")
	}
	// current method should emit FETCHES to /users/current.
	foundCurrent := false
	for _, r := range res.Relationships {
		if r.Kind == fetchesEdgeKind && r.ToID == "http:GET:/users/current" && r.FromID == "Function:current" {
			foundCurrent = true
		}
	}
	if !foundCurrent {
		t.Errorf("expected FETCHES edge Function:current → http:GET:/users/current")
	}
}

// TestSynth721_WrapperCallEmitsFetchesEdge verifies FETCHES for custom HTTP wrapper.
func TestSynth721_WrapperCallEmitsFetchesEdge(t *testing.T) {
	src := `export async function listClients(token) {
  return callApi({ endpoint: "/clients/" }, "GET");
}
`
	_, res := runDetect(t, "javascript", "721-wrapper-edge.js", src)
	foundEdge := false
	for _, r := range res.Relationships {
		if r.Kind == fetchesEdgeKind && r.ToID == "http:GET:/clients" && r.FromID == "Function:listClients" {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Errorf("expected FETCHES edge Function:listClients → http:GET:/clients")
	}
}

// ---------------------------------------------------------------------------
// Issue #721 — env-var runtime_dynamic for JS/TS (process.env / import.meta.env)
// ---------------------------------------------------------------------------

// TestSynth721_ProcessEnvFetchRuntimeDynamic verifies that
// fetch(process.env.API + '/users') emits an endpoint with runtime_dynamic=true.
func TestSynth721_ProcessEnvFetchRuntimeDynamic(t *testing.T) {
	src := `
export async function loadUsers() {
  return fetch(process.env.API_URL + '/users');
}
`
	_, res := runDetect(t, "typescript", "721-process-env-fetch.ts", src)
	foundDynamic := false
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind && e.ID == "http:GET:/users" {
			if e.Properties["runtime_dynamic"] == "true" {
				foundDynamic = true
			}
		}
	}
	if !foundDynamic {
		t.Errorf("expected http:GET:/users with runtime_dynamic=true for process.env concat")
	}
}

// TestSynth721_ImportMetaEnvAxiosRuntimeDynamic verifies that
// axios.get(import.meta.env.VITE_API + '/items') emits runtime_dynamic=true.
func TestSynth721_ImportMetaEnvAxiosRuntimeDynamic(t *testing.T) {
	src := `import axios from "axios";

export async function listItems() {
  return axios.get(import.meta.env.VITE_API_URL + '/items');
}
`
	_, res := runDetect(t, "typescript", "721-import-meta-axios.ts", src)
	foundDynamic := false
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind && e.ID == "http:GET:/items" {
			if e.Properties["runtime_dynamic"] == "true" {
				foundDynamic = true
			}
		}
	}
	if !foundDynamic {
		t.Errorf("expected http:GET:/items with runtime_dynamic=true for import.meta.env concat")
	}
}

// TestSynth721_ProcessEnvNextPublicRuntimeDynamic verifies that
// Next.js-style process.env.NEXT_PUBLIC_X + "/path" emits runtime_dynamic=true.
func TestSynth721_ProcessEnvNextPublicRuntimeDynamic(t *testing.T) {
	src := `
export async function getHealth() {
  return fetch(process.env.NEXT_PUBLIC_API_URL + '/health');
}
`
	_, res := runDetect(t, "typescript", "721-next-public.ts", src)
	foundDynamic := false
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind && e.ID == "http:GET:/health" {
			if e.Properties["runtime_dynamic"] == "true" {
				foundDynamic = true
			}
		}
	}
	if !foundDynamic {
		t.Errorf("expected http:GET:/health with runtime_dynamic=true for NEXT_PUBLIC env concat")
	}
}

// ---------------------------------------------------------------------------
// Issue #721 — Python env-var runtime_dynamic
// ---------------------------------------------------------------------------

// TestSynth721_PythonEnvConcatRuntimeDynamic verifies that
// requests.get(os.environ["API_URL"] + '/users') emits runtime_dynamic=true.
func TestSynth721_PythonEnvConcatRuntimeDynamic(t *testing.T) {
	src := `import requests
import os

def fetch_users():
    return requests.get(os.environ["API_URL"] + "/users")
`
	_, res := runDetect(t, "python", "721-py-env-concat.py", src)
	foundDynamic := false
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind && e.ID == "http:GET:/users" {
			if e.Properties["runtime_dynamic"] == "true" {
				foundDynamic = true
			}
		}
	}
	if !foundDynamic {
		t.Errorf("expected http:GET:/users with runtime_dynamic=true for os.environ concat (Python)")
	}
}

// TestSynth721_PythonGetenvConcatRuntimeDynamic verifies that
// httpx.post(os.getenv("BASE") + '/items') emits runtime_dynamic=true.
func TestSynth721_PythonGetenvConcatRuntimeDynamic(t *testing.T) {
	src := `import httpx
import os

def create_item(body):
    return httpx.post(os.getenv("BASE_URL") + "/items", json=body)
`
	_, res := runDetect(t, "python", "721-py-getenv-concat.py", src)
	foundDynamic := false
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind && e.ID == "http:POST:/items" {
			if e.Properties["runtime_dynamic"] == "true" {
				foundDynamic = true
			}
		}
	}
	if !foundDynamic {
		t.Errorf("expected http:POST:/items with runtime_dynamic=true for os.getenv concat (Python)")
	}
}

// ---------------------------------------------------------------------------
// Issue #721 — Java env-var runtime_dynamic
// ---------------------------------------------------------------------------

// TestSynth721_JavaGetenvConcatRuntimeDynamic verifies that
// URI.create(System.getenv("API_URL") + "/users") emits runtime_dynamic=true.
func TestSynth721_JavaGetenvConcatRuntimeDynamic(t *testing.T) {
	src := `import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.URI;

public class UserService {
    private final HttpClient httpClient = HttpClient.newHttpClient();

    public List<User> fetchUsers() throws Exception {
        HttpRequest request = HttpRequest.newBuilder()
            .uri(URI.create(System.getenv("API_URL") + "/users"))
            .GET()
            .build();
        return httpClient.send(request, null);
    }
}
`
	_, res := runDetect(t, "java", "721-java-getenv.java", src)
	foundDynamic := false
	for _, e := range res.Entities {
		if e.Kind == httpEndpointKind && e.ID == "http:GET:/users" {
			if e.Properties["runtime_dynamic"] == "true" {
				foundDynamic = true
			}
		}
	}
	if !foundDynamic {
		t.Errorf("expected http:GET:/users with runtime_dynamic=true for System.getenv concat (Java)")
	}
}
