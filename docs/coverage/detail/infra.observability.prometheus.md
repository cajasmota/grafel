<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.observability.prometheus` — Prometheus

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [observability](../by-category/observability.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | — `not_applicable` | — | — | — | — |
| Metric extraction | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3768) | `internal/engine/rules/_engine/comment_marker_extractor.yaml`<br>`internal/extractors/golang/observability.go`<br>`internal/extractors/javascript/observability.go`<br>`internal/extractors/python/observability.go` | #3628 area #11: prometheus client metrics emit INSTRUMENTS edges (enclosing op -> metric:<name> stub) in Python, Go and JS/TS. Python (#3762): module-level X = Counter|Gauge|Summary|Histogram("name") + @X.time()/X.inc()/observe(). Go: a metric var bound to prometheus.New{Counter,Gauge,Summary,Histogram}[Vec](prometheus.*Opts{Name:"x"}) (package- or fn-scope) resolves .Inc()/Dec()/Add()/Sub()/Observe()/Set() body calls to metric:x. JS/TS prom-client: a var bound to new client.{Counter,Gauge,Histogram,Summary}({name:'x'}) resolves .inc()/.dec()/.set()/.observe()/.startTimer() body calls to metric:x. Honest-partial: a non-literal metric name yields traced=true+dynamic=true; a metric method on a variable that is NOT a known metric declaration is NOT emitted (precision). |
| Trace extraction | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.observability.prometheus ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
