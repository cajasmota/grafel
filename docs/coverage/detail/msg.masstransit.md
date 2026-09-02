<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.masstransit` — MassTransit (.NET cross-process service bus)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Config binding | 🔴 `missing` | — | 5782 | — | — |
| Consumer extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/masstransit.go`<br>`internal/custom/csharp/masstransit_test.go` | #4967 (builds on MediatR #4922): class XConsumer : IConsumer<T> -> SCOPE.Service(consumer) with task_id masstransit:message:<T> so the consumer converges with its Publish/Send producer by message contract. Saga (class : ISaga) -> SCOPE.Service(saga) and MassTransitStateMachine<TState> -> SCOPE.Service(state_machine). Proven by TestMassTransitPublishConsumerConverge (task_id convergence) and TestMassTransitSagaAndStateMachine. #6767 (2026-09-03) -- RE-DERIVED against what is emitted. This record used to say these three 'CONSUMES', which was true only of an inert `edge_kind: CONSUMES` entity PROPERTY (removed in #6767). NO CONSUMES EDGE IS EMITTED by this pass; that no pass in any language emits it is a repo-wide fact recorded and grep-verified in ADR-0028, not re-verified by these tests (known gap -- the honest form needs the two-hop work-unit node #6741 declined to build). All three consumer entities are edgeless and join their producer by task_id only; pinned by TestMassTransitConsumerSagaStateMachineEmitNoRelationship. |
| Producer extraction | ✅ `full` | `2026-06-14` | — | `internal/custom/csharp/masstransit.go`<br>`internal/custom/csharp/masstransit_test.go` | #4967: _publishEndpoint.Publish(new T{...}) / bus.Publish / context.Publish -> SCOPE.Operation(publish) and _sendEndpoint.Send(new T{...}) / context.Send -> SCOPE.Operation(send), each PRODUCES carrying message_type + task_id masstransit:message:<T>. A MassTransit signal gate (using MassTransit / IConsumer / ISaga / IPublishEndpoint / ConsumeContext) keeps this pass off MediatR-only files (proven by TestMassTransitSignalGate). That gate is ONE-DIRECTIONAL and was never symmetric: until #6767 MediatR had no gate at all, so it fired on MassTransit- and NServiceBus-only files. Honest-partial: dispatch where the message is not a 'new' literal (inline var / generic Publish<T>()) is not parsed; no AST receiver-type resolution. #6767 (2026-09-03) -- RE-DERIVED against what is emitted. Until #6767 the word PRODUCES here named an inert `edge_kind` entity PROPERTY and this pass emitted ZERO relationships. Every publish/send entity now carries a real PRODUCES relationship to the message contract it names (csMessageProducesEdge -> Class:<T>, carrying framework/message_type/provenance/task_id). Pinned by TestMassTransitDispatchProducesTheMessageContract. ONE-EDGE-PER-HOP: MassTransit, MediatR and NServiceBus/Rebus spell .Publish(new T()) and .Send(new T()) identically (mtSendRe and mxSendRe are the same regex byte for byte), and Coravel's mailer matches the same bytes for a Mailable-suffixed type, so csSharedDispatchVerbOwner (internal/custom/csharp/helpers.go) names ONE owner per file -- NServiceBus > MassTransit > MediatR > Coravel's mailer, most specific marker first -- and only that pass attaches the PRODUCES edge. Signal gates alone do NOT settle it: a MassTransit consumer dispatching through IMediator passes both gates truthfully. Graded by TestSharedDispatchVerbsEmitOnePRODUCESPerHop, which runs every producing C# pass over one source. |
| Topic attribution | 🟢 `partial` | — | 4967 | `internal/custom/csharp/masstransit.go`<br>`internal/custom/csharp/masstransit_test.go` | #4967: the message type is the topic — producer, consumer and saga/state-machine all share task_id masstransit:message:<T> so they converge by contract. Honest-partial: the message contract class itself is not separately emitted as a SCOPE.Schema (MassTransit messages are plain POCOs with no marker interface, unlike MediatR's IRequest/INotification), and transport endpoint/queue names from cfg.ReceiveEndpoint("q", ...) are not yet recovered — tracked as a follow-up. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.masstransit ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
