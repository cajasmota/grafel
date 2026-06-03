<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.php.framework.api-platform` — API Platform

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
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
| Endpoint synthesis | ✅ `full` | `2026-05-31` | — | `internal/custom/php/apiplatform.go`<br>`internal/custom/php/apiplatform_test.go` | — |
| Handler attribution | ✅ `full` | `2026-05-31` | — | `internal/custom/php/apiplatform.go`<br>`internal/custom/php/apiplatform_test.go` | — |
| Route extraction | ✅ `full` | `2026-05-31` | — | `internal/custom/php/apiplatform.go`<br>`internal/custom/php/apiplatform_test.go` | — |

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
| DTO extraction | ✅ `full` | `2026-05-31` | — | `internal/custom/php/apiplatform.go`<br>`internal/custom/php/apiplatform_test.go` | — |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

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
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/php/config_consumer.go`<br>`internal/extractors/php/config_consumer_test.go` | getenv/$_ENV + Laravel env()/config() -> config:<key> DEPENDS_ON_CONFIG edges (issue #3641) |
| Constant propagation | ✅ `full` | `2026-06-03` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/php.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_php.go` | Framework-agnostic reachability pass via php open-tag and public-function entry points; lifecycle hooks matched by name heuristic only — unified status across all PHP frameworks |
| Def use chain extraction | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_php.go` | PHP def-use sniffer registered in substrate; flows through language-agnostic def_use_pass |
| Env fallback recognition | ✅ `full` | `2026-06-03` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/php.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | — | `internal/extractor/exception_flow.go`<br>`internal/extractors/php/exception_flow.go`<br>`internal/extractors/php/exception_flow_test.go` | throw new X / throw new \Ns\X -> THROWS; catch (X $e) incl PHP8 union A|B -> CATCHES; broad \Throwable/\Exception recorded (typed); re-throw $e / dynamic dropped (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| HTTP effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-06-03` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/php.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/extractors/php/php.go`<br>`internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; PHP namespace_use_declaration edges emitted by tree-sitter extractor |
| Mutation effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_php.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_php.go` | Language-agnostic pure-function tagging pass reads effect stamps from PHP effect_sinks substrate |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_php.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_php.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_php.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_php.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |
| Taint source detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |
| Template pattern catalog | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/template_pattern_php.go` | PHP template-pattern sniffer registered; covers i18n trans(), log literals, SQL strings |
| Vulnerability finding | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_php.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.php.framework.api-platform ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
