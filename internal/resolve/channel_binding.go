// channel_binding.go — late-binding resolver + orphan detection for the
// #5782 (ADR-0025) messaging config↔code↔topic join.
//
// The config-discovery ChannelBinding recognizer emits, per
// mp.messaging.{incoming,outgoing}.<channel> group:
//
//   - a SCOPE.ChannelBinding entity carrying channel/direction/connector/
//     topic/serializer props, and
//   - two unresolved structural refs:
//     BINDS_CHANNEL ToID = <channel value>          (→ SCOPE.Operation)
//     BINDS_TOPIC   ToID = "kafka:" + <topic value> (→ SCOPE.MessageTopic)
//
// The generic stub resolver cannot rebind these: BINDS_CHANNEL joins on the
// `channel` PROPERTY of an @Incoming/@Outgoing Operation (not on any indexed
// name/kind), and BINDS_TOPIC joins on the MessageTopic Name string. Both are
// DISTINCT kinds (BINDS_CHANNEL is NOT the reused Helm/DI "BINDS") so an
// unresolved channel ref is never swept by hintKinds→componentKindFamily and
// never surfaced as a dependency-injection edge. This
// file provides the dedicated pass (ResolveChannelBindings) that rewrites the
// matched ToIDs to real entity IDs, plus DetectChannelBindingOrphans which
// re-derives the same joins directly from the entity set to flag
// producer-less / consumer-less / dangling-topic bindings (§1.4).
//
// ResolveChannelBindings runs AFTER entity IDs are populated and BEFORE the
// generic reference resolver, mirroring the ordering of the Django/Go/Rust
// late-binding passes in cmd/grafel/index.go.
package resolve

import (
	"sort"

	"github.com/cajasmota/grafel/internal/types"
)

// channelBindingIDPrefix is the ID prefix of SCOPE.ChannelBinding entities /
// the FromID of their BINDS_CHANNEL / BINDS_TOPIC edges. Kept as a defensive
// gate on the resolver even though both edge kinds are now ChannelBinding-only.
const channelBindingIDPrefix = "scope:channelbinding:"

// opChannelKey is the (channel, direction) match key for reactive-messaging
// Operations and ChannelBindings.
func opChannelKey(channel, direction string) string {
	return channel + "\x00" + direction
}

// buildOperationChannelIndex maps (channel, direction) → Operation entity ID
// for SCOPE.Operation entities that carry both a `channel` and a `direction`
// property (stamped by ExtractMicroProfile). A blank-string sentinel marks an
// ambiguous (channel, direction) pair so the resolver never mis-binds — a
// duplicate is itself a modeling error the orphan scan surfaces (§6.3).
func buildOperationChannelIndex(entities []types.EntityRecord) map[string]string {
	idx := make(map[string]string)
	for i := range entities {
		e := &entities[i]
		if !isOperationKind(e.Kind) || e.Properties == nil || e.ID == "" {
			continue
		}
		ch, dir := e.Properties["channel"], e.Properties["direction"]
		if ch == "" || dir == "" {
			continue
		}
		k := opChannelKey(ch, dir)
		if existing, ok := idx[k]; ok && existing != e.ID {
			idx[k] = "" // ambiguous sentinel
			continue
		}
		if _, ok := idx[k]; !ok {
			idx[k] = e.ID
		}
	}
	return idx
}

// buildMessageTopicIndex maps a SCOPE.MessageTopic Name (e.g. "kafka:orders.placed")
// → entity ID. Blank-string sentinel marks an ambiguous name.
func buildMessageTopicIndex(entities []types.EntityRecord) map[string]string {
	idx := make(map[string]string)
	for i := range entities {
		e := &entities[i]
		if e.Kind != string(types.EntityKindMessageTopic) || e.Name == "" || e.ID == "" {
			continue
		}
		if existing, ok := idx[e.Name]; ok && existing != e.ID {
			idx[e.Name] = ""
			continue
		}
		if _, ok := idx[e.Name]; !ok {
			idx[e.Name] = e.ID
		}
	}
	return idx
}

// ResolveChannelBindings rewrites the unresolved ToID of every BINDS_CHANNEL /
// BINDS_TOPIC edge originating from a ChannelBinding to the matching entity
// ID. BINDS_CHANNEL joins by (channel, direction) against reactive-messaging
// Operations; BINDS_TOPIC joins by the "kafka:<topic>" MessageTopic Name.
//
// Edges that do not match are LEFT UNRESOLVED (their config-side ToID stays
// intact) — they feed DetectChannelBindingOrphans. Returns the number of ToIDs
// rewritten. Mutates rels in place.
func ResolveChannelBindings(entities []types.EntityRecord, rels []types.RelationshipRecord) int {
	opIdx := buildOperationChannelIndex(entities)
	topicIdx := buildMessageTopicIndex(entities)
	rewrites := 0
	for i := range rels {
		r := &rels[i]
		// Gate strictly to ChannelBinding-sourced edges (defensive; both kinds
		// below are ChannelBinding-only).
		if len(r.FromID) < len(channelBindingIDPrefix) || r.FromID[:len(channelBindingIDPrefix)] != channelBindingIDPrefix {
			continue
		}
		if r.ToID == "" || isHexID(r.ToID) {
			continue
		}
		switch r.Kind {
		case string(types.RelationshipKindBindsChannel):
			channel := r.ToID
			direction := ""
			if r.Properties != nil {
				if c := r.Properties.Get("channel"); c != "" {
					channel = c
				}
				direction = r.Properties.Get("direction")
			}
			if id := opIdx[opChannelKey(channel, direction)]; id != "" {
				r.ToID = id
				rewrites++
			}
		case string(types.RelationshipKindBindsTopic):
			if id := topicIdx[r.ToID]; id != "" {
				r.ToID = id
				rewrites++
			}
		}
	}
	return rewrites
}

// ChannelBindingOrphanKind enumerates the #5782 orphan classes (§1.4).
type ChannelBindingOrphanKind string

const (
	// OrphanOutgoing: an outgoing binding (config declares a producer channel)
	// whose channel has no @Outgoing Operation — no code publishes to it.
	OrphanOutgoing ChannelBindingOrphanKind = "orphan-outgoing"
	// OrphanIncoming: symmetric — an incoming binding with no @Incoming consumer.
	OrphanIncoming ChannelBindingOrphanKind = "orphan-incoming"
	// DanglingTopic: the BINDS_TOPIC target has no synthesized SCOPE.MessageTopic
	// (typo / external topic the engine never saw).
	DanglingTopic ChannelBindingOrphanKind = "dangling-topic"
)

// ChannelBindingOrphan is one flagged binding defect. A single binding may be
// reported under multiple kinds (e.g. both orphan-outgoing and dangling-topic).
type ChannelBindingOrphan struct {
	BindingID string                   `json:"binding_id"`
	Channel   string                   `json:"channel"`
	Direction string                   `json:"direction"`
	Topic     string                   `json:"topic"`
	Kind      ChannelBindingOrphanKind `json:"kind"`
}

// DetectChannelBindingOrphans re-derives the BINDS / BINDS_TOPIC joins from the
// entity set and returns the orphan/dangling flags (§1.4). It is independent
// of edge-rewriting so it is deterministic regardless of resolver ordering.
// Output is sorted by (BindingID, Kind).
func DetectChannelBindingOrphans(entities []types.EntityRecord) []ChannelBindingOrphan {
	opIdx := buildOperationChannelIndex(entities)
	topicIdx := buildMessageTopicIndex(entities)

	var out []ChannelBindingOrphan
	for i := range entities {
		e := &entities[i]
		if e.Kind != string(types.EntityKindChannelBinding) || e.Properties == nil {
			continue
		}
		channel := e.Properties["channel"]
		direction := e.Properties["direction"]
		topic := e.Properties["topic"]

		// Producer-less / consumer-less: no Operation with this (channel,
		// direction). A blank sentinel (ambiguous duplicate op) counts as
		// resolved — the duplication is a separate signal, not an orphan.
		if _, matched := opIdx[opChannelKey(channel, direction)]; !matched {
			kind := OrphanIncoming
			if direction == "outgoing" {
				kind = OrphanOutgoing
			}
			out = append(out, ChannelBindingOrphan{
				BindingID: e.ID, Channel: channel, Direction: direction, Topic: topic, Kind: kind,
			})
		}

		// Dangling topic: no MessageTopic for this topic. The canonical node
		// is Name == "kafka:<topic>". #5782 NOTE(ii): the engine
		// (internal/engine/kafka_edges.go) emits a runtime-dynamic FALLBACK
		// placeholder whose Name is "kafka:channel:<channel>" when it can't
		// resolve the topic; in the SmallRye default (topic == channel) that
		// placeholder IS the topic node, so accept it too rather than raising a
		// spurious dangling-topic. Only the dangling scan is relaxed — the
		// BINDS_TOPIC edge target itself is left unchanged.
		_, matched := topicIdx["kafka:"+topic]
		if !matched {
			_, matched = topicIdx["kafka:channel:"+topic]
		}
		if !matched {
			out = append(out, ChannelBindingOrphan{
				BindingID: e.ID, Channel: channel, Direction: direction, Topic: topic, Kind: DanglingTopic,
			})
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].BindingID != out[b].BindingID {
			return out[a].BindingID < out[b].BindingID
		}
		return out[a].Kind < out[b].Kind
	})
	return out
}
