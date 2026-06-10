package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
		OK   bool              `json:"ok"`
		Data v2OrphansResponse `json:"data"`
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
// Grouping + label helpers (#1551)
// ---------------------------------------------------------------------------

func TestControllerLabelFromFile(t *testing.T) {
	cases := map[string]string{
		"src/orders.controller.ts": "OrdersController",
		"src/saga.controller.ts":   "SagaController",
		"src/resolvers.ts":         "resolvers",
		"src/index.ts":             "index",
		"app/orders/views.py":      "views",
		"":                         "",
	}
	for in, want := range cases {
		if got := controllerLabelFromFile(in); got != want {
			t.Errorf("controllerLabelFromFile(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBackendLabelFromRepo(t *testing.T) {
	all := []string{
		"polyglot-platform-services-gateway",
		"polyglot-platform-services-payments",
		"polyglot-platform-services-catalog",
	}
	if got := backendLabelFromRepo("polyglot-platform-services-gateway", all); got != "gateway" {
		t.Errorf("backendLabelFromRepo gateway = %q, want gateway", got)
	}
	// Single repo: no trimming.
	if got := backendLabelFromRepo("solo-repo", []string{"solo-repo"}); got != "solo-repo" {
		t.Errorf("backendLabelFromRepo solo = %q, want solo-repo", got)
	}
}

func TestServiceTypeFromVerbs(t *testing.T) {
	cases := []struct {
		verbs map[string]bool
		want  string
	}{
		{map[string]bool{"GRAPHQL": true}, "GraphQL"},
		{map[string]bool{"GRPC": true}, "gRPC"},
		{map[string]bool{"GET": true, "POST": true}, "REST"},
		{map[string]bool{"GET": true, "GRAPHQL": true}, "GraphQL"},
	}
	for _, c := range cases {
		if got := serviceTypeFromVerbs(c.verbs, "svc", nil); got != c.want {
			t.Errorf("serviceTypeFromVerbs(%v) = %q, want %q", c.verbs, got, c.want)
		}
	}
}

// TestV2PathsList_GroupsByFile verifies endpoints in the same source file are
// grouped into one controller, while different files become separate controllers
// — the real multi-module grouping behaviour (#1551).
func TestV2PathsList_GroupsByFile(t *testing.T) {
	entities := []graph.Entity{
		{ID: "a", Name: "list", Kind: "http_endpoint", SourceFile: "src/orders.controller.ts",
			Properties: map[string]string{"path": "/orders", "verb": "GET", "owning_backend": "src"}},
		{ID: "b", Name: "create", Kind: "http_endpoint", SourceFile: "src/orders.controller.ts",
			Properties: map[string]string{"path": "/orders/new", "verb": "POST", "owning_backend": "src"}},
		{ID: "c", Name: "checkout", Kind: "http_endpoint", SourceFile: "src/saga.controller.ts",
			Properties: map[string]string{"path": "/checkout", "verb": "POST", "owning_backend": "src"}},
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
		Data v2PathsListResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(body.Data.Backends) != 1 {
		t.Fatalf("backends: want 1 (repo), got %d", len(body.Data.Backends))
	}
	if got := len(body.Data.Backends[0].Groups); got != 2 {
		t.Fatalf("controllers: want 2 (by file), got %d", got)
	}
	if body.Data.Totals.Controllers != 2 {
		t.Errorf("totals.controllers: want 2, got %d", body.Data.Totals.Controllers)
	}
	// Parameters must never serialise as null (#1551).
	hash := hashStr("/orders")
	dr, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths/" + hash)
	if err != nil {
		t.Fatalf("GET detail: %v", err)
	}
	defer dr.Body.Close()
	var detail struct {
		Data v2PathDetail `json:"data"`
	}
	if err := json.NewDecoder(dr.Body).Decode(&detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.Data.Parameters == nil {
		t.Error("parameters: want [] (non-nil), got nil")
	}
}

// TestV2PathsList_RouteSourceFilePerEndpoint is the #4608 guard: every emitted
// route must carry its OWN defining `source_file`, and the `src/modules/<MODULE>`
// segment parsed from that file must match the route's own module — never a
// sibling's. The frontend module left-tree groups per-route by this field, so a
// stale/shared file here is exactly the cross-module mis-bucketing bug (an
// inspections endpoint showing under modules/dob-sync).
func TestV2PathsList_RouteSourceFilePerEndpoint(t *testing.T) {
	entities := []graph.Entity{
		{ID: "a", Name: "getCounts", Kind: "http_endpoint",
			SourceFile: "src/modules/inspections/api/inspection.controller.ts",
			Properties: map[string]string{"path": "/v1/inspections/get_counts", "verb": "GET", "owning_backend": "src"}},
		{ID: "b", Name: "sync", Kind: "http_endpoint",
			SourceFile: "src/modules/dob-sync/api/dob-sync.controller.ts",
			Properties: map[string]string{"path": "/v1/dob-sync/run", "verb": "POST", "owning_backend": "src"}},
		{ID: "c", Name: "templates", Kind: "http_endpoint",
			SourceFile: "src/modules/me-email-templates/api/me-email-templates.controller.ts",
			Properties: map[string]string{"path": "/v1/me-email-templates", "verb": "GET", "owning_backend": "src"}},
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
		Data v2PathsListResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Expected module segment per path, derived from each endpoint's OWN file.
	wantModule := map[string]string{
		"/v1/inspections/get_counts": "inspections",
		"/v1/dob-sync/run":           "dob-sync",
		"/v1/me-email-templates":     "me-email-templates",
	}
	seen := 0
	for _, b := range body.Data.Backends {
		for _, g := range b.Groups {
			for _, r := range g.Routes {
				if r.SourceFile == "" {
					t.Errorf("route %q: source_file is empty — frontend cannot derive its own module", r.Path)
					continue
				}
				wantMod, ok := wantModule[r.Path]
				if !ok {
					continue
				}
				seen++
				// Guard: an endpoint's module must equal `src/modules/<label>/`
				// of ITS OWN source file (#4608).
				wantPrefix := "src/modules/" + wantMod + "/"
				if !strings.HasPrefix(r.SourceFile, wantPrefix) {
					t.Errorf("route %q: source_file %q does not live under %q (cross-module mis-bucketing)",
						r.Path, r.SourceFile, wantPrefix)
				}
				if gotMod := moduleSegmentFromFile(r.SourceFile); gotMod != wantMod {
					t.Errorf("route %q: derived module %q, want %q", r.Path, gotMod, wantMod)
				}
			}
		}
	}
	if seen != len(wantModule) {
		t.Fatalf("expected %d guarded routes, saw %d", len(wantModule), seen)
	}
}

// moduleSegmentFromFile mirrors the frontend deriveModule() anchor rule: returns
// the segment immediately after a `modules/` (or apps/packages/services/domains)
// anchor in the path. Used by the #4608 guard to prove the backend stamps each
// route with a file whose module matches the route.
func moduleSegmentFromFile(file string) string {
	segs := strings.Split(strings.ReplaceAll(file, "\\", "/"), "/")
	anchors := map[string]bool{"modules": true, "apps": true, "packages": true, "services": true, "domains": true}
	for i := 0; i < len(segs)-1; i++ {
		if anchors[strings.ToLower(segs[i])] {
			return segs[i+1]
		}
	}
	return ""
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

// TestV2PathDetail_Issue1936_JavaParametersSurfaced verifies that when the
// Java annotation extractor emits a `parameters` JSON property on an
// http_endpoint entity, every parameter location surfaces in the detail
// response with the correct `in` bucket — query/header/cookie/body in
// addition to the path row synthesised from the URL template.
func TestV2PathDetail_Issue1936_JavaParametersSurfaced(t *testing.T) {
	path := "/transfers/confirm/{transferId}"
	hash := hashStr(path)
	// Mirror what ApplyJavaAnnotationRoutes would produce on a JAX-RS
	// PUT handler with mixed parameter locations.
	paramsJSON := `[` +
		`{"name":"transferId","in":"path","type":"String","required":true,"annotations":["@PathParam"]},` +
		`{"name":"dryRun","in":"query","type":"boolean","required":false,"default_value":"false","annotations":["@QueryParam","@DefaultValue"]},` +
		`{"name":"X-Request-ID","in":"header","type":"String","required":true,"annotations":["@HeaderParam"]},` +
		`{"name":"session","in":"cookie","type":"String","required":false,"annotations":["@CookieParam"]},` +
		`{"name":"body","in":"body","type":"ConfirmRequest","required":true,"annotations":["@Valid"]}` +
		`]`
	entities := []graph.Entity{
		{
			ID:         "e-java-1",
			Name:       "TransfersResource.confirm",
			Kind:       "http_endpoint",
			SourceFile: "src/main/java/TransfersResource.java",
			StartLine:  17,
			Language:   "java",
			Properties: map[string]string{
				"path":       path,
				"verb":       "PUT",
				"framework":  "jaxrs",
				"parameters": paramsJSON,
			},
		},
	}
	grp := makePathsTestGroup(entities, nil)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths/" + hash)
	if err != nil {
		t.Fatalf("GET detail: %v", err)
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
	wantIns := map[string]string{
		"transferId":   "path",
		"dryRun":       "query",
		"X-Request-ID": "header",
		"session":      "cookie",
		"body":         "body",
	}
	gotByName := map[string]v2PathParameter{}
	for _, p := range body.Data.Parameters {
		gotByName[p.Name] = p
	}
	for name, wantIn := range wantIns {
		p, ok := gotByName[name]
		if !ok {
			t.Errorf("[#1936] parameter %q missing from detail response", name)
			continue
		}
		if p.In != wantIn {
			t.Errorf("[#1936] %q: want in=%s, got %s", name, wantIn, p.In)
		}
	}
	// dryRun must be optional (default value present).
	if dr, ok := gotByName["dryRun"]; ok && dr.Required {
		t.Errorf("[#1936] dryRun: should not be required when default value present, got %+v", dr)
	}
}
