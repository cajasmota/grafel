<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.buffalo` — Buffalo

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | ✅ `full` | `2026-06-04` | — | `internal/engine/http_endpoint_response_codes.go`<br>`internal/engine/http_endpoint_response_codes_test.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/response_shape_go.go` | #3818: goResponseCodes resolves the literal status-code set from the buffalo handler body (located via source_handler). Buffalo renders with the stdlib net/http constant via c.Render(http.StatusCreated, r.JSON(x)) — the Render verb + http.Status* family were already matched by the flagship extractor, so this is a CREDIT (no new idiom needed); the parity probe had it MISSING. Honest-partial: a dynamic status var (c.Render(code,x)) is skipped — no framework-default 200 fabricated. Value-asserting TestResponseCodes_Go_Buffalo_RenderStatus asserts the SPECIFIC set 201,400 + success_code=201 + source='status call'. |
| Endpoint synthesis | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/buffalo.go`<br>`internal/engine/rules/go/frameworks/buffalo.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/buffalo.go`<br>`internal/engine/rules/go/frameworks/buffalo.yaml` | — |
| Route extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/golang/buffalo.go`<br>`internal/engine/rules/go/frameworks/buffalo.yaml` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-05-29` | — | `internal/custom/golang/helpers.go`<br>`internal/custom/golang/middleware_auth_extend.go`<br>`internal/custom/golang/middleware_auth_extend_test.go` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | `2026-06-12` | [link](https://github.com/cajasmota/archigraph/issues/3255) | `internal/custom/golang/dto.go`<br>`internal/custom/golang/dto_field_members.go`<br>`internal/custom/golang/dto_field_members_test.go`<br>`internal/custom/golang/dto_test.go`<br>`internal/extractors/golang/issue4850_struct_field_membership_test.go`<br>`internal/extractors/golang/struct_fields.go` | #4715: each request/response DTO struct field is emitted as a SCOPE.Schema/field member (field_name from json tag, normalized field_type, parent_class, optional from omitempty/pointer/non-required validate|binding rule, validators as @rule markers + parseable Signature) with a CONTAINS edge to the struct — the SAME uniform shape as the JS (#4635) and Python/Java (#4613) DTO field members. emitGoDTOFieldMembers in dto_field_members.go; value-asserted by TestGoDTO_FieldMembers (type/optional/validators/CONTAINS). DTO struct resolution stays same-file heuristic (partial where not proven by a per-framework fixture). #4850: the #4715 custom emitter only fired for endpoint-bound DTO structs; the GENERAL primary-pass now emits a SCOPE.Schema/field entity + struct->field CONTAINS for EVERY struct's named members (json-tag wire name, embedded fields -> EXTENDS to in-file base), so any Go data class projects field rows in the dashboard shape tree — closing the same gap #4845/#4851 fixed for JS/TS. extractStructFieldEntities + attachClassContains in golang/struct_fields.go; value-asserted by TestStructFieldsAreContained/TestStructGroupedFieldsAreContained/TestStructEmbeddedFieldEmitsExtends. Field Names align with the custom emitter (json wire name) so MergeWithCustom dedups on endpoint-bound DTOs. |
| Request validation | 🟢 `partial` | `2026-05-29` | 3213 | `internal/custom/golang/validation.go`<br>`internal/custom/golang/validation_test.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | `2026-05-29` | — | `internal/custom/golang/helpers.go`<br>`internal/custom/golang/middleware_auth_extend.go`<br>`internal/custom/golang/middleware_auth_extend_test.go` | — |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | — `not_applicable` | `2026-05-29` | — | — | Go has no first-class enum keyword; the idiom is const(...iota). The Go extractor extracts no const/iota enum constructs, so this capability is not applicable. |
| Interface extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/golang/extractor.go`<br>`internal/extractors/golang/extractor_test.go` | — |
| Type alias extraction | 🟢 `partial` | `2026-05-29` | — | `internal/extractors/golang/extractor.go` | — |
| Type extraction | ✅ `full` | `2026-05-29` | — | `internal/extractors/golang/extractor.go`<br>`internal/extractors/golang/extractor_test.go` | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go` | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-29` | 3215 | `internal/custom/golang/observability.go`<br>`internal/custom/golang/observability_test.go` | — |
| Metric extraction | 🟢 `partial` | `2026-05-29` | 3215 | `internal/custom/golang/observability.go`<br>`internal/custom/golang/observability_test.go` | — |
| Trace extraction | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/observability.go`<br>`internal/custom/golang/observability_test.go` | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-29` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/golang/config_consumer.go`<br>`internal/extractors/golang/config_consumer_test.go` | os.Getenv/viper.GetString -> config:<key> DEPENDS_ON_CONFIG edges (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_golang.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_golang.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/golang/exception_flow.go`<br>`internal/extractors/golang/exception_flow_test.go` | return ErrX / fmt.Errorf %w -> THROWS; errors.Is/As -> CATCHES; named sentinels only (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic Go engine pass, fires regardless of router). Honest-partial on Go: Unleash IsEnabled / Split GetTreatment / custom getFlag,featureEnabled / LD generic Variation fire & attribute to the enclosing handler; Go-canonical LD camelCase BoolVariation + OpenFeature context-first GetBooleanValue(ctx,key) miss. |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_golang.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | — | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_golang.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_golang.go` | — |
| Request sink dataflow | 🟢 `partial` | `2026-06-04` | 3943 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_golang.go`<br>`internal/substrate/dataflow_golang_test.go` | wave4 #3872: VERIFIED-FLIP. Buffalo handlers are func(c buffalo.Context) error; its request-body bind idiom c.Bind(&dto) is matched by the live go sniffer's dfGoBindRe (the bare `Bind` arm, identical to echo's c.Bind). The bound root is tainted in place and the SourceField is lifted from a later static member access off it (req.Sku -> "Sku"), exactly as gin ShouldBindJSON. HONEST-PARTIAL: buffalo's c.Param("id") path read is also matched (singular Param getter), but c.Request().FormValue(...) reached through the wrapped stdlib request is not (chained-call receiver != r/req). Value-asserting live test on the REAL buffalo idiom: TestGoDataFlow_BuffaloBindToDBSave (c.Bind(&req) then db.Save(&Order{Sku:req.Sku}) -> SourceField=Sku, origin=CreateOrder); non-vacuousness TestGoDataFlow_BuffaloNoBindNoFlow proves a plain var with no c.Bind yields NO flow. |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_golang.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_golang.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | — | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_golang.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.framework.buffalo ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
