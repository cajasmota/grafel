<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `analysis.orchestration.scheduled-jobs` — Scheduled-job / cron entry-points (SCOPE.ScheduledJob + TRIGGERS)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [platform](../by-category/platform.md)
- **Subcategory:** App Topology & Integration
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency attribution | ✅ `full` | `2026-06-12` | — | `internal/engine/scheduled_jobs_edges.go`<br>`internal/engine/scheduled_jobs_edges_test.go` | Emits TRIGGERS edges (RelationshipKindTriggers) from each SCOPE.ScheduledJob to its handler function, plus #1404 Celery PUBLISHES_TO topology from call sites (.delay()/.apply_async()/send_task()/signature()) enclosing-fn -> SCOPE.ScheduledJob, and ENQUEUES edges for Sidekiq-style perform_async. Honest: only statically-named handlers/schedules are linked; dynamic cron exprs/handlers are not fabricated. |
| Resource extraction | ✅ `full` | `2026-06-12` | — | `internal/engine/detector.go`<br>`internal/engine/scheduled_jobs_edges.go`<br>`internal/engine/scheduled_jobs_edges_test.go` | #728 area: append-only detector pass applyScheduledJobEdges (detector.go) synthesises SCOPE.ScheduledJob entities (kind "SCOPE.ScheduledJob", ID scheduledJobKind:<jobID>) across 12+ frameworks: Python Celery (@celery.task/@app.task + celery_beat_schedule), APScheduler (scheduler.add_job trigger=cron), schedule lib; Node node-cron, Bull/BullMQ repeat:{cron}; Quarkus/MicroProfile & Spring @Scheduled (cron/fixedRate/fixedDelay), Java Quartz JobBuilder+cronSchedule; Go robfig/cron AddFunc; AWS EventBridge rate()/cron() Lambda event sources; Kubernetes CronJob spec.schedule; GitHub Actions schedule:/cron:; Ruby Sidekiq/sidekiq-cron/Resque/DelayedJob (perform handlers). 35 value-asserting tests in scheduled_jobs_edges_test.go. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update analysis.orchestration.scheduled-jobs ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
