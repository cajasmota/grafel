package csharp_test

// #6767 — the sweep #6741 arms 3-5 did not reach.
//
// Five C# passes (coravel, masstransit, mediatr, wolverine, handle_messages)
// stamped an `edge_kind` entity PROPERTY naming a relationship kind while
// emitting ZERO relationships. On a producer that names a type the property
// duplicated an edge that did not exist; on an anonymous producer and on every
// consumer it was simply FALSE.
//
// The resolution follows #6741's binding precedent:
//
//   - PRODUCES is real and gets emitted, dispatch site -> the type the site
//     names (`Class:<T>` — the cross-file C# convention csJobProducesEdge
//     already uses for Hangfire / Quartz.NET).
//   - Where a dispatch site names no type (Coravel's anonymous
//     `Schedule(() => ...)` / `QueueAsyncTask(...)`), NOTHING is emitted. An
//     honest unresolved producer is not an edge.
//   - CONSUMES is emitted by nothing in any language — the owner declined to
//     build the two-hop work-unit form under #6741. So consumer entities carry
//     no relationship and, critically, no property claiming one.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// producesEdges6767 returns every PRODUCES edge as "<entity name> -> <ToID>".
func producesEdges6767(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindProduces) {
				out = append(out, e.Name+" -> "+r.ToID)
			}
		}
	}
	return out
}

// hasEdge6767 reports whether want appears in the "<from> -> <toID>" list.
func hasEdge6767(got []string, want string) bool {
	for _, g := range got {
		if g == want {
			return true
		}
	}
	return false
}

// assertNoEdgeKindProperty fails naming the pass and the entity whenever an
// extracted entity still carries the inert `edge_kind` property. This is the
// FALSE-CLAIM guard: pinning the new edges alone would let a pass re-stamp the
// property and go unnoticed, which is exactly how #6741 stayed invisible.
func assertNoEdgeKindProperty(t *testing.T, pass string, ents []types.EntityRecord) {
	t.Helper()
	for _, e := range ents {
		if v, ok := e.Properties["edge_kind"]; ok {
			t.Errorf("%s: entity %s/%s still stamps edge_kind=%q — a property naming a "+
				"relationship kind is not a relationship (#6767)", pass, e.Subtype, e.Name, v)
		}
	}
}

// ---------------------------------------------------------------------------
// Coravel — scheduler-style dispatch onto an IInvocable (a class target)
// ---------------------------------------------------------------------------

func TestCoravelScheduleAndQueueProduceTheirInvocable(t *testing.T) {
	src := `
using Coravel;

public class Startup
{
    public void Configure(IServiceProvider services, IQueue queue, IMailer mailer)
    {
        services.UseScheduler(scheduler =>
        {
            scheduler.Schedule<SendNewsletter>().EveryMinute();
        });
        queue.QueueInvocable<CleanupInvocable>();
        mailer.Send(new WelcomeMailable());
    }
}
`
	ents := extractFull(t, "custom_csharp_coravel", fi("Startup.cs", "csharp", src))
	got := producesEdges6767(ents)

	for _, want := range []string{
		"Schedule<SendNewsletter> -> Class:SendNewsletter",
		"QueueInvocable<CleanupInvocable> -> Class:CleanupInvocable",
		"Send<WelcomeMailable> -> Class:WelcomeMailable",
	} {
		if !hasEdge6767(got, want) {
			t.Errorf("missing PRODUCES %q; got %v", want, got)
		}
	}
	assertNoEdgeKindProperty(t, "coravel", ents)
}

func TestCoravelScheduleProducesEdgeCarriesTheTaskAddress(t *testing.T) {
	src := `
using Coravel;
app.Services.UseScheduler(scheduler => scheduler.Schedule<SendNewsletter>().Hourly());
`
	ents := extractFull(t, "custom_csharp_coravel", fi("Program.cs", "csharp", src))
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != string(types.RelationshipKindProduces) {
				continue
			}
			if got := r.Properties.Get("task_id"); got != "task:coravel:SendNewsletter" {
				t.Errorf("task_id on PRODUCES = %q, want task:coravel:SendNewsletter", got)
			}
			if got := r.Properties.Get("framework"); got != "coravel" {
				t.Errorf("framework on PRODUCES = %q, want coravel", got)
			}
			return
		}
	}
	t.Fatal("no PRODUCES edge emitted for scheduler.Schedule<SendNewsletter>()")
}

// TestCoravelAnonymousDispatchEmitsNoProduces is the over-firing mutant. The
// anonymous forms name no invocable at all; widening the emitter to "any
// producer subtype" would fabricate an edge out of a call site whose target
// Coravel itself only knows at runtime.
func TestCoravelAnonymousDispatchEmitsNoProduces(t *testing.T) {
	// The file DECLARES an invocable. That is the load-bearing part of the
	// fixture: an emitter widened to "any producer subtype" would reach for the
	// nearest invocable in the file and fabricate an edge the source never
	// names. Without a declared invocable here, that widening survives.
	src := `
using Coravel;

public class SendNewsletter : IInvocable
{
    public Task Invoke() => Task.CompletedTask;
}

app.Services.UseScheduler(scheduler =>
{
    scheduler.ScheduleAsync(async () => await DoWork()).Daily();
});
queue.QueueAsyncTask(async () => await DoOther());
`
	ents := extractFull(t, "custom_csharp_coravel", fi("Anon.cs", "csharp", src))
	// The entities themselves must still exist — an honest unresolved producer
	// is recorded, it just carries no edge and no claim of one.
	if len(ents) == 0 {
		t.Fatal("expected the anonymous schedule/queue entities to still be extracted")
	}
	if got := producesEdges6767(ents); len(got) != 0 {
		t.Errorf("anonymous Coravel dispatch emitted PRODUCES %v; it names no invocable", got)
	}
	assertNoEdgeKindProperty(t, "coravel", ents)
}

func TestCoravelInvocableConsumerEmitsNoRelationship(t *testing.T) {
	src := `
using Coravel.Invocable;

public class SendNewsletter : IInvocable
{
    public Task Invoke() => Task.CompletedTask;
}
`
	ents := extractFull(t, "custom_csharp_coravel", fi("SendNewsletter.cs", "csharp", src))
	inv := findCoravelSubtype(ents, "invocable")
	if inv == nil {
		t.Fatal("expected an invocable entity")
	}
	if len(inv.Relationships) != 0 {
		t.Errorf("invocable carries %d relationships; CONSUMES is emitted by nothing (#6741)",
			len(inv.Relationships))
	}
	assertNoEdgeKindProperty(t, "coravel", ents)
}

// ---------------------------------------------------------------------------
// MassTransit / MediatR / Wolverine / NServiceBus-Rebus — message dispatch
// ---------------------------------------------------------------------------

func TestMassTransitDispatchProducesTheMessageContract(t *testing.T) {
	src := `
using MassTransit;

public class OrderService
{
    private readonly IPublishEndpoint _publishEndpoint;
    private readonly ISendEndpoint _sendEndpoint;

    public async Task Submit()
    {
        await _publishEndpoint.Publish(new OrderSubmitted { Id = 1 });
        await _sendEndpoint.Send(new ProcessOrder { Id = 1 });
    }
}
`
	ents := extractFull(t, "custom_csharp_masstransit", fi("OrderService.cs", "csharp", src))
	got := producesEdges6767(ents)
	for _, want := range []string{
		"Publish OrderSubmitted -> Class:OrderSubmitted",
		"Send ProcessOrder -> Class:ProcessOrder",
	} {
		if !hasEdge6767(got, want) {
			t.Errorf("missing PRODUCES %q; got %v", want, got)
		}
	}
	assertNoEdgeKindProperty(t, "masstransit", ents)
}

func TestMassTransitConsumerSagaStateMachineEmitNoRelationship(t *testing.T) {
	src := `
using MassTransit;

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
`
	ents := extractFull(t, "custom_csharp_masstransit", fi("Consumers.cs", "csharp", src))
	if len(ents) == 0 {
		t.Fatal("expected consumer/saga/state-machine entities")
	}
	for _, e := range ents {
		if len(e.Relationships) != 0 {
			t.Errorf("%s/%s carries %d relationships; CONSUMES is emitted by nothing (#6741)",
				e.Subtype, e.Name, len(e.Relationships))
		}
	}
	assertNoEdgeKindProperty(t, "masstransit", ents)
}

func TestMediatRDispatchProducesTheMessageContract(t *testing.T) {
	src := `
using MediatR;

public class OrdersController
{
    private readonly IMediator _mediator;

    public async Task Post()
    {
        await _mediator.Send(new CreateOrder { Id = 1 });
        await _mediator.Publish(new OrderPlaced { Id = 1 });
    }
}
`
	ents := extractFull(t, "custom_csharp_mediatr", fi("OrdersController.cs", "csharp", src))
	got := producesEdges6767(ents)
	for _, want := range []string{
		"Send CreateOrder -> Class:CreateOrder",
		"Publish OrderPlaced -> Class:OrderPlaced",
	} {
		if !hasEdge6767(got, want) {
			t.Errorf("missing PRODUCES %q; got %v", want, got)
		}
	}
	assertNoEdgeKindProperty(t, "mediatr", ents)
}

func TestMediatRHandlersAndPipelineEmitNoRelationship(t *testing.T) {
	src := `
using MediatR;

public class CreateOrderHandler : IRequestHandler<CreateOrder, int>
{
    public Task<int> Handle(CreateOrder request, CancellationToken ct) => Task.FromResult(1);
}

public class OrderPlacedHandler : INotificationHandler<OrderPlaced>
{
    public Task Handle(OrderPlaced n, CancellationToken ct) => Task.CompletedTask;
}

public class LoggingBehavior : IPipelineBehavior<CreateOrder, int>
{
}
`
	ents := extractFull(t, "custom_csharp_mediatr", fi("Handlers.cs", "csharp", src))
	if len(ents) == 0 {
		t.Fatal("expected handler / pipeline entities")
	}
	for _, e := range ents {
		if len(e.Relationships) != 0 {
			t.Errorf("%s/%s carries %d relationships; CONSUMES is emitted by nothing (#6741)",
				e.Subtype, e.Name, len(e.Relationships))
		}
	}
	assertNoEdgeKindProperty(t, "mediatr", ents)
}

func TestWolverineDispatchProducesTheMessageContract(t *testing.T) {
	src := `
using Wolverine;

public class OrderService
{
    private readonly IMessageBus _bus;

    public async Task Run()
    {
        await _bus.PublishAsync(new OrderPlaced());
        await _bus.SendAsync(new ProcessOrder());
        await _bus.InvokeAsync<OrderView>(new GetOrder());
    }
}
`
	ents := extractFull(t, "custom_csharp_wolverine", fi("OrderService.cs", "csharp", src))
	got := producesEdges6767(ents)
	for _, want := range []string{
		"PublishAsync OrderPlaced -> Class:OrderPlaced",
		"SendAsync ProcessOrder -> Class:ProcessOrder",
		"InvokeAsync GetOrder -> Class:GetOrder",
	} {
		if !hasEdge6767(got, want) {
			t.Errorf("missing PRODUCES %q; got %v", want, got)
		}
	}
	assertNoEdgeKindProperty(t, "wolverine", ents)
}

func TestWolverineHandlerEmitsNoRelationship(t *testing.T) {
	src := `
using Wolverine;

public class OrderPlacedHandler
{
    public Task Handle(OrderPlaced msg) => Task.CompletedTask;
}
`
	ents := extractFull(t, "custom_csharp_wolverine", fi("Handler.cs", "csharp", src))
	if len(ents) == 0 {
		t.Fatal("expected a convention handler entity")
	}
	for _, e := range ents {
		if len(e.Relationships) != 0 {
			t.Errorf("%s/%s carries %d relationships; CONSUMES is emitted by nothing (#6741)",
				e.Subtype, e.Name, len(e.Relationships))
		}
	}
	assertNoEdgeKindProperty(t, "wolverine", ents)
}

func TestHandleMessagesDispatchProducesTheMessageContract(t *testing.T) {
	src := `
using NServiceBus;

public class OrderPlacedHandler : IHandleMessages<OrderPlaced>
{
    public Task Handle(OrderPlaced message, IMessageHandlerContext context)
    {
        return context.Publish(new OrderConfirmed());
    }
}

public class Dispatcher
{
    public Task Go(IMessageSession bus) => bus.Send(new ProcessOrder());
}
`
	ents := extractFull(t, "custom_csharp_nservicebus", fi("Handlers.cs", "csharp", src))
	got := producesEdges6767(ents)
	for _, want := range []string{
		"Publish OrderConfirmed -> Class:OrderConfirmed",
		"Send ProcessOrder -> Class:ProcessOrder",
	} {
		if !hasEdge6767(got, want) {
			t.Errorf("missing PRODUCES %q; got %v", want, got)
		}
	}
	// The IHandleMessages<T> consumer in the same file must stay edgeless.
	for _, e := range ents {
		if e.Subtype != "message_handler" && e.Subtype != "saga_initiator" {
			continue
		}
		if len(e.Relationships) != 0 {
			t.Errorf("%s/%s carries %d relationships; CONSUMES is emitted by nothing (#6741)",
				e.Subtype, e.Name, len(e.Relationships))
		}
	}
	assertNoEdgeKindProperty(t, "nservicebus", ents)
}

// ---------------------------------------------------------------------------
// Corpus-wide: nothing under internal/custom re-introduces the property
// ---------------------------------------------------------------------------

// TestNoCustomPassStampsAnEdgeKindProperty is the sweep guard. The behavioural
// assertions above cover the five passes #6767 names; this one covers the ones
// nobody has written a test for yet, in every language, so the defect class
// cannot come back through a sixth pass.
//
// Scope is deliberately `internal/custom` only. `internal/extractors/assembly`
// also sets a property spelled `edge_kind`, but its VALUES are "call"/"branch"
// — a mnemonic classification, not a relationship-kind name — so it is not a
// member of this defect class and widening the walk to catch it would be a
// false positive.
func TestNoCustomPassStampsAnEdgeKindProperty(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	// Positive control: the walk must actually reach source. A silent zero-file
	// walk would report a clean sweep of nothing (the failure mode that let the
	// original claim stand for months).
	files := 0
	var offenders []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, `"edge_kind"`) {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, rel+":"+itoa6767(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if files < 100 {
		t.Fatalf("walk reached only %d non-test .go files under %s — the sweep is measuring nothing",
			files, root)
	}
	if len(offenders) != 0 {
		t.Errorf("%d custom pass(es) stamp an `edge_kind` entity property, which is inert "+
			"metadata and not a relationship (#6767):\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

func itoa6767(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
