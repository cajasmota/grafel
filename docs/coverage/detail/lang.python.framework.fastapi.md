<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.fastapi` — FastAPI

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 37

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/python/frameworks/fastapi.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/python/frameworks/fastapi.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-29` | — | `internal/engine/http_endpoint_synthesis.go` | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-29` | 3052 | `internal/custom/python/fastapi.go`<br>`internal/mcp/auth_coverage.go`<br>`internal/patterns/auth_endpoint_linker.go` | Depends(get_current_user/get_current_active_user/oauth2_scheme/verify_token/authenticate/require_auth) detected by auth_endpoint_linker authFastAPIDependsRE; additional injection via fastapi.go decorator pass; comprehensive OAuth2/JWT coverage |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/python/fastapi.go`<br>`internal/custom/python/fastapi_reqresp.go` | fastapi_reqresp.go extracts Pydantic BaseModel body params and response_model=/return annotations for all FastAPI route decorators AND emits traversable endpoint→DTO graph edges (#3629): each endpoint SCOPE.Operation carries ACCEPTS_INPUT → request DTO (body param) and RETURNS → response DTO (response_model= or return annotation), ToID=Class:<Name> structural ref the resolver binds by name. Previously DTO entities were emitted but no edges, so expand/traces/payload_drift could not follow endpoint→DTO; now they can (parity with Java Spring). fastapi.go extracts APIRouter, Depends(), per-route metadata. Pydantic v1/v2 unwrapping via unwrapType; Depends/Query/Path injection tokens skipped. Tests: TestFastAPIReqResp_AcceptsInputEdge, TestFastAPIReqResp_ReturnsEdgeResponseModel, TestFastAPIReqResp_ReturnsEdgeAnnotation, TestFastAPIReqResp_PrimitiveParamNoEdge, TestFastAPIReqResp_FullFixture. |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/python/fastapi.go`<br>`internal/custom/python/fastapi_reqresp.go`<br>`internal/custom/python/http_reqresp_generic.go` | fastapi.go extracts Depends() injection tokens (dependency-injection as validation). fastapi_reqresp.go extracts Pydantic body-parameter type annotations (ACCEPTS_INPUT) proving request validation at the type level. http_reqresp_generic.go handles pydantic model_validate/parse_obj/from_orm calls in handler bodies. Tests: TestFastAPI_Depends, TestFastAPIReqResp_AcceptsInput, TestFastAPIReqResp_FullFixture. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/python/fastapi.go`<br>`internal/custom/python/http_middleware.go` | @app.middleware('http') decorator extracted by fastapi.go (faMiddlewareRe); app.add_middleware(Cls, ...) extracted via starletteAddMiddlewareRe in http_middleware.go for Starlette/FastAPI. Tests: TestFastAPI_Middleware (decorator form), TestFastAPI_FullFixture_Middleware (fixture). Covers both ASGI middleware registration patterns used in FastAPI. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-29` | 3049 | `internal/extractors/python/types.go` | — |
| Interface extraction | ✅ `full` | `2026-05-29` | 3049 | `internal/extractors/python/types.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-29` | 3049 | `internal/extractors/python/types.go` | — |
| Type extraction | ✅ `full` | `2026-05-29` | 3049 | `internal/extractors/python/types.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-29` | 3051 | `internal/engine/tests_edges.go` | pytest.go extracts test funcs; multi-hop TESTS pass (#2987) links test-client calls through ROUTES_TO to handlers; framework fixture tests in tests_edges_test.go |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-30` | 3063 | `internal/custom/python/observability.go` | observability.go: import-heuristic detection of stdlib logging (logging.getLogger + call sites), loguru (from loguru import logger + bind/opt/contextualize), and structlog (structlog.get_logger + structlog.configure). Emits SCOPE.Pattern/logger + SCOPE.Pattern/log_statement entities per file. Partial by design: no cross-file dataflow — a logger declared in utils.py and used in views.py produces entities only in the file where the call site lives. Tests: TestObservability_StdlibLogging, TestObservability_Loguru, TestObservability_Structlog, TestObservability_FixtureLogging. |
| Metric extraction | 🟢 `partial` | `2026-05-30` | 3063 | `internal/custom/python/observability.go` | observability.go: import-heuristic detection of prometheus_client (Counter/Gauge/Histogram/Summary construction + push_to_gateway), statsd (incr/decr/gauge/timing/histogram calls), and datadog DogStatsd (increment/gauge/histogram/timing). Emits SCOPE.Pattern/metric entities with metric_type and metric_name properties. Partial by design: no cross-file dataflow; prometheus_client REGISTRY custom collector classes not detected; StatsD pipelines not followed. Tests: TestObservability_PrometheusClient, TestObservability_Statsd, TestObservability_Datadog, TestObservability_FixtureMetrics. |
| Trace extraction | 🟢 `partial` | `2026-05-30` | 3063 | `internal/custom/python/observability.go` | observability.go: import-heuristic detection of OpenTelemetry (tracer.start_as_current_span decorator + context-manager + start_span), ddtrace (@tracer.wrap decorator + tracer.trace context-manager), and jaeger_client (Config(service_name=) + tracer.start_span). Emits SCOPE.Pattern/trace_span entities with span_name, span_kind, and library properties. Partial by design: no cross-file dataflow; OTel Resource/TracerProvider setup not tracked; auto-instrumentation via opentelemetry-instrument not detected. Tests: TestObservability_OpenTelemetry, TestObservability_DDTrace, TestObservability_JaegerClient, TestObservability_FixtureTracing. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | 3068 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractors/python/config_consumer.go`<br>`internal/extractors/python/config_consumer_test.go` | settings.X / os.environ.get(k) -> DEPENDS_ON_CONFIG (live pre-#3641; config-blast-radius) |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points_python.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_python.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_python.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_python.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.fastapi ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
