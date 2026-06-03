<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.actix` — Actix Web

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
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
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/rust/frameworks/actix_web.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/rust/frameworks/actix_web.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/actix_web.go`<br>`internal/custom/rust/extractors_test.go`<br>`internal/custom/rust/helpers.go` | Extracts attribute-macro and manual web::get().to() routes; normalises path params; composes web::scope() prefix onto manual routes |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/auth.go`<br>`internal/custom/rust/auth_policy.go`<br>`internal/custom/rust/auth_policy_test.go` | HttpAuthentication::bearer/basic(validator) binds validator_name + auth_method + auth_required; custom Transform middleware impls classified. Validator symbol is bound by name, not resolved cross-file. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/fw_validation.go` | Detects #[derive(Deserialize)] and #[derive(Validate)] structs; actix web::Json/Query/Form/Path<T> extractors |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/fw_validation.go` | Detects #[validate(...)] field attrs, .validate() calls, actix extractor types |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/actix_web.go`<br>`internal/custom/rust/auth.go`<br>`internal/custom/rust/auth_policy.go`<br>`internal/custom/rust/auth_policy_test.go` | custom Transform<S,ServiceRequest> impls + .wrap() registrations captured with middleware_name/middleware_trait |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/rust/rust.go` | — |
| Interface extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/rust/rust.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/rust/rust.go` | — |
| Type extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/rust/rust.go` | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/extractors/cross/testmap/frameworks.go` | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/rust/observability.go`<br>`internal/custom/rust/observability_auth_test.go` | tracing info!/warn!/error!/debug!/trace! (qualified + bare), log::*, event!(Level,..), slog::*, #[instrument]; level+library captured, static message head captured when leading string literal. Stays PARTIAL: messages are often format strings with interpolated/structured fields, and logger->subscriber/appender binding is cross-file (same limitation as PHP/Java/Ruby per-framework log cells) |
| Metric extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/observability.go`<br>`internal/custom/rust/observability_auth_test.go` | metrics crate counter!/gauge!/histogram!("name"), prometheus register_*!/IntCounter::new/Opts::new("name"), opentelemetry meter.u64_counter("name"); metric NAME captured as observability_name + observability_kind/library props; value-asserting tests TestRustObs_MetricsMacro_CapturesName_Issue3416 + TestRustObs_PrometheusName_Issue3416 + TestRustObs_OtelMeter_Issue3416. Per-call-site literal name needs no cross-file resolution; binding meter->exporter stays out of scope |
| Trace extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/observability.go`<br>`internal/custom/rust/observability_auth_test.go` | tracing span!(Level,"name")/info_span!("name"), opentelemetry global::tracer("svc")/tracer.start("name")/span_builder("name"); span NAME captured as observability_name; value-asserting tests TestRustObs_SpanName_Issue3416 + TestRustObs_OtelSpanName_Issue3416. Literal span name needs no cross-file resolution; #[instrument]-derived names and tracer->exporter binding stay out of scope |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_rust.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/rust/exception_flow.go`<br>`internal/extractors/rust/exception_flow_test.go` | Err(Type::ctor())/Err(Type::Variant)/Err(Type(..)) + bail!/ensure!(Type::X) + .ok_or(Type::X)/.ok_or_else(||Type::X) -> THROWS (enum variant normalized to leading-segment ENUM type); match Err(Type)/if let Err(Type)/.map_err(|e: Type|) -> CATCHES; bare ? propagation, Box<dyn Error>, string panic!, Err(var)/Err(make()) re-raise dropped (honest-partial, #3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_rust.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_rust.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_rust.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_rust.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.actix ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
