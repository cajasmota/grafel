<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.broker.redis` — Redis pub/sub & streams

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Brokers
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-06-12` | 5016 | `internal/engine/redis_pubsub_edges.go`<br>`internal/engine/redis_pubsub_edges_test.go` | #5016: C# (StackExchange.Redis ISubscriber) added via synthesizeCSharpRedisPubSub — .Subscribe/.SubscribeAsync("chan" | RedisChannel.Literal/Pattern(...)) -> SUBSCRIBES_TO the canonical channel:redis-pubsub:<name> SCOPE.Queue, so a C# subscriber pairs cross-repo with publishers in any covered language. Wildcard (RedisChannel.Pattern / `*`) flagged is_pattern. Languages: python, js/ts, go, ruby, java/kotlin (Spring), elixir (Redix), csharp. |
| Producer extraction | ✅ `full` | `2026-06-12` | 5016 | `internal/engine/redis_pubsub_edges.go`<br>`internal/engine/redis_pubsub_edges_test.go` | #5016: C# (StackExchange.Redis ISubscriber) added via synthesizeCSharpRedisPubSub — .Publish/.PublishAsync("chan" | RedisChannel.Literal(...)) -> PUBLISHES_TO the canonical channel:redis-pubsub:<name> SCOPE.Queue from the enclosing class/method. Honest-partial: dynamic/interpolated channels honest-skipped. Languages: python, js/ts, go, ruby, java/kotlin, elixir, csharp. |
| Topic attribution | ✅ `full` | `2026-06-12` | 5016 | `internal/engine/redis_pubsub_edges.go`<br>`internal/engine/redis_pubsub_edges_test.go` | #5016: C# pub/sub channels mint the SAME channel:redis-pubsub:<name> SCOPE.Queue node as every other language, so topic_pass.go joins C# producers/consumers to cross-language counterparts (broker=redis). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.broker.redis ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
