<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.c-cpp.framework.protobuf` — Protocol Buffers (C++)

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
| Procedure extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/protobuf.go` | each .proto rpc -> SCOPE.Operation endpoint /<Service>/<Method> |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/protobuf.go` | .proto messages+fields fully parsed (name/type/number/label); generated .pb.h message classes recovered by name |
| Type graph extraction | — `not_applicable` | — | 3804 | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-SDL concept; gRPC/protobuf/tRPC message schemas are modelled separately and have no GraphQL object-type relationship graph. |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/protobuf.go` | .proto service+rpc -> SCOPE.Service + RPC SCOPE.Operation endpoints with streaming kind |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3963 | — | — |
| Endpoint pagination posture | 🔴 `missing` | — | 3963 | — | — |
| Endpoint response codes | 🔴 `missing` | — | 3963 | — | — |
| Endpoint synthesis | 🔴 `missing` | — | 3963 | — | — |
| Handler attribution | 🔴 `missing` | — | 3963 | — | — |
| Route extraction | 🔴 `missing` | — | 3963 | — | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | 3963 | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 3963 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 3963 | — | — |
| Request validation | 🔴 `missing` | — | 3963 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 3963 | — | — |
| Rate limit stamping | 🔴 `missing` | — | 3963 | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3963 | — | — |
| Interface extraction | 🔴 `missing` | — | 3963 | — | — |
| Type alias extraction | 🔴 `missing` | — | 3963 | — | — |
| Type extraction | 🔴 `missing` | — | 3963 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3963 | — | — |
| DI injection point | 🔴 `missing` | — | 3963 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3963 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 3963 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 3963 | — | — |
| Metric extraction | 🔴 `missing` | — | 3963 | — | — |
| Trace extraction | 🔴 `missing` | — | 3963 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_c_cpp.go` | c-cpp entry-point sniffer roots reachability/dead-code on the generated service-method library_export; #4047 |
| Def use chain extraction | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_c_cpp.go` | c-cpp def-use sniffer fires on .cc handler bodies (named def->use, e.g. SayHello name/greeting); #4047 |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | language-agnostic Tarjan SCC over IMPORTS; c-cpp #include edges flow through extractor; #4047 |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | c-cpp effect sniffer registered; handler methods with no effect match tagged pure=true; #4047 |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_c_cpp.go` | c-cpp entry-point sniffer roots reachability on the generated service-method library_export; #4047 |
| Request shape extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/protobuf.go` | rpc request message names from .proto service rpc decls |
| Request sink dataflow | 🔴 `missing` | — | 3963 | — | — |
| Response shape extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/protobuf.go` | rpc response message names from .proto service rpc decls |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.c-cpp.framework.protobuf ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
