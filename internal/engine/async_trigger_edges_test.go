// Tests for the async-trigger edge-synthesis pass (#5686).
//
// Async/event-driven handlers whose only trigger is a queue/topic
// subscription carry an OUTBOUND SUBSCRIBES_TO edge (handler → topic) but
// no INBOUND edge, so callers/impact/trace dead-end at the async boundary.
// ApplyAsyncTriggerEdges synthesises a distinct DELIVERS_TO edge
// (topic → handler) that gives the handler an inbound trigger and completes
// the publisher → topic → handler chain without polluting the CALLS graph.
package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// buildAsyncTriggerDoc builds a minimal SNS/SQS-shaped graph:
//
//	Publisher.publish --PUBLISHES_TO--> sqs:orders  <--SUBSCRIBES_TO-- Consumer.onOrder
//
// Both edges point AT the topic, so before the synthesis pass the consumer
// handler has zero inbound edges (the reporter's dead-end).
func buildAsyncTriggerDoc(repo string) *graph.Document {
	doc := &graph.Document{Repo: repo}
	doc.Entities = []graph.Entity{
		{ID: "op:publish", Name: "Publisher.publish", Kind: "SCOPE.Operation", Language: "java", SourceFile: "Publisher.java"},
		{ID: "op:onOrder", Name: "Consumer.onOrder", Kind: "SCOPE.Operation", Language: "java", SourceFile: "Consumer.java"},
		{ID: "topic:orders", Name: "sqs:orders", Kind: "SCOPE.Queue", Language: "java", SourceFile: ""},
	}
	doc.Relationships = []graph.Relationship{
		graph.Relationship{ID: "pub:1", FromID: "op:publish", ToID: "topic:orders", Kind: "PUBLISHES_TO"}.WithProperties(map[string]string{"broker": "sqs"}),
		graph.Relationship{ID: "sub:1", FromID: "op:onOrder", ToID: "topic:orders", Kind: "SUBSCRIBES_TO"}.WithProperties(map[string]string{"broker": "sqs", "messaging_layer": "spring_sqs"}),
	}
	return doc
}

func inboundEdges(doc *graph.Document, toID string) []graph.Relationship {
	var out []graph.Relationship
	for _, r := range doc.Relationships {
		if r.ToID == toID {
			out = append(out, r)
		}
	}
	return out
}

// TestApplyAsyncTriggerEdges_HandlerHasNoInboundBefore documents the pre-fix
// dead-end: the consumer handler has NO inbound edge, so callers/impact/trace
// cannot reach it.
func TestApplyAsyncTriggerEdges_HandlerHasNoInboundBefore(t *testing.T) {
	doc := buildAsyncTriggerDoc("fixture-async")
	if in := inboundEdges(doc, "op:onOrder"); len(in) != 0 {
		t.Fatalf("precondition: consumer handler should have 0 inbound edges before synthesis, got %d: %+v", len(in), in)
	}
}

// TestApplyAsyncTriggerEdges_EmitsDeliversTo asserts the pass synthesises
// exactly one DELIVERS_TO edge topic → handler (giving the handler an inbound
// trigger), and does NOT emit any CALLS edge.
func TestApplyAsyncTriggerEdges_EmitsDeliversTo(t *testing.T) {
	doc := buildAsyncTriggerDoc("fixture-async")
	stats := ApplyAsyncTriggerEdges(doc)
	if stats.DeliversEdges != 1 {
		t.Fatalf("expected 1 DELIVERS_TO edge, got %d", stats.DeliversEdges)
	}

	in := inboundEdges(doc, "op:onOrder")
	if len(in) != 1 {
		t.Fatalf("expected consumer handler to have exactly 1 inbound edge after synthesis, got %d: %+v", len(in), in)
	}
	e := in[0]
	if e.Kind != asyncDeliversToKindLiteral {
		t.Fatalf("inbound edge kind = %q, want DELIVERS_TO", e.Kind)
	}
	if e.FromID != "topic:orders" {
		t.Fatalf("DELIVERS_TO FromID = %q, want topic:orders", e.FromID)
	}
	if e.ToID != "op:onOrder" {
		t.Fatalf("DELIVERS_TO ToID = %q, want op:onOrder", e.ToID)
	}

	// Hard constraint: no CALLS edge synthesised (pure call graph stays clean).
	for _, r := range doc.Relationships {
		if r.Kind == "CALLS" {
			t.Fatalf("pass must not emit CALLS edges; found %+v", r)
		}
	}

	// The publisher → topic → handler chain is now fully connected inbound:
	// handler's inbound is the topic; topic's inbound is the publisher.
	topicIn := inboundEdges(doc, "topic:orders")
	var sawPub bool
	for _, r := range topicIn {
		if r.Kind == "PUBLISHES_TO" && r.FromID == "op:publish" {
			sawPub = true
		}
	}
	if !sawPub {
		t.Fatalf("expected topic to retain inbound PUBLISHES_TO from publisher; inbound=%+v", topicIn)
	}
}

// TestApplyAsyncTriggerEdges_Idempotent verifies re-running the pass does not
// duplicate DELIVERS_TO edges.
func TestApplyAsyncTriggerEdges_Idempotent(t *testing.T) {
	doc := buildAsyncTriggerDoc("fixture-async")
	ApplyAsyncTriggerEdges(doc)
	before := len(doc.Relationships)
	stats := ApplyAsyncTriggerEdges(doc)
	if stats.DeliversEdges != 0 {
		t.Fatalf("second run should synthesise 0 new edges, got %d", stats.DeliversEdges)
	}
	if len(doc.Relationships) != before {
		t.Fatalf("relationship count changed on re-run: before=%d after=%d", before, len(doc.Relationships))
	}
}

// asyncDeliversToKindLiteral mirrors types.RelationshipKindDeliversTo so this
// test stays in the engine package without importing types for one constant.
const asyncDeliversToKindLiteral = "DELIVERS_TO"
