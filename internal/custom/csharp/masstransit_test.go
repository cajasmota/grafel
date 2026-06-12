package csharp_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// MassTransit cross-process message-bus coverage (#4967). Value-asserting:
// Publish/Send (PRODUCES) and IConsumer<T> (CONSUMES) must converge by task_id,
// so the bus producer and its consumer link by message contract.

const massTransitSrc = `
using MassTransit;

public record OrderSubmitted(int Id);
public record ProcessOrder(int Id);

public class OrderSubmittedConsumer : IConsumer<OrderSubmitted>
{
    public Task Consume(ConsumeContext<OrderSubmitted> context) => Task.CompletedTask;
}

public class OrderSaga : ISaga
{
    public Guid CorrelationId { get; set; }
}

public class OrderStateMachine : MassTransitStateMachine<OrderState>
{
}

public class OrderService
{
    private readonly IPublishEndpoint _publishEndpoint;
    private readonly ISendEndpoint _sendEndpoint;

    public async Task Submit(int id)
    {
        await _publishEndpoint.Publish(new OrderSubmitted(id));
        await _sendEndpoint.Send(new ProcessOrder { Id = id });
    }
}
`

func findBySub(ents []types.EntityRecord, subtype, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == subtype && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func TestMassTransitPublishConsumerConverge(t *testing.T) {
	ents := extractFull(t, "custom_csharp_masstransit", fi("Orders.cs", "csharp", massTransitSrc))

	pub := findBySub(ents, "publish", "Publish OrderSubmitted")
	if pub == nil {
		t.Fatal("expected publish 'Publish OrderSubmitted'")
	}
	if pub.Properties["edge_kind"] != "PRODUCES" {
		t.Errorf("publish edge_kind = %q, want PRODUCES", pub.Properties["edge_kind"])
	}
	if pub.Properties["task_id"] != "masstransit:message:OrderSubmitted" {
		t.Errorf("publish task_id = %q", pub.Properties["task_id"])
	}

	cons := findBySub(ents, "consumer", "OrderSubmittedConsumer")
	if cons == nil {
		t.Fatal("expected consumer 'OrderSubmittedConsumer'")
	}
	if cons.Kind != "SCOPE.Service" {
		t.Errorf("consumer kind = %q, want SCOPE.Service", cons.Kind)
	}
	if cons.Properties["edge_kind"] != "CONSUMES" {
		t.Errorf("consumer edge_kind = %q, want CONSUMES", cons.Properties["edge_kind"])
	}
	if cons.Properties["task_id"] != pub.Properties["task_id"] {
		t.Errorf("consumer task_id %q != publish task_id %q",
			cons.Properties["task_id"], pub.Properties["task_id"])
	}
	if cons.Properties["message_type"] != "OrderSubmitted" {
		t.Errorf("consumer message_type = %q, want OrderSubmitted", cons.Properties["message_type"])
	}
}

func TestMassTransitSend(t *testing.T) {
	ents := extractFull(t, "custom_csharp_masstransit", fi("Orders.cs", "csharp", massTransitSrc))

	send := findBySub(ents, "send", "Send ProcessOrder")
	if send == nil {
		t.Fatal("expected send 'Send ProcessOrder'")
	}
	if send.Properties["edge_kind"] != "PRODUCES" {
		t.Errorf("send edge_kind = %q, want PRODUCES", send.Properties["edge_kind"])
	}
	if send.Properties["task_id"] != "masstransit:message:ProcessOrder" {
		t.Errorf("send task_id = %q", send.Properties["task_id"])
	}
}

func TestMassTransitSagaAndStateMachine(t *testing.T) {
	ents := extractFull(t, "custom_csharp_masstransit", fi("Orders.cs", "csharp", massTransitSrc))

	saga := findBySub(ents, "saga", "OrderSaga")
	if saga == nil {
		t.Fatal("expected saga 'OrderSaga'")
	}
	if saga.Properties["edge_kind"] != "CONSUMES" {
		t.Errorf("saga edge_kind = %q, want CONSUMES", saga.Properties["edge_kind"])
	}

	sm := findBySub(ents, "state_machine", "OrderStateMachine")
	if sm == nil {
		t.Fatal("expected state_machine 'OrderStateMachine'")
	}
	if sm.Properties["message_type"] != "OrderState" {
		t.Errorf("state_machine message_type = %q, want OrderState", sm.Properties["message_type"])
	}
}

// A plain C# file with no MassTransit signal must produce nothing — the shared
// Publish/Send verbs must not be attributed to MassTransit (MediatR guard).
func TestMassTransitSignalGate(t *testing.T) {
	const noSignal = `
public class Foo
{
    public void Bar()
    {
        _mediator.Publish(new SomethingHappened());
        _x.Send(new DoThing());
    }
}
`
	ents := extractFull(t, "custom_csharp_masstransit", fi("Foo.cs", "csharp", noSignal))
	if len(ents) != 0 {
		t.Errorf("expected no entities without MassTransit signal, got %d", len(ents))
	}
}
