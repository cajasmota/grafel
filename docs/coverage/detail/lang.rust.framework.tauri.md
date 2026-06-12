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
| IPC extraction | 🟢 `partial` | `2026-05-30` | — | `internal/custom/rust/tauri.go`<br>`internal/custom/rust/tauri_test.go`<br>`internal/custom/rust/testdata/tauri_app.rs` | Detects #[tauri::command] fn declarations and generate_handler![...] registrations |
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
| Config consumption | 🟢 `partial` | `2026-06-12` | — | `internal/extractor/config_key.go`<br>`internal/extractors/rust/config_consumer.go`<br>`internal/extractors/rust/config_consumer_test.go`<br>`internal/extractors/rust/rust.go` | #5020: literal env/config-crate key reads now emit the config-consumption topology — env::var("K")/std::env::var/env::var_os, dotenvy::var("K"), and figment Env::prefixed("P") become a shared SCOPE.Config/config_key node + a DEPENDS_ON_CONFIG edge from the reading function (receiver-qualified Foo.method for impl blocks), via emitConfigConsumerEdges -> extractor.EmitConfigReads (parity with go/java/php/python config-key blast radius). Honest-partial: only LITERAL string keys are recorded — dynamic env::var(name) yields no node/edge; keyless crate APIs envy::from_env::<T>() and config::Config::builder() (whole-struct deserialise / source merge with no single literal key) are deferred. Value-asserted: TestRustConfig_EnvVar/Dotenvy/FigmentPrefix/MethodHostName/DynamicKeySkipped. |
| Constant propagation | ✅ `full` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/rust.go` | — |
| DB effect | 🟢 `partial` | `2026-06-11` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_cross_orm_read_4692_test.go`<br>`internal/substrate/effect_sinks_rust.go` | #4737 (Rust slice of the #4692 cross-ORM receiver-typed read-reach audit): the ambiguous Diesel/sea-orm read terminals (.first/.find/.filter/.select/.all/.one + .order/.limit/.offset/.join) that collide with Rust Iterator combinators are now credited db_read ONLY on a query/table/Entity-typed receiver (Diesel schema::table root, .into_boxed()/QueryDsl chain, sea-orm Entity::find()) -- propagated across let q2 = q.filter(...) chains to a fixpoint and matched inline off a query root (users::table.filter(...).first(conn)). The distinctive terminals (sqlx::query!, .fetch_*, diesel::select/sql_query, .load/.get_result(s), .find_by_id/.stream/.paginate) stay bare on any receiver. vec.iter().filter(...).find(...) / slice.first() stay PURE (over-credit guard). Value-asserted in TestRustDieselSeaOrmTypedRead_4737 / TestRustIteratorNoFalsePositive_4737 / TestRustRepoReadChainSink_4737. |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_rust.go` | — |
| Env fallback recognition | ✅ `full` | — | — | `internal/substrate/rust.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/rust/exception_flow.go`<br>`internal/extractors/rust/exception_flow_test.go` | Err(Type::ctor())/Err(Type::Variant)/Err(Type(..)) + bail!/ensure!(Type::X) + .ok_or(Type::X)/.ok_or_else(||Type::X) -> THROWS (enum variant normalized to leading-segment ENUM type); match Err(Type)/if let Err(Type)/.map_err(|e: Type|) -> CATCHES; bare ? propagation, Box<dyn Error>, string panic!, Err(var)/Err(make()) re-raise dropped (honest-partial, #3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
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
