package dashboard

// channelbinding_topology_test.go — #5782 (ADR-0025) topology surfacing of
// ChannelBinding orphan flags.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

func cbEntity(id, channel, direction, topic string) graph.Entity {
	return graph.Entity{
		ID: id, Name: channel, Kind: string(types.EntityKindChannelBinding),
		Subtype: direction,
	}.WithProperties(map[string]string{
		"channel": channel, "direction": direction, "topic": topic,
	},
	)
}

// TestCollectChannelBindingOrphans surfaces orphan-outgoing and dangling-topic
// while a fully-wired binding produces no flag.
func TestCollectChannelBindingOrphans(t *testing.T) {
	ents := []graph.Entity{
		// Healthy: op + topic present.
		cbEntity("cb-ok", "orders-out", "outgoing", "orders.placed"),
		graph.Entity{ID: "op-ok", Name: "publish", Kind: string(types.EntityKindOperation)}.WithProperties(map[string]string{"channel": "orders-out", "direction": "outgoing"}),
		{ID: "topic-ok", Name: "kafka:orders.placed", Kind: string(types.EntityKindMessageTopic)},
		// Orphan outgoing: topic exists, no @Outgoing op.
		cbEntity("cb-orphan", "ghost-out", "outgoing", "ghost.topic"),
		{ID: "topic-ghost", Name: "kafka:ghost.topic", Kind: string(types.EntityKindMessageTopic)},
		// Dangling: op exists, topic MessageTopic missing.
		cbEntity("cb-dangling", "typo-in", "incoming", "mis.spelled"),
		graph.Entity{ID: "op-typo", Name: "consume", Kind: string(types.EntityKindOperation)}.WithProperties(map[string]string{"channel": "typo-in", "direction": "incoming"}),
	}
	grp := &DashGroup{
		Name: "g",
		Repos: map[string]*DashRepo{
			"backend": {Slug: "backend", Path: "/tmp/x", Doc: &graph.Document{Repo: "backend", Entities: ents}},
		},
	}

	rows := collectChannelBindingOrphans(grp)
	byChannel := map[string]string{}
	for _, r := range rows {
		byChannel[r["channel"].(string)] = r["kind"].(string)
	}
	if _, ok := byChannel["orders-out"]; ok {
		t.Errorf("healthy binding wrongly flagged: %v", byChannel["orders-out"])
	}
	if byChannel["ghost-out"] != "orphan-outgoing" {
		t.Errorf("ghost-out = %q, want orphan-outgoing", byChannel["ghost-out"])
	}
	if byChannel["typo-in"] != "dangling-topic" {
		t.Errorf("typo-in = %q, want dangling-topic", byChannel["typo-in"])
	}
}
