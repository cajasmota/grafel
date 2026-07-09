package mcp

// Async-trigger caller/impact/trace surfacing (#5686).
//
// An async/event-driven handler whose only trigger is a queue/topic
// subscription previously had ZERO inbound edges: find_callers, impact_radius
// and trace all dead-ended at the async boundary. engine.ApplyAsyncTriggerEdges
// synthesises a DELIVERS_TO edge (topic → handler); these tests assert the MCP
// read tools traverse it.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// buildAsyncTriggerGraph mirrors the engine fixture but as a graph the MCP
// server can load. The DELIVERS_TO edge is what ApplyAsyncTriggerEdges emits:
//
//	Publisher.publish --PUBLISHES_TO--> sqs:orders --DELIVERS_TO--> Consumer.onOrder
//	Consumer.onOrder  --SUBSCRIBES_TO--> sqs:orders
func buildAsyncTriggerGraph(withDelivers bool) *graph.Document {
	rels := []graph.Relationship{
		{ID: "pub:1", FromID: "op:publish", ToID: "topic:orders", Kind: "PUBLISHES_TO"},
		{ID: "sub:1", FromID: "op:onOrder", ToID: "topic:orders", Kind: "SUBSCRIBES_TO"},
	}
	if withDelivers {
		rels = append(rels, graph.Relationship{
			ID: "del:1", FromID: "topic:orders", ToID: "op:onOrder", Kind: "DELIVERS_TO",
			Properties: map[string]string{"trigger": "async", "broker": "sqs"},
		})
	}
	return minDoc(
		[]graph.Entity{
			{ID: "op:publish", Name: "Publisher.publish", Kind: "SCOPE.Operation", SourceFile: "pub.java", StartLine: 10, QualifiedName: "Publisher.publish"},
			{ID: "op:onOrder", Name: "Consumer.onOrder", Kind: "SCOPE.Operation", SourceFile: "consumer.java", StartLine: 20, QualifiedName: "Consumer.onOrder"},
			{ID: "topic:orders", Name: "sqs:orders", Kind: "SCOPE.Queue", SourceFile: ""},
		},
		rels,
	)
}

// TestAsyncTrigger_FindCallers_DeadEndBeforeSynthesis is the RED assertion:
// without the DELIVERS_TO edge the handler has no callers.
func TestAsyncTrigger_FindCallers_DeadEndBeforeSynthesis(t *testing.T) {
	srv := newTestServer(t, buildAsyncTriggerGraph(false))
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "op:onOrder",
		"depth":     float64(2),
	})
	callers, _ := out["callers"].([]any)
	if len(callers) != 0 {
		t.Fatalf("pre-synthesis: expected async handler to have 0 callers (dead-end), got %d: %+v", len(callers), callers)
	}
}

// TestAsyncTrigger_FindCallers_SurfacesTopicAndPublisher is the GREEN
// assertion: with DELIVERS_TO, find_callers reaches the topic (depth 1) and the
// publisher (depth 2, via the topic's inbound PUBLISHES_TO).
func TestAsyncTrigger_FindCallers_SurfacesTopicAndPublisher(t *testing.T) {
	srv := newTestServer(t, buildAsyncTriggerGraph(true))
	out := callFlowTool(t, srv.handleFindCallers, map[string]any{
		"entity_id": "op:onOrder",
		"depth":     float64(2),
	})
	callers, _ := out["callers"].([]any)
	names := map[string]bool{}
	for _, c := range callers {
		m := c.(map[string]any)
		names[m["name"].(string)] = true
	}
	if !names["sqs:orders"] {
		t.Errorf("expected topic sqs:orders as an inbound trigger (caller), got %+v", names)
	}
	if !names["Publisher.publish"] {
		t.Errorf("expected publisher Publisher.publish reachable via topic at depth 2, got %+v", names)
	}
}

// TestAsyncTrigger_ImpactRadius_CrossesAsyncBoundary asserts impact_radius no
// longer dead-ends at the async handler. impact_radius walks INBOUND (what is
// affected if this entity changes); before synthesis the handler had zero
// inbound edges and returned an empty blast radius. With DELIVERS_TO the walk
// crosses the async boundary into the topic and (transitively) the publisher.
func TestAsyncTrigger_ImpactRadius_CrossesAsyncBoundary(t *testing.T) {
	// Before: empty blast radius (dead-end).
	srvBefore := newTestServer(t, buildAsyncTriggerGraph(false))
	outBefore := callFlowTool(t, srvBefore.handleImpactRadius, map[string]any{
		"entity_id": "op:onOrder",
		"hops":      float64(3),
	})
	if before, _ := outBefore["affected"].([]any); len(before) != 0 {
		t.Fatalf("pre-synthesis: async handler impact_radius should be empty (dead-end), got %+v", before)
	}

	// After: reaches the topic and the publisher across the async boundary.
	srv := newTestServer(t, buildAsyncTriggerGraph(true))
	out := callFlowTool(t, srv.handleImpactRadius, map[string]any{
		"entity_id": "op:onOrder",
		"hops":      float64(3),
	})
	affected, _ := out["affected"].([]any)
	names := map[string]bool{}
	for _, a := range affected {
		m := a.(map[string]any)
		names[m["name"].(string)] = true
	}
	if !names["sqs:orders"] {
		t.Errorf("impact_radius on async handler should surface topic sqs:orders across the boundary; got %+v", names)
	}
	if !names["Publisher.publish"] {
		t.Errorf("impact_radius on async handler should reach publisher transitively; got %+v", names)
	}
}

// TestAsyncTrigger_Trace_PublisherToHandler asserts a shortest-path trace from
// the publisher to the handler completes through topic --DELIVERS_TO--> handler.
func TestAsyncTrigger_Trace_PublisherToHandler(t *testing.T) {
	srv := newTestServer(t, buildAsyncTriggerGraph(true))
	out := callFlowTool(t, srv.handleShortestPath, map[string]any{
		"source": "op:publish",
		"target": "op:onOrder",
	})
	if found, _ := out["found"].(bool); !found {
		t.Fatalf("trace publisher→handler should complete via DELIVERS_TO; got not found: %+v", out)
	}
}
