package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// syntheticIDEndpoints returns endpoint entities shaped the way the real
// producers emit them: Name is the synthetic route ID
// ("http:<VERB>:<path>"), NOT a handler function name, and no
// owning_backend property is set — which is what deriveOwningBackend now
// yields for a single-service repo since #6555.
//
// The PascalCase path segments ("OrderService", "UserView",
// "OrdersController") are deliberate: they are the shapes that used to be
// mistaken for handler-name affixes and turned into URL-fragment backend
// names (#6592).
func syntheticIDEndpoints() []graph.Entity {
	mk := func(id, verb, path string) graph.Entity {
		synth := "http:" + verb + ":" + path
		return graph.Entity{
			ID:   id,
			Name: synth,
			Kind: "http_endpoint_definition",
		}.WithProperties(map[string]string{
			"method":         verb,
			"verb":           verb,
			"path":           path,
			"framework":      "gin",
			"qualified_name": synth,
		})
	}
	return []graph.Entity{
		mk("ep1", "GET", "/api/v1/users/{id}"),
		mk("ep2", "GET", "/api/OrderService/{id}"),
		mk("ep3", "POST", "/api/UserView/create"),
		mk("ep4", "GET", "/OrdersController/list"),
		mk("ep5", "GET", "/api/RouterStatus"),
	}
}

// TestExportOpenAPI_SyntheticIDNameYieldsRepoSlugTag pins the OpenAPI caller
// (handlers_export_openapi.go). Every operation tag must be the repo slug —
// no lowercased URL fragment may appear (#6592).
func TestExportOpenAPI_SyntheticIDNameYieldsRepoSlugTag(t *testing.T) {
	srv := newOpenAPITestServer(t, "g", syntheticIDEndpoints())
	req := httptest.NewRequest(http.MethodGet, "/api/export/g/openapi?format=json", nil)
	req.SetPathValue("group", "g")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var doc openAPIDoc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	if len(doc.Tags) != 1 || doc.Tags[0].Name != "repo1" {
		names := make([]string, 0, len(doc.Tags))
		for _, tg := range doc.Tags {
			names = append(names, tg.Name)
		}
		t.Fatalf("doc.Tags = %v, want exactly [repo1] (the repo slug)", names)
	}

	if len(doc.Paths) != 5 {
		t.Fatalf("paths = %d, want 5", len(doc.Paths))
	}
	for path, item := range doc.Paths {
		for verb, op := range map[string]*openAPIOperation{"GET": item.Get, "POST": item.Post} {
			if op == nil {
				continue
			}
			if len(op.Tags) != 1 || op.Tags[0] != "repo1" {
				t.Errorf("%s %s: tags = %v, want [repo1]", verb, path, op.Tags)
			}
		}
	}
}

// TestPathsList_SyntheticIDNameYieldsRepoSlugBackend pins the Paths-panel
// caller (handlers_paths.go). All five endpoints must collapse into the one
// repo-slug backend rather than fanning out into URL-fragment names (#6592).
func TestPathsList_SyntheticIDNameYieldsRepoSlugBackend(t *testing.T) {
	backends := pathsListBackends(t, syntheticIDEndpoints())
	if len(backends) != 1 {
		names := make([]string, 0, len(backends))
		for _, b := range backends {
			names = append(names, b.Name)
		}
		t.Fatalf("owning_backends = %v, want exactly one entry named repo1", names)
	}
	if backends[0].Name != "repo1" {
		t.Errorf("backend name = %q, want %q (the repo slug)", backends[0].Name, "repo1")
	}
	if backends[0].EndpointCount != 5 {
		t.Errorf("endpoint_count = %d, want 5", backends[0].EndpointCount)
	}
}

// pathsBackendRow is the subset of the Paths-panel owning_backends payload
// these tests assert on.
type pathsBackendRow struct {
	Name          string `json:"name"`
	EndpointCount int    `json:"endpoint_count"`
}

// pathsListBackends runs GET /api/paths/{group} over a single repo ("repo1")
// holding ents, and returns the owning_backends rows.
func pathsListBackends(t *testing.T, ents []graph.Entity) []pathsBackendRow {
	t.Helper()
	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["g"] = &cacheEntry{
		group:    openAPITestGroup("g", ents),
		loadedAt: time.Now(),
	}
	srv.graphs.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/paths/g", nil)
	req.SetPathValue("group", "g")
	w := httptest.NewRecorder()
	srv.handlePathsList(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var body struct {
		OwningBackends []pathsBackendRow `json:"owning_backends"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	return body.OwningBackends
}

// bareEndpoints returns endpoints carrying *no* properties beyond method,
// verb and path: no owning_backend and, deliberately, no framework either.
// This is a large real class — django urlconf mounts, webhooks, serverless,
// and YAML-rule Route entities — and it is the class that catches a fallback
// accidentally gated on some other property being present.
//
// The Names are the degenerate shapes: empty, and a synthetic ID whose path
// portion is missing.
func bareEndpoints() []graph.Entity {
	return []graph.Entity{
		graph.Entity{ID: "e1", Name: "", Kind: "http_endpoint_definition"}.
			WithProperties(map[string]string{"method": "GET", "verb": "GET", "path": "/api/ping"}),
		graph.Entity{ID: "e2", Name: "http::", Kind: "http_endpoint_definition"}.
			WithProperties(map[string]string{"method": "GET", "verb": "GET", "path": "/api/ServiceRegistry"}),
	}
}

// TestOwningBackendFallback_EmptyAndMalformedNames pins the degenerate
// shapes on the OpenAPI caller. Neither may produce anything other than the
// repo slug.
func TestOwningBackendFallback_EmptyAndMalformedNames(t *testing.T) {
	srv := newOpenAPITestServer(t, "g", bareEndpoints())
	req := httptest.NewRequest(http.MethodGet, "/api/export/g/openapi?format=json", nil)
	req.SetPathValue("group", "g")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var doc openAPIDoc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if len(doc.Tags) != 1 || doc.Tags[0].Name != "repo1" {
		t.Fatalf("doc.Tags = %+v, want exactly [repo1]", doc.Tags)
	}
}

// TestPathsList_BareEndpointsYieldRepoSlugBackend pins the same degenerate
// shapes on the *Paths* caller. The fallback must be unconditional: an
// endpoint carrying neither owning_backend nor framework still belongs to the
// repo slug, never to an empty backend name.
func TestPathsList_BareEndpointsYieldRepoSlugBackend(t *testing.T) {
	backends := pathsListBackends(t, bareEndpoints())
	if len(backends) != 1 {
		t.Fatalf("owning_backends = %+v, want exactly one entry", backends)
	}
	if backends[0].Name != "repo1" {
		t.Errorf("backend name = %q, want %q (the repo slug)", backends[0].Name, "repo1")
	}
	if backends[0].EndpointCount != 2 {
		t.Errorf("endpoint_count = %d, want 2", backends[0].EndpointCount)
	}
}

// TestOwningBackendProperty_StillWins guards the non-fallback path: when the
// graph carries an explicit owning_backend, it must be used verbatim and the
// repo slug must not override it.
func TestOwningBackendProperty_StillWins(t *testing.T) {
	ents := []graph.Entity{
		graph.Entity{ID: "e1", Name: "http:GET:/api/users", Kind: "http_endpoint_definition"}.
			WithProperties(map[string]string{
				"method": "GET", "verb": "GET", "path": "/api/users",
				"owning_backend": "user-service",
			}),
	}
	srv := newOpenAPITestServer(t, "g", ents)
	req := httptest.NewRequest(http.MethodGet, "/api/export/g/openapi?format=json", nil)
	req.SetPathValue("group", "g")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)
	var doc openAPIDoc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if len(doc.Tags) != 1 || doc.Tags[0].Name != "user-service" {
		t.Fatalf("doc.Tags = %+v, want exactly [user-service]", doc.Tags)
	}
}
