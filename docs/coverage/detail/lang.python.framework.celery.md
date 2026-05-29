<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.celery` — Celery (task queue)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Task Queue
- **Capability cells:** 28

## Capabilities


### Tasks

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Task extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/python/frameworks/celery.yaml`<br>`internal/extractors/python/celery.go` | — |
| Task routing | ✅ `full` | `2026-05-28` | backfill:dictionary-completeness | `internal/custom/python/celery.go`<br>`internal/extractors/python/celery.go` | — |

### Schedule

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Schedule extraction | ✅ `full` | `2026-05-28` | backfill:dictionary-completeness | `internal/custom/python/celery.go`<br>`internal/engine/scheduled_jobs_edges.go` | — |

### Broker

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Broker binding | ❌ `missing` | — | #2982-B-build | — | No broker-URL extraction implemented yet; requires parsing CELERY_BROKER_URL / broker= constructor arg |
| Result backend binding | ❌ `missing` | — | #2982-B-build | — | No result-backend extraction implemented yet; requires parsing CELERY_RESULT_BACKEND / backend= constructor arg |

### Reliability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Retry policy extraction | ✅ `full` | `2026-05-28` | backfill:dictionary-completeness | `internal/extractors/python/celery.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/python/pytest.go` | pytest.go extracts test functions and fixtures; coverage is partial because Celery-task test helpers are not directly linked |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/effect_sinks_python.go` | language-wide Python effect sniffer detects Django ORM / SQLAlchemy db writes and reads; partial because Celery-specific task context is not disambiguated |
| Dead code detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/def_use_python.go` | language-wide Python def-use sniffer captures variable defs/uses; partial for Celery task argument flows |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/entry_points_python.go` | language-wide Python entry-point sniffer detects module-level test/main/lifecycle entry points; partial for Celery worker entry wiring |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Sanitizer recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/effect_sinks_python.go` | language-wide Python effect sniffer recognises SQL/command-injection sink shapes; partial for Celery task context |
| Taint source detection | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/taint_sites_python.go` | language-wide Python taint sniffer recognises request/env sources; partial for Celery task context |
| Template pattern catalog | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/template_pattern_python.go` | language-wide Python template-pattern sniffer covers i18n/log/SQL patterns; partial for Celery-specific message formatting |
| Vulnerability finding | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.celery ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
