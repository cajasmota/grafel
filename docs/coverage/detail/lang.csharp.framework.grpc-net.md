<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.csharp.framework.grpc-net` — grpc-dotnet

Auto-generated. Back to [summary](../summary.md).

- **Language:** [csharp](../by-language/csharp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `auth_coverage` | ❌ `missing` | — | — | — | — |
| `endpoint_synthesis` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/grpc_edges.go`<br>`internal/engine/rules/csharp/frameworks/grpc_net.yaml` |
| `handler_attribution` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/rules/csharp/frameworks/grpc_net.yaml` |
| `middleware_coverage` | ❌ `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.csharp.framework.grpc-net ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
