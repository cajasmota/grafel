<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.rdkafka` — rdkafka (Kafka)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | `2026-05-31` | 3558 | `internal/engine/kafka_edges.go`<br>`internal/engine/kafka_edges_test.go` | rdkafka FutureRecord::to("topic")/BaseRecord::to("topic") -> PUBLISHES_TO and StreamConsumer .subscribe(&["a","b"]) -> SUBSCRIBES_TO, attributed to the enclosing fn (SCOPE.Operation); MessageTopic keyed kafka:<topic> joins cross-repo. Partial: only literal topic names are resolved (const/variable topics fall back to dynamic kafka:channel), and caller attribution is same-file nearest-fn. |
| Producer extraction | 🟢 `partial` | `2026-05-31` | 3558 | `internal/engine/kafka_edges.go`<br>`internal/engine/kafka_edges_test.go` | rdkafka FutureRecord::to("topic")/BaseRecord::to("topic") -> PUBLISHES_TO and StreamConsumer .subscribe(&["a","b"]) -> SUBSCRIBES_TO, attributed to the enclosing fn (SCOPE.Operation); MessageTopic keyed kafka:<topic> joins cross-repo. Partial: only literal topic names are resolved (const/variable topics fall back to dynamic kafka:channel), and caller attribution is same-file nearest-fn. |
| Topic attribution | 🟢 `partial` | `2026-05-31` | 3558 | `internal/engine/kafka_edges.go`<br>`internal/engine/kafka_edges_test.go` | rdkafka FutureRecord::to("topic")/BaseRecord::to("topic") -> PUBLISHES_TO and StreamConsumer .subscribe(&["a","b"]) -> SUBSCRIBES_TO, attributed to the enclosing fn (SCOPE.Operation); MessageTopic keyed kafka:<topic> joins cross-repo. Partial: only literal topic names are resolved (const/variable topics fall back to dynamic kafka:channel), and caller attribution is same-file nearest-fn. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.rdkafka ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
