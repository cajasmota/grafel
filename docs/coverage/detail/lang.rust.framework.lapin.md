<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.lapin` — lapin (AMQP/RabbitMQ)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | `2026-05-31` | 3558 | `internal/engine/rabbitmq_edges.go`<br>`internal/engine/rabbitmq_edges_test.go` | lapin channel.basic_publish("exchange","routing_key",...) -> PUBLISHES_TO (routing_key as queue identity, exchange recorded as edge prop) and basic_consume("queue",...) -> SUBSCRIBES_TO, plus queue_declare("queue",...) node; SCOPE.Queue keyed rabbitmq:<name> joins cross-repo, attributed to the enclosing fn. Partial: only literal exchange/queue/routing-key string args are resolved; dynamic/expression args and cross-file fn attribution are not modelled. |
| Producer extraction | 🟢 `partial` | `2026-05-31` | 3558 | `internal/engine/rabbitmq_edges.go`<br>`internal/engine/rabbitmq_edges_test.go` | lapin channel.basic_publish("exchange","routing_key",...) -> PUBLISHES_TO (routing_key as queue identity, exchange recorded as edge prop) and basic_consume("queue",...) -> SUBSCRIBES_TO, plus queue_declare("queue",...) node; SCOPE.Queue keyed rabbitmq:<name> joins cross-repo, attributed to the enclosing fn. Partial: only literal exchange/queue/routing-key string args are resolved; dynamic/expression args and cross-file fn attribution are not modelled. |
| Topic attribution | 🟢 `partial` | `2026-05-31` | 3558 | `internal/engine/rabbitmq_edges.go`<br>`internal/engine/rabbitmq_edges_test.go` | lapin channel.basic_publish("exchange","routing_key",...) -> PUBLISHES_TO (routing_key as queue identity, exchange recorded as edge prop) and basic_consume("queue",...) -> SUBSCRIBES_TO, plus queue_declare("queue",...) node; SCOPE.Queue keyed rabbitmq:<name> joins cross-repo, attributed to the enclosing fn. Partial: only literal exchange/queue/routing-key string args are resolved; dynamic/expression args and cross-file fn attribution are not modelled. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.lapin ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
