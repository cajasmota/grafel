<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.rufus-scheduler` — rufus-scheduler (Ruby in-process scheduler)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | — | 3628 | `internal/engine/scheduled_jobs_edges.go`<br>`internal/engine/scheduled_jobs_edges_test.go` | #3628 area: scheduler.cron/every/interval/in/at 'EXPR' do ... end emits a SCOPE.ScheduledJob (rufus:<path>:<expr>) carrying '<kind> <expr>' as schedule and a TRIGGERS edge to the first bare method call in the block body. Honest-partial: block-body handler is a heuristic first-call proxy; multi-statement blocks pick the first call. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.rufus-scheduler ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
