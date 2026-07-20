package dashboard

// v2_paths_posture_test.go — unit tests for the lazy Paths posture +
// effective-contract sibling route (#4254). Verifies the dashboard reuses the
// MCP endpoint_posture / effective_contract computation and surfaces it under
// the v2 envelope, with honest-empty handling.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/mcp"
)

// postureResponseEnvelope decodes the v2OK envelope around v2PathPostureResponse.
type postureResponseEnvelope struct {
	OK   bool                  `json:"ok"`
	Data v2PathPostureResponse `json:"data"`
}

func fetchPosture(t *testing.T, srv *Server, group, hash string) v2PathPostureResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/groups/"+group+"/paths/"+hash+"/posture", nil)
	req.SetPathValue("id", group)
	req.SetPathValue("hash", hash)
	rw := httptest.NewRecorder()
	srv.handleV2PathPosture(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rw.Code, rw.Body.String())
	}
	var env postureResponseEnvelope
	if err := json.NewDecoder(rw.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v — body: %s", err, rw.Body.String())
	}
	if !env.OK {
		t.Fatalf("envelope ok=false: %s", rw.Body.String())
	}
	return env.Data
}

func injectPostureGroup(t *testing.T, name string, doc *graph.Document) *Server {
	t.Helper()
	cache := NewGraphCache(60 * time.Second)
	srv := &Server{graphs: cache}
	grp := &DashGroup{Name: name, Repos: map[string]*DashRepo{doc.Repo: {Slug: doc.Repo, Doc: doc}}}
	cache.mu.Lock()
	cache.entries[name] = &cacheEntry{group: grp, loadedAt: time.Now()}
	cache.mu.Unlock()
	return srv
}

// TestPathPosture_SurfacesFacets — an endpoint carrying THROWS / rate_limit /
// deprecation / feature-gate / auth props is surfaced with its posture, AND the
// router-expanded route's effective contract resolves per verb.
func TestPathPosture_SurfacesFacets(t *testing.T) {
	const path = "/api/v1/roles/{pk}"
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			// The DRF router-expanded POST route — carries the stamped per-verb
			// effective contract AND posture props.
			graph.Entity{
				ID:   "ep:roles:post",
				Name: "RoleViewSet.create",
				Kind: "http_endpoint_definition",

				// posture props

				SourceFile: "roles/views.py",
				StartLine:  12,
			}.WithProperties(map[string]string{
				"path":                     path,
				"verb":                     "POST",
				"pattern_type":             "drf_router_expanded",
				"drf_view_method":          "RoleViewSet.create",
				"effective_kind":           "inherited",
				"effective_source_class":   "CreateModelMixin",
				"effective_status":         "201",
				"effective_error_statuses": "400",
				"serializer_class":         "RoleSerializer",
				"middleware_names":         "IsAuthenticated",
				"auth_required":            "true",

				"rate_limited": "true",
				"rate_limit":   "100/min",
				"deprecated":   "true",
				"api_version":  "v1",
				"auth_method":  "session",
			},
			),
			// The exception type the route throws (THROWS target).
			{
				ID:   "exc:ValidationError",
				Name: "exception:ValidationError",
				Kind: "SCOPE.ExceptionType",
			},
			// A feature flag the route is gated by (GATED_BY target).
			{
				ID:   "feature:roles_v2",
				Name: "feature:roles_v2",
				Kind: "SCOPE.FeatureFlag",
			},
		},
		Relationships: []graph.Relationship{
			{ID: "t1", FromID: "ep:roles:post", ToID: "exc:ValidationError", Kind: "THROWS"},
			{ID: "g1", FromID: "ep:roles:post", ToID: "feature:roles_v2", Kind: "GATED_BY"},
		},
	}

	srv := injectPostureGroup(t, "g1", doc)
	resp := fetchPosture(t, srv, "g1", hashStr(path))

	if resp.Path != path {
		t.Errorf("Path = %q, want %q", resp.Path, path)
	}

	// --- Posture assertions ---
	if len(resp.Endpoints) == 0 {
		t.Fatalf("expected at least one posture endpoint")
	}
	var found *mcp.PosturePayload
	for i := range resp.Endpoints {
		if resp.Endpoints[i].EntityID == "svc::ep:roles:post" {
			found = &resp.Endpoints[i]
		}
	}
	if found == nil {
		t.Fatalf("posture endpoint svc::ep:roles:post not found in %+v", resp.Endpoints)
	}
	if !found.HasPosture {
		t.Errorf("expected HasPosture=true")
	}
	if found.ErrorFlow == nil || len(found.ErrorFlow.Throws) != 1 || found.ErrorFlow.Throws[0] != "ValidationError" {
		t.Errorf("error_flow throws = %+v, want [ValidationError]", found.ErrorFlow)
	}
	if found.RateLimit["rate_limit"] != "100/min" {
		t.Errorf("rate_limit = %+v, want 100/min", found.RateLimit)
	}
	if found.Deprecation["api_version"] != "v1" {
		t.Errorf("deprecation api_version = %+v, want v1", found.Deprecation)
	}
	if len(found.FeatureGate) != 1 || found.FeatureGate[0] != "roles_v2" {
		t.Errorf("feature_gates = %+v, want [roles_v2]", found.FeatureGate)
	}
	if found.Auth["auth_method"] != "session" {
		t.Errorf("auth = %+v, want auth_method=session", found.Auth)
	}

	// --- Effective contract assertions (reused MRO/pack-aware projection) ---
	if !resp.ContractApplicable {
		t.Errorf("expected ContractApplicable=true for a DRF ViewSet path")
	}
	if resp.Contract == nil {
		t.Fatalf("expected non-nil effective contract for a DRF ViewSet path")
	}
	if len(resp.Contract.Groups) == 0 {
		t.Fatalf("expected at least one contract group, note=%q", resp.Contract.Note)
	}
	var post *mcp.EffectiveContract
	for gi := range resp.Contract.Groups {
		for hi := range resp.Contract.Groups[gi].Handlers {
			h := &resp.Contract.Groups[gi].Handlers[hi]
			if h.Verb == "POST" {
				post = h
			}
		}
	}
	if post == nil {
		t.Fatalf("POST contract not found in %+v", resp.Contract.Groups)
	}
	if post.DefaultStatus != 201 {
		t.Errorf("POST default_status = %d, want 201", post.DefaultStatus)
	}
	if len(post.ErrorStatuses) != 1 || post.ErrorStatuses[0] != 400 {
		t.Errorf("POST error_statuses = %+v, want [400]", post.ErrorStatuses)
	}
	if post.Serializer != "RoleSerializer" {
		t.Errorf("POST serializer = %q, want RoleSerializer", post.Serializer)
	}
	if post.Kind != "inherited" {
		t.Errorf("POST kind = %q, want inherited (MRO-inherited handler)", post.Kind)
	}
}

// TestPathPosture_HonestEmpty — a plain endpoint with no posture facets and no
// DRF ViewSet still returns 200 with an honest-empty posture row and a null
// contract (non-ViewSet endpoint).
func TestPathPosture_HonestEmpty(t *testing.T) {
	const path = "/health"
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			graph.Entity{
				ID:   "ep:health",
				Name: "healthCheck",
				Kind: "http_endpoint",

				SourceFile: "main.go",
				StartLine:  3,
			}.WithProperties(map[string]string{"path": path, "verb": "GET"}),
		},
	}
	srv := injectPostureGroup(t, "g2", doc)
	resp := fetchPosture(t, srv, "g2", hashStr(path))

	if len(resp.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint row, got %d", len(resp.Endpoints))
	}
	if resp.Endpoints[0].HasPosture {
		t.Errorf("expected HasPosture=false for a bare endpoint")
	}
	if resp.Contract != nil {
		t.Errorf("expected nil contract for non-ViewSet endpoint, got %+v", resp.Contract)
	}
	if resp.ContractApplicable {
		t.Errorf("expected ContractApplicable=false for a non-DRF endpoint")
	}
}

// TestPathPosture_NestJSNotApplicable — #4486: a NestJS controller endpoint must
// NOT surface the DRF-only effective contract (nor any DRF empty-state prose):
// the contract is nil and ContractApplicable is false so the UI hides the
// section entirely.
func TestPathPosture_NestJSNotApplicable(t *testing.T) {
	const path = "/api/users"
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			graph.Entity{
				ID:   "ep:users:get",
				Name: "UsersController.findAll",
				Kind: "http_endpoint",

				SourceFile: "src/users/users.controller.ts",
				StartLine:  10,
			}.WithProperties(map[string]string{
				"path":      path,
				"verb":      "GET",
				"framework": "nestjs",
			},
			),
		},
	}
	srv := injectPostureGroup(t, "g-nest", doc)
	resp := fetchPosture(t, srv, "g-nest", hashStr(path))

	if resp.ContractApplicable {
		t.Errorf("expected ContractApplicable=false for a NestJS endpoint")
	}
	if resp.Contract != nil {
		t.Errorf("expected nil contract for a NestJS endpoint (no DRF prose leak), got %+v", resp.Contract)
	}
}

// TestPathPosture_NotFound — an unknown hash returns 404.
func TestPathPosture_NotFound(t *testing.T) {
	doc := &graph.Document{Repo: "svc"}
	srv := injectPostureGroup(t, "g3", doc)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/groups/g3/paths/deadbeef/posture", nil)
	req.SetPathValue("id", "g3")
	req.SetPathValue("hash", "deadbeef")
	rw := httptest.NewRecorder()
	srv.handleV2PathPosture(rw, req)
	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d — %s", rw.Code, rw.Body.String())
	}
}
