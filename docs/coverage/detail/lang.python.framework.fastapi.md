<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.fastapi` — FastAPI

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | ✅ `full` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_deprecation.go`<br>`internal/engine/http_endpoint_deprecation_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628 epic: api_version + deprecation on http_endpoint_definition. Python: @deprecated decorator, DRF/drf-spectacular deprecated=True, Sphinx .. deprecated:: docstring. |
| Endpoint pagination posture | ✅ `full` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: paginated/pagination_style(offset|page|cursor)/pagination_params/pagination_source on http_endpoint_definition via applyEndpointPagination. Direct signals: DRF pagination_class + DEFAULT_PAGINATION_CLASS, Django Paginator, FastAPI/fastapi-pagination, Spring Pageable/Page<>, Express req.query, Sequelize/Prisma take/skip/.cursor(). Honest-partial: lone limit not stamped. |
| Endpoint response codes | ✅ `full` | `2026-06-02` | 3818 | `internal/engine/http_endpoint_response_codes.go`<br>`internal/engine/http_endpoint_response_codes_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3818: response_codes (sorted unique) + success_code (lone 2xx) + response_codes_source on http_endpoint_definition via applyEndpointResponseCodes. FastAPI signals: decorator status_code=NNN, HTTPException(status_code=)/JSONResponse(status_code=)/Response(status_code=) body literals. Honest-partial: dynamic status var skipped; no literal -> absent. |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/python/frameworks/fastapi.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/python/frameworks/fastapi.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-29` | — | `internal/engine/http_endpoint_synthesis.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-06-02` | 3052 | `internal/custom/python/auth_endpoint.go`<br>`internal/custom/python/auth_endpoint_test.go`<br>`internal/custom/python/fastapi.go`<br>`internal/mcp/auth_coverage.go` | #3628 area #6 (endpoint protection): fastapi.go now scans each route handler's def signature for Depends(get_current_user) / Security(get_user, scopes=[...]) auth dependencies and the route-decorator dependencies=[Depends(verify_token)] kwarg, stamping auth_required/auth_method=dependency/auth_confidence/auth_guard/auth_scopes ON THE ENDPOINT entity (resolveFastAPIRouteAuth in auth_endpoint.go). Auth dependencies are distinguished from plain DI (get_db) by name idiom; Security(...) is always auth and carries scopes. Value-asserting tests: TestFastAPIAuth_DependsCurrentUser (/me -> auth_required=true, auth_guard=get_current_user), TestFastAPIAuth_SecurityScopes (auth_scopes=items:list,items:read), TestFastAPIAuth_DependenciesKwarg, plus negatives TestFastAPIAuth_PlainDependencyNotProtected / NoDependencyUnprotected. Honest-partial: dynamic/cross-file dependency resolution out of scope. Prior auth_endpoint_linker file-proximity heuristic superseded by per-endpoint signature extraction. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/python/fastapi.go`<br>`internal/custom/python/fastapi_reqresp.go` | fastapi_reqresp.go extracts Pydantic BaseModel body params and response_model=/return annotations for all FastAPI route decorators AND emits traversable endpoint→DTO graph edges (#3629): each endpoint SCOPE.Operation carries ACCEPTS_INPUT → request DTO (body param) and RETURNS → response DTO (response_model= or return annotation), ToID=Class:<Name> structural ref the resolver binds by name. Previously DTO entities were emitted but no edges, so expand/traces/payload_drift could not follow endpoint→DTO; now they can (parity with Java Spring). fastapi.go extracts APIRouter, Depends(), per-route metadata. Pydantic v1/v2 unwrapping via unwrapType; Depends/Query/Path injection tokens skipped. Tests: TestFastAPIReqResp_AcceptsInputEdge, TestFastAPIReqResp_ReturnsEdgeResponseModel, TestFastAPIReqResp_ReturnsEdgeAnnotation, TestFastAPIReqResp_PrimitiveParamNoEdge, TestFastAPIReqResp_FullFixture. |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/python/fastapi.go`<br>`internal/custom/python/fastapi_reqresp.go`<br>`internal/custom/python/http_reqresp_generic.go` | fastapi.go extracts Depends() injection tokens (dependency-injection as validation). fastapi_reqresp.go extracts Pydantic body-parameter type annotations (ACCEPTS_INPUT) proving request validation at the type level. http_reqresp_generic.go handles pydantic model_validate/parse_obj/from_orm calls in handler bodies. Tests: TestFastAPI_Depends, TestFastAPIReqResp_AcceptsInput, TestFastAPIReqResp_FullFixture. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-06-02` | — | `internal/custom/python/fastapi.go`<br>`internal/custom/python/http_middleware.go`<br>`internal/engine/http_endpoint_python_middleware.go` | @app.middleware('http') + app.add_middleware(Cls) extracted by fastapi.go/http_middleware.go. #3628: applyPythonMiddlewareCoverage now BINDS the ordered chain to ENDPOINTS — app.add_middleware as global-scope entries (outermost) + per-route dependencies=[Depends(...)] as route-scope entries (innermost) — stamping middleware_chain/count/names/scope per the cross-stack {name,expr,scope,order,auth_kind?} contract shared with Go (#3777)/JS-TS (#2853). The FastAPI route-decorator regex was hardened to tolerate nested-paren kwargs so dependencies-bearing routes synthesise. Test: TestMiddleware_FastAPIGlobalAndRoute. |
| Rate limit stamping | ✅ `full` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3778) | `internal/custom/python/fastapi.go`<br>`internal/custom/python/rate_limit_endpoint.go`<br>`internal/custom/python/rate_limit_endpoint_test.go` | slowapi @limiter.limit("5/minute") stamps rate_limited/rate_limit/rate_limit_source on the route op. DRF @throttle_classes resolver (rate from a co-located custom throttle's rate attr; settings-driven built-ins → honest-partial) shared via resolvePyEndpointRateLimit. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-29` | 3049 | `internal/extractors/python/types.go` | — |
| Interface extraction | ✅ `full` | `2026-05-29` | 3049 | `internal/extractors/python/types.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-29` | 3049 | `internal/extractors/python/types.go` | — |
| Type extraction | ✅ `full` | `2026-05-29` | 3049 | `internal/extractors/python/types.go` | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🟢 `partial` | `2026-06-02` | — | `internal/custom/python/di_graph.go`<br>`internal/custom/python/di_graph_test.go` | dependency-injector DeclarativeContainer providers (providers.Factory/Singleton(Impl)) emit BINDS(token->impl), token=container attribute name. Value-asserted TestPyDI_ContainerProviderBinds (service->Service, repo->Repository); negative TestPyDI_ConfigurationProviderNoBinds. PARTIAL: cross-file token resolution + dynamic providers skipped; FastAPI itself has no token-binding container. |
| DI injection point | 🟢 `partial` | `2026-06-02` | — | `internal/custom/python/di_graph.go`<br>`internal/custom/python/di_graph_test.go` | FastAPI Depends() and dependency-injector @inject/Provide[...] emit INJECTED_INTO(provider->consumer). Value-asserted: TestPyDI_FastAPIDependsCallable (get_service->handler), TestPyDI_FastAPIDependsClass (SvcClass->handler), TestPyDI_FastAPIDependsBareType (type annotation), TestPyDI_InjectProvideInjectedInto (service->main). Negatives: TestPyDI_FastAPIDynamicNoEdge (Depends(getattr(...))), TestPyDI_ProvideWithoutInjectNoEdge. PARTIAL: dynamic/unresolved deps skipped (honest-partial); cross-file provider binding via resolver. |
| DI scope resolution | — `not_applicable` | `2026-06-02` | — | — | FastAPI Depends has no container-managed lifetime/scope annotations to resolve (use_cache aside); dependency-injector scope is encoded in the provider kind (Factory vs Singleton), already captured as provider_kind on the BINDS edge in di_binding_extraction. No separate scope-resolution pass. |

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
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/python/exception_flow.go`<br>`internal/extractors/python/exception_flow_test.go` | raise X / raise mod.X -> THROWS; except (A,B) -> CATCHES; bare except + dynamic raise dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 4106 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic engine pass, fires regardless of router/task decorator). Honest-partial on Python: LaunchDarkly variation/bool_variation, Unleash is_enabled, OpenFeature get_boolean_value, Flagsmith has_feature, Split getTreatment, custom getFlag/feature_enabled fire & attribute to the enclosing handler/task/resolver. Miss: OpenFeature kwarg form get_boolean_value(flag_key=...) and plain env-var gating os.environ.get('FEATURE_X') (config consumption, not SDK flag). |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_python.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
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
