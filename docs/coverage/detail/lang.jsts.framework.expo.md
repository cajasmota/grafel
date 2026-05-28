<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.expo` — Expo

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 16

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `context_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Navigation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `deep_link_extraction` | ✅ `full` | — | — | [link](2860) | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_expo/linking.ts`<br>`testdata/fixtures/real-world/typescript/react_native_navigator.tsx` | — |
| `navigation_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/expo.yaml` | — |
| `screen_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/expo.yaml` | — |

### Platform

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `platform_branching` | ✅ `full` | `2026-05-28` | — | [link](2860) | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/platform_variants.go`<br>`internal/extractors/javascript/testdata/mobile_expo/StatusBar.ios.tsx` | — |

### Native Bridge

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `native_module_imports` | ✅ `full` | — | — | [link](2860) | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_expo/linking.ts`<br>`internal/extractors/javascript/testdata/mobile_react_native/AppNavigator.tsx`<br>`testdata/fixtures/real-world/typescript/react_native_navigator.tsx` | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `branch_conditions` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/discriminator.go` | — |
| `state_management` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/testdata/mobile_expo/ProfileScreen.tsx`<br>`internal/extractors/javascript/zustand_store.go` | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `enum_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `interface_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `type_alias_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `state_setter_emission` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `tests_linkage` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `constant_propagation` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `env_fallback_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `import_resolution_quality` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_mobile/App.tsx` | — |

## Framework-specific

### Expo Ecosystem

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `eas_build_detection` | ✅ `full` | `2026-05-29` | — | [link](https://github.com/cajasmota/archigraph/issues/2879) | `internal/extractors/config/discover.go`<br>`internal/extractors/config/testdata/mobile/expo_config/eas.json` | — |
| `expo_config_extraction` | ✅ `full` | `2026-05-29` | — | [link](https://github.com/cajasmota/archigraph/issues/2879) | `internal/extractors/config/discover.go`<br>`internal/extractors/config/testdata/mobile/expo_config/app.json` | — |
| `expo_router_specifics` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/navigation.go` | — |

### Expo Internals

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `hoc_wrapper_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.expo ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
