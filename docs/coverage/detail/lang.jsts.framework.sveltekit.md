<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.sveltekit` — SvelteKit

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 8

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `component_extraction` | ❌ `missing` | — | — | — | — |
| `hook_recognition` | — `not_applicable` | — | — | — | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `data_loaders` | ❌ `missing` | — | — | — | — |

### Server

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `hydration_boundaries` | ❌ `missing` | — | — | — | — |
| `server_components` | ❌ `missing` | — | — | — | — |

### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `route_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/sveltekit.yaml` |
| `router_pattern` | ❌ `missing` | — | — | — | — |

### Build

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `static_generation` | ❌ `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.sveltekit ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
