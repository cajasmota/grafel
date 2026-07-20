// Tests for the Phase-1 EventFlow walker (#1944).
package engine

import (
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// fixtureBuilder is a tiny DSL for assembling pub/sub graphs in tests.
// Channels, operations, CALLS / PUBLISHES_TO / SUBSCRIBES_TO edges are
// all addressed by short test-local IDs (e.g. "topic.A", "svc.handleA").
type fixtureBuilder struct {
	doc *graph.Document
}

func newFixture(repo string) *fixtureBuilder {
	return &fixtureBuilder{doc: &graph.Document{Repo: repo}}
}

func (b *fixtureBuilder) channel(id, name string) *fixtureBuilder {
	b.doc.Entities = append(b.doc.Entities, graph.Entity{
		ID:   id,
		Name: name,
		Kind: eventFlowChannelKindMessageTopic,
	})
	return b
}

func (b *fixtureBuilder) eventBus(id, name string) *fixtureBuilder {
	b.doc.Entities = append(b.doc.Entities, graph.Entity{
		ID:   id,
		Name: name,
		Kind: eventFlowChannelKindEventBus,
	})
	return b
}

func (b *fixtureBuilder) op(id, name string) *fixtureBuilder {
	b.doc.Entities = append(b.doc.Entities, graph.Entity{
		ID:         id,
		Name:       name,
		Kind:       "SCOPE.Function",
		Language:   "go",
		SourceFile: "main.go",
	})
	return b
}

func (b *fixtureBuilder) calls(from, to string) *fixtureBuilder {
	b.doc.Relationships = append(b.doc.Relationships, graph.Relationship{
		ID: "call:" + from + "->" + to, FromID: from, ToID: to, Kind: "CALLS",
	})
	return b
}

func (b *fixtureBuilder) publishes(op, channel string) *fixtureBuilder {
	b.doc.Relationships = append(b.doc.Relationships, graph.Relationship{
		ID: "pub:" + op + "->" + channel, FromID: op, ToID: channel, Kind: "PUBLISHES_TO",
	})
	return b
}

func (b *fixtureBuilder) subscribes(op, channel string) *fixtureBuilder {
	b.doc.Relationships = append(b.doc.Relationships, graph.Relationship{
		ID: "sub:" + op + "->" + channel, FromID: op, ToID: channel, Kind: "SUBSCRIBES_TO",
	})
	return b
}

// firstEventFlow returns the first EventFlow entity emitted into doc,
// or nil if none is present.
func firstEventFlow(doc *graph.Document) *graph.Entity {
	for i := range doc.Entities {
		if doc.Entities[i].Kind == EntityKindEventFlow {
			return &doc.Entities[i]
		}
	}
	return nil
}

func countEventFlows(doc *graph.Document) int {
	n := 0
	for _, e := range doc.Entities {
		if e.Kind == EntityKindEventFlow {
			n++
		}
	}
	return n
}

// --- linear single-channel: pub → channel → sub ---
func TestEventFlow_LinearPubSub(t *testing.T) {
	// publisher publishes to topic.A; subscriber consumes from topic.A.
	// Phase 1 walker seeds from topic.A and emits a flow:
	//   topic.A → sub.handler
	// (no downstream channel, so this is the terminal shape.)
	b := newFixture("r").
		channel("topic.A", "kafka:payments.settled").
		op("svc.publisher", "publishPayment").
		op("svc.subscriber", "handlePayment").
		publishes("svc.publisher", "topic.A").
		subscribes("svc.subscriber", "topic.A")

	cfg := DefaultEventFlowConfig()
	cfg.MinSteps = 2
	stats := RunEventFlow(b.doc, cfg)

	if stats.SeedChannels != 1 {
		t.Fatalf("want 1 seed channel, got %d", stats.SeedChannels)
	}
	if stats.EventFlows == 0 {
		t.Fatalf("expected at least 1 EventFlow, got 0")
	}
	ef := firstEventFlow(b.doc)
	if ef == nil {
		t.Fatalf("no EventFlow entity emitted")
	}
	chain := strings.Split(ef.PropGet("chain"), ",")
	// Minimum-shape chain is exactly [topic.A, svc.subscriber].
	if len(chain) < 2 || chain[0] != "topic.A" || chain[1] != "svc.subscriber" {
		t.Errorf("unexpected chain %v", chain)
	}
	if ef.PropGet("entry_kind") != "channel" {
		t.Errorf("entry_kind = %q, want channel", ef.PropGet("entry_kind"))
	}
}

// --- multi-hop: sub publishes onto a downstream channel ---
func TestEventFlow_MultiHopChain(t *testing.T) {
	// topic.A → subA (calls forward) → publisherB → topic.B → subB
	b := newFixture("r").
		channel("topic.A", "kafka:orders.placed").
		channel("topic.B", "kafka:payments.requested").
		op("svc.subA", "handleOrder").
		op("svc.helper", "buildPayment").
		op("svc.publisherB", "publishPaymentReq").
		op("svc.subB", "handlePaymentReq").
		subscribes("svc.subA", "topic.A").
		calls("svc.subA", "svc.helper").
		calls("svc.helper", "svc.publisherB").
		publishes("svc.publisherB", "topic.B").
		subscribes("svc.subB", "topic.B")

	cfg := DefaultEventFlowConfig()
	cfg.MinSteps = 2
	stats := RunEventFlow(b.doc, cfg)
	if stats.EventFlows == 0 {
		t.Fatalf("expected at least 1 EventFlow")
	}

	// Find the longest chain seeded by topic.A — it should hit topic.B.
	var best *graph.Entity
	for i := range b.doc.Entities {
		e := &b.doc.Entities[i]
		if e.Kind != EntityKindEventFlow {
			continue
		}
		if e.PropGet("entry_id") != "topic.A" {
			continue
		}
		if best == nil {
			best = e
			continue
		}
		bsc, _ := strconv.Atoi(best.PropGet("step_count"))
		csc, _ := strconv.Atoi(e.PropGet("step_count"))
		if csc > bsc {
			best = e
		}
	}
	if best == nil {
		t.Fatal("no EventFlow seeded by topic.A")
	}
	chain := strings.Split(best.PropGet("chain"), ",")
	// Expect topic.A as seed and topic.B somewhere downstream.
	if chain[0] != "topic.A" {
		t.Errorf("seed = %s, want topic.A", chain[0])
	}
	hasTopicB := false
	hasSubB := false
	for _, id := range chain {
		if id == "topic.B" {
			hasTopicB = true
		}
		if id == "svc.subB" {
			hasSubB = true
		}
	}
	if !hasTopicB {
		t.Errorf("multi-hop chain missing topic.B: %v", chain)
	}
	if !hasSubB {
		t.Errorf("multi-hop chain missing svc.subB: %v", chain)
	}

	channelCount, _ := strconv.Atoi(best.PropGet("channel_count"))
	if channelCount < 2 {
		t.Errorf("channel_count = %d, want ≥2 for multi-hop chain", channelCount)
	}
}

// --- cycle prevention: A → sub publishes back to A ---
func TestEventFlow_CycleStopsAtRevisit(t *testing.T) {
	// Saga loop: topic.A subscribers publish back to topic.A.
	// Walker must stop after one full revolution rather than diverging.
	b := newFixture("r").
		channel("topic.A", "saga:retry").
		op("svc.handler", "retryHandler").
		subscribes("svc.handler", "topic.A").
		publishes("svc.handler", "topic.A")

	cfg := DefaultEventFlowConfig()
	cfg.MinSteps = 2
	stats := RunEventFlow(b.doc, cfg)
	if stats.EventFlows == 0 {
		t.Fatal("expected at least one EventFlow")
	}

	// No emitted chain should mention topic.A more than once.
	for _, e := range b.doc.Entities {
		if e.Kind != EntityKindEventFlow {
			continue
		}
		chain := strings.Split(e.PropGet("chain"), ",")
		seen := 0
		for _, id := range chain {
			if id == "topic.A" {
				seen++
			}
		}
		if seen > 1 {
			t.Errorf("cycle not prevented; chain visits topic.A %d times: %v", seen, chain)
		}
	}
}

// --- depth cap: a long chain of channels is truncated at MaxDepth ---
func TestEventFlow_DepthCap(t *testing.T) {
	// Build a 10-channel linear chain: each sub publishes to the next
	// channel. With MaxDepth=2 the walker must stop after 2 channel
	// hops past the seed.
	b := newFixture("r")
	const N = 10
	for i := 0; i < N; i++ {
		b.channel("topic."+strconv.Itoa(i), "topic."+strconv.Itoa(i))
		b.op("sub."+strconv.Itoa(i), "handler"+strconv.Itoa(i))
		b.subscribes("sub."+strconv.Itoa(i), "topic."+strconv.Itoa(i))
		if i < N-1 {
			b.publishes("sub."+strconv.Itoa(i), "topic."+strconv.Itoa(i+1))
		}
	}

	cfg := DefaultEventFlowConfig()
	cfg.MinSteps = 2
	cfg.MaxDepth = 2 // permit at most 2 channel transitions past the seed
	RunEventFlow(b.doc, cfg)

	// The longest chain seeded by topic.0 should mention at most
	// MaxDepth+1 = 3 channels total.
	maxChannels := 0
	for _, e := range b.doc.Entities {
		if e.Kind != EntityKindEventFlow {
			continue
		}
		if e.PropGet("entry_id") != "topic.0" {
			continue
		}
		cc, _ := strconv.Atoi(e.PropGet("channel_count"))
		if cc > maxChannels {
			maxChannels = cc
		}
	}
	if maxChannels == 0 {
		t.Fatal("no flow emitted for topic.0")
	}
	if maxChannels > 3 {
		t.Errorf("depth cap violated: longest chain has %d channels, want ≤3", maxChannels)
	}
}

// --- EventBusEvent channels are also seeded (not just MessageTopic) ---
func TestEventFlow_EventBusSeeded(t *testing.T) {
	b := newFixture("r").
		eventBus("evt.A", "event:eventbridge:orders:placed").
		op("svc.handler", "onPlaced").
		subscribes("svc.handler", "evt.A")

	cfg := DefaultEventFlowConfig()
	cfg.MinSteps = 2
	stats := RunEventFlow(b.doc, cfg)
	if stats.EventFlows == 0 {
		t.Fatalf("expected EventBusEvent to seed at least one flow")
	}
}

// --- entity/edge shape: SEED_OF_EVENT_FLOW and STEP_IN_EVENT_FLOW ---
func TestEventFlow_EmitsSeedAndStepEdges(t *testing.T) {
	b := newFixture("r").
		channel("topic.A", "kafka:t").
		op("svc.handler", "h").
		subscribes("svc.handler", "topic.A")

	cfg := DefaultEventFlowConfig()
	cfg.MinSteps = 2
	RunEventFlow(b.doc, cfg)

	var seedEdges, stepEdges int
	for _, r := range b.doc.Relationships {
		switch r.Kind {
		case RelationshipKindSeedOfEventFlow:
			seedEdges++
		case RelationshipKindStepInEventFlow:
			stepEdges++
		}
	}
	if seedEdges == 0 {
		t.Errorf("no SEED_OF_EVENT_FLOW edges emitted")
	}
	if stepEdges == 0 {
		t.Errorf("no STEP_IN_EVENT_FLOW edges emitted")
	}
}

// --- determinism: two runs over the same fixture produce identical output ---
func TestEventFlow_Deterministic(t *testing.T) {
	build := func() *graph.Document {
		b := newFixture("r").
			channel("topic.A", "a").
			channel("topic.B", "b").
			op("subA", "h1").
			op("pubB", "h2").
			op("subB", "h3").
			subscribes("subA", "topic.A").
			calls("subA", "pubB").
			publishes("pubB", "topic.B").
			subscribes("subB", "topic.B")
		return b.doc
	}
	d1 := build()
	d2 := build()
	RunEventFlow(d1, DefaultEventFlowConfig())
	RunEventFlow(d2, DefaultEventFlowConfig())

	collect := func(d *graph.Document) []string {
		var out []string
		for _, e := range d.Entities {
			if e.Kind == EntityKindEventFlow {
				out = append(out, e.PropGet("chain"))
			}
		}
		return out
	}
	a, c := collect(d1), collect(d2)
	if strings.Join(a, "|") != strings.Join(c, "|") {
		t.Errorf("non-deterministic emission:\n  run1=%v\n  run2=%v", a, c)
	}
}

// --- safety: nil doc and empty doc don't panic ---
func TestEventFlow_NilAndEmptyDocSafe(t *testing.T) {
	RunEventFlow(nil, DefaultEventFlowConfig())
	doc := &graph.Document{Repo: "r"}
	stats := RunEventFlow(doc, DefaultEventFlowConfig())
	if stats.EventFlows != 0 {
		t.Errorf("expected 0 EventFlows on empty doc, got %d", stats.EventFlows)
	}
}
