<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.whenever` — whenever (Ruby cron / config/schedule.rb)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | — | 3628 | `internal/engine/scheduled_jobs_edges.go`<br>`internal/engine/scheduled_jobs_edges_test.go` | #3628 area: every <interval> do runner/rake/command '...' end blocks in config/schedule.rb emit a SCOPE.ScheduledJob (whenever:<path>:<schedule>) carrying the interval/cron as schedule and a TRIGGERS edge to the first runner/rake/command descriptor. Honest-partial: only config/schedule.rb files are scanned; multi-line job bodies use the first descriptor as handler proxy. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.whenever ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
