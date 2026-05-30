<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.framework.phoenix-liveview` — Phoenix LiveView

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 34

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
| Route extraction | 🟢 `partial` | — | — | `internal/custom/elixir/phoenix.go`<br>`internal/engine/phoenix_routes.go` | live/1 macro routes extracted as SCOPE.Operation/endpoint with route_type=live; scope context preserved |
| Router pattern | 🟢 `partial` | — | — | `internal/custom/elixir/phoenix.go` | Phoenix scope blocks extracted as SCOPE.Pattern/scope; pipeline declarations as SCOPE.Pattern; live_session not yet separately tracked |

### Build

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Static generation | — `not_applicable` | — | — | — | Phoenix LiveView is server-rendered; no static site generation. Dead-letter for this framework. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | — `not_applicable` | — | — | — | No enum keyword in Elixir; atom discriminants not statically enumerable as declared sets |
| Interface extraction | 🟢 `partial` | — | — | `internal/custom/elixir/typespec.go` | @callback + @behaviour attrs extracted; defprotocol → SCOPE.Component/interface |
| Type alias extraction | 🟢 `partial` | — | — | `internal/custom/elixir/typespec.go` | @type Name :: OtherType simple alias forms extracted |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🟢 `partial` | — | — | `internal/custom/elixir/phoenix.go`<br>`internal/substrate/effect_sinks_elixir.go` | socket.assigns updates tracked through handle_event/handle_params; Agent.update/GenServer.cast in mutation effect sniffer |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | — | — | `internal/substrate/entry_points_elixir.go` | ExUnit test/describe macros recognised as TestEntry entry-points; PhoenixLiveViewTest uses conn.dispatch flows not yet traced |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Dead code detection | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_elixir.go` | Elixir def-use sniffer registered; intra-procedural def-use chains over .ex/.exs |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; Elixir use/alias/import edges flow through extractor |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_elixir.go` | Elixir effect sniffer registered; functions with no elixir effect matches tagged pure=true; immutable semantics make Elixir especially suitable |
| Reachability analysis | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
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
