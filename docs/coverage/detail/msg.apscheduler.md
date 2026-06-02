<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.apscheduler` — APScheduler (Python advanced scheduler)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | — | 3628 | `internal/engine/scheduled_jobs_edges.go`<br>`internal/engine/scheduled_jobs_edges_test.go` | #3628 area: scheduler.add_job(fn, 'cron'/'interval', ...) and the @scheduler.scheduled_job('cron'|'interval', hour=.., minutes=..) decorator form both emit a SCOPE.ScheduledJob (apscheduler:<path>:<handler>) carrying the trigger + cron/interval kwargs as schedule, with a TRIGGERS edge to the handler function. Honest-partial: dynamic handler refs and date-trigger args not fully normalized. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.apscheduler ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
