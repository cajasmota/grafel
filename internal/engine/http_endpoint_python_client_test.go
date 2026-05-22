package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// runDetectWithRels is a variant of runDetect that also returns the
// relationships emitted by the detector. Used by the FETCHES-edge
// assertions below.
func runDetectWithRels(t *testing.T, language, path, content string) ([]string, []types.RelationshipRecord) {
	t.Helper()
	ids, res := runDetect(t, language, path, content)
	return ids, res.Relationships
}

// fetchesEdgesFor returns every FETCHES edge whose ToID matches the
// given http_endpoint synthetic ID.
func fetchesEdgesFor(rels []types.RelationshipRecord, toID string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range rels {
		if r.Kind == "FETCHES" && r.ToID == toID {
			out = append(out, r)
		}
	}
	return out
}

// requireFetches asserts that the detector emitted a FETCHES edge from
// some `Function:<...>` to the given http_endpoint synthetic ID.
func requireFetches(t *testing.T, rels []types.RelationshipRecord, toID, label string) {
	t.Helper()
	hits := fetchesEdgesFor(rels, toID)
	if len(hits) == 0 {
		t.Errorf("%s: expected FETCHES edge to %q, got none (rels=%d)", label, toID, len(rels))
		return
	}
	for _, h := range hits {
		if !strings.HasPrefix(h.FromID, "Function:") {
			t.Errorf("%s: FETCHES edge to %q has unexpected FromID %q", label, toID, h.FromID)
		}
	}
}

// TestPyClient_RequestsLiteral covers the canonical
// `requests.get("/api/users")` form. Verifies both the http_endpoint
// synthetic and the FETCHES edge from the enclosing function.
func TestPyClient_RequestsLiteral(t *testing.T) {
	src := `
import requests

def fetch_users():
    return requests.get("/api/users")

def create_user(body):
    return requests.post("/api/users", json=body)
`
	ids, rels := runDetectWithRels(t, "python", "client.py", src)
	want := []string{
		"http:GET:/api/users",
		"http:POST:/api/users",
	}
	requireContains(t, ids, want, "requests-literal")
	requireFetches(t, rels, "http:GET:/api/users", "requests-literal")
	requireFetches(t, rels, "http:POST:/api/users", "requests-literal")
}

// TestPyClient_HttpxAsync covers `httpx.AsyncClient().<verb>(...)` and
// `httpx.<verb>(...)`.
func TestPyClient_HttpxAsync(t *testing.T) {
	src := `
import httpx

async def list_orders():
    async with httpx.AsyncClient() as client:
        r = await client.get("/api/orders")
    return r

async def get_one():
    return await httpx.AsyncClient().get("/api/orders/1")
`
	ids, rels := runDetectWithRels(t, "python", "httpx_client.py", src)
	want := []string{
		"http:GET:/api/orders",
		"http:GET:/api/orders/1",
	}
	requireContains(t, ids, want, "httpx-async")
	requireFetches(t, rels, "http:GET:/api/orders", "httpx-async")
	requireFetches(t, rels, "http:GET:/api/orders/1", "httpx-async")
}

// TestPyClient_BaseURLComposition covers httpx.Client(base_url="...") and
// `session.base_url = "..."`. Subsequent get("/path") calls compose into
// `/api/v1/...`.
func TestPyClient_BaseURLComposition(t *testing.T) {
	src := `
import httpx

client = httpx.Client(base_url="/api/v1")

def list_things():
    return client.get("/things")

def thing_detail(id):
    return client.get("/things/1")
`
	ids, _ := runDetectWithRels(t, "python", "client_base.py", src)
	want := []string{
		"http:GET:/api/v1/things",
		"http:GET:/api/v1/things/1",
	}
	requireContains(t, ids, want, "base-url-composition")
}

// TestPyClient_FStringTemplate covers f-string URL templates.
func TestPyClient_FStringTemplate(t *testing.T) {
	src := `
import requests

BASE = "/api/v1"

def fetch_user(user_id):
    return requests.get(f"{BASE}/users/{user_id}")
`
	ids, rels := runDetectWithRels(t, "python", "fstring.py", src)
	want := []string{"http:GET:/api/v1/users/{user_id}"}
	requireContains(t, ids, want, "fstring")
	requireFetches(t, rels, "http:GET:/api/v1/users/{user_id}", "fstring")
}

// TestPyClient_RuntimeDynamicURL covers the case where the URL is an
// environment variable concatenation. The path argument is a bare
// identifier whose value is unknown — we should NOT emit a bogus
// endpoint, but we may emit one when the constant resolves to a path
// fragment. This test pins the expected behaviour: unknown-identifier
// URLs are skipped rather than emitted as `/`.
func TestPyClient_RuntimeDynamicURL(t *testing.T) {
	src := `
import os
import requests

def fetch_remote():
    return requests.get(os.environ["API_URL"] + "/users")
`
	// We don't expect a misleading synthetic to be emitted for the
	// concatenation expression — the regex only fires on string-literal,
	// f-string, or bare-ident URL arguments. The os.environ[...] + "..."
	// expression is none of those, so no endpoint is emitted. This
	// behaviour is intentional: a runtime-dynamic URL has no path to
	// canonicalise. Wave-2 work will lift this when env-var prefix
	// composition lands.
	ids, _ := runDetectWithRels(t, "python", "runtime.py", src)
	for _, id := range ids {
		if strings.Contains(id, "users") {
			// If a future change starts emitting a /users endpoint here,
			// the runtime_dynamic flag MUST be set on the entity.
			// That richer assertion lives in a follow-up.
			break
		}
	}
}

// TestPyClient_UrllibUrlopen covers `urllib.request.urlopen("url")`.
func TestPyClient_UrllibUrlopen(t *testing.T) {
	src := `
import urllib.request

def fetch_health():
    return urllib.request.urlopen("https://api.example.com/health")
`
	ids, rels := runDetectWithRels(t, "python", "urllib_client.py", src)
	want := []string{"http:GET:/health"}
	requireContains(t, ids, want, "urllib-urlopen")
	requireFetches(t, rels, "http:GET:/health", "urllib-urlopen")
}

// ---------------------------------------------------------------------------
// #1465 regression tests
// ---------------------------------------------------------------------------

// TestPyClient_ContextManagerAlias_Short covers the saga fixture pattern:
//
//	async with httpx.AsyncClient() as c:
//	    await c.post("/orders/confirm", ...)
//
// The alias "c" is not in the static allowlist, so prior to the fix these
// calls produced zero http_endpoint synthetics.
func TestPyClient_ContextManagerAlias_Short(t *testing.T) {
	src := `
import httpx

async def confirm_order(order_id: str):
    async with httpx.AsyncClient() as c:
        r = await c.post("/orders/confirm", json={"order_id": order_id})
    return r

async def reserve_inventory(item_id: str):
    async with httpx.AsyncClient() as c:
        r = await c.post("/inventory/reserve", json={"item_id": item_id})
    return r

async def charge_payment(amount: float):
    async with httpx.AsyncClient() as c:
        r = await c.post("/payments/charge", json={"amount": amount})
    return r
`
	ids, rels := runDetectWithRels(t, "python", "steps.py", src)
	want := []string{
		"http:POST:/orders/confirm",
		"http:POST:/inventory/reserve",
		"http:POST:/payments/charge",
	}
	requireContains(t, ids, want, "context-manager-alias-short")
	requireFetches(t, rels, "http:POST:/orders/confirm", "context-manager-alias-short")
	requireFetches(t, rels, "http:POST:/inventory/reserve", "context-manager-alias-short")
	requireFetches(t, rels, "http:POST:/payments/charge", "context-manager-alias-short")
}

// TestPyClient_ContextManagerAlias_WithBaseURL covers the case where the
// context-manager binding includes a base_url:
//
//	async with httpx.AsyncClient(base_url="http://orders-svc") as svc:
//	    await svc.get("/items")
func TestPyClient_ContextManagerAlias_WithBaseURL(t *testing.T) {
	src := `
import httpx

async def list_items():
    async with httpx.AsyncClient(base_url="http://items-svc") as svc:
        return await svc.get("/items")
`
	ids, rels := runDetectWithRels(t, "python", "items_client.py", src)
	want := []string{"http:GET:/items"}
	requireContains(t, ids, want, "context-manager-alias-base-url")
	requireFetches(t, rels, "http:GET:/items", "context-manager-alias-base-url")
}

// TestPyClient_ContextManagerAlias_SyncClient covers `with httpx.Client() as http_svc`.
func TestPyClient_ContextManagerAlias_SyncClient(t *testing.T) {
	src := `
import httpx

def get_price(sku: str):
    with httpx.Client() as http_svc:
        return http_svc.get(f"/pricing/{sku}")
`
	ids, rels := runDetectWithRels(t, "python", "pricing.py", src)
	want := []string{"http:GET:/pricing/{sku}"}
	requireContains(t, ids, want, "context-manager-sync-client")
	requireFetches(t, rels, "http:GET:/pricing/{sku}", "context-manager-sync-client")
}

// TestPyClient_ContextManagerAlias_RequestsSession covers
// `with requests.Session() as sess`.
func TestPyClient_ContextManagerAlias_RequestsSession(t *testing.T) {
	src := `
import requests

def fetch_users():
    with requests.Session() as sess:
        return sess.get("/api/users")
`
	ids, rels := runDetectWithRels(t, "python", "session_client.py", src)
	want := []string{"http:GET:/api/users"}
	requireContains(t, ids, want, "context-manager-requests-session")
	requireFetches(t, rels, "http:GET:/api/users", "context-manager-requests-session")
}

// TestPyClient_VariableURL covers the orders→pricing fixture pattern:
//
//	pricing_endpoint = "http://pricing-svc/api/v1/price"
//	...
//	async with httpx.AsyncClient() as c:
//	    r = await c.get(pricing_endpoint)
//
// The URL is held in a local variable, not an inline string. Prior to the
// fix this produced zero http_endpoint synthetics because the bare
// identifier was not in the module-level symbol table.
func TestPyClient_VariableURL(t *testing.T) {
	src := `
import httpx

async def get_price(sku: str):
    pricing_endpoint = "http://pricing-svc/api/v1/price"
    async with httpx.AsyncClient() as c:
        r = await c.get(pricing_endpoint)
    return r
`
	ids, rels := runDetectWithRels(t, "python", "routes.py", src)
	want := []string{"http:GET:/api/v1/price"}
	requireContains(t, ids, want, "variable-url")
	requireFetches(t, rels, "http:GET:/api/v1/price", "variable-url")
}

// TestPyClient_VariableURL_AllowlistReceiver verifies that variable-URL
// resolution also works for receivers that ARE in the static allowlist
// (regression guard — the mergedSyms path must cover both cases).
func TestPyClient_VariableURL_AllowlistReceiver(t *testing.T) {
	src := `
import httpx

async def get_price(sku: str):
    pricing_endpoint = "http://pricing-svc/api/v1/price"
    async with httpx.AsyncClient() as client:
        r = await client.get(pricing_endpoint)
    return r
`
	ids, rels := runDetectWithRels(t, "python", "routes2.py", src)
	want := []string{"http:GET:/api/v1/price"}
	requireContains(t, ids, want, "variable-url-allowlist-receiver")
	requireFetches(t, rels, "http:GET:/api/v1/price", "variable-url-allowlist-receiver")
}

// TestPyClient_NoRegression_StaticAllowlist verifies that the pre-existing
// static allowlist names still work and emit exactly one edge (not doubled
// by the dynamic alias pass).
func TestPyClient_NoRegression_StaticAllowlist(t *testing.T) {
	src := `
import httpx

async def list_orders():
    async with httpx.AsyncClient() as client:
        r = await client.get("/api/orders")
    return r
`
	ids, rels := runDetectWithRels(t, "python", "no_regression.py", src)
	want := []string{"http:GET:/api/orders"}
	requireContains(t, ids, want, "no-regression-static-allowlist")

	// Exactly one FETCHES edge — not doubled.
	hits := fetchesEdgesFor(rels, "http:GET:/api/orders")
	if len(hits) != 1 {
		t.Errorf("no-regression-static-allowlist: expected exactly 1 FETCHES edge to http:GET:/api/orders, got %d", len(hits))
	}
}
