package dashboard

// paths_handler_resolve_test.go — #1646 unit tests for endpoint-definition →
// handler resolution and the v2 detail / list re-grouping.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// drfRoleFixture mirrors the acme shape: a synthetic http_endpoint_definition
// at routers.py:0 + the RoleViewSet.retrieve handler linked by IMPLEMENTS, plus
// a real CALLS edge into a downstream helper and a model REFERENCES edge that
// must surface as a Side-effect.
func drfRoleFixture() ([]graph.Entity, []graph.Relationship) {
	entities := []graph.Entity{
		// Synthetic endpoint definition produced by the DRF router expansion.
		graph.Entity{
			ID:         "ep_get_roles_pk",
			Name:       "http:GET:/api/v1/roles/{pk}",
			Kind:       "http_endpoint_definition",
			SourceFile: "core/routers.py",
			StartLine:  0,
		}.WithProperties(map[string]string{"path": "/api/v1/roles/{pk}", "verb": "GET", "framework": "drf"}),
		// Real handler — a viewset method.
		{
			ID:            "op_retrieve",
			Name:          "RoleViewSet.retrieve",
			QualifiedName: "core.views.role_viewset.RoleViewSet.retrieve",
			Kind:          "SCOPE.Operation",
			SourceFile:    "core/views/role_viewset.py",
			StartLine:     42,
			Language:      "python",
		},
		// A downstream operation the handler CALLS.
		{
			ID: "op_helper", Name: "find_role_by_pk", Kind: "SCOPE.Operation",
			SourceFile: "core/services/role_service.py", StartLine: 10,
		},
		// A Model the handler REFERENCES (a DB touch → Side-effect).
		{
			ID: "model_role", Name: "Role", Kind: "Model",
			SourceFile: "core/models/role.py", StartLine: 5,
		},
		// A test method that TESTS the handler.
		{
			ID: "op_test", Name: "RoleViewSetTests.test_retrieve", Kind: "SCOPE.Operation",
			SourceFile: "core/tests/test_role_viewset.py", StartLine: 12,
		},
		// A frontend FETCH call site retargeted to the definition.
		{
			ID: "fe_caller", Name: "useRoleDetail", Kind: "SCOPE.Operation",
			SourceFile: "src/hooks/useRoleDetail.ts", StartLine: 7,
		},
	}
	rels := []graph.Relationship{
		// Handler IMPLEMENTS the definition — this is what the resolver follows.
		{FromID: "op_retrieve", ToID: "ep_get_roles_pk", Kind: "IMPLEMENTS"},
		// Handler's outbound edges (downstream + side-effects).
		{FromID: "op_retrieve", ToID: "op_helper", Kind: "CALLS"},
		{FromID: "op_retrieve", ToID: "model_role", Kind: "REFERENCES"},
		// Test → handler.
		{FromID: "op_test", ToID: "op_retrieve", Kind: "TESTS"},
		// Retargeted frontend FETCHES → definition.
		{FromID: "fe_caller", ToID: "ep_get_roles_pk", Kind: "FETCHES"},
	}
	return entities, rels
}

// TestV2PathDetail_PopulatesSectionsViaIMPLEMENTS verifies #1646: the detail
// pane resolves the definition to its IMPLEMENTS-linked handler and traverses
// the handler's edges to populate Called-by, Downstream, Side-effects, Tests.
func TestV2PathDetail_PopulatesSectionsViaIMPLEMENTS(t *testing.T) {
	entities, rels := drfRoleFixture()
	grp := makePathsTestGroup(entities, rels)
	ts := newPathsTestServer(t, grp)
	defer ts.Close()

	hash := hashStr("/api/v1/roles/{pk}")
	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp/paths/" + hash)
	if err != nil {
		t.Fatalf("GET detail: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200 got %d", resp.StatusCode)
	}
	var body struct {
		Data v2PathDetail `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	d := body.Data

	// Handler card must point at the resolved viewset method, not the
	// routers.py:0 synthetic.
	if len(d.Handlers) != 1 {
		t.Fatalf("handlers: want 1 got %d", len(d.Handlers))
	}
	h := d.Handlers[0]
	if h.SourceFile != "core/views/role_viewset.py" || h.StartLine != 42 {
		t.Errorf("handler source: want core/views/role_viewset.py:42, got %s:%d", h.SourceFile, h.StartLine)
	}
	if h.QualifiedName != "core.views.role_viewset.RoleViewSet.retrieve" {
		t.Errorf("handler qualified_name: got %q", h.QualifiedName)
	}

	// Called-by must include the frontend fetch caller.
	if len(d.InboundFetches) != 1 || d.InboundFetches[0].QualifiedName == "" && d.InboundFetches[0].Label != "useRoleDetail" {
		t.Errorf("called-by: want useRoleDetail, got %+v", d.InboundFetches)
	}
	// Downstream must include the helper operation the handler CALLS.
	allDown := append(append(append(append([]v2PathEntity{}, d.Outbound.DB...), d.Outbound.Event...),
		d.Outbound.Queue...), d.Outbound.External...)
	allDown = append(allDown, d.Outbound.GRPC...)
	found := false
	for _, x := range allDown {
		if x.Label == "find_role_by_pk" {
			found = true
		}
	}
	if !found {
		t.Errorf("downstream: missing find_role_by_pk, got %+v", allDown)
	}
	// Side-effects must include the Role model REFERENCES.
	foundSE := false
	for _, x := range d.SideEffects {
		if x.Label == "Role" {
			foundSE = true
		}
	}
	if !foundSE {
		t.Errorf("side_effects: missing Role model, got %+v", d.SideEffects)
	}
	// Tests must include the test method that TESTS the handler.
	if len(d.Tests) != 1 || d.Tests[0].Label != "RoleViewSetTests.test_retrieve" {
		t.Errorf("tests: want RoleViewSetTests.test_retrieve, got %+v", d.Tests)
	}
}

// TestV2PathsList_GroupsByViewSet_NotRouterFile verifies #1646: when endpoint
// definitions are router-expanded synthetics all sharing routers.py, the list
// must group routes by their IMPLEMENTS-resolved viewset, not by the shared
// router file.
func TestV2PathsList_GroupsByViewSet_NotRouterFile(t *testing.T) {
	entities := []graph.Entity{
		// Two router-expanded definitions — both at routers.py:0.
		graph.Entity{ID: "ep_roles_get", Name: "http:GET:/api/v1/roles", Kind: "http_endpoint_definition",
			SourceFile: "core/routers.py", StartLine: 0,
		}.WithProperties(map[string]string{"path": "/api/v1/roles", "verb": "GET"}),
		graph.Entity{ID: "ep_buildings_get", Name: "http:GET:/api/v1/buildings", Kind: "http_endpoint_definition",
			SourceFile: "core/routers.py", StartLine: 0,
		}.WithProperties(map[string]string{"path": "/api/v1/buildings", "verb": "GET"}),
		// Two viewset handlers in different files.
		{ID: "op_role_list", Name: "RoleViewSet.list", Kind: "SCOPE.Operation",
			SourceFile: "core/views/role_viewset.py", StartLine: 10},
		{ID: "op_bldg_list", Name: "BuildingViewSet.list", Kind: "SCOPE.Operation",
			SourceFile: "core/views/building_viewset.py", StartLine: 12},
	}
	rels := []graph.Relationship{
		{FromID: "op_role_list", ToID: "ep_roles_get", Kind: "IMPLEMENTS"},
		{FromID: "op_bldg_list", ToID: "ep_buildings_get", Kind: "IMPLEMENTS"},
	}
	grp := makePathsTestGroup(entities, rels)
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
		t.Fatalf("backends: want 1, got %d", len(body.Data.Backends))
	}
	groups := body.Data.Backends[0].Groups
	if len(groups) != 2 {
		t.Fatalf("controllers: want 2 (one per viewset), got %d (labels=%v)",
			len(groups), groupLabels(groups))
	}
	labels := map[string]bool{}
	for _, g := range groups {
		labels[g.Label] = true
	}
	if !labels["RoleViewSet"] || !labels["BuildingViewSet"] {
		t.Errorf("controller labels: want {RoleViewSet, BuildingViewSet}, got %v", labels)
	}
	if body.Data.Totals.Controllers != 2 {
		t.Errorf("totals.controllers: want 2, got %d", body.Data.Totals.Controllers)
	}
}

func groupLabels(gs []v2ControllerGroup) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		out = append(out, g.Label)
	}
	return out
}

// ---------------------------------------------------------------------------
// Cross-repo Called-by (#1891)
// ---------------------------------------------------------------------------

// makeCrossRepoPathsGroup builds a two-repo DashGroup that mirrors the R5
// dogfood topology: a backend repo exposes an http_endpoint_definition for
// /auth/login with an IMPLEMENTS-linked handler; a client repo has an
// http_endpoint_call entity that invokes the same endpoint. The cross-repo link
// (Source=client:caller, Target=backend:handler) is injected into grp.Links.
func makeCrossRepoPathsGroup() *DashGroup {
	// Backend repo: definition + handler.
	backendEntities := []graph.Entity{
		graph.Entity{
			ID:         "ep_login",
			Name:       "http:POST:/auth/login",
			Kind:       "http_endpoint_definition",
			SourceFile: "api/auth/routes.py",
			StartLine:  0,
		}.WithProperties(map[string]string{"path": "/auth/login", "verb": "POST", "framework": "drf"}),
		{
			ID:            "op_login",
			Name:          "AuthViewSet.login",
			QualifiedName: "api.auth.views.AuthViewSet.login",
			Kind:          "SCOPE.Operation",
			SourceFile:    "api/auth/views.py",
			StartLine:     15,
			Language:      "python",
		},
	}
	backendRels := []graph.Relationship{
		{FromID: "op_login", ToID: "ep_login", Kind: "IMPLEMENTS"},
	}
	backendDoc := &graph.Document{
		Repo:          "client-fixture-d",
		Entities:      backendEntities,
		Relationships: backendRels,
	}

	// Client repo: an http_endpoint_call entity that calls /auth/login.
	clientEntities := []graph.Entity{
		graph.Entity{
			ID:         "call_login",
			Name:       "http:POST:/auth/login",
			Kind:       "http_endpoint_call",
			SourceFile: "src/api/auth.ts",
			StartLine:  22,
		}.WithProperties(map[string]string{"path": "/auth/login", "verb": "POST"}),
		{
			ID:         "fn_do_login",
			Name:       "doLogin",
			Kind:       "SCOPE.Operation",
			SourceFile: "src/api/auth.ts",
			StartLine:  20,
		},
	}
	clientRels := []graph.Relationship{
		{FromID: "fn_do_login", ToID: "call_login", Kind: "FETCHES"},
	}
	clientDoc := &graph.Document{
		Repo:          "client-fixture-e",
		Entities:      clientEntities,
		Relationships: clientRels,
	}

	grp := &DashGroup{
		Name: "testgrp-cross",
		Repos: map[string]*DashRepo{
			"client-fixture-d": {Slug: "client-fixture-d", Path: "/tmp/fake-d", Doc: backendDoc},
			"client-fixture-e": {Slug: "client-fixture-e", Path: "/tmp/fake-e", Doc: clientDoc},
		},
		// Cross-repo link: client caller → backend handler (the http_pass emits this).
		// normalizeLinkEndpoints would normally rewrite "<repo>::<id>" → "<slug>:<id>";
		// here we pre-compute the canonical dashPrefixedID form directly.
		Links: []CrossRepoLink{
			{
				Source: dashPrefixedID("client-fixture-e", "fn_do_login"),
				Target: dashPrefixedID("client-fixture-d", "op_login"),
				Kind:   "calls",
				Method: "http",
			},
		},
	}
	return grp
}

// TestV2PathDetail_CrossRepoCalledBy verifies #1891: the detail pane populates
// the Called-by section from cross-repo links (grp.Links) when the intra-repo
// FETCHES traversal produces no results because the caller lives in a different
// repo than the endpoint definition.
func TestV2PathDetail_CrossRepoCalledBy(t *testing.T) {
	grp := makeCrossRepoPathsGroup()

	store := newFakeStore()
	store.groups["testgrp-cross"] = GroupSummary{Name: "testgrp-cross", ConfigPath: "/tmp/cross.json"}
	srv, err := NewServer(DefaultConfig(), store)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["testgrp-cross"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	hash := hashStr("/auth/login")
	resp, err := http.Get(ts.URL + "/api/v2/groups/testgrp-cross/paths/" + hash)
	if err != nil {
		t.Fatalf("GET detail: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200 got %d", resp.StatusCode)
	}
	var body struct {
		Data v2PathDetail `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	d := body.Data

	// The critical assertion: Called-by must include the cross-repo caller.
	if len(d.InboundFetches) == 0 {
		t.Fatalf("inbound_fetches (Called by): want ≥1 cross-repo caller, got 0 (before fix this was always 0 due to #1891)")
	}
	found := false
	for _, e := range d.InboundFetches {
		if e.Label == "doLogin" && e.Repo == "client-fixture-e" {
			found = true
		}
	}
	if !found {
		t.Errorf("inbound_fetches: want doLogin from client-fixture-e, got %+v", d.InboundFetches)
	}
}

// TestHandlerGroupKey covers the name → grouping-key heuristic.
func TestHandlerGroupKey(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"viewset method", "RoleViewSet.retrieve", "RoleViewSet"},
		{"nested method", "auth.RoleViewSet.create", "auth.RoleViewSet"},
		{"bare function view", "health_check", "health_check"},
		{"scope-separated", "pkg::OrderViewSet", "OrderViewSet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := handlerGroupKey(&graph.Entity{Name: tc.in})
			if got != tc.want {
				t.Errorf("handlerGroupKey(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
