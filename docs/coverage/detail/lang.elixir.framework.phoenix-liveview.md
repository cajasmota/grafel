<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.framework.phoenix-liveview` — Phoenix LiveView

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 38

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | 🟢 `partial` | — | — | `internal/custom/elixir/phoenix.go` | phoenixExtractor recognises use Phoenix.LiveView and use Phoenix.LiveComponent; emits SCOPE.UIComponent/component per module |
| Hook recognition | 🟢 `partial` | — | — | `internal/custom/elixir/phoenix.go` | Mount/handle_event/handle_info/handle_params/render callbacks recognised as SCOPE.Operation/function by phoenixExtractor |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Data loaders | 🟢 `partial` | — | — | `internal/substrate/effect_sinks_elixir.go`<br>`internal/substrate/payload_shapes_elixir.go` | Ecto Repo.all/get/preload calls in mount/handle_params recognised as db_read effects; payload shape sniffer captures loaded fields |

### Server

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Hydration boundaries | — `not_applicable` | — | — | — | LiveView has no client/server hydration boundary concept distinct from its socket lifecycle; all rendering is server-side via render/1 |
| Server components | 🟢 `partial` | — | — | `internal/custom/elixir/phoenix.go` | use Phoenix.LiveComponent modules extracted as SCOPE.UIComponent/component with server-rendered semantics |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/elixir/phoenix.go`<br>`internal/engine/elixir_routes.go`<br>`internal/engine/elixir_routes_test.go`<br>`internal/engine/phoenix_routes.go` | synthesizePhoenixLive emits the initial-GET http_endpoint for each Phoenix LiveView 'live "/path", Module, :action' route, composing the active scope prefix and normalising :id->{id}; live-module :action attributed as handler (route_type=live). Value-asserting tests (TestPhoenixLive_Routes proves GET /dashboard + GET /users/{id} with handler attribution; TestPhoenixLive_NoAction covers the action-less form). |
| Router pattern | 🟢 `partial` | — | — | `internal/custom/elixir/phoenix.go` | Phoenix scope blocks extracted as SCOPE.Pattern/scope; pipeline declarations as SCOPE.Pattern; live_session not yet separately tracked |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 5499 | — | #5499: meta_framework Auth lane added. The JS/TS home-rolled body-check resolver (indexAuthBodyChecks) is JS/TS-only; Phoenix LiveView auth (Elixir on_mount / plug pipelines) is unaddressed here — honest missing. |

### Build

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Static generation | — `not_applicable` | — | — | — | Phoenix LiveView is server-rendered; no static site generation. Dead-letter for this framework. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🟢 `partial` | — | [link](https://github.com/cajasmota/grafel/issues/3471) | `internal/custom/elixir/typespec.go` | Elixir has no enum keyword. Literal atom-union typespecs (@type role :: :admin | :member | :guest) are the idiomatic enum analogue and ARE captured as SCOPE.Schema/enum with enum_members/member_count props, value-asserted (TestTypespecAtomUnionEnum). Partial (not full): only literal-union typespecs qualify; runtime atom sets / Ecto.Enum field options not statically resolved. |
| Interface extraction | 🟢 `partial` | — | [link](https://github.com/cajasmota/grafel/issues/3471) | `internal/custom/elixir/typespec.go` | @callback declarations + @behaviour attrs extracted; defprotocol -> SCOPE.Component/interface. Partial: callback arities and per-argument typespecs not parsed into structured signatures. |
| Type alias extraction | 🟢 `partial` | — | [link](https://github.com/cajasmota/grafel/issues/3471) | `internal/custom/elixir/typespec.go` | @type Name :: OtherType simple alias forms extracted as SCOPE.Schema/type_alias with alias_target. Partial: parametric/compound RHS (unions, maps, tuples) not decomposed. |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🟢 `partial` | — | — | `internal/custom/elixir/phoenix.go`<br>`internal/substrate/effect_sinks_elixir.go` | socket.assigns updates tracked through handle_event/handle_params; Agent.update/GenServer.cast in mutation effect sniffer |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-30` | — | `internal/extractors/cross/testmap/frameworks.go` | Deep testmap Elixir TESTS linkage: ExUnit (test "..." do leaves, describe groups via balanced do/end body walk) + StreamData (property "..." do) with subject-from-module-name (MyApp.UserServiceTest->UserService) + body call resolution (Foo.bar(...) promoted high); Elixir assertion stopwords (assert/refute/assert_raise/assert_received/assert_in_delta/catch_throw) + check-all generator DSL. Value-asserting tests in extractor_test.go assert specific test->target edges (UserService.register, Accounts.create_user, Serializer.encode, Guard.parse). Closes #3473. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_elixir.go` | Elixir def-use sniffer registered; intra-procedural def-use chains over .ex/.exs |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | — | `internal/extractor/exception_flow.go`<br>`internal/extractors/elixir/exception_flow.go`<br>`internal/extractors/elixir/exception_flow_test.go` | raise X / raise mod.X -> THROWS; rescue e in [A,B] / unbound typed rescue -> CATCHES; bare rescue + string raise + reraise + catch :throw + {:error,_} tuple dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 4149 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go` | Elixir flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic engine pass, fires regardless of framework). Verified to fire & attribute: FunWithFlags.enabled?(:key/"key", for: actor) with atom key normalized (leading : stripped, like Ruby symbols), Flippant.enabled?("key", actor), Unleash.enabled?("key") bare predicate, plus Unleash is_enabled? predicate. Honest-partial: dynamic keys (FunWithFlags.enabled?(flag)) + non-FF receivers (record.enabled?, SomeMod.enabled?(:x)) emit nothing; no Elixir enclosing-function index so edges are file-scope-anchored (File:<path>). |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; Elixir use/alias/import edges flow through extractor |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_elixir.go` | Elixir effect sniffer registered; functions with no elixir effect matches tagged pure=true; immutable semantics make Elixir especially suitable |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Request shape extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/payload_shapes_elixir.go` | Payload shape sniffer collects handle_event/handle_params parameter destructuring patterns as request shapes |
| Response shape extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/payload_shapes_elixir.go` | Ecto field declarations and json() response bodies captured as response shapes; LiveView assigns not typed statically |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Schema drift detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/payload_drift.go`<br>`internal/substrate/payload_shapes_elixir.go` | Payload drift pass compares producer/consumer shapes across LiveView event call sites |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_elixir.go` | Elixir template-pattern sniffer registered: i18n (gettext/dgettext), log_format (Logger.*), SQL literals via Ecto.Adapters.SQL |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.framework.phoenix-liveview ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
