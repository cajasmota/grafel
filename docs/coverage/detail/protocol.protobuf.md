<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `protocol.protobuf` — Protocol Buffers

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [protocol](../by-category/protocol.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Cross repo linkage | 🟢 `partial` | `2026-05-28` | — | `internal/engine/grpc_edges.go` | — |
| Method attribution | ✅ `full` | `2026-05-28` | — | `internal/extractors/proto` | — |
| Service extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/protobuf/_manifest.yaml`<br>`internal/extractors/proto` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update protocol.protobuf ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
