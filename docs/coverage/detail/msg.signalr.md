<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.signalr` — SignalR

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Subcategory:** Realtime Channels
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | 🔴 `missing` | — | 3682 | — | Client-side SignalR (HubConnectionBuilder().WithUrl) extraction not yet implemented; producer-side endpoints land in #3682. |
| Producer extraction | ✅ `full` | `2026-06-14` | — | `internal/engine/realtime_endpoint_synthesis.go`<br>`internal/engine/realtime_endpoint_synthesis_test.go` | #3682 (epic #3628 area #7): SignalR Hubs emit endpoint-shaped http_endpoint_definition entities. Each client-invokable public Task/void method on a 'class XHub : Hub' becomes a realtime endpoint http:WS:<base>/<Method> (transport=signalr) with a HANDLES edge Class:Hub.Method -> endpoint; app.MapHub<XHub>("/path") rebinds the base path, else default /<hub-without-suffix>. Lifecycle overrides (OnConnectedAsync/OnDisconnectedAsync/Dispose) excluded. #5003: [HubMethodName("wire")] is now honored — the endpoint path uses the client-facing wire name (http:WS:<base>/<wire>) instead of the C# method name, with hub_method_name stamped on the endpoint; the HANDLES edge still references the real C# method symbol; the override may be stacked with other attributes (e.g. [Authorize]). Honest: hub method discovery is regex-scoped to the class body; no cross-assembly client-invoke verification; single-line attribute+signature (on one line) is deferred (#5003 follow-up). #5095: outbound server→client push is now modeled (see topic_attribution). |
| Topic attribution | ✅ `full` | — | 5095 | `internal/engine/ws_channel_grouping.go`<br>`internal/engine/ws_channel_grouping_test.go` | #5095 (follow-up #5003): SignalR server→client outbound push — Clients.All/Caller/Others/Group("g")/Client(id)/User(id).SendAsync("evt", ...)/InvokeAsync (also via _hubContext.Clients.*) is modeled as a BROADCASTS_TO edge Function:<caller> -> SCOPE.Channel:signalr:<scope>, with the pushed event name (method) + signalr_scope on the edge so the graph answers "which events does the server push, to which client scope, and who subscribes". A literal Group("g") folds into the node id (signalr:group:g); a dynamic Group(var)/Client(id) degrades to the bare scope node (signalr:group) and carries no scope_arg (honest-partial). Lang-gated to csharp. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.signalr ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
