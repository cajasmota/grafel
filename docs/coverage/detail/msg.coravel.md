<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.coravel` — Coravel (.NET task scheduler / queue / mailer)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Schedulers
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-06-13` | 5075 | `internal/custom/csharp/coravel.go`<br>`internal/custom/csharp/coravel_test.go` | #5075 (spun out of #5016/#4969): custom_csharp_coravel extracts the consumer side -- class X : IInvocable -> SCOPE.Service(invocable) CONSUMES task:coravel:<X> (the Coravel analogue of a Quartz IJob). The invocable name + task_id join with the producer Schedule<X>()/QueueInvocable<X>() sites. |
| Producer extraction | ✅ `full` | `2026-06-13` | 5075 | `internal/custom/csharp/coravel.go`<br>`internal/custom/csharp/coravel_test.go` | #5075: producer side -- scheduler.Schedule<T>()/ScheduleAsync<T>() and anonymous Schedule(() => ...) -> SCOPE.Operation(schedule) PRODUCES task:coravel:<T>; IQueue.QueueInvocable<T>()/QueueInvocableWithPayload and QueueAsyncTask/QueueTask -> SCOPE.Operation(queue); IMailer.Send/SendAsync(new XMailable(...)) -> SCOPE.Operation(mail). Schedule<T> and the IInvocable consumer converge on task:coravel:<T>. Honest-partial: a Send(new T()) whose type does not end in 'Mailable' is not stamped as a mail surface. |
| Topic attribution | ✅ `full` | `2026-06-13` | 5075 | `internal/custom/csharp/coravel.go`<br>`internal/custom/csharp/coravel_test.go` | #5075 (parallels the Quartz.NET schedule-string parse from #4969): the Coravel fluent schedule chain is scanned (bounded to the next ';' so co-located schedules don't bleed) and the cadence is parsed onto the schedule SCOPE.Operation. .Cron("...") -> schedule_type=cron + cron_expression; .DailyAt("hh:mm") -> schedule_type=daily + daily_at; EveryMinute/EveryFiveMinutes/.../Hourly named tokens -> schedule_type=interval (or daily/weekly/monthly) + frequency + interval_seconds; EveryNMinutes/EverySeconds -> interval_seconds normalised from the literal magnitude. Honest-partial: a Schedule<T>() with no recognised frequency token records the producer without a resolved cadence (schedule_type omitted, not guessed). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.coravel ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
