<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.bullmq` — BullMQ / bull (Node task queue)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/scheduled_jobs_edges.go` | — |
| Producer extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/scheduled_jobs_edges.go` | — |
| Topic attribution | ✅ `full` | `2026-05-28` | 2865 | `internal/engine/bullmq_edges.go`<br>`internal/engine/bullmq_edges_test.go`<br>`internal/links/topic_pass.go`<br>`internal/links/topic_pass_test.go`<br>`testdata/fixtures/typescript/bullmq_topic_attribution.ts` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.bullmq ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
