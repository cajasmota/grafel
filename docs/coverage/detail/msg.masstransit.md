<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.masstransit` — MassTransit (.NET cross-process service bus)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/masstransit.go`<br>`internal/custom/csharp/masstransit_test.go` | #4967 (builds on MediatR #4922): class XConsumer : IConsumer<T> -> SCOPE.Service(consumer) CONSUMES with task_id masstransit:message:<T> so the consumer converges with its Publish/Send producer by message contract. Saga (class : ISaga) -> SCOPE.Service(saga) and MassTransitStateMachine<TState> -> SCOPE.Service(state_machine), both CONSUMES. Proven by TestMassTransitPublishConsumerConverge (asserts task_id convergence + edge_kind=CONSUMES) and TestMassTransitSagaAndStateMachine. |
| Producer extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/masstransit.go`<br>`internal/custom/csharp/masstransit_test.go` | #4967: _publishEndpoint.Publish(new T{...}) / bus.Publish / context.Publish -> SCOPE.Operation(publish) and _sendEndpoint.Send(new T{...}) / context.Send -> SCOPE.Operation(send), each PRODUCES carrying message_type + task_id masstransit:message:<T>. A MassTransit signal gate (using MassTransit / IConsumer / ISaga / IPublishEndpoint / ConsumeContext) prevents the shared Publish/Send verbs from being mis-attributed on MediatR-only files (proven by TestMassTransitSignalGate). Honest-partial: dispatch where the message is not a 'new' literal (inline var / generic Publish<T>()) is not parsed; no AST receiver-type resolution. |
| Topic attribution | 🟢 `partial` | — | 4967 | `internal/custom/csharp/masstransit.go`<br>`internal/custom/csharp/masstransit_test.go` | #4967: the message type is the topic — producer, consumer and saga/state-machine all share task_id masstransit:message:<T> so they converge by contract. Honest-partial: the message contract class itself is not separately emitted as a SCOPE.Schema (MassTransit messages are plain POCOs with no marker interface, unlike MediatR's IRequest/INotification), and transport endpoint/queue names from cfg.ReceiveEndpoint("q", ...) are not yet recovered — tracked as a follow-up. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.masstransit ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
