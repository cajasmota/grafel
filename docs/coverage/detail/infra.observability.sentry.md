<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.observability.sentry` — Sentry

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [observability](../by-category/observability.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | — | — | — |
| Metric extraction | 🔴 `missing` | — | — | — | — |
| Trace extraction | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3762) | `internal/extractors/python/observability.go` | #3628 area #11: Sentry Python performance-tracing sites emit INSTRUMENTS edges (enclosing op -> span:<name> stub). Bare @sentry_sdk.trace / @sentry.trace decorators key the span on the function name; sentry_sdk.start_transaction(...) / start_span(...) body calls take the name from name=/op=/description= kwargs. Honest-partial: dynamic names emit traced=true+dynamic=true without fabrication; only Python is covered. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.observability.sentry ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
