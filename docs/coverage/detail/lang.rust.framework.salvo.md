<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.salvo` — Salvo

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
| Endpoint pagination posture | ✅ `full` | `2026-06-13` | 5019 | `internal/custom/rust/endpoint_pagination.go`<br>`internal/custom/rust/endpoint_pagination_test.go` | #5019 (child of #3628) Rust port: paginated/pagination_style/pagination_params/pagination_source stamped at the SOURCE by re-emitting the SCOPE.Operation/endpoint op so it merges onto the producer route op by Name (same approach as endpoint_response_codes.go; the engine applyEndpointPagination pass is gated on http_endpoint_definition and cannot reach Rust SCOPE.Operation custom entities). salvo: `Router::with_path("p").get(handler)` verb chain names a handler whose body/signature carries the pagination signals. Same handler->verdict map as axum; honest-partial holds. HONEST-PARTIAL (shared classifier): a lone limit-like param or a lone `.limit()` with no offset companion is ambiguous and NOT stamped (TestRustPagination_LoneLimitNotStamped, _LimitOnlyOrmNotStamped); a handler with no pagination shape is not re-emitted (TestRustPagination_NoPaginationNotStamped). Styles: limit+offset->offset, page->page, cursor/after/before/page_token->cursor. hyper (raw match-arm dispatch) and tower (no HTTP routes) remain missing (no named handler) — deferred follow-up. |
| Endpoint response codes | ✅ `full` | `2026-06-12` | 5018 | `internal/custom/rust/endpoint_response_codes.go`<br>`internal/custom/rust/endpoint_response_codes_test.go` | #5018 (child of #3628, follow-up to #4965) Rust port: response_codes/success_code/response_codes_source stamped at the SOURCE by re-emitting the SCOPE.Operation/endpoint op so it merges onto the producer route op by Name (same handler-name correlation as axum; the engine applyEndpointResponseCodes pass is gated on http_endpoint_definition and cannot reach Rust SCOPE.Operation custom entities). extractHandlerNamed builds a handler->verdict map from every fn body once, then re-runs each framework's PRODUCER route regexes (minor_fw_routing.go) and stamps the verdict onto routes naming a resolving handler. poem: .at("/p", post(h)) + Response::builder().status(StatusCode::X)/bare StatusCode::X. warp: filter chain .and_then(h)/.map(h) + warp::reply::with_status(reply, StatusCode::X). tide: .at("/p").post(h) + Response::builder(NNN)/.set_status(NNN) positional status. gotham: route.verb("/p").to(h)/.associate + (StatusCode::X, body) create_response. salvo: Router::with_path("p").verb(h) + res.status_code(StatusCode::X). Shared StatusCode/Status name table + numeric constructors. Honest-partial: a route whose handler resolves no literal status is left to the producer (no fabricated 200). Value-asserted: TestRustRespCodes_PoemHandlerStatus, _WarpWithStatus, _TideBuilderStatus, _GothamRouteStatus, _SalvoStatusCode (+ _MinorFwNoStatusNotStamped negative). hyper (raw match-arm, no named handler) and tower (no HTTP routes) DEFERRED to a follow-up. |
| Endpoint synthesis | ✅ `full` | `2026-06-03` | — | `internal/custom/rust/minor_fw_routing.go`<br>`internal/custom/rust/minor_fw_routing_test.go` | #3982: salvo chained method routers Router::new().path("/p").get(a).post(b) — each verb now synthesises its own endpoint with the path preserved across lines; Router::with_path("p") now carries its path onto the endpoint (rooted to /p) instead of dropping it. Value-asserting tests TestSalvoChainedVerbs + TestSalvoWithPathEndpoint. |
| Handler attribution | ✅ `full` | `2026-06-03` | — | `internal/custom/rust/minor_fw_routing.go`<br>`internal/custom/rust/minor_fw_routing_test.go` | #3982: every verb in a salvo chained router is attributed to its own handler. Asserted by TestSalvoChainedVerbs + TestSalvoWithPathEndpoint. |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/helpers.go`<br>`internal/custom/rust/minor_fw_routing.go`<br>`internal/custom/rust/minor_fw_routing_test.go` | Router with_path/.path().verb(h); <id> path params normalised to {id} |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-05-30` | — | `internal/custom/rust/auth.go`<br>`internal/custom/rust/auth_policy.go` | heuristic auth-signal detection (jwt/bearer/apikey/session keywords + middleware-name classification); no framework-specific guard-to-validator binding modelled for salvo |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | `2026-06-12` | backfill:dictionary-completeness | `internal/custom/rust/fw_validation.go`<br>`internal/extractors/rust/issue4854_field_membership_test.go`<br>`internal/extractors/rust/struct_fields.go` | serde/validator DTO field+constraint decomposition (field name, type, #[serde] rename_all/rename/default/skip/flatten, #[validate(length/range/email/regex/custom/nested)] with specific bounds) is implemented in fw_validation.go and proven by value-asserting tests. Flipped full only for flagship axum/actix/rocket where DTOs are typed extractor payloads (Json/Form). This framework's body-deser path is value-untyped at the extraction site (serde_json::from_slice / body_json), so DTO-to-extractor binding is not statically proven here — honest-partial pending cross-file type-flow. #4854: the serde/utoipa/ORM-gated custom emitters only emitted field members for bound DTOs; the GENERAL primary-pass now emits a SCOPE.Schema/field entity + struct->field CONTAINS for EVERY named struct field (serde rename wire name honoured, serde skip excluded, Name '<Struct>.<wire>' dedups by Name in MergeWithCustom) and for named fields of struct-style enum variants ('<Enum>.<Variant>.<field>'), so any Rust data struct projects field rows in the dashboard shape tree — closing the same gap #4845/#4851 fixed for JS/TS and #4850/#4855 for Go. Rust has no inheritance so there is no EXTENDS. emitRustStructFields/emitRustEnumVariantFields in rust/struct_fields.go; value-asserted by TestRustStructFieldsAreContained/TestRustEnumVariantFieldsAreContained. |
| Request validation | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/rust/fw_validation.go` | serde/validator DTO field+constraint decomposition (field name, type, #[serde] rename_all/rename/default/skip/flatten, #[validate(length/range/email/regex/custom/nested)] with specific bounds) is implemented in fw_validation.go and proven by value-asserting tests. Flipped full only for flagship axum/actix/rocket where DTOs are typed extractor payloads (Json/Form). This framework's body-deser path is value-untyped at the extraction site (serde_json::from_slice / body_json), so DTO-to-extractor binding is not statically proven here — honest-partial pending cross-file type-flow. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | `2026-05-30` | — | `internal/custom/rust/auth.go`<br>`internal/custom/rust/auth_policy.go` | framework middleware registration surface detected (with/middleware/attach/filter/add_middleware) but salvo layer chains are not ordered/enumerated like tower |
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
| DB effect | 🟢 `partial` | `2026-06-11` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_cross_orm_read_4692_test.go`<br>`internal/substrate/effect_sinks_rust.go` | #4737 (Rust slice of the #4692 cross-ORM receiver-typed read-reach audit): the ambiguous Diesel/sea-orm read terminals (.first/.find/.filter/.select/.all/.one + .order/.limit/.offset/.join) that collide with Rust Iterator combinators are now credited db_read ONLY on a query/table/Entity-typed receiver (Diesel schema::table root, .into_boxed()/QueryDsl chain, sea-orm Entity::find()) -- propagated across let q2 = q.filter(...) chains to a fixpoint and matched inline off a query root (users::table.filter(...).first(conn)). The distinctive terminals (sqlx::query!, .fetch_*, diesel::select/sql_query, .load/.get_result(s), .find_by_id/.stream/.paginate) stay bare on any receiver. vec.iter().filter(...).find(...) / slice.first() stay PURE (over-credit guard). Value-asserted in TestRustDieselSeaOrmTypedRead_4737 / TestRustIteratorNoFalsePositive_4737 / TestRustRepoReadChainSink_4737. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🟢 `partial` | `2026-06-12` | config_consumption:#5079-keyless-envy-figment-extract-deferred | `internal/extractor/config_key.go`<br>`internal/extractors/rust/config_consumer.go`<br>`internal/extractors/rust/config_consumer_test.go`<br>`internal/extractors/rust/rust.go` | #5020+#5079: literal env/config-crate key reads emit the config-consumption topology — env::var(K)/std::env::var/env::var_os, dotenvy::var(K), figment Env::prefixed(P), and (#5079) the config crate typed getters cfg.get_string/get_int/get_bool/get_float(K) + turbofish cfg.get::<T>(K) — each becomes a shared SCOPE.Config/config_key node + a DEPENDS_ON_CONFIG edge (pattern=config_crate) from the reading function (receiver-qualified Foo.method), via emitConfigConsumerEdges -> extractor.EmitConfigReads. Honest-partial: only LITERAL string keys recorded — dynamic env::var(name) and bare HashMap .get(k) yield nothing; the truly KEYLESS crate APIs envy::from_env::<T>() and Figment::new().merge(...).extract::<T>() (whole-struct deserialise, no single literal key) remain deferred (#5079 follow-up). Value-asserted: TestRustConfig_EnvVar/Dotenvy/FigmentPrefix/MethodHostName/ConfigCrateGetters/ConfigCrateTurbofish/BareGetNotConfig/DynamicKeySkipped. |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_rust.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/rust/exception_flow.go`<br>`internal/extractors/rust/exception_flow_test.go` | Err(Type::ctor())/Err(Type::Variant)/Err(Type(..)) + bail!/ensure!(Type::X) + .ok_or(Type::X)/.ok_or_else(||Type::X) -> THROWS (enum variant normalized to leading-segment ENUM type); match Err(Type)/if let Err(Type)/.map_err(|e: Type|) -> CATCHES; bare ? propagation, Box<dyn Error>, string panic!, Err(var)/Err(make()) re-raise dropped (honest-partial, #3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-12` | feature_flag_gating:#5079-cfg-combinator-keys-and-attribute-attribution-deferred | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go` | #5079: Rust conditional-compilation feature gating — cfg!(feature=x) macro + #[cfg(feature=x)] / #[cfg_attr(feature=x,...)] attributes — emits a SCOPE.FeatureFlag entity (feature:<key>, subtype rust-cfg) + a GATED_BY edge from the enclosing function, via a lang-gated matcher in applyFeatureFlagEdges (distinct from the runtime flag-SDK model). Honest-partial: a cfg! macro in a function body attributes to that function; a #[cfg(...)] attribute precedes its item and attributes to prior-function/file scope (same caveat as .NET [FeatureGate]); a multi-feature combinator all(...)/any(...) captures only the FIRST feature key. Value-asserted: TestFeatureFlag_Rust_cfg_macro/cfg_attribute/cfg_combinator_firstKey/cfg_langGated_noFabrication. |
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
(or use `go run ./tools/coverage update lang.rust.framework.salvo ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
