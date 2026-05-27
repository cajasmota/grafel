<!-- DO NOT EDIT — generated from docs/coverage.json by 'go run ./tools/coverage gen' -->
# `msg.broker.kafka` — Apache Kafka

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `consumer_extraction` | `full` | `2026-05-28` | — | — | `internal/engine/kafka_edges.go` |
| `producer_extraction` | `full` | `2026-05-28` | — | — | `internal/engine/kafka_edges.go` |
| `topic_attribution` | `full` | `2026-05-28` | — | — | `internal/links/topic_pass.go` |

## Provenance

This record is sourced from `docs/coverage.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.broker.kafka ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
