package csharp_test

import (
	"testing"
)

// Wolverine cross-process message-bus coverage (#4995). Unlike MassTransit /
// NServiceBus (marker interfaces), Wolverine routes by method convention:
// a Handle(T)/Consume(T) method makes the enclosing class a handler. Producers
// dispatch via IMessageBus PublishAsync/SendAsync/InvokeAsync. Value-asserting:
// the convention handler (CONSUMES) and the dispatch site (PRODUCES) must
// converge by task_id wolverine:message:<T>.

const wolverineSrc = `
using Wolverine;

public record OrderPlaced(int Id);
public record ProcessOrder(int Id);
public record GetTotal(int Id);

public class OrderPlacedHandler
{
    public Task Handle(OrderPlaced msg) => Task.CompletedTask;
}

public class ProcessOrderHandler
{
    public void Consume(ProcessOrder msg) { }
}

public class OrderService
{
    private readonly IMessageBus _bus;

    public async Task Run(int id)
    {
        await _bus.PublishAsync(new OrderPlaced(id));
        await _bus.SendAsync(new ProcessOrder { Id = id });
        var total = await _bus.InvokeAsync<int>(new GetTotal(id));
    }
}
`

func TestWolverinePublishHandlerConverge(t *testing.T) {
	ents := extractFull(t, "custom_csharp_wolverine", fi("Orders.cs", "csharp", wolverineSrc))

	pub := findBySub(ents, "publish", "PublishAsync OrderPlaced")
	if pub == nil {
		t.Fatal("expected publish 'PublishAsync OrderPlaced'")
	}
	if pub.Properties["edge_kind"] != "PRODUCES" {
		t.Errorf("publish edge_kind = %q, want PRODUCES", pub.Properties["edge_kind"])
	}
	if pub.Properties["task_id"] != "wolverine:message:OrderPlaced" {
		t.Errorf("publish task_id = %q", pub.Properties["task_id"])
	}

	h := findBySub(ents, "handler", "OrderPlacedHandler")
	if h == nil {
		t.Fatal("expected handler 'OrderPlacedHandler'")
	}
	if h.Kind != "SCOPE.Service" {
		t.Errorf("handler kind = %q, want SCOPE.Service", h.Kind)
	}
	if h.Properties["edge_kind"] != "CONSUMES" {
		t.Errorf("handler edge_kind = %q, want CONSUMES", h.Properties["edge_kind"])
	}
	if h.Properties["message_type"] != "OrderPlaced" {
		t.Errorf("handler message_type = %q, want OrderPlaced", h.Properties["message_type"])
	}
	if h.Properties["task_id"] != pub.Properties["task_id"] {
		t.Errorf("handler task_id %q != publish task_id %q",
			h.Properties["task_id"], pub.Properties["task_id"])
	}
}

func TestWolverineConsumeConventionHandler(t *testing.T) {
	ents := extractFull(t, "custom_csharp_wolverine", fi("Orders.cs", "csharp", wolverineSrc))

	// A Consume(T) method also marks a convention handler.
	h := findBySub(ents, "handler", "ProcessOrderHandler")
	if h == nil {
		t.Fatal("expected handler 'ProcessOrderHandler' from Consume(T)")
	}
	if h.Properties["task_id"] != "wolverine:message:ProcessOrder" {
		t.Errorf("handler task_id = %q, want wolverine:message:ProcessOrder", h.Properties["task_id"])
	}

	send := findBySub(ents, "send", "SendAsync ProcessOrder")
	if send == nil {
		t.Fatal("expected send 'SendAsync ProcessOrder'")
	}
	if send.Properties["edge_kind"] != "PRODUCES" {
		t.Errorf("send edge_kind = %q, want PRODUCES", send.Properties["edge_kind"])
	}
	if send.Properties["task_id"] != h.Properties["task_id"] {
		t.Errorf("send task_id %q != handler task_id %q",
			send.Properties["task_id"], h.Properties["task_id"])
	}
}

func TestWolverineInvokeRequestResponse(t *testing.T) {
	ents := extractFull(t, "custom_csharp_wolverine", fi("Orders.cs", "csharp", wolverineSrc))

	inv := findBySub(ents, "invoke", "InvokeAsync GetTotal")
	if inv == nil {
		t.Fatal("expected invoke 'InvokeAsync GetTotal'")
	}
	if inv.Properties["edge_kind"] != "PRODUCES" {
		t.Errorf("invoke edge_kind = %q, want PRODUCES", inv.Properties["edge_kind"])
	}
	if inv.Properties["task_id"] != "wolverine:message:GetTotal" {
		t.Errorf("invoke task_id = %q, want wolverine:message:GetTotal", inv.Properties["task_id"])
	}
	if inv.Kind != "SCOPE.Operation" {
		t.Errorf("invoke kind = %q, want SCOPE.Operation", inv.Kind)
	}
}

// The signal gate must keep a convention Handle(T) on a non-Wolverine file from
// being mis-attributed. A plain class with a Handle method but no Wolverine /
// IMessageBus / *Async dispatch signal yields nothing.
func TestWolverineSignalGate(t *testing.T) {
	const noSignal = `
public class SomeButton
{
    public void Handle(ClickEvent e) { }
}
`
	ents := extractFull(t, "custom_csharp_wolverine", fi("Button.cs", "csharp", noSignal))
	for _, e := range ents {
		if e.Properties["framework"] == "wolverine" {
			t.Errorf("signal gate leaked a wolverine entity: %s %s", e.Subtype, e.Name)
		}
	}
}

// Convergence sanity: every emitted entity carries a wolverine:message:<T>
// task_id and a PRODUCES/CONSUMES edge_kind, so the graph can link them.
func TestWolverineAllEntitiesConverge(t *testing.T) {
	ents := extractFull(t, "custom_csharp_wolverine", fi("Orders.cs", "csharp", wolverineSrc))
	if len(ents) == 0 {
		t.Fatal("expected wolverine entities")
	}
	for _, e := range ents {
		tid := e.Properties["task_id"]
		if tid == "" || tid[:len("wolverine:message:")] != "wolverine:message:" {
			t.Errorf("entity %s %s has bad task_id %q", e.Subtype, e.Name, tid)
		}
		ek := e.Properties["edge_kind"]
		if ek != "PRODUCES" && ek != "CONSUMES" {
			t.Errorf("entity %s %s has bad edge_kind %q", e.Subtype, e.Name, ek)
		}
	}
}
