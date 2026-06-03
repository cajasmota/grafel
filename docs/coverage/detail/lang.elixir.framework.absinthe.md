<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.framework.absinthe` — Absinthe (GraphQL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
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
| Endpoint synthesis | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/elixir/frameworks/absinthe.yaml`<br>`internal/engine/rules/graphql/frameworks/absinthe_elixir.yaml` | — |
| Handler attribution | 🟢 `partial` | `2026-05-28` | — | `internal/engine/rules/elixir/frameworks/absinthe.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/engine/elixir_routes.go`<br>`internal/engine/elixir_routes_test.go` | synthesizeAbsinthe maps each top-level 'field :name' under an Absinthe query/mutation/subscription block to http:GRAPHQL:/graphql/<Root>/<field> (Strawberry/Apollo convention, #3066), tracking do/end depth so nested object-type fields are excluded. Value-asserting test (TestAbsinthe_Schema proves Query/users, Query/user, Mutation/create_user and excludes nested object fields). |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | — | — | `internal/substrate/taint_sites_elixir.go` | Absinthe middleware/context-based auth tracked via conn.params taint sources; Phoenix.Token.verify recognised |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/payload_shapes_elixir.go` | GraphQL resolver params captured via params map access patterns in payload sniffer |
| Request validation | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/taint_sites_elixir.go` | Ecto.Changeset validate_* patterns recognised; Absinthe has built-in type coercion not statically tracked |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | — | — | `internal/engine/rules/elixir/frameworks/absinthe.yaml`<br>`internal/substrate/taint_sites_elixir.go` | Absinthe.Middleware.* module uses detected via engine YAML; plug pipeline middleware tracked via taint substrate |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | 🟢 `partial` | — | 3804 | `internal/custom/elixir/absinthe_typegraph.go`<br>`internal/custom/elixir/absinthe_typegraph_test.go` | #4028: absintheTypeGraphExtractor builds the Absinthe code-first object-type→type graph — each `object :name` (plus interface/union as valid targets) emits a SCOPE.Schema/type node, and each object-typed `field :f, <type>` emits a GRAPH_RELATES edge with the cross-language cardinality contract (field_name/list/nullable/item_nullable/cardinality/self_ref/graphql_field). Parses non_null(:t)/list_of(:t)/list_of(non_null(:t))/non_null(list_of(:t)) wrappers; self-ref handled. Value-asserted (TestAbsintheTypeGraph_*: user.orders->order to_many list, user.account->account to_one, user.manager->user self_ref; scalar/enum/input_object fields emit NO edge). Partial (not full): same-file heuristic only — cross-file multi-module schemas split via `import_types` are not chased, and input_object/enum relations are excluded from the output object graph by design. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🟢 `partial` | — | [link](https://github.com/cajasmota/archigraph/issues/3471) | `internal/custom/elixir/typespec.go` | Elixir has no enum keyword. Literal atom-union typespecs (@type role :: :admin | :member | :guest) are the idiomatic enum analogue and ARE captured as SCOPE.Schema/enum with enum_members/member_count props, value-asserted (TestTypespecAtomUnionEnum). Partial (not full): only literal-union typespecs qualify; runtime atom sets / Ecto.Enum field options not statically resolved. |
| Interface extraction | 🟢 `partial` | — | [link](https://github.com/cajasmota/archigraph/issues/3471) | `internal/custom/elixir/typespec.go` | @callback declarations + @behaviour attrs extracted; defprotocol -> SCOPE.Component/interface. Partial: callback arities and per-argument typespecs not parsed into structured signatures. |
| Type alias extraction | 🟢 `partial` | — | [link](https://github.com/cajasmota/archigraph/issues/3471) | `internal/custom/elixir/typespec.go` | @type Name :: OtherType simple alias forms extracted as SCOPE.Schema/type_alias with alias_target. Partial: parametric/compound RHS (unions, maps, tuples) not decomposed. |
| Type extraction | ✅ `full` | — | — | `internal/custom/elixir/typespec.go` | @type/@typep/@opaque declarations + defstruct fields (with @enforce_keys required-key subset) extracted as SCOPE.Schema/struct carrying literal struct_fields/field_count props; @spec annotations captured. defstruct field sets are fully static and value-asserted (TestTypespecDefStructFields). |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-30` | — | `internal/extractors/cross/testmap/frameworks.go` | Deep testmap Elixir TESTS linkage: ExUnit (test "..." do leaves, describe groups via balanced do/end body walk) + StreamData (property "..." do) with subject-from-module-name (MyApp.UserServiceTest->UserService) + body call resolution (Foo.bar(...) promoted high); Elixir assertion stopwords (assert/refute/assert_raise/assert_received/assert_in_delta/catch_throw) + check-all generator DSL. Value-asserting tests in extractor_test.go assert specific test->target edges (UserService.register, Accounts.create_user, Serializer.encode, Guard.parse). Closes #3473. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/elixir/observability.go`<br>`internal/custom/elixir/observability_test.go` | Logger.debug/info/warning/error(...) statements captured by dedicated observabilityExtractor as SCOPE.Pattern/log_statement with log_level + leading string-literal message; Logger.metadata(...) captured. PARTIAL: logger require/import and message binding not correlated cross-file; interpolated/concatenated message tails not resolved. |
| Metric extraction | 🟢 `partial` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3474) | `internal/custom/elixir/observability.go`<br>`internal/custom/elixir/observability_test.go` | :telemetry.execute([:a,:b],...) event names and Telemetry.Metrics counter/summary/last_value/distribution/sum("name") captured by observabilityExtractor as SCOPE.Pattern/metric (metric_name + telemetry_event) when literal at call site. Value-asserting tests prove exact names. PARTIAL: metric/event name -> :telemetry.attach handler -> reporter/exporter wiring spans multiple files and is not resolved. |
| Trace extraction | 🟢 `partial` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3474) | `internal/custom/elixir/observability.go`<br>`internal/custom/elixir/observability_test.go` | :telemetry.span([:a,:b],...) event-prefix captured by observabilityExtractor as SCOPE.Pattern/trace_span (span_name + telemetry_event) when literal at call site. Value-asserting test proves exact name. PARTIAL: idiomatic Elixir has no static OTel span/exporter binding; spans are bridged from :telemetry events by a handler attached at runtime (cross-file). |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

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
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; Elixir use/alias/import edges flow through extractor |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_elixir.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_elixir.go` | Elixir effect sniffer registered; functions with no elixir effect matches tagged pure=true; immutable semantics make Elixir especially suitable |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_elixir.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_elixir.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_elixir.go` | Elixir template-pattern sniffer registered: i18n (gettext/dgettext), log_format (Logger.*), SQL literals via Ecto.Adapters.SQL |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_elixir.go` | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.graphql`](./protocol.graphql.md) hub record (GraphQL),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.framework.absinthe ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
