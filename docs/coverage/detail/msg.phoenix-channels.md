<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.phoenix-channels` — Phoenix Channels

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🔴 `missing` | — | 3682 | — | Client-side Phoenix channel.join() extraction not yet implemented; producer-side endpoints land in #3682. |
| Producer extraction | ✅ `full` | `2026-06-02` | — | `internal/engine/realtime_endpoint_synthesis.go`<br>`internal/engine/realtime_endpoint_synthesis_test.go` | #3682 (epic #3628 area #7): Phoenix 'channel "topic:*", XChannel' declarations in a Phoenix.Socket module emit endpoint-shaped http_endpoint_definition entities http:WS:<topic> (transport=channels) with a HANDLES edge Class:XChannel -> endpoint. Honest-partial: topic wildcards (room:*) are kept verbatim as the route_path; per-message handle_in event extraction is deferred. |
| Topic attribution | 🟢 `partial` | `2026-06-02` | — | `internal/engine/realtime_endpoint_synthesis.go` | Topic pattern captured verbatim as route_path (room:*); wildcard segments not expanded. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.phoenix-channels ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
