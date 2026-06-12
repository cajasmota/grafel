<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.kratos` — Kratos (Bilibili)

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
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/go/frameworks/kratos.yaml` | — |
| Handler attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/go/frameworks/kratos.yaml` | — |
| Route extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/rules/go/frameworks/kratos.yaml` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-05-29` | — | `internal/custom/golang/kratos.go` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | `2026-06-12` | [link](https://github.com/cajasmota/archigraph/issues/3255) | `internal/custom/golang/dto.go`<br>`internal/custom/golang/dto_field_members.go`<br>`internal/custom/golang/dto_field_members_test.go`<br>`internal/custom/golang/dto_test.go`<br>`internal/extractors/golang/issue4850_struct_field_membership_test.go`<br>`internal/extractors/golang/struct_fields.go` | #4715: each request/response DTO struct field is emitted as a SCOPE.Schema/field member (field_name from json tag, normalized field_type, parent_class, optional from omitempty/pointer/non-required validate|binding rule, validators as @rule markers + parseable Signature) with a CONTAINS edge to the struct — the SAME uniform shape as the JS (#4635) and Python/Java (#4613) DTO field members. emitGoDTOFieldMembers in dto_field_members.go; value-asserted by TestGoDTO_FieldMembers (type/optional/validators/CONTAINS). DTO struct resolution stays same-file heuristic (partial where not proven by a per-framework fixture). #4850: the #4715 custom emitter only fired for endpoint-bound DTO structs; the GENERAL primary-pass now emits a SCOPE.Schema/field entity + struct->field CONTAINS for EVERY struct's named members (json-tag wire name, embedded fields -> EXTENDS to in-file base), so any Go data class projects field rows in the dashboard shape tree — closing the same gap #4845/#4851 fixed for JS/TS. extractStructFieldEntities + attachClassContains in golang/struct_fields.go; value-asserted by TestStructFieldsAreContained/TestStructGroupedFieldsAreContained/TestStructEmbeddedFieldEmitsExtends. Field Names align with the custom emitter (json wire name) so MergeWithCustom dedups on endpoint-bound DTOs. |
| Request validation | 🟢 `partial` | `2026-05-29` | — | `internal/custom/golang/kratos.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | `2026-05-29` | — | `internal/custom/golang/kratos.go` | — |
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
| Log extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/golang/observability.go`<br>`internal/custom/golang/observability_test.go` | — |
| Metric extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/golang/observability.go`<br>`internal/custom/golang/observability_test.go` | — |
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
| Request sink dataflow | 🔴 `missing` | `2026-06-04` | 3740 | — | wave4 #3872: HONEST-MISSING (verified divergent). Kratos HTTP handlers are generated wrappers func(ctx http.Context) error binding via ctx.Bind(&in) on a kratos `http.Context` (not gin's *gin.Context), and validation is protoc-gen-validate Validate()/ValidateAll() on the message — neither the `ctx` receiver + ctx.Bind nor the PGV path matches the sniffer's receiver(c/r/req)+Query/FormValue/Param/Decode idiom. Follow-up: recognise the kratos http.Context ctx.Bind idiom. |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_golang.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_golang.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | — | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_golang.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.framework.kratos ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
