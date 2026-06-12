<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.broker.rabbitmq-go` — RabbitMQ — Go (amqp091-go)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | `2026-06-12` | — | `internal/engine/rabbitmq_edges.go`<br>`internal/engine/rabbitmq_edges_test.go` | #4923: synthesizeGoRabbitMQ resolves channel.Consume / QueueDeclare consumer sites -> SUBSCRIBES_TO. Honest-partial: literal queue/routing-key only. |
| Producer extraction | 🟢 `partial` | `2026-06-12` | — | `internal/engine/rabbitmq_edges.go`<br>`internal/engine/rabbitmq_edges_test.go` | #4923 (de-dupe of multi msg.broker.rabbitmq): synthesizeGoRabbitMQ (rabbitmq_edges.go, case "go") resolves amqp091-go channel.Publish producer sites (messaging_layer=amqp091-go) -> PUBLISHES_TO from the enclosing func, keyed by routing-key/queue literal. Honest-partial: exchange+routing-key composition and dynamic keys not fully modelled. |
| Topic attribution | 🟢 `partial` | `2026-06-12` | — | `internal/engine/rabbitmq_edges.go`<br>`internal/links/topic_pass.go` | #4923: amqp091-go routing-key/queue literals become topic nodes joined producer->consumer via topic_pass.go (channel=rabbitmq). Honest-partial: literal keys only; exchange topology not fully decomposed. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.broker.rabbitmq-go ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
