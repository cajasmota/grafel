<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.bullmq` — BullMQ / bull (Node task queue)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [javascript](../by-language/javascript.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `consumer_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/scheduled_jobs_edges.go` |
| `producer_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/scheduled_jobs_edges.go` |
| `topic_attribution` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/topic_pass.go` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.bullmq ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
