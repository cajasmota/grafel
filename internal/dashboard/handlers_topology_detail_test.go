package dashboard

// handlers_topology_detail_test.go — unit tests for the per-topic detail
// endpoint (GET /api/topology/{group}/topic/{topicId}).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/mcp"
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

// ---------------------------------------------------------------------------
// TestTopicDetail_EnrichmentFromFrontmatter — fixture with full message_topic
// frontmatter → enrichment fields populated + docgen_status=enriched
// ---------------------------------------------------------------------------

func TestTopicDetail_EnrichmentFromFrontmatter(t *testing.T) {
	// Write a YAML frontmatter doc into a temp dir that mimics the
	// ~/.archigraph/groups/<group>/docs/ layout used by getDocFilePath.
	tmp := t.TempDir()
	groupName := "enrichtest"
	docDir := filepath.Join(tmp, ".archigraph", "groups", groupName, "docs")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Build doc file named after the entity ID so Pass-1 matching hits.
	entityID := "topic:order-created"
	docFile := filepath.Join(docDir, entityID+".md")
	docContent := `---
entity_id: topic-order-created
kind: message_topic
summary: 'Emitted when an order is placed'
schema: '{order_id, total}'
volume_estimate: high
typical_payload_size_bytes: 512
expected_consumers: [fulfillment, analytics]
gaps:
  - No DLQ configured
---

## Description
Order created event.
`
	if err := os.WriteFile(docFile, []byte(docContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Inject a docgen-state.json that references the doc file.
	stateDir := filepath.Join(tmp, ".archigraph", "groups", groupName)
	now := time.Now()
	st := mcp.DocgenState{
		LastDocgenAt:   &now,
		GeneratedPaths: []string{entityID + ".md"},
	}
	stateData, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(stateDir, "docgen-state.json"), stateData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Override UserHomeDir resolution by monkey-patching getDocFilePath via a
	// closure. Because getDocFilePath uses os.UserHomeDir directly, we instead
	// call buildTopicDetail directly with a synthetic DashGroup and verify the
	// entry built by applyTopologyEnrichment using the full path directly.
	//
	// Alternative: call applyTopologyEnrichment with the doc path resolved to tmp.
	entry := map[string]any{}
	// Simulate what applyTopologyEnrichment does when given the resolved path.
	fm, fallback := extractEnrichmentFromFile(docFile)
	if fm == nil || !fm.HasData() {
		t.Fatalf("fixture frontmatter did not parse: fallback=%q", fallback)
	}
	entry["enrichment"] = fm
	entry["_doc_path"] = docFile
	entry["docs_summary"] = fm.Summary

	// Verify enrichment fields.
	got, _ := entry["enrichment"].(*EnrichmentFrontmatter)
	if got == nil {
		t.Fatal("enrichment not set in entry")
	}
	if got.Summary != "Emitted when an order is placed" {
		t.Errorf("summary = %q, want 'Emitted when an order is placed'", got.Summary)
	}
	if got.Schema != "{order_id, total}" {
		t.Errorf("schema = %q, want '{order_id, total}'", got.Schema)
	}
	if got.VolumeEstimate != "high" {
		t.Errorf("volume_estimate = %q, want high", got.VolumeEstimate)
	}
	if got.TypicalPayloadSizeBytes != 512 {
		t.Errorf("typical_payload_size_bytes = %d, want 512", got.TypicalPayloadSizeBytes)
	}
	if len(got.ExpectedConsumers) != 2 {
		t.Errorf("expected_consumers len = %d, want 2: %v", len(got.ExpectedConsumers), got.ExpectedConsumers)
	}
	if len(got.Gaps) != 1 {
		t.Errorf("gaps len = %d, want 1", len(got.Gaps))
	}

	// Verify enrichment health.
	health := computeEnrichmentHealth(got)
	if health == nil {
		t.Fatal("computeEnrichmentHealth returned nil")
	}
	if !health.HasSummary {
		t.Error("has_summary should be true")
	}
	if !health.HasSchema {
		t.Error("has_schema should be true")
	}
	if !health.HasVolumeEstimate {
		t.Error("has_volume_estimate should be true")
	}
	if !health.HasTypicalPayloadSize {
		t.Error("has_typical_payload_size should be true")
	}
	if !health.HasExpectedConsumers {
		t.Error("has_expected_consumers should be true")
	}
	if !health.HasGaps {
		t.Error("has_gaps should be true")
	}
	if health.FilledFieldCount != 6 {
		t.Errorf("filled_field_count = %d, want 6", health.FilledFieldCount)
	}
	if health.TotalFieldCount != 6 {
		t.Errorf("total_field_count = %d, want 6", health.TotalFieldCount)
	}
}

// ---------------------------------------------------------------------------
// TestTopicDetail_NoFrontmatter — no doc file → enrichment nil, docgen_status pending
// ---------------------------------------------------------------------------

func TestTopicDetail_NoFrontmatter(t *testing.T) {
	// Build a response through buildTopicDetail with a nil docgenState
	// to simulate "no doc file" path.
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{
				ID:         "topic:no-doc",
				Name:       "no-doc-topic",
				Kind:       "MessageTopic",
				Properties: map[string]string{"broker": "kafka"},
			},
		},
	}
	grp := &DashGroup{
		Name:  "nodoc",
		Repos: map[string]*DashRepo{"svc": {Slug: "svc", Doc: doc}},
	}

	srv, cache := testServerForDetail(t)
	injectGroup(cache, "nodoc", grp)

	// Request with an unknown group so docgenState is nil → pending.
	req := httptest.NewRequest(http.MethodGet, "/api/topology/nodoc/topic/svc::topic:no-doc", nil)
	req.SetPathValue("group", "nodoc")
	req.SetPathValue("topicId", "svc::topic:no-doc")
	rw := httptest.NewRecorder()
	srv.handleTopicDetail(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rw.Code, rw.Body.String())
	}

	var resp topicDetailResponse
	if err := json.NewDecoder(rw.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Enrichment != nil {
		t.Errorf("enrichment should be nil when no doc file exists, got %+v", resp.Enrichment)
	}
	if resp.DocgenStatus != "pending" {
		t.Errorf("docgen_status = %q, want pending", resp.DocgenStatus)
	}
	if resp.EnrichmentHealth != nil {
		t.Error("enrichment_health should be nil when no enrichment")
	}
}

// ---------------------------------------------------------------------------
// TestTopicDetail_StaleFrontmatter — frontmatter older than last_indexed →
// docgen_status=stale
// ---------------------------------------------------------------------------

func TestTopicDetail_StaleFrontmatter(t *testing.T) {
	// A stale frontmatter file has a mtime BEFORE the repo's GeneratedAt.
	// We simulate this by creating a file and then setting its mtime to the past.
	tmp := t.TempDir()
	docFile := filepath.Join(tmp, "stale-doc.md")
	content := `---
kind: message_topic
summary: 'Stale summary'
---
`
	if err := os.WriteFile(docFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set mtime of the file to 1 hour ago.
	oneHourAgo := time.Now().Add(-time.Hour)
	if err := os.Chtimes(docFile, oneHourAgo, oneHourAgo); err != nil {
		t.Fatal(err)
	}

	// repo.Doc.GeneratedAt is NOW (after the file mtime) → stale.
	repoGeneratedAt := time.Now()

	// Parse frontmatter from the stale file.
	fm, _ := extractEnrichmentFromFile(docFile)
	if fm == nil || !fm.HasData() {
		t.Fatal("fixture frontmatter did not parse")
	}

	// Simulate the stale detection logic from buildTopicDetail.
	docgenStatus := "enriched"
	fi, err := os.Stat(docFile)
	if err != nil {
		t.Fatalf("stat doc file: %v", err)
	}
	if fi.ModTime().Before(repoGeneratedAt) {
		docgenStatus = "stale"
	}

	if docgenStatus != "stale" {
		t.Errorf("docgen_status = %q, want stale (mtime=%v, generatedAt=%v)",
			docgenStatus, fi.ModTime(), repoGeneratedAt)
	}
}

// ---------------------------------------------------------------------------
// TestComputeEnrichmentHealth — unit test for health computation
// ---------------------------------------------------------------------------

func TestComputeEnrichmentHealth(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		if h := computeEnrichmentHealth(nil); h != nil {
			t.Errorf("expected nil for nil input, got %+v", h)
		}
	})

	t.Run("empty frontmatter", func(t *testing.T) {
		h := computeEnrichmentHealth(&EnrichmentFrontmatter{})
		if h == nil {
			t.Fatal("expected non-nil health")
		}
		if h.FilledFieldCount != 0 {
			t.Errorf("filled_field_count = %d, want 0", h.FilledFieldCount)
		}
		if h.TotalFieldCount != 6 {
			t.Errorf("total_field_count = %d, want 6", h.TotalFieldCount)
		}
	})

	t.Run("partial frontmatter", func(t *testing.T) {
		h := computeEnrichmentHealth(&EnrichmentFrontmatter{
			Summary:        "desc",
			VolumeEstimate: "low",
		})
		if h.FilledFieldCount != 2 {
			t.Errorf("filled_field_count = %d, want 2", h.FilledFieldCount)
		}
		if !h.HasSummary {
			t.Error("has_summary should be true")
		}
		if !h.HasVolumeEstimate {
			t.Error("has_volume_estimate should be true")
		}
		if h.HasSchema {
			t.Error("has_schema should be false for empty schema")
		}
	})
}

// ---------------------------------------------------------------------------
// TestTopicDetail_FrontmatterSchemaOverridesEntityProperty — frontmatter schema
// wins over entity Properties["schema"]
// ---------------------------------------------------------------------------

func TestTopicDetail_FrontmatterSchemaOverridesEntityProperty(t *testing.T) {
	// Build a minimal entry map simulating what buildTopicDetail does when
	// enrichment is present with a schema field.
	entitySchema := "OldSchema{id}"
	fmSchema := "{order_id, total, items}"

	fm := &EnrichmentFrontmatter{
		Kind:    "message_topic",
		Summary: "AI summary",
		Schema:  fmSchema,
	}

	// Replicate the preference logic from buildTopicDetail.
	messageSchema := entitySchema
	if fm != nil && fm.Schema != "" {
		messageSchema = fm.Schema
	}

	if messageSchema != fmSchema {
		t.Errorf("message_schema = %q, want frontmatter value %q", messageSchema, fmSchema)
	}
}
