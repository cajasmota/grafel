<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.nservicebus` — NServiceBus / Rebus (IHandleMessages<T> convention)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Config binding | 🔴 `missing` | — | 5782 | — | — |
| Consumer extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/handle_messages.go`<br>`internal/custom/csharp/handle_messages_test.go` | #4967: class XHandler : IHandleMessages<T> (the shared NServiceBus + Rebus handler interface) -> SCOPE.Service(message_handler) with task_id msgbus:message:<T>, and class : IAmInitiatedBy<T> -> SCOPE.Service(saga_initiator). Each converges with its dispatch by message contract. Proven by TestHandleMessagesConverge (task_id convergence). #6767 (2026-09-03) -- RE-DERIVED against what is emitted. This record used to say both 'CONSUMES', which was true only of an inert `edge_kind: CONSUMES` entity PROPERTY (removed in #6767). NO CONSUMES EDGE IS EMITTED by this pass; that no pass in any language emits it is a repo-wide fact recorded and grep-verified in ADR-0028, not re-verified by these tests (known gap). Both consumer entities are edgeless and join their dispatch by task_id only; pinned by TestHandleMessagesDispatchProducesTheMessageContract. |
| Producer extraction | ✅ `full` | `2026-06-14` | — | `internal/custom/csharp/handle_messages.go`<br>`internal/custom/csharp/handle_messages_test.go` | #4967: bus.Publish(new T()) / context.Publish -> SCOPE.Operation(publish) and bus.Send(new T()) / context.Send -> SCOPE.Operation(send), each PRODUCES carrying message_type + task_id msgbus:message:<T>. Gated on an IHandleMessages/IAmInitiatedBy (or using NServiceBus/Rebus) signal so the shared Publish/Send verbs are not mis-attributed (proven by TestHandleMessagesSignalGate). Honest-partial: only 'new' literal dispatch is parsed. #6767 (2026-09-03) -- RE-DERIVED against what is emitted. Until #6767 the word PRODUCES here named an inert `edge_kind` entity PROPERTY and this pass emitted ZERO relationships. Every publish/send entity now carries a real PRODUCES relationship to the message contract it names (csMessageProducesEdge -> Class:<T>). Pinned by TestHandleMessagesDispatchProducesTheMessageContract. ONE-EDGE-PER-HOP: MassTransit, MediatR and NServiceBus/Rebus spell .Publish(new T()) and .Send(new T()) identically (mtSendRe and mxSendRe are the same regex byte for byte), and Coravel's mailer matches the same bytes for a Mailable-suffixed type, so csSharedDispatchVerbOwner (internal/custom/csharp/helpers.go) names ONE owner per file -- NServiceBus > MassTransit > MediatR > Coravel's mailer, most specific marker first -- and only that pass attaches the PRODUCES edge. Signal gates alone do NOT settle it: a MassTransit consumer dispatching through IMediator passes both gates truthfully. Graded by TestSharedDispatchVerbsEmitOnePRODUCESPerHop, which runs every producing C# pass over one source. |
| Topic attribution | 🟢 `partial` | — | 4967 | `internal/custom/csharp/handle_messages.go`<br>`internal/custom/csharp/handle_messages_test.go` | #4967: the message type is the topic — producer and handler share task_id msgbus:message:<T>. Honest-partial: message contracts are plain POCOs (no marker interface) so they are not separately emitted as SCOPE.Schema, and endpoint/routing config (UseTransport, routing.RouteToEndpoint) is not yet recovered — tracked as a follow-up. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.nservicebus ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
