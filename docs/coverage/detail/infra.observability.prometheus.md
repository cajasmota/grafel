<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `infra.observability.prometheus` — Prometheus

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [observability](../by-category/observability.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `log_extraction` | — `not_applicable` | — | — | — | — |
| `metric_extraction` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/rules/_engine/comment_marker_extractor.yaml` |
| `trace_extraction` | — `not_applicable` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update infra.observability.prometheus ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
