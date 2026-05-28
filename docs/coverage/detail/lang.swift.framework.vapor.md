<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.swift.framework.vapor` — Vapor

Auto-generated. Back to [summary](../summary.md).

- **Language:** [swift](../by-language/swift.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 9

## Capabilities


### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `endpoint_synthesis` | ❌ `missing` | — | — | — | — | — |

### Auth

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

### Validation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

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
| `db_effect` | ⚠️ `partial` | — | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_swift.go` | — |
| `fs_effect` | ⚠️ `partial` | — | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_swift.go` | — |
| `http_effect` | ⚠️ `partial` | — | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_swift.go` | — |
| `mutation_effect` | ⚠️ `partial` | — | — | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_swift.go` | — |
| `sanitizer_recognition` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_swift.go` | — |
| `taint_sink_detection` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_swift.go` | — |
| `taint_source_detection` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_swift.go` | — |
| `vulnerability_finding` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_swift.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.swift.framework.vapor ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
