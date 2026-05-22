package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// makePathsTestGroup builds a minimal DashGroup for Paths v2 handler tests.
func makePathsTestGroup(entities []graph.Entity, rels []graph.Relationship) *DashGroup {
	doc := &graph.Document{
		Repo:          "api-backend",
		Entities:      entities,
		Relationships: rels,
	}
	return &DashGroup{
		Name: "testgrp",
		Repos: map[string]*DashRepo{
			"api-backend": {Slug: "api-backend", Path: "/tmp/fake", Doc: doc},
		},
	}
}

// newPathsTestServer wires the server with an in-memory GraphCache seeded with
// grp and returns an httptest.Server.
func newPathsTestServer(t *testing.T, grp *DashGroup) *httptest.Server {
	t.Helper()
	store := newFakeStore()
	store.groups["testgrp"] = GroupSummary{Name: "testgrp", ConfigPath: "/tmp/testgrp.json"}

	srv, err := NewServer(DefaultConfig(), store)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Inject the group into the cache directly (same pattern as other handler tests).
	srv.graphs.mu.Lock()
	srv.graphs.entries["testgrp"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()
	return httptest.NewServer(srv.routes())
}

// ---------------------------------------------------------------------------
// v2PathsList
// ---------------------------------------------------------------------------

// TestV2PathsList_EmptyGroup verifies GET /api/v2/groups/:id/paths returns an
// ok:true envelope with empty backends and zero totals when there are no
// http_endpoint entities.
func TestV2PathsList_EmptyGroup(t *testing.T) {
	grp := makePathsTestGroup(nil, nil)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths")
	if err != nil {
		t.Fatalf("GET paths: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		OK   bool                `json:"ok"`
		Data v2PathsListResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Error("ok: want true")
	}
	if body.Data.Totals.Routes != 0 {
		t.Errorf("totals.routes: want 0, got %d", body.Data.Totals.Routes)
	}
	if len(body.Data.Backends) != 0 {
		t.Errorf("backends: want 0, got %d", len(body.Data.Backends))
	}
}

// TestV2PathsList_SingleEndpoint verifies GET /api/v2/groups/:id/paths groups
// a single http_endpoint entity into one backend with one controller and one route.
func TestV2PathsList_SingleEndpoint(t *testing.T) {
	entities := []graph.Entity{
		{
			ID:         "e1",
			Name:       "OrderViewSet.list",
			Kind:       "http_endpoint",
			SourceFile: "app/orders/views.py",
			StartLine:  10,
			Properties: map[string]string{
				"path":           "/api/v1/orders",
				"verb":           "GET",
				"framework":      "django-rest",
				"owning_backend": "api-backend",
				"auth":           "true",
				"auth_scheme":    "Bearer",
			},
		},
	}
	grp := makePathsTestGroup(entities, nil)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths")
	if err != nil {
		t.Fatalf("GET paths: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		OK   bool                `json:"ok"`
		Data v2PathsListResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Error("ok: want true")
	}

	totals := body.Data.Totals
	if totals.Routes != 1 {
		t.Errorf("totals.routes: want 1, got %d", totals.Routes)
	}
	if totals.Backends != 1 {
		t.Errorf("totals.backends: want 1, got %d", totals.Backends)
	}
	if totals.Endpoints < 1 {
		t.Errorf("totals.endpoints: want >=1, got %d", totals.Endpoints)
	}

	if len(body.Data.Backends) != 1 {
		t.Fatalf("backends: want 1, got %d", len(body.Data.Backends))
	}
	be := body.Data.Backends[0]
	if be.ServiceType != "REST" {
		t.Errorf("service_type: want REST, got %s", be.ServiceType)
	}
	if len(be.Groups) == 0 {
		t.Fatal("backend groups: want >=1, got 0")
	}
	grpRow := be.Groups[0]
	if len(grpRow.Routes) != 1 {
		t.Fatalf("routes in group: want 1, got %d", len(grpRow.Routes))
	}
	route := grpRow.Routes[0]
	if route.Path != "/api/v1/orders" {
		t.Errorf("route.path: want /api/v1/orders, got %s", route.Path)
	}
	if !route.Auth {
		t.Error("route.auth: want true")
	}
	if len(route.Verbs) != 1 || route.Verbs[0] != "GET" {
		t.Errorf("route.verbs: want [GET], got %v", route.Verbs)
	}
}

// TestV2PathsList_GrpcBackend verifies gRPC endpoints get service_type "gRPC".
func TestV2PathsList_GrpcBackend(t *testing.T) {
	entities := []graph.Entity{
		{
			ID:   "g1",
			Name: "OrderService.GetOrder",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"path":           "/order.OrderService/GetOrder",
				"verb":           "GRPC",
				"owning_backend": "gateway-grpc",
			},
		},
	}
	grp := makePathsTestGroup(entities, nil)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths")
	if err != nil {
		t.Fatalf("GET paths: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		OK   bool                `json:"ok"`
		Data v2PathsListResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data.Backends) != 1 {
		t.Fatalf("backends: want 1, got %d", len(body.Data.Backends))
	}
	if body.Data.Backends[0].ServiceType != "gRPC" {
		t.Errorf("service_type: want gRPC, got %s", body.Data.Backends[0].ServiceType)
	}
}

// TestV2PathsList_GroupNotFound verifies 404 when the group doesn't exist.
func TestV2PathsList_GroupNotFound(t *testing.T) {
	store := newFakeStore()
	srv, _ := NewServer(DefaultConfig(), store)
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/doesnotexist/paths")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
	var body struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.OK {
		t.Error("ok: want false")
	}
	if body.Error.Code != "group_not_found" {
		t.Errorf("error.code: want group_not_found, got %s", body.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// v2PathDetail
// ---------------------------------------------------------------------------

// TestV2PathDetail_Found verifies GET /api/v2/groups/:id/paths/:hash returns
// the full detail envelope for a known path hash.
func TestV2PathDetail_Found(t *testing.T) {
	path := "/api/v1/orders/{id}"
	hash := hashStr(path)
	entities := []graph.Entity{
		{
			ID:         "e2",
			Name:       "OrderViewSet.retrieve",
			Kind:       "http_endpoint",
			SourceFile: "app/orders/views.py",
			StartLine:  42,
			Properties: map[string]string{
				"path":           path,
				"verb":           "GET",
				"framework":      "django-rest",
				"owning_backend": "api-backend",
				"auth":           "true",
				"auth_scheme":    "Bearer",
			},
		},
	}
	grp := makePathsTestGroup(entities, nil)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths/" + hash)
	if err != nil {
		t.Fatalf("GET path detail: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		OK   bool         `json:"ok"`
		Data v2PathDetail `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Error("ok: want true")
	}
	if body.Data.Path != path {
		t.Errorf("path: want %s, got %s", path, body.Data.Path)
	}
	if body.Data.PathHash != hash {
		t.Errorf("path_hash: want %s, got %s", hash, body.Data.PathHash)
	}
	if !body.Data.Auth {
		t.Error("auth: want true")
	}
	if body.Data.AuthScheme != "Bearer" {
		t.Errorf("auth_scheme: want Bearer, got %s", body.Data.AuthScheme)
	}
	if len(body.Data.Verbs) != 1 || body.Data.Verbs[0] != "GET" {
		t.Errorf("verbs: want [GET], got %v", body.Data.Verbs)
	}
	// Path params should include {id}.
	hasIDParam := false
	for _, p := range body.Data.Parameters {
		if p.Name == "id" && p.In == "path" {
			hasIDParam = true
		}
	}
	if !hasIDParam {
		t.Error("parameters: want {id} path param extracted")
	}
}

// TestV2PathDetail_NotFound verifies 404 for unknown hash.
func TestV2PathDetail_NotFound(t *testing.T) {
	grp := makePathsTestGroup(nil, nil)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths/deadbeef00000000")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// v2PathsOrphans
// ---------------------------------------------------------------------------

// TestV2PathsOrphans_Empty verifies GET /api/v2/groups/:id/paths/orphans
// returns an empty list (not 404) when there are no orphan callers.
func TestV2PathsOrphans_Empty(t *testing.T) {
	grp := makePathsTestGroup(nil, nil)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths/orphans")
	if err != nil {
		t.Fatalf("GET orphans: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var body struct {
		OK   bool               `json:"ok"`
		Data v2OrphansResponse  `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Error("ok: want true")
	}
	if len(body.Data.Orphans) != 0 {
		t.Errorf("orphans: want 0, got %d", len(body.Data.Orphans))
	}
	totals := body.Data.Totals
	if totals.NoHandlerFound != 0 || totals.DynamicBaseURL != 0 || totals.TemplateLiteral != 0 {
		t.Errorf("totals: want all 0, got %+v", totals)
	}
}

// TestV2PathsOrphans_OrphanRoute verifies the orphan route registration:
// /api/v2/groups/:id/paths/orphans is matched BEFORE /:hash.
// A HEAD request to the orphans path should not return 405 (wrong handler).
func TestV2PathsOrphans_RouteRegistration(t *testing.T) {
	grp := makePathsTestGroup(nil, nil)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths/orphans")
	if err != nil {
		t.Fatalf("GET orphans: %v", err)
	}
	defer resp.Body.Close()
	// The critical invariant: /paths/orphans must not be routed to /paths/:hash.
	// If it were, the hash would be "orphans" and we'd get a 404 from the detail
	// handler (not a 200 orphans list).
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200 from orphans handler, got %d (route precedence bug?)", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// v2PathsList — v1 routes untouched
// ---------------------------------------------------------------------------

// TestV2PathsList_V1Untouched verifies that the v1 GET /api/paths/{group}
// endpoint still responds 200 after the v2 routes are registered. This is the
// "v1 untouched" acceptance criterion.
func TestV2PathsList_V1Untouched(t *testing.T) {
	entities := []graph.Entity{
		{
			ID:   "e3",
			Name: "MyHandler",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"path": "/api/v1/health",
				"verb": "GET",
			},
		},
	}
	grp := makePathsTestGroup(entities, nil)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/paths/testgrp")
	if err != nil {
		t.Fatalf("GET v1 paths: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("v1 GET /api/paths/{group}: want 200, got %d", resp.StatusCode)
	}
}
