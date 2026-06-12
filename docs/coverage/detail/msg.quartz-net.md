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
| Topic attribution | 🟢 `partial` | — | 3628 | `internal/custom/csharp/quartz_net.go` | #4922: trigger identity is recovered from .WithIdentity("name") onto the trigger SCOPE.Operation, and job_type onto the job_builder. Honest-partial: cron/interval schedule strings and JobKey group are not parsed into the node (mirrors the scheduled_jobs schedule gap, #3628). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.quartz-net ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
