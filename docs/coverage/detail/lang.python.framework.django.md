<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python.framework.django` — Django

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 10

## Capabilities


### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `endpoint_synthesis` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/django_routes.go`<br>`internal/engine/django_urlconf_nested.go`<br>`internal/engine/rules/python/frameworks/django.yaml` | — |
| `handler_attribution` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/django_admin_routes.go`<br>`internal/engine/django_routes.go` | — |

### Security

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `auth_coverage` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/django_drf_actions.go` | — |

### Validation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Middleware

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `middleware_coverage` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/engine/django_imports_rewrite.go` | — |

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
| `constant_propagation` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| `env_fallback_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| `import_resolution_quality` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/python.go`<br>`internal/substrate/substrate.go` | — |
| `request_shape_extraction` | ✅ `full` | `2026-05-27` | — | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| `response_shape_extraction` | ✅ `full` | `2026-05-27` | — | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |
| `schema_drift_detection` | ✅ `full` | `2026-05-27` | — | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_python.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python.framework.django ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
