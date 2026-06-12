<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `analysis.orchestration.bullmq` — BullMQ / Bull cross-repo queue topic attribution

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-06-12` | — | `internal/engine/bullmq_edges.go`<br>`internal/engine/bullmq_edges_test.go` | Emits SUBSCRIBES_TO edges (enclosing fn -> SCOPE.Queue) for consumers: new Worker('name', handler), queue.process(). |
| Producer extraction | ✅ `full` | `2026-06-12` | — | `internal/engine/bullmq_edges.go`<br>`internal/engine/bullmq_edges_test.go`<br>`internal/engine/detector.go` | #2865: append-only detector pass applyBullMQEdges (detector.go) emits PUBLISHES_TO edges (enclosing fn -> SCOPE.Queue) for Bull/BullMQ producers: new Queue('name'), queue.add(), FlowProducer.add(). Queue-variable -> name binding tracked file-locally so .add() on a known var attributes to the right topic. 6 value-asserting tests. |
| Topic attribution | ✅ `full` | `2026-06-12` | — | `internal/engine/bullmq_edges.go`<br>`internal/engine/bullmq_edges_test.go` | Emits one synthetic SCOPE.Queue entity per queue name keyed canonically bullmq:<name> (identical across repos) so the cross-repo import-channel topic linker (internal/links/topic_pass.go P7) joins producer and consumer sides with no new linker code — same technique as kafka_edges.go/rabbitmq_edges.go. Before this pass BullMQ queues carried no topic node and topic_attribution stayed partial. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update analysis.orchestration.bullmq ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
