<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.next-api` — Next.js API Routes / App Router

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 13

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `component_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2857) | `internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/nextjs.go`<br>`internal/custom/javascript/react_shared.go` | — |
| `hook_recognition` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2857) | `internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/nextjs.go`<br>`internal/custom/javascript/react_shared.go` | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `data_loaders` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2735) | — | — |

### Server

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `hydration_boundaries` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2735) | — | — |
| `server_components` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2735) | — | — |

### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `route_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/next_js.yaml` | — |
| `router_pattern` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/next_js.yaml` | — |

### Build

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `static_generation` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2735) | — | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `enum_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `interface_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `type_alias_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `state_setter_emission` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | — | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `tests_linkage` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

## Framework-specific

### Next.js Internals

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `middleware_runtime_detection` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2739) | — | — |
| `next_config_detection` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2739) | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.next-api ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
