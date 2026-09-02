<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.mediatr` — MediatR (.NET in-process CQRS / mediator)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Config binding | 🔴 `missing` | — | 5782 | — | — |
| Consumer extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/mediatr.go`<br>`internal/custom/csharp/mediatr_test.go` | #4922: class XHandler : IRequestHandler<TReq,TResp> / IRequestHandler<TReq> -> SCOPE.Service(request_handler) and class : INotificationHandler<TNote> -> SCOPE.Service(notification_handler), each with task_id mediatr:request:<T> / mediatr:notification:<T> so handler converges with its dispatch by message type. IPipelineBehavior<TReq,TResp> -> SCOPE.Pattern(pipeline_behavior). Proven by TestMediatRSendPublishHandlersConverge (task_id convergence). #6767 (2026-09-03) -- RE-DERIVED against what is emitted. This record used to say the handlers 'CONSUMES', which was true only of an inert `edge_kind: CONSUMES` entity PROPERTY (removed in #6767). NO CONSUMES EDGE IS EMITTED by this pass; that no pass in any language emits it is a repo-wide fact recorded and grep-verified in ADR-0028, not re-verified by these tests (known gap). The handler and pipeline-behavior entities are edgeless and join their dispatch by task_id only; pinned by TestMediatRHandlersAndPipelineEmitNoRelationship. The DISPATCH side does now emit a real PRODUCES edge to Class:<T> (TestMediatRDispatchProducesTheMessageContract). #6767 also gave this pass its FIRST signal gate (mtSignalRe: using MediatR / IMediator / IRequest* / INotification* / IPipelineBehavior). It was the only one of the four C# bus passes without one, so its producer regexes -- byte-identical to MassTransit's -- fired on every C# file in the corpus using those two very common verb spellings. ONE-EDGE-PER-HOP: MassTransit, MediatR and NServiceBus/Rebus spell .Publish(new T()) and .Send(new T()) identically (mtSendRe and mxSendRe are the same regex byte for byte), and Coravel's mailer matches the same bytes for a Mailable-suffixed type, so csSharedDispatchVerbOwner (internal/custom/csharp/helpers.go) names ONE owner per file -- NServiceBus > MassTransit > MediatR > Coravel's mailer, most specific marker first -- and only that pass attaches the PRODUCES edge. Signal gates alone do NOT settle it: a MassTransit consumer dispatching through IMediator passes both gates truthfully. Graded by TestSharedDispatchVerbsEmitOnePRODUCESPerHop, which runs every producing C# pass over one source. |
| Producer extraction | ✅ `full` | `2026-06-14` | — | `internal/custom/csharp/mediatr.go`<br>`internal/custom/csharp/mediatr_test.go` | #4922: _mediator.Send(new FooQuery(...)) -> SCOPE.Operation(request_dispatch) and _mediator.Publish(new BarNotification(...)) -> SCOPE.Operation(notification_dispatch), each PRODUCES carrying message_type + task_id. Honest-partial edges-to-handler are bound by shared task_id (no AST receiver-type resolution). Inline-variable / generic Send<T>() dispatch where the message is not a 'new' literal is not parsed. #6767 (2026-09-03) -- RE-DERIVED against what is emitted. Until #6767 the word PRODUCES here named an inert `edge_kind` entity PROPERTY and this pass emitted ZERO relationships; it is now a real edge to Class:<T> (csMessageProducesEdge), pinned by TestMediatRDispatchProducesTheMessageContract. But 'each PRODUCES' is NOT unconditional: MediatR is LAST in csSharedDispatchVerbOwner's precedence (NServiceBus > MassTransit > MediatR > Coravel's mailer), because its .Send(new T()) / .Publish(new T()) regexes are byte-identical to MassTransit's. In a file that is also a MassTransit or NServiceBus surface -- a consumer dispatching through IMediator is an ordinary .NET shape -- the bus owns the hop and this pass emits NO edge for it. Deliberate under-reporting in preference to the ADR-0028 section 3 double edge; graded by TestSharedDispatchVerbsEmitOnePRODUCESPerHop and by TestMediatROnlyFileStillKeepsItsEdge, the under-firing control. |
| Topic attribution | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/mediatr.go`<br>`internal/custom/csharp/mediatr_test.go` | #4922: the message contract itself is the 'topic' — class/record FooQuery : IRequest<T> -> SCOPE.Schema(request_message) and BarNotification : INotification -> SCOPE.Schema(notification_message), each stamped with task_id mediatr:request:<T> / mediatr:notification:<T> so dispatch, handler and contract all share one key. Handler/pipeline declarations are guarded out of the contract pass. Proven by TestMediatRMessageContractsAreSchemas (incl. negative: a handler is never a message). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.mediatr ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
