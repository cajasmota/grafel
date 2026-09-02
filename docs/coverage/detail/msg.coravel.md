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
| Consumer extraction | ✅ `full` | `2026-06-13` | 5075 | `internal/custom/csharp/coravel.go`<br>`internal/custom/csharp/coravel_test.go` | #5075 (spun out of #5016/#4969): custom_csharp_coravel extracts the consumer side -- class X : IInvocable -> SCOPE.Service(invocable) with task_id task:coravel:<X> (the Coravel analogue of a Quartz IJob). The invocable name + task_id join with the producer Schedule<X>()/QueueInvocable<X>() sites. #6767 (2026-09-03) -- RE-DERIVED against what is emitted. This record used to say the invocable 'CONSUMES', which was true only of an inert `edge_kind: CONSUMES` entity PROPERTY (removed in #6767). NO CONSUMES EDGE IS EMITTED by this pass; that no pass in any language emits it -- the kind is declared and read by four consumers, and nothing produces it -- is a repo-wide fact recorded and grep-verified in ADR-0028, not re-verified by these tests (known gap; the honest form needs the two-hop work-unit node #6741 declined to build). The consumer entity is edgeless and joins its producer by task_id only. |
| Producer extraction | ✅ `full` | `2026-06-13` | 5075 | `internal/custom/csharp/coravel.go`<br>`internal/custom/csharp/coravel_test.go` | #5075: producer side -- scheduler.Schedule<T>()/ScheduleAsync<T>() and anonymous Schedule(() => ...) -> SCOPE.Operation(schedule) carrying task:coravel:<T>; IQueue.QueueInvocable<T>()/QueueInvocableWithPayload and QueueAsyncTask/QueueTask -> SCOPE.Operation(queue); IMailer.Send/SendAsync(new XMailable(...)) -> SCOPE.Operation(mail). Schedule<T> and the IInvocable consumer converge on task:coravel:<T>. Honest-partial: a Send(new T()) whose type does not end in 'Mailable' is not stamped as a mail surface. #6767 (2026-09-03) -- RE-DERIVED against what is emitted. Until #6767 this pass emitted ZERO relationships: the word PRODUCES here named an inert `edge_kind` entity PROPERTY. It is now a real edge, but only where the dispatch site NAMES its work unit -- Schedule<T>()/ScheduleAsync<T>()/ScheduleWithParams<T>() and QueueInvocable<T>() emit PRODUCES to Class:<T> via csJobProducesEdge, and Send(new XMailable()) emits PRODUCES to Class:<XMailable> via csMessageProducesEdge. The mail edge carries NO task_id: task_id is a join key, nothing in the tree mints a Mailable-side entity for one to join with, and a key with one end is not a key. The mailer also DEFERS to whichever service bus owns the file (csSharedDispatchVerbOwner), because `.Send(new XMailable())` is the same bytes MassTransit / MediatR / NServiceBus match. The two ANONYMOUS forms, Schedule(() => ...) and QueueAsyncTask/QueueTask, name no invocable and deliberately emit NO edge -- so 'every Coravel producer carries a PRODUCES edge' would be false; what is true is that every producer that names its target does. Pinned by TestCoravelScheduleAndQueueProduceTheirInvocable and TestCoravelAnonymousDispatchEmitsNoProduces. |
| Topic attribution | ✅ `full` | `2026-06-13` | 5075 | `internal/custom/csharp/coravel.go`<br>`internal/custom/csharp/coravel_test.go` | #5075 (parallels the Quartz.NET schedule-string parse from #4969): the Coravel fluent schedule chain is scanned (bounded to the next ';' so co-located schedules don't bleed) and the cadence is parsed onto the schedule SCOPE.Operation. .Cron("...") -> schedule_type=cron + cron_expression; .DailyAt("hh:mm") -> schedule_type=daily + daily_at; EveryMinute/EveryFiveMinutes/.../Hourly named tokens -> schedule_type=interval (or daily/weekly/monthly) + frequency + interval_seconds; EveryNMinutes/EverySeconds -> interval_seconds normalised from the literal magnitude. Honest-partial: a Schedule<T>() with no recognised frequency token records the producer without a resolved cadence (schedule_type omitted, not guessed). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.coravel ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
