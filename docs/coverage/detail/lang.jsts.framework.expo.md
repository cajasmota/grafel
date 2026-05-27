<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.expo` — Expo

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 6

## Capabilities


### Navigation

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `deep_link_extraction` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2735) | — |
| `navigation_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/expo.yaml` |
| `screen_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/expo.yaml` |

### Platform

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `platform_branching` | ⚠️ `partial` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2666) | `internal/engine/rules/javascript_typescript/frameworks/expo.yaml` |

### Native Bridge

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `native_module_imports` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2735) | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `state_management` | ⚠️ `partial` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2632) | `internal/engine/rules/javascript_typescript/frameworks/expo.yaml` |

### Lifecycle

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|

## Framework-specific

### Expo Ecosystem

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `eas_build_detection` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2739) | — |
| `expo_config_extraction` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2739) | — |
| `expo_router_specifics` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/navigation.go` |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.expo ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
