<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.tauri` — Tauri (desktop)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Desktop
- **Capability cells:** 16

## Capabilities


### Process

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| IPC extraction | 🟢 `partial` | `2026-06-13` | — | `internal/custom/rust/tauri.go`<br>`internal/custom/rust/tauri_test.go`<br>`internal/custom/rust/testdata/tauri_app.rs` | #5023: detects #[tauri::command] fn declarations and generate_handler![...] registrations, now wired into IPC topology edges. generate_handler![a, commands::b] emits a CALLS edge from the SCOPE.Component(ipc_handler_registration) to each registered SCOPE.Operation(tauri:command:<name>) (path-qualified entries resolve to the final ident) — the in-binary half of the invoke() contract. emit/listen event channels become a shared SCOPE.Datastore(ipc_event) node keyed tauri:event:<name>: app.emit/emit_all/emit_to("evt") -> PUBLISHES_TO, app.listen/listen_global/once("evt") -> SUBSCRIBES_TO, so producer<->consumer join through one channel node (redis-pubsub modelling). Value-asserted: TestTauri_GenerateHandlerCallsCommands/EmitPublishes/ListenSubscribes/EmitListenSameChannelJoin + fixture edge assertions. Honest-partial: the TS-side invoke("cmd") caller is cross-language/cross-repo (deferred, follow-up #5023), and dynamic/non-literal event names yield no channel/edge. |
| Main renderer split | 🟢 `partial` | `2026-05-30` | — | `internal/custom/rust/tauri.go`<br>`internal/custom/rust/tauri_test.go` | Detects tauri::Builder::default() and fn main() in Tauri files as Rust backend entry points; WindowBuilder for renderer side |

### Native

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | 🟢 `partial` | `2026-05-30` | — | `internal/custom/rust/tauri.go`<br>`internal/custom/rust/tauri_test.go` | Detects tauri::api::* module usage and tauri_plugin_* crate imports |

### Updates

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🟢 `partial` | `2026-06-12` | config_consumption:#5079-keyless-envy-figment-extract-deferred | `internal/extractor/config_key.go`<br>`internal/extractors/rust/config_consumer.go`<br>`internal/extractors/rust/config_consumer_test.go`<br>`internal/extractors/rust/rust.go` | #5020+#5079: literal env/config-crate key reads emit the config-consumption topology — env::var(K)/std::env::var/env::var_os, dotenvy::var(K), figment Env::prefixed(P), and (#5079) the config crate typed getters cfg.get_string/get_int/get_bool/get_float(K) + turbofish cfg.get::<T>(K) — each becomes a shared SCOPE.Config/config_key node + a DEPENDS_ON_CONFIG edge (pattern=config_crate) from the reading function (receiver-qualified Foo.method), via emitConfigConsumerEdges -> extractor.EmitConfigReads. Honest-partial: only LITERAL string keys recorded — dynamic env::var(name) and bare HashMap .get(k) yield nothing; the truly KEYLESS crate APIs envy::from_env::<T>() and Figment::new().merge(...).extract::<T>() (whole-struct deserialise, no single literal key) remain deferred (#5079 follow-up). Value-asserted: TestRustConfig_EnvVar/Dotenvy/FigmentPrefix/MethodHostName/ConfigCrateGetters/ConfigCrateTurbofish/BareGetNotConfig/DynamicKeySkipped. |
| Constant propagation | ✅ `full` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go` | — |
| DB effect | 🟢 `partial` | `2026-06-11` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_cross_orm_read_4692_test.go`<br>`internal/substrate/effect_sinks_rust.go` | #4737 (Rust slice of the #4692 cross-ORM receiver-typed read-reach audit): the ambiguous Diesel/sea-orm read terminals (.first/.find/.filter/.select/.all/.one + .order/.limit/.offset/.join) that collide with Rust Iterator combinators are now credited db_read ONLY on a query/table/Entity-typed receiver (Diesel schema::table root, .into_boxed()/QueryDsl chain, sea-orm Entity::find()) -- propagated across let q2 = q.filter(...) chains to a fixpoint and matched inline off a query root (users::table.filter(...).first(conn)). The distinctive terminals (sqlx::query!, .fetch_*, diesel::select/sql_query, .load/.get_result(s), .find_by_id/.stream/.paginate) stay bare on any receiver. vec.iter().filter(...).find(...) / slice.first() stay PURE (over-credit guard). Value-asserted in TestRustDieselSeaOrmTypedRead_4737 / TestRustIteratorNoFalsePositive_4737 / TestRustRepoReadChainSink_4737. |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | — |
| Env fallback recognition | ✅ `full` | — | — | `internal/substrate/rust.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/rust/exception_flow.go`<br>`internal/extractors/rust/exception_flow_test.go` | Err(Type::ctor())/Err(Type::Variant)/Err(Type(..)) + bail!/ensure!(Type::X) + .ok_or(Type::X)/.ok_or_else(||Type::X) -> THROWS (enum variant normalized to leading-segment ENUM type); match Err(Type)/if let Err(Type)/.map_err(|e: Type|) -> CATCHES; bare ? propagation, Box<dyn Error>, string panic!, Err(var)/Err(make()) re-raise dropped (honest-partial, #3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-12` | feature_flag_gating:#5079-cfg-combinator-keys-and-attribute-attribution-deferred | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go` | #5079: Rust conditional-compilation feature gating — cfg!(feature=x) macro + #[cfg(feature=x)] / #[cfg_attr(feature=x,...)] attributes — emits a SCOPE.FeatureFlag entity (feature:<key>, subtype rust-cfg) + a GATED_BY edge from the enclosing function, via a lang-gated matcher in applyFeatureFlagEdges (distinct from the runtime flag-SDK model). Honest-partial: a cfg! macro in a function body attributes to that function; a #[cfg(...)] attribute precedes its item and attributes to prior-function/file scope (same caveat as .NET [FeatureGate]); a multi-feature combinator all(...)/any(...) captures only the FIRST feature key. Value-asserted: TestFeatureFlag_Rust_cfg_macro/cfg_attribute/cfg_combinator_firstKey/cfg_langGated_noFabrication. |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| Import resolution quality | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/rust.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_rust.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.tauri ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
