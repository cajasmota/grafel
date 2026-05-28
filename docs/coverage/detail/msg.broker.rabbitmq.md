<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.broker.rabbitmq` — RabbitMQ

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rabbitmq_edges.go` | — |
| Producer extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rabbitmq_edges.go` | — |
| Topic attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/rabbitmq_edges.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.broker.rabbitmq ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
