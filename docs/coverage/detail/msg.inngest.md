<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.inngest` — Inngest (durable functions / event-driven jobs)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Task Queues
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | `2026-06-24` | 5480 | `internal/custom/javascript/inngest.go`<br>`internal/custom/javascript/inngest_test.go`<br>`testdata/fixtures/typescript/inngest_functions.ts` | #5480 (epic #5479, Phase 1 item 1 — ENTITIES only, edges are #5482/#5483/#5484): custom_js_inngest extracts each inngest.createFunction({ id|name }, { event|cron }, handler) call site (modern object-config and the older positional trigger form) as one SCOPE.Function (subtype inngest_function) named after the config id/name — the consumer side of an Inngest event, the durable-function analogue of a BullMQ Worker. The trigger is captured as an attribute only (trigger_event + trigger_type=event, or trigger_cron + trigger_type=cron); function_id/receiver/framework=inngest also recorded. Attribution-gated on an `inngest` import or a receiver named `inngest`. Honest-partial: a createFunction with no literal id/name (dynamically named) is skipped, and the EMITS/TRIGGERS edges that wire trigger_event to producers/topics are not yet emitted (#5482/#5483). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.inngest ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
