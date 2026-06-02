<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.framework.grpc` — elixir-grpc

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 30

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Federation extraction | — `not_applicable` | — | 3623 | — | Apollo GraphQL Federation directives (@key/@external/@requires/@provides/extend type) do not exist in this RPC framework; not applicable. |
| Procedure extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/elixir/grpc.go`<br>`internal/custom/elixir/grpc_test.go` | rpc :Method, Req, Resp declarations in use GRPC.Service modules emitted as SCOPE.GrpcMethod (grpc:<service>/<method>) with method + request/response message names. |
| Schema extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/elixir/grpc.go`<br>`internal/custom/elixir/grpc_test.go` | Request/response protobuf message names captured per rpc; stream() wrappers stripped and classified into streaming mode. |
| Type graph extraction | — `not_applicable` | — | 3804 | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-SDL concept; gRPC/protobuf/tRPC message schemas are modelled separately and have no GraphQL object-type relationship graph. |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/elixir/grpc.go`<br>`internal/custom/elixir/grpc_test.go` | use GRPC.Server / GRPC.Service / GRPC.Stub modules emitted as SCOPE.GrpcService with grpc_role server|definition|client; service name resolved from name:/service: option. |

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
| Import resolution quality | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/elixir/grpc.go`<br>`internal/custom/elixir/grpc_test.go` | Cross-repo identity grpc:<service>/<method> matches the shared #725 linker convention so client stub and server impl join across repos. |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/elixir/grpc.go`<br>`internal/custom/elixir/grpc_test.go` | rpc request message type recorded as request_message on each SCOPE.GrpcMethod. |
| Response shape extraction | 🟢 `partial` | `2026-05-31` | backfill:dictionary-completeness | `internal/custom/elixir/grpc.go`<br>`internal/custom/elixir/grpc_test.go` | rpc response message type recorded as response_message on each SCOPE.GrpcMethod. |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.framework.grpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
