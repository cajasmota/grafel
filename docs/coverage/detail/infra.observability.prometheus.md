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
| Metric extraction | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3762) | `internal/engine/rules/_engine/comment_marker_extractor.yaml`<br>`internal/extractors/python/observability.go` | #3628 area #11: prometheus_client Python metrics emit INSTRUMENTS edges (enclosing op -> metric:<name> stub). A module-level metric declaration X = Counter|Gauge|Summary|Histogram|Info|Enum("name", ...) is registered, then @X.time()/@X.count_exceptions()/@X.track_inprogress() decorators and X.inc()/dec()/observe()/set() body calls resolve to metric:name with the metric name captured. Honest-partial: a non-literal metric name yields traced=true+dynamic=true; a .inc()/.time() on a variable that is NOT a known module-level metric is NOT emitted (precision). Only Python is covered. |
| Trace extraction | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.observability.prometheus ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
