<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.async-graphql` — async-graphql

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
| Endpoint synthesis | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/async_graphql.go`<br>`internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go` | Synthesizes verb GRAPHQL endpoints from resolver impl blocks; Schema::build root captured as SCOPE.Service |
| Handler attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/async_graphql.go`<br>`internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go` | handler_name=<Root>.<field> attributed per resolver method |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/async_graphql.go`<br>`internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go` | Each #[Object] impl Query/Mutation/Subscription resolver method becomes a GRAPHQL endpoint at /graphql/<Root>/<field>; operation kind derived from impl root |

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
| DTO extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/rust/async_graphql.go`<br>`internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go`<br>`internal/extractors/rust/issue4854_field_membership_test.go`<br>`internal/extractors/rust/struct_fields.go` | #[derive(SimpleObject/InputObject/MergedObject)] structs + #[derive(Enum)] enums emitted as SCOPE.Schema DTOs with role (object/input/enum) #4854: the serde/utoipa/ORM-gated custom emitters only emitted field members for bound DTOs; the GENERAL primary-pass now emits a SCOPE.Schema/field entity + struct->field CONTAINS for EVERY named struct field (serde rename wire name honoured, serde skip excluded, Name '<Struct>.<wire>' dedups by Name in MergeWithCustom) and for named fields of struct-style enum variants ('<Enum>.<Variant>.<field>'), so any Rust data struct projects field rows in the dashboard shape tree — closing the same gap #4845/#4851 fixed for JS/TS and #4850/#4855 for Go. Rust has no inheritance so there is no EXTENDS. emitRustStructFields/emitRustEnumVariantFields in rust/struct_fields.go; value-asserted by TestRustStructFieldsAreContained/TestRustEnumVariantFieldsAreContained. |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/rust/async_graphql.go`<br>`internal/custom/rust/graphql_codefirst_typegraph.go`<br>`internal/custom/rust/graphql_codefirst_typegraph_test.go` | #3983: new internal/custom/rust/graphql_codefirst_typegraph.go mirrors the py/jsts code-first type-graph extractors (completes #3804 for Rust). Emits SCOPE.Schema/type nodes (BuildOperationStructuralRef("graphql",file,Type), shared identity with the SDL #3805 pass) + GRAPH_RELATES field->type edges off #[derive(SimpleObject/MergedObject)] struct fields and #[Object] impl resolver return types, carrying the SDL cardinality contract (field_name/list/nullable/item_nullable/cardinality/self_ref). Probe TestGqlTG_SimpleObject_FieldGraph asserts User.orders Vec<Order> to_many + Option<Account> nullable to_one + scalar fields no edge; TestGqlTG_ResolverReturnType asserts Query.user Result<User> unwrap. Honest-partial: same-file resolution only; InputObject/Enum stay DTO-catalog. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/async_graphql.go`<br>`internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go` | #[derive(Enum)] GraphQL enums recovered as DTOs |
| Interface extraction | ✅ `full` | `2026-06-03` | 3980 | `internal/extractors/rust/rust.go`<br>`internal/extractors/rust/rust_test.go` | #3980: the language-level `rust` extractor (rust.go, unconditional per-language) emits trait_item -> SCOPE.Component subtype="trait" with methods/supertraits/generics + EXTENDS edges for every .rs file. Probe TestRustExtractor_TypeSystem_PerFramework asserts the trait entity fires on a async-graphql-style file. |
| Type alias extraction | ✅ `full` | `2026-06-03` | 3980 | `internal/extractors/rust/rust.go`<br>`internal/extractors/rust/rust_test.go` | #3980: the language-level `rust` extractor (rust.go, unconditional per-language) emits type_item -> SCOPE.Component subtype="type_alias" with aliased_type/generics props for every .rs file. Probe TestRustExtractor_TypeSystem_PerFramework asserts the type_alias entity + its aliased_type prop on a async-graphql-style file. |
| Type extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/async_graphql.go`<br>`internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go` | GraphQL DTO type names recovered from derive macros |

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
| Log extraction | 🟢 `partial` | `2026-06-03` | 3981 | `internal/custom/rust/observability.go`<br>`internal/custom/rust/observability_auth_test.go` | #3981: the framework-agnostic Rust observability scanner (internal/custom/rust/observability.go) recognises tracing/log/slog macros + #[instrument] on any .rs file; the #3981 import marker now attributes async-graphql files to this cell. Probe TestRustObs_FrameworkAttribution_TonicAsyncGraphql asserts a async-graphql file emits a tracing log entity with framework="async-graphql". Stays partial-equivalent for message binding per the scanner's documented log honesty note, but detection + attribution fire. |
| Metric extraction | ✅ `full` | `2026-06-03` | 3981 | `internal/custom/rust/observability.go`<br>`internal/custom/rust/observability_auth_test.go` | #3981: the framework-agnostic observability scanner (observability.go) captures metric NAMEs (metrics!/prometheus/otel meter) at the call site on any .rs file; the #3981 async-graphql import marker attributes them to this cell. The same value-asserting metric-name machinery proven for axum applies — async-graphql services that emit these metric macros are now credited. |
| Trace extraction | ✅ `full` | `2026-06-03` | 3981 | `internal/custom/rust/observability.go`<br>`internal/custom/rust/observability_auth_test.go` | #3981: the framework-agnostic observability scanner (observability.go) captures span NAMEs (span!/info_span!/otel tracer + #[instrument]) at the call site on any .rs file; the #3981 async-graphql import marker attributes them to this cell. Probe TestRustObs_FrameworkAttribution_TonicAsyncGraphql asserts a async-graphql file emits a span entity with framework="async-graphql". |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | `2026-06-11` | 4218 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_cross_orm_read_4692_test.go`<br>`internal/substrate/effect_sinks_rust.go` | #4737 (Rust slice of the #4692 cross-ORM receiver-typed read-reach audit): the ambiguous Diesel/sea-orm read terminals (.first/.find/.filter/.select/.all/.one + .order/.limit/.offset/.join) that collide with Rust Iterator combinators are now credited db_read ONLY on a query/table/Entity-typed receiver (Diesel schema::table root, .into_boxed()/QueryDsl chain, sea-orm Entity::find()) -- propagated across let q2 = q.filter(...) chains to a fixpoint and matched inline off a query root (users::table.filter(...).first(conn)). The distinctive terminals (sqlx::query!, .fetch_*, diesel::select/sql_query, .load/.get_result(s), .find_by_id/.stream/.paginate) stay bare on any receiver. vec.iter().filter(...).find(...) / slice.first() stay PURE (over-credit guard). Value-asserted in TestRustDieselSeaOrmTypedRead_4737 / TestRustIteratorNoFalsePositive_4737 / TestRustRepoReadChainSink_4737. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🟢 `partial` | `2026-06-12` | — | `internal/extractor/config_key.go`<br>`internal/extractors/rust/config_consumer.go`<br>`internal/extractors/rust/config_consumer_test.go`<br>`internal/extractors/rust/rust.go` | #5020: literal env/config-crate key reads now emit the config-consumption topology — env::var("K")/std::env::var/env::var_os, dotenvy::var("K"), and figment Env::prefixed("P") become a shared SCOPE.Config/config_key node + a DEPENDS_ON_CONFIG edge from the reading function (receiver-qualified Foo.method for impl blocks), via emitConfigConsumerEdges -> extractor.EmitConfigReads (parity with go/java/php/python config-key blast radius). Honest-partial: only LITERAL string keys are recorded — dynamic env::var(name) yields no node/edge; keyless crate APIs envy::from_env::<T>() and config::Config::builder() (whole-struct deserialise / source merge with no single literal key) are deferred. Value-asserted: TestRustConfig_EnvVar/Dotenvy/FigmentPrefix/MethodHostName/DynamicKeySkipped. |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | #3980 wave1-structural: reachability/dead-code BFS flags unreferenced async-graphql #[Object] resolver methods (async fn users); rust entry points seeded by entry_points_rust.go. |
| Def use chain extraction | 🟢 `partial` | `2026-06-03` | 3980 | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_rust.go` | #3980 wave1-structural: language-level rust def-use sniffer (def_use_rust.go, registers on "rust" slug, framework-agnostic) fires on async-graphql #[Object] resolver methods (async fn users). Probe TestW1jr_DefUseRust_AsyncGraphqlResolver asserts exact (fn,var) def/use pairs. |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/rust/exception_flow.go`<br>`internal/extractors/rust/exception_flow_test.go` | Err(Type::ctor())/Err(Type::Variant)/Err(Type(..)) + bail!/ensure!(Type::X) + .ok_or(Type::X)/.ok_or_else(||Type::X) -> THROWS (enum variant normalized to leading-segment ENUM type); match Err(Type)/if let Err(Type)/.map_err(|e: Type|) -> CATCHES; bare ? propagation, Box<dyn Error>, string panic!, Err(var)/Err(make()) re-raise dropped (honest-partial, #3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🟢 `partial` | `2026-06-03` | 4218 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go`<br>`internal/substrate/graphql_client_effects_4218_test.go` | #4218 (verify-first): per-LANGUAGE effect_sinks_rust.go http_out detector (rustHTTPRe reqwest::Client::new()...send().await) fires on an async-graphql resolver driving an outbound call, attributed to create_order. Proven by TestSubstrate_Rust_AsyncGraphql_EffectsAttribute. partial: standard reqwest/hyper/surf call forms; no datasource/federation client model. |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🟢 `partial` | `2026-06-03` | 3980 | `internal/links/module_cycle_pass.go` | #3980 wave1-structural: Tarjan SCC over IMPORTS detects cycles among async-graphql modules; rust mod/use IMPORTS emitted by the rust extractor. |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🟢 `partial` | `2026-06-03` | 3980 | `internal/links/pure_function_pass.go` | #3980 wave1-structural: language-agnostic pure-function pass tags async-graphql #[Object] resolver methods (async fn users) left un-stamped by the effect pass; same rust idiom proven in TestW1jr_DefUseRust_AsyncGraphqlResolver. |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | #3980 wave1-structural: reachability BFS reaches async-graphql #[Object] resolver methods (async fn users) through CALLS/IMPORTS edges from the rust extractor; entry points via entry_points_rust.go. |
| Request shape extraction | 🟢 `partial` | `2026-05-30` | 3508 | `internal/custom/rust/async_graphql.go`<br>`internal/custom/rust/graphql_grpc_test.go` | InputObject DTO type names recovered; per-field shape of the input struct not statically chased |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🟢 `partial` | `2026-05-30` | 3508 | `internal/custom/rust/async_graphql.go`<br>`internal/custom/rust/graphql_grpc_test.go` | Resolver return DTO type names recovered via SimpleObject derive; field-level shape not chased |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.graphql`](./protocol.graphql.md) hub record (GraphQL),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.async-graphql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
