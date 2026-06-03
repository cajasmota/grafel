<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.framework.aspnet-core` — ASP.NET Core

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
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/aspnet_core_routes.go`<br>`internal/engine/rules/csharp/frameworks/asp_net_core.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/aspnet_core_routes.go` | — |
| Route extraction | ✅ `full` | — | — | `internal/custom/csharp/aspnet_core.go`<br>`internal/engine/aspnet_core_routes.go` | Minimal-API app.MapGet/Post/Put/Delete route path strings extracted via reAspNetMinimalAPI; attribute routes via reAspNetHTTPMethod; route_path property set on each emitted entity. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | — | — | `internal/custom/csharp/auth.go`<br>`internal/custom/csharp/auth_test.go` | [Authorize]/[AllowAnonymous]/RequireAuthorization/AddPolicy regex extractor; heuristic |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/csharp/aspnet_request_response.go`<br>`internal/custom/csharp/extractors_test.go`<br>`internal/custom/csharp/validation.go`<br>`internal/custom/csharp/validation_test.go` | DTO types inferred from FluentValidation AbstractValidator<T> type arg and DataAnnotation model classes. aspnet_request_response.go ALSO emits traversable endpoint→DTO graph edges (#3629): an action SCOPE.Operation entity with ACCEPTS_INPUT → [FromBody] request DTO and RETURNS → ActionResult<T>/concrete response DTO (Class:<Name> structural ref; FromID=action entity). Previously these extractors emitted DTO entities only, so expand/traces/payload_drift could not follow endpoint→DTO; now they can, restoring parity with Java Spring. Tests: TestAspNetReqRespAcceptsInputAndReturnsEdges, TestAspNetReqRespEdgeHasFromID, TestAspNetReqRespPrimitiveParamNoEdge. |
| Request validation | ✅ `full` | — | — | `internal/custom/csharp/validation.go`<br>`internal/custom/csharp/validation_test.go` | FluentValidation AbstractValidator<T> + DataAnnotations [Required]/[StringLength]/[Range]/[RegularExpression] regex extractor; heuristic |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-28` | — | `internal/custom/csharp/middleware_extra.go`<br>`internal/custom/csharp/middleware_extra_test.go` | — |
| Rate limit stamping | 🟢 `partial` | — | [link](https://github.com/cajasmota/archigraph/issues/4089) | `internal/custom/csharp/rate_limit_endpoint.go`<br>`internal/custom/csharp/rate_limit_endpoint_test.go` | #4089: .NET RateLimiter binding `.MapGet(...).RequireRateLimiting("p")` (minimal API) and `[EnableRateLimiting("p")]` (controller/action) stamp rate_limited/rate_limit_scope=route/rate_limit_name=p on a SCOPE.Pattern/rate_limit marker. When policy `p` is defined in-file via AddFixedWindow/SlidingWindow/TokenBucket/ConcurrencyLimiter, rate_limit_source resolves the limiter kind and rate="<PermitLimit|TokenLimit>/<TimeSpan window>s" (PermitLimit=100,Window=FromMinutes(1) -> "100/60s"); a concurrency limiter resolves the kind but has no window (rate honest-partial). AspNetCoreRateLimit app.UseIp/ClientRateLimiting() stamps an engine-scope marker (limits live in appsettings -> rate omitted). Negatives: [DisableRateLimiting] suppresses an adjacent binding; AllowAnonymous/plain endpoints are not stamped. Partial: a cross-file policy is rate_limited with rate + limiter-kind omitted (honest-partial). |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-06-02` | — | `internal/extractor/enum_valueset.go`<br>`internal/extractors/csharp/csharp.go` | enum_declaration -> SCOPE.Schema/enum (members in Signature) AND a value-carrying SCOPE.Enum value-set node (buildEnumValueSet) recording explicit member values (Active=1); value-less for implicit-ordinal members (honest-partial). |
| Interface extraction | ✅ `full` | — | — | `internal/extractors/csharp/csharp.go` | tree-sitter CST interface_declaration → SCOPE.Component/interface; was already extracted, cell now confirmed |
| Type alias extraction | — `not_applicable` | — | — | — | C# has only file-scoped using-aliases, not first-class type aliases |
| Type extraction | ✅ `full` | — | — | `internal/extractors/csharp/csharp.go` | tree-sitter CST class/struct/record_declaration → SCOPE.Component; record_declaration added this PR |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/csharp/dotnet_di.go`<br>`internal/custom/csharp/dotnet_di_test.go` | #3699: ExtractDotnetDI (custom_csharp_dotnet_di) emits the DI binding GRAPH for Microsoft.Extensions.DependencyInjection: services.AddSingleton/AddScoped/AddTransient<IFoo,Foo>() (and Try/Keyed + typeof(IFoo),typeof(Foo) forms) emit IFoo BINDS Foo with lifetime=Singleton|Scoped|Transient; single-type-arg AddScoped<Foo>() emits a self-BINDS (binding_kind=self). Value-asserted in dotnet_di_test.go (IRepo BINDS Repo lifetime=Scoped; IClock/IMailer Singleton/Transient; typeof form; self-registration). |
| DI injection point | 🟢 `partial` | `2026-06-02` | — | `internal/custom/csharp/dotnet_di.go`<br>`internal/custom/csharp/dotnet_di_test.go` | #3699: constructor params of a class emit INJECTED_INTO (service type -> consumer class, via=dotnet_constructor); IConfiguration/IServiceProvider/IOptions<>/ILogger<> infrastructure types + primitives rejected. Value-asserted in dotnet_di_test.go (IOrderService INJECTED_INTO OrderController; negative: ILogger<> + string yield no edge). PARTIAL: the registered-as-DI gate is structural (any class ctor), and the impl/provider class resolves cross-file only via the resolver pass; factory-lambda registrations not linked. |
| DI scope resolution | ✅ `full` | `2026-06-02` | — | `internal/custom/csharp/dotnet_di.go`<br>`internal/custom/csharp/dotnet_di_test.go` | #3699: BINDS edges carry the service lifetime (Singleton/Scoped/Transient) parsed from the AddXxx registration verb. Value-asserted in dotnet_di_test.go. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | — | — | `internal/extractors/cross/testmap/extractor_test.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go` | C# NUnit/xUnit/MSTest: [Fact]/[Theory]/[Test]/[TestMethod] attrs detected via csharpTestRE |

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
| Dead code detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_csharp.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-06-02` | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/dataflow_csharp.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_csharp.go` | The C# def-use chain (def_use_csharp.go) is now consumed by the connected request→sink dataflow pass (internal/substrate/dataflow_csharp.go, #3960) to propagate taint across assignments and bounded local-call hops. Remains partial (dictionary-completeness backfill still open). |
| Env fallback recognition | ✅ `full` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/csharp/exception_flow.go`<br>`internal/extractors/csharp/exception_flow_test.go` | x |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_csharp.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | — |
| Request sink dataflow | 🟢 `partial` | `2026-06-02` | 3960 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_csharp.go`<br>`internal/substrate/dataflow_csharp_test.go` | SCOPED request-input → sink DATA_FLOWS_TO (#3628 area #22, epic #3872, #3960): new C# sniffer (internal/substrate/dataflow_csharp.go) registered on the "csharp" slug and dispatched by .cs extension through LanguageForPath (internal/links/dataflow_pass.go), mirroring the java/python/jsts/go/ruby/php sniffers and COMPLETING the cross-language dataflow generalization (all 7 langs). Sources: ASP.NET Core / MVC action params [FromBody]/[FromQuery]/[FromForm]/[FromHeader]/[FromRoute] — each bound param is a request-derived root; field = attribute literal ([FromQuery(Name="q")]->q), else param name for scalar binders, else "" for [FromBody] whole-object (recovered from dto.Email property access); PLUS in-body Request.Query["x"]/Request.Form["x"]/Request.RouteValues["id"] accessors seeded as local roots with the key literal as field. Intra-method var/typed-decl + reassignment taint tracking + multi-hop (<=DataFlowMaxHops=3) local same-file call propagation by exact positional index, AND cross-file boundary emission continued by the links pass (continueDataFlowCSharp). Sinks: EF Core write (_context.Set.Add/AddRange/Update/Remove/Attach, SaveChanges/SaveChangesAsync, ExecuteSqlRaw), Dapper (connection.Execute/ExecuteAsync), response (Ok/Json/Content/Created/return <tainted>), outbound HTTP (httpClient.PostAsync/PutAsync/SendAsync/PostAsJsonAsync). HONEST-PARTIAL: drops static/constant values, non-request params (injected services), reassignment, embedded-arg expressions, params arrays, recursion/cycle, the 4th hop, external/unresolved imports; whole-object [FromBody] and dynamic member access flow with field="". DEPLOY-DEFERRED (daemon not rebuilt). |
| Response shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_csharp.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-06-02` | — | `internal/links/taint_flow.go`<br>`internal/substrate/dataflow_csharp.go`<br>`internal/substrate/taint_sites_csharp.go` | Now connected to its originating request source by the dataflow pass (internal/substrate/dataflow_csharp.go, #3960): EF/Dapper/response/outbound-HTTP sinks are reached from a [From*]/Request.* source via source→sink stitching (DATA_FLOWS_TO). Remains partial (honest-partial; ambiguous positions and the 4th hop drop). |
| Taint source detection | 🟢 `partial` | `2026-06-02` | — | `internal/links/taint_flow.go`<br>`internal/substrate/dataflow_csharp.go`<br>`internal/substrate/taint_sites_csharp.go` | Now stitched end-to-end by the connected request→sink dataflow pass (internal/substrate/dataflow_csharp.go, #3960): the [From*]/Request.* source recognised here is followed through assignment + bounded multi-hop into a recognised sink, materialising DATA_FLOWS_TO with the resolved source field. Remains partial (honest-partial sniffer: dynamic/whole-object/embedded args drop or carry field=""). |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_csharp.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_csharp.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.framework.aspnet-core ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
