<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.nestjs` — NestJS

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 5

## Capabilities


### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `endpoint_synthesis` | ✅ `full` | `2026-05-27` | — | — | `internal/engine/http_endpoint_synthesis.go` | — |
| `handler_attribution` | ✅ `full` | `2026-05-27` | — | — | `internal/engine/http_endpoint_synthesis.go` | — |

### Auth

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `auth_coverage` | ⚠️ `partial` | `2026-05-27` | — | [link](https://github.com/cajasmota/archigraph/issues/2940) | — | — |

### Validation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `request_validation` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2940) | — | — |

### Middleware

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

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
| `import_resolution_quality` | ✅ `full` | `2026-05-27` | — | — | `internal/engine/http_endpoint_synthesis.go` | — |

## Framework-specific

### NestJS Internals

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `dependency_injection` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2940) | — | — |
| `module_graph` | ⚠️ `partial` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2940) | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.nestjs ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
