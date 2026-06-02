<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.observability.newrelic` — New Relic

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [observability](../by-category/observability.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | — | — | — |
| Metric extraction | 🔴 `missing` | — | — | — | — |
| Trace extraction | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3762) | `internal/extractors/python/observability.go` | #3628 area #11: New Relic Python agent trace decorators emit INSTRUMENTS edges (enclosing op -> span:<name> stub). @newrelic.agent.function_trace() / @function_trace() / @background_task() / @web_transaction() default the span name to the function name; name="..." uses the explicit name. Honest-partial: dynamic names emit traced=true+dynamic=true without fabrication; only Python is covered. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.observability.newrelic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
