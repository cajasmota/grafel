<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.quartz-net` — Quartz.NET (.NET job scheduler)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Schedulers
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/extractors_test.go`<br>`internal/custom/csharp/quartz_net.go` | #4922: quartz_net.go (registered custom_csharp_quartz_net) extracts the consumer side — class X : IJob -> SCOPE.Service(job_class) CONSUMES task:quartz.net:<X>, and [DisallowConcurrentExecution] -> SCOPE.Pattern(concurrency_policy). Was fully implemented + init-registered + tested but entirely undocumented (registry search 'quartz' was empty). |
| Producer extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/extractors_test.go`<br>`internal/custom/csharp/quartz_net.go` | #4922: producer side — JobBuilder.Create<T>() -> SCOPE.Operation(job_builder) PRODUCES task:quartz.net:<T>; TriggerBuilder.Create().WithIdentity(name) -> SCOPE.Operation(trigger); scheduler.ScheduleJob(job,trigger) -> SCOPE.Operation(schedule_job). job_builder and the IJob consumer converge on task:quartz.net:<T>. |
| Topic attribution | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/quartz_net.go`<br>`internal/custom/csharp/quartz_net_schedule_test.go` | #4969 (was partial #4922/#3628): the trigger fluent chain is now scanned (bounded to the next ';' so co-located triggers don't bleed) and the schedule string is parsed onto the trigger SCOPE.Operation. .WithCronSchedule("...") / CronScheduleBuilder.CronSchedule("...") -> schedule_type=cron + cron_expression; .WithSimpleSchedule(...) -> schedule_type=simple with interval_seconds (normalised from WithIntervalInSeconds/Minutes/Hours and WithInterval(TimeSpan.From{Seconds,Minutes,Hours,Days}(n))) + repeat_forever. JobKey/TriggerKey group from .WithIdentity("name","group") -> job_group (on job_builder) / trigger_group (on trigger), plus trigger_name and job_type as before. Closes the #3628 schedule-string gap for Quartz.NET. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.quartz-net ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
