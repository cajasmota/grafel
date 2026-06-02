package consumes_api

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func runExtract(t *testing.T, path, lang, source string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	recs, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(source),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	return recs
}

// consumesEdges flattens every CONSUMES_API relationship across the returned
// records.
func consumesEdges(recs []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == string(types.RelationshipKindConsumesAPI) {
				out = append(out, rel)
			}
		}
	}
	return out
}

// findConsumesTo returns the CONSUMES_API edge whose ToID is the given endpoint
// id, failing the test when absent.
func findConsumesTo(t *testing.T, recs []types.EntityRecord, toID string) types.RelationshipRecord {
	t.Helper()
	for _, rel := range consumesEdges(recs) {
		if rel.ToID == toID {
			return rel
		}
	}
	t.Fatalf("no CONSUMES_API edge to %q; edges=%+v", toID, consumesEdges(recs))
	return types.RelationshipRecord{}
}

// ---------------------------------------------------------------------------
// Interface / registration
// ---------------------------------------------------------------------------

func TestLanguageKey(t *testing.T) {
	e := &Extractor{}
	if got := e.Language(); got != "_cross_consumes_api" {
		t.Errorf("Language()=%q, want _cross_consumes_api", got)
	}
}

func TestRegisteredInExtractorRegistry(t *testing.T) {
	ext, ok := extractor.Get("_cross_consumes_api")
	if !ok {
		t.Fatal("_cross_consumes_api not registered in extractor registry")
	}
	if ext.Language() != "_cross_consumes_api" {
		t.Errorf("registered extractor Language()=%q", ext.Language())
	}
}

func TestConsumesAPIIsValidRelationshipKind(t *testing.T) {
	if !types.IsValidRelationshipKind(string(types.RelationshipKindConsumesAPI)) {
		t.Fatal("CONSUMES_API is not registered in AllRelationshipKinds — producer-kind validator would reject it")
	}
}

func TestEmptyFileReturnsNil(t *testing.T) {
	if recs := runExtract(t, "empty.js", "javascript", ""); len(recs) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d", len(recs))
	}
}

// ---------------------------------------------------------------------------
// Pure matcher units (ported semantics)
// ---------------------------------------------------------------------------

func TestExtractURLPath(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com/api/users/123": "/api/users/123",
		"/api/users/123":                        "/api/users/123",
		"/api/users/123?expand=1":               "/api/users/123",
		"":                                      "",
		"://bad-host":                           "",
	}
	for in, want := range cases {
		if got := extractURLPath(in); got != want {
			t.Errorf("extractURLPath(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestNormalizePathCollapsesParamStyles(t *testing.T) {
	// Server (:id / {id} / <int:id>) and client (${id} / {*}) param styles must
	// all collapse to the same key.
	want := "/api/users/{*}"
	for _, in := range []string{
		"/api/users/:id",
		"/api/users/{id}",
		"/api/users/<int:id>",
		"/api/users/${userId}",
		"/api/users/{*}",
		"/api/Users/{id}/", // case + trailing slash
	} {
		if got := normalizePath(in); got != want {
			t.Errorf("normalizePath(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestMethodMatches(t *testing.T) {
	if !methodMatches("GET", "GET") {
		t.Error("GET should match GET")
	}
	if !methodMatches("get", "GET") {
		t.Error("case-insensitive verb match expected")
	}
	if !methodMatches("POST", "ANY") {
		t.Error("ANY endpoint verb is a wildcard")
	}
	if !methodMatches("DELETE", "*") {
		t.Error("* endpoint verb is a wildcard")
	}
	if methodMatches("GET", "POST") {
		t.Error("GET must NOT match a POST endpoint")
	}
	if methodMatches("", "GET") {
		t.Error("empty call verb must NOT match a specific endpoint verb")
	}
}

// ---------------------------------------------------------------------------
// VALUE assertion: a specific CONSUMES_API edge is emitted for a same-file
// client GET → server GET endpoint, param-normalized.
// ---------------------------------------------------------------------------

// expressBFF is a co-located client+server file: it both SERVES the route
// `GET /api/users/:id` (Express) and CALLS `GET /api/users/123` (axios). The
// extractor must join them into exactly one CONSUMES_API edge.
const expressBFF = `
import express from 'express';
import axios from 'axios';

const app = express();

// server: declares the endpoint
app.get('/api/users/:id', async (req, res) => {
  res.json({ id: req.params.id });
});

// client: consumes its own endpoint (template-literal id → {*} param)
async function fetchUser(id) {
  return axios.get(` + "`" + `https://gateway.internal/api/users/${id}` + "`" + `);
}
`

func TestEmitsSpecificConsumesAPIEdge(t *testing.T) {
	recs := runExtract(t, "bff/users.js", "javascript", expressBFF)

	edges := consumesEdges(recs)
	if len(edges) != 1 {
		t.Fatalf("expected exactly 1 CONSUMES_API edge, got %d: %+v", len(edges), edges)
	}

	// The endpoint extractor canonicalises `:id` → `{id}`, so the endpoint ID is
	// scope:endpoint:<file>#GET:/api/users/{id}. Assert the edge targets exactly
	// that endpoint identity and originates from the http_caller stub.
	wantEndpoint := "scope:endpoint:bff/users.js#GET:/api/users/{id}"
	rel := findConsumesTo(t, recs, wantEndpoint)

	if rel.FromID != "scope:component:http_caller:bff/users.js" {
		t.Errorf("CONSUMES_API FromID=%q, want the http_caller stub", rel.FromID)
	}
	if rel.Properties["method"] != "GET" {
		t.Errorf("edge method=%q, want GET", rel.Properties["method"])
	}
	if rel.Properties["matched_path"] != "/api/users/{*}" {
		t.Errorf("edge matched_path=%q, want /api/users/{*}", rel.Properties["matched_path"])
	}
	if rel.Properties["endpoint_path"] != "/api/users/{id}" {
		t.Errorf("edge endpoint_path=%q, want /api/users/{id}", rel.Properties["endpoint_path"])
	}
	if rel.Properties["via"] != "same_file_http_consumption" {
		t.Errorf("edge via=%q, want same_file_http_consumption", rel.Properties["via"])
	}
}

// ---------------------------------------------------------------------------
// NEGATIVE: no edge when the verb mismatches (client POST, server GET only).
// ---------------------------------------------------------------------------

const expressVerbMismatch = `
import express from 'express';
import axios from 'axios';
const app = express();
app.get('/api/users/:id', handler);
async function f(){ return axios.post(` + "`" + `https://h/api/users/${id}` + "`" + `); }
`

func TestNoEdgeOnVerbMismatch(t *testing.T) {
	recs := runExtract(t, "bff/mismatch.js", "javascript", expressVerbMismatch)
	if edges := consumesEdges(recs); len(edges) != 0 {
		t.Fatalf("expected 0 CONSUMES_API edges on verb mismatch, got %d: %+v", len(edges), edges)
	}
}

// ---------------------------------------------------------------------------
// NEGATIVE: no edge when the path does not match (different route).
// ---------------------------------------------------------------------------

const expressPathMismatch = `
import express from 'express';
import axios from 'axios';
const app = express();
app.get('/api/orders/:id', handler);
async function f(){ return axios.get(` + "`" + `https://h/api/users/${id}` + "`" + `); }
`

func TestNoEdgeOnPathMismatch(t *testing.T) {
	recs := runExtract(t, "bff/pathmiss.js", "javascript", expressPathMismatch)
	if edges := consumesEdges(recs); len(edges) != 0 {
		t.Fatalf("expected 0 CONSUMES_API edges on path mismatch, got %d: %+v", len(edges), edges)
	}
}

// ---------------------------------------------------------------------------
// NEGATIVE: a file with only a client call (no server endpoint) — the cross-repo
// case owned by links/http_pass — must NOT emit a CONSUMES_API edge here.
// ---------------------------------------------------------------------------

const clientOnly = `
import axios from 'axios';
async function f(){ return axios.get('https://h/api/users/9'); }
`

func TestNoEdgeWhenNoServerEndpointInFile(t *testing.T) {
	recs := runExtract(t, "client.js", "javascript", clientOnly)
	if edges := consumesEdges(recs); len(edges) != 0 {
		t.Fatalf("expected 0 CONSUMES_API edges when no server endpoint co-located, got %d: %+v", len(edges), edges)
	}
}
