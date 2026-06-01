package elixir_test

import "testing"

// TestBroadwayKafkaPipeline asserts that a Broadway pipeline with a Kafka
// producer yields the ingress-topic MessageTopic entities (from the
// producer's topics list) plus the handle_message / handle_batch stages.
func TestBroadwayKafkaPipeline(t *testing.T) {
	src := `
defmodule MyApp.OrderPipeline do
  use Broadway

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {BroadwayKafka.Producer, [
          hosts: [localhost: 9092],
          group_id: "order_consumers",
          topics: ["orders", "payments"]
        ]}
      ],
      processors: [default: []],
      batchers: [default: []]
    )
  end

  def handle_message(_processor, message, _context) do
    message
  end

  def handle_batch(_batcher, messages, _info, _context) do
    messages
  end
end
`
	ents := extract(t, "custom_elixir_broadway", fi("order_pipeline.ex", "elixir", src))

	pipe := findEntity(ents, "SCOPE.Component", "MyApp.OrderPipeline")
	if pipe == nil {
		t.Fatal("expected MyApp.OrderPipeline pipeline component")
	}
	if got := pipe.Props["broker"]; got != "kafka" {
		t.Errorf("expected broker kafka, got %q", got)
	}
	if got := pipe.Props["producer_module"]; got != "BroadwayKafka.Producer" {
		t.Errorf("expected producer_module BroadwayKafka.Producer, got %q", got)
	}

	orders := findEntity(ents, "SCOPE.MessageTopic", "orders")
	if orders == nil {
		t.Fatal("expected ingress topic 'orders'")
	}
	if got := orders.Props["broker"]; got != "kafka" {
		t.Errorf("expected orders broker kafka, got %q", got)
	}
	if got := orders.Props["ingress"]; got != "true" {
		t.Errorf("expected ingress=true on orders topic")
	}
	if orders.Subtype != "topic" {
		t.Errorf("expected orders subtype topic, got %q", orders.Subtype)
	}
	if findEntity(ents, "SCOPE.MessageTopic", "payments") == nil {
		t.Error("expected ingress topic 'payments'")
	}

	hm := findEntity(ents, "SCOPE.Operation", "MyApp.OrderPipeline.handle_message")
	if hm == nil {
		t.Fatal("expected handle_message handler stage")
	}
	if got := hm.Props["flow_root"]; got != "true" {
		t.Error("expected flow_root=true on handle_message")
	}
	if findEntity(ents, "SCOPE.Operation", "MyApp.OrderPipeline.handle_batch") == nil {
		t.Error("expected handle_batch handler stage")
	}
}

// TestBroadwaySQSPipeline asserts an SQS off_broadway producer maps the queue
// option to an ingress queue MessageTopic with broker=sqs.
func TestBroadwaySQSPipeline(t *testing.T) {
	src := `
defmodule MyApp.EventPipeline do
  use Broadway

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {BroadwaySQS.Producer, queue: "events-queue"}
      ]
    )
  end

  def handle_message(_, message, _), do: message
end
`
	ents := extract(t, "custom_elixir_broadway", fi("event_pipeline.ex", "elixir", src))

	q := findEntity(ents, "SCOPE.MessageTopic", "events-queue")
	if q == nil {
		t.Fatal("expected ingress queue 'events-queue'")
	}
	if got := q.Props["broker"]; got != "sqs" {
		t.Errorf("expected broker sqs, got %q", got)
	}
	if q.Subtype != "queue" {
		t.Errorf("expected subtype queue, got %q", q.Subtype)
	}
}

// TestBroadwayNoMatch ensures non-Broadway modules produce nothing.
func TestBroadwayNoMatch(t *testing.T) {
	src := `
defmodule MyApp.Plain do
  def handle_message(_, m, _), do: m
end
`
	ents := extract(t, "custom_elixir_broadway", fi("plain.ex", "elixir", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities from non-Broadway module, got %d", len(ents))
	}
}
