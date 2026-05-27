<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.gin` — Gin

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 13

## Capabilities


### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `endpoint_synthesis` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/go_routes.go`<br>`internal/engine/rules/go/frameworks/gin.yaml` | — |
| `handler_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/go_routes.go` | — |

### Security

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `auth_coverage` | ❌ `missing` | — | — | — | — | — |

### Validation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Middleware

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `middleware_coverage` | ❌ `missing` | — | — | — | — | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Observability

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Data

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `constant_propagation` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go` | — |
| `db_effect` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/substrate/effect_sinks_golang.go`<br>`internal/links/effect_propagation.go` | — |
| `dead_code_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/substrate/entry_points_golang.go`<br>`internal/substrate/entry_points.go`<br>`internal/links/reachability.go`<br>`internal/mcp/dead_code.go` | — |
| `env_fallback_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go` | — |
| `fs_effect` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/substrate/effect_sinks_golang.go`<br>`internal/links/effect_propagation.go` | — |
| `http_effect` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/substrate/effect_sinks_golang.go`<br>`internal/links/effect_propagation.go` | — |
| `import_resolution_quality` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/golang.go`<br>`internal/substrate/substrate.go` | — |
| `mutation_effect` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/substrate/effect_sinks_golang.go`<br>`internal/links/effect_propagation.go` | — |
| `reachability_analysis` | ✅ `full` | `2026-05-28` | — | — | `internal/substrate/entry_points_golang.go`<br>`internal/substrate/entry_points.go`<br>`internal/links/reachability.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.framework.gin ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
