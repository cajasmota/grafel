<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.tonic` — Tonic

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 38

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go`<br>`internal/custom/rust/tonic.go` | RPC endpoints synthesized per async method; .add_service(<Svc>Server::new) captured as SCOPE.Service registration |
| Handler attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go`<br>`internal/custom/rust/tonic.go` | handler_name=<ImplType>.<method> attributed per RPC method |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go`<br>`internal/custom/rust/tonic.go` | #[tonic::async_trait] impl <Service> for <Type> RPC methods become RPC endpoints at /<Service>/<Method>; verb=RPC, rpc_protocol=grpc |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go`<br>`internal/custom/rust/tonic.go` | Request<T>/Response<T> message types emitted as SCOPE.Schema DTOs with grpc_message_role request/response |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | 🟢 `partial` | `2026-05-30` | 3508 | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/tonic.go` | Service trait NAME recovered from impl <Service> for <Type>; the trait itself is tonic-build-generated and not statically present |
| Type alias extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/helpers.go`<br>`internal/custom/rust/tonic.go` | gRPC message type names recovered from Request<T>/Response<T> wrappers |

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
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🟢 `partial` | `2026-05-30` | 3508 | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/tonic.go` | Request<T> message type NAME recovered; field shapes live in tonic-build-generated structs (build.rs OUT_DIR), not statically present in source |
| Response shape extraction | 🟢 `partial` | `2026-05-30` | 3508 | `internal/custom/rust/graphql_grpc_test.go`<br>`internal/custom/rust/tonic.go` | Response<T> message type NAME recovered; generated message field shapes not statically resolvable |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.tonic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
