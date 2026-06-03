<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.huma` — Huma

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
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3920 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3920: huma endpoints are synthesized but huma binds pagination params via input-struct fields with query tags, not in-body query reads — goPaginationVerdict's body-read signal does not fire. Left honest-missing pending an input-struct tag reader. |
| Endpoint response codes | 🔴 `missing` | — | 3920 | — | #3920: huma DOES synthesize http_endpoint_definition entities (huma.Register), but huma handlers return typed structs and signal errors via huma.ErrorNNN helpers (e.g. huma.Error404NotFound) rather than c.JSON/WriteHeader, and the success code is declared in huma.Operation{DefaultStatus:...} not the body. goResponseCodes does not yet read those — left honest-missing. |
| Endpoint synthesis | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/hertz_huma_test.go`<br>`internal/custom/golang/huma.go`<br>`internal/custom/golang/testdata/huma_routes.go`<br>`internal/engine/http_endpoint_go_trio.go` | huma.Register(api, huma.Operation{Method,Path}, handler) -> endpoint. Engine synthesis (go_trio) + dedicated SCOPE-entity extractor; fixture-proven. |
| Handler attribution | ✅ `full` | `2026-05-29` | — | `internal/custom/golang/hertz_huma_test.go`<br>`internal/custom/golang/huma.go`<br>`internal/custom/golang/testdata/huma_routes.go`<br>`internal/engine/http_endpoint_go_trio.go` | Final huma.Register argument attributed as handler; fixture-proven. |
| Route extraction | 🟢 `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/golang/hertz_huma_test.go`<br>`internal/custom/golang/huma.go`<br>`internal/custom/golang/testdata/huma_routes.go`<br>`internal/engine/http_endpoint_go_trio.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-05-29` | — | `internal/custom/golang/huma.go` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3255) | `internal/custom/golang/dto.go`<br>`internal/custom/golang/dto_test.go` | — |
| Request validation | 🟢 `partial` | `2026-05-29` | — | `internal/custom/golang/huma.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | `2026-05-29` | — | `internal/custom/golang/huma.go` | — |
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
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_golang.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_golang.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | — | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_golang.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_golang.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.framework.huma ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
