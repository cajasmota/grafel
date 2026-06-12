<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.go-cron` — robfig/cron (Go scheduler)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Schedulers
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | `2026-06-12` | 4923 | `internal/engine/scheduled_jobs_edges.go`<br>`internal/engine/scheduled_jobs_edges_test.go` | #4923 (de-dupe — Go robfig/cron was implemented but had no Go record): synthesizeGoCron (scheduled_jobs_edges.go, case "go") matches c.AddFunc("EXPR", handler) on a robfig/cron scheduler and emits a SCOPE.ScheduledJob (go_cron:<path>:<expr>, framework=go_cron) carrying the cron expression, with a TRIGGERS edge to the named handler func. Value-asserted TestScheduledJobs_GoCron_AddFunc. Honest-partial: inline func() literals emit the job node without a TRIGGERS target; AddJob(spec, Job) interface form and dynamic expressions not yet matched. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.go-cron ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
