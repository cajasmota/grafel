package config

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// findChannelBinding returns the first SCOPE.ChannelBinding entity matching
// (direction, channel).
func findChannelBinding(es []types.EntityRecord, direction, channel string) *types.EntityRecord {
	for i := range es {
		e := &es[i]
		if e.Kind != string(types.EntityKindChannelBinding) {
			continue
		}
		if e.Properties["direction"] == direction && e.Properties["channel"] == channel {
			return e
		}
	}
	return nil
}

// relsFrom returns every relationship whose FromID matches and Kind matches.
func relsFrom(rs []types.RelationshipRecord, fromID, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range rs {
		if r.FromID == fromID && r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

// TestDiscover_ChannelBinding_Properties asserts the recognizer emits one
// SCOPE.ChannelBinding per mp.messaging.<dir>.<channel> group with the right
// captured props, and the BINDS / BINDS_TOPIC structural refs (ADR-0025 §1.2).
func TestDiscover_ChannelBinding_Properties(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "src/main/resources/application.properties", `
mp.messaging.outgoing.orders-out.connector=smallrye-kafka
mp.messaging.outgoing.orders-out.topic=orders.placed
mp.messaging.outgoing.orders-out.value.serializer=io.quarkus.kafka.client.serialization.ObjectMapperSerializer
mp.messaging.incoming.payments-in.connector=smallrye-kafka
# payments-in has no explicit topic -> SmallRye default falls back to channel
`)
	ents, rels := runDiscover(t, dir, []string{"src/main/resources/application.properties"})

	out := findChannelBinding(ents, "outgoing", "orders-out")
	if out == nil {
		t.Fatalf("expected outgoing/orders-out ChannelBinding; got kinds %v", kindsOf(ents))
	}
	if got := out.Properties["connector"]; got != "smallrye-kafka" {
		t.Errorf("connector = %q, want smallrye-kafka", got)
	}
	if got := out.Properties["topic"]; got != "orders.placed" {
		t.Errorf("topic = %q, want orders.placed", got)
	}
	if got := out.Properties["serializer"]; got != "io.quarkus.kafka.client.serialization.ObjectMapperSerializer" {
		t.Errorf("serializer = %q, unexpected", got)
	}
	if got := out.Properties["source_config"]; got != "src/main/resources/application.properties" {
		t.Errorf("source_config = %q", got)
	}
	if out.StartLine != 1 || out.EndLine != 1 {
		t.Errorf("StartLine/EndLine = %d/%d, want 1/1", out.StartLine, out.EndLine)
	}
	wantID := "scope:channelbinding:spring_properties:src/main/resources/application.properties:outgoing:orders-out"
	if out.ID != wantID {
		t.Errorf("ID = %q, want %q", out.ID, wantID)
	}

	// Topic falls back to the channel name when unset.
	in := findChannelBinding(ents, "incoming", "payments-in")
	if in == nil {
		t.Fatal("expected incoming/payments-in ChannelBinding")
	}
	if got := in.Properties["topic"]; got != "payments-in" {
		t.Errorf("fallback topic = %q, want payments-in (channel default)", got)
	}

	// BINDS_CHANNEL ref → channel value; BINDS_TOPIC ref → kafka:<topic>.
	binds := relsFrom(rels, out.ID, string(types.RelationshipKindBindsChannel))
	if len(binds) != 1 || binds[0].ToID != "orders-out" {
		t.Fatalf("BINDS_CHANNEL refs = %+v, want single ToID=orders-out", binds)
	}
	if binds[0].Properties.Get("direction") != "outgoing" {
		t.Errorf("BINDS_CHANNEL direction prop = %q", binds[0].Properties.Get("direction"))
	}
	// The channel edge must NOT reuse the Helm/DI "BINDS" kind (semantic bleed).
	if bleed := relsFrom(rels, out.ID, string(types.RelationshipKindBinds)); len(bleed) != 0 {
		t.Errorf("channel edge must not be emitted as BINDS (DI kind); got %+v", bleed)
	}
	bt := relsFrom(rels, out.ID, string(types.RelationshipKindBindsTopic))
	if len(bt) != 1 || bt[0].ToID != "kafka:orders.placed" {
		t.Fatalf("BINDS_TOPIC refs = %+v, want single ToID=kafka:orders.placed", bt)
	}
}

// TestDiscover_ChannelBinding_NotEmittedForNonMessagingConfig asserts the
// recognizer stays silent on config subtypes outside the messaging gate.
func TestDiscover_ChannelBinding_NonMessagingSubtype(t *testing.T) {
	dir := t.TempDir()
	// go.mod is not a messaging subtype; even a stray mp.messaging line (it
	// won't appear here) must not synthesize a ChannelBinding.
	writeFixture(t, dir, "go.mod", "module example.com/x\n\ngo 1.22\n")
	ents, _ := runDiscover(t, dir, []string{"go.mod"})
	for i := range ents {
		if ents[i].Kind == string(types.EntityKindChannelBinding) {
			t.Fatalf("unexpected ChannelBinding from non-messaging config: %+v", ents[i])
		}
	}
}

func kindsOf(es []types.EntityRecord) []string {
	out := make([]string, 0, len(es))
	for i := range es {
		out = append(out, es[i].Kind)
	}
	return out
}
