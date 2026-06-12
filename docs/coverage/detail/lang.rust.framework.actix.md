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
| Endpoint deprecation versioning | ✅ `full` | `2026-06-03` | 4152 | `internal/custom/rust/endpoint_deprecation.go`<br>`internal/custom/rust/endpoint_deprecation_test.go` | #4152 (child of #3628) Rust port: deprecated/deprecation_source(+deprecated_since/deprecated_replacement)+path-derived api_version stamped at the SOURCE by re-emitting the SCOPE.Operation/endpoint op so it merges onto the producer route op by Name. Rust HTTP endpoints are SCOPE.Operation custom-extractor entities the engine resolveEndpointDeprecation pass (gated on http_endpoint_definition) cannot reach, so the contract is stamped in the custom-extractor stage (Kotlin/Scala/PHP precedent). The Rust stdlib #[deprecated(since = "2.0", note = "use /api/v2/...")] attribute credits deprecated=true+deprecated_since+deprecated_replacement; a rustdoc @deprecated tag, a // DEPRECATED banner, and a Sunset/Deprecation response header (RFC 8594, headers.insert("Sunset", ...)) also fire. api_version is path-derived from /api/vN or /vN route segments. actix #[deprecated] is stacked with the #[get("/p")] route macro above the fn (above OR below the macro). Value-asserted TestRustDep_ActixDeprecatedMacro (since=3.0, replacement=/v2/items, api_version=1) + TestRustDep_ActixDeprecatedBelowMacro (deprecated below the route macro). Identical property contract to the flagship. Negatives: TestRustDep_NonDeprecatedVersionlessNone (plain route not re-emitted), TestRustDep_NonRouteDeprecatedUnaffected (non-route #[deprecated] helper), TestRustDep_VersionlessNoApiVersion. |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | ✅ `full` | `2026-06-12` | 4965 | `internal/custom/rust/endpoint_response_codes.go`<br>`internal/custom/rust/endpoint_response_codes_test.go` | #4965 (child of #3628) Rust port: response_codes/success_code/response_codes_source stamped at the SOURCE by re-emitting the SCOPE.Operation/endpoint op (see endpoint_response_codes.go; same custom-extractor approach as endpoint_deprecation.go). actix: the #[get("/p")] macro names the fn that follows; status comes from HttpResponse::Created()/Ok()/NotFound()... builders (method name -> code), .status(StatusCode::X)/.status(404), and StatusCode::X enum refs in the fn body. success_code is the lone 2xx. Value-asserted TestRustRespCodes_ActixBuilders (POST /items=201 via HttpResponse::Created(), source=HttpResponse::Created()), TestRustRespCodes_ActixMultiCode (GET /items/{id}=200,404 success=200). Honest-partial identical to axum (no literal -> not re-emitted; dynamic status skipped). |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/rust/frameworks/actix_web.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/rust/frameworks/actix_web.yaml` | — |
| Route extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/rust/actix_web.go`<br>`internal/custom/rust/extractors_test.go`<br>`internal/custom/rust/helpers.go` | Extracts attribute-macro and manual web::get().to() routes; normalises path params; composes web::scope() prefix onto manual routes. #4921: WebSocket handlers ARE recovered too — reActixWebSocket (ws::WebsocketContext<H>) synthesises a SCOPE.Operation/websocket entity per handler (provenance INFERRED_FROM_ACTIX_WEBSOCKET, handler_name stamped); documented here as the http_backend taxonomy has no dedicated websocket_route_extraction key. |

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
| DTO extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/rust/fw_validation.go`<br>`internal/extractors/rust/issue4854_field_membership_test.go`<br>`internal/extractors/rust/struct_fields.go` | Detects #[derive(Deserialize)] and #[derive(Validate)] structs; actix web::Json/Query/Form/Path<T> extractors #4854: the serde/utoipa/ORM-gated custom emitters only emitted field members for bound DTOs; the GENERAL primary-pass now emits a SCOPE.Schema/field entity + struct->field CONTAINS for EVERY named struct field (serde rename wire name honoured, serde skip excluded, Name '<Struct>.<wire>' dedups by Name in MergeWithCustom) and for named fields of struct-style enum variants ('<Enum>.<Variant>.<field>'), so any Rust data struct projects field rows in the dashboard shape tree — closing the same gap #4845/#4851 fixed for JS/TS and #4850/#4855 for Go. Rust has no inheritance so there is no EXTENDS. emitRustStructFields/emitRustEnumVariantFields in rust/struct_fields.go; value-asserted by TestRustStructFieldsAreContained/TestRustEnumVariantFieldsAreContained. |
| Request validation | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/fw_validation.go` | Detects #[validate(...)] field attrs, .validate() calls, actix extractor types |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/actix_web.go`<br>`internal/custom/rust/auth.go`<br>`internal/custom/rust/auth_policy.go`<br>`internal/custom/rust/auth_policy_test.go` | custom Transform<S,ServiceRequest> impls + .wrap() registrations captured with middleware_name/middleware_trait |
| Rate limit stamping | 🟢 `partial` | `2026-06-03` | — | `internal/custom/rust/rate_limit.go`<br>`internal/custom/rust/rate_limit_test.go` | #4124 greenfield: custom_rust_rate_limit stamps the flat contract (rate_limited/rate_limit/rate_limit_scope/rate_limit_source/limit/period/rate_limit_burst) for actix-governor — a .wrap(Governor::new(&conf)) on the App, resolving GovernorConfigBuilder::default().per_second(N).burst_size(M) when literal (scope=app, source=actix_governor). Partial: rate omitted when non-literal/cross-statement. Negatives: a plain route does not stamp. |

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
| DI binding extraction | ✅ `full` | — | 4963 | `internal/custom/rust/di_graph.go`<br>`internal/custom/rust/di_graph_test.go` | #4963 (follow-up to #4921): the actix-web app-data REGISTRATION site App::app_data(web::Data::new(T)) (also .app_data(Data::new(T)) and a smart-pointer-wrapped Arc/Rc/Box/Mutex/RwLock::new(T) inner type) binds the web::Data<T> injection point. custom_rust_di_graph emits a SCOPE.Pattern(di_binding) owner carrying injected_type/mechanism=data/scope=singleton plus a BINDS edge (bound type -> actix_web_registration:<T> site token) and an INJECTED_INTO edge (bound type -> every handler whose signature extracts web::Data<T>) -- the concrete provider->consumer edge the type-keyed di_injection_point pattern could not form (handler resolved by scanning each fn signature). Cross-file resolution via the global byName index (Go/Python DI-graph precedent). Proven by TestActixAppDataBindingAndInjection (scope=singleton + BINDS + INJECTED_INTO DbPool->handler); negative TestRustDINoRegistrationNoBinding (no binding without a registration site). |
| DI injection point | ✅ `full` | `2026-06-12` | — | `internal/custom/rust/actix_web.go`<br>`internal/custom/rust/extractors_test.go` | #4921: actix-web web::Data<T> is the app-data DI container (App::app_data(web::Data::new(T))) injected into handlers; it is now split out of the request-shape extractors and emitted as SCOPE.Pattern(di_injection_point) with di_framework=actix_web, injected_type=<T>, mechanism=data (web::Json/Path/Query/Form stay request-shape SCOPE.Schema). Proven by TestActixDataDIInjection (asserts Data->di_injection_point + mechanism=data, and that Json stays a schema). di_binding_extraction (app_data registration) and di_scope_resolution remain honest-missing follow-ups. |
| DI scope resolution | ✅ `full` | — | 4963 | `internal/custom/rust/di_graph.go`<br>`internal/custom/rust/di_graph_test.go` | #4963: actix-web web::Data<T> is an APP-SINGLETON (one value shared for the process lifetime, registered once via App::app_data); custom_rust_di_graph derives scope=singleton (rustDIScope, mechanism=data) and stamps it on the di_binding owner and the BINDS/INJECTED_INTO edge props. Proven by TestActixAppDataBindingAndInjection (scope=singleton). |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-11` | — | `internal/custom/rust/tests_route_e2e.go`<br>`internal/custom/rust/tests_route_e2e_test.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/engine/http_endpoint_e2e_testmap_4749_test.go`<br>`internal/extractors/cross/testmap/frameworks.go` | #4749 (Rust slice of epic #4615 test->endpoint coverage linkage): route-hit linkage stamps e2e_route_calls from Actix test::TestRequest::<verb>().uri(path)+call_service, Axum/tower app.oneshot(Request::<verb>(path))/Request::builder().method(Method::X).uri(path), Rocket client.<verb>(path).dispatch(), and reqwest test-server client.<verb>(format!({}/path, addr)); the shared linkE2ERouteTestsToEndpoints pass emits the endpoint TESTS edge. Rust tests are NAMED #[test]/#[tokio::test]/#[actix_web::test] fns (no closure DSL -> no scope-owner). Local-variable handler-receiver typing N/A: Rust handlers are free functions wired by path, not constructed/called in-test. Variable-only routes dropped. Value-asserted in tests_route_e2e_test.go + http_endpoint_e2e_testmap_4749_test.go. |

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
| Config consumption | 🟢 `partial` | `2026-06-12` | — | `internal/extractor/config_key.go`<br>`internal/extractors/rust/config_consumer.go`<br>`internal/extractors/rust/config_consumer_test.go`<br>`internal/extractors/rust/rust.go` | #5020: literal env/config-crate key reads now emit the config-consumption topology — env::var("K")/std::env::var/env::var_os, dotenvy::var("K"), and figment Env::prefixed("P") become a shared SCOPE.Config/config_key node + a DEPENDS_ON_CONFIG edge from the reading function (receiver-qualified Foo.method for impl blocks), via emitConfigConsumerEdges -> extractor.EmitConfigReads (parity with go/java/php/python config-key blast radius). Honest-partial: only LITERAL string keys are recorded — dynamic env::var(name) yields no node/edge; keyless crate APIs envy::from_env::<T>() and config::Config::builder() (whole-struct deserialise / source merge with no single literal key) are deferred. Value-asserted: TestRustConfig_EnvVar/Dotenvy/FigmentPrefix/MethodHostName/DynamicKeySkipped. |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_rust.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/rust/exception_flow.go`<br>`internal/extractors/rust/exception_flow_test.go` | Err(Type::ctor())/Err(Type::Variant)/Err(Type(..)) + bail!/ensure!(Type::X) + .ok_or(Type::X)/.ok_or_else(||Type::X) -> THROWS (enum variant normalized to leading-segment ENUM type); match Err(Type)/if let Err(Type)/.map_err(|e: Type|) -> CATCHES; bare ? propagation, Box<dyn Error>, string panic!, Err(var)/Err(make()) re-raise dropped (honest-partial, #3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-06-11` | 4703 | `internal/external/synth.go`<br>`internal/external/synth_named_imports_ruby_rust_4783_test.go`<br>`internal/extractors/rust/import_contract_4783_test.go`<br>`internal/extractors/rust/rust.go`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/rust.go`<br>`internal/substrate/substrate.go` | #4783: Rust use-declaration IMPORTS edges now stamp the imported_name/local_name (+ wildcard) contract for the #4515 per-symbol external-node synth. use tokio::sync::Mutex; -> ext:tokio:Mutex end-to-end; brace groups use serde::{Serialize, Deserialize as De} fan out to one leaf-stamped edge each (alias keeps imported=leaf, local=alias); glob use std::collections::* stamps wildcard=1 with the namespace local; intra-crate crate::/self::/super:: stay unstamped (internal). Value-asserted TestRust_ImportContract_SingleNamed/_BraceGroupAndAlias/_Wildcard + end-to-end TestSynthesize_Rust_PerSymbolNode_4783/_AliasConverge/_CrossCrateNoCollision. Honest-partial: re-export chains and macro-generated uses are not statically followed. |
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
