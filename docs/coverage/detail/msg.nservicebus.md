<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.nservicebus` — NServiceBus / Rebus (IHandleMessages<T> convention)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/handle_messages.go`<br>`internal/custom/csharp/handle_messages_test.go` | #4967: class XHandler : IHandleMessages<T> (the shared NServiceBus + Rebus handler interface) -> SCOPE.Service(message_handler) CONSUMES with task_id msgbus:message:<T>, and class : IAmInitiatedBy<T> -> SCOPE.Service(saga_initiator) CONSUMES. Each converges with its dispatch by message contract. Proven by TestHandleMessagesConverge (asserts task_id convergence + edge_kind=CONSUMES). |
| Producer extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/handle_messages.go`<br>`internal/custom/csharp/handle_messages_test.go` | #4967: bus.Publish(new T()) / context.Publish -> SCOPE.Operation(publish) and bus.Send(new T()) / context.Send -> SCOPE.Operation(send), each PRODUCES carrying message_type + task_id msgbus:message:<T>. Gated on an IHandleMessages/IAmInitiatedBy (or using NServiceBus/Rebus) signal so the shared Publish/Send verbs are not mis-attributed (proven by TestHandleMessagesSignalGate). Honest-partial: only 'new' literal dispatch is parsed. |
| Topic attribution | 🟢 `partial` | — | 4967 | `internal/custom/csharp/handle_messages.go`<br>`internal/custom/csharp/handle_messages_test.go` | #4967: the message type is the topic — producer and handler share task_id msgbus:message:<T>. Honest-partial: message contracts are plain POCOs (no marker interface) so they are not separately emitted as SCOPE.Schema, and endpoint/routing config (UseTransport, routing.RouteToEndpoint) is not yet recovered — tracked as a follow-up. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.nservicebus ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
