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
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
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
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup?lod=full")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	nodes, _ := body["nodes"].([]interface{})
	// Should include all non-blocked entities (doc has 6 entities).
	if len(nodes) != 6 {
		t.Fatalf("expected 6 nodes, got %d", len(nodes))
	}
	if body["lod_level"] != "full" {
		t.Fatalf("wrong lod_level: %v", body["lod_level"])
	}
}

func TestGraph_Centroids_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup?lod=centroids")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	nodes, _ := body["nodes"].([]interface{})
	// Should return one centroid per community (2 communities).
	if len(nodes) != 2 {
		t.Fatalf("expected 2 centroids, got %d", len(nodes))
	}
	if body["lod_level"] != "centroids" {
		t.Fatalf("wrong lod_level: %v", body["lod_level"])
	}
}

func TestGraph_Mid_200(t *testing.T) {
	ts, _ := newPhase1Server(t)
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup?lod=mid")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	if body["lod_level"] != "mid" {
		t.Fatalf("wrong lod_level: %v", body["lod_level"])
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
	code, body := getJSON(t, ts.URL, "/api/graph/testgroup?lod=full&filter_kind=Function")
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
