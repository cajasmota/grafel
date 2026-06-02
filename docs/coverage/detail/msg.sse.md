<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `msg.sse` — Server-Sent Events

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [message_broker](../by-category/message_broker.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Consumer extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/sse_edges.go` | — |
| Producer extraction | ✅ `full` | `2026-06-02` | — | `internal/engine/realtime_endpoint_synthesis.go`<br>`internal/engine/sse_edges.go` | #3682 (epic #3628 area #7): in addition to Stream + STREAMS_TO edges, the producer side now ALSO emits endpoint-shaped http_endpoint_definition entities (verb=SSE, route_path, realtime=true, transport=sse) with a HANDLES edge to the handler, so endpoints/find surface SSE endpoints. Frameworks: NestJS @Sse('path'), FastAPI sse-starlette EventSourceResponse / StreamingResponse(text/event-stream) on an @app.get handler. Honest-partial for dynamic paths. |
| Topic attribution | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update msg.sse ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
