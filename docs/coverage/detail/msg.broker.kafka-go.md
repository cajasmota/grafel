<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.broker.kafka-go` — Kafka — Go (Sarama / segmentio/kafka-go)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | `2026-06-12` | 1467 | `internal/engine/kafka_edges.go`<br>`internal/engine/kafka_edges_test.go` | #4923: synthesizeGoKafka resolves consumer-side topics for kafka.ReaderConfig / NewReader / ConsumerGroup via surrounding-text direction heuristics (the Topic: literal alone does not encode direction), emitting SUBSCRIBES_TO from the enclosing func to topic:kafka:<name>. Honest-partial (#1467): direction is text-heuristic; wrapper/idiom consumer shapes (#1467) and dynamic topic names are not fully resolved. |
| Producer extraction | ✅ `full` | `2026-06-12` | — | `internal/engine/kafka_edges.go`<br>`internal/engine/kafka_edges_test.go`<br>`internal/engine/kafka_wrapper_edges.go` | #4923 (de-dupe of the multi msg.broker.kafka record — Go topology was implemented but invisible to a Go auditor): synthesizeGoKafka (kafka_edges.go, case "go") handles Sarama (ProducerMessage Topic: "name") and segmentio/kafka-go (Writer Topic field) producer sites, emitting a PUBLISHES_TO edge from the enclosing Go func to topic:kafka:<name>. kafka_wrapper_edges.go covers wrapper idioms. topic_pass.go joins to consumers cross-repo. |
| Topic attribution | 🟢 `partial` | `2026-06-12` | — | `internal/engine/kafka_edges.go`<br>`internal/links/topic_pass.go` | #4923: literal Topic field strings become topic:kafka:<name> SCOPE.MessageTopic nodes; topic_pass.go joins a Go producer to a consumer (any language) sharing the node name into a cross-repo PUBLISHES_TO->SUBSCRIBES_TO topology edge (channel=kafka). Honest-partial: only string-literal topics. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.broker.kafka-go ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
