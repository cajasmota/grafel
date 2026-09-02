<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.wolverine` — Wolverine (.NET convention-based message bus)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Config binding | 🔴 `missing` | — | 5782 | — | — |
| Consumer extraction | ✅ `full` | `2026-06-13` | — | `internal/custom/csharp/wolverine.go`<br>`internal/custom/csharp/wolverine_test.go` | #4995 (spun out of #4967): Wolverine has no marker interface — it routes by method convention, so the handler is any class with a Handle/Handles/Consume/Consumes method whose first parameter is the message type. The enclosing class -> SCOPE.Service(handler) with task_id wolverine:message:<T> so it converges with its PublishAsync/SendAsync/InvokeAsync producer by message contract. Proven by TestWolverinePublishHandlerConverge (task_id convergence) and TestWolverineConsumeConventionHandler. #6767 (2026-09-03) -- RE-DERIVED against what is emitted. This record used to say the handler 'CONSUMES', which was true only of an inert `edge_kind: CONSUMES` entity PROPERTY (removed in #6767). NO CONSUMES EDGE IS EMITTED by this pass; that no pass in any language emits it is a repo-wide fact recorded and grep-verified in ADR-0028, not re-verified by these tests (known gap). The handler entity is edgeless and joins its producer by task_id only; pinned by TestWolverineHandlerEmitsNoRelationship. Honest-partial: the convention method is attributed to the nearest preceding class declaration (regex enclosing-scope, not AST), so deeply nested types are not disambiguated, and Handle methods with no explicit-typed first parameter are not parsed. |
| Producer extraction | ✅ `full` | `2026-06-13` | — | `internal/custom/csharp/wolverine.go`<br>`internal/custom/csharp/wolverine_test.go` | #4995: IMessageBus dispatch — bus.PublishAsync(new T{...}) -> SCOPE.Operation(publish), bus.SendAsync(new T{...}) -> SCOPE.Operation(send), bus.InvokeAsync<TResp>(new T{...}) -> SCOPE.Operation(invoke), each PRODUCES carrying message_type + task_id wolverine:message:<T>. A Wolverine signal gate (using Wolverine / IMessageBus / .PublishAsync / .InvokeAsync / .SendAsync) keeps the shared convention Handle/Consume verbs from being mis-attributed on non-Wolverine files (proven by TestWolverineSignalGate). Honest-partial: only 'new' literal dispatch is parsed; inline-var / generic PublishAsync<T>() with no literal, and AST receiver-type resolution, are not. #6767 (2026-09-03) -- RE-DERIVED against what is emitted. Until #6767 the word PRODUCES here named an inert `edge_kind` entity PROPERTY and this pass emitted ZERO relationships. All three dispatch entities now carry a real PRODUCES relationship to the message contract they name (csMessageProducesEdge -> Class:<T>). Pinned by TestWolverineDispatchProducesTheMessageContract. Wolverine needs no arbitration: PublishAsync/SendAsync/InvokeAsync are distinct spellings that the MassTransit / MediatR / NServiceBus regexes cannot match (`.Publish\s*\(` does not match `PublishAsync(`), so it is the one bus pass whose verbs collide with nobody. |
| Topic attribution | 🟢 `partial` | — | 4995 | `internal/custom/csharp/wolverine.go`<br>`internal/custom/csharp/wolverine_test.go` | #4995: the message type is the topic — producer (publish/send/invoke) and convention handler all share task_id wolverine:message:<T> so they converge by contract (proven by TestWolverineAllEntitiesConverge). Honest-partial: the message contract class itself is not separately emitted as a SCOPE.Schema (Wolverine messages are plain POCOs, no marker interface), and transport/queue routing from endpoint config (PublishMessage<T>().ToRabbitExchange(...) etc.) is not yet recovered — tracked as a follow-up. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.wolverine ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
