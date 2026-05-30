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
| Broker binding | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3074) | `internal/custom/python/broker_binding_test.go`<br>`internal/custom/python/celery.go` | No broker-URL extraction implemented yet; requires parsing CELERY_BROKER_URL / broker= constructor arg |
| Result backend binding | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3074) | `internal/custom/python/broker_binding_test.go`<br>`internal/custom/python/celery.go` | No result-backend extraction implemented yet; requires parsing CELERY_RESULT_BACKEND / backend= constructor arg |

### Reliability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Retry policy extraction | ✅ `full` | `2026-05-28` | backfill:dictionary-completeness | `internal/extractors/python/celery.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/python/pytest.go` | pytest.go extracts test functions (test_*), test classes (Test*), pytest fixtures (@pytest.fixture with scope/autouse), parametrize marks, and conftest.py fixtures. These cover all generic Python tests including Celery task tests written with pytest. Partial because: Celery-specific test helpers (pytest-celery, celery.contrib.pytest app fixture, task.apply() / task.delay() calls within test bodies) are not specifically linked back to the Celery task entity via TESTS edges — test functions and Celery task entities are both emitted but the TESTS edge between them is not synthesised. Tests: TestPytest_TestFunction, TestPytest_Fixture. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | 3068 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go`<br>`internal/types/confidence.go` | — |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/effect_sinks_python.go` | language-wide Python effect sniffer detects Django ORM / SQLAlchemy db writes and reads; partial because Celery-specific task context is not disambiguated |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | dead code derived from reachability.go + entry_points_python.go; fires on all Python; partial because Celery task entry wiring via @app.task is not modelled |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/def_use_python.go` | language-wide Python def-use sniffer captures variable defs/uses; partial for Celery task argument flows |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | language-wide Python effect sniffer (open/pathlib/os/shutil) fires on any Python file; partial because Celery task file I/O is not disambiguated from worker code |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | language-wide Python HTTP-effect sniffer (requests/httpx/boto3) fires on any Python file; partial because Celery task outbound HTTP is not separated from unrelated code |
| Import resolution quality | 🟢 `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/module_cycle_pass.go` | language-agnostic Tarjan SCC over IMPORTS edges; fires on all Python modules; partial because task module cycles are not distinguished from app-level cycles |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | language-wide Python mutation sniffer (self.attr=) fires on any Python file; partial for Celery task class attribute mutations |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | derivative of effect propagation: any entity with no detected effect is tagged pure; fires on all Python entities; partial because task functions may have side effects missed by heuristic sniffers |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/entry_points_python.go` | language-wide Python entry-point sniffer detects module-level test/main/lifecycle entry points; partial for Celery worker entry wiring |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | language-wide Python sanitizer sniffer (parameterised SQL, bleach, pydantic schemas) fires on any Python file; partial for Celery task context |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/effect_sinks_python.go` | language-wide Python effect sniffer recognises SQL/command-injection sink shapes; partial for Celery task context |
| Taint source detection | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/taint_sites_python.go` | language-wide Python taint sniffer recognises request/env sources; partial for Celery task context |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/substrate/template_pattern_python.go` | language-wide Python template-pattern sniffer covers i18n/log/SQL patterns; partial for Celery-specific message formatting |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3047 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | vulnerability_finding derives from taint_source+taint_sink co-occurrence (taint_flow.go); fires on all Python; partial for task queue context |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.celery ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
