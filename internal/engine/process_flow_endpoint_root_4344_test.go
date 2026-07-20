// process_flow_endpoint_root_4344_test.go — tests for issue #4344.
//
// #4344 roots process-flows at the HTTP endpoint via the reversed-IMPLEMENTS
// bridge (#4319) and stops dropping short endpoint-rooted flows, so every
// endpoint with a handler yields a flow labeled by route.
//
// Shape under test:
//
//	http_endpoint_definition --IMPLEMENTS--> handler --CALLS--> service --CALLS--> repo
//
// Assertions:
//  1. The emitted Process ROOTS at the endpoint (step 1 = endpoint, label =
//     route) and spans endpoint → handler → service → repo with no
//     double-counting of the handler.
//  2. A SHORT endpoint flow (endpoint → handler → service, 2 hops) is NOT
//     dropped by the MinSteps cutoff.
//  3. Broker / scheduled entry flows are unchanged (guard against regression).
package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// endpointRootDoc builds the canonical
// endpoint --IMPLEMENTS--> handler --CALLS--> service --CALLS--> repo shape.
// The route label is intentionally distinct from the handler function name so
// the test can assert the flow is labeled by ROUTE, not by function.
func endpointRootDoc(repo string) *graph.Document {
	return &graph.Document{
		Repo: repo,
		Entities: []graph.Entity{
			graph.Entity{ID: "ep:latest", Name: "GET /buildings/{id}/latest", Kind: "http_endpoint_definition",
				Language: "python", SourceFile: "urls.py",
			}.WithProperties(map[string]string{"http_method": "GET", "route": "/buildings/{id}/latest"}),
			{ID: "fn:latest", Name: "BuildingViewSet.latest", Kind: "SCOPE.Operation",
				Language: "python", SourceFile: "views.py"},
			{ID: "fn:service", Name: "BuildingService.get_latest", Kind: "SCOPE.Operation",
				Language: "python", SourceFile: "service.py"},
			{ID: "fn:repo", Name: "BuildingRepository.fetch", Kind: "SCOPE.Operation",
				Language: "python", SourceFile: "repo.py"},
		},
		Relationships: []graph.Relationship{
			// handler IMPLEMENTS endpoint — reversed into a continuation edge.
			{ID: "impl:1", FromID: "fn:latest", ToID: "ep:latest", Kind: "IMPLEMENTS"},
			// handler CALLS chain.
			{ID: "c:1", FromID: "fn:latest", ToID: "fn:service", Kind: "CALLS"},
			{ID: "c:2", FromID: "fn:service", ToID: "fn:repo", Kind: "CALLS"},
		},
	}
}

// Test 1 — the Process roots at the endpoint and spans the full chain.
func TestProcessFlow_4344_RootsAtEndpoint(t *testing.T) {
	doc := endpointRootDoc("fixture-4344")
	RunProcessFlow(doc, DefaultProcessFlowConfig())

	p := findProcessByEntry(doc, "ep:latest")
	if p == nil {
		t.Fatalf("no Process rooted at the endpoint ep:latest; processes: %v", allProcessProps(doc))
	}
	chain := strings.Split(p.PropGet("chain"), ",")
	want := []string{"ep:latest", "fn:latest", "fn:service", "fn:repo"}
	if len(chain) != len(want) {
		t.Fatalf("chain length = %d, want %d; chain=%v", len(chain), len(want), chain)
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Fatalf("chain[%d] = %q, want %q; chain=%v", i, chain[i], want[i], chain)
		}
	}
	// Step 1 (index 0) must be the endpoint.
	if chain[0] != "ep:latest" {
		t.Fatalf("step 1 is not the endpoint: %q", chain[0])
	}
	// Label / entry_name must be the ROUTE, not the handler function name.
	if !strings.Contains(p.Name, "GET /buildings/{id}/latest") {
		t.Errorf("Process label %q is not the route; expected the route as the entry label", p.Name)
	}
	if p.PropGet("entry_name") != "GET /buildings/{id}/latest" {
		t.Errorf("entry_name = %q, want the route", p.PropGet("entry_name"))
	}
	if p.PropGet("entry_kind") != "http" {
		t.Errorf("entry_kind = %q, want http", p.PropGet("entry_kind"))
	}
	// No double-counting: the handler appears exactly once.
	handlerCount := 0
	for _, s := range chain {
		if s == "fn:latest" {
			handlerCount++
		}
	}
	if handlerCount != 1 {
		t.Errorf("handler fn:latest appears %d times in the chain (double-counting); chain=%v", handlerCount, chain)
	}
	// And there must NOT also be a separate flow rooted at the handler — the
	// route would otherwise be counted twice.
	if hp := findProcessByEntry(doc, "fn:latest"); hp != nil {
		t.Errorf("a redundant Process is rooted at the handler fn:latest; route double-counted")
	}
}

// Test 2 — a SHORT endpoint flow (endpoint → handler → service, 2 hops, 3
// steps once rooted) is NOT dropped. The pre-#4344 default MinSteps=3 only
// counted handler-rooted steps (handler → service = 2 < 3) and dropped these.
func TestProcessFlow_4344_ShortEndpointFlowNotDropped(t *testing.T) {
	doc := &graph.Document{
		Repo: "fixture-4344-short",
		Entities: []graph.Entity{
			{ID: "ep:ping", Name: "GET /ping", Kind: "http_endpoint_definition",
				Language: "go", SourceFile: "routes.go"},
			{ID: "fn:ping", Name: "PingHandler", Kind: "SCOPE.Function",
				Language: "go", SourceFile: "handler.go"},
			{ID: "fn:status", Name: "StatusService.check", Kind: "SCOPE.Function",
				Language: "go", SourceFile: "status.go"},
		},
		Relationships: []graph.Relationship{
			{ID: "impl:1", FromID: "fn:ping", ToID: "ep:ping", Kind: "IMPLEMENTS"},
			{ID: "c:1", FromID: "fn:ping", ToID: "fn:status", Kind: "CALLS"},
		},
	}
	cfg := DefaultProcessFlowConfig() // MinSteps = 3
	stats := RunProcessFlow(doc, cfg)
	if stats.Processes == 0 {
		t.Fatalf("short endpoint flow was dropped; expected ≥1 Process")
	}
	p := findProcessByEntry(doc, "ep:ping")
	if p == nil {
		t.Fatalf("short endpoint flow not rooted at endpoint; processes: %v", allProcessProps(doc))
	}
	chain := strings.Split(p.PropGet("chain"), ",")
	want := []string{"ep:ping", "fn:ping", "fn:status"}
	if strings.Join(chain, ",") != strings.Join(want, ",") {
		t.Fatalf("short chain = %v, want %v", chain, want)
	}
}

// Test 3 — even a 2-step endpoint → handler flow (handler is a leaf) survives.
func TestProcessFlow_4344_TwoStepEndpointFlowSurvives(t *testing.T) {
	doc := &graph.Document{
		Repo: "fixture-4344-tiny",
		Entities: []graph.Entity{
			{ID: "ep:health", Name: "GET /health", Kind: "http_endpoint_definition",
				Language: "go", SourceFile: "routes.go"},
			{ID: "fn:health", Name: "HealthHandler", Kind: "SCOPE.Function",
				Language: "go", SourceFile: "handler.go"},
		},
		Relationships: []graph.Relationship{
			{ID: "impl:1", FromID: "fn:health", ToID: "ep:health", Kind: "IMPLEMENTS"},
		},
	}
	stats := RunProcessFlow(doc, DefaultProcessFlowConfig())
	if stats.Processes == 0 {
		t.Fatalf("2-step endpoint→handler flow was dropped; expected 1 Process")
	}
	p := findProcessByEntry(doc, "ep:health")
	if p == nil {
		t.Fatalf("2-step flow not rooted at endpoint; processes: %v", allProcessProps(doc))
	}
	if got := p.PropGet("chain"); got != "ep:health,fn:health" {
		t.Fatalf("chain = %q, want ep:health,fn:health", got)
	}
}

// Test 4 (guard) — broker / scheduled entry flows are unchanged: they do NOT
// root at an endpoint and keep their broker entry_kind + handler root.
func TestProcessFlow_4344_BrokerFlowsUnchanged(t *testing.T) {
	// Kafka consumer — must still root at the handler with entry_kind kafka_consumer.
	kdoc := buildKafkaConsumerDoc("fixture-4344-broker")
	RunProcessFlow(kdoc, DefaultProcessFlowConfig())
	kp := processWithEntryKind(kdoc, "kafka_consumer")
	if kp == nil {
		t.Fatalf("kafka consumer flow regressed; processes: %v", allProcessProps(kdoc))
	}
	if kp.PropGet("chain") == "" || strings.HasPrefix(kp.PropGet("entry_name"), "GET ") {
		t.Errorf("kafka flow unexpectedly rooted at an endpoint: %v", kp.PropsSnapshot())
	}
	// The kafka chain must still root at the handler op:onFeedback.
	if got := strings.Split(kp.PropGet("chain"), ",")[0]; got != "op:onFeedback" {
		t.Errorf("kafka flow root = %q, want op:onFeedback", got)
	}

	// Scheduled handler — must still root at the handler with entry_kind scheduled.
	sdoc := &graph.Document{
		Repo: "fixture-4344-sched",
		Entities: []graph.Entity{
			{ID: "job:cleanup", Name: "scheduled:cleanup", Kind: "SCOPE.ScheduledJob", Language: "java", SourceFile: "CleanupJob.java"},
			{ID: "op:cleanup", Name: "CleanupJob.cleanup", Kind: "SCOPE.Operation", Language: "java", SourceFile: "CleanupJob.java"},
			{ID: "op:del", Name: "RecordStore.deleteExpired", Kind: "SCOPE.Operation", Language: "java", SourceFile: "RecordStore.java"},
			{ID: "op:notify", Name: "NotificationService.send", Kind: "SCOPE.Operation", Language: "java", SourceFile: "NotificationService.java"},
		},
		Relationships: []graph.Relationship{
			{ID: "tr:1", FromID: "job:cleanup", ToID: "op:cleanup", Kind: "TRIGGERS"},
			{ID: "c:1", FromID: "op:cleanup", ToID: "op:del", Kind: "CALLS"},
			{ID: "c:2", FromID: "op:del", ToID: "op:notify", Kind: "CALLS"},
		},
	}
	RunProcessFlow(sdoc, DefaultProcessFlowConfig())
	sp := processWithEntryKind(sdoc, "scheduled")
	if sp == nil {
		t.Fatalf("scheduled flow regressed; processes: %v", allProcessProps(sdoc))
	}
	if got := strings.Split(sp.PropGet("chain"), ",")[0]; got != "op:cleanup" {
		t.Errorf("scheduled flow root = %q, want op:cleanup", got)
	}
}
