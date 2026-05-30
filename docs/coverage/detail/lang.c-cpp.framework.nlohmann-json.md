<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.c-cpp.framework.nlohmann-json` — nlohmann/json (C++)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [C/C++](../by-language/c-cpp.md)
- **Category:** [validation](../by-category/validation.md)
- **Subcategory:** Validation
- **Capability cells:** 6

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Nested model extraction | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/nlohmann_json.go` | to_json/from_json free-function pairs bind a struct type as serialize/deserialize/bidirectional; nested member C++ types not resolved |
| Schema extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/nlohmann_json.go` | NLOHMANN_DEFINE_TYPE(_INTRUSIVE/_NON_INTRUSIVE/_WITH_DEFAULT) -> SCOPE.Schema DTO + per-member field entities; member list is the serialization contract |

### Constraints

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constraint extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Custom validator extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Coercion

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type coercion recognition | 🟢 `partial` | `2026-05-30` | backfill:dictionary-completeness | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/nlohmann_json.go` | serialization_direction (to_json/from_json) recorded; field C++ types declared in struct body, not resolved here |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-30` | — | `internal/custom/cpp/grpc_protobuf_test.go`<br>`internal/custom/cpp/nlohmann_json.go` | value-asserting fixtures assert DTO + exact field list + macro variant + free-function direction |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.c-cpp.framework.nlohmann-json ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
