package dashboard

// handlers_topology_detail_test.go — unit tests for the per-topic detail
// endpoint (GET /api/topology/{group}/topic/{topicId}).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// testServer returns a *Server wired against an in-memory fake store. The
// caller must populate srv.graphs via the returned *GraphCache.
func testServerForDetail(t *testing.T) (*Server, *GraphCache) {
	t.Helper()
	cache := NewGraphCache(60 * time.Second)
	srv := &Server{
		graphs: cache,
	}
	return srv, cache
}

func injectGroup(cache *GraphCache, groupName string, grp *DashGroup) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.entries[groupName] = &cacheEntry{group: grp, loadedAt: time.Now()}
}

// ---------------------------------------------------------------------------
// TestTopicDetail_TwoProducersOneConsumer — fixture: 2 producers + 1 consumer
// ---------------------------------------------------------------------------

func TestTopicDetail_TwoProducersOneConsumer(t *testing.T) {
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{
				ID:         "topic:orders",
				Name:       "orders",
				Kind:       "MessageTopic",
				SourceFile: "kafka/topics.go",
				StartLine:  10,
				Properties: map[string]string{"broker": "kafka", "schema": "OrderCreated{id,amount}"},
			},
			{
				ID:         "fn:api",
				Name:       "ApiHandler",
				Kind:       "SCOPE.Function",
				SourceFile: "api/handler.go",
				StartLine:  42,
			},
			{
				ID:         "fn:checkout",
				Name:       "CheckoutService",
				Kind:       "SCOPE.Function",
				SourceFile: "checkout/service.go",
				StartLine:  7,
			},
			{
				ID:         "fn:warehouse",
				Name:       "WarehouseConsumer",
				Kind:       "SCOPE.Function",
				SourceFile: "warehouse/consumer.go",
				StartLine:  15,
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "fn:api", ToID: "topic:orders", Kind: "PUBLISHES_TO"},
			{ID: "r2", FromID: "fn:checkout", ToID: "topic:orders", Kind: "PUBLISHES_TO"},
			{ID: "r3", FromID: "fn:warehouse", ToID: "topic:orders", Kind: "SUBSCRIBES_TO"},
		},
	}
	grp := &DashGroup{
		Name:  "testgroup",
		Repos: map[string]*DashRepo{"svc": {Slug: "svc", Doc: doc}},
	}

	srv, cache := testServerForDetail(t)
	injectGroup(cache, "testgroup", grp)

	req := httptest.NewRequest(http.MethodGet, "/api/topology/testgroup/topic/svc::topic:orders", nil)
	req.SetPathValue("group", "testgroup")
	req.SetPathValue("topicId", "svc::topic:orders")
	rw := httptest.NewRecorder()
	srv.handleTopicDetail(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rw.Code, rw.Body.String())
	}

	var resp topicDetailResponse
	if err := json.NewDecoder(rw.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Basic identity.
	if resp.ID != "svc::topic:orders" {
		t.Errorf("ID = %q, want svc::topic:orders", resp.ID)
	}
	if resp.Label != "orders" {
		t.Errorf("Label = %q, want orders", resp.Label)
	}
	if resp.Broker != "kafka" {
		t.Errorf("Broker = %q, want kafka", resp.Broker)
	}
	if resp.MessageSchema != "OrderCreated{id,amount}" {
		t.Errorf("MessageSchema = %q, want OrderCreated{id,amount}", resp.MessageSchema)
	}
	if resp.Repo != "svc" {
		t.Errorf("Repo = %q, want svc", resp.Repo)
	}
	if resp.SourceFile != "kafka/topics.go" {
		t.Errorf("SourceFile = %q, want kafka/topics.go", resp.SourceFile)
	}
	if resp.StartLine != 10 {
		t.Errorf("StartLine = %d, want 10", resp.StartLine)
	}

	// Producers: 2.
	if len(resp.Producers) != 2 {
		t.Fatalf("Producers len = %d, want 2", len(resp.Producers))
	}
	// Each producer must have source_file populated.
	for _, p := range resp.Producers {
		if p.SourceFile == "" {
			t.Errorf("producer %q missing source_file", p.Name)
		}
	}

	// Consumers: 1.
	if len(resp.Consumers) != 1 {
		t.Fatalf("Consumers len = %d, want 1", len(resp.Consumers))
	}
	if resp.Consumers[0].Name != "WarehouseConsumer" {
		t.Errorf("consumer name = %q, want WarehouseConsumer", resp.Consumers[0].Name)
	}
	if resp.Consumers[0].SourceFile != "warehouse/consumer.go" {
		t.Errorf("consumer source_file = %q, want warehouse/consumer.go", resp.Consumers[0].SourceFile)
	}

	// Lifecycle: active (has both).
	if resp.LifecycleState != "active" {
		t.Errorf("LifecycleState = %q, want active", resp.LifecycleState)
	}

	// Beyond-minimum fields must be present.
	if resp.UsageHistory == nil {
		t.Error("UsageHistory must not be nil")
	}
	// CrossRepo false: all entities in same repo.
	if resp.CrossRepo {
		t.Error("CrossRepo should be false — all entities in same repo")
	}

	// Tests array must be non-nil even when empty.
	if resp.Tests == nil {
		t.Error("Tests must not be nil")
	}
}

// ---------------------------------------------------------------------------
// TestTopicDetail_CeleryScheduledJob — framework=celery + schedule field
// ---------------------------------------------------------------------------

func TestTopicDetail_CeleryScheduledJob(t *testing.T) {
	doc := &graph.Document{
		Repo: "worker",
		Entities: []graph.Entity{
			{
				ID:         "celery_beat:nightly",
				Name:       "nightly_cleanup",
				Kind:       "SCOPE.ScheduledJob",
				SourceFile: "worker/beat.py",
				StartLine:  5,
				Properties: map[string]string{
					"framework": "celery_beat",
					"schedule":  "*/5 * * * *",
				},
			},
		},
	}
	grp := &DashGroup{
		Name:  "grp2",
		Repos: map[string]*DashRepo{"worker": {Slug: "worker", Doc: doc}},
	}

	srv, cache := testServerForDetail(t)
	injectGroup(cache, "grp2", grp)

	req := httptest.NewRequest(http.MethodGet, "/api/topology/grp2/topic/worker::celery_beat:nightly", nil)
	req.SetPathValue("group", "grp2")
	req.SetPathValue("topicId", "worker::celery_beat:nightly")
	rw := httptest.NewRecorder()
	srv.handleTopicDetail(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rw.Code, rw.Body.String())
	}

	var resp topicDetailResponse
	if err := json.NewDecoder(rw.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Schedule fields.
	if !resp.Scheduled {
		t.Errorf("Scheduled = false, want true for ScheduledJob")
	}
	if resp.Schedule != "*/5 * * * *" {
		t.Errorf("Schedule = %q, want */5 * * * *", resp.Schedule)
	}
	if resp.Framework != "celery_beat" {
		t.Errorf("Framework = %q, want celery_beat", resp.Framework)
	}

	// Lifecycle: orphan (no producers/consumers).
	if resp.LifecycleState != "orphan" {
		t.Errorf("LifecycleState = %q, want orphan", resp.LifecycleState)
	}
}

// ---------------------------------------------------------------------------
// TestTopicDetail_UnknownTopic — 404 for unknown topicId
// ---------------------------------------------------------------------------

func TestTopicDetail_UnknownTopic(t *testing.T) {
	doc := &graph.Document{
		Repo:     "svc",
		Entities: []graph.Entity{},
	}
	grp := &DashGroup{
		Name:  "grp3",
		Repos: map[string]*DashRepo{"svc": {Slug: "svc", Doc: doc}},
	}

	srv, cache := testServerForDetail(t)
	injectGroup(cache, "grp3", grp)

	req := httptest.NewRequest(http.MethodGet, "/api/topology/grp3/topic/svc::does-not-exist", nil)
	req.SetPathValue("group", "grp3")
	req.SetPathValue("topicId", "svc::does-not-exist")
	rw := httptest.NewRecorder()
	srv.handleTopicDetail(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rw.Code)
	}
}

// ---------------------------------------------------------------------------
// TestTopicDetail_UnknownGroup — 404 for unknown group
// ---------------------------------------------------------------------------

func TestTopicDetail_UnknownGroup(t *testing.T) {
	srv, _ := testServerForDetail(t)

	req := httptest.NewRequest(http.MethodGet, "/api/topology/nogroup/topic/svc::x", nil)
	req.SetPathValue("group", "nogroup")
	req.SetPathValue("topicId", "svc::x")
	rw := httptest.NewRecorder()
	srv.handleTopicDetail(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown group, got %d", rw.Code)
	}
}

// ---------------------------------------------------------------------------
// TestTopicDetail_LifecycleStates — orphan_publisher / orphan_subscriber
// ---------------------------------------------------------------------------

func TestTopicDetail_LifecycleStates(t *testing.T) {
	cases := []struct {
		name           string
		relationships  []graph.Relationship
		wantLifecycle  string
	}{
		{
			name: "orphan_publisher",
			relationships: []graph.Relationship{
				{ID: "r1", FromID: "fn:producer", ToID: "topic:t", Kind: "PUBLISHES_TO"},
			},
			wantLifecycle: "orphan_publisher",
		},
		{
			name: "orphan_subscriber",
			relationships: []graph.Relationship{
				{ID: "r1", FromID: "fn:consumer", ToID: "topic:t", Kind: "SUBSCRIBES_TO"},
			},
			wantLifecycle: "orphan_subscriber",
		},
		{
			name:          "full_orphan",
			relationships: []graph.Relationship{},
			wantLifecycle: "orphan",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &graph.Document{
				Repo: "svc",
				Entities: []graph.Entity{
					{
						ID:         "topic:t",
						Name:       "test-topic",
						Kind:       "MessageTopic",
						Properties: map[string]string{"broker": "kafka"},
					},
				},
				Relationships: tc.relationships,
			}
			grp := &DashGroup{
				Name:  tc.name,
				Repos: map[string]*DashRepo{"svc": {Slug: "svc", Doc: doc}},
			}
			srv, cache := testServerForDetail(t)
			injectGroup(cache, tc.name, grp)

			req := httptest.NewRequest(http.MethodGet, "/api/topology/"+tc.name+"/topic/svc::topic:t", nil)
			req.SetPathValue("group", tc.name)
			req.SetPathValue("topicId", "svc::topic:t")
			rw := httptest.NewRecorder()
			srv.handleTopicDetail(rw, req)

			if rw.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rw.Code)
			}
			var resp topicDetailResponse
			if err := json.NewDecoder(rw.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.LifecycleState != tc.wantLifecycle {
				t.Errorf("lifecycle = %q, want %q", resp.LifecycleState, tc.wantLifecycle)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestTopicDetail_CrossRepo — cross_repo=true when entities in different repos
// ---------------------------------------------------------------------------

func TestTopicDetail_CrossRepo(t *testing.T) {
	docA := &graph.Document{
		Repo: "svc-a",
		Entities: []graph.Entity{
			{
				ID:         "topic:payments",
				Name:       "payments",
				Kind:       "MessageTopic",
				SourceFile: "events/topics.go",
				Properties: map[string]string{"broker": "kafka"},
			},
			{
				ID:         "fn:publisher",
				Name:       "PaymentPublisher",
				Kind:       "SCOPE.Function",
				SourceFile: "payments/publisher.go",
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "fn:publisher", ToID: "topic:payments", Kind: "PUBLISHES_TO"},
		},
	}
	docB := &graph.Document{
		Repo: "svc-b",
		Entities: []graph.Entity{
			{
				ID:         "fn:consumer",
				Name:       "PaymentConsumer",
				Kind:       "SCOPE.Function",
				SourceFile: "billing/consumer.go",
			},
		},
		Relationships: []graph.Relationship{
			// Consumer in svc-b subscribes to the topic in svc-a (local ID resolves across repos).
			{ID: "r2", FromID: "fn:consumer", ToID: "topic:payments", Kind: "SUBSCRIBES_TO"},
		},
	}
	grp := &DashGroup{
		Name: "cross",
		Repos: map[string]*DashRepo{
			"svc-a": {Slug: "svc-a", Doc: docA},
			"svc-b": {Slug: "svc-b", Doc: docB},
		},
	}

	srv, cache := testServerForDetail(t)
	injectGroup(cache, "cross", grp)

	req := httptest.NewRequest(http.MethodGet, "/api/topology/cross/topic/svc-a::topic:payments", nil)
	req.SetPathValue("group", "cross")
	req.SetPathValue("topicId", "svc-a::topic:payments")
	rw := httptest.NewRecorder()
	srv.handleTopicDetail(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rw.Code, rw.Body.String())
	}

	var resp topicDetailResponse
	if err := json.NewDecoder(rw.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !resp.CrossRepo {
		t.Error("CrossRepo should be true when consumer is in different repo")
	}
}

// ---------------------------------------------------------------------------
// TestTopicDetail_ArrayFieldsNeverNull — wire contract: [] not null
// ---------------------------------------------------------------------------

func TestTopicDetail_ArrayFieldsNeverNull(t *testing.T) {
	// Topic with no edges — every array field must marshal as [].
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{
				ID:         "topic:empty",
				Name:       "empty-topic",
				Kind:       "MessageTopic",
				Properties: map[string]string{"broker": "kafka"},
			},
		},
	}
	grp := &DashGroup{
		Name:  "nullcheck",
		Repos: map[string]*DashRepo{"svc": {Slug: "svc", Doc: doc}},
	}

	srv, cache := testServerForDetail(t)
	injectGroup(cache, "nullcheck", grp)

	req := httptest.NewRequest(http.MethodGet, "/api/topology/nullcheck/topic/svc::topic:empty", nil)
	req.SetPathValue("group", "nullcheck")
	req.SetPathValue("topicId", "svc::topic:empty")
	rw := httptest.NewRecorder()
	srv.handleTopicDetail(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}

	// Decode into raw JSON to verify [] not null.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(rw.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}

	arrayFields := []string{"producers", "consumers", "tests", "related_topics", "usage_history"}
	for _, field := range arrayFields {
		v, ok := raw[field]
		if !ok {
			t.Errorf("field %q missing from response", field)
			continue
		}
		if string(v) == "null" {
			t.Errorf("field %q is null, want []", field)
		}
	}
}
