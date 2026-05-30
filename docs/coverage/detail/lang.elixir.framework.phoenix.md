<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.framework.phoenix` — Phoenix

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 36

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/phoenix_routes.go`<br>`internal/engine/rules/elixir/frameworks/phoenix.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/phoenix_routes.go` | — |
| Route extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/elixir/phoenix.go`<br>`internal/engine/phoenix_routes.go`<br>`internal/engine/rules/elixir/frameworks/phoenix.yaml` | phoenixExtractor extracts HTTP verbs (get/post/put/patch/delete), resources CRUD expansion (8 routes), live routes; scope blocks captured as SCOPE.Pattern |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | — | — | `internal/engine/rules/elixir/frameworks/phoenix.yaml`<br>`internal/substrate/taint_sites_elixir.go` | Phoenix.Token.verify and Plug.Crypto.MessageVerifier tracked as sanitisers; conn.params sources tracked; Guardian-style pattern in taint sinks |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/elixir/ecto.go`<br>`internal/substrate/payload_shapes_elixir.go` | Ecto changesets (cast+validate_required) capture DTO field lists; payload shape sniffer collects cast allow-lists as request shapes |
| Request validation | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/elixir/ecto.go`<br>`internal/substrate/taint_sites_elixir.go` | Ecto.Changeset validate_required/validate_format/validate_length tracked as sanitisers in taint substrate; changeset extractor emits SCOPE.Operation |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | — | — | `internal/custom/elixir/phoenix.go`<br>`internal/substrate/taint_sites_elixir.go` | Phoenix pipeline/plug declarations extracted by phoenixExtractor; Plug.Conn flow tracked via taint substrate |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | — `not_applicable` | — | — | — | Elixir has no enum keyword; atoms serve as discriminants but are not declared types. Static atom-set extraction not implemented. |
| Interface extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/elixir/typespec.go` | @callback declarations in behaviour modules extracted; defprotocol extracted as SCOPE.Component/interface; @behaviour attrs mark implementing modules |
| Type alias extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/elixir/typespec.go` | @type Name :: OtherType simple alias forms extracted as SCOPE.Schema/type_alias |
| Type extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/elixir/typespec.go` | @type/@typep/@opaque declarations extracted as SCOPE.Schema/type entities; @spec annotations captured |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/entry_points_elixir.go` | ExUnit test/describe macros recognised as TestEntry entry-points; ConnCase/ChannelCase helpers in Phoenix not yet attributed to specific routes |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/template_pattern_elixir.go` | Logger.debug/info/warn/error with string literals captured as log_format template patterns via elixir template sniffer |
| Metric extraction | — `not_applicable` | — | — | — | Telemetry event calls are convention-based (:telemetry.execute/3); no dedicated extractor. Covered at framework level by Phoenix.Telemetry integration. |
| Trace extraction | — `not_applicable` | — | — | — | OpenTelemetry / :telemetry spans not statically extractable without runtime context. No dedicated elixir trace extractor. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_elixir.go` | Elixir def-use sniffer registered; intra-procedural def-use chains over .ex/.exs |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; Elixir use/alias/import edges flow through extractor |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_elixir.go` | Elixir effect sniffer registered; functions with no elixir effect matches tagged pure=true; immutable semantics make Elixir especially suitable |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_elixir.go` | Elixir template-pattern sniffer registered: i18n (gettext/dgettext), log_format (Logger.*), SQL literals via Ecto.Adapters.SQL |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.framework.phoenix ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
