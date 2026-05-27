<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.fiber` — Fiber

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `auth_coverage` | ❌ `missing` | — | — | — | — |
| `endpoint_synthesis` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/go_routes.go`<br>`internal/engine/rules/go/frameworks/fiber.yaml` |
| `handler_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/go_routes.go` |
| `middleware_coverage` | ❌ `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.framework.fiber ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
