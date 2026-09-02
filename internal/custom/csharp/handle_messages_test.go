package csharp_test

import (
	"testing"
)

// NServiceBus / Rebus IHandleMessages<T> handler-convention coverage (#4967).
// Value-asserting: Publish/Send (PRODUCES) and IHandleMessages<T> (CONSUMES)
// converge by task_id, so dispatch and handler link by message contract.

const nServiceBusSrc = `
using NServiceBus;

public class OrderPlaced { public int Id { get; set; } }
public class ProcessOrder { public int Id { get; set; } }

public class OrderPlacedHandler : IHandleMessages<OrderPlaced>
{
    public Task Handle(OrderPlaced message, IMessageHandlerContext context) => Task.CompletedTask;
}

public class OrderSaga : IAmInitiatedBy<OrderPlaced>
{
    public Task Handle(OrderPlaced message, IMessageHandlerContext context) => Task.CompletedTask;
}

public class OrderEndpoint
{
    private readonly IMessageSession _bus;

    public async Task Run()
    {
        await _bus.Publish(new OrderPlaced { Id = 1 });
        await _bus.Send(new ProcessOrder { Id = 1 });
    }
}
`

func TestHandleMessagesConverge(t *testing.T) {
	ents := extractFull(t, "custom_csharp_nservicebus", fi("Orders.cs", "csharp", nServiceBusSrc))

	pub := findBySub(ents, "publish", "Publish OrderPlaced")
	if pub == nil {
		t.Fatal("expected publish 'Publish OrderPlaced'")
	}
	if _, ok := pub.Properties["edge_kind"]; ok {
		t.Errorf("publish still stamps the inert edge_kind property (#6767)")
	}

	h := findBySub(ents, "message_handler", "OrderPlacedHandler")
	if h == nil {
		t.Fatal("expected message_handler 'OrderPlacedHandler'")
	}
	if _, ok := h.Properties["edge_kind"]; ok {
		t.Errorf("handler still stamps the inert edge_kind property (#6767)")
	}
	if h.Properties["task_id"] != pub.Properties["task_id"] {
		t.Errorf("handler task_id %q != publish task_id %q",
			h.Properties["task_id"], pub.Properties["task_id"])
	}
	if h.Properties["message_type"] != "OrderPlaced" {
		t.Errorf("handler message_type = %q, want OrderPlaced", h.Properties["message_type"])
	}

	if findBySub(ents, "send", "Send ProcessOrder") == nil {
		t.Error("expected send 'Send ProcessOrder'")
	}

	saga := findBySub(ents, "saga_initiator", "OrderSaga")
	if saga == nil {
		t.Fatal("expected saga_initiator 'OrderSaga'")
	}
	if saga.Properties["message_type"] != "OrderPlaced" {
		t.Errorf("saga_initiator message_type = %q, want OrderPlaced", saga.Properties["message_type"])
	}
}

// No IHandleMessages signal -> nothing (shared Publish/Send not mis-claimed).
func TestHandleMessagesSignalGate(t *testing.T) {
	const noSignal = `
public class Foo
{
    public void Bar()
    {
        _mediator.Publish(new SomethingHappened());
    }
}
`
	ents := extractFull(t, "custom_csharp_nservicebus", fi("Foo.cs", "csharp", noSignal))
	if len(ents) != 0 {
		t.Errorf("expected no entities without IHandleMessages signal, got %d", len(ents))
	}
}
