<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.framework.api-platform-graphql` — API Platform GraphQL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint pagination posture | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint response codes | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-02` | — | `internal/custom/php/apiplatform_graphql.go`<br>`internal/custom/php/graphql_parity_test.go` | — |
| Handler attribution | ✅ `full` | `2026-06-02` | — | `internal/custom/php/apiplatform_graphql.go`<br>`internal/custom/php/graphql_parity_test.go` | — |
| Route extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/php/apiplatform_graphql.go`<br>`internal/custom/php/graphql_parity_test.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-06-02` | — | `internal/custom/php/apiplatform_graphql.go`<br>`internal/custom/php/graphql_parity_test.go` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/php/apiplatform_graphql.go`<br>`internal/custom/php/graphql_parity_test.go`<br>`internal/extractors/php/field_members.go`<br>`internal/extractors/php/issue4854_field_membership_test.go` |  #4854: the framework/ORM-gated custom emitters only emitted field members for HTTP/ORM-bound DTOs; the GENERAL primary-pass now emits a SCOPE.Schema/field entity + class->field CONTAINS for EVERY typed property and promoted constructor parameter (Name '<Class>.<prop>', '$' stripped, dedups by Name in MergeWithCustom), plus an EXTENDS edge to an in-file parent class (interfaces excluded), so any PHP data class projects field rows in the dashboard shape tree — closing the same gap #4845/#4851 fixed for JS/TS and #4850/#4855 for Go. emitPhpFieldMembers + attachPhpExtends in php/field_members.go; value-asserted by TestPhpTypedPropertiesAndPromotedParamsAreContained/TestPhpBaseClassEmitsExtends. |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type alias extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI injection point | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI scope resolution | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Metric extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Trace extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-06-03` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-04` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/php/config_consumer.go`<br>`internal/extractors/php/config_consumer_test.go` | framework-agnostic config-read pass; api-platform-graphql resolvers emit getenv/$_ENV + env()/config() -> config:<key> DEPENDS_ON_CONFIG edges identically (parity flip, epic #3872; TestPHPConfigConsumer_APIPlatformGraphQLResolver) |
| Constant propagation | ✅ `full` | `2026-06-03` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/php.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_php.go` | Framework-agnostic reachability pass via php open-tag and public-function entry points; lifecycle hooks matched by name heuristic only — unified status across all PHP frameworks |
| Def use chain extraction | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_php.go` | PHP def-use sniffer registered in substrate; flows through language-agnostic def_use_pass |
| Env fallback recognition | ✅ `full` | `2026-06-03` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/php.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | — | `internal/extractor/exception_flow.go`<br>`internal/extractors/php/exception_flow.go`<br>`internal/extractors/php/exception_flow_test.go` | throw new X / throw new \Ns\X -> THROWS; catch (X $e) incl PHP8 union A|B -> CATCHES; broad \Throwable/\Exception recorded (typed); re-throw $e / dynamic dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 4154 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go` | PHP flag-check call sites -> feature:<key> + GATED_BY edge (framework-agnostic engine pass, fires regardless of framework). Generic cross-language SDKs verified to fire on PHP: OpenFeature getBooleanValue('key',default), Unleash isEnabled('key'), LaunchDarkly variation('key',...). No first-party flag facade for this framework (Laravel Pennant / Symfony Flagception idioms are credited on their owning frameworks). Honest-partial: dynamic keys emit nothing. |
| Fs effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| HTTP effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-06-03` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/php.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/extractors/php/php.go`<br>`internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; PHP namespace_use_declaration edges emitted by tree-sitter extractor |
| Mutation effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_php.go` | Language-agnostic pure-function tagging pass reads effect stamps from PHP effect_sinks substrate |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_php.go` | — |
| Request shape extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/php/apiplatform_graphql.go`<br>`internal/custom/php/graphql_parity_test.go` | — |
| Request sink dataflow | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Response shape extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/php/apiplatform_graphql.go`<br>`internal/custom/php/graphql_parity_test.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_php.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |
| Taint source detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/template_pattern_php.go` | PHP template-pattern sniffer registered; covers i18n trans(), log literals, SQL strings |
| Vulnerability finding | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.graphql`](./protocol.graphql.md) hub record (GraphQL),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.php.framework.api-platform-graphql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
