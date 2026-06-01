<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.framework.broadway` — Broadway (Elixir data pipelines)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🟢 `partial` | `2026-05-31` | — | `internal/custom/elixir/broadway.go`<br>`internal/custom/elixir/broadway_test.go` | handle_message/3 and handle_batch/4 pipeline stages emitted as SCOPE.Operation/handler flow roots bound to the use Broadway module. |
| Producer extraction | 🟢 `partial` | `2026-05-31` | — | `internal/custom/elixir/broadway.go`<br>`internal/custom/elixir/broadway_test.go` | Broadway producer module (BroadwayKafka/BroadwaySQS/OffBroadway...) parsed from producer: [module: {Mod, opts}]; broker normalised (kafka|sqs|rabbitmq|gcp-pubsub|kinesis). |
| Topic attribution | 🟢 `partial` | `2026-05-31` | — | `internal/custom/elixir/broadway.go`<br>`internal/custom/elixir/broadway_test.go` | Ingress topics/queues parsed from producer topics:/topic:/queue:/queue_url: options emitted as SCOPE.MessageTopic with broker + ingress=true + owning pipeline. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.framework.broadway ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
