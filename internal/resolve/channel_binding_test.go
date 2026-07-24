package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// buildChannelBindingFixture returns a synthetic entity/rel set modeling the
// SmallRye/Quarkus reference slice (ADR-0025):
//
//   - a healthy outgoing binding "orders-out" -> @Outgoing op + kafka:orders.placed topic
//   - an orphan outgoing binding "ghost-out" with NO matching @Outgoing op
//   - a dangling binding "typo-in" whose topic has no MessageTopic
func buildChannelBindingFixture() ([]types.EntityRecord, []types.RelationshipRecord) {
	entities := []types.EntityRecord{
		{
			ID:   "scope:channelbinding:quarkus_properties:app.properties:outgoing:orders-out",
			Name: "orders-out", Kind: string(types.EntityKindChannelBinding), Subtype: "outgoing",
			Properties: map[string]string{
				"channel": "orders-out", "direction": "outgoing",
				"connector": "smallrye-kafka", "topic": "orders.placed",
			},
		},
		{
			ID:   "op-orders-out-hex",
			Name: "publishOrder", Kind: string(types.EntityKindOperation),
			Properties: map[string]string{"channel": "orders-out", "direction": "outgoing"},
		},
		{
			ID:   "topic-orders-hex",
			Name: "kafka:orders.placed", Kind: string(types.EntityKindMessageTopic),
		},
		// Orphan outgoing: config declares producer channel, no @Outgoing op.
		{
			ID:   "scope:channelbinding:quarkus_properties:app.properties:outgoing:ghost-out",
			Name: "ghost-out", Kind: string(types.EntityKindChannelBinding), Subtype: "outgoing",
			Properties: map[string]string{
				"channel": "ghost-out", "direction": "outgoing", "topic": "ghost.topic",
			},
		},
		{ID: "topic-ghost-hex", Name: "kafka:ghost.topic", Kind: string(types.EntityKindMessageTopic)},
		// Dangling topic: incoming binding wired to a code consumer, but no
		// MessageTopic for its topic name.
		{
			ID:   "scope:channelbinding:quarkus_properties:app.properties:incoming:typo-in",
			Name: "typo-in", Kind: string(types.EntityKindChannelBinding), Subtype: "incoming",
			Properties: map[string]string{
				"channel": "typo-in", "direction": "incoming", "topic": "mis.spelled",
			},
		},
		{
			ID:   "op-typo-in-hex",
			Name: "consume", Kind: string(types.EntityKindOperation),
			Properties: map[string]string{"channel": "typo-in", "direction": "incoming"},
		},
	}
	rels := []types.RelationshipRecord{
		{
			FromID: "scope:channelbinding:quarkus_properties:app.properties:outgoing:orders-out",
			ToID:   "orders-out", Kind: string(types.RelationshipKindBindsChannel),
			Properties: types.Props{{K: "channel", V: "orders-out"}, {K: "direction", V: "outgoing"}},
		},
		{
			FromID: "scope:channelbinding:quarkus_properties:app.properties:outgoing:orders-out",
			ToID:   "kafka:orders.placed", Kind: string(types.RelationshipKindBindsTopic),
			Properties: types.Props{{K: "topic", V: "orders.placed"}},
		},
	}
	return entities, rels
}

// TestResolveChannelBindings_JoinsCodeAndTopic asserts BINDS resolves to the
// @Outgoing Operation and BINDS_TOPIC resolves to the kafka: MessageTopic.
func TestResolveChannelBindings_JoinsCodeAndTopic(t *testing.T) {
	entities, rels := buildChannelBindingFixture()
	n := ResolveChannelBindings(entities, rels)
	if n != 2 {
		t.Fatalf("ResolveChannelBindings rewrote %d edges, want 2", n)
	}
	var binds, bindsTopic *types.RelationshipRecord
	for i := range rels {
		switch rels[i].Kind {
		case string(types.RelationshipKindBindsChannel):
			binds = &rels[i]
		case string(types.RelationshipKindBindsTopic):
			bindsTopic = &rels[i]
		}
	}
	if binds == nil || binds.ToID != "op-orders-out-hex" {
		t.Errorf("BINDS_CHANNEL ToID = %v, want op-orders-out-hex", binds)
	}
	if bindsTopic == nil || bindsTopic.ToID != "topic-orders-hex" {
		t.Errorf("BINDS_TOPIC ToID = %v, want topic-orders-hex", bindsTopic)
	}
}

// TestResolveChannelBindings_DirectionMustAgree asserts an outgoing binding
// does NOT bind to an @Incoming op that happens to share the channel name.
func TestResolveChannelBindings_DirectionMustAgree(t *testing.T) {
	entities := []types.EntityRecord{
		{
			ID: "cb", Name: "shared", Kind: string(types.EntityKindChannelBinding),
			Properties: map[string]string{"channel": "shared", "direction": "outgoing", "topic": "t"},
		},
		{
			ID: "op-incoming", Name: "consume", Kind: string(types.EntityKindOperation),
			Properties: map[string]string{"channel": "shared", "direction": "incoming"},
		},
	}
	rels := []types.RelationshipRecord{{
		FromID: "scope:channelbinding:quarkus_properties:app.properties:outgoing:shared",
		ToID:   "shared", Kind: string(types.RelationshipKindBindsChannel),
		Properties: types.Props{{K: "channel", V: "shared"}, {K: "direction", V: "outgoing"}},
	}}
	if n := ResolveChannelBindings(entities, rels); n != 0 {
		t.Fatalf("rewrote %d edges; outgoing binding must not bind an @Incoming op", n)
	}
	if rels[0].ToID != "shared" {
		t.Errorf("ToID = %q, want unresolved 'shared'", rels[0].ToID)
	}
}

// TestResolveChannelBindings_IgnoresHelmBinds asserts the reused Helm/DI "BINDS"
// kind is never touched by the ChannelBinding resolver — the channel edge is a
// DISTINCT BINDS_CHANNEL kind, so a "BINDS" edge (even coincidentally naming a
// channel) is left untouched.
func TestResolveChannelBindings_IgnoresHelmBinds(t *testing.T) {
	entities := []types.EntityRecord{
		{ID: "op", Name: "x", Kind: string(types.EntityKindOperation),
			Properties: map[string]string{"channel": "orders-out", "direction": "outgoing"}},
	}
	rels := []types.RelationshipRecord{{
		FromID: "helm:values:something", ToID: "orders-out",
		Kind: string(types.RelationshipKindBinds),
	}}
	if n := ResolveChannelBindings(entities, rels); n != 0 {
		t.Fatalf("rewrote %d Helm BINDS edges, want 0", n)
	}
	if rels[0].ToID != "orders-out" {
		t.Errorf("Helm BINDS ToID mutated to %q", rels[0].ToID)
	}
}

// TestHintKinds_BindsChannelNotComponentFamily guards consumer #2: the
// BINDS_CHANNEL kind must NOT map to componentKindFamily, so the generic
// ambiguous-bare-name resolver never biases a channel token toward a component.
func TestHintKinds_BindsChannelNotComponentFamily(t *testing.T) {
	if fams := hintKinds(string(types.RelationshipKindBindsChannel)); fams != nil {
		t.Errorf("hintKinds(BINDS_CHANNEL) = %v, want nil (no component-family bias)", fams)
	}
	// Sanity: the reused Helm/DI "BINDS" kind DOES map to a family — this is
	// exactly the bleed we avoid by using a distinct kind.
	if fams := hintKinds(string(types.RelationshipKindBinds)); len(fams) == 0 {
		t.Error("expected hintKinds(BINDS) to map to a component family (contrast case)")
	}
}

// TestReferencesWithAllowlist_UnresolvedBindsChannelNotSwept guards consumer #2
// end-to-end: an unresolved BINDS_CHANNEL ref (channel with no matching op)
// must NOT be mis-bound by the generic resolver to a component that happens to
// share the channel name — it stays unresolved and is flagged an orphan.
func TestReferencesWithAllowlist_UnresolvedBindsChannelNotSwept(t *testing.T) {
	entities := []types.EntityRecord{
		{
			ID:   "scope:channelbinding:quarkus_properties:app.properties:outgoing:orders-out",
			Name: "orders-out", Kind: string(types.EntityKindChannelBinding), Subtype: "outgoing",
			Properties: map[string]string{
				"channel": "orders-out", "direction": "outgoing", "topic": "orders.placed",
			},
		},
		// A component COINCIDENTALLY named exactly like the channel. Unique, so
		// the generic byName fallback would sweep a bare "orders-out" ref.
		{ID: "component-orders-out-hex", Name: "orders-out", Kind: "SCOPE.Component",
			SourceFile: "src/OrdersOut.java", StartLine: 10, EndLine: 20},
	}
	rels := []types.RelationshipRecord{{
		FromID: "scope:channelbinding:quarkus_properties:app.properties:outgoing:orders-out",
		ToID:   "orders-out", Kind: string(types.RelationshipKindBindsChannel),
		Properties: types.Props{{K: "channel", V: "orders-out"}, {K: "direction", V: "outgoing"}},
	}}

	// Dedicated pass first (no matching op -> leaves ToID unresolved).
	if n := ResolveChannelBindings(entities, rels); n != 0 {
		t.Fatalf("ResolveChannelBindings rewrote %d, want 0 (no @Outgoing op)", n)
	}
	// Generic resolver must not sweep it despite the unique same-named component.
	idx := BuildIndex(entities)
	ReferencesWithAllowlist(rels, idx, nil)
	if rels[0].ToID != "orders-out" {
		t.Fatalf("BINDS_CHANNEL ToID = %q, want unresolved 'orders-out' (must NOT bind component-orders-out-hex)", rels[0].ToID)
	}

	// And it is flagged as an orphan-outgoing binding.
	orphans := DetectChannelBindingOrphans(entities)
	found := false
	for _, o := range orphans {
		if o.Channel == "orders-out" && o.Kind == OrphanOutgoing {
			found = true
		}
	}
	if !found {
		t.Errorf("expected orphan-outgoing for orders-out; got %+v", orphans)
	}
}

// TestDetectChannelBindingOrphans_RuntimeDynamicPlaceholderNotDangling guards
// #5782 NOTE(ii): a binding whose topic falls back to the channel and whose
// engine topic node is the runtime-dynamic placeholder "kafka:channel:<channel>"
// must NOT be flagged dangling-topic.
func TestDetectChannelBindingOrphans_RuntimeDynamicPlaceholderNotDangling(t *testing.T) {
	entities := []types.EntityRecord{
		{
			ID:   "scope:channelbinding:quarkus_properties:app.properties:outgoing:events-out",
			Name: "events-out", Kind: string(types.EntityKindChannelBinding), Subtype: "outgoing",
			Properties: map[string]string{
				"channel": "events-out", "direction": "outgoing",
				// SmallRye default: topic unset -> falls back to channel.
				"topic": "events-out",
			},
		},
		{ID: "op-events", Name: "emit", Kind: string(types.EntityKindOperation),
			Properties: map[string]string{"channel": "events-out", "direction": "outgoing"}},
		// Engine runtime-dynamic placeholder: Name == "kafka:channel:<channel>".
		{ID: "kafka:channel:events-out", Name: "kafka:channel:events-out",
			Kind:       string(types.EntityKindMessageTopic),
			Properties: map[string]string{"runtime_dynamic": "true"}},
	}
	for _, o := range DetectChannelBindingOrphans(entities) {
		if o.Kind == DanglingTopic {
			t.Errorf("runtime-dynamic placeholder wrongly flagged dangling-topic: %+v", o)
		}
	}
}

// TestDetectChannelBindingOrphans flags the orphan-outgoing and dangling-topic
// bindings while leaving the healthy binding un-flagged.
func TestDetectChannelBindingOrphans(t *testing.T) {
	entities, _ := buildChannelBindingFixture()
	orphans := DetectChannelBindingOrphans(entities)

	got := map[string]ChannelBindingOrphanKind{}
	for _, o := range orphans {
		got[o.Channel] = o.Kind
	}
	if _, ok := got["orders-out"]; ok {
		t.Errorf("healthy binding orders-out wrongly flagged: %v", got["orders-out"])
	}
	if got["ghost-out"] != OrphanOutgoing {
		t.Errorf("ghost-out flag = %q, want orphan-outgoing", got["ghost-out"])
	}
	if got["typo-in"] != DanglingTopic {
		t.Errorf("typo-in flag = %q, want dangling-topic", got["typo-in"])
	}
}
