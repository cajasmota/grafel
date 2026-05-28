<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.remix` — Remix

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 13

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `component_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2857) | `internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/react_shared.go`<br>`internal/custom/javascript/remix.go` | — |
| `hook_recognition` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2857) | `internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/react_shared.go`<br>`internal/custom/javascript/remix.go` | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `data_loaders` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/remix.go` | — |

### Server

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `hydration_boundaries` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/metafw_server.go`<br>`internal/custom/javascript/remix.go` | — |
| `server_components` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/metafw_server.go`<br>`internal/custom/javascript/remix.go` | — |

### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `route_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/remix.yaml` | — |
| `router_pattern` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2857) | `internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/remix.go` | — |

### Build

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `static_generation` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/remix.go` | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `enum_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `interface_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `type_alias_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `state_setter_emission` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2858_metafw_state_setter_test.go` | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `tests_linkage` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.remix ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
