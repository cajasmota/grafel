<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.broker.sqs` — AWS SQS

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `consumer_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/sqs_edges.go` |
| `producer_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/sqs_edges.go` |
| `topic_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/sqs_edges.go` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.broker.sqs ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
