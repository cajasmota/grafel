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
| Trace extraction | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3768) | `internal/extractors/golang/observability.go`<br>`internal/extractors/javascript/observability.go`<br>`internal/extractors/python/observability.go` | #3628 area #11: ddtrace span-creation sites now emit INSTRUMENTS edges (enclosing op -> span:<name> stub) in Python, Go and JS/TS. Python (#3762): @tracer.wrap()/tracer.trace("op"). Go: tracer.StartSpan("web.request") and tracer.StartSpanFromContext(ctx, "op") take the first string-literal arg as the span name. JS/TS dd-trace: tracer.trace('op', cb) / tracer.wrap('op', fn) take the first string-literal arg. Honest-partial: dynamic span names emit traced=true+dynamic=true keyed on the enclosing fn without a fabricated name; .StartSpan/.trace on a non-tracer receiver is not emitted (precision). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.observability.datadog ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
