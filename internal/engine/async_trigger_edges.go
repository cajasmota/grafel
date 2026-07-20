// Async-trigger edge synthesis (#5686).
//
// Async/event-driven handlers whose only trigger is a queue/topic subscription
// carry an OUTBOUND SUBSCRIBES_TO edge (handler → topic) but no INBOUND edge.
// As a result find_callers / impact_radius / trace all dead-end at the async
// boundary: the handler looks like an orphan with zero production callers even
// though it is triggered on every message delivered to the topic/queue it
// subscribes to (SNS+SQS @SqsListener handlers, Go-Lambda SQS receive loops,
// NATS subscribers, etc.).
//
// ApplyAsyncTriggerEdges is a PROJECT-SCOPE, APPEND-ONLY post-pass that runs
// over the already-extracted topics + subscription edges. For every consumer
// handler that SUBSCRIBES_TO a topic/queue, it emits the directional inverse —
// a distinct DELIVERS_TO edge (topic → handler) — giving the handler an inbound
// trigger edge. Combined with the existing inbound PUBLISHES_TO edge on the
// topic, this completes the publisher → topic → handler chain so callers /
// impact / trace can walk from a publisher (or the topic) into the handler.
//
// Design constraints:
//   - Distinct kind, never CALLS. The pure call graph (process-flow BFS,
//     calls/called_by) must stay clean. DELIVERS_TO surfaces only in the
//     inbound/impact/trace/semantic-edge facets that already accept non-CALLS
//     semantic edges.
//   - No new extraction. The pass reads existing SUBSCRIBES_TO edges + topic
//     entities; a handler with no SUBSCRIBES_TO edge is out of scope (nothing
//     to invert).
//   - Append-only + idempotent. Re-running never duplicates an edge and never
//     modifies or removes existing entities/edges.
//   - Intra-repo only. The cross-repo publisher↔subscriber join lives in the
//     links topic pass (P7); this pass closes the SAME-repo async boundary that
//     P7 skips (it requires ≥2 graphs).
package engine

import (
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// AsyncTriggerStats summarises one ApplyAsyncTriggerEdges run.
type AsyncTriggerStats struct {
	// Topics is the number of distinct topic/queue entities that had ≥1
	// inbound SUBSCRIBES_TO handler.
	Topics int
	// DeliversEdges is the number of DELIVERS_TO edges newly synthesised.
	DeliversEdges int
}

// asyncTriggerTopicKinds are the entity kinds that represent an async
// delivery channel: a handler that SUBSCRIBES_TO one of these is triggered by
// message delivery. Matched case-insensitively. Covers the broker MessageTopic
// synthetics (kafka/sns/nats/pulsar/eventbridge/…), the SQS/RabbitMQ Queue
// synthetics, the managed event-bus event kind, and datastores that emit a
// change stream (DynamoDB Streams / Kinesis-backed tables) which a Lambda
// EventSourceMapping consumes — a table's stream is an async delivery source
// exactly like a queue (#5801 Bug 2).
var asyncTriggerTopicKinds = map[string]bool{
	strings.ToUpper("SCOPE.MessageTopic"):  true,
	strings.ToUpper("SCOPE.Queue"):         true,
	strings.ToUpper("SCOPE.EventBusEvent"): true,
	strings.ToUpper("SCOPE.Datastore"):     true,
}

func isAsyncTriggerTopicKind(kind string) bool {
	return asyncTriggerTopicKinds[strings.ToUpper(kind)]
}

// ApplyAsyncTriggerEdges synthesises DELIVERS_TO edges (topic → handler) for
// every SUBSCRIBES_TO edge (handler → topic) whose target is a known async
// topic/queue entity in `doc`. Edges are appended to doc in place. Safe to call
// on a doc with no subscription edges (returns an empty stats record).
func ApplyAsyncTriggerEdges(doc *graph.Document) AsyncTriggerStats {
	stats := AsyncTriggerStats{}
	if doc == nil {
		return stats
	}

	// Index topic/queue entities by ID so we only invert subscriptions whose
	// target is a real async channel (never a stray SUBSCRIBES_TO into a
	// non-topic node).
	topicIDs := make(map[string]bool)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if isAsyncTriggerTopicKind(e.Kind) {
			topicIDs[e.ID] = true
		}
	}
	if len(topicIDs) == 0 {
		return stats
	}

	// Pre-record every existing DELIVERS_TO edge so re-runs are idempotent and
	// duplicate SUBSCRIBES_TO edges (same handler+topic) collapse to one.
	const deliversKind = string(types.RelationshipKindDeliversTo)
	existing := make(map[string]bool)
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if strings.EqualFold(r.Kind, deliversKind) {
			existing[r.FromID+"\x00"+r.ToID] = true
		}
	}

	// Collect (topic, handler) pairs from SUBSCRIBES_TO edges, deduped. Sorted
	// for deterministic emission order.
	type pair struct {
		topicID   string
		handlerID string
		props     map[string]string
	}
	seenPair := make(map[string]bool)
	topicsWithSub := make(map[string]bool)
	var pairs []pair
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if !strings.EqualFold(r.Kind, string(types.RelationshipKindSubscribesTo)) {
			continue
		}
		if r.FromID == "" || r.ToID == "" {
			continue
		}
		// SUBSCRIBES_TO points handler(From) → topic(To). Only invert when the
		// target is a recognised async topic/queue entity.
		if !topicIDs[r.ToID] {
			continue
		}
		key := r.ToID + "\x00" + r.FromID
		if seenPair[key] {
			continue
		}
		seenPair[key] = true
		topicsWithSub[r.ToID] = true
		pairs = append(pairs, pair{topicID: r.ToID, handlerID: r.FromID, props: r.PropsSnapshot()})
	}
	stats.Topics = len(topicsWithSub)

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].topicID != pairs[j].topicID {
			return pairs[i].topicID < pairs[j].topicID
		}
		return pairs[i].handlerID < pairs[j].handlerID
	})

	for _, p := range pairs {
		dedupKey := p.topicID + "\x00" + p.handlerID
		if existing[dedupKey] {
			continue
		}
		existing[dedupKey] = true

		props := map[string]string{
			"pattern_type": "async_trigger_synthesis",
			"trigger":      "async",
			"synthesized":  "true",
		}
		// Carry broker / messaging_layer context from the originating
		// SUBSCRIBES_TO edge so the trigger reads coherently in inspect/expand.
		for _, k := range []string{"broker", "messaging_layer", "queue_url", "queue_name"} {
			if v, ok := p.props[k]; ok && v != "" {
				props[k] = v
			}
		}

		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     graph.RelationshipID(p.topicID, p.handlerID, deliversKind),
			FromID: p.topicID,
			ToID:   p.handlerID,
			Kind:   deliversKind,
		}.WithProperties(props))
		stats.DeliversEdges++
	}

	return stats
}
