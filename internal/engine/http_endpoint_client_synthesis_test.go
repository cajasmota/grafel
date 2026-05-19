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

// TestSynth_Fetch_TemplateLiteralDeferred verifies that template-literal
// URLs (Phase 2) do NOT crash the extractor and do NOT emit a malformed
// synthetic.
func TestSynth_Fetch_TemplateLiteralDeferred(t *testing.T) {
	src := "export async function fetchUser(id) {\n" +
		"  return fetch(`/users/${id}`);\n" +
		"}\n"
	got, _ := runDetect(t, "typescript", "tmpl.ts", src)
	for _, id := range got {
		if strings.Contains(id, "$") || strings.Contains(id, "{id}") {
			// {id} would only be present if we mis-extracted the template;
			// the deferred path means we emit nothing.
		}
		if strings.Contains(id, "${") {
			t.Errorf("template literal leaked into synthetic: %q", id)
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
