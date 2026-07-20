package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helper: build a minimal DashGroup for topology orphan tests.
// ─────────────────────────────────────────────────────────────────────────────

func makeTopologyOrphanGroup(entities []graph.Entity, rels []graph.Relationship) *DashGroup {
	doc := &graph.Document{
		Repo:          "backend",
		Entities:      entities,
		Relationships: rels,
	}
	return &DashGroup{
		Name: "testgrp",
		Repos: map[string]*DashRepo{
			"backend": {Slug: "backend", Path: "/tmp/fake-backend", Doc: doc},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests for collectOrphanPublishers
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanPublishers_ProducerOnly — producer publishes to topic-X but no
// consumer exists → 1 orphan row.
func TestOrphanPublishers_ProducerOnly(t *testing.T) {
	entities := []graph.Entity{
		graph.Entity{
			ID:   "topic-x",
			Name: "topic-X",
			Kind: "MessageTopic",
		}.WithProperties(map[string]string{"broker": "rabbitmq"}),
		{
			ID:   "producer-svc",
			Name: "OrderPublisher",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		{
			ID:     "r1",
			FromID: "producer-svc",
			ToID:   "topic-x",
			Kind:   "PUBLISHES_TO",
		},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	rows := collectOrphanPublishers(grp)

	if len(rows) != 1 {
		t.Fatalf("expected 1 orphan row, got %d", len(rows))
	}
	row := rows[0]
	if row.Label != "topic-X" {
		t.Errorf("label: want topic-X, got %q", row.Label)
	}
	if row.Broker != "rabbitmq" {
		t.Errorf("broker: want rabbitmq, got %q", row.Broker)
	}
	if row.Repo != "backend" {
		t.Errorf("repo: want backend, got %q", row.Repo)
	}
	if row.Reason != reasonNoSubscriberFound {
		t.Errorf("reason: want %q, got %q", reasonNoSubscriberFound, row.Reason)
	}
	if len(row.Producers) != 1 {
		t.Errorf("producers: want 1, got %d", len(row.Producers))
	}
	// Producers should be prefixed IDs.
	if row.Producers[0] != "backend::producer-svc" {
		t.Errorf("producers[0]: want backend::producer-svc, got %q", row.Producers[0])
	}
}

// TestOrphanPublishers_ProducerAndConsumer — both sides present → 0 orphans.
func TestOrphanPublishers_ProducerAndConsumer(t *testing.T) {
	entities := []graph.Entity{
		graph.Entity{
			ID:   "topic-a",
			Name: "queue-A",
			Kind: "Queue",
		}.WithProperties(map[string]string{"broker": "sqs"}),
		{
			ID:   "pub-svc",
			Name: "Sender",
			Kind: "Function",
		},
		{
			ID:   "sub-svc",
			Name: "Receiver",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "pub-svc", ToID: "topic-a", Kind: "PUBLISHES_TO"},
		{ID: "r2", FromID: "topic-a", ToID: "sub-svc", Kind: "SUBSCRIBES_TO"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	rows := collectOrphanPublishers(grp)

	if len(rows) != 0 {
		t.Errorf("expected 0 orphan rows when consumer exists, got %d", len(rows))
	}
}

// TestOrphanPublishers_ConsumerOnly — topic has only a consumer and no
// producer → NOT an orphan publisher (different endpoint).
func TestOrphanPublishers_ConsumerOnly(t *testing.T) {
	entities := []graph.Entity{
		graph.Entity{
			ID:   "topic-b",
			Name: "broker-1",
			Kind: "MessageTopic",
		}.WithProperties(map[string]string{"broker": "kafka"}),
		{
			ID:   "consumer-svc",
			Name: "Listener",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "topic-b", ToID: "consumer-svc", Kind: "SUBSCRIBES_TO"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	rows := collectOrphanPublishers(grp)

	if len(rows) != 0 {
		t.Errorf("expected 0 orphan rows for consumer-only topic, got %d", len(rows))
	}
}

// TestOrphanPublishers_ZeroProducersZeroConsumers — neither side present →
// NOT reported by this endpoint (would be orphan subscriber territory).
func TestOrphanPublishers_ZeroProducersZeroConsumers(t *testing.T) {
	entities := []graph.Entity{
		{
			ID:   "lonely-topic",
			Name: "lonely",
			Kind: "MessageTopic",
		},
	}

	grp := makeTopologyOrphanGroup(entities, nil)
	rows := collectOrphanPublishers(grp)

	if len(rows) != 0 {
		t.Errorf("expected 0 rows for isolated topic, got %d", len(rows))
	}
}

// TestOrphanPublishers_ChannelOrphan — a ChannelEvent with an emitter and no
// subscriber is an orphan publisher too.
func TestOrphanPublishers_ChannelOrphan(t *testing.T) {
	entities := []graph.Entity{
		graph.Entity{
			ID:   "chan-1",
			Name: "updates",
			Kind: "ChannelEvent",
		}.WithProperties(map[string]string{
			"channel_type": "websocket",
		},
		),
		{
			ID:   "emitter-svc",
			Name: "Notifier",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "emitter-svc", ToID: "chan-1", Kind: "WS_EMITS"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	rows := collectOrphanPublishers(grp)

	if len(rows) != 1 {
		t.Fatalf("expected 1 orphan channel row, got %d", len(rows))
	}
	if rows[0].Label != "updates" {
		t.Errorf("label: want updates, got %q", rows[0].Label)
	}
}

// TestOrphanPublishers_EmptyGroup — empty group returns [] not nil.
func TestOrphanPublishers_EmptyGroup(t *testing.T) {
	grp := &DashGroup{
		Name:  "empty",
		Repos: map[string]*DashRepo{},
	}
	rows := collectOrphanPublishers(grp)

	if rows == nil {
		t.Fatal("expected non-nil slice for empty group")
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

// TestOrphanPublishers_NonTopologyEntitiesIgnored — Function entities must
// not appear in orphan publisher output.
func TestOrphanPublishers_NonTopologyEntitiesIgnored(t *testing.T) {
	entities := []graph.Entity{
		{ID: "fn1", Name: "doWork", Kind: "Function"},
		{ID: "fn2", Name: "helper", Kind: "Function"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "fn1", ToID: "fn2", Kind: "CALLS"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	rows := collectOrphanPublishers(grp)

	if len(rows) != 0 {
		t.Errorf("expected 0 rows for non-topology entities, got %d", len(rows))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration smoke: HTTP endpoint returns correct JSON shape
// ─────────────────────────────────────────────────────────────────────────────

func newOrphanPublisherTestServer(t *testing.T, grp *DashGroup) *httptest.Server {
	t.Helper()
	st := newFakeStore()
	st.groups["mygrp"] = GroupSummary{
		Name:       "mygrp",
		ConfigPath: "/tmp/mygrp.json",
		Repos:      []string{"backend"},
	}
	cfg := DefaultConfig()
	srv, err := NewServer(cfg, st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["mygrp"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts
}

func TestHandleOrphanPublishers_HTTPSmoke(t *testing.T) {
	entities := []graph.Entity{
		graph.Entity{
			ID:   "topic-smoke",
			Name: "topic-X",
			Kind: "MessageTopic",
		}.WithProperties(map[string]string{"broker": "rabbitmq"}),
		{
			ID:   "prod-smoke",
			Name: "SmokePublisher",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		{ID: "rs1", FromID: "prod-smoke", ToID: "topic-smoke", Kind: "PUBLISHES_TO"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	grp.Name = "mygrp"

	ts := newOrphanPublisherTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/topology/mygrp/orphan-publishers")
	if err != nil {
		t.Fatalf("GET orphan-publishers: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	b, _ := io.ReadAll(resp.Body)
	var body struct {
		OrphanPublishers []OrphanPublisherRow `json:"orphan_publishers"`
		Total            int                  `json:"total"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, b)
	}

	if body.Total != 1 {
		t.Errorf("total: want 1, got %d", body.Total)
	}
	if len(body.OrphanPublishers) != 1 {
		t.Fatalf("orphan_publishers len: want 1, got %d", len(body.OrphanPublishers))
	}

	row := body.OrphanPublishers[0]
	if row.Label != "topic-X" {
		t.Errorf("label: want topic-X, got %q", row.Label)
	}
	if row.Broker != "rabbitmq" {
		t.Errorf("broker: want rabbitmq, got %q", row.Broker)
	}
	if row.Reason != reasonNoSubscriberFound {
		t.Errorf("reason: want %q, got %q", reasonNoSubscriberFound, row.Reason)
	}
	if row.Producers == nil {
		t.Error("producers must not be nil")
	}
}

func TestHandleOrphanPublishers_EmptyResult_ArrayNotNull(t *testing.T) {
	// A group with no PUBLISHES_TO edges must return [] (not null).
	entities := []graph.Entity{
		graph.Entity{
			ID:   "topic-covered",
			Name: "covered",
			Kind: "MessageTopic",
		}.WithProperties(map[string]string{"broker": "kafka"}),
		{
			ID:   "pub",
			Name: "Publisher",
			Kind: "Function",
		},
		{
			ID:   "sub",
			Name: "Subscriber",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "pub", ToID: "topic-covered", Kind: "PUBLISHES_TO"},
		{ID: "r2", FromID: "topic-covered", ToID: "sub", Kind: "SUBSCRIBES_TO"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	grp.Name = "mygrp"

	ts := newOrphanPublisherTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/topology/mygrp/orphan-publishers")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	publishers, ok := body["orphan_publishers"]
	if !ok {
		t.Fatal("orphan_publishers key missing")
	}
	arr, ok := publishers.([]any)
	if !ok || len(arr) != 0 {
		t.Errorf("expected empty array, got %v", publishers)
	}
}

func TestHandleOrphanPublishers_UnknownGroup(t *testing.T) {
	grp := makeTopologyOrphanGroup(nil, nil)
	grp.Name = "mygrp"
	ts := newOrphanPublisherTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/topology/nosuchgroup/orphan-publishers")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", resp.StatusCode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Unit tests for collectOrphanSubscribers (#1137)
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanSubscribers_ConsumerOnly — consumer subscribes to topic-X but no
// producer exists → 1 orphan subscriber row.
func TestOrphanSubscribers_ConsumerOnly(t *testing.T) {
	entities := []graph.Entity{
		graph.Entity{
			ID:   "topic-x",
			Name: "topic-X",
			Kind: "MessageTopic",
		}.WithProperties(map[string]string{"broker": "rabbitmq"}),
		{
			ID:   "consumer-svc",
			Name: "OrderListener",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		{
			ID:     "r1",
			FromID: "topic-x",
			ToID:   "consumer-svc",
			Kind:   "SUBSCRIBES_TO",
		},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	rows := collectOrphanSubscribers(grp)

	if len(rows) != 1 {
		t.Fatalf("expected 1 orphan subscriber row, got %d", len(rows))
	}
	row := rows[0]
	if row.Label != "topic-X" {
		t.Errorf("label: want topic-X, got %q", row.Label)
	}
	if row.Broker != "rabbitmq" {
		t.Errorf("broker: want rabbitmq, got %q", row.Broker)
	}
	if row.Repo != "backend" {
		t.Errorf("repo: want backend, got %q", row.Repo)
	}
	if row.Reason != reasonNoPublisherFound {
		t.Errorf("reason: want %q, got %q", reasonNoPublisherFound, row.Reason)
	}
	if len(row.Consumers) != 1 {
		t.Errorf("consumers: want 1, got %d", len(row.Consumers))
	}
	if row.Consumers[0] != "backend::consumer-svc" {
		t.Errorf("consumers[0]: want backend::consumer-svc, got %q", row.Consumers[0])
	}
	if row.LastMessageSeen != nil {
		t.Errorf("last_message_seen: want nil, got %v", row.LastMessageSeen)
	}
}

// TestOrphanSubscribers_ProducerAndConsumer — both sides present → 0 orphan
// subscribers.
func TestOrphanSubscribers_ProducerAndConsumer(t *testing.T) {
	entities := []graph.Entity{
		graph.Entity{
			ID:   "topic-a",
			Name: "queue-A",
			Kind: "Queue",
		}.WithProperties(map[string]string{"broker": "sqs"}),
		{
			ID:   "pub-svc",
			Name: "Sender",
			Kind: "Function",
		},
		{
			ID:   "sub-svc",
			Name: "Receiver",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "pub-svc", ToID: "topic-a", Kind: "PUBLISHES_TO"},
		{ID: "r2", FromID: "topic-a", ToID: "sub-svc", Kind: "SUBSCRIBES_TO"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	rows := collectOrphanSubscribers(grp)

	if len(rows) != 0 {
		t.Errorf("expected 0 orphan subscribers when producer exists, got %d", len(rows))
	}
}

// TestOrphanSubscribers_ProducerOnly — topic has only a producer and no
// consumer → NOT an orphan subscriber (different endpoint).
func TestOrphanSubscribers_ProducerOnly(t *testing.T) {
	entities := []graph.Entity{
		graph.Entity{
			ID:   "topic-b",
			Name: "orders.created",
			Kind: "MessageTopic",
		}.WithProperties(map[string]string{"broker": "kafka"}),
		{
			ID:   "producer-svc",
			Name: "OrderPublisher",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "producer-svc", ToID: "topic-b", Kind: "PUBLISHES_TO"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	rows := collectOrphanSubscribers(grp)

	if len(rows) != 0 {
		t.Errorf("expected 0 orphan subscribers for producer-only topic, got %d", len(rows))
	}
}

// TestOrphanSubscribers_ZeroProducersZeroConsumers — isolated topic is not
// reported by either endpoint.
func TestOrphanSubscribers_ZeroProducersZeroConsumers(t *testing.T) {
	entities := []graph.Entity{
		{ID: "lonely-topic", Name: "lonely", Kind: "MessageTopic"},
	}

	grp := makeTopologyOrphanGroup(entities, nil)
	rows := collectOrphanSubscribers(grp)

	if len(rows) != 0 {
		t.Errorf("expected 0 rows for isolated topic, got %d", len(rows))
	}
}

// TestOrphanSubscribers_ChannelOrphan — a ChannelEvent with a subscriber and
// no emitter is an orphan subscriber.
func TestOrphanSubscribers_ChannelOrphan(t *testing.T) {
	entities := []graph.Entity{
		graph.Entity{
			ID:   "chan-1",
			Name: "updates",
			Kind: "ChannelEvent",
		}.WithProperties(map[string]string{
			"channel_type": "websocket",
		},
		),
		{
			ID:   "listener-svc",
			Name: "FrontendListener",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		// WS_SUBSCRIBES_TO: subscriber points TO the channel entity (same
		// convention as WS_EMITS pointing TO the channel from the emitter).
		{ID: "r1", FromID: "listener-svc", ToID: "chan-1", Kind: "WS_SUBSCRIBES_TO"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	rows := collectOrphanSubscribers(grp)

	if len(rows) != 1 {
		t.Fatalf("expected 1 orphan channel subscriber row, got %d", len(rows))
	}
	if rows[0].Label != "updates" {
		t.Errorf("label: want updates, got %q", rows[0].Label)
	}
}

// TestOrphanSubscribers_ExternalPublisherReason — if publisher_source=external
// the row reason must be 'publisher_only_in_external_lib'.
func TestOrphanSubscribers_ExternalPublisherReason(t *testing.T) {
	entities := []graph.Entity{
		graph.Entity{
			ID:   "ext-topic",
			Name: "vendor.events",
			Kind: "MessageTopic",
		}.WithProperties(map[string]string{
			"broker":           "kafka",
			"publisher_source": "external",
		},
		),
		{
			ID:   "inner-consumer",
			Name: "VendorEventConsumer",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "ext-topic", ToID: "inner-consumer", Kind: "SUBSCRIBES_TO"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	rows := collectOrphanSubscribers(grp)

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Reason != reasonPublisherOnlyInExternal {
		t.Errorf("reason: want %q, got %q", reasonPublisherOnlyInExternal, rows[0].Reason)
	}
}

// TestOrphanSubscribers_EmptyGroup — empty group returns [] not nil.
func TestOrphanSubscribers_EmptyGroup(t *testing.T) {
	grp := &DashGroup{
		Name:  "empty",
		Repos: map[string]*DashRepo{},
	}
	rows := collectOrphanSubscribers(grp)

	if rows == nil {
		t.Fatal("expected non-nil slice for empty group")
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

// TestOrphanSubscribers_NonTopologyEntitiesIgnored — Function entities must
// not appear in orphan subscriber output.
func TestOrphanSubscribers_NonTopologyEntitiesIgnored(t *testing.T) {
	entities := []graph.Entity{
		{ID: "fn1", Name: "doWork", Kind: "Function"},
		{ID: "fn2", Name: "helper", Kind: "Function"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "fn1", ToID: "fn2", Kind: "CALLS"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	rows := collectOrphanSubscribers(grp)

	if len(rows) != 0 {
		t.Errorf("expected 0 rows for non-topology entities, got %d", len(rows))
	}
}

// TestOrphanSubscribers_StableSort — multiple orphan subscribers are sorted
// repo → label.
func TestOrphanSubscribers_StableSort(t *testing.T) {
	entities := []graph.Entity{
		graph.Entity{ID: "topic-z", Name: "zzz-queue", Kind: "Queue"}.WithProperties(map[string]string{"broker": "sqs"}),
		graph.Entity{ID: "topic-a", Name: "aaa-queue", Kind: "Queue"}.WithProperties(map[string]string{"broker": "sqs"}),
		{ID: "cons-1", Name: "ConsumerA", Kind: "Function"},
		{ID: "cons-2", Name: "ConsumerB", Kind: "Function"},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "topic-z", ToID: "cons-1", Kind: "SUBSCRIBES_TO"},
		{ID: "r2", FromID: "topic-a", ToID: "cons-2", Kind: "SUBSCRIBES_TO"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	rows := collectOrphanSubscribers(grp)

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Label != "aaa-queue" || rows[1].Label != "zzz-queue" {
		t.Errorf("sort order wrong: [%q, %q]", rows[0].Label, rows[1].Label)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration smoke: HTTP endpoint for orphan-subscribers
// ─────────────────────────────────────────────────────────────────────────────

func TestHandleOrphanSubscribers_HTTPSmoke(t *testing.T) {
	entities := []graph.Entity{
		graph.Entity{
			ID:   "topic-smoke",
			Name: "topic-X",
			Kind: "MessageTopic",
		}.WithProperties(map[string]string{"broker": "rabbitmq"}),
		{
			ID:   "cons-smoke",
			Name: "SmokeConsumer",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		{ID: "rs1", FromID: "topic-smoke", ToID: "cons-smoke", Kind: "SUBSCRIBES_TO"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	grp.Name = "mygrp"

	ts := newOrphanPublisherTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/topology/mygrp/orphan-subscribers")
	if err != nil {
		t.Fatalf("GET orphan-subscribers: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	b, _ := io.ReadAll(resp.Body)
	var body struct {
		OrphanSubscribers []OrphanSubscriberRow `json:"orphan_subscribers"`
		Total             int                   `json:"total"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, b)
	}

	if body.Total != 1 {
		t.Errorf("total: want 1, got %d", body.Total)
	}
	if len(body.OrphanSubscribers) != 1 {
		t.Fatalf("orphan_subscribers len: want 1, got %d", len(body.OrphanSubscribers))
	}

	row := body.OrphanSubscribers[0]
	if row.Label != "topic-X" {
		t.Errorf("label: want topic-X, got %q", row.Label)
	}
	if row.Broker != "rabbitmq" {
		t.Errorf("broker: want rabbitmq, got %q", row.Broker)
	}
	if row.Reason != reasonNoPublisherFound {
		t.Errorf("reason: want %q, got %q", reasonNoPublisherFound, row.Reason)
	}
	if row.Consumers == nil {
		t.Error("consumers must not be nil")
	}
	if row.LastMessageSeen != nil {
		t.Errorf("last_message_seen: want null, got %v", row.LastMessageSeen)
	}
}

func TestHandleOrphanSubscribers_EmptyResult_ArrayNotNull(t *testing.T) {
	// A group where the topic has both producer and consumer must return [].
	entities := []graph.Entity{
		graph.Entity{
			ID:   "topic-covered",
			Name: "covered",
			Kind: "MessageTopic",
		}.WithProperties(map[string]string{"broker": "kafka"}),
		{
			ID:   "pub",
			Name: "Publisher",
			Kind: "Function",
		},
		{
			ID:   "sub",
			Name: "Subscriber",
			Kind: "Function",
		},
	}
	rels := []graph.Relationship{
		{ID: "r1", FromID: "pub", ToID: "topic-covered", Kind: "PUBLISHES_TO"},
		{ID: "r2", FromID: "topic-covered", ToID: "sub", Kind: "SUBSCRIBES_TO"},
	}

	grp := makeTopologyOrphanGroup(entities, rels)
	grp.Name = "mygrp"

	ts := newOrphanPublisherTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/topology/mygrp/orphan-subscribers")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	subscribers, ok := body["orphan_subscribers"]
	if !ok {
		t.Fatal("orphan_subscribers key missing")
	}
	arr, ok := subscribers.([]any)
	if !ok || len(arr) != 0 {
		t.Errorf("expected empty array, got %v", subscribers)
	}
}

func TestHandleOrphanSubscribers_UnknownGroup(t *testing.T) {
	grp := makeTopologyOrphanGroup(nil, nil)
	grp.Name = "mygrp"
	ts := newOrphanPublisherTestServer(t, grp)

	resp, err := http.Get(ts.URL + "/api/topology/nosuchgroup/orphan-subscribers")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", resp.StatusCode)
	}
}
