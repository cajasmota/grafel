<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.mediatr` — MediatR (.NET in-process CQRS / mediator)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/mediatr.go`<br>`internal/custom/csharp/mediatr_test.go` | #4922: class XHandler : IRequestHandler<TReq,TResp> / IRequestHandler<TReq> -> SCOPE.Service(request_handler) and class : INotificationHandler<TNote> -> SCOPE.Service(notification_handler), each CONSUMES with task_id mediatr:request:<T> / mediatr:notification:<T> so handler converges with its dispatch by message type. IPipelineBehavior<TReq,TResp> -> SCOPE.Pattern(pipeline_behavior). Proven by TestMediatRSendPublishHandlersConverge (asserts task_id convergence + edge_kind=CONSUMES). |
| Producer extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/mediatr.go`<br>`internal/custom/csharp/mediatr_test.go` | #4922: _mediator.Send(new FooQuery(...)) -> SCOPE.Operation(request_dispatch) and _mediator.Publish(new BarNotification(...)) -> SCOPE.Operation(notification_dispatch), each PRODUCES carrying message_type + task_id. Honest-partial edges-to-handler are bound by shared task_id (no AST receiver-type resolution). Inline-variable / generic Send<T>() dispatch where the message is not a 'new' literal is not parsed. |
| Topic attribution | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/mediatr.go`<br>`internal/custom/csharp/mediatr_test.go` | #4922: the message contract itself is the 'topic' — class/record FooQuery : IRequest<T> -> SCOPE.Schema(request_message) and BarNotification : INotification -> SCOPE.Schema(notification_message), each stamped with task_id mediatr:request:<T> / mediatr:notification:<T> so dispatch, handler and contract all share one key. Handler/pipeline declarations are guarded out of the contract pass. Proven by TestMediatRMessageContractsAreSchemas (incl. negative: a handler is never a message). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.mediatr ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
