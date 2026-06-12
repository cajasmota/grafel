<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.utoipa` — utoipa

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
| Endpoint synthesis | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | each #[utoipa::path] attribute -> SCOPE.Operation endpoint (verb + canonical path) |
| Handler attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | handler fn name captured from the fn following each #[utoipa::path] attribute |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | utoipa::path(verb, path=...) yields canonical verb+path (normalises {id}/:id/<id>); captures handler fn; enriches bare axum/actix routes with documented contract |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go`<br>`internal/extractors/rust/issue4854_field_membership_test.go`<br>`internal/extractors/rust/struct_fields.go` | #[derive(ToSchema)]/IntoParams structs -> SCOPE.Schema DTO with deep fields; request_body=/responses(body=) refs emitted as request/response DTOs #4854: the serde/utoipa/ORM-gated custom emitters only emitted field members for bound DTOs; the GENERAL primary-pass now emits a SCOPE.Schema/field entity + struct->field CONTAINS for EVERY named struct field (serde rename wire name honoured, serde skip excluded, Name '<Struct>.<wire>' dedups by Name in MergeWithCustom) and for named fields of struct-style enum variants ('<Enum>.<Variant>.<field>'), so any Rust data struct projects field rows in the dashboard shape tree — closing the same gap #4845/#4851 fixed for JS/TS and #4850/#4855 for Go. Rust has no inheritance so there is no EXTENDS. emitRustStructFields/emitRustEnumVariantFields in rust/struct_fields.go; value-asserted by TestRustStructFieldsAreContained/TestRustEnumVariantFieldsAreContained. |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-06-03` | 3980 | `internal/extractors/rust/rust.go`<br>`internal/extractors/rust/rust_test.go` | #3980: the language-level `rust` extractor (internal/extractors/rust/rust.go, registered unconditionally as "rust" with no framework gating) emits enum_item -> SCOPE.Component subtype="enum" with variants/generics/derives props for every .rs file. Value-asserting probe TestRustExtractor_TypeSystem_PerFramework drives a utoipa-style file through the extractor and asserts the enum entity fires. |
| Interface extraction | ✅ `full` | `2026-06-03` | 3980 | `internal/extractors/rust/rust.go`<br>`internal/extractors/rust/rust_test.go` | #3980: the language-level `rust` extractor (rust.go, unconditional per-language) emits trait_item -> SCOPE.Component subtype="trait" with methods/supertraits/generics + EXTENDS edges for every .rs file. Probe TestRustExtractor_TypeSystem_PerFramework asserts the trait entity fires on a utoipa-style file. |
| Type alias extraction | ✅ `full` | `2026-06-03` | 3980 | `internal/extractors/rust/rust.go`<br>`internal/extractors/rust/rust_test.go` | #3980: the language-level `rust` extractor (rust.go, unconditional per-language) emits type_item -> SCOPE.Component subtype="type_alias" with aliased_type/generics props for every .rs file. Probe TestRustExtractor_TypeSystem_PerFramework asserts the type_alias entity + its aliased_type prop on a utoipa-style file. |
| Type extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | ToSchema struct fields parsed (name/type/wire_name) -> schema_field entities |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-06-04` | backfill:dictionary-completeness | `internal/custom/rust/observability.go`<br>`internal/custom/rust/observability_auth_test.go` | tracing info!/warn!/error!/debug!/trace! (qualified + bare), log::*, event!(Level,..), slog::*, #[instrument]; level+library captured, static message head captured when leading string literal. utoipa-only handler/doc modules (#[utoipa::path]) attributed via the utoipa import marker (parity-grind-rust), proven by value-asserting TestRustObs_FrameworkAttribution_Utoipa (asserts framework="utoipa"). Stays PARTIAL: messages are often format strings with interpolated/structured fields, and logger->subscriber/appender binding is cross-file (same limitation as PHP/Java/Ruby per-framework log cells) |
| Metric extraction | ✅ `full` | `2026-06-04` | — | `internal/custom/rust/observability.go`<br>`internal/custom/rust/observability_auth_test.go` | metrics crate counter!/gauge!/histogram!("name"), prometheus register_*!/IntCounter::new/Opts::new("name"), opentelemetry meter.u64_counter("name"); metric NAME captured as observability_name + observability_kind/library props. utoipa-only handler modules attributed via the utoipa import marker (parity-grind-rust); value-asserting TestRustObs_FrameworkAttribution_Utoipa asserts the exact metric entity obs:metrics:metrics_macro:counter:utoipa_requests_total with observability_name="utoipa_requests_total" + framework="utoipa". Per-call-site literal name needs no cross-file resolution; binding meter->exporter stays out of scope |
| Trace extraction | ✅ `full` | `2026-06-04` | — | `internal/custom/rust/observability.go`<br>`internal/custom/rust/observability_auth_test.go` | tracing span!(Level,"name")/info_span!("name"), opentelemetry global::tracer("svc")/tracer.start("name")/span_builder("name"); span NAME captured as observability_name. utoipa-only handler modules attributed via the utoipa import marker (parity-grind-rust); value-asserting TestRustObs_FrameworkAttribution_Utoipa asserts the exact span entity obs:tracing:tracing_level_span:info:utoipa_get_user with observability_name="utoipa_get_user" + framework="utoipa". Literal span name needs no cross-file resolution; #[instrument]-derived names and tracer->exporter binding stay out of scope |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🟢 `partial` | `2026-06-12` | — | `internal/extractor/config_key.go`<br>`internal/extractors/rust/config_consumer.go`<br>`internal/extractors/rust/config_consumer_test.go`<br>`internal/extractors/rust/rust.go` | #5020: literal env/config-crate key reads now emit the config-consumption topology — env::var("K")/std::env::var/env::var_os, dotenvy::var("K"), and figment Env::prefixed("P") become a shared SCOPE.Config/config_key node + a DEPENDS_ON_CONFIG edge from the reading function (receiver-qualified Foo.method for impl blocks), via emitConfigConsumerEdges -> extractor.EmitConfigReads (parity with go/java/php/python config-key blast radius). Honest-partial: only LITERAL string keys are recorded — dynamic env::var(name) yields no node/edge; keyless crate APIs envy::from_env::<T>() and config::Config::builder() (whole-struct deserialise / source merge with no single literal key) are deferred. Value-asserted: TestRustConfig_EnvVar/Dotenvy/FigmentPrefix/MethodHostName/DynamicKeySkipped. |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | #3980 wave1-structural: reachability/dead-code BFS flags unreferenced utoipa #[utoipa::path] OpenAPI-annotated axum handlers (async fn get_item); rust entry points seeded by entry_points_rust.go. |
| Def use chain extraction | 🟢 `partial` | `2026-06-03` | 3980 | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_rust.go` | #3980 wave1-structural: language-level rust def-use sniffer (def_use_rust.go, registers on "rust" slug, framework-agnostic) fires on utoipa #[utoipa::path] OpenAPI-annotated axum handlers (async fn get_item). Probe TestW1jr_DefUseRust_UtoipaHandler asserts exact (fn,var) def/use pairs. |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/rust/exception_flow.go`<br>`internal/extractors/rust/exception_flow_test.go` | Err(Type::ctor())/Err(Type::Variant)/Err(Type(..)) + bail!/ensure!(Type::X) + .ok_or(Type::X)/.ok_or_else(||Type::X) -> THROWS (enum variant normalized to leading-segment ENUM type); match Err(Type)/if let Err(Type)/.map_err(|e: Type|) -> CATCHES; bare ? propagation, Box<dyn Error>, string panic!, Err(var)/Err(make()) re-raise dropped (honest-partial, #3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🟢 `partial` | `2026-06-03` | 3980 | `internal/links/module_cycle_pass.go` | #3980 wave1-structural: Tarjan SCC over IMPORTS detects cycles among utoipa modules; rust mod/use IMPORTS emitted by the rust extractor. |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🟢 `partial` | `2026-06-03` | 3980 | `internal/links/pure_function_pass.go` | #3980 wave1-structural: language-agnostic pure-function pass tags utoipa #[utoipa::path] OpenAPI-annotated axum handlers (async fn get_item) left un-stamped by the effect pass; same rust idiom proven in TestW1jr_DefUseRust_UtoipaHandler. |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | #3980 wave1-structural: reachability BFS reaches utoipa #[utoipa::path] OpenAPI-annotated axum handlers (async fn get_item) through CALLS/IMPORTS edges from the rust extractor; entry points via entry_points_rust.go. |
| Request shape extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | request_body = <DTO> (incl inline()/content=) -> request_dto tied to verb+path |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/utoipa.go`<br>`internal/custom/rust/utoipa_test.go` | responses((status=N, body=<DTO>)) -> response_dto per status, tied to verb+path |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.utoipa ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
