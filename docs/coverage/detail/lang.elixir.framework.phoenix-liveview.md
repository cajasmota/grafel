<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.elixir.framework.phoenix-liveview` — Phoenix LiveView

Auto-generated. Back to [summary](../summary.md).

- **Language:** [elixir](../by-language/elixir.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 19

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `component_extraction` | ❌ `missing` | — | — | — | — | — |
| `hook_recognition` | ❌ `missing` | — | — | — | — | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `data_loaders` | ❌ `missing` | — | — | — | — | — |

### Server

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `hydration_boundaries` | ❌ `missing` | — | — | — | — | — |
| `server_components` | ❌ `missing` | — | — | — | — | — |

### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `route_extraction` | ❌ `missing` | — | — | — | — | — |
| `router_pattern` | ❌ `missing` | — | — | — | — | — |

### Build

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `static_generation` | ❌ `missing` | — | — | — | — | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `enum_extraction` | ❌ `missing` | — | — | — | — | — |
| `interface_extraction` | ❌ `missing` | — | — | — | — | — |
| `type_alias_extraction` | ❌ `missing` | — | — | — | — | — |

### Lifecycle

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `state_setter_emission` | ❌ `missing` | — | — | — | — | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `tests_linkage` | ❌ `missing` | — | — | — | — | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `confidence_overlay` | ✅ `full` | `2026-05-28` | — | — | `internal/types/confidence.go`<br>`internal/graph/graph.go`<br>`internal/mcp/tools.go` | — |
| `constant_propagation` | ✅ `full` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| `dead_code_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/substrate/entry_points_elixir.go`<br>`internal/substrate/entry_points.go`<br>`internal/links/reachability.go`<br>`internal/mcp/dead_code.go` | — |
| `env_fallback_recognition` | ✅ `full` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| `import_resolution_quality` | ⚠️ `partial` | `2026-05-27` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/elixir.go`<br>`internal/substrate/substrate.go` | — |
| `reachability_analysis` | ✅ `full` | `2026-05-28` | — | — | `internal/substrate/entry_points_elixir.go`<br>`internal/substrate/entry_points.go`<br>`internal/links/reachability.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.elixir.framework.phoenix-liveview ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
