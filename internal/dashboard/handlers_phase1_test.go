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
// /api/flows/{group}
// ---------------------------------------------------------------------------

func TestFlowsList_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/flows/testgroup")
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
	code, body := getJSON(t, ts.URL, "/api/flows/testgroup?cross_stack_only=true")
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
