<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.ariadne` — Ariadne GraphQL

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
| Endpoint synthesis | ✅ `full` | `2026-06-02` | 3620 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphene_ariadne_3620_test.go`<br>`internal/engine/httproutes/canonicalize.go` | schema-first Ariadne: QueryType()/MutationType()/SubscriptionType()/ObjectType("Query") binders + @<binder>.field("<name>") decorator resolvers -> http:GRAPHQL:/graphql/<Root>/<field>, identical shape to Strawberry. synthesizeAriadne. |
| Handler attribution | ✅ `full` | `2026-06-02` | 3620 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphene_ariadne_3620_test.go`<br>`internal/engine/httproutes/canonicalize.go` | decorated resolver function is the handler; source_handler=SCOPE.Operation:<funcName> rebinds to a HANDLES edge. |
| Route extraction | 🟢 `partial` | `2026-06-02` | 3620 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphene_ariadne_3620_test.go`<br>`internal/engine/httproutes/canonicalize.go` | Binder var -> root type resolved from QueryType/MutationType/SubscriptionType ctor or ObjectType("<Type>") arg; field name is the literal decorator string. Dynamically-named fields and set_field()/schema-directive bindings not resolved (honest-partial). |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | — | 3620 | `internal/mcp/auth_coverage.go`<br>`internal/patterns/decorator_extractor.go` | Strawberry-GraphQL auth context not yet specifically extracted; generic decorator sniffer detects @authorized/@authenticated on resolver functions [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | — | 3620 | `internal/custom/python/http_reqresp_generic.go` | Pydantic BaseModel type-hinted params in route/handler functions; marshmallow schema.load() in handler bodies. Generic extractor covering all non-FastAPI/Flask Python web frameworks. [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Request validation | 🟢 `partial` | — | 3620 | `internal/custom/python/http_reqresp_generic.go` | Pydantic model_validate/parse_obj calls in handler bodies; marshmallow schema.load() validation evidence. Generic extractor for all non-FastAPI/Flask Python web frameworks. [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | — | 3620 | `internal/custom/python/http_middleware.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | 🔴 `missing` | — | 3804 | — | Ariadne is SCHEMA-FIRST (SDL strings, not Python code-first type classes); the code-first type-graph extractor (internal/custom/python/graphql_codefirst_typegraph.go) does not apply. SDL type-graph for ariadne is the remaining backfill (tracked with the other-language GraphQL SDL lane). |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | — | 3620 | `internal/extractors/python/types.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Interface extraction | ✅ `full` | — | 3620 | `internal/extractors/python/types.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Type alias extraction | ✅ `full` | — | 3620 | `internal/extractors/python/types.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Type extraction | ✅ `full` | — | 3620 | `internal/extractors/python/types.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | — | 3620 | `internal/engine/tests_edges.go` | Multi-hop TESTS pass (#2987) links test-client calls (client/session/test_client.<verb>('/path')) through ROUTES_TO to handlers; framework fixture tests in tests_edges_test.go [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | — | 3620 | `internal/custom/python/observability.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Metric extraction | 🟢 `partial` | — | 3620 | `internal/custom/python/observability.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Trace extraction | 🟢 `partial` | — | 3620 | `internal/custom/python/observability.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | — | 3620 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | — | 3620 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go`<br>`internal/types/confidence.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Config consumption | ✅ `full` | — | 3620 | `internal/extractors/python/config_consumer.go`<br>`internal/extractors/python/config_consumer_test.go` | settings.X / os.environ.get(k) -> DEPENDS_ON_CONFIG (live pre-#3641; config-blast-radius) [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Constant propagation | ✅ `full` | — | 3620 | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Dead code detection | 🟢 `partial` | — | 3620 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Def use chain extraction | 🟢 `partial` | — | 3620 | `internal/substrate/def_use_python.go`<br>`internal/substrate/def_use_test.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Env fallback recognition | ✅ `full` | — | 3620 | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/python/exception_flow.go`<br>`internal/extractors/python/exception_flow_test.go` | raise X / raise mod.X -> THROWS; except (A,B) -> CATCHES; bare except + dynamic raise dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 4106 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic engine pass, fires regardless of router/task decorator). Honest-partial on Python: LaunchDarkly variation/bool_variation, Unleash is_enabled, OpenFeature get_boolean_value, Flagsmith has_feature, Split getTreatment, custom getFlag/feature_enabled fire & attribute to the enclosing handler/task/resolver. Miss: OpenFeature kwarg form get_boolean_value(flag_key=...) and plain env-var gating os.environ.get('FEATURE_X') (config consumption, not SDK flag). |
| Fs effect | 🟢 `partial` | — | 3620 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| HTTP effect | 🟢 `partial` | — | 3620 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Import resolution quality | 🟢 `partial` | — | 3620 | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Module cycle detection | 🟢 `partial` | — | 3620 | `internal/links/module_cycle_pass.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Mutation effect | 🟢 `partial` | — | 3620 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_python.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Pure function tagging | 🟢 `partial` | — | 3620 | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Reachability analysis | 🟢 `partial` | — | 3620 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_python.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Request shape extraction | ✅ `full` | — | 3620 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | ✅ `full` | — | 3620 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Sanitizer recognition | 🟢 `partial` | — | 3620 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Schema drift detection | ✅ `full` | — | 3620 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Taint sink detection | 🟢 `partial` | — | 3620 | `internal/substrate/taint_sites_python.go`<br>`internal/substrate/taint_sites_test.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Taint source detection | 🟢 `partial` | — | 3620 | `internal/substrate/taint_sites_python.go`<br>`internal/substrate/taint_sites_test.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Template pattern catalog | 🟢 `partial` | — | 3620 | `internal/substrate/template_pattern_python.go`<br>`internal/substrate/template_pattern_test.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |
| Vulnerability finding | 🟢 `partial` | — | 3620 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_python.go` | [#3911: language-dispatched python extractor — fires for graphene/ariadne identically (probe-verified)] |

## Framework-specific

### DataLoader (N+1 batching)

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dataloader extraction | 🟢 `partial` | `2026-06-02` | 3624 | `internal/custom/python/graphql_dataloader.go`<br>`internal/custom/python/graphql_dataloader_test.go`<br>`internal/types/kinds.go` | aiodataloader DataLoader(load_fn=batch_users) / DataLoader(batch_users) -> SCOPE.DataLoader entity named by the assigned var + BATCHES edge to the named batch fn; <loader>.load(id)/.load_many(ids) in a resolver body -> USES edge resolver->loader (resolver = nearest enclosing def), via=graphql_dataloader. Value-asserted: user_loader BATCHES batch_users + author resolver USES user_loader. PARTIAL (honest): regex+enclosing-def heuristic; lambda batch fns get no BATCHES edge; top-level .load() with no enclosing def skipped. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.ariadne ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
