<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.axum` — Axum

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 37

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_axum.go`<br>`internal/engine/rules/rust/frameworks/axum.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_axum.go` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/axum.go`<br>`internal/custom/rust/extractors_test.go`<br>`internal/custom/rust/helpers.go` | Extracts verb+path; normalises :id/<id>/{id} to canonical {id}; composes .nest() prefix; expands chained method routers get(h).post(h) |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/auth.go`<br>`internal/custom/rust/auth_policy.go`<br>`internal/custom/rust/auth_policy_test.go` | from_fn(auth_fn) guards, .route_layer auth, FromRequestParts extractor guards, tower-http ValidateRequestHeaderLayer::bearer; records guard_name + auth_method + auth_required. Cross-file resolution of the from_fn handler body is not chased. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/fw_validation.go` | Detects serde Deserialize/Validate structs; axum Json/Query/Form/Path<T> extractors |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/fw_validation.go` | Detects #[validate(...)] field attrs and axum extractor types |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/auth.go`<br>`internal/custom/rust/auth_policy.go`<br>`internal/custom/rust/auth_policy_test.go`<br>`internal/custom/rust/axum.go` | .layer/.route_layer + tower ServiceBuilder ordered layer chains enumerated in source order (layer_order + layer_order_list) |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/rust/rust.go` | — |
| Interface extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/rust/rust.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/rust/rust.go` | — |
| Type extraction | ✅ `full` | `2026-05-30` | — | `internal/extractors/rust/rust.go` | — |

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
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_rust.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_rust.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_rust.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_rust.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_rust.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_rust.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_rust.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_rust.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.axum ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
