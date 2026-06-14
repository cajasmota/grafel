<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.broker.kafka-dotnet` — Kafka — C# (Confluent.Kafka)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | `2026-06-14` | 5125 | `internal/engine/kafka_edges.go`<br>`internal/engine/kafka_edges_test.go` | #4996: synthesizeCSharpKafka (kafka_edges.go, case "csharp") resolves Confluent.Kafka consumer.Subscribe("topic") and the array form consumer.Subscribe(new[] { "a", "b" }) / new List<string>{...}, emitting SUBSCRIBES_TO from the enclosing method to topic:kafka:<name> (messaging_layer=confluent-kafka-dotnet). #5125: the per-partition manual-assignment form consumer.Assign(new TopicPartition("topic", n)) (and lists thereof) is now recognised, emitting SUBSCRIBES_TO with assignment=manual on both the topic node and the edge (fired only when the file calls .Assign( so a stray new TopicPartition is not miscounted). Honest-partial: only string-literal topics; dynamic/computed topic names are still skipped. |
| Producer extraction | 🟢 `partial` | `2026-06-13` | 4996 | `internal/engine/kafka_edges.go`<br>`internal/engine/kafka_edges_test.go` | #4996: synthesizeCSharpKafka handles Confluent.Kafka IProducer.Produce("topic", ...) and ProduceAsync("topic", ...) producer sites, emitting PUBLISHES_TO from the enclosing method to topic:kafka:<name> (messaging_layer=confluent-kafka-dotnet). Honest-partial: literal topic first-arg only. |
| Topic attribution | ✅ `full` | `2026-06-13` | 4996 | `internal/engine/kafka_edges.go`<br>`internal/links/topic_pass.go` | #4996: topic is the explicit string-literal first argument (Produce/ProduceAsync/Subscribe), so attribution is FULL for literals — topic:kafka:<name> SCOPE.MessageTopic nodes join a C# producer to a consumer (any language) cross-repo via topic_pass.go (channel=kafka). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.broker.kafka-dotnet ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
