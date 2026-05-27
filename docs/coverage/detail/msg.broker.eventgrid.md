<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.broker.eventgrid` — Azure Event Grid

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `consumer_extraction` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/event_bus_edges.go` |
| `producer_extraction` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/event_bus_edges.go` |
| `topic_attribution` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/event_bus_edges.go` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.broker.eventgrid ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
