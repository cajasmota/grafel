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
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/csharp/config_consumer.go`<br>`internal/extractors/csharp/config_consumer_test.go` | IConfiguration indexer/GetValue/GetConnectionString + Environment.GetEnvironmentVariable -> config:<key> (issue #3641) |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | ✅ `full` | `2026-06-03` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/csharp/exception_flow.go`<br>`internal/extractors/csharp/exception_flow_test.go` | throw new X / throw new pkg.X -> THROWS; catch (X ex) / catch (pkg.X) -> CATCHES; bare catch + throw;/throw e re-throw dropped (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🟢 `partial` | `2026-06-02` | 3961 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go`<br>`internal/substrate/payload_shapes_hotchocolate_test.go` | #3961: sniffHotChocolateResolverShapes surfaces each HotChocolate resolver method's typed argument list as a producer REQUEST shape (DTO-typed args expanded to the DTO's fields; scalar args one field each; ambient framework params like CancellationToken/IResolverContext skipped) and its typed return as a producer RESPONSE shape (Task<>/IEnumerable<>/List<>/nullable wrappers unwrapped to the leaf DTO), flowing through the standard payload-drift join. Gated on a HotChocolate file-signal. Partial: DTO-field + scalar-arg expansion only — no inline-literal resolver-body resolution or cross-file DTO import. |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🟢 `partial` | `2026-06-02` | 3961 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go`<br>`internal/substrate/payload_shapes_hotchocolate_test.go` | #3961: sniffHotChocolateResolverShapes surfaces each HotChocolate resolver method's typed argument list as a producer REQUEST shape (DTO-typed args expanded to the DTO's fields; scalar args one field each; ambient framework params like CancellationToken/IResolverContext skipped) and its typed return as a producer RESPONSE shape (Task<>/IEnumerable<>/List<>/nullable wrappers unwrapped to the leaf DTO), flowing through the standard payload-drift join. Gated on a HotChocolate file-signal. Partial: DTO-field + scalar-arg expansion only — no inline-literal resolver-body resolution or cross-file DTO import. |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

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
