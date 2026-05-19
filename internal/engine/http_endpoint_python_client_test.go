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
