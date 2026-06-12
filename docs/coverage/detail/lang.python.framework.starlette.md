<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.starlette` — Starlette

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | ✅ `full` | `2026-06-03` | — | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3913: applyEndpointPagination stamps paginated/pagination_style/pagination_params from literal-named request query reads in the handler body (request.args.get('limit')/request.query_params.get('offset')/request.args['cursor']) plus the cross-language parameters fallback. Honest-partial: a lone limit (no offset/page/cursor companion) and dynamically-named reads are not stamped. |
| Endpoint response codes | ✅ `full` | `2026-06-03` | — | `internal/engine/http_endpoint_response_codes.go`<br>`internal/engine/http_endpoint_response_codes_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3913: applyEndpointResponseCodes resolves starlette JSONResponse/Response(status_code=NNN) body literals; the positional and endpoint= Route handler is now resolved (synthesizeStarlette) so the handler body is in scope; honest-partial on dynamic status vars. |
| Endpoint synthesis | ✅ `full` | `2026-05-29` | — | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/python/frameworks/starlette.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/python/frameworks/starlette.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-29` | — | `internal/engine/http_endpoint_synthesis.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-05-29` | 3052 | `internal/mcp/auth_coverage.go`<br>`internal/patterns/auth_endpoint_linker.go` | Starlette shares FastAPI Depends() auth injection mechanism; auth_endpoint_linker authFastAPIDependsRE detects Depends(get_current_user) etc. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | `2026-05-29` | 3185 | `internal/custom/python/http_reqresp_generic.go` | Pydantic BaseModel type-hinted params in route/handler functions; marshmallow schema.load() in handler bodies. Generic extractor covering all non-FastAPI/Flask Python web frameworks. |
| Request validation | 🟢 `partial` | `2026-05-29` | 3185 | `internal/custom/python/http_reqresp_generic.go` | Pydantic model_validate/parse_obj calls in handler bodies; marshmallow schema.load() validation evidence. Generic extractor for all non-FastAPI/Flask Python web frameworks. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | `2026-05-29` | 3054 | `internal/custom/python/http_middleware.go` | — |
| Rate limit stamping | 🟢 `partial` | `2026-06-03` | 4122 | `internal/engine/http_endpoint_python_ratelimit.go`<br>`internal/engine/http_endpoint_python_ratelimit_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | slowapi/flask-limiter @<limiter>.limit("N/window") and django-ratelimit @ratelimit(key=,rate=) decorators on the synthesized endpoint are bound by handler def name (class methods by class-qualified key, no bare-method bleed) and the literal rate is resolved, via the cross-framework applyPythonRateLimit pass (parity with JS/TS #2853, Java #4082). Partial: framework-native limiter middleware (e.g. sanic-limiter / litestar RateLimitConfig) beyond the decorator idiom is future work. Value-asserting + negative tests. |

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
| Tests linkage | ✅ `full` | `2026-05-29` | 3051 | `internal/custom/python/pytest.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/engine/tests_edges.go`<br>`internal/extractors/python/extractor.go` | pytest.go extracts test funcs; multi-hop TESTS pass (#2987) links test-client calls through ROUTES_TO to handlers; framework fixture tests in tests_edges_test.go. #4681 (epic #4615) — test→endpoint coverage-linkage for Python: (1) local-variable receiver typing in extractor.go (localReceiverTypes/receiverClass) types `v = ProposalViewSet(); v.get_counts(...)` so the enclosing named test_* function emits a resolved CALLS edge to ProposalViewSet.get_counts (mirrors TS/JS #4671); Python test_* funcs are already named call-bearing entities so NO synthetic test-scope owner is needed (unlike TS/JS). (2) route-hit test-client linkage (DRF self.client.get(url) / APIClient().post(url) / FastAPI TestClient(app).get / Flask app.test_client().get / pytest client.get) already lands a TESTS edge to the matching http_endpoint_definition via pytest.go e2e_route_calls -> http_endpoint_e2e_testmap.go (#4369). ComputeCoverage credits the endpoint via test->CALLS->handler + handler<-definition (no coverage-side change). Honest exclusion: shape-only assertion specs that never call the handler or hit a route get NO edge. Tests: issue4681_localvar_receiver_test.go (ViewSet/CBV/serializer + factory & shape-only negatives). |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | — | 3063 | `internal/custom/python/observability.go` | — |
| Metric extraction | 🟢 `partial` | — | 3063 | `internal/custom/python/observability.go` | — |
| Trace extraction | 🟢 `partial` | — | 3063 | `internal/custom/python/observability.go` | — |

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
| Dead code detection | 🟢 `partial` | `2026-05-29` | 2972 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 2972 | `internal/substrate/def_use_python.go`<br>`internal/substrate/def_use_test.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/python/exception_flow.go`<br>`internal/extractors/python/exception_flow_test.go` | raise X / raise mod.X -> THROWS; except (A,B) -> CATCHES; bare except + dynamic raise dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 4106 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic engine pass, fires regardless of router/task decorator). Honest-partial on Python: LaunchDarkly variation/bool_variation, Unleash is_enabled, OpenFeature get_boolean_value, Flagsmith has_feature, Split getTreatment, custom getFlag/feature_enabled fire & attribute to the enclosing handler/task/resolver. Miss: OpenFeature kwarg form get_boolean_value(flag_key=...) and plain env-var gating os.environ.get('FEATURE_X') (config consumption, not SDK flag). |
| Fs effect | 🟢 `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 2972 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 2972 | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 2972 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 2972 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 2972 | `internal/substrate/taint_sites_python.go`<br>`internal/substrate/taint_sites_test.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | 2972 | `internal/substrate/taint_sites_python.go`<br>`internal/substrate/taint_sites_test.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 2972 | `internal/substrate/template_pattern_python.go`<br>`internal/substrate/template_pattern_test.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 2972 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.starlette ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
