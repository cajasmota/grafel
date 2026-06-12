<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.hangfire-recurring` — Hangfire RecurringJob (.NET scheduled jobs)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Schedulers
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | — | 3628 | `internal/custom/csharp/hangfire.go`<br>`internal/engine/scheduled_jobs_edges.go`<br>`internal/engine/scheduled_jobs_edges_test.go` | #3628 area: RecurringJob.AddOrUpdate("id", () => Type.Method(), SCHEDULE) and the generic AddOrUpdate<T>("id", x => x.Method(), SCHEDULE) form emit a SCOPE.ScheduledJob (hangfire_recurring:<id>) carrying the schedule with a TRIGGERS edge to the handler. #4922: the custom_csharp_hangfire extractor ALSO recovers the consumer side -- class X : IJob/IBackgroundJob -> SCOPE.Service(job_class) and [AutomaticRetry] -> SCOPE.Pattern(retry_policy), both CONSUMES. Honest-partial: dynamic schedules not parsed. |
| Producer extraction | 🟢 `partial` | — | 3628 | `internal/custom/csharp/extractors_test.go`<br>`internal/custom/csharp/hangfire.go` | #4922: custom_csharp_hangfire emits the producer side that was previously undocumented -- BackgroundJob.Enqueue(() => T.M()) and the typed Enqueue<T>(x => x.M()) -> SCOPE.Operation(task_enqueue); BackgroundJob.Schedule(() => T.M(), delay) -> SCOPE.Operation(task_schedule); RecurringJob.AddOrUpdate -> SCOPE.Pattern(recurring_job). All PRODUCES carrying task:hangfire:<Type>.<Method>. Honest-partial: the schedule-carrying ScheduledJob node + TRIGGERS edge is the complementary scheduled_jobs_edges.go path; dynamic args not resolved. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.hangfire-recurring ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
