<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.grpc` — gRPC-Go (google.golang.org/grpc)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 54

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Federation extraction | — `not_applicable` | — | — | — | Apollo GraphQL Federation directives (@key/@external/@requires/@provides/extend type) do not exist in this RPC framework; not applicable. |
| Procedure extraction | 🟢 `partial` | `2026-06-03` | 4041 | `internal/engine/grpc_edges.go`<br>`internal/engine/grpc_edges_test.go` | Each receiver-method on a registered impl type -> SCOPE.GrpcMethod (grpc:<Service>/<Method>) + GRPC_IMPLEMENTS edge, with streaming variant (unary/server/client/bidi) inferred from stream.Send/Recv. Same-file methods only; boilerplate stubs skipped. |
| Schema extraction | 🔴 `missing` | — | 4041 | — | — |
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type->type graph is a GraphQL-SDL concept; gRPC/protobuf message schemas are modelled separately (protocol.protobuf) and have no GraphQL object-type relationship graph. |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | 🟢 `partial` | `2026-06-03` | 4041 | `internal/engine/grpc_edges.go`<br>`internal/engine/grpc_edges_test.go` | pb.NewXxxClient(conn) client-stub site + stub.Method(ctx, req) call -> SCOPE.GrpcMethod + GRPC_HANDLES edge. |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | 🟢 `partial` | `2026-06-03` | 4041 | `internal/engine/grpc_edges.go`<br>`internal/engine/grpc_edges_test.go` | RPC service synthesis from Go pb.RegisterXxxServer(srv, &Impl{}) call sites -> SCOPE.GrpcService (grpc_go_server); regex, no AST. synthesizeGoGRPC in grpc_edges.go (#725). |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | — `not_applicable` | — | — | — | HTTP endpoint deprecation/versioning (Sunset/Deprecation headers, /v1 route segments) is an HTTP-API concept; gRPC versions via proto package + service evolution, not HTTP route versioning. |
| Endpoint pagination posture | — `not_applicable` | — | — | — | HTTP limit/offset/page/cursor pagination posture is an HTTP-endpoint concept; gRPC paginates via in-message fields / server-streaming, not HTTP query params. |
| Endpoint response codes | — `not_applicable` | — | — | — | HTTP status-code sets do not apply to gRPC, which signals outcome via grpc.Status/codes on the trailer, not HTTP 2xx/4xx codes. |
| Endpoint synthesis | — `not_applicable` | — | — | — | HTTP http_endpoint synthesis (path+verb producer endpoints) does not apply to gRPC; service registration is captured as transport_binding. gRPC method addressing is package.Service/Method, not an HTTP path+verb. |
| Handler attribution | — `not_applicable` | — | — | — | No HTTP handler->route attribution for gRPC. RPC-method->service binding is modelled by procedure_extraction + GRPC_IMPLEMENTS, not by an HTTP route table. |
| Route extraction | — `not_applicable` | — | — | — | gRPC has no HTTP route paths; the HTTP route extractor emits 0 entities on a gRPC service impl. RPC method addressing is package.Service/Method, surfaced via procedure_extraction + transport_binding. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | — `not_applicable` | — | — | — | gRPC services render no server-side views/templates; responses are protobuf messages, not rendered views. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-06-03` | — | `internal/engine/grpc_edges.go`<br>`internal/engine/grpc_go_auth.go`<br>`internal/engine/grpc_go_auth_test.go` | gRPC-Go interceptor auth (#4041, epic #3872). resolveGoGRPCInterceptorAuth detects an auth-enforcing UnaryServerInterceptor/StreamServerInterceptor wired into grpc.NewServer (grpc.UnaryInterceptor / ChainUnaryInterceptor / StreamInterceptor / ChainStreamInterceptor) — an interceptor func that BOTH reads metadata.FromIncomingContext AND rejects with codes.Unauthenticated/PermissionDenied — plus the go-grpc-middleware grpc_auth.UnaryServerInterceptor/StreamServerInterceptor helper. Stamps auth_required=true + auth_method=grpc_interceptor + auth_middleware (MCP signal-1) + auth_policy on the SCOPE.GrpcService and the same-file SCOPE.GrpcMethod handlers. Negatives: logging/tracing interceptor (no metadata-reject) and no-interceptor server leave methods UNSTAMPED. HONEST PARTIAL boundary: handler methods declared in a separate file from the RegisterXxxServer call are not stamped (same same-file boundary as the rest of Go gRPC synthesis). |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | — `not_applicable` | — | — | — | gRPC request/response payloads are protobuf messages (surfaced under protocol.protobuf schema_extraction); MVC DTO inference does not model them. HTTP MVC DTO extractor yields 0 entities on a gRPC service. |
| Request validation | — `not_applicable` | — | — | — | gRPC has no MVC request-validation pipeline; message-field constraints live in proto. HTTP MVC validation extractor yields 0 entities here. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | — `not_applicable` | — | — | — | gRPC-Go has no HTTP middleware pipeline; cross-cutting concerns are server INTERCEPTORS (auth interceptors credited under auth_coverage). The HTTP route-middleware extractor yields 0 entities on a gRPC server. |
| Rate limit stamping | — `not_applicable` | — | — | — | Rate-limit stamping keys off HTTP route-middleware / decorators; gRPC-Go rate limiting is an interceptor, not an HTTP route rate-limiter. HTTP rate-limit extractor yields 0 entities here. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 4041 | — | — |
| Interface extraction | 🔴 `missing` | — | 4041 | — | — |
| Type alias extraction | 🔴 `missing` | — | 4041 | — | — |
| Type extraction | 🔴 `missing` | — | 4041 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 4041 | — | — |
| DI injection point | 🔴 `missing` | — | 4041 | — | — |
| DI scope resolution | 🔴 `missing` | — | 4041 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 4041 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 4041 | — | — |
| Metric extraction | 🔴 `missing` | — | 4041 | — | — |
| Trace extraction | 🔴 `missing` | — | 4041 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 4041 | — | — |
| Config consumption | 🔴 `missing` | — | 4041 | — | — |
| Constant propagation | 🔴 `missing` | — | 4041 | — | — |
| DB effect | 🔴 `missing` | — | 4041 | — | — |
| Dead code detection | 🔴 `missing` | — | 4041 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 4041 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 4041 | — | — |
| Error flow | 🔴 `missing` | — | 4041 | — | — |
| Feature flag gating | 🔴 `missing` | — | 4041 | — | — |
| Fs effect | 🔴 `missing` | — | 4041 | — | — |
| HTTP effect | 🔴 `missing` | — | 4041 | — | — |
| Import resolution quality | 🔴 `missing` | — | 4041 | — | — |
| Module cycle detection | 🔴 `missing` | — | 4041 | — | — |
| Mutation effect | 🔴 `missing` | — | 4041 | — | — |
| Pure function tagging | 🔴 `missing` | — | 4041 | — | — |
| Reachability analysis | 🔴 `missing` | — | 4041 | — | — |
| Request shape extraction | 🔴 `missing` | — | 4041 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 4041 | — | — |
| Response shape extraction | 🔴 `missing` | — | 4041 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 4041 | — | — |
| Schema drift detection | 🔴 `missing` | — | 4041 | — | — |
| Taint sink detection | 🔴 `missing` | — | 4041 | — | — |
| Taint source detection | 🔴 `missing` | — | 4041 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 4041 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 4041 | — | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.grpc`](./protocol.grpc.md) hub record (gRPC),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.framework.grpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
