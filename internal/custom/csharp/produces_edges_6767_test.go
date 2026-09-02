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
//   - CONSUMES is emitted by nothing in the tree — the owner declined to build
//     the two-hop work-unit form under #6741. That repo-wide fact is recorded
//     and grep-verified in ADR-0028; what the tests BELOW pin is the narrower
//     claim that these five passes emit none. So consumer entities carry no
//     relationship and, critically, no property claiming one.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
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

// TestCoravelProducesEdgesCarryTheirAddresses grades EVERY Coravel PRODUCES
// edge, not the first one it finds. The earlier version of this test returned
// after the first edge, so `task:coravel:` could be corrupted on the queue and
// mail edges with the whole package still green — two of the three addresses
// were ungraded.
func TestCoravelProducesEdgesCarryTheirAddresses(t *testing.T) {
	src := `
using Coravel;

public class Bootstrap
{
    public void Configure(IServiceProvider services, IQueue queue, IMailer mailer)
    {
        services.UseScheduler(scheduler => scheduler.Schedule<SendNewsletter>().Hourly());
        queue.QueueInvocable<CleanupInvocable>();
        mailer.Send(new WelcomeMailable());
    }
}
`
	ents := extractFull(t, "custom_csharp_coravel", fi("Program.cs", "csharp", src))

	// want maps each producer entity to the task_id its edge must carry. The
	// mail edge carries NONE: `task_id` is a join key, and nothing in the tree
	// mints a Mailable-side entity for one to join with, so inventing a
	// `mail:coravel:` namespace would be a key with one end.
	want := map[string]string{
		"Schedule<SendNewsletter>":         "task:coravel:SendNewsletter",
		"QueueInvocable<CleanupInvocable>": "task:coravel:CleanupInvocable",
		"Send<WelcomeMailable>":            "",
	}
	seen := map[string]bool{}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != string(types.RelationshipKindProduces) {
				continue
			}
			wantTask, known := want[e.Name]
			if !known {
				t.Errorf("unexpected PRODUCES from %q -> %s", e.Name, r.ToID)
				continue
			}
			seen[e.Name] = true
			if got := r.Properties.Get("task_id"); got != wantTask {
				t.Errorf("%s: task_id on PRODUCES = %q, want %q", e.Name, got, wantTask)
			}
			if got := r.Properties.Get("framework"); got != "coravel" {
				t.Errorf("%s: framework on PRODUCES = %q, want coravel", e.Name, got)
			}
			if got := r.Properties.Get("provenance"); got == "" {
				t.Errorf("%s: PRODUCES carries no provenance", e.Name)
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("no PRODUCES edge emitted for %s", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Cross-pass: one hop, one PRODUCES edge (ADR-0028 §3)
// ---------------------------------------------------------------------------

// csharpPassesThatProduce lists every registered C# custom pass that can emit a
// PRODUCES edge. Every cross-pass test below runs ALL of them over ONE source,
// which is the axis the per-pass tests structurally cannot see: each of those
// calls extractFull with a single pass name, so a second pass emitting the same
// edge for the same call site is invisible to them by construction.
var csharpPassesThatProduce = []string{
	"custom_csharp_masstransit",
	"custom_csharp_mediatr",
	"custom_csharp_nservicebus",
	"custom_csharp_wolverine",
	"custom_csharp_coravel",
	"custom_csharp_hangfire",
	"custom_csharp_quartz_net",
}

// producesAcrossPasses runs every producing C# pass over one source and returns
// each PRODUCES edge as "<from> -> <toID>" mapped to the framework property of
// every pass that emitted it.
func producesAcrossPasses(t *testing.T, path, src string) map[string][]string {
	t.Helper()
	got := map[string][]string{}
	for _, pass := range csharpPassesThatProduce {
		ents := extractFull(t, pass, fi(path, "csharp", src))
		for _, e := range ents {
			for _, r := range e.Relationships {
				if r.Kind != string(types.RelationshipKindProduces) {
					continue
				}
				key := e.Name + " -> " + r.ToID
				got[key] = append(got[key], pass+"/"+r.Properties.Get("framework"))
			}
		}
	}
	return got
}

// assertOneEdgePerHop fails naming every hop that more than one pass claimed.
func assertOneEdgePerHop(t *testing.T, got map[string][]string) {
	t.Helper()
	for hop, by := range got {
		if len(by) > 1 {
			t.Errorf("ADR-0028 §3 double edge: hop %q emitted %d times, by %v", hop, len(by), by)
		}
	}
}

// TestSharedDispatchVerbsEmitOnePRODUCESPerHop is the regression pin for the
// defect this file's first version shipped. `.Publish(new T())` and
// `.Send(new T())` are spelled identically by MassTransit, MediatR and
// NServiceBus/Rebus — mtSendRe and mxSendRe are the same regex, byte for byte —
// and MediatR had no signal gate at all, so on an NServiceBus handler that
// merely calls `context.Publish(new OrderConfirmed())` BOTH passes emitted a
// PRODUCES edge for the one hop.
func TestSharedDispatchVerbsEmitOnePRODUCESPerHop(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want map[string]string // hop -> the pass that must own it
	}{
		{
			name: "nservicebus handler dispatching with the shared Publish verb",
			src: `
using NServiceBus;

public class OrderPlacedHandler : IHandleMessages<OrderPlaced>
{
    public Task Handle(OrderPlaced message, IMessageHandlerContext context)
    {
        return context.Publish(new OrderConfirmed());
    }
}
`,
			want: map[string]string{
				"Publish OrderConfirmed -> Class:OrderConfirmed": "custom_csharp_nservicebus/nservicebus",
			},
		},
		{
			// The case a signal gate alone does NOT fix, and the reason
			// csSharedDispatchVerbOwner exists: both gates pass truthfully.
			name: "masstransit consumer dispatching through IMediator",
			src: `
using MassTransit;
using MediatR;

public class OrderSubmittedConsumer : IConsumer<OrderSubmitted>
{
    private readonly IMediator _mediator;
    private readonly IPublishEndpoint _publishEndpoint;

    public async Task Consume(ConsumeContext<OrderSubmitted> context)
    {
        await _mediator.Send(new CreateOrder());
        await _publishEndpoint.Publish(new OrderConfirmed());
    }
}
`,
			want: map[string]string{
				"Send CreateOrder -> Class:CreateOrder":          "custom_csharp_masstransit/masstransit",
				"Publish OrderConfirmed -> Class:OrderConfirmed": "custom_csharp_masstransit/masstransit",
			},
		},
		{
			// Coravel's mailer matches the same `.Send(new T())` bytes whenever
			// the type ends in Mailable, so it defers to the bus that owns the
			// file. Without the deferral this hop gets two edges.
			name: "coravel mailable inside a masstransit surface",
			src: `
using MassTransit;
using Coravel;

public class Notifier
{
    private readonly IPublishEndpoint _publishEndpoint;
    private readonly IMailer _mailer;

    public async Task Go()
    {
        await _mailer.Send(new WelcomeMailable());
    }
}
`,
			want: map[string]string{
				"Send WelcomeMailable -> Class:WelcomeMailable": "custom_csharp_masstransit/masstransit",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := producesAcrossPasses(t, "Case.cs", tc.src)
			assertOneEdgePerHop(t, got)
			for hop, owner := range tc.want {
				by, ok := got[hop]
				if !ok {
					t.Errorf("hop %q got no PRODUCES edge at all; edges present: %v", hop, got)
					continue
				}
				if by[0] != owner {
					t.Errorf("hop %q owned by %q, want %q", hop, by[0], owner)
				}
			}
			if len(got) != len(tc.want) {
				t.Errorf("got %d hops %v, want %d", len(got), got, len(tc.want))
			}
		})
	}
}

// TestMediatROnlyFileStillKeepsItsEdge is the under-firing control for the
// arbitration above. MediatR is LAST in the precedence, so it would be easy to
// silence it everywhere and still pass every double-edge assertion; a file with
// no competing bus signal must still get its edge.
func TestMediatROnlyFileStillKeepsItsEdge(t *testing.T) {
	src := `
using MediatR;

public class OrdersController
{
    private readonly IMediator _mediator;

    public Task Post() => _mediator.Send(new CreateOrder());
}
`
	got := producesAcrossPasses(t, "OrdersController.cs", src)
	assertOneEdgePerHop(t, got)
	by, ok := got["Send CreateOrder -> Class:CreateOrder"]
	if !ok {
		t.Fatalf("a MediatR-only file lost its PRODUCES edge; edges present: %v", got)
	}
	if by[0] != "custom_csharp_mediatr/mediatr" {
		t.Errorf("owner = %q, want custom_csharp_mediatr/mediatr", by[0])
	}
}

// TestCoravelOnlyFileStillKeepsItsMailEdge is the same control for Coravel's
// mailer: it defers to a bus, but with no bus in the file it must still fire.
func TestCoravelOnlyFileStillKeepsItsMailEdge(t *testing.T) {
	src := `
using Coravel;

public class Notifier
{
    private readonly IMailer _mailer;

    public Task Go() => _mailer.Send(new WelcomeMailable());
}
`
	got := producesAcrossPasses(t, "Notifier.cs", src)
	assertOneEdgePerHop(t, got)
	by, ok := got["Send<WelcomeMailable> -> Class:WelcomeMailable"]
	if !ok {
		t.Fatalf("a Coravel-only file lost its mail PRODUCES edge; edges present: %v", got)
	}
	if by[0] != "custom_csharp_coravel/coravel" {
		t.Errorf("owner = %q, want custom_csharp_coravel/coravel", by[0])
	}
}

// TestEveryProducingPassIsRegistered keeps csharpPassesThatProduce honest — a
// renamed or dropped pass must fail loudly rather than silently shrink the
// cross-pass sweep to the passes that still exist.
func TestEveryProducingPassIsRegistered(t *testing.T) {
	for _, pass := range csharpPassesThatProduce {
		if _, ok := extreg.Get(pass); !ok {
			t.Errorf("%s is not registered — csharpPassesThatProduce is stale", pass)
		}
	}
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
