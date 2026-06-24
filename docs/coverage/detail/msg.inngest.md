<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.inngest` — Inngest (durable functions / event-driven jobs)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Task Queues
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-06-24` | 5483 | `internal/custom/javascript/inngest.go`<br>`internal/engine/inngest_edges.go`<br>`internal/engine/inngest_edges_test.go` | #5483 (epic #5479, Phase 2 — EDGES): the applyInngestEdges engine pass now wires the consumer/trigger side too. #5480 extracts each inngest.createFunction({ id|name }, { event|cron }, handler) call site as one SCOPE.Function (subtype inngest_function) named after the config id/name — the consumer side of an Inngest event, the durable-function analogue of a BullMQ Worker — capturing the trigger as an attribute (trigger_event+trigger_type=event, or trigger_cron+trigger_type=cron). On top of that, for each event-triggered createFunction, #5483 emits a SUBSCRIBES_TO edge from Function:<id> to the SCOPE.MessageTopic event entity #5481 created, resolved by the `SCOPE.MessageTopic:<name>` Kind:Name ToID stub — reusing the SAME SUBSCRIBES_TO consumer edge kind+direction (consumer Function → topic) as the Kafka/BullMQ/Azure consumer passes, so the cross-repo topic linker (internal/links/topic_pass.go) and dashboard topology/flows panels understand it with no new code. This is the symmetric partner of #5482's PUBLISHES_TO: a function that sends event X and another triggered by X form an event→function→event workflow chain. Cron-triggered functions ({ cron }, no event) are scheduled jobs and emit no subscriber edge. Same attribution gate (inngest import or a receiver named/ending in `inngest`); function identity is the config id (preferred) or name (fallback); append-only. Honest-partial: a createFunction with no literal id/name (dynamically named) is skipped. Proven by TestInngest_Consumer_EventTriggerEmitsSubscribesTo, _Consumer_CronTriggerEmitsNoEdge, and the end-to-end _WorkflowChain. |
| Producer extraction | ✅ `full` | `2026-06-24` | 5482 | `internal/custom/javascript/inngest.go`<br>`internal/engine/inngest_edges.go`<br>`internal/engine/inngest_edges_test.go` | #5482 (epic #5479, Phase 2 — EDGES): the applyInngestEdges engine pass wires the producer side. For each `<client>.send({ name: "..." })` / `<client>.sendEvent(...)` (and in-handler `step.sendEvent(...)`) call, it emits a PUBLISHES_TO edge from the enclosing scope to the SCOPE.MessageTopic event entity #5481 created, resolved by the `SCOPE.MessageTopic:<name>` Kind:Name ToID stub — reusing the SAME PUBLISHES_TO edge kind as the Kafka/BullMQ/RabbitMQ producer passes, so the cross-repo topic linker (internal/links/topic_pass.go) and dashboard topology/flows panels understand it with no new code. An array of payloads yields one edge per distinct name. The enclosing function/handler/route is resolved via findEnclosingNodeName; an unresolved enclosing scope (e.g. a module-top-level send) falls back to Function:module rather than dropping the edge. Same attribution gate (inngest import or a receiver named/ending in `inngest`, plus the `step` object). Append-only. Proven by TestInngest_Producer_SendEmitsPublishesTo, _MultiSend, _StepSendEvent, and _EnclosingScopeFallback. |
| Topic attribution | 🟢 `partial` | `2026-06-24` | 5481 | `internal/custom/javascript/inngest.go`<br>`internal/custom/javascript/inngest_test.go`<br>`testdata/fixtures/typescript/inngest_functions.ts` | #5481 (epic #5479, Phase 1 item 2 — ENTITIES only): each DISTINCT Inngest event name becomes one SCOPE.MessageTopic (subtype inngest, framework=inngest, topic_id=event:<name>) — the Inngest event analogue of a BullMQ/Kafka topic. Event names are harvested from createFunction `{ event: "..." }` triggers, `<client>.send/sendEvent({ name: "..." })` producer payloads (one topic per name: key in the bounded send region), and typed `new EventSchemas().fromRecord<{ "name": ... }>()` / fromUnion schema definitions (the quoted keys of the balanced type-argument record). Deduped by event name within a file; the first reference site is the topic's source location, with provenance INFERRED_FROM_INNGEST_EVENT_REFERENCE or INFERRED_FROM_INNGEST_EVENT_SCHEMA. Same attribution gate as the function extractor (inngest import or a receiver named/ending in `inngest`). Honest-partial: dynamic/computed event names and schema records referenced via a named type alias (fromRecord<Events>) rather than an inline literal are not resolved; the EMITS/TRIGGERS edges wiring topics to their producers/consumers are #5482/#5483. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.inngest ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
