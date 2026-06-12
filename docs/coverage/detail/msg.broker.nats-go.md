<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.broker.nats-go` — NATS — Go (nats.go / JetStream)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-06-12` | — | `internal/engine/nats_edges.go`<br>`internal/engine/nats_edges_test.go` | #4923: synthesizeGoNATS resolves nc.Subscribe/QueueSubscribe consumer sites -> SUBSCRIBES_TO topic:nats:<subject>. Honest limit: string-literal subjects (wildcards/dynamic subjects not normalised). |
| Producer extraction | ✅ `full` | `2026-06-12` | — | `internal/engine/nats_edges.go`<br>`internal/engine/nats_edges_test.go` | #4923 (de-dupe of multi msg.broker.nats — Go topology was invisible to a Go auditor): synthesizeGoNATS (nats_edges.go, case "go") resolves nc.Publish(subject,...)/JetStream js.Publish producer sites (messaging_layer=nats.go) -> PUBLISHES_TO topic:nats:<subject> from the enclosing func. |
| Topic attribution | ✅ `full` | `2026-06-12` | — | `internal/engine/nats_edges.go`<br>`internal/links/topic_pass.go` | #4923: literal NATS subjects become topic:nats:<subject> nodes joined producer->consumer cross-repo via topic_pass.go (channel=nats). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.broker.nats-go ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
