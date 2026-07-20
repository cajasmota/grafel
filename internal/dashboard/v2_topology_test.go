package dashboard

// v2_topology_test.go — smoke tests for the WebUI v2 topology endpoints.
//
// GET /api/v2/topology/{group}            → v2 envelope, correct shape
// GET /api/v2/topology/{group}/topic/{id} → v2 envelope, detail shape
// 404 on unknown group and unknown topic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// newV2TopologyTestServer creates a test httptest.Server pre-loaded with a
// single group ("topo-grp") containing the supplied DashGroup.
func newV2TopologyTestServer(t *testing.T, grp *DashGroup) *httptest.Server {
	t.Helper()
	st := newFakeStore()
	st.groups["topo-grp"] = GroupSummary{
		Name:       "topo-grp",
		ConfigPath: "/tmp/topo-grp.json",
		Repos:      []string{"svc-a"},
	}
	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["topo-grp"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts
}

// minTopologyGroup returns a minimal DashGroup with two topics (one active,
// one orphan-publisher) and one serverless function.
func minTopologyGroup() *DashGroup {
	entities := []graph.Entity{
		graph.Entity{
			ID:   "topic-orders",
			Name: "orders.created",
			Kind: "MessageTopic",
		}.WithProperties(map[string]string{
			"broker": "kafka",
		},
		),
		graph.Entity{
			ID:   "topic-dead",
			Name: "dead.letter",
			Kind: "MessageTopic",
		}.WithProperties(map[string]string{
			"broker": "kafka",
		},
		),
		{
			ID:   "fn-producer",
			Name: "OrderService.createOrder",
			Kind: "Function",
		},
		{
			ID:   "fn-consumer",
			Name: "NotificationService.onOrder",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		// fn-producer → orders.created (producer edge)
		{ID: "r1", FromID: "fn-producer", ToID: "topic-orders", Kind: "PUBLISHES_TO"},
		// orders.created → fn-consumer (consumer edge)
		{ID: "r2", FromID: "topic-orders", ToID: "fn-consumer", Kind: "SUBSCRIBES_TO"},
		// fn-producer → dead.letter (orphan publisher — no consumer)
		{ID: "r3", FromID: "fn-producer", ToID: "topic-dead", Kind: "PUBLISHES_TO"},
	}
	return makeTopologyGroupFromEntities(entities, rels)
}

// makeTopologyGroupFromEntities builds a DashGroup with a single "svc-a" repo.
func makeTopologyGroupFromEntities(entities []graph.Entity, rels []graph.Relationship) *DashGroup {
	doc := &graph.Document{
		Entities:      entities,
		Relationships: rels,
	}
	return &DashGroup{
		Name: "topo-grp",
		Repos: map[string]*DashRepo{
			"svc-a": {Slug: "svc-a", Doc: doc},
		},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestV2Topology_Shape verifies the full topology endpoint returns a valid
// v2 envelope with non-null array fields and at least the topics from the
// fixture.
func TestV2Topology_Shape(t *testing.T) {
	grp := minTopologyGroup()
	ts := newV2TopologyTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/v2/topology/topo-grp")
	if err != nil {
		t.Fatalf("GET /api/v2/topology/topo-grp: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: want application/json, got %q", ct)
	}

	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			Topics               []map[string]any `json:"topics"`
			Queues               []map[string]any `json:"queues"`
			Channels             []map[string]any `json:"channels"`
			NatsSubjects         []map[string]any `json:"nats_subjects"`
			GraphQLSubscriptions []map[string]any `json:"graphql_subscriptions"`
			Functions            []map[string]any `json:"functions"`
			Transforms           []map[string]any `json:"transforms"`
			BrokerGroups         []map[string]any `json:"broker_groups"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Error("ok field: want true")
	}
	// All arrays must be non-nil (the v1 invariant carries through).
	if body.Data.Topics == nil {
		t.Error("topics: want [], got null")
	}
	if body.Data.Queues == nil {
		t.Error("queues: want [], got null")
	}
	if body.Data.Channels == nil {
		t.Error("channels: want [], got null")
	}
	if body.Data.NatsSubjects == nil {
		t.Error("nats_subjects: want [], got null")
	}
	if body.Data.GraphQLSubscriptions == nil {
		t.Error("graphql_subscriptions: want [], got null")
	}
	if body.Data.Functions == nil {
		t.Error("functions: want [], got null")
	}
	if body.Data.Transforms == nil {
		t.Error("transforms: want [], got null")
	}
	// Fixture has 2 kafka topics.
	if len(body.Data.Topics) != 2 {
		t.Errorf("topics: want 2, got %d", len(body.Data.Topics))
	}
	// Fixture has 1 broker group (kafka).
	if len(body.Data.BrokerGroups) != 1 {
		t.Errorf("broker_groups: want 1, got %d", len(body.Data.BrokerGroups))
	}
	bg := body.Data.BrokerGroups[0]
	if bg["broker"] != "kafka" {
		t.Errorf("broker_groups[0].broker: want kafka, got %v", bg["broker"])
	}
}

// TestV2Topology_NotFound verifies a 404 v2 error for an unknown group.
func TestV2Topology_NotFound(t *testing.T) {
	grp := minTopologyGroup()
	ts := newV2TopologyTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/v2/topology/no-such-group")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", resp.StatusCode)
	}

	var body struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.OK {
		t.Error("ok field: want false")
	}
	if body.Error.Code != "not_found" {
		t.Errorf("error.code: want not_found, got %q", body.Error.Code)
	}
}

// TestV2TopologyDetail_Shape verifies the detail endpoint wraps a single
// topic in the v2 envelope with the expected fields.
func TestV2TopologyDetail_Shape(t *testing.T) {
	grp := minTopologyGroup()
	ts := newV2TopologyTestServer(t, grp)

	// The topic ID is "<repo>::<localId>" — our fixture uses "svc-a::topic-orders".
	topicID := "svc-a%3A%3Atopic-orders"
	resp, err := http.Get(ts.URL + "/api/v2/topology/topo-grp/topic/" + topicID)
	if err != nil {
		t.Fatalf("GET topic detail: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			ID             string `json:"id"`
			Label          string `json:"label"`
			LifecycleState string `json:"lifecycle_state"`
			CrossRepo      bool   `json:"cross_repo"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Error("ok: want true")
	}
	if body.Data.Label != "orders.created" {
		t.Errorf("label: want orders.created, got %q", body.Data.Label)
	}
	// orders.created has both producer and consumer in the fixture → active.
	if body.Data.LifecycleState != "active" {
		t.Errorf("lifecycle_state: want active, got %q", body.Data.LifecycleState)
	}
}

// TestV2TopologyDetail_NotFound verifies a 404 v2 error for an unknown topic.
func TestV2TopologyDetail_NotFound(t *testing.T) {
	grp := minTopologyGroup()
	ts := newV2TopologyTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/v2/topology/topo-grp/topic/no-such-topic")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", resp.StatusCode)
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
	if body.Error.Code != "not_found" {
		t.Errorf("error.code: want not_found, got %q", body.Error.Code)
	}
}

// TestV2Topology_V1RouteUnchanged confirms the v1 topology route still
// returns a non-v2 JSON response (the old format without ok/data envelope).
func TestV2Topology_V1RouteUnchanged(t *testing.T) {
	grp := minTopologyGroup()
	ts := newV2TopologyTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/topology/topo-grp")
	if err != nil {
		t.Fatalf("GET v1 topology: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("v1 topology status: want 200, got %d", resp.StatusCode)
	}

	// v1 response has a "topics" top-level key, not "ok"/"data".
	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, hasOK := body["ok"]; hasOK {
		t.Error("v1 route should NOT have ok field (would mean v2 envelope leaked)")
	}
	if _, hasTopics := body["topics"]; !hasTopics {
		t.Error("v1 route should have top-level topics field")
	}
}
