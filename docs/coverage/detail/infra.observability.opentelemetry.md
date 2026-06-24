<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.observability.opentelemetry` — OpenTelemetry (OTEL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [observability](../by-category/observability.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 3828 | — | No log/metric/trace extraction yet for this vendor; tracked in #3828. |
| Metric extraction | 🟢 `partial` | `2026-05-28` | — | `internal/engine/event_bus_edges.go` | — |
| Trace extraction | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/grafel/issues/3689) | `internal/extractors/golang/tracing.go`<br>`internal/extractors/java/tracing.go`<br>`internal/extractors/javascript/tracing.go`<br>`internal/extractors/python/tracing.go` | OpenTelemetry span-creation sites: emits INSTRUMENTS edges (enclosing op -> span:<name> stub) for the dominant span-start idioms in Python (start_as_current_span/start_span + decorator), Go (tracer.Start), JS/TS, Java (@WithSpan + spanBuilder().startSpan()). #5500 (epic #5479) expanded the JS/TS lane to parity with Python: startSpan/startActiveSpan PLUS inline trace.getTracer(...).startSpan(...), @vercel/otel registerOTel(service), and manual context.with(trace.setSpan(...)) scopes, now ATTRIBUTION-GATED on an @opentelemetry/* or @vercel/otel import OR a tracer-like receiver (tracer/*Tracer/getTracer chain) so an unrelated .startSpan( is not matched. Honest-partial OVERALL because the cell is multi-language: the Go/Java/Python lanes retain their original (narrower) idiom set and only JS/TS carries the import-gate; dynamic/variable span names emit traced=true without a fabricated name; non-OTel vendors and other langs not yet covered. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.observability.opentelemetry ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
