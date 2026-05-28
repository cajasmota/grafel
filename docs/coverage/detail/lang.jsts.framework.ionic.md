<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.ionic` — Ionic

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 17

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `context_extraction` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/javascript/extractor.go` | — |
| `hoc_wrapper_recognition` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/SessionContext.tsx` | — |

### Navigation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `deep_link_extraction` | ✅ `full` | — | — | [link](2860) | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/deepLinks.ts` | — |
| `navigation_extraction` | ✅ `full` | — | — | [link](2860) | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/AppRouter.tsx` | — |
| `screen_detection` | ✅ `full` | — | — | [link](2860) | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/AppRouter.tsx` | — |

### Platform

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `platform_branching` | ✅ `full` | — | — | [link](2860) | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/AppRouter.tsx` | — |

### Native Bridge

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `native_module_imports` | ✅ `full` | — | — | [link](2860) | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/AppRouter.tsx`<br>`internal/extractors/javascript/testdata/mobile_ionic/deepLinks.ts` | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `branch_conditions` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/discriminator.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/SessionContext.tsx` | — |
| `state_management` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/SessionContext.tsx`<br>`internal/extractors/javascript/zustand_store.go` | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `enum_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `interface_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `type_alias_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `state_setter_emission` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/SessionContext.tsx` | — |

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

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.ionic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
