<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.framework.grpc-net` — grpc-dotnet

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C#](../by-language/csharp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 54

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Federation extraction | — `not_applicable` | — | 3623 | — | Apollo GraphQL Federation directives (@key/@external/@requires/@provides/extend type) do not exist in this RPC framework; not applicable. |
| Procedure extraction | 🟢 `partial` | — | — | `internal/custom/csharp/grpc_net.go`<br>`internal/custom/csharp/grpc_net_test.go` | [ProtoContract] annotated C# classes, [ProtoMember] field annotations, and proto file service/rpc declarations emitted as SCOPE.Schema/procedure_extraction. |
| Schema extraction | 🟢 `partial` | — | — | `internal/custom/csharp/grpc_net.go`<br>`internal/custom/csharp/grpc_net_test.go` | Proto message declarations, [DataContract] C# classes, and XxxService:XxxServiceBase generated implementation classes emitted as SCOPE.Schema/schema_extraction. |
| Type graph extraction | — `not_applicable` | — | 3804 | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-SDL concept; gRPC/protobuf/tRPC message schemas are modelled separately and have no GraphQL object-type relationship graph. |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | 🟢 `partial` | — | — | `internal/custom/csharp/grpc_net.go`<br>`internal/custom/csharp/grpc_net_test.go` | GrpcChannel.ForAddress(), new XxxClient(channel), class XxxClient:ClientBase, and services.AddGrpcClient<T>() generated-client detection. |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/custom/csharp/grpc_net.go`<br>`internal/custom/csharp/grpc_net_test.go` | app.MapGrpcService<T>() endpoint registration, services.AddGrpc(), ServerCredentials/SslServerCredentials security bindings, and GrpcServiceOptions/GrpcChannelOptions usage detected. |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | — `not_applicable` | — | — | — | HTTP endpoint deprecation/versioning (Sunset/Deprecation headers, /v1 route segments) is an HTTP-API concept; gRPC versions via proto package + service evolution, not HTTP route versioning. |
| Endpoint pagination posture | — `not_applicable` | — | — | — | HTTP limit/offset/page/cursor pagination posture is an HTTP-endpoint concept; gRPC paginates via in-message fields / server-streaming, not HTTP query params. |
| Endpoint response codes | — `not_applicable` | — | — | — | HTTP status-code sets do not apply to gRPC, which signals outcome via grpc::StatusCode (OK/NOT_FOUND/...) on the trailer, not HTTP 2xx/4xx codes. |
| Endpoint synthesis | — `not_applicable` | — | — | — | HTTP http_endpoint synthesis (path+verb producer endpoints) does not apply to gRPC; gRPC service registration is captured as transport_binding (MapGrpcService<T>/AddGrpc). VERIFIED 0 HTTP endpoints synthesized from a gRPC service impl. |
| Handler attribution | — `not_applicable` | — | — | — | No HTTP handler->route attribution for gRPC. VERIFIED 0 entities from aspnet_core route extractor. rpc-method->service binding is modelled by procedure_extraction (proto rpc/service) + the XxxServiceBase impl in schema_extraction. |
| Route extraction | — `not_applicable` | — | — | — | gRPC-net has no HTTP route paths. VERIFIED: the aspnet route extractor (MapGet/attribute routes) emits 0 entities on a GreeterService:Greeter.GreeterBase impl. RPC method addressing is package.Service/Method, surfaced via procedure_extraction (rpc/service) + transport_binding (MapGrpcService). |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | — `not_applicable` | — | — | — | gRPC services render no server-side views/templates; responses are protobuf messages. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-06-03` | — | `internal/custom/csharp/auth.go`<br>`internal/custom/csharp/auth_test.go` | [Authorize]/[Authorize(Roles=...)]/[Authorize(Policy=...)]/[AllowAnonymous]/RequireAuthorization attrs fire framework-agnostically on gRPC service methods (custom_csharp_auth). VERIFIED: [Authorize(Roles="admin")] on an override Task<HelloReply> SayHello(...) gRPC method emits SCOPE.Pattern/auth_coverage/auth:Authorize:roles:admin. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | — `not_applicable` | — | — | — | gRPC request/response payloads are protobuf messages, surfaced under schema_extraction (message:/datacontract:/service_impl:). MVC DTO inference (FluentValidation AbstractValidator<T> / DataAnnotation model classes) does not model gRPC payloads. VERIFIED 0 entities from custom_csharp_aspnet_reqresp on a gRPC service. |
| Request validation | — `not_applicable` | — | — | — | gRPC has no MVC request-validation pipeline (FluentValidation/DataAnnotations on action models); message-field constraints live in proto. VERIFIED 0 entities from custom_csharp_validation on a gRPC service. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | — `not_applicable` | — | — | — | gRPC cross-cutting concerns use Interceptors (Interceptor base / AddInterceptor), not the ASP.NET Core Use*/middleware pipeline. VERIFIED 0 entities from custom_csharp_middleware_extra on a gRPC service. gRPC interceptor cataloguing is potential future work, tracked separately if needed. |
| Rate limit stamping | — `not_applicable` | — | — | — | HTTP endpoint rate-limit/throttle stamping is an HTTP-middleware concept; gRPC throttling is done via interceptors/server options, not HTTP rate limiters. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-06-03` | — | `internal/extractor/enum_valueset.go`<br>`internal/extractors/csharp/csharp.go` | enum_declaration -> SCOPE.Schema/enum + value-carrying SCOPE.Enum value-set (buildEnumValueSet); framework-agnostic, fires on enums in gRPC service/contract code. |
| Interface extraction | ✅ `full` | `2026-06-03` | — | `internal/extractors/csharp/csharp.go` | tree-sitter CST interface_declaration -> SCOPE.Component/interface; framework-agnostic. |
| Type alias extraction | — `not_applicable` | — | — | — | C# has only file-scoped using-aliases, not first-class type aliases (same as all C# frameworks). |
| Type extraction | ✅ `full` | `2026-06-03` | — | `internal/extractors/csharp/csharp.go` | tree-sitter CST class/struct/record_declaration -> SCOPE.Component; framework-agnostic, fires on every .cs incl. gRPC service impl classes and proto-generated partial classes. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/csharp/dotnet_di.go`<br>`internal/custom/csharp/dotnet_di_test.go` | #3699 ExtractDotnetDI fires framework-agnostically. VERIFIED on a gRPC project Program.cs: services.AddScoped<IGreetRepository,GreetRepository>() emits IGreetRepository BINDS GreetRepository (lifetime=Scoped). |
| DI injection point | 🟢 `partial` | `2026-06-03` | 3963 | `internal/custom/csharp/dotnet_di.go`<br>`internal/custom/csharp/dotnet_di_test.go` | #3699 constructor params emit INJECTED_INTO (service type -> consumer class). VERIFIED: a GreeterService(IGreetRepository repo, ILogger<> logger) ctor emits IGreetRepository INJECTED_INTO GreeterService (ILogger<>/primitives rejected). PARTIAL: same caveat as aspnet-core (impl resolves cross-file only via resolver pass; factory-lambda registrations not linked). |
| DI scope resolution | ✅ `full` | `2026-06-03` | — | `internal/custom/csharp/dotnet_di.go`<br>`internal/custom/csharp/dotnet_di_test.go` | #3699 BINDS edges carry Singleton/Scoped/Transient lifetime parsed from the AddXxx verb; framework-agnostic, fires for gRPC service registrations. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-03` | — | `internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go` | C# NUnit/xUnit/MSTest [Fact]/[Theory]/[Test]/[TestMethod] detected via csharp*MethodRE; framework-agnostic, links gRPC service tests like any C# tests. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | — | 3963 | `internal/custom/csharp/observability.go`<br>`internal/substrate/template_pattern_csharp.go` | ILogger<T> decl + _logger.Log<Level>/Console.WriteLine captured framework-agnostically. VERIFIED on a gRPC service: ILogger<GreeterService> field + _logger.LogInformation(...) emit log_extraction entities. PARTIAL: recording-win, heuristic. |
| Metric extraction | 🟢 `partial` | — | 3963 | `internal/custom/csharp/observability.go` | OTel Meter/CreateCounter/CreateHistogram + prometheus-net regex extractor; framework-agnostic, fires for gRPC services that instrument metrics. PARTIAL: heuristic. |
| Trace extraction | 🟢 `partial` | — | 3963 | `internal/custom/csharp/observability.go` | OTel ActivitySource/StartActivity/Activity.Current regex extractor; framework-agnostic. PARTIAL: heuristic. |

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
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-27` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/csharp.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_csharp.go` | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_csharp.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | — |
| Request sink dataflow | — `not_applicable` | — | — | — | The C# request->sink dataflow sniffer (dataflow_csharp.go) roots taint at ASP.NET MVC binders ([FromBody]/[FromQuery]/[FromForm]/[FromHeader]/[FromRoute]) + Request.Query/Form/RouteValues accessors. gRPC service methods take a strongly-typed protobuf request + ServerCallContext (no MVC binders), so there is no MVC request-root to seed; not applicable to this transport. |
| Response shape extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_csharp.go` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_csharp.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_csharp.go` | — |
| Taint source detection | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_csharp.go` | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_csharp.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_csharp.go` | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.grpc`](./protocol.grpc.md) hub record (gRPC),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.framework.grpc-net ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
