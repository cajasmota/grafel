<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.grpc` — gRPC-Python (grpcio / grpc.aio)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 54

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Federation extraction | — `not_applicable` | — | — | — | Apollo GraphQL Federation directives do not exist in this RPC framework. |
| Procedure extraction | 🟢 `partial` | `2026-06-12` | 4918 | `internal/engine/grpc_edges_test.go` | Servicer impl methods -> SCOPE.GrpcMethod (grpc:<Service>/<Method>) + GRPC_IMPLEMENTS edge (grpc_python_server). #4918 added streaming-shape inference (pyGrpcStreaming): request_iterator param => client_streaming, yielding body => server_streaming, both => bidi_streaming, else unary. Same-file methods only. Fixtures: TestGRPC_Python_Server_AddServicer / _StreamingDetection. |
| Schema extraction | 🔴 `missing` | — | 4918 | — | — |
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL object-type graph is an SDL concept; protobuf message schemas are modelled in protocol.protobuf with no GraphQL type-relationship graph. |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | 🟢 `partial` | `2026-06-12` | 4918 | `internal/engine/grpc_edges_test.go` | pb2_grpc XServiceStub(channel) client (module-qualified, direct-import, and bare forms, gated on a _pb2_grpc import) -> SCOPE.GrpcService (grpc_python_client, role=client) + GrpcMethod + GRPC_HANDLES edge. Fixtures: TestGRPC_Python_Client_Stub / _DirectImportStub / _ModuleQualifiedStub. |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | 🟢 `partial` | `2026-06-12` | 4918 | `internal/engine/grpc_edges_test.go` | synthesizePythonGRPC in grpc_edges.go: pb2_grpc.add_XServicer_to_server(impl, server) AND classes extending a *Servicer base -> SCOPE.GrpcService (grpc_python_server). Regex, no AST; fires when a _pb2_grpc import is present. Fixture: TestGRPC_Python_Server_AddServicer. |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | — `not_applicable` | — | — | — | HTTP Sunset/Deprecation/route-version is an HTTP concept; gRPC versions via proto package + service evolution. |
| Endpoint pagination posture | — `not_applicable` | — | — | — | HTTP limit/offset/page/cursor pagination is an HTTP concept; gRPC paginates via in-message fields / server-streaming. |
| Endpoint response codes | — `not_applicable` | — | — | — | HTTP status codes do not apply to gRPC, which signals outcome via grpc.StatusCode on the trailer. |
| Endpoint synthesis | — `not_applicable` | — | — | — | HTTP http_endpoint synthesis does not apply to gRPC; service registration is captured as transport_binding. |
| Handler attribution | — `not_applicable` | — | — | — | No HTTP handler->route attribution for gRPC; RPC-method->service binding is procedure_extraction + GRPC_IMPLEMENTS. |
| Route extraction | — `not_applicable` | — | — | — | gRPC has no HTTP route paths; RPC method addressing is package.Service/Method, surfaced via procedure_extraction + transport_binding. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | — `not_applicable` | — | — | — | gRPC renders no server-side views/templates; responses are protobuf messages. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 4918 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 4918 | — | — |
| Request validation | 🔴 `missing` | — | 4918 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 4918 | — | — |
| Rate limit stamping | 🔴 `missing` | — | 4918 | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 4918 | — | — |
| Interface extraction | 🔴 `missing` | — | 4918 | — | — |
| Type alias extraction | 🔴 `missing` | — | 4918 | — | — |
| Type extraction | 🔴 `missing` | — | 4918 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 4918 | — | — |
| DI injection point | 🔴 `missing` | — | 4918 | — | — |
| DI scope resolution | 🔴 `missing` | — | 4918 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 4918 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 4918 | — | — |
| Metric extraction | 🔴 `missing` | — | 4918 | — | — |
| Trace extraction | 🔴 `missing` | — | 4918 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 4918 | — | — |
| Config consumption | 🔴 `missing` | — | 4918 | — | — |
| Constant propagation | 🔴 `missing` | — | 4918 | — | — |
| DB effect | 🔴 `missing` | — | 4918 | — | — |
| Dead code detection | 🔴 `missing` | — | 4918 | — | — |
| Def use chain extraction | 🔴 `missing` | — | 4918 | — | — |
| Env fallback recognition | 🔴 `missing` | — | 4918 | — | — |
| Error flow | 🔴 `missing` | — | 4918 | — | — |
| Feature flag gating | 🔴 `missing` | — | 4918 | — | — |
| Fs effect | 🔴 `missing` | — | 4918 | — | — |
| HTTP effect | 🔴 `missing` | — | 4918 | — | — |
| Import resolution quality | 🟢 `partial` | `2026-06-12` | 3045 | `internal/substrate/python.go` | Framework-blind python import resolver (internal/substrate/python.go + constant_propagation.go) runs over gRPC handler/stub modules like any other python file; same posture as the other python framework records. |
| Module cycle detection | 🔴 `missing` | — | 4918 | — | — |
| Mutation effect | 🔴 `missing` | — | 4918 | — | — |
| Pure function tagging | 🔴 `missing` | — | 4918 | — | — |
| Reachability analysis | 🔴 `missing` | — | 4918 | — | — |
| Request shape extraction | 🔴 `missing` | — | 4918 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 4918 | — | — |
| Response shape extraction | 🔴 `missing` | — | 4918 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 4918 | — | — |
| Schema drift detection | 🔴 `missing` | — | 4918 | — | — |
| Taint sink detection | 🔴 `missing` | — | 4918 | — | — |
| Taint source detection | 🔴 `missing` | — | 4918 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 4918 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 4918 | — | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.grpc`](./protocol.grpc.md) hub record (gRPC),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.grpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
