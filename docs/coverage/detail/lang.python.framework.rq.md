<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.rq` — RQ (Redis Queue)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Task Queue
- **Capability cells:** 28

## Capabilities


### Tasks

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Task extraction | ⚠️ `partial` | `2026-05-28` | — | `internal/custom/python/rq.go` | — |
| Task routing | ⚠️ `partial` | `2026-05-28` | — | `internal/custom/python/rq.go` | — |

### Schedule

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Schedule extraction | ❌ `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/2983) | — | — |

### Broker

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Broker binding | ❌ `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/2983) | — | — |
| Result backend binding | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Reliability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Retry policy extraction | ❌ `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/2983) | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/pytest.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go` | — |
| DB effect | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/effect_sinks_python.go` | — |
| Dead code detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/def_use_python.go` | — |
| Env fallback recognition | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go` | — |
| Fs effect | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/effect_sinks_python.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/effect_sinks_python.go` | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/python.go` | — |
| Module cycle detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/effect_sinks_python.go` | — |
| Pure function tagging | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/payload_shapes_python.go` | — |
| Response shape extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/payload_shapes_python.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/taint_sites_python.go` | — |
| Schema drift detection | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/payload_shapes_python.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/taint_sites_python.go` | — |
| Taint source detection | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/taint_sites_python.go` | — |
| Template pattern catalog | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/template_pattern_python.go` | — |
| Vulnerability finding | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.rq ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
