<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.framework.nancyfx` — NancyFX

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
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
| Endpoint synthesis | ✅ `full` | `2026-06-03` | 3962 | `internal/engine/http_endpoint_csharp_minor.go`<br>`internal/engine/http_endpoint_csharp_minor_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3962: synthesizeNancy promotes both index-syntax Get["/path"] (Nancy 1.x) and call-syntax Get("/path") (Nancy 2.x) routes inside a : NancyModule subclass to the canonical http_endpoint_definition shape (http:<VERB>:<path>), dispatched in the csharp case. No longer regex-only SCOPE.Operation. |
| Handler attribution | ✅ `full` | `2026-06-03` | 3962 | `internal/engine/http_endpoint_csharp_minor.go`<br>`internal/engine/http_endpoint_csharp_minor_test.go` | #3962: each synthesized Nancy endpoint carries source_handler=SCOPE.Operation:<Module> (routes are inline lambdas assigned in the module ctor, so the module is the handler). Value-asserted in TestSynth_Nancy. |
| Route extraction | ✅ `full` | `2026-06-03` | 3962 | `internal/engine/http_endpoint_csharp_minor.go`<br>`internal/engine/http_endpoint_csharp_minor_test.go` | #3962: nancyIndexRouteRe + nancyCallRouteRe extract verb+path for both Nancy route-declaration styles; the module gate prevents call-syntax collision with FastEndpoints' bare Get(). Asserted on /widgets/{id} in TestSynth_Nancy. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | — | — | `internal/custom/csharp/auth.go` | [Authorize]/[AllowAnonymous]/RequireAuthorization/AddPolicy regex extractor; heuristic |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/validation.go` | DTO types inferred from FluentValidation + DataAnnotation model classes; heuristic |
| Request validation | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/validation.go` | FluentValidation AbstractValidator<T> + DataAnnotations regex extractor; heuristic |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🟢 `partial` | — | — | `internal/custom/csharp/middleware_extra.go`<br>`internal/custom/csharp/middleware_extra_test.go` | NancyFX DefaultNancyBootstrapper subclass, RequestStartup/ApplicationStartup overrides, and this.Before += / this.After += module pipeline hook registrations detected. |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/4089) | — | #4089: native rate-limit stamping not implemented for this framework. This framework runs its own request pipeline (not ASP.NET Core endpoint routing), so the .NET built-in RateLimiter (RequireRateLimiting/[EnableRateLimiting]) does not apply; its native throttling idiom is future work. AspNetCoreRateLimit can still front it at the host level but the limits are config-driven (honest-partial). |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | — | — | `internal/extractors/csharp/csharp.go` | tree-sitter CST enum_declaration → SCOPE.Schema/enum |
| Interface extraction | ✅ `full` | — | — | `internal/extractors/csharp/csharp.go` | tree-sitter CST interface_declaration → SCOPE.Component/interface |
| Type alias extraction | — `not_applicable` | — | — | — | C# has only file-scoped using-aliases, not first-class type aliases |
| Type extraction | ✅ `full` | — | — | `internal/extractors/csharp/csharp.go` | tree-sitter CST class/struct/record_declaration → SCOPE.Component; record added this PR |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/extractors/cross/testmap/frameworks.go` | C# NUnit/xUnit/MSTest: [Fact]/[Theory]/[Test]/[TestMethod] attrs detected via csharpTestRE |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/substrate/template_pattern_csharp.go` | template_pattern sniffer captures _logger.Log<Level>/Console.WriteLine; recording-win, no dedicated emitter |
| Metric extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/observability.go` | OTel Meter/CreateCounter/CreateHistogram regex extractor; heuristic |
| Trace extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/observability.go` | OTel ActivitySource/StartActivity/Activity.Current regex extractor; heuristic |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/csharp/config_consumer.go`<br>`internal/extractors/csharp/config_consumer_test.go` | IConfiguration indexer/GetValue/GetConnectionString + Environment.GetEnvironmentVariable -> config:<key> (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_csharp.go` | — |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_csharp.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/csharp/exception_flow.go`<br>`internal/extractors/csharp/exception_flow_test.go` | throw new X / throw new pkg.X -> THROWS; catch (X ex) / catch (pkg.X) -> CATCHES; bare catch + throw;/throw e re-throw dropped (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_csharp.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_csharp.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_csharp.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_csharp.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_csharp.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_csharp.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.framework.nancyfx ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
