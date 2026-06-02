<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.node-schedule` — node-schedule (Node scheduled jobs)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | — | 3628 | `internal/engine/scheduled_jobs_edges.go`<br>`internal/engine/scheduled_jobs_edges_test.go` | #3628 area: schedule.scheduleJob('EXPR', handler) emits a SCOPE.ScheduledJob (node_schedule:<path>:<expr>) carrying the cron string as schedule, with a TRIGGERS edge to the named handler. Honest-partial: inline function literals and RecurrenceRule object rules yield the job node but no handler edge / no schedule string. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.node-schedule ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
