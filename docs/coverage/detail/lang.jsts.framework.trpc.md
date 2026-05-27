<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.trpc` — tRPC

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `auth_coverage` | ❌ `missing` | — | — | — | — |
| `endpoint_synthesis` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/http_endpoint_trpc.go`<br>`internal/engine/rules/javascript_typescript/frameworks/trpc.yaml` |
| `handler_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/http_endpoint_trpc.go` |
| `middleware_coverage` | ❌ `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.trpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
