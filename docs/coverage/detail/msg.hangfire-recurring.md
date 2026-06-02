<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.hangfire-recurring` — Hangfire RecurringJob (.NET scheduled jobs)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | — | 3628 | `internal/engine/scheduled_jobs_edges.go`<br>`internal/engine/scheduled_jobs_edges_test.go` | #3628 area: RecurringJob.AddOrUpdate("id", () => Type.Method(), SCHEDULE) and the generic AddOrUpdate<T>("id", x => x.Method(), SCHEDULE) form emit a SCOPE.ScheduledJob (hangfire_recurring:<id>) carrying the schedule (Cron.* factory or literal cron string) with a TRIGGERS edge to the handler method. Complements custom_csharp_hangfire which records a SCOPE.Pattern node but drops the schedule. Honest-partial: enqueue (BackgroundJob.Enqueue/Schedule) producer edges stay in the custom extractor; dynamic schedules not parsed. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.hangfire-recurring ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
