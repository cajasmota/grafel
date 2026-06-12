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
| Producer extraction | ✅ `full` | — | 5015 | `internal/custom/csharp/extractors_test.go`<br>`internal/custom/csharp/hangfire.go`<br>`internal/custom/csharp/hangfire_schedule_test.go` | #4922: custom_csharp_hangfire emits the producer side -- BackgroundJob.Enqueue(() => T.M()) and the typed Enqueue<T>(x => x.M()) -> SCOPE.Operation(task_enqueue); BackgroundJob.Schedule(() => T.M(), delay) -> SCOPE.Operation(task_schedule); RecurringJob.AddOrUpdate -> SCOPE.Pattern(recurring_job). All PRODUCES carrying task:hangfire:<Type>.<Method>. #5015: now FULL on the producer axis -- (a) Hangfire Cron.* fluent helpers (Cron.Daily/Hourly/Weekly/Monthly/Yearly/Minutely) and raw 5/6-field cron string literals parse onto the recurring_job node as cron_expression + schedule_type (interval helpers like Cron.MinuteInterval(n) record schedule_type=interval, arg runtime-dependent); (b) dynamic / non-literal call-sites -- captured-variable job-ids, nested member-access lambda bodies (a.b.Method()), method-group enqueue -- no longer drop: they emit an honest unresolved producer (pattern_type recurring_dynamic / enqueue_dynamic, resolution=unresolved) so the PRODUCES call stays in-graph, with cron still parsed when present. The schedule-carrying ScheduledJob node + TRIGGERS edge remains the complementary scheduled_jobs_edges.go path. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.hangfire-recurring ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
