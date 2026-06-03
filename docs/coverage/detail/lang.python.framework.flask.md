<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.flask` — Flask

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 50

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/python/frameworks/flask.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/python/frameworks/flask.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-29` | — | `internal/engine/http_endpoint_synthesis.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/template_render.go`<br>`internal/extractors/python/template_render.go`<br>`internal/extractors/python/template_render_test.go` | render_template('x.html') -> RENDERS SCOPE.Template; dynamic/f-string names dropped (#3628) |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-06-02` | 3052 | `internal/custom/python/auth_endpoint.go`<br>`internal/custom/python/auth_endpoint_test.go`<br>`internal/custom/python/flask.go`<br>`internal/mcp/auth_coverage.go` | #3628 area #6 (endpoint protection): flask.go now scans the stacked-decorator block of each @app.route / @app.get route for a Flask-Login / Flask-Security / Flask-Principal auth decorator (login_required/roles_required/permission_required/jwt_required/...), stamping auth_required=true/auth_method=decorator/auth_confidence/auth_guard ON THE ENDPOINT entity, with roles captured from @roles_required('admin','superuser') (resolveFlaskDecoratorAuth in auth_endpoint.go). Value-asserting tests: TestFlaskAuth_LoginRequired (/dashboard -> auth_required=true, auth_guard=login_required, auth_method=decorator), TestFlaskAuth_RolesRequired (auth_roles=admin,superuser), negative TestFlaskAuth_NoDecoratorUnprotected (/public -> no auth_required). |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/python/flask.go`<br>`internal/custom/python/flask_reqresp.go` | flask_reqresp.go extracts marshmallow Schema.load() call sites (request DTO) and return annotations (response DTO) AND emits traversable endpoint→DTO graph edges (#3629): the endpoint SCOPE.Operation carries ACCEPTS_INPUT → schema (statically-resolvable schema.load()) and RETURNS → return-annotation type, ToID=Class:<Name>. Honest-partial by design: dynamic/untyped request.get_json() bodies and runtime-serialized responses stay edge-less (only statically resolvable DTOs get edges). Previously entity-only; edges now restore endpoint→DTO traversal for expand/traces/payload_drift. flask.go emits FlaskForm/class entities; canonicalSchemaName converts snake_case vars to PascalCase. Tests: TestFlaskReqResp_AcceptsInputEdge, TestFlaskReqResp_ReturnsEdge, TestFlaskReqResp_UntypedNoEdge, TestFlaskReqResp_PascalCaseSchema. |
| Request validation | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/python/flask.go`<br>`internal/custom/python/flask_reqresp.go`<br>`internal/custom/python/http_reqresp_generic.go` | flask_reqresp.go detects marshmallow schema.load() in handler bodies as ACCEPTS_INPUT evidence. flask.go emits before_request hooks (lifecycle gates). http_reqresp_generic.go detects Pydantic model_validate/parse_obj calls and marshmallow load in Flask handlers. Partial because: Flask-WTF form.validate_on_submit() is not explicitly detected; request.json/request.form.get() raw access patterns are not tracked; no Flask-RESTX/flask-smorest RequestParser body validation. Tests: TestFlaskReqResp_SchemaLoad, TestGHR_Marshmallow_SchemaLoad. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/python/flask.go` | flask.go extracts @app.before_request / @app.after_request / @app.teardown_request / @bp.before_app_request decorators as request_hook pattern entities — these ARE Flask's middleware mechanism (no traditional middleware class in Flask; hooks are the canonical approach). Test: TestFlask_RequestHook proves before_request hook extraction with hook_type property. |
| Rate limit stamping | ✅ `full` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3778) | `internal/custom/python/flask.go`<br>`internal/custom/python/rate_limit_endpoint.go`<br>`internal/custom/python/rate_limit_endpoint_test.go` | flask-limiter @limiter.limit("100/hour") and django-ratelimit @ratelimit(key='ip', rate='5/m') stamp rate_limited/rate_limit/rate_limit_scope/rate_limit_source on the route op. |

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
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

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
| Request sink dataflow | 🟢 `partial` | `2026-06-02` | 3740 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_python.go`<br>`internal/substrate/dataflow_python_framework_sources_test.go` | SCOPED request-input → sink DATA_FLOWS_TO (#3628 area #22): intra-fn assignment tracking + multi-hop (≤DataFlowMaxHops=3) local-call propagation AND cross-file propagation into imported helpers. Multi-hop: value followed through nested module-level calls a→b→c, each bound by exact positional index (self/cls-aware); full chain in hop_path/hop_count props. Cross-file (#3772): when the callee resolves (via the CALLS graph) to exactly one same-repo function entity, that file is read and the bounded walk continues there (continueDataFlowPython); sink resolves to the callee-file entity. Sources request.data/json/GET/POST + DRF serializer.validated_data (static string keys only). Sinks Model.objects.create/.save/.insert, return Response/JsonResponse, requests/httpx.post. HONEST-PARTIAL (precision-first): drops reassignment, branch-merge, collection mutation, dynamic keys, embedded-arg, *args/**kwargs/keyword-arg call sites, recursion/entity-cycle, the 4th hop, and external/unresolved/ambiguous imports. DEPLOY-DEFERRED (daemon not rebuilt). #3927 (cross-lang request-source generalization): Flask sources now recognised — request.form['x']/request.form.get('x') (form), request.args.get('x')/request.args['x'] (query), request.values.get('x')/request.values['x'] (combined), request.json (have). Dynamic keys skipped (honest-partial). |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_python.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3045 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |

### Uncategorized

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Validate on submit detection | ✅ `full` | `2026-05-30` | — | `internal/custom/python/extractors_test.go`<br>`internal/custom/python/flask.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.flask ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
