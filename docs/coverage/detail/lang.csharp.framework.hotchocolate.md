<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.framework.hotchocolate` — HotChocolate (GraphQL)

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
| Endpoint synthesis | ✅ `full` | `2026-06-02` | 3617 | `internal/engine/http_endpoint_hotchocolate.go`<br>`internal/engine/http_endpoint_hotchocolate_test.go` | synthesizeHotChocolate emits http:GRAPHQL:/graphql/<Query|Mutation|Subscription>/<field> per public resolver method — EXACT canonical shape as gqlgen (Go) / Strawberry (Python) / Apollo (JS) / Absinthe so client links (#3667) + cross-repo linker join. Field name = HotChocolate default: leading Get stripped + first letter lower-cased (GetUser->user, GetUsersByTeam->usersByTeam, CreateUser->createUser). Recognises [QueryType]/[MutationType]/[SubscriptionType] markers, [ExtendObjectType(typeof(Query))] extensions, and same-file fluent .AddQueryType<T>() registrations. Value-asserting tests assert the EXACT endpoint ids. |
| Handler attribution | ✅ `full` | `2026-06-02` | 3617 | `internal/engine/http_endpoint_hotchocolate.go`<br>`internal/engine/http_endpoint_hotchocolate_test.go` | Each endpoint carries source_handler=SCOPE.Operation:<Class>.<Method> (same naming as the C# extractor buildOperation), which ResolveHTTPEndpointHandlers rebinds to the resolver method — HANDLES edge endpoint->method. Value-asserting tests assert source_handler=SCOPE.Operation:Query.GetUser / Mutation.CreateUser / UserQueries.GetUserByEmail. |
| Route extraction | 🟢 `partial` | `2026-06-02` | 3617 | `internal/engine/http_endpoint_hotchocolate.go`<br>`internal/engine/http_endpoint_hotchocolate_test.go` | Root-type registration recovered from marker attributes, [ExtendObjectType] extensions, or SAME-FILE fluent .AddQueryType<T>(). PARTIAL (honest): a resolver class registered fluently in a DIFFERENT file than its declaration, descriptor.Field() runtime fluent field definitions, and SDL schema-first .graphqls binding are not resolved. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-06-02` | 3961 | `internal/custom/csharp/auth.go`<br>`internal/engine/http_endpoint_hotchocolate_auth.go`<br>`internal/engine/http_endpoint_hotchocolate_auth_test.go`<br>`internal/engine/http_endpoint_jsts_auth.go`<br>`internal/mcp/auth_coverage.go` | #3961: applyHotChocolateAuthShapes stamps [Authorize]/[Authorize(Roles=...)]/[Authorize(Policy=...)]/[AllowAnonymous] from each HotChocolate resolver method (and inherited type-level [Authorize]) onto its http:GRAPHQL: endpoint via the shared stampAuthPolicy contract — auth_required + auth_roles + auth_permissions + auth_policy, plus signal-1 auth_decorator so archigraph_auth_coverage fires. [AllowAnonymous] overrides class-level Authorize to explicit-public. Value-asserting tests assert the verdict on the specific resolver endpoint + negative (undecorated/no-signal) cases. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Rate limit stamping | 🟢 `partial` | — | [link](https://github.com/cajasmota/archigraph/issues/4089) | `internal/custom/csharp/rate_limit_endpoint.go`<br>`internal/custom/csharp/rate_limit_endpoint_test.go` | #4089: .NET RateLimiter binding `.MapGet(...).RequireRateLimiting("p")` (minimal API) and `[EnableRateLimiting("p")]` (controller/action) stamp rate_limited/rate_limit_scope=route/rate_limit_name=p on a SCOPE.Pattern/rate_limit marker. When policy `p` is defined in-file via AddFixedWindow/SlidingWindow/TokenBucket/ConcurrencyLimiter, rate_limit_source resolves the limiter kind and rate="<PermitLimit|TokenLimit>/<TimeSpan window>s" (PermitLimit=100,Window=FromMinutes(1) -> "100/60s"); a concurrency limiter resolves the kind but has no window (rate honest-partial). AspNetCoreRateLimit app.UseIp/ClientRateLimiting() stamps an engine-scope marker (limits live in appsettings -> rate omitted). Negatives: [DisableRateLimiting] suppresses an adjacent binding; AllowAnonymous/plain endpoints are not stamped. Partial: a cross-file policy is rate_limited with rate + limiter-kind omitted (honest-partial). |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | 🔴 `missing` | — | 3804 | — | GraphQL object-type→type graph applies (this is a GraphQL server) but is not yet implemented for this framework/language; SDL servers are covered by internal/extractors/graphql/type_graph.go (#3805) and the TS/Python code-first set (TypeGraphQL/Nexus/Pothos/Strawberry/graphene) by the code-first type-graph extractors. This lane is the remaining backfill for other-language GraphQL frameworks. |

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
| DB effect | 🟢 `partial` | `2026-06-11` | 4218 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_cross_orm_read_4692_test.go`<br>`internal/substrate/effect_sinks_csharp.go`<br>`internal/substrate/graphql_client_effects_4218_test.go` | #4218 (verify-first): per-LANGUAGE effect_sinks_csharp.go EF detectors fire on HotChocolate resolver bodies and attribute to the exact resolver method. db_read via csharpDBReadRe (FirstOrDefaultAsync) on GetAccount; db_write via csharpDBWriteRe (AddAsync/SaveChangesAsync) on CreateOrder. Proven by TestSubstrate_CSharp_HotChocolate_EffectsAttribute. partial: EF/Dapper call-shape detection (conf 0.85). #4692 read-reach: ambiguous LINQ terminals (.Where/.First/.FirstOrDefault/.Single/.ToList/.ToArray/.Any/.All/.Count/.Find) now credited db_read on a DbSet/IQueryable-typed receiver (csharpDBSetReadMatches) so layered-repo reads (_context.Users.Where(...).FirstOrDefault()) reach; in-memory LINQ on a plain List stays pure. Distinctive async/EF-only verbs stay bare. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-06-04` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/csharp/config_consumer.go`<br>`internal/extractors/csharp/config_consumer_test.go` | IConfiguration indexer/GetValue/GetConnectionString + Environment.GetEnvironmentVariable -> config:<key> (issue #3641) |
| Constant propagation | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_sibling_sweep_test.go` | #3872: the per-LANGUAGE sniffCSharp sniffer (Register("csharp")) gates only on file content with zero per-framework branching, so HotChocolate .cs files dispatch the SAME const/literal sniffer as flagship siblings. Value-asserting test drives the HotChocolate idiom and asserts the EXACT literal value + ProvenanceLiteral + Confidence 1.0. |
| Dead code detection | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/3872) | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_csharp.go` | #3872: language-level dead-code detection. sniffCSharpEntryPoints (Register("csharp")) seeds the language-agnostic reachability BFS from HotChocolate-host entry-points; whatever the BFS does not reach is a dead-code candidate. Proven by TestEntryPoints_HotChocolate_w1crp: the Program.cs `static async Task Main(` surfaces as cli_main AND the public resolver-builder method `BuildSchema` surfaces as library_export — exactly the seeds the dead-code pass needs. Partial: GraphQL resolvers reached via runtime SchemaBuilder registration (not a static CALLS edge) can be flagged unreached without the framework-registration model. |
| Def use chain extraction | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/3872) | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_csharp.go` | #3872: the per-LANGUAGE sniffDefUseCSharp sniffer (RegisterDefUseSniffer("csharp")) is framework-blind and fires on a HotChocolate resolver body — a plain `public Book GetBook(...)` method — flowing through the language-agnostic def_use_pass. Proven by TestDefUse_HotChocolate_resolver_w1crp: asserts the EXACT defs `book` and `title` AND uses `book`,`title` all attributed to the resolver method `GetBook`. Partial: regex-scoped to the nearest preceding method header (mirrors all csharp siblings). |
| Env fallback recognition | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_sibling_sweep_test.go` | #3872: the framework-blind csharp substrate sniffer recognises the env-fallback idiom regardless of framework; HotChocolate dispatches it identically. Test asserts the EXACT env-var name + default literal + ProvenanceEnvFallback + Confidence 0.85 on the HotChocolate idiom. |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/csharp/exception_flow.go`<br>`internal/extractors/csharp/exception_flow_test.go` | throw new X / throw new pkg.X -> THROWS; catch (X ex) / catch (pkg.X) -> CATCHES; bare catch + throw;/throw e re-throw dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic C# engine pass, fires regardless of framework). .NET idioms attribute to the enclosing method: Microsoft.FeatureManagement _featureManager.IsEnabledAsync/IsEnabled("key") + [FeatureGate("key")] attribute, LaunchDarkly PascalCase BoolVariation/Variation, Unleash IsEnabled, OpenFeature GetBooleanValue. Honest-partial: dynamic keys + non-FeatureManager .IsEnabled miss (no literal / wrong receiver). |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🟢 `partial` | `2026-06-03` | 4218 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go`<br>`internal/substrate/graphql_client_effects_4218_test.go` | #4218 (verify-first): per-LANGUAGE effect_sinks_csharp.go http_out detector (csharpHTTPRe _httpClient.PostAsync) fires on a HotChocolate resolver driving an outbound call, attributed to CreateOrder. Proven by TestSubstrate_CSharp_HotChocolate_EffectsAttribute. partial: HttpClient/WebClient/RestSharp call forms; no HotChocolate DataLoader/HttpClientFactory model. |
| Import resolution quality | 🟢 `partial` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_sibling_sweep_test.go` | #3872: the csharp cross-file import sniffer is framework-blind; HotChocolate dispatches it identically. PARTIAL (mirrors all siblings): single-segment binding, no transitive/re-export graph. Test asserts the EXACT ImportSource + ProvenanceCrossFile + Confidence 0.6 on the HotChocolate idiom. |
| Module cycle detection | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/3872) | `internal/extractors/csharp/csharp.go`<br>`internal/links/module_cycle_pass.go` | #3872: language-agnostic Tarjan SCC over IMPORTS edges. The C# extractor emits an IMPORTS edge per `using` directive (buildImport, csharp.go), and a HotChocolate codebase spans ≥2 .cs modules (schema types, resolvers, repositories) cross-referencing via `using` — so the SCC pass genuinely applies to this idiom. Partial: zero per-framework code; cycle membership is whatever the IMPORTS graph yields. |
| Mutation effect | 🟢 `partial` | `2026-06-03` | 4218 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go`<br>`internal/substrate/graphql_client_effects_4218_test.go` | #4218 (verify-first): per-LANGUAGE effect_sinks_csharp.go mutation detector (csharpMutationRe this.<Member> = ...) fires on a HotChocolate resolver body and attributes to CreateOrder (this.LastAmount = amount). Proven by TestSubstrate_CSharp_HotChocolate_EffectsAttribute. partial: this.field writes only (conf 0.7). |
| Pure function tagging | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/3872) | `internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_csharp.go` | #3872: language-agnostic pure-function tagging reads the effect stamps produced by the per-LANGUAGE csharp effect substrate. Any HotChocolate resolver method the effect pass left un-stamped is tagged a pure-function candidate (pure=true, pure_confidence=0.30) — zero per-framework code. Partial: a candidacy tag, not a proof of purity (mirrors all csharp siblings). |
| Reachability analysis | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/3872) | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_csharp.go` | #3872: language-level reachability. The language-agnostic BFS seeds from sniffCSharpEntryPoints; a HotChocolate host exposes a real reachable call structure — `Main` (cli_main) calls `BuildSchema` (library_export). Proven by TestEntryPoints_HotChocolate_w1crp asserting BOTH seed kinds surface on the host idiom. Partial: framework runtime-dispatch edges (SchemaBuilder→resolver) are not static, so reachable-via provenance is conservative. |
| Request shape extraction | 🟢 `partial` | `2026-06-02` | 3961 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go`<br>`internal/substrate/payload_shapes_hotchocolate_test.go` | #3961: sniffHotChocolateResolverShapes surfaces each HotChocolate resolver method's typed argument list as a producer REQUEST shape (DTO-typed args expanded to the DTO's fields; scalar args one field each; ambient framework params like CancellationToken/IResolverContext skipped) and its typed return as a producer RESPONSE shape (Task<>/IEnumerable<>/List<>/nullable wrappers unwrapped to the leaf DTO), flowing through the standard payload-drift join. Gated on a HotChocolate file-signal. Partial: DTO-field + scalar-arg expansion only — no inline-literal resolver-body resolution or cross-file DTO import. |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🟢 `partial` | `2026-06-02` | 3961 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go`<br>`internal/substrate/payload_shapes_hotchocolate_test.go` | #3961: sniffHotChocolateResolverShapes surfaces each HotChocolate resolver method's typed argument list as a producer REQUEST shape (DTO-typed args expanded to the DTO's fields; scalar args one field each; ambient framework params like CancellationToken/IResolverContext skipped) and its typed return as a producer RESPONSE shape (Task<>/IEnumerable<>/List<>/nullable wrappers unwrapped to the leaf DTO), flowing through the standard payload-drift join. Gated on a HotChocolate file-signal. Partial: DTO-field + scalar-arg expansion only — no inline-literal resolver-body resolution or cross-file DTO import. |
| Sanitizer recognition | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/3872) | `internal/links/taint_flow.go`<br>`internal/substrate/taint_other_sweep_test.go`<br>`internal/substrate/taint_sites_csharp.go` | #3872 (verify-first, vuln-finding sibling sweep): the per-LANGUAGE taint_sites_csharp.go sanitizer detectors are framework-blind and fire on a HotChocolate resolver body — HtmlEncoder.Default.Encode as an XSS sanitizer (csSanitizerHTMLRe) and SqlCommand.Parameters.AddWithValue as a parameterised-SQL sanitizer (csSanitizerSQLRe) — both attributing to the resolver method `GetBookByTitle` (scanCSharpFuncHeaders accepts the plain `public Book GetBookByTitle(...)` header). Proven by TestSubstrate_CSharp_HotChocolate_SanitizerFires (asserts sanitizer/xss AND sanitizer/sql_injection both attributed to `GetBookByTitle`). partial: sanitizer primitives detected per-LANGUAGE regardless of framework; the HotChocolate request-input (GraphQL-typed resolver args) source is not seeded, so a full source→sink flow is not modelled (see vulnerability_finding, honest-missing). |
| Schema drift detection | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | #3872: the framework-agnostic payload-drift pass dispatches sniffPayloadShapesCSharp by LANGUAGE slug (LanguageForPath->PayloadShapeSnifferFor), so HotChocolate producer/consumer shapes feed the same drift join as siblings. PARTIAL (mirrors siblings): no HotChocolate-specific drift fixture asserted end-to-end yet. |
| Taint sink detection | 🟢 `partial` | — | [link](https://github.com/cajasmota/archigraph/issues/3872) | `internal/links/taint_flow.go`<br>`internal/substrate/taint_other_sweep_test.go`<br>`internal/substrate/taint_sites_csharp.go` | #3872 (verify-first, vuln-finding sibling sweep): the per-LANGUAGE taint_sites_csharp.go SQL-injection sink (csSinkSQLRe: SqlCommand.CommandText assigned an interpolated $"... {arg} ..." string) fires on a HotChocolate resolver body and attributes to the resolver method `GetBookByTitle`. Proven by TestSubstrate_CSharp_HotChocolate_TaintSinkFires (CommandText = $"... '{title}' ..." flagged sql_injection on GetBookByTitle). partial: the SQL sink anchors on the CommandText-interpolation / +-concat / FromSqlRaw($) shapes, so not all sink forms are covered; security-relevant sink primitives are detected per-LANGUAGE regardless of framework. |
| Taint source detection | 🟢 `partial` | — | [link](https://github.com/cajasmota/archigraph/issues/3872) | `internal/links/taint_flow.go`<br>`internal/substrate/taint_other_sweep_test.go`<br>`internal/substrate/taint_sites_csharp.go` | #3872 (verify-first, vuln-finding sibling sweep): the per-LANGUAGE taint_sites_csharp.go source detectors are framework-blind; the IConfiguration["..."] / Environment.GetEnvironmentVariable env-config source (csSourceEnvRe) fires on a HotChocolate resolver body and attributes to the resolver method `GetBookByTitle`. Proven by TestSubstrate_CSharp_HotChocolate_TaintSourceFires (asserts source/generic on GetBookByTitle). partial: env/config + deserialization + ASP.NET request-input sources are detected per-LANGUAGE regardless of framework; the GraphQL request-input idiom (typed resolver args) is NOT a recognised request-input source (see vulnerability_finding honest-negative). |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_csharp.go` | #3872: sniffTemplatePatternsCSharp is registered on the csharp language slug and gates only on file content (no per-framework branch), so HotChocolate dispatches it identically. PARTIAL: mirrors all siblings. |
| Vulnerability finding | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3872) | `internal/links/taint_flow.go`<br>`internal/substrate/taint_other_sweep_test.go`<br>`internal/substrate/taint_sites_csharp.go` | #3872 (verify-first NEGATIVE, stays missing): a vulnerability_finding (SecurityFinding) requires a request-input source→sink path and taint_flow.go only seeds its BFS from a TaintKindSource match. The C# taint request-input SOURCE regexes key on ASP.NET HttpRequest accessors (Request.Form/Query/Headers, csSourceRequestRe) and the [From*] action-parameter attributes (csSourceAttrRe). A HotChocolate resolver receives untrusted input via its GraphQL-typed method arguments, which are NOT recognised request-input sources, so although the SQL sink and sanitizers fire on the resolver body, no end-to-end request-input→sink finding is emitted. Proven by TestSubstrate_CSharp_HotChocolate_RequestInputSourceDoesNotFire (zero request-input sources). Honest-missing pending a GraphQL-arg-aware source model. (Note: an env/config source DOES fire — see taint_source_detection — but env→sink is not the request-input vulnerability_finding flow.) |

## Related extraction records

This record provides code-level coverage for the
[`protocol.graphql`](./protocol.graphql.md) hub record (GraphQL),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.framework.hotchocolate ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
