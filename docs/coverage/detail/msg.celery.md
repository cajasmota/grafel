<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.celery` — Celery (Python task queue)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/scheduled_jobs_edges.go` | — |
| Producer extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/scheduled_jobs_edges.go`<br>`internal/extractors/python/celery.go` | — |
| Topic attribution | ✅ `full` | `2026-05-28` | — | `internal/links/topic_pass.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.celery ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
