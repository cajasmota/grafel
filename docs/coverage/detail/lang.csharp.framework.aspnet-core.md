<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.framework.aspnet-core` — ASP.NET Core

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 50

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-07-20` | — | `internal/engine/aspnet_core_routes.go`<br>`internal/engine/http_endpoint_csharp_abp.go`<br>`internal/engine/http_endpoint_csharp_abp_test.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/csharp/frameworks/asp_net_core.yaml` | ASP.NET Core attribute routing and YAML rule coverage remain full; ABP Framework conventional controllers are now synthesized from *AppService/ApplicationService/CrudAppService methods into canonical http_endpoint_definition entities. |
| Handler attribution | ✅ `full` | `2026-07-20` | — | `internal/engine/aspnet_core_routes.go`<br>`internal/engine/http_endpoint_csharp_abp.go`<br>`internal/engine/http_endpoint_csharp_abp_test.go` | ASP.NET endpoints carry source_handler attribution to SCOPE.Operation handlers; ABP conventional AppService endpoints attribute to <AppService>.<Method> so the shared resolver can bridge endpoint to method. |
| Route extraction | ✅ `full` | `2026-07-20` | — | `internal/custom/csharp/aspnet_core.go`<br>`internal/engine/aspnet_core_routes.go`<br>`internal/engine/http_endpoint_csharp_abp.go`<br>`internal/engine/http_endpoint_csharp_abp_test.go` | Minimal-API app.MapGet/Post/Put/Delete route path strings extracted via reAspNetMinimalAPI; attribute routes via reAspNetHTTPMethod; ABP conventional AppService routes synthesized under /api/app/<service> with method-name verb/action conventions. |
| Websocket route extraction | 🔴 `missing` | `2026-06-14` | — | — | #4965: dedicated websocket_route_extraction not yet implemented for this framework. The capability key was introduced for the rust axum/actix/warp WS extractor (internal/custom/rust/websocket_routes.go); this framework's WebSocket-upgrade idiom is not yet recognised and is a follow-up gap. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-06-11` | — | `internal/authposture/aspnet.go`<br>`internal/authposture/authposture_test.go`<br>`internal/authposture/resolvers.go`<br>`internal/custom/csharp/auth.go`<br>`internal/custom/csharp/auth_test.go` | [Authorize]/[AllowAnonymous]/RequireAuthorization/AddPolicy regex extractor; heuristic #4542 auth_posture_diff resolver (internal/authposture/aspnet.go): decodes ASP.NET Core auth posture into the shared {kind,literal} vocabulary with method ▸ class ▸ global precedence — [Authorize] -> authenticated; [Authorize(Roles="Admin")] -> role; [Authorize(Policy="CanEdit")] -> policy/role; [AllowAnonymous] -> public OVERRIDE (method [AllowAnonymous] wins over class [Authorize]); global UseAuthorization FallbackPolicy(RequireAuthenticatedUser) -> authenticated default when no method/class attribute covers the action. Reconciled props (incl aspnet_class_*/aspnet_fallback_policy) win with an attribute-source fallback. #4750 ENGINE STAMPING: applyAspnetCoreAuth (internal/engine/aspnet_core_auth.go) now stamps the method then class then global [Authorize]/[AllowAnonymous]/Roles/Policy posture (auth_required/auth_roles/auth_policy/allow_anonymous + aspnet_class_*/aspnet_fallback_policy + action_source) onto the synthesized ASP.NET endpoints, so these postures decode LIVE (structured) rather than only via the resolver source-scan. Live-path tests: TestStamp_Aspnet_MethodRoles/ClassAuthorize/MethodAllowAnonymousOverride. Fixture tests in authposture_test.go (protected/public-override/precedence/E2E-looser-vs-Django-oracle). |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/csharp/aspnet_request_response.go`<br>`internal/custom/csharp/dto_field_members.go`<br>`internal/custom/csharp/dto_field_members_test.go`<br>`internal/custom/csharp/extractors_test.go`<br>`internal/custom/csharp/validation.go`<br>`internal/custom/csharp/validation_test.go`<br>`internal/extractors/csharp/field_members.go`<br>`internal/extractors/csharp/issue4854_field_membership_test.go` | #4715: each DataAnnotations-bearing DTO/record class property is emitted as a SCOPE.Schema/field member (field_name, normalized field_type, parent_class, optional from nullable T? unless [Required]/required modifier, validators as @Attr markers from [Required]/[StringLength]/[Range]/[EmailAddress]/... + parseable Signature) with a CONTAINS edge to the class — the SAME uniform shape as the JS (#4635) and Python/Java (#4613) DTO field members. extractCsharpDTOFields + emitCsharpDTOFieldMembers in dto_field_members.go; value-asserted by TestCsharpDTO_FieldMembers (type/optional/validators/CONTAINS). Class/property detection is heuristic regex (partial where not a flagship fixture). #4854: the framework/endpoint-gated custom emitter (dto_field_members.go) only fired for HTTP-bound DTOs; the GENERAL primary-pass now emits a SCOPE.Schema/field entity + class->field CONTAINS for EVERY class/record/struct property, public field, and record positional parameter (Name '<Class>.<member>' dedups by Name with the custom members in MergeWithCustom), plus an EXTENDS edge to an in-file base class, so any C# data class projects field rows in the dashboard shape tree — closing the same gap #4845/#4851 fixed for JS/TS and #4850/#4855 for Go. emitFieldMembers + attachCsharpExtends in csharp/field_members.go; value-asserted by TestCsharpDataClassFieldsAreContained/TestCsharpRecordPositionalParamsAreContained/TestCsharpBaseClassEmitsExtends. |
| Request validation | ✅ `full` | — | — | `internal/custom/csharp/validation.go`<br>`internal/custom/csharp/validation_test.go` | FluentValidation AbstractValidator<T> + DataAnnotations [Required]/[StringLength]/[Range]/[RegularExpression] regex extractor; heuristic |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-28` | — | `internal/custom/csharp/middleware_extra.go`<br>`internal/custom/csharp/middleware_extra_test.go` | — |
| Rate limit stamping | 🟢 `partial` | — | [link](https://github.com/cajasmota/grafel/issues/4089) | `internal/custom/csharp/rate_limit_endpoint.go`<br>`internal/custom/csharp/rate_limit_endpoint_test.go` | #4089: .NET RateLimiter binding `.MapGet(...).RequireRateLimiting("p")` (minimal API) and `[EnableRateLimiting("p")]` (controller/action) stamp rate_limited/rate_limit_scope=route/rate_limit_name=p on a SCOPE.Pattern/rate_limit marker. When policy `p` is defined in-file via AddFixedWindow/SlidingWindow/TokenBucket/ConcurrencyLimiter, rate_limit_source resolves the limiter kind and rate="<PermitLimit|TokenLimit>/<TimeSpan window>s" (PermitLimit=100,Window=FromMinutes(1) -> "100/60s"); a concurrency limiter resolves the kind but has no window (rate honest-partial). AspNetCoreRateLimit app.UseIp/ClientRateLimiting() stamps an engine-scope marker (limits live in appsettings -> rate omitted). Negatives: [DisableRateLimiting] suppresses an adjacent binding; AllowAnonymous/plain endpoints are not stamped. Partial: a cross-file policy is rate_limited with rate + limiter-kind omitted (honest-partial). |

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
| Tests linkage | ✅ `full` | — | — | `internal/custom/csharp/integration_e2e.go`<br>`internal/custom/csharp/integration_e2e_test.go`<br>`internal/engine/http_endpoint_e2e_testmap_4685_test.go`<br>`internal/extractors/cross/testmap/extractor_test.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go`<br>`internal/extractors/csharp/csharp.go`<br>`internal/extractors/csharp/relationships_test.go` | C# NUnit/xUnit/MSTest: [Fact]/[Theory]/[Test]/[TestMethod] attrs detected via csharpTestRE. #4685 (epic #4615) — test→CALLS→endpoint coverage-linkage for C#/.NET: (a) receiver typing in internal/extractors/csharp/csharp.go — implicitly-typed `var c = new XController(svc)` infers XController from the object-creation initialiser (inferImplicitLocalType) and `var c = sp.GetRequiredService<XController>()` binds the DI generic type arg (diServiceTypeArg, WebApplicationFactory/IServiceProvider) so `c.GetCounts()` resolves to XController.GetCounts; target-typed `XController c = new(svc)` and ctor-injected fields were already typed; factory/method-call RHS (`var s = factory.Create()`) stays bare — honest exclusion (mirrors Java #4717 newExprClassName). xUnit [Fact]/[Theory] + NUnit [Test] are NAMED methods already mined for CALLS, so no synthetic test-scope owner is needed (unlike TS/JS #4680 / Ruby #4684). (b) Route-hit linkage: custom_csharp_integration_e2e (internal/custom/csharp/integration_e2e.go) emits one test_suite per C# test file carrying e2e_route_calls for WebApplicationFactory+HttpClient route-by-string hits (client.GetAsync/PostAsJsonAsync/PutAsync/.../new HttpRequestMessage(HttpMethod.X, "/path")); the shared engine.linkE2ERouteTestsToEndpoints pass (#4351) emits the TESTS edge to the matched http_endpoint_definition. Non-literal/interpolated routes dropped (conservative). ComputeCoverage credits via test→CALLS→handler→endpoint (no coverage-side change). Verified RED→GREEN end-to-end in http_endpoint_e2e_testmap_4685_test.go (before=0, after=2 endpoint TESTS edges). |

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
| Feature flag gating | ✅ `full` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY edge (Microsoft.FeatureManagement/LaunchDarkly/Unleash/OpenFeature/Split.io/generic). .NET-canonical idioms verified: Microsoft.FeatureManagement await _featureManager.IsEnabledAsync("key") + sync IsEnabled("key") + [FeatureGate("key")] attribute, LaunchDarkly PascalCase _ldClient.BoolVariation("key",...), OpenFeature client.GetBooleanValue("key",false) all fire & attribute to the enclosing controller action. Honest-partial: dynamic keys (IsEnabledAsync(flagName)) and non-FeatureManager .IsEnabled property emit nothing. |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-06-11` | 4704 | `internal/external/synth.go`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_csharp.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/grafel/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | — |
| Request sink dataflow | 🟢 `partial` | `2026-06-02` | 3960 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_csharp.go`<br>`internal/substrate/dataflow_csharp_test.go` | SCOPED request-input → sink DATA_FLOWS_TO (#3628 area #22, epic #3872, #3960): new C# sniffer (internal/substrate/dataflow_csharp.go) registered on the "csharp" slug and dispatched by .cs extension through LanguageForPath (internal/links/dataflow_pass.go), mirroring the java/python/jsts/go/ruby/php sniffers and COMPLETING the cross-language dataflow generalization (all 7 langs). Sources: ASP.NET Core / MVC action params [FromBody]/[FromQuery]/[FromForm]/[FromHeader]/[FromRoute] — each bound param is a request-derived root; field = attribute literal ([FromQuery(Name="q")]->q), else param name for scalar binders, else "" for [FromBody] whole-object (recovered from dto.Email property access); PLUS in-body Request.Query["x"]/Request.Form["x"]/Request.RouteValues["id"] accessors seeded as local roots with the key literal as field. Intra-method var/typed-decl + reassignment taint tracking + multi-hop (<=DataFlowMaxHops=3) local same-file call propagation by exact positional index, AND cross-file boundary emission continued by the links pass (continueDataFlowCSharp). Sinks: EF Core write (_context.Set.Add/AddRange/Update/Remove/Attach, SaveChanges/SaveChangesAsync, ExecuteSqlRaw), Dapper (connection.Execute/ExecuteAsync), response (Ok/Json/Content/Created/return <tainted>), outbound HTTP (httpClient.PostAsync/PutAsync/SendAsync/PostAsJsonAsync). HONEST-PARTIAL: drops static/constant values, non-request params (injected services), reassignment, embedded-arg expressions, params arrays, recursion/cycle, the 4th hop, external/unresolved imports; whole-object [FromBody] and dynamic member access flow with field="". DEPLOY-DEFERRED (daemon not rebuilt). |
| Response shape extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/grafel/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_csharp.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/grafel/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | — |
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
