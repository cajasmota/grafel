<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.async-nats` — async-nats (NATS)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | `2026-06-12` | 5008 | `internal/engine/nats_edges.go`<br>`internal/engine/nats_edges_test.go` | async-nats client.subscribe("subject").await and client.queue_subscribe("subject","queue").await -> SUBSCRIBES_TO (queue group recorded as edge/entity prop); jetstream subscribes flagged jetstream=true. SCOPE.Queue keyed nats:<subject> joins cross-repo, attributed to the enclosing fn. Partial: literal string subjects only; caller attribution is same-file nearest-fn. |
| Producer extraction | 🟢 `partial` | `2026-06-12` | 5008 | `internal/engine/nats_edges.go`<br>`internal/engine/nats_edges_test.go` | async-nats client.publish("subject",payload).await and client.request("subject",...).await (request_reply) -> PUBLISHES_TO; jetstream.publish("subject",...) flagged jetstream=true. SCOPE.Queue keyed nats:<subject> joins cross-repo via the import-channel linker, attributed to the enclosing fn. Partial: only literal string subjects are resolved (impl ToSubject variables / format!() are skipped); caller attribution is same-file nearest-fn. |
| Topic attribution | 🟢 `partial` | `2026-06-12` | 5008 | `internal/engine/nats_edges.go`<br>`internal/engine/nats_edges_test.go` | Subjects keyed nats:<subject> as SCOPE.Queue entities matched across producer/consumer and across repos by shared synthetic ID. Wildcard subjects (orders.*, inventory.>) preserved as-is. Partial: only literal subjects are attributed; dynamic subjects are skipped. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.async-nats ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
