<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rust.framework.axum` — Axum

Auto-generated. Back to [summary](../summary.md).

- **Language:** [rust](../by-language/rust.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `auth_coverage` | ❌ `missing` | — | — | — | — |
| `endpoint_synthesis` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/http_endpoint_axum.go`<br>`internal/engine/rules/rust/frameworks/axum.yaml` |
| `handler_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/http_endpoint_axum.go` |
| `middleware_coverage` | ❌ `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rust.framework.axum ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
