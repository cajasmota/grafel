<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.observability.opentelemetry` — OpenTelemetry (OTEL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [observability](../by-category/observability.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `log_extraction` | ❌ `missing` | — | — | — | — |
| `metric_extraction` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/event_bus_edges.go` |
| `trace_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/event_flow.go`<br>`internal/engine/process_flow.go` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.observability.opentelemetry ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
