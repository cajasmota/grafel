package dashboard

// handlers_phase1_test.go — unit tests for Phase 1 REST endpoints.
//
// Each test uses httptest.NewServer(s.routes()) and an in-memory fakeStore,
// so no disk I/O is required.  Graph data is injected via a fakeGraphCache
// that satisfies the same *GraphCache API by embedding it.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/mcp"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newPhase1Server builds a test server with a seeded GraphCache containing
// a fake group "testgroup" with one repo "svc".
func newPhase1Server(t *testing.T) (*httptest.Server, *GraphCache) {
	t.Helper()
	st := newFakeStore()
	st.groups["testgroup"] = GroupSummary{
		Name:       "testgroup",
		ConfigPath: "/tmp/testgroup.json",
		Repos:      []string{"svc"},
	}

	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Inject a fake graph into the cache.
	grp := fakeDashGroup()
	srv.graphs.mu.Lock()
	srv.graphs.entries["testgroup"] = &cacheEntry{
		group:    grp,
		loadedAt: time.Now(),
	}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts, srv.graphs
}

// fakeDashGroup returns a minimal DashGroup with one repo and a handful of
// entities / relationships / communities for testing.
func fakeDashGroup() *DashGroup {
	pr1 := 0.8
	pr2 := 0.3
	cid1 := 0
	cid2 := 1

	godTrue := true

	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{
				ID:          "e1",
				Name:        "UserService",
				Kind:        "SCOPE.Component",
				SourceFile:  "src/user_service.go",
				StartLine:   1,
				EndLine:     50,
				Language:    "go",
				PageRank:    &pr1,
				IsGodNode:   true,
				CommunityID: &cid1,
				Properties:  map[string]string{"framework": "gin"},
			},
			{
				ID:          "e2",
				Name:        "AuthHandler",
				Kind:        "SCOPE.Function",
				SourceFile:  "src/auth.go",
				StartLine:   10,
				EndLine:     30,
				Language:    "go",
				PageRank:    &pr2,
				CommunityID: &cid2,
			},
			{
				ID:         "e3",
				Name:       "POST /api/auth/login",
				Kind:       "Endpoint",
				SourceFile: "src/routes.go",
				StartLine:  5,
				EndLine:    5,
				Language:   "go",
				Properties: map[string]string{
					"verb":      "POST",
					"path":      "/api/auth/login",
					"framework": "gin",
				},
			},
			{
				ID:         "e4",
				Name:       "GET /api/users",
				Kind:       "Endpoint",
				SourceFile: "src/routes.go",
				StartLine:  10,
				EndLine:    10,
				Language:   "go",
				Properties: map[string]string{
					"verb":      "GET",
					"path":      "/api/users",
					"framework": "gin",
				},
			},
			{
				ID:         "e5",
				Name:       "UserCreatedTopic",
				Kind:       "MessageTopic",
				SourceFile: "src/events.go",
				StartLine:  1,
				EndLine:    10,
				Language:   "go",
				Properties: map[string]string{"broker": "kafka"},
			},
			{
				ID:         "e6",
				Name:       "MainProcess",
				Kind:       "SCOPE.Process",
				SourceFile: "src/main.go",
				StartLine:  1,
				EndLine:    100,
				Language:   "go",
				Properties: map[string]string{
					"cross_stack":  "false",
					"step_count":   "2",
					"entry_id":     "e1",
					"entry_name":   "UserService",
					"terminal_id":  "e2",
					"chain_labels": "UserService,AuthHandler",
				},
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "e1", ToID: "e2", Kind: "CALLS"},
			{ID: "r2", FromID: "e1", ToID: "e5", Kind: "PUBLISHES_TO"},
			{
				ID:         "r3",
				FromID:     "e2",
				ToID:       "e6",
				Kind:       "STEP_IN_PROCESS",
				Properties: map[string]string{"step_index": "0"},
			},
			{
				ID:         "r4",
				FromID:     "e1",
				ToID:       "e6",
				Kind:       "STEP_IN_PROCESS",
				Properties: map[string]string{"step_index": "1"},
			},
		},
		Communities: []graph.CommunityResult{
			{ID: 0, Size: 3, AutoName: "user-auth", TopEntities: []string{"e1", "e2"}},
			{ID: 1, Size: 2, AutoName: "events", TopEntities: []string{"e5"}},
		},
	}

	// silence unused variable warning
	_ = godTrue

	return &DashGroup{
		Name: "testgroup",
		Repos: map[string]*DashRepo{
			"svc": {Slug: "svc", Path: "/tmp/fake-svc", Doc: doc},
		},
		Links: []CrossRepoLink{},
	}
}

func getJSON(t *testing.T, base, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	return resp.StatusCode, body
}

// ---------------------------------------------------------------------------
// /api/dashboard/init
// ---------------------------------------------------------------------------

func TestDashboardInit_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/dashboard/init")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	if _, ok := body["groups"]; !ok {
		t.Fatalf("missing groups key: %+v", body)
	}
	if _, ok := body["registry"]; !ok {
		t.Fatalf("missing registry key: %+v", body)
	}
	if _, ok := body["served_at"]; !ok {
		t.Fatalf("missing served_at key: %+v", body)
	}
}

// ---------------------------------------------------------------------------
// /api/graph/{group}
// ---------------------------------------------------------------------------

func TestGraph_Full_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	// #1023: lod param is ignored; always returns dense tier.
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	nodes, _ := body["nodes"].([]interface{})
	// Should include all entities (doc has 6 entities, below dense cap of 500).
	if len(nodes) != 6 {
		t.Fatalf("expected 6 nodes, got %d", len(nodes))
	}
	// #1023: no lod_level in response; use total_node_count instead.
	if body["lod_level"] != nil {
		t.Fatalf("lod_level should be absent after #1023, got: %v", body["lod_level"])
	}
}

func TestGraph_Centroids_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	// #1023: lod=centroids param is ignored; always returns dense tier.
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	nodes, _ := body["nodes"].([]interface{})
	// Dense tier returns all 6 entities (no centroid collapsing post-#1023).
	if len(nodes) != 6 {
		t.Fatalf("expected 6 nodes (dense), got %d", len(nodes))
	}
	if body["lod_level"] != nil {
		t.Fatalf("lod_level should be absent after #1023, got: %v", body["lod_level"])
	}
}

func TestGraph_Mid_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	// #1023: lod=mid param is ignored; always returns dense tier.
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	if body["lod_level"] != nil {
		t.Fatalf("lod_level should be absent after #1023, got: %v", body["lod_level"])
	}
}

func TestGraph_UnknownGroup_404(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, _ := getJSON(t, ts.URL, "/api/graph/nonexistent")
	if code != 404 {
		t.Fatalf("expected 404, got %d", code)
	}
}

func TestGraph_FilterKind(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup?filter_kind=Function")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	nodes, _ := body["nodes"].([]interface{})
	// Only "AuthHandler" is SCOPE.Function.
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node (Function), got %d", len(nodes))
	}
}

// ---------------------------------------------------------------------------
// /api/graph/{group}/entity/{id}
// ---------------------------------------------------------------------------

func TestGraphEntity_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup/entity/svc::e1")
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	entity, ok := body["entity"].(map[string]any)
	if !ok {
		t.Fatalf("missing entity: %+v", body)
	}
	if entity["label"] != "UserService" {
		t.Fatalf("wrong label: %v", entity["label"])
	}
	outbound, _ := body["outbound_edges"].([]interface{})
	if len(outbound) == 0 {
		t.Fatalf("expected outbound edges")
	}
}

func TestGraphEntity_NotFound_404(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, _ := getJSON(t, ts.URL, "/api/graph/testgroup/entity/svc::nonexistent")
	if code != 404 {
		t.Fatalf("expected 404, got %d", code)
	}
}

// ---------------------------------------------------------------------------
// /api/graph/{group}/labels — Tier 2
// ---------------------------------------------------------------------------

func TestGraphLabels_TopN(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup/labels?top=3")
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	labels, ok := body["labels"].([]interface{})
	if !ok {
		t.Fatalf("missing labels array: %+v", body)
	}
	if len(labels) == 0 {
		t.Fatalf("expected labels, got 0")
	}
	if len(labels) > 3 {
		t.Fatalf("expected at most 3 labels (top=3), got %d", len(labels))
	}
	// Each entry must have id and label fields.
	entry, _ := labels[0].(map[string]any)
	if entry["id"] == nil || entry["label"] == nil {
		t.Fatalf("label entry missing id or label: %+v", entry)
	}
}

func TestGraphLabels_IDs(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup/labels?ids=svc::e1")
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	labels, ok := body["labels"].([]interface{})
	if !ok {
		t.Fatalf("missing labels array: %+v", body)
	}
	if len(labels) != 1 {
		t.Fatalf("expected 1 label for ids=svc::e1, got %d", len(labels))
	}
	entry, _ := labels[0].(map[string]any)
	if entry["label"] != "UserService" {
		t.Fatalf("wrong label: %v", entry["label"])
	}
}

func TestGraphLabels_UnknownGroup_404(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, _ := getJSON(t, ts.URL, "/api/graph/nonexistent/labels")
	if code != 404 {
		t.Fatalf("expected 404, got %d", code)
	}
}

func TestGraph_LitePayload_LabelOnAllNodes(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	nodes, _ := body["nodes"].([]interface{})
	if len(nodes) == 0 {
		t.Fatalf("expected nodes")
	}
	// #1374: Tier 1 compact nodes include `kind` and `label` for every node.
	// Previously only Process nodes carried label, causing other nodes to render
	// as repo::<hash-id> in the graph view. Now all nodes carry their human name.
	// source_file and pagerank remain excluded.
	for _, n := range nodes {
		nm, _ := n.(map[string]any)
		// kind is always present (#1121 P3 — frontend needs it for Process sizing)
		if nm["kind"] == nil {
			t.Errorf("Tier 1 node missing kind field; id=%v", nm["id"])
		}
		// label is now always present (#1374 — all nodes must carry human-readable name)
		if nm["label"] == nil {
			t.Errorf("Tier 1 node missing label field; kind=%v id=%v", nm["kind"], nm["id"])
		}
		// source_file stays excluded
		if nm["source_file"] != nil {
			t.Errorf("Tier 1 node should not carry source_file field; got %v", nm["source_file"])
		}
		// Mandatory fields
		if nm["id"] == nil {
			t.Errorf("Tier 1 node missing id")
		}
		if nm["repo"] == nil {
			t.Errorf("Tier 1 node missing repo")
		}
	}
}

// ---------------------------------------------------------------------------
// /api/flows/{group}
// ---------------------------------------------------------------------------

func TestFlowsList_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	// min_steps=0 disables the short-flow filter (#1639) — this fixture
	// process has only 2 steps and is testing list shape, not filtering.
	code, body := getJSON(t, ts.URL, "/api/flows/testgroup?min_steps=0")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	processes, _ := body["processes"].([]interface{})
	if len(processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(processes))
	}
	proc := processes[0].(map[string]any)
	if proc["label"] != "MainProcess" {
		t.Fatalf("wrong label: %v", proc["label"])
	}
}

func TestFlowsList_CrossStackFilter(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/flows/testgroup?cross_stack_only=true&min_steps=0")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	processes, _ := body["processes"].([]interface{})
	// The fake process has cross_stack=false, so should return 0.
	if len(processes) != 0 {
		t.Fatalf("expected 0 cross-stack processes, got %d", len(processes))
	}
}

func TestFlowDetail_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/flows/testgroup/svc::e6")
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	proc, ok := body["process"].(map[string]any)
	if !ok {
		t.Fatalf("missing process: %+v", body)
	}
	if proc["label"] != "MainProcess" {
		t.Fatalf("wrong label: %v", proc["label"])
	}
}

func TestFlowDetail_NotFound_404(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, _ := getJSON(t, ts.URL, "/api/flows/testgroup/svc::noprocess")
	if code != 404 {
		t.Fatalf("expected 404, got %d", code)
	}
}

// ---------------------------------------------------------------------------
// /api/paths/{group}
// ---------------------------------------------------------------------------

func TestPathsList_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/paths/testgroup")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	paths, _ := body["paths"].([]interface{})
	// Two endpoint entities: POST /api/auth/login and GET /api/users.
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(paths))
	}
	if _, ok := body["tree"]; !ok {
		t.Fatalf("missing tree")
	}
	if _, ok := body["total"]; !ok {
		t.Fatalf("missing total")
	}
}

func TestPathsList_PrefixFilter(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/paths/testgroup?prefix=/api/auth")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	paths, _ := body["paths"].([]interface{})
	if len(paths) != 1 {
		t.Fatalf("expected 1 path with prefix /api/auth, got %d", len(paths))
	}
}

func TestPathsList_SearchFilter(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/paths/testgroup?q=users")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	paths, _ := body["paths"].([]interface{})
	if len(paths) != 1 {
		t.Fatalf("expected 1 path matching 'users', got %d", len(paths))
	}
}

// TestPathsList_OwningBackends_Present verifies that owning_backends is
// present in the response and has at least one entry (#1218).
func TestPathsList_OwningBackends_Present(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/paths/testgroup")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	// owning_backends must be present and be an array.
	raw, ok := body["owning_backends"]
	if !ok {
		t.Fatalf("owning_backends missing from response")
	}
	backends, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("owning_backends is not an array: %T", raw)
	}
	if len(backends) == 0 {
		t.Fatalf("expected at least 1 backend entry, got 0")
	}
	// Verify required fields are present on each entry.
	for i, b := range backends {
		bm, ok := b.(map[string]any)
		if !ok {
			t.Fatalf("backend[%d] is not an object", i)
		}
		if bm["name"] == nil {
			t.Errorf("backend[%d] missing name", i)
		}
		if bm["endpoint_count"] == nil {
			t.Errorf("backend[%d] missing endpoint_count", i)
		}
		if bm["endpoints"] == nil {
			t.Errorf("backend[%d] missing endpoints array", i)
		}
		if bm["repos"] == nil {
			t.Errorf("backend[%d] missing repos array", i)
		}
	}
}

// TestPathsList_OwningBackends_MultiBackend verifies that a group with
// two repos each owning distinct http_endpoint_definition entities produces
// two entries in owning_backends sorted by endpoint_count descending.
func TestPathsList_OwningBackends_MultiBackend(t *testing.T) {
	st := newFakeStore()
	st.groups["multigrp"] = GroupSummary{
		Name:       "multigrp",
		ConfigPath: "/tmp/multigrp.json",
		Repos:      []string{"core-api", "admin-api"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// core-api: 3 http_endpoint_definition endpoints owned by "core"
	coreDoc := &graph.Document{
		Repo: "core-api",
		Entities: []graph.Entity{
			{
				ID: "ce1", Name: "GET /api/users", Kind: "http_endpoint_definition",
				SourceFile: "routes.go", Language: "go",
				Properties: map[string]string{"verb": "GET", "path": "/api/users", "owning_backend": "core"},
			},
			{
				ID: "ce2", Name: "POST /api/users", Kind: "http_endpoint_definition",
				SourceFile: "routes.go", Language: "go",
				Properties: map[string]string{"verb": "POST", "path": "/api/users/create", "owning_backend": "core"},
			},
			{
				ID: "ce3", Name: "GET /api/products", Kind: "http_endpoint_definition",
				SourceFile: "routes.go", Language: "go",
				Properties: map[string]string{"verb": "GET", "path": "/api/products", "owning_backend": "core"},
			},
		},
	}
	// admin-api: 1 http_endpoint_definition endpoint owned by "admin"
	adminDoc := &graph.Document{
		Repo: "admin-api",
		Entities: []graph.Entity{
			{
				ID: "ae1", Name: "GET /admin/users", Kind: "http_endpoint_definition",
				SourceFile: "admin_routes.go", Language: "go",
				Properties: map[string]string{"verb": "GET", "path": "/admin/users", "owning_backend": "admin"},
			},
		},
	}

	grp := &DashGroup{
		Name: "multigrp",
		Repos: map[string]*DashRepo{
			"core-api":  {Slug: "core-api", Path: "/tmp/core-api", Doc: coreDoc},
			"admin-api": {Slug: "admin-api", Path: "/tmp/admin-api", Doc: adminDoc},
		},
		Links: []CrossRepoLink{},
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["multigrp"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	code, body := getJSON(t, ts.URL, "/api/paths/multigrp")
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}

	backends, ok := body["owning_backends"].([]interface{})
	if !ok {
		t.Fatalf("owning_backends missing or not array: %v", body["owning_backends"])
	}
	if len(backends) != 2 {
		t.Fatalf("expected 2 backends (core, admin), got %d: %v", len(backends), backends)
	}

	// First backend should be "core" (3 endpoints > 1 endpoint).
	first := backends[0].(map[string]any)
	if first["name"] != "core" {
		t.Errorf("expected first backend = core (most endpoints), got %v", first["name"])
	}
	firstCount := int(first["endpoint_count"].(float64))
	if firstCount != 3 {
		t.Errorf("expected core endpoint_count=3, got %d", firstCount)
	}

	second := backends[1].(map[string]any)
	if second["name"] != "admin" {
		t.Errorf("expected second backend = admin, got %v", second["name"])
	}
	secondCount := int(second["endpoint_count"].(float64))
	if secondCount != 1 {
		t.Errorf("expected admin endpoint_count=1, got %d", secondCount)
	}

	// Verify total endpoint count across backends matches top-level total.
	total := int(body["total"].(float64))
	if total != firstCount+secondCount {
		t.Errorf("total=%d does not match sum of backend counts %d+%d", total, firstCount, secondCount)
	}
}

// TestPathsList_OwningBackends_SingleBackend verifies that a group with all
// endpoints from one backend produces exactly one entry in owning_backends.
func TestPathsList_OwningBackends_SingleBackend(t *testing.T) {
	st := newFakeStore()
	st.groups["singlegrp"] = GroupSummary{
		Name:       "singlegrp",
		ConfigPath: "/tmp/singlegrp.json",
		Repos:      []string{"my-api"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	doc := &graph.Document{
		Repo: "my-api",
		Entities: []graph.Entity{
			{
				ID: "ep1", Name: "GET /health", Kind: "http_endpoint_definition",
				SourceFile: "routes.go", Language: "go",
				Properties: map[string]string{"verb": "GET", "path": "/health", "owning_backend": "my-api"},
			},
			{
				ID: "ep2", Name: "GET /api/items", Kind: "http_endpoint_definition",
				SourceFile: "routes.go", Language: "go",
				Properties: map[string]string{"verb": "GET", "path": "/api/items", "owning_backend": "my-api"},
			},
		},
	}

	grp := &DashGroup{
		Name: "singlegrp",
		Repos: map[string]*DashRepo{
			"my-api": {Slug: "my-api", Path: "/tmp/my-api", Doc: doc},
		},
		Links: []CrossRepoLink{},
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["singlegrp"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	code, body := getJSON(t, ts.URL, "/api/paths/singlegrp")
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}

	backends, ok := body["owning_backends"].([]interface{})
	if !ok {
		t.Fatalf("owning_backends missing or not array: %v", body["owning_backends"])
	}
	if len(backends) != 1 {
		t.Fatalf("expected 1 backend entry for single-backend group, got %d", len(backends))
	}

	bm := backends[0].(map[string]any)
	if bm["name"] != "my-api" {
		t.Errorf("expected backend name my-api, got %v", bm["name"])
	}
	cnt := int(bm["endpoint_count"].(float64))
	if cnt != 2 {
		t.Errorf("expected endpoint_count=2, got %d", cnt)
	}

	// Count math: total must equal sum of backend endpoint_counts.
	total := int(body["total"].(float64))
	if total != cnt {
		t.Errorf("total=%d does not match backend endpoint_count=%d", total, cnt)
	}
}

// TestIsHTTPEndpointPath verifies the path-shape predicate used by the
// Paths list API to filter out XML namespace XPath strings (issue #1125).
func TestIsHTTPEndpointPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		// Valid HTTP paths.
		{"/api/v1/users", true},
		{"/api/v1/users/{id}", true},
		{"/webhooks/stripe", true},
		{"/v1/orders/{pk}", true},
		{"/", true},
		{"https://example.com/api/v1/users", true},
		{"http://localhost/api/orders", true},
		// XML namespace XPath strings — must be rejected.
		{"./w:tblBorders", false},
		{"./w:tcBorders", false},
		{"/./w:tblBorders", false},      // canonicalized form
		{"/api/v1/w:something", false},  // XML prefix colon in segment
		{"/w:root", false},
		{"/div[@class='x']", false}, // XPath attribute selector
		{"", false},
		// Long "prefix" before colon — NOT XML namespace, should pass.
		{"/version1/items", true},  // no colon at all
	}
	for _, tc := range cases {
		got := isHTTPEndpointPath(tc.path)
		if got != tc.want {
			t.Errorf("isHTTPEndpointPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestPathsList_XMLNoiseFiltered verifies that XML namespace XPath strings
// (e.g. "./w:tblBorders") are excluded from the Paths list even when they
// are present as Route or http_endpoint entities in the graph (issue #1125).
func TestPathsList_XMLNoiseFiltered(t *testing.T) {
	// Build a server with extra XML-noise entities injected.
	st := newFakeStore()
	st.groups["xmlgroup"] = GroupSummary{
		Name:       "xmlgroup",
		ConfigPath: "/tmp/xmlgroup.json",
		Repos:      []string{"svc"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	grp := fakeDashGroup()
	grp.Name = "xmlgroup"

	// Inject XML-noise Route and http_endpoint entities that should be
	// filtered out from the Paths list.
	xmlEntities := []graph.Entity{
		{
			ID:         "xml1",
			Name:       "./w:tblBorders",
			Kind:       "Route",
			SourceFile: "docx_utils.py",
			Language:   "python",
			Properties: map[string]string{
				"pattern_type": "ast_driven",
				"framework":    "python",
			},
		},
		{
			ID:         "xml2",
			Name:       "./w:tcBorders",
			Kind:       "Route",
			SourceFile: "docx_utils.py",
			Language:   "python",
			Properties: map[string]string{
				"pattern_type": "ast_driven",
				"framework":    "python",
			},
		},
		{
			ID:   "xml3",
			Name: "http:ANY:/./w:tblBorders",
			Kind: "http_endpoint",
			Properties: map[string]string{
				"path":         "/./w:tblBorders",
				"verb":         "ANY",
				"pattern_type": "http_endpoint_synthesis",
			},
		},
	}
	doc := grp.Repos["svc"].Doc
	for _, e := range xmlEntities {
		doc.Entities = append(doc.Entities, e)
	}

	srv.graphs.mu.Lock()
	srv.graphs.entries["xmlgroup"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	code, body := getJSON(t, ts.URL, "/api/paths/xmlgroup")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	paths, _ := body["paths"].([]interface{})

	// Verify none of the paths contain XML namespace strings.
	for _, p := range paths {
		pm := p.(map[string]any)
		pathStr, _ := pm["path"].(string)
		if strings.Contains(pathStr, "w:tblBorders") || strings.Contains(pathStr, "w:tcBorders") {
			t.Errorf("XML namespace path %q should not appear in Paths list", pathStr)
		}
		if strings.Contains(pathStr, "./") {
			t.Errorf("XPath relative path %q should not appear in Paths list", pathStr)
		}
	}
}

func TestPathDetail_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	// Compute the hash for /api/users.
	h := hashStr("/api/users")
	code, body := getJSON(t, ts.URL, "/api/paths/testgroup/"+h)
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	if body["path"] != "/api/users" {
		t.Fatalf("wrong path: %v", body["path"])
	}
}

func TestPathDetail_NotFound_404(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, _ := getJSON(t, ts.URL, "/api/paths/testgroup/badhash00")
	if code != 404 {
		t.Fatalf("expected 404, got %d", code)
	}
}

func TestPathDetail_DocgenFields(t *testing.T) {
	ts, _ := newPhase1Server(t)
	// Compute the hash for /api/users.
	h := hashStr("/api/users")
	code, body := getJSON(t, ts.URL, "/api/paths/testgroup/"+h)
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}

	// Verify response structure is valid.
	if body["handlers"] == nil {
		t.Fatalf("handlers field missing")
	}

	// Check that response is valid JSON and handlers is an array.
	handlers, ok := body["handlers"].([]interface{})
	if !ok {
		t.Fatalf("handlers is not an array: %T", body["handlers"])
	}
	if len(handlers) == 0 {
		t.Fatal("handlers array is empty")
	}

	// When docgen hasn't run, has_docs should be absent or false.
	// Since we use omitempty, it will be absent when false.
	handler := handlers[0].(map[string]any)
	if hasDocs, ok := handler["has_docs"].(bool); ok && hasDocs {
		t.Fatalf("has_docs should not be true when docgen hasn't run")
	}

	// Verify the handler has the basic fields we expect.
	if handler["entity"] == nil {
		t.Fatalf("entity field missing from handler")
	}
	if handler["verb"] == nil {
		t.Fatalf("verb field missing from handler")
	}
}

func TestPathDetail_DocgenFields_WithDocumentation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	ts, _ := newPhase1Server(t)

	// Create docgen state with a generated doc file for the /api/users endpoint.
	pathHash := hashStr("/api/users")
	docPath := "reference/endpoints/" + pathHash + ".md"
	docsDir := filepath.Join(tmp, ".archigraph", "groups", "testgroup", "docs", "reference", "endpoints")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a sample doc file.
	docContent := "# GET /api/users\n\nFetch all users from the system with pagination support.\n\n## Parameters\n- page: Page number\n- limit: Results per page\n"
	if err := os.WriteFile(filepath.Join(docsDir, pathHash+".md"), []byte(docContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Save docgen state.
	now := time.Now().UTC()
	docgenState := mcp.DocgenState{
		LastDocgenAt:   &now,
		GeneratedPaths: []string{docPath},
	}
	if err := mcp.SaveDocgenState("testgroup", docgenState); err != nil {
		t.Fatal(err)
	}

	// Query the endpoint.
	code, body := getJSON(t, ts.URL, "/api/paths/testgroup/"+pathHash)
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}

	// Verify has_docs is true.
	handlers, ok := body["handlers"].([]interface{})
	if !ok || len(handlers) == 0 {
		t.Fatal("handlers empty or not found")
	}

	handler := handlers[0].(map[string]any)
	if hasDocs, ok := handler["has_docs"].(bool); !ok || !hasDocs {
		t.Fatalf("has_docs should be true when docs exist, got: %v", handler["has_docs"])
	}

	// Verify docs_summary is populated.
	if summary, ok := handler["docs_summary"].(string); !ok || summary == "" {
		t.Fatalf("docs_summary should be populated, got: %v", handler["docs_summary"])
	}

	// Verify docs_path is populated.
	if path, ok := handler["docs_path"].(string); !ok || path == "" {
		t.Fatalf("docs_path should be populated, got: %v", handler["docs_path"])
	}
}

// ---------------------------------------------------------------------------
// /api/topology/{group}
// ---------------------------------------------------------------------------

func TestTopology_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/topology/testgroup")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	topics, _ := body["topics"].([]interface{})
	if len(topics) != 1 {
		t.Fatalf("expected 1 topic (UserCreatedTopic), got %d", len(topics))
	}
	topic := topics[0].(map[string]any)
	if topic["label"] != "UserCreatedTopic" {
		t.Fatalf("wrong topic label: %v", topic["label"])
	}
	if topic["broker"] != "kafka" {
		t.Fatalf("wrong broker: %v", topic["broker"])
	}
}

// TestTopology_NullGuard_NatsSubjects verifies that a group with no NATS
// edges returns nats_subjects: [] (not null) in the JSON wire shape (#944).
func TestTopology_NullGuard_NatsSubjects(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/topology/testgroup")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	// All array fields must be present and be arrays (not null).
	for _, field := range []string{"nats_subjects", "graphql_subscriptions", "transforms", "queues", "channels"} {
		v, exists := body[field]
		if !exists {
			t.Errorf("field %q missing from topology response", field)
			continue
		}
		if v == nil {
			t.Errorf("field %q is null, want []", field)
			continue
		}
		if _, ok := v.([]interface{}); !ok {
			t.Errorf("field %q is type %T, want []interface{}", field, v)
		}
	}
}

// ---------------------------------------------------------------------------
// /api/search/{group}
// ---------------------------------------------------------------------------

func TestSearch_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/search/testgroup?q=User")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	entities, _ := body["entities"].([]interface{})
	if len(entities) == 0 {
		t.Fatalf("expected entity results for 'User'")
	}
	// First result should be UserService (prefix match = score 2).
	first := entities[0].(map[string]any)
	if !strings.HasPrefix(first["label"].(string), "User") {
		t.Fatalf("unexpected first result: %v", first["label"])
	}
}

func TestSearch_NoQuery_400(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, _ := getJSON(t, ts.URL, "/api/search/testgroup")
	if code != 400 {
		t.Fatalf("expected 400, got %d", code)
	}
}

// ---------------------------------------------------------------------------
// /api/patterns/{group}
// ---------------------------------------------------------------------------

func TestPatterns_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	// No patterns.json on disk for testgroup — should return empty list, not error.
	code, body := getJSON(t, ts.URL, "/api/patterns/testgroup")
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	patterns, _ := body["patterns"].([]interface{})
	// Empty is valid.
	_ = patterns
}

// ---------------------------------------------------------------------------
// /api/repairs/{group}
// ---------------------------------------------------------------------------

func TestRepairs_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	// Repos have empty paths so readRepairCandidates returns nil — should be
	// empty but not 500.
	code, body := getJSON(t, ts.URL, "/api/repairs/testgroup")
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}
	if _, ok := body["open_count"]; !ok {
		t.Fatalf("missing open_count")
	}
}

// ---------------------------------------------------------------------------
// /api/groups/{group}/communities
// ---------------------------------------------------------------------------

func TestGroupCommunities_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/groups/testgroup/communities")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	comms, _ := body["communities"].([]interface{})
	if len(comms) != 2 {
		t.Fatalf("expected 2 communities, got %d", len(comms))
	}
}

// ---------------------------------------------------------------------------
// /api/groups/{group}/god-nodes
// ---------------------------------------------------------------------------

func TestGroupGodNodes_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/groups/testgroup/god-nodes")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	nodes, _ := body["god_nodes"].([]interface{})
	if len(nodes) != 1 {
		t.Fatalf("expected 1 god node (UserService), got %d", len(nodes))
	}
}

// ---------------------------------------------------------------------------
// /api/groups/{group}/links
// ---------------------------------------------------------------------------

func TestGroupLinks_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/groups/testgroup/links")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	links, _ := body["links"].([]interface{})
	if links == nil {
		t.Fatalf("links should be [] not nil")
	}
}

// ---------------------------------------------------------------------------
// /api/source
// ---------------------------------------------------------------------------

func TestSource_MissingParams_400(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, _ := getJSON(t, ts.URL, "/api/source")
	if code != 400 {
		t.Fatalf("expected 400, got %d", code)
	}
}

func TestSource_UnknownEntity_404(t *testing.T) {
	ts, _ := newPhase1Server(t)
	v := url.Values{}
	v.Set("node_id", "svc::notexists")
	v.Set("group", "testgroup")
	code, _ := getJSON(t, ts.URL, "/api/source?"+v.Encode())
	if code != 404 {
		t.Fatalf("expected 404, got %d", code)
	}
}

// ---------------------------------------------------------------------------
// Prefix tree
// ---------------------------------------------------------------------------

func TestBuildPrefixTree(t *testing.T) {
	rows := []PathRow{
		{Path: "/api/auth/login"},
		{Path: "/api/auth/logout"},
		{Path: "/api/users"},
		{Path: "/health"},
	}
	tree := buildPrefixTree(rows)
	// Root level: "api" and "health"
	if len(tree) != 2 {
		t.Fatalf("expected 2 root segments, got %d: %+v", len(tree), tree)
	}
	// "api" should have children "auth" and "users"
	var apiNode *PathTreeNode
	for i := range tree {
		if tree[i].Segment == "api" {
			apiNode = &tree[i]
			break
		}
	}
	if apiNode == nil {
		t.Fatalf("missing 'api' node")
	}
	if len(apiNode.Children) != 2 {
		t.Fatalf("expected 2 children under api, got %d", len(apiNode.Children))
	}
}

// ---------------------------------------------------------------------------
// Hash helper
// ---------------------------------------------------------------------------

func TestHashStr_Stable(t *testing.T) {
	h1 := hashStr("/api/users")
	h2 := hashStr("/api/users")
	if h1 != h2 {
		t.Fatalf("hashStr not stable: %q != %q", h1, h2)
	}
	h3 := hashStr("/api/auth")
	if h1 == h3 {
		t.Fatalf("hashStr collision for different paths")
	}
}

// ---------------------------------------------------------------------------
// splitChainLabels
// ---------------------------------------------------------------------------

func TestSplitChainLabels(t *testing.T) {
	cases := []struct {
		in  string
		out []string
	}{
		{"", []string{}},
		{"A,B,C", []string{"A", "B", "C"}},
		{" A , B ", []string{"A", "B"}},
	}
	for _, c := range cases {
		got := splitChainLabels(c.in)
		if len(got) != len(c.out) {
			t.Errorf("splitChainLabels(%q) = %v, want %v", c.in, got, c.out)
			continue
		}
		for i, v := range got {
			if v != c.out[i] {
				t.Errorf("splitChainLabels(%q)[%d] = %q, want %q", c.in, i, v, c.out[i])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Backtick symbol extraction
// ---------------------------------------------------------------------------

func TestExtractBacktickSymbols(t *testing.T) {
	md := "Use `UserService` and `AuthHandler` for auth."
	syms := extractBacktickSymbols(md)
	if len(syms) != 2 {
		t.Fatalf("expected 2 symbols, got %d: %v", len(syms), syms)
	}
	if syms[0] != "UserService" || syms[1] != "AuthHandler" {
		t.Fatalf("unexpected symbols: %v", syms)
	}
}

// ---------------------------------------------------------------------------
// Graph isolated-node regression tests (#1020)
// ---------------------------------------------------------------------------

// TestBuildDegreeMap verifies that buildDegreeMap correctly counts in+out edges.
func TestBuildDegreeMap(t *testing.T) {
	rels := []graph.Relationship{
		{ID: "r1", FromID: "A", ToID: "B", Kind: "CALLS"},
		{ID: "r2", FromID: "A", ToID: "C", Kind: "CALLS"},
		{ID: "r3", FromID: "B", ToID: "C", Kind: "CALLS"},
	}
	deg := buildDegreeMap(rels)
	// A: out=2 → degree 2; B: in=1+out=1 → degree 2; C: in=2 → degree 2
	if deg["A"] != 2 {
		t.Errorf("degree[A]=%d want 2", deg["A"])
	}
	if deg["B"] != 2 {
		t.Errorf("degree[B]=%d want 2", deg["B"])
	}
	if deg["C"] != 2 {
		t.Errorf("degree[C]=%d want 2", deg["C"])
	}
	if deg["X"] != 0 {
		t.Errorf("degree[X] should be 0 for unknown node")
	}
}

// TestGraph_Dense_EdgeConnectivity verifies that the dense tier returns edges
// where both endpoints are in the node set (low isolated-node rate).
// The fake group has 4 relationships so the high-degree nodes should all
// survive the denseNodeLimit cap, yielding in-sample edges.
func TestGraph_Dense_EdgeConnectivity(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup?lod=dense")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	nodes, _ := body["nodes"].([]interface{})
	edges, _ := body["edges"].([]interface{})
	if len(nodes) == 0 {
		t.Fatalf("expected nodes, got 0")
	}
	if len(edges) == 0 {
		t.Fatalf("expected edges in dense response, got 0; nodes=%d", len(nodes))
	}
	// Build node ID set.
	nodeIDs := map[string]bool{}
	for _, n := range nodes {
		nm, _ := n.(map[string]any)
		id, _ := nm["id"].(string)
		nodeIDs[id] = true
	}
	// Count edges where both endpoints are in the node set.
	connected := 0
	for _, e := range edges {
		em, _ := e.(map[string]any)
		from, _ := em["from_id"].(string)
		to, _ := em["to_id"].(string)
		if nodeIDs[from] && nodeIDs[to] {
			connected++
		}
	}
	if connected == 0 {
		t.Errorf("all %d edges are isolated (no both-endpoint match); dense tier should include connected edges", len(edges))
	}
}

// TestGraph_CrossRepoEdges_MergedIntoPayload verifies that cross-repo links
// (grp.Links) are included in the GET /api/graph/{group} edge list when both
// endpoints are present in the returned node set. Regression test for #1388:
// before the fix serveGraphDense only iterated per-repo Relationships and
// never emitted grp.Links, so the unified multi-repo graph showed 0 cross-repo
// edges even though the link pass had computed them.
func TestGraph_CrossRepoEdges_MergedIntoPayload(t *testing.T) {
	st := newFakeStore()
	st.groups["xrepogrp"] = GroupSummary{
		Name:       "xrepogrp",
		ConfigPath: "/tmp/xrepogrp.json",
		Repos:      []string{"frontend", "backend"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	frontendDoc := &graph.Document{
		Repo: "frontend",
		Entities: []graph.Entity{
			{ID: "fe1", Name: "FetchUsers", Kind: "SCOPE.Function", SourceFile: "src/api.ts", Language: "typescript"},
		},
	}
	backendDoc := &graph.Document{
		Repo: "backend",
		Entities: []graph.Entity{
			{ID: "be1", Name: "GET /api/users", Kind: "http_endpoint_definition", SourceFile: "routes.go", Language: "go"},
		},
	}

	grp := &DashGroup{
		Name: "xrepogrp",
		Repos: map[string]*DashRepo{
			"frontend": {Slug: "frontend", Path: "/tmp/frontend", Doc: frontendDoc},
			"backend":  {Slug: "backend", Path: "/tmp/backend", Doc: backendDoc},
		},
		// Cross-repo link: frontend FetchUsers → backend GET /api/users
		Links: []CrossRepoLink{
			{
				Source:     "frontend::fe1",
				Target:     "backend::be1",
				Kind:       "HTTP_FETCH",
				Confidence: 0.95,
			},
		},
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["xrepogrp"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	code, body := getJSON(t, ts.URL, "/api/graph/xrepogrp")
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}

	edges, _ := body["edges"].([]interface{})
	// Must contain the cross-repo edge.
	var xrepoEdgeCount int
	for _, e := range edges {
		em, _ := e.(map[string]any)
		from, _ := em["from_id"].(string)
		to, _ := em["to_id"].(string)
		if from == "frontend::fe1" && to == "backend::be1" {
			xrepoEdgeCount++
		}
	}
	if xrepoEdgeCount == 0 {
		t.Errorf("cross-repo edge frontend::fe1 → backend::be1 missing from /api/graph payload (edges=%d); fix #1388", len(edges))
	}

	// Verify the cross-repo edge connects nodes from different repos.
	nodes, _ := body["nodes"].([]interface{})
	nodeRepos := map[string]string{}
	for _, n := range nodes {
		nm, _ := n.(map[string]any)
		id, _ := nm["id"].(string)
		repo, _ := nm["repo"].(string)
		nodeRepos[id] = repo
	}
	for _, e := range edges {
		em, _ := e.(map[string]any)
		from, _ := em["from_id"].(string)
		to, _ := em["to_id"].(string)
		if from == "frontend::fe1" && to == "backend::be1" {
			if nodeRepos[from] == nodeRepos[to] {
				t.Errorf("cross-repo edge endpoints are in the same repo %q; expected different repos", nodeRepos[from])
			}
		}
	}
}

// TestGraph_CrossRepoEdges_ExcludedWhenRepoFiltered verifies that cross-repo
// edges are excluded from the response when one endpoint is filtered out by
// a repo filter. This prevents dangling edge references (#1388 filter guard).
func TestGraph_CrossRepoEdges_ExcludedWhenRepoFiltered(t *testing.T) {
	st := newFakeStore()
	st.groups["xrepofilter"] = GroupSummary{
		Name:       "xrepofilter",
		ConfigPath: "/tmp/xrepofilter.json",
		Repos:      []string{"frontend", "backend"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	frontendDoc := &graph.Document{
		Repo: "frontend",
		Entities: []graph.Entity{
			{ID: "fe1", Name: "FetchUsers", Kind: "SCOPE.Function", SourceFile: "src/api.ts", Language: "typescript"},
		},
	}
	backendDoc := &graph.Document{
		Repo: "backend",
		Entities: []graph.Entity{
			{ID: "be1", Name: "GET /api/users", Kind: "http_endpoint_definition", SourceFile: "routes.go", Language: "go"},
		},
	}

	grp := &DashGroup{
		Name: "xrepofilter",
		Repos: map[string]*DashRepo{
			"frontend": {Slug: "frontend", Path: "/tmp/frontend", Doc: frontendDoc},
			"backend":  {Slug: "backend", Path: "/tmp/backend", Doc: backendDoc},
		},
		Links: []CrossRepoLink{
			{Source: "frontend::fe1", Target: "backend::be1", Kind: "HTTP_FETCH"},
		},
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["xrepofilter"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	// Filter to only the frontend repo — backend node is absent, so the
	// cross-repo edge must not appear (would dangle to an unknown target).
	code, body := getJSON(t, ts.URL, "/api/graph/xrepofilter?filter_repo=frontend")
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}

	edges, _ := body["edges"].([]interface{})
	for _, e := range edges {
		em, _ := e.(map[string]any)
		from, _ := em["from_id"].(string)
		to, _ := em["to_id"].(string)
		if from == "frontend::fe1" && to == "backend::be1" {
			t.Errorf("cross-repo edge must be excluded when target repo (backend) is filtered out")
		}
	}
}

// TestGraph_Dense_TotalNodeCount verifies the dense response includes total_node_count
// (replacing the old lod_level field removed in #1023).
func TestGraph_Dense_TotalNodeCount(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	nodes, _ := body["nodes"].([]interface{})
	if len(nodes) != 6 {
		t.Fatalf("expected 6 nodes, got %d", len(nodes))
	}
	// total_node_count replaces the old total_nodes/lod_level fields (#1023).
	totalNodeCount, ok := body["total_node_count"].(float64)
	if !ok {
		t.Fatalf("missing or wrong type for total_node_count: %v", body["total_node_count"])
	}
	if int(totalNodeCount) != 6 {
		t.Fatalf("expected total_node_count=6, got %v", totalNodeCount)
	}
}

// TestGraph_CrossRepoLinks_WrappedFileFormat verifies that readCrossRepoLinks
// handles both the bare-array format and the wrapped {"version":N,"links":[...]}
// format written by the link pass (BUG 1 root cause: graphstate.go was only
// trying the bare-array unmarshal which fails silently on the wrapper format).
func TestGraph_CrossRepoLinks_WrappedFileFormat(t *testing.T) {
	// Bare-array format (legacy).
	bareJSON := `[{"source":"a::1","target":"b::2","kind":"CALLS"}]`
	links, err := readCrossRepoLinks([]byte(bareJSON))
	if err != nil {
		t.Fatalf("bare array: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("bare array: want 1 link, got %d", len(links))
	}
	if links[0].Source != "a::1" || links[0].Target != "b::2" || links[0].Kind != "CALLS" {
		t.Errorf("bare array: unexpected link %+v", links[0])
	}

	// Wrapped object format (written by the link pass, e.g. upvate-links.json).
	wrappedJSON := `{"version":1,"links":[{"source":"c::3","target":"d::4","relation":"calls"}]}`
	links, err = readCrossRepoLinks([]byte(wrappedJSON))
	if err != nil {
		t.Fatalf("wrapped: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("wrapped: want 1 link, got %d", len(links))
	}
	if links[0].Source != "c::3" || links[0].Target != "d::4" {
		t.Errorf("wrapped: unexpected link source/target %+v", links[0])
	}
	// "relation" field must be mapped to Kind.
	if links[0].Kind != "calls" {
		t.Errorf("wrapped: relation field not mapped to Kind; got %q, want %q", links[0].Kind, "calls")
	}
}

// TestGraph_CrossRepoLinks_LoadedFromFile verifies that serveGraphDense emits
// cross-repo edges when grp.Links is populated from a wrapped JSON file on
// disk (regression for BUG 1: daemon used to leave grp.Links empty because
// the file format didn't match the bare-array unmarshal).
func TestGraph_CrossRepoLinks_LoadedFromFile(t *testing.T) {
	// Write a wrapped-format links file to a temp dir.
	dir := t.TempDir()
	linksPath := filepath.Join(dir, "testfilelinks-links.json")
	linksJSON := `{"version":1,"links":[
		{"source":"frontend::fe1","target":"backend::be1","relation":"HTTP_FETCH","confidence":0.95}
	]}`
	if err := os.WriteFile(linksPath, []byte(linksJSON), 0o644); err != nil {
		t.Fatalf("write links file: %v", err)
	}

	// Parse via readCrossRepoLinks (simulates what loadGroup does).
	data, err := os.ReadFile(linksPath)
	if err != nil {
		t.Fatalf("read links file: %v", err)
	}
	links, err := readCrossRepoLinks(data)
	if err != nil {
		t.Fatalf("readCrossRepoLinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("want 1 link, got %d", len(links))
	}
	if links[0].Kind != "HTTP_FETCH" {
		t.Errorf("kind: got %q, want %q", links[0].Kind, "HTTP_FETCH")
	}

	// Now inject into a handler test and verify the edge appears in /api/graph.
	st := newFakeStore()
	st.groups["testfilelinks"] = GroupSummary{
		Name:       "testfilelinks",
		ConfigPath: "/tmp/testfilelinks.json",
		Repos:      []string{"frontend", "backend"},
	}
	cfg := DefaultConfig()
	srv, srvErr := NewServer(cfg, st)
	if srvErr != nil {
		t.Fatalf("NewServer: %v", srvErr)
	}

	frontendDoc := &graph.Document{
		Repo: "frontend",
		Entities: []graph.Entity{
			{ID: "fe1", Name: "FetchUsers", Kind: "SCOPE.Function", SourceFile: "src/api.ts", Language: "typescript"},
		},
	}
	backendDoc := &graph.Document{
		Repo: "backend",
		Entities: []graph.Entity{
			{ID: "be1", Name: "GET /api/users", Kind: "http_endpoint_definition", SourceFile: "routes.go", Language: "go"},
		},
	}

	grp := &DashGroup{
		Name: "testfilelinks",
		Repos: map[string]*DashRepo{
			"frontend": {Slug: "frontend", Path: "/tmp/frontend", Doc: frontendDoc},
			"backend":  {Slug: "backend", Path: "/tmp/backend", Doc: backendDoc},
		},
		Links: links, // loaded from wrapped JSON file
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["testfilelinks"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	code, body := getJSON(t, ts.URL, "/api/graph/testfilelinks")
	if code != 200 {
		t.Fatalf("status=%d body=%v", code, body)
	}

	edges, _ := body["edges"].([]interface{})
	var xrepoCount int
	for _, e := range edges {
		em, _ := e.(map[string]any)
		from, _ := em["from_id"].(string)
		to, _ := em["to_id"].(string)
		if from == "frontend::fe1" && to == "backend::be1" {
			xrepoCount++
		}
	}
	if xrepoCount == 0 {
		t.Errorf("cross-repo edge from wrapped links file missing from payload (total edges=%d); BUG 1 regression", len(edges))
	}
}

// TestGraph_ModuleNodes_ExcludedByDefault verifies that synthetic Module-kind
// nodes and their incident CONTAINS/DEPENDS_ON edges are excluded from the
// default GET /api/graph/{group} response.  They should only appear when
// ?view=modules is passed (BUG 2: module aggregation pollutes default view).
func TestGraph_ModuleNodes_ExcludedByDefault(t *testing.T) {
	st := newFakeStore()
	st.groups["modgrp"] = GroupSummary{
		Name:       "modgrp",
		ConfigPath: "/tmp/modgrp.json",
		Repos:      []string{"svc"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Inject a doc with a Module node and normal entity nodes.
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{ID: "fn1", Name: "HandleLogin", Kind: "SCOPE.Function", SourceFile: "auth.go", Language: "go"},
			{ID: "fn2", Name: "ValidateToken", Kind: "SCOPE.Function", SourceFile: "auth.go", Language: "go"},
			{ID: "mod1", Name: "auth", Kind: "Module", SourceFile: "", Language: ""},
		},
		Relationships: []graph.Relationship{
			// Normal entity-to-entity edge — must always appear.
			{FromID: "fn1", ToID: "fn2", Kind: "CALLS"},
			// Module CONTAINS edge — must be excluded in default view.
			{FromID: "mod1", ToID: "fn1", Kind: "CONTAINS"},
			{FromID: "mod1", ToID: "fn2", Kind: "CONTAINS"},
		},
	}

	grp := &DashGroup{
		Name:  "modgrp",
		Repos: map[string]*DashRepo{"svc": {Slug: "svc", Path: "/tmp/svc", Doc: doc}},
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["modgrp"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	// Default view: 0 Module nodes, 0 CONTAINS edges from Module.
	code, body := getJSON(t, ts.URL, "/api/graph/modgrp")
	if code != 200 {
		t.Fatalf("default: status=%d body=%v", code, body)
	}
	nodes, _ := body["nodes"].([]interface{})
	edges, _ := body["edges"].([]interface{})

	for _, n := range nodes {
		nm, _ := n.(map[string]any)
		kind, _ := nm["kind"].(string)
		if kind == "Module" {
			t.Errorf("default view: Module-kind node %v must be excluded", nm["id"])
		}
	}
	var containsCount int
	for _, e := range edges {
		em, _ := e.(map[string]any)
		if em["kind"] == "CONTAINS" {
			containsCount++
		}
	}
	if containsCount > 0 {
		t.Errorf("default view: got %d CONTAINS edges from Module node; want 0", containsCount)
	}
	// Normal CALLS edge must still be present.
	var callsCount int
	for _, e := range edges {
		em, _ := e.(map[string]any)
		if em["kind"] == "CALLS" {
			callsCount++
		}
	}
	if callsCount == 0 {
		t.Errorf("default view: normal CALLS edge must not be excluded (total edges=%d)", len(edges))
	}
	_ = nodes

	// ?view=modules: Module node appears, CONTAINS edges appear.
	code2, body2 := getJSON(t, ts.URL, "/api/graph/modgrp?view=modules")
	if code2 != 200 {
		t.Fatalf("view=modules: status=%d body=%v", code2, body2)
	}
	nodes2, _ := body2["nodes"].([]interface{})
	var moduleNodeCount int
	for _, n := range nodes2 {
		nm, _ := n.(map[string]any)
		if nm["kind"] == "Module" {
			moduleNodeCount++
		}
	}
	if moduleNodeCount == 0 {
		t.Errorf("view=modules: Module node must be present in opt-in response")
	}
}
