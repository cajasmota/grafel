<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.c-cpp.framework.grpc` — gRPC C++ (grpc++)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C/C++](../by-language/c-cpp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 54

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Federation extraction | — `not_applicable` | — | 3623 | — | Apollo GraphQL Federation directives (@key/@external/@requires/@provides/extend type) do not exist in this RPC framework; not applicable. |
| Procedure extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc.go`<br>`internal/custom/cpp/grpc_protobuf_test.go` | each overridden Status RPC method -> SCOPE.Operation endpoint |
| Schema extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc.go`<br>`internal/custom/cpp/grpc_protobuf_test.go` | req/resp messages emitted as SCOPE.Schema DTO refs (names only) |
| Type graph extraction | — `not_applicable` | — | 3804 | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-SDL concept; gRPC/protobuf/tRPC message schemas are modelled separately and have no GraphQL object-type relationship graph. |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc.go`<br>`internal/custom/cpp/grpc_protobuf_test.go` | Service::NewStub(channel) client-stub site detected |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc.go`<br>`internal/custom/cpp/grpc_protobuf_test.go` | RPC endpoint synthesis from service-impl override methods + RegisterService; regex, no AST |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | — `not_applicable` | — | — | — | HTTP endpoint deprecation/versioning (Sunset/Deprecation headers, /v1 route segments) is an HTTP-API concept; gRPC versions via proto package + service evolution, not HTTP route versioning. |
| Endpoint pagination posture | — `not_applicable` | — | — | — | HTTP limit/offset/page/cursor pagination posture is an HTTP-endpoint concept; gRPC paginates via in-message fields / server-streaming, not HTTP query params. |
| Endpoint response codes | — `not_applicable` | — | — | — | HTTP status-code sets do not apply to gRPC, which signals outcome via grpc::StatusCode on the trailer, not HTTP 2xx/4xx codes. |
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
| Auth coverage | 🔴 `missing` | — | 4038:auth-rpc-gap | — | The c-cpp auth_coverage sniffer is HTTP-route/endpoint-keyed (it scans synthesized HTTP endpoints / router pipelines / framework guards), not framework-agnostic like csharp's [Authorize] attribute. VERIFIED: it emits 0 auth entities on a gRPC service idiom (grpc::ServerContext / protobuf messages / grpc::StatusCode / RegisterService). gRPC auth (call-credentials / metadata interceptors) is a real, untriaged gap — honest missing, not n/a. |

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
| Enum extraction | ✅ `full` | `2026-06-03` | — | `internal/extractors/cpp/extractor.go` | language-level type-system sniffer is framework-agnostic; fires on enums in gRPC service/contract code. VERIFIED on the gRPC idiom. |
| Interface extraction | ✅ `full` | `2026-06-03` | — | `internal/extractors/cpp/extractor.go` | framework-agnostic; fires on the gRPC service impl/contract interface. VERIFIED on the gRPC idiom. |
| Type alias extraction | ✅ `full` | `2026-06-03` | — | `internal/extractors/cpp/extractor.go` | framework-agnostic; fires on type aliases in gRPC code. VERIFIED on the gRPC idiom. |
| Type extraction | ✅ `full` | `2026-06-03` | — | `internal/extractors/cpp/extractor.go` | framework-agnostic; fires on every gRPC service-impl class / payload struct / case class. VERIFIED on the gRPC idiom. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3963 | — | — |
| DI injection point | 🔴 `missing` | — | 3963 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3963 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-03` | — | `internal/extractors/cross/testmap/frameworks_cpp.go`<br>`internal/extractors/cross/testmap/resolver.go` | test-framework sniffer keys on the standard test macros, not the framework-under-test; links gRPC service/router tests like any other test. VERIFIED a named test case is extracted from a gRPC test idiom. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | — | — | `internal/custom/cpp/observability.go`<br>`internal/substrate/template_pattern_c_cpp.go` | logging sniffer is library/call-keyed (framework-agnostic); fires on a gRPC service that logs. VERIFIED on the gRPC idiom. PARTIAL: heuristic. |
| Metric extraction | ✅ `full` | — | — | `internal/custom/cpp/observability.go`<br>`internal/substrate/template_pattern_c_cpp.go` | metric sniffer is library-keyed (framework-agnostic); fires for gRPC services that instrument metrics. VERIFIED on the gRPC idiom. |
| Trace extraction | ✅ `full` | — | — | `internal/custom/cpp/observability.go`<br>`internal/substrate/template_pattern_c_cpp.go` | trace sniffer is library-keyed (framework-agnostic); fires for gRPC services that instrument traces. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc.go`<br>`internal/custom/cpp/grpc_protobuf_test.go` | request message type name from RPC method args; field shapes are protoc-generated |
| Request sink dataflow | — `not_applicable` | — | — | — | The request->sink dataflow sniffer roots taint at HTTP MVC binders / request accessors. gRPC methods take a strongly-typed protobuf request (tRPC: a zod-typed input) + call context, with no HTTP MVC request-root to seed; not applicable to this transport. |
| Response shape extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc.go`<br>`internal/custom/cpp/grpc_protobuf_test.go` | response message type name from RPC method args; field shapes are protoc-generated |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.grpc`](./protocol.grpc.md) hub record (gRPC),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.c-cpp.framework.grpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
