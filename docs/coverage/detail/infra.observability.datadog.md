<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.observability.datadog` — Datadog

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [observability](../by-category/observability.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | — | — | — |
| Metric extraction | 🔴 `missing` | — | — | — | — |
| Trace extraction | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3762) | `internal/extractors/python/observability.go` | #3628 area #11: ddtrace span-creation sites in Python emit INSTRUMENTS edges (enclosing op -> span:<name> stub). Decorator @tracer.wrap() defaults the span name to the function name; @tracer.wrap(name="...")/resource="..." use the explicit name; manual tracer.trace("op") body calls carry the first positional name. Honest-partial: dynamic span names emit traced=true+dynamic=true without a fabricated name; only Python is covered (Go ddtrace.StartSpan / JS not yet). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.observability.datadog ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
