<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.c-cpp.framework.protobuf` — Protocol Buffers (C++)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C/C++](../by-language/c-cpp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 26

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Procedure extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/protobuf.go` | each .proto rpc -> SCOPE.Operation endpoint /<Service>/<Method> |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/protobuf.go` | .proto messages+fields fully parsed (name/type/number/label); generated .pb.h message classes recovered by name |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/protobuf.go` | .proto service+rpc -> SCOPE.Service + RPC SCOPE.Operation endpoints with streaming kind |

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
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/protobuf.go` | rpc request message names from .proto service rpc decls |
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
