<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.scala.framework.scalapb-grpc` — ScalaPB / zio-grpc / fs2-grpc

Auto-generated. Back to [summary](../summary.md).

- **Language:** [scala](../by-language/scala.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 54

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Federation extraction | — `not_applicable` | — | 3623 | — | Apollo GraphQL Federation directives (@key/@external/@requires/@provides/extend type) do not exist in this RPC framework; not applicable. |
| Procedure extraction | ✅ `full` | `2026-05-31` | — | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | custom_scala_grpc: each def <rpc>(request: ReqT): Eff[RespT] of a ScalaPB AbstractService / zio-grpc ZGeneratedService / fs2-grpc *Fs2Grpc service trait -> SCOPE.Operation endpoint at /<Service>/<rpc>, verb=RPC, rpc_protocol=grpc, grpc_service+grpc_method+handler_name stamped. Value-asserting tests pin sayHello/listUsers + service Greeter (Z/Fs2Grpc decorations stripped). File-local. |
| Schema extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | custom_scala_grpc: request/response message types recovered from the method signature (Request<T> param + last effect type-arg of ZIO/Future/F[_]) and emitted as SCOPE.Schema DTO refs with grpc_message_role request/response. PARTIAL: message FIELD shapes live in ScalaPB-generated case-class companions; names only. Value-asserted (HelloRequest/HelloReply/UserList). |
| Type graph extraction | — `not_applicable` | — | 3804 | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-SDL concept; gRPC/protobuf/tRPC message schemas are modelled separately and have no GraphQL object-type relationship graph. |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | custom_scala_grpc: <Service>Grpc.stub/blockingStub/bindService(Resource) site detected -> SCOPE.Component grpc_stub with companion+accessor. PARTIAL: generated stub/companion code itself is scalapbc-emitted, not statically present; we record the use site. Value-asserted (GreeterGrpc.stub). |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | ✅ `full` | `2026-05-31` | — | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | custom_scala_grpc: RPC endpoint synthesis /<Service>/<rpc> from the service-trait method set; service trait emitted as SCOPE.Service grpc_service. Value-asserting tests pin the path + grpc_service. Regex, file-local; no .proto/AST. |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | — `not_applicable` | — | — | — | HTTP endpoint deprecation/versioning (Sunset/Deprecation headers, /v1 route segments) is an HTTP-API concept; gRPC versions via proto package + service evolution, not HTTP route versioning. |
| Endpoint pagination posture | — `not_applicable` | — | — | — | HTTP limit/offset/page/cursor pagination posture is an HTTP-endpoint concept; gRPC paginates via in-message fields / server-streaming, not HTTP query params. |
| Endpoint response codes | — `not_applicable` | — | — | — | HTTP status-code sets do not apply to gRPC, which signals outcome via io.grpc.Status on the trailer, not HTTP 2xx/4xx codes. |
| Endpoint synthesis | — `not_applicable` | — | — | — | HTTP http_endpoint synthesis (path+verb producer endpoints) does not apply to gRPC; service registration is captured as transport_binding. gRPC method addressing is package.Service/Method, not an HTTP path+verb. |
| Handler attribution | — `not_applicable` | — | — | — | No HTTP handler->route attribution for gRPC. RPC-method->service binding is modelled by procedure_extraction (rpc/service) + the service-impl in schema_extraction, not by an HTTP route table. |
| Route extraction | — `not_applicable` | — | — | — | gRPC has no HTTP route paths; the HTTP route extractor emits 0 entities on a gRPC service impl. RPC method addressing is package.Service/Method, surfaced via procedure_extraction + transport_binding. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | — `not_applicable` | — | — | — | gRPC services render no server-side views/templates; responses are protobuf messages (tRPC returns plain typed values), not rendered views. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | — | 4041:grpc-scala-interceptor-auth | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_auth_test.go` | gRPC interceptor auth is now detected (#4041, gRPC-Scala slice): scalaGRPCResolveAuth recognises an auth-enforcing interceptor and stamps auth_required=true + auth_method=grpc_interceptor + auth_middleware (MCP signal-1) + auth_confidence on every RPC-method SCOPE.Operation endpoint (+ the SCOPE.Service) of the gRPC services in the file. Three idioms: (1) a grpc-java io.grpc.ServerInterceptor class/object (scalapb-grpc & fs2-grpc ride grpc-java) whose body rejects with Status.UNAUTHENTICATED / PERMISSION_DENIED AND is WIRED via ServerInterceptors.intercept(...) / .intercept(...); (2) a zio-grpc ZServerInterceptor class rejecting unauthenticated; (3) a zio-grpc transformContextZIO/transformContext combinator rejecting unauthenticated (the transform is itself the wiring). VALUE-ASSERTING tests pin auth_required+grpc_interceptor+auth_middleware=AuthInterceptor on sayHello/listUsers, auth_middleware=JwtInterceptor (ZServerInterceptor), and auth_middleware=transformContextZIO (zio transform). NEGATIVES proven: logging interceptor (no UNAUTHENTICATED reject) → NO auth; present-but-UNWIRED grpc-java interceptor → NO auth; no interceptor → NO auth. VERIFY-FIRST confirmed auth_required='' before the change. HONEST PARTIAL: the interceptor→service binding is file-local (same scope as the rest of the Scala gRPC synthesis) — an auth interceptor defined in another module and wired in a third file is not chased; per-method [Authorize]-style selective interception is not modelled (Scala gRPC interceptors guard the whole server/service). Remaining gRPC siblings on #4041: elixir.grpc + grpc-net. DEPLOY-DEFERRED. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | — `not_applicable` | — | — | — | gRPC request/response payloads are protobuf messages (tRPC: zod-typed inputs), surfaced under schema_extraction; MVC DTO inference does not model them. HTTP MVC DTO extractor yields 0 entities on a gRPC service. |
| Request validation | — `not_applicable` | — | — | — | gRPC has no MVC request-validation pipeline; message-field constraints live in proto (tRPC validates via the zod input schema, surfaced as schema, not an MVC validator). HTTP MVC validation extractor yields 0 entities here. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | — `not_applicable` | — | — | — | gRPC cross-cutting concerns use interceptors (tRPC: middleware links), not the HTTP framework's Use*/plug/filter request-pipeline. The HTTP middleware extractor yields 0 entities on a gRPC service; interceptor cataloguing is potential future work. |
| Rate limit stamping | — `not_applicable` | — | — | — | HTTP endpoint rate-limit/throttle stamping is an HTTP-middleware concept; gRPC throttling is done via interceptors/server options, not HTTP rate limiters. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/type_system.go` | language-level type-system sniffer is framework-agnostic; fires on enums in gRPC service/contract code. VERIFIED on the gRPC idiom. |
| Interface extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/type_system.go` | framework-agnostic; fires on the gRPC service impl/contract interface. VERIFIED on the gRPC idiom. |
| Type alias extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/type_system.go` | framework-agnostic; fires on type aliases in gRPC code. VERIFIED on the gRPC idiom. |
| Type extraction | ✅ `full` | `2026-06-03` | — | `internal/custom/scala/type_system.go` | framework-agnostic; fires on every gRPC service-impl class / payload struct / case class. VERIFIED on the gRPC idiom. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | — `not_applicable` | — | — | — | matches the scala HTTP flagship (http4s): scala backends compose via explicit constructors/`Resource`/cats-effect wiring, not a runtime DI container; nothing for the DI sniffer to bind on a scalapb gRPC impl. |
| DI injection point | — `not_applicable` | — | — | — | matches the scala HTTP flagship (http4s): scala backends compose via explicit constructors/`Resource`/cats-effect wiring, not a runtime DI container; nothing for the DI sniffer to bind on a scalapb gRPC impl. |
| DI scope resolution | — `not_applicable` | — | — | — | matches the scala HTTP flagship (http4s): scala backends compose via explicit constructors/`Resource`/cats-effect wiring, not a runtime DI container; nothing for the DI sniffer to bind on a scalapb gRPC impl. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-03` | — | `internal/extractors/cross/testmap/frameworks.go` | test-framework sniffer keys on the standard test macros, not the framework-under-test; links gRPC service/router tests like any other test. VERIFIED a named test case is extracted from a gRPC test idiom. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | — | — | `internal/custom/scala/frameworks.go` | logging sniffer is library/call-keyed (framework-agnostic); fires on a gRPC service that logs. VERIFIED on the gRPC idiom. PARTIAL: heuristic. |
| Metric extraction | ✅ `full` | — | — | `internal/custom/scala/frameworks.go` | metric sniffer is library-keyed (framework-agnostic); fires for gRPC services that instrument metrics. VERIFIED on the gRPC idiom. |
| Trace extraction | ✅ `full` | — | — | `internal/custom/scala/frameworks.go` | trace sniffer is library-keyed (framework-agnostic); fires for gRPC services that instrument traces. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go, RegisterEffectSniffer('scala')) recognises Slick/Doobie/Quill/JPA read+write primitives; framework-agnostic, fires on any .scala file. Fixture: TestScalaTrailing_Grpc_Effects. |
| Dead code detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | Language-agnostic reachability/dead-code pass over Scala entry points (entry_points_scala.go) + IMPORTS/CALLS edges; framework-agnostic, fires on any .scala file. |
| Def use chain extraction | 🟢 `partial` | `2026-06-03` | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_scala.go` | Scala def-use sniffer (RegisterDefUseSniffer('scala'), def_use_scala.go) fires on any .scala file via LanguageForPath; def_use_pass.go invokes it for all scala entities. File-local val/var/for-generator def->use pairs. Fixture: TestScalaTrailing_Grpc_DefUse. |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go) recognises Source.fromFile/Files/os-lib read+write primitives; framework-agnostic. |
| HTTP effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go) recognises outbound HTTP primitives (sttp/akka-pekko/http4s/requests); framework-agnostic. |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/module_cycle_pass.go` | Language-agnostic module-cycle pass uses IMPORTS edges emitted by the Scala extractor pipeline; framework-agnostic. |
| Mutation effect | 🟢 `partial` | `2026-06-03` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_scala.go` | Scala effect sniffer (effect_sinks_scala.go) recognises this.<field>= mutation; framework-agnostic. |
| Pure function tagging | 🟢 `partial` | `2026-06-03` | — | `internal/links/pure_function_pass.go` | Language-agnostic pure-function pass tags Scala functions with no effect properties; framework-agnostic (esp. apt for effectful/functional Scala idioms). |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_scala.go` | Language-agnostic reachability pass seeded from Scala entry points (entry_points_scala.go); framework-agnostic. |
| Request shape extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | Request message type NAME recovered from the RPC param type; field shapes live in ScalaPB-generated message companions (not statically present). Value-asserted (HelloRequest). |
| Request sink dataflow | — `not_applicable` | — | — | — | The request->sink dataflow sniffer roots taint at HTTP MVC binders / request accessors. gRPC methods take a strongly-typed protobuf request (tRPC: a zod-typed input) + call context, with no HTTP MVC request-root to seed; not applicable to this transport. |
| Response shape extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/scala/grpc.go`<br>`internal/custom/scala/grpc_test.go` | Response message type NAME recovered as the last effect type-argument (Future/ZIO/F[_]); generated message field shapes not statically resolvable. Value-asserted (HelloReply). |
| Sanitizer recognition | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint sniffer (taint_sites_scala.go) recognises parameterised-SQL/HTML-escape/Form-mapping sanitizers; framework-agnostic. |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint sniffer (taint_sites_scala.go) recognises SQL-splice/command/path/XSS/ReDoS sinks; framework-agnostic. Fixture: TestScalaTrailing_Grpc_Taint. |
| Taint source detection | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint sniffer (taint_sites_scala.go, RegisterTaintSniffer('scala')) recognises request/param/sys.env/decode sources; framework-agnostic, fires on any .scala file. |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🟢 `partial` | `2026-06-03` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_scala.go` | Scala taint flow (taint_flow.go over taint_sites_scala.go) reports source->sink findings; framework-agnostic. Fixture: TestScalaTrailing_Grpc_Taint. |

## Related extraction records

This record provides code-level coverage for the
[`protocol.grpc`](./protocol.grpc.md) hub record (gRPC),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.scala.framework.scalapb-grpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
