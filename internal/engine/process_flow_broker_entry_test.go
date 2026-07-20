// Tests for broker-side entry-point detection (#797).
//
// Covers Kafka @Incoming / @KafkaListener consumers, @Scheduled handlers,
// WebSocket @OnMessage handlers, and GraphQL subscription resolvers as BFS
// entry points in process_flow.go / process_flow_entry.go.
//
// All tests assert SCOPE.Process entities are emitted when the relevant
// broker-boundary edges (SUBSCRIBES_TO / WS_SUBSCRIBES_TO / TRIGGERS) are
// present in the graph — verifying that the previous zero-process outcome
// (fixture-f) is resolved.
package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// processWithEntryKind returns the first Process entity with the given
// entry_kind property, or nil when none is found.
func processWithEntryKind(doc *graph.Document, kind string) *graph.Entity {
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind != string(EntityKindProcess) {
			continue
		}
		if e.PropGet("entry_kind") == kind {
			return e
		}
	}
	return nil
}

// buildKafkaConsumerDoc builds a synthetic graph representing a Quarkus
// @Incoming("feedback") method that CALLS downstream business logic.
//
//	TriageProcessor.onFeedback → CALLS → TriageTools.classify → CALLS → DataStore.save
//
// The SUBSCRIBES_TO edge from TriageProcessor.onFeedback to the Kafka
// MessageTopic entity is added manually to simulate what kafka_edges.go
// emits at extraction time.
func buildKafkaConsumerDoc(repo string) *graph.Document {
	doc := &graph.Document{Repo: repo}
	doc.Entities = []graph.Entity{
		{ID: "op:onFeedback", Name: "TriageProcessor.onFeedback", Kind: "SCOPE.Operation", Language: "java", SourceFile: "TriageProcessor.java"},
		{ID: "op:classify", Name: "TriageTools.classify", Kind: "SCOPE.Operation", Language: "java", SourceFile: "TriageTools.java"},
		{ID: "op:save", Name: "DataStore.save", Kind: "SCOPE.Operation", Language: "java", SourceFile: "DataStore.java"},
		{ID: "topic:feedback", Name: "kafka:feedback", Kind: "SCOPE.MessageTopic", Language: "java", SourceFile: ""},
	}
	doc.Relationships = []graph.Relationship{
		// Broker signal: onFeedback subscribes to the Kafka topic.
		graph.Relationship{ID: "sub:1", FromID: "op:onFeedback", ToID: "topic:feedback", Kind: "SUBSCRIBES_TO"}.WithProperties(map[string]string{"messaging_layer": "smallrye_reactive"}),
		// Business logic CALLS chain.
		{ID: "c:1", FromID: "op:onFeedback", ToID: "op:classify", Kind: "CALLS"},
		{ID: "c:2", FromID: "op:classify", ToID: "op:save", Kind: "CALLS"},
	}
	return doc
}

// ---------------------------------------------------------------------------
// Test 1: Kafka @Incoming handler produces SCOPE.Process
// ---------------------------------------------------------------------------

func TestBrokerEntry_KafkaIncoming_EmitsProcess(t *testing.T) {
	doc := buildKafkaConsumerDoc("fixture-f")
	stats := RunProcessFlow(doc, DefaultProcessFlowConfig())
	if stats.Processes == 0 {
		t.Fatalf("expected ≥1 SCOPE.Process for Kafka @Incoming handler, got 0")
	}
}

// ---------------------------------------------------------------------------
// Test 2: Kafka handler entry_kind == "kafka_consumer"
// ---------------------------------------------------------------------------

func TestBrokerEntry_KafkaIncoming_EntryKind(t *testing.T) {
	doc := buildKafkaConsumerDoc("fixture-f")
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	p := processWithEntryKind(doc, "kafka_consumer")
	if p == nil {
		t.Fatalf("no Process with entry_kind=kafka_consumer; all processes: %v", allProcessProps(doc))
	}
}

// ---------------------------------------------------------------------------
// Test 3: Kafka chain labels include downstream business logic
// ---------------------------------------------------------------------------

func TestBrokerEntry_KafkaIncoming_ChainContainsBusinessLogic(t *testing.T) {
	doc := buildKafkaConsumerDoc("fixture-f")
	RunProcessFlow(doc, DefaultProcessFlowConfig())
	p := processWithEntryKind(doc, "kafka_consumer")
	if p == nil {
		t.Fatalf("no kafka_consumer process emitted")
	}
	chainLabels := p.PropGet("chain_labels")
	if !strings.Contains(chainLabels, "TriageTools.classify") {
		t.Errorf("chain_labels %q does not contain classify step", chainLabels)
	}
	if !strings.Contains(chainLabels, "DataStore.save") {
		t.Errorf("chain_labels %q does not contain save step", chainLabels)
	}
}

// ---------------------------------------------------------------------------
// Test 4: Spring @KafkaListener produces a Process
// ---------------------------------------------------------------------------

func TestBrokerEntry_SpringKafkaListener_EmitsProcess(t *testing.T) {
	doc := &graph.Document{Repo: "fixture-f"}
	doc.Entities = []graph.Entity{
		{ID: "op:onOrder", Name: "OrderConsumer.onOrder", Kind: "SCOPE.Operation", Language: "java", SourceFile: "OrderConsumer.java"},
		{ID: "op:process", Name: "OrderService.process", Kind: "SCOPE.Operation", Language: "java", SourceFile: "OrderService.java"},
		{ID: "op:audit", Name: "AuditLog.record", Kind: "SCOPE.Operation", Language: "java", SourceFile: "AuditLog.java"},
		{ID: "topic:orders", Name: "kafka:orders", Kind: "SCOPE.MessageTopic", Language: "java", SourceFile: ""},
	}
	doc.Relationships = []graph.Relationship{
		graph.Relationship{ID: "sub:1", FromID: "op:onOrder", ToID: "topic:orders", Kind: "SUBSCRIBES_TO"}.WithProperties(map[string]string{"messaging_layer": "spring_kafka"}),
		{ID: "c:1", FromID: "op:onOrder", ToID: "op:process", Kind: "CALLS"},
		{ID: "c:2", FromID: "op:process", ToID: "op:audit", Kind: "CALLS"},
	}
	stats := RunProcessFlow(doc, DefaultProcessFlowConfig())
	if stats.Processes == 0 {
		t.Fatal("expected SCOPE.Process for Spring @KafkaListener handler")
	}
	p := processWithEntryKind(doc, "kafka_consumer")
	if p == nil {
		t.Errorf("entry_kind not set to kafka_consumer for Spring @KafkaListener handler")
	}
}

// ---------------------------------------------------------------------------
// Test 5: @Scheduled handler produces a Process with entry_kind="scheduled"
// ---------------------------------------------------------------------------

func TestBrokerEntry_Scheduled_EmitsProcess(t *testing.T) {
	doc := &graph.Document{Repo: "fixture-f"}
	doc.Entities = []graph.Entity{
		// The scheduled job entity emitted by applyScheduledJobEdges.
		{ID: "job:cleanup", Name: "quarkus_scheduled:/CleanupJob.java:cleanup", Kind: "SCOPE.ScheduledJob", Language: "java", SourceFile: "CleanupJob.java"},
		// The handler method entity from the Java extractor.
		{ID: "op:cleanup", Name: "CleanupJob.cleanup", Kind: "SCOPE.Operation", Language: "java", SourceFile: "CleanupJob.java"},
		{ID: "op:deleteExpired", Name: "RecordStore.deleteExpired", Kind: "SCOPE.Operation", Language: "java", SourceFile: "RecordStore.java"},
		{ID: "op:notify", Name: "NotificationService.send", Kind: "SCOPE.Operation", Language: "java", SourceFile: "NotificationService.java"},
	}
	doc.Relationships = []graph.Relationship{
		// ScheduledJob TRIGGERS the handler.
		graph.Relationship{ID: "tr:1", FromID: "job:cleanup", ToID: "op:cleanup", Kind: "TRIGGERS"}.WithProperties(map[string]string{"framework": "quarkus_scheduled"}),
		// Business logic CALLS chain.
		{ID: "c:1", FromID: "op:cleanup", ToID: "op:deleteExpired", Kind: "CALLS"},
		{ID: "c:2", FromID: "op:deleteExpired", ToID: "op:notify", Kind: "CALLS"},
	}
	stats := RunProcessFlow(doc, DefaultProcessFlowConfig())
	if stats.Processes == 0 {
		t.Fatal("expected SCOPE.Process for @Scheduled handler")
	}
	p := processWithEntryKind(doc, "scheduled")
	if p == nil {
		t.Errorf("entry_kind not set to 'scheduled'; processes: %v", allProcessProps(doc))
	}
}

// ---------------------------------------------------------------------------
// Test 6: WebSocket @OnMessage handler produces a Process with entry_kind="websocket"
// ---------------------------------------------------------------------------

func TestBrokerEntry_WebSocket_EmitsProcess(t *testing.T) {
	doc := &graph.Document{Repo: "fixture-f"}
	doc.Entities = []graph.Entity{
		// WebSocket handler (Jakarta @ServerEndpoint @OnMessage)
		{ID: "cls:TraceEndpoint.onMessage", Name: "TraceEndpoint.onMessage", Kind: "SCOPE.Operation", Language: "java", SourceFile: "TraceEndpoint.java"},
		{ID: "op:processTrace", Name: "TraceService.processTrace", Kind: "SCOPE.Operation", Language: "java", SourceFile: "TraceService.java"},
		{ID: "op:store", Name: "TraceStore.store", Kind: "SCOPE.Operation", Language: "java", SourceFile: "TraceStore.java"},
		// ChannelEvent entity from websocket synthesis.
		{ID: "ws:/ws/trace", Name: "ws:/ws/trace", Kind: "ChannelEvent", Language: "java", SourceFile: ""},
	}
	doc.Relationships = []graph.Relationship{
		// WebSocket subscription signal.
		graph.Relationship{ID: "wssub:1", FromID: "cls:TraceEndpoint.onMessage", ToID: "ws:/ws/trace", Kind: "WS_SUBSCRIBES_TO"}.WithProperties(map[string]string{"framework": "jakarta_websocket"}),
		// Business logic.
		{ID: "c:1", FromID: "cls:TraceEndpoint.onMessage", ToID: "op:processTrace", Kind: "CALLS"},
		{ID: "c:2", FromID: "op:processTrace", ToID: "op:store", Kind: "CALLS"},
	}
	stats := RunProcessFlow(doc, DefaultProcessFlowConfig())
	if stats.Processes == 0 {
		t.Fatal("expected SCOPE.Process for WebSocket @OnMessage handler")
	}
	p := processWithEntryKind(doc, "websocket")
	if p == nil {
		t.Errorf("entry_kind not set to 'websocket'; processes: %v", allProcessProps(doc))
	}
}

// ---------------------------------------------------------------------------
// Test 7: Broker-entry handler is ranked above plain Function with same fan-out
// ---------------------------------------------------------------------------

func TestBrokerEntry_RankedAbovePlainFunction(t *testing.T) {
	// Graph: two entry candidates with identical CALLS fan-out.
	// plainHandler has no broker signal; kafkaHandler has SUBSCRIBES_TO.
	// kafkaHandler should be the selected entry (higher score).
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "plain:handler", Name: "plainHandler", Kind: "SCOPE.Function", Language: "java", SourceFile: "A.java"},
		{ID: "kafka:handler", Name: "kafkaHandler", Kind: "SCOPE.Function", Language: "java", SourceFile: "B.java"},
		{ID: "shared:leaf1", Name: "step1", Kind: "SCOPE.Function", Language: "java", SourceFile: "C.java"},
		{ID: "shared:leaf2", Name: "step2", Kind: "SCOPE.Function", Language: "java", SourceFile: "C.java"},
		{ID: "shared:leaf3", Name: "step3", Kind: "SCOPE.Function", Language: "java", SourceFile: "C.java"},
		{ID: "topic:x", Name: "kafka:x", Kind: "SCOPE.MessageTopic", Language: "java", SourceFile: ""},
	}
	doc.Relationships = []graph.Relationship{
		// Kafka broker signal only for kafkaHandler.
		{ID: "sub:1", FromID: "kafka:handler", ToID: "topic:x", Kind: "SUBSCRIBES_TO"},
		// Both handlers have identical CALLS fan-out (3 leaves).
		{ID: "c:p1", FromID: "plain:handler", ToID: "shared:leaf1", Kind: "CALLS"},
		{ID: "c:p2", FromID: "plain:handler", ToID: "shared:leaf2", Kind: "CALLS"},
		{ID: "c:p3", FromID: "plain:handler", ToID: "shared:leaf3", Kind: "CALLS"},
		{ID: "c:k1", FromID: "kafka:handler", ToID: "shared:leaf1", Kind: "CALLS"},
		{ID: "c:k2", FromID: "kafka:handler", ToID: "shared:leaf2", Kind: "CALLS"},
		{ID: "c:k3", FromID: "kafka:handler", ToID: "shared:leaf3", Kind: "CALLS"},
	}

	byID := make(map[string]*graph.Entity)
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = &doc.Entities[i]
	}
	adj := buildCallsAdjacency(doc)
	candidates := rankEntryPoints(doc, byID, adj, DefaultProcessFlowConfig())

	// Find scores.
	kafkaScore := -1.0
	plainScore := -1.0
	for _, c := range candidates {
		switch c.id {
		case "kafka:handler":
			kafkaScore = c.score
		case "plain:handler":
			plainScore = c.score
		}
	}
	if kafkaScore <= plainScore {
		t.Errorf("kafkaHandler score (%v) should exceed plainHandler score (%v) due to broker boost", kafkaScore, plainScore)
	}
}

// ---------------------------------------------------------------------------
// Test 8: Multiple Kafka services in same doc each get their own Process
// ---------------------------------------------------------------------------

func TestBrokerEntry_MultipleKafkaServices_EachGetProcess(t *testing.T) {
	// Simulates 2 of the 8 fixture-f Java services with independent
	// @Incoming handlers and distinct call chains.
	doc := &graph.Document{Repo: "fixture-f"}
	doc.Entities = []graph.Entity{
		// Service A: ai-triage
		{ID: "op:triage", Name: "TriageProcessor.onFeedback", Kind: "SCOPE.Operation", Language: "java", SourceFile: "TriageProcessor.java"},
		{ID: "op:classify", Name: "TriageTools.classify", Kind: "SCOPE.Operation", Language: "java", SourceFile: "TriageTools.java"},
		{ID: "op:emitLabel", Name: "LabelEmitter.emit", Kind: "SCOPE.Operation", Language: "java", SourceFile: "LabelEmitter.java"},
		// Service B: order-processor
		{ID: "op:orderConsume", Name: "OrderConsumer.onOrder", Kind: "SCOPE.Operation", Language: "java", SourceFile: "OrderConsumer.java"},
		{ID: "op:fulfil", Name: "FulfilmentService.fulfil", Kind: "SCOPE.Operation", Language: "java", SourceFile: "FulfilmentService.java"},
		{ID: "op:ack", Name: "AckService.ack", Kind: "SCOPE.Operation", Language: "java", SourceFile: "AckService.java"},
		// Topics
		{ID: "topic:feedback", Name: "kafka:feedback", Kind: "SCOPE.MessageTopic", Language: "java", SourceFile: ""},
		{ID: "topic:orders", Name: "kafka:orders", Kind: "SCOPE.MessageTopic", Language: "java", SourceFile: ""},
	}
	doc.Relationships = []graph.Relationship{
		// Broker signals.
		{ID: "sub:A", FromID: "op:triage", ToID: "topic:feedback", Kind: "SUBSCRIBES_TO"},
		{ID: "sub:B", FromID: "op:orderConsume", ToID: "topic:orders", Kind: "SUBSCRIBES_TO"},
		// Service A call chain.
		{ID: "c:A1", FromID: "op:triage", ToID: "op:classify", Kind: "CALLS"},
		{ID: "c:A2", FromID: "op:classify", ToID: "op:emitLabel", Kind: "CALLS"},
		// Service B call chain.
		{ID: "c:B1", FromID: "op:orderConsume", ToID: "op:fulfil", Kind: "CALLS"},
		{ID: "c:B2", FromID: "op:fulfil", ToID: "op:ack", Kind: "CALLS"},
	}
	stats := RunProcessFlow(doc, DefaultProcessFlowConfig())
	if stats.Processes < 2 {
		t.Errorf("expected ≥2 processes for 2 independent Kafka services, got %d", stats.Processes)
	}
	// Both entries should be marked kafka_consumer.
	kafkaCount := 0
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind == string(EntityKindProcess) && e.PropGet("entry_kind") == "kafka_consumer" {
			kafkaCount++
		}
	}
	if kafkaCount < 2 {
		t.Errorf("expected ≥2 kafka_consumer processes, got %d", kafkaCount)
	}
}

// ---------------------------------------------------------------------------
// Test 9: Broker entry respects MinSteps (≥3 steps to survive)
// ---------------------------------------------------------------------------

func TestBrokerEntry_MinStepsEnforced(t *testing.T) {
	// A Kafka handler with only 1 CALLS hop (2 nodes total) should NOT
	// emit a Process — it falls below MinSteps=3.
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "op:onMsg", Name: "Handler.onMsg", Kind: "SCOPE.Operation", Language: "java", SourceFile: "Handler.java"},
		{ID: "op:doIt", Name: "Service.doIt", Kind: "SCOPE.Operation", Language: "java", SourceFile: "Service.java"},
		{ID: "topic:t", Name: "kafka:t", Kind: "SCOPE.MessageTopic", Language: "java", SourceFile: ""},
	}
	doc.Relationships = []graph.Relationship{
		{ID: "sub:1", FromID: "op:onMsg", ToID: "topic:t", Kind: "SUBSCRIBES_TO"},
		{ID: "c:1", FromID: "op:onMsg", ToID: "op:doIt", Kind: "CALLS"},
	}
	cfg := DefaultProcessFlowConfig()
	cfg.MinSteps = 3
	stats := RunProcessFlow(doc, cfg)
	if stats.Processes != 0 {
		t.Errorf("2-node broker chain should be filtered by MinSteps=3, got %d processes", stats.Processes)
	}
}

// ---------------------------------------------------------------------------
// Test 10: Existing HTTP handler chains are not regressed by broker boost
// ---------------------------------------------------------------------------

func TestBrokerEntry_HTTPHandlerNotRegressed(t *testing.T) {
	// Classic HTTP handler with IMPLEMENTS edge — the existing http_boundary
	// boost should still work and entry_kind should be "http".
	doc := &graph.Document{Repo: "r"}
	doc.Entities = []graph.Entity{
		{ID: "fn:handle", Name: "handleOrders", Kind: "SCOPE.Function", Language: "java", SourceFile: "api.java"},
		{ID: "fn:svc", Name: "OrderService.create", Kind: "SCOPE.Function", Language: "java", SourceFile: "svc.java"},
		{ID: "fn:repo", Name: "OrderRepo.save", Kind: "SCOPE.Function", Language: "java", SourceFile: "repo.java"},
		graph.Entity{ID: "ep:orders", Name: "http:POST:/orders", Kind: "http_endpoint", Language: "java", SourceFile: "api.java"}.WithProperties(map[string]string{"pattern_type": "http_endpoint_synthesis"}),
	}
	doc.Relationships = []graph.Relationship{
		{ID: "impl:1", FromID: "fn:handle", ToID: "ep:orders", Kind: "IMPLEMENTS"},
		{ID: "c:1", FromID: "fn:handle", ToID: "fn:svc", Kind: "CALLS"},
		{ID: "c:2", FromID: "fn:svc", ToID: "fn:repo", Kind: "CALLS"},
	}
	stats := RunProcessFlow(doc, DefaultProcessFlowConfig())
	if stats.Processes == 0 {
		t.Fatal("HTTP handler chain should still produce a Process after broker-boost changes")
	}
	p := processWithEntryKind(doc, "http")
	if p == nil {
		t.Errorf("HTTP handler process should have entry_kind=http; processes: %v", allProcessProps(doc))
	}
}

// ---------------------------------------------------------------------------
// Test 11: buildBrokerBoundarySet detects all three edge kinds
// ---------------------------------------------------------------------------

func TestBuildBrokerBoundarySet(t *testing.T) {
	doc := &graph.Document{Repo: "r"}
	doc.Relationships = []graph.Relationship{
		// Kafka SUBSCRIBES_TO.
		{ID: "s1", FromID: "fn:kafkaHandler", ToID: "topic:x", Kind: "SUBSCRIBES_TO"},
		// WebSocket WS_SUBSCRIBES_TO.
		{ID: "s2", FromID: "fn:wsHandler", ToID: "ws:/ws/chat", Kind: "WS_SUBSCRIBES_TO"},
		// ScheduledJob TRIGGERS.
		{ID: "s3", FromID: "job:nightly", ToID: "fn:schedHandler", Kind: "TRIGGERS"},
	}
	boundary := buildBrokerBoundarySet(doc)

	if boundary["fn:kafkaHandler"] != "kafka_consumer" {
		t.Errorf("fn:kafkaHandler: want kafka_consumer, got %q", boundary["fn:kafkaHandler"])
	}
	if boundary["fn:wsHandler"] != "websocket" {
		t.Errorf("fn:wsHandler: want websocket, got %q", boundary["fn:wsHandler"])
	}
	if boundary["fn:schedHandler"] != "scheduled" {
		t.Errorf("fn:schedHandler: want scheduled, got %q", boundary["fn:schedHandler"])
	}
	// The topic / channel / job entities themselves should NOT be in the boundary.
	if _, found := boundary["topic:x"]; found {
		t.Errorf("topic:x should not be in broker boundary")
	}
	if _, found := boundary["job:nightly"]; found {
		t.Errorf("job:nightly should not be in broker boundary")
	}
}

// ---------------------------------------------------------------------------
// Test 12: Node.js Kafka consumer (kafkajs) produces a Process
// ---------------------------------------------------------------------------

func TestBrokerEntry_NodeKafkaConsumer_EmitsProcess(t *testing.T) {
	// Node.js kafkajs consumer: the handler function is linked via SUBSCRIBES_TO
	// to a MessageTopic entity by kafka_edges.go. The function then CALLS
	// downstream business logic.
	doc := &graph.Document{Repo: "fixture-f-node"}
	doc.Entities = []graph.Entity{
		{ID: "fn:processMessage", Name: "processMessage", Kind: "SCOPE.Function", Language: "javascript", SourceFile: "consumer.js"},
		{ID: "fn:validateMsg", Name: "validateMessage", Kind: "SCOPE.Function", Language: "javascript", SourceFile: "validator.js"},
		{ID: "fn:storeMsg", Name: "storeMessage", Kind: "SCOPE.Function", Language: "javascript", SourceFile: "store.js"},
		{ID: "topic:events", Name: "kafka:events", Kind: "SCOPE.MessageTopic", Language: "javascript", SourceFile: ""},
	}
	doc.Relationships = []graph.Relationship{
		graph.Relationship{ID: "sub:1", FromID: "fn:processMessage", ToID: "topic:events", Kind: "SUBSCRIBES_TO"}.WithProperties(map[string]string{"messaging_layer": "kafkajs"}),
		{ID: "c:1", FromID: "fn:processMessage", ToID: "fn:validateMsg", Kind: "CALLS"},
		{ID: "c:2", FromID: "fn:validateMsg", ToID: "fn:storeMsg", Kind: "CALLS"},
	}
	stats := RunProcessFlow(doc, DefaultProcessFlowConfig())
	if stats.Processes == 0 {
		t.Fatal("Node.js kafkajs consumer should produce SCOPE.Process")
	}
	p := processWithEntryKind(doc, "kafka_consumer")
	if p == nil {
		t.Errorf("Node.js kafka consumer entry_kind should be kafka_consumer")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// allProcessProps returns a diagnostic slice of all Process entity properties
// for error messages.
func allProcessProps(doc *graph.Document) []map[string]string {
	var out []map[string]string
	for _, e := range doc.Entities {
		if e.Kind == string(EntityKindProcess) {
			out = append(out, e.PropsSnapshot())
		}
	}
	return out
}
