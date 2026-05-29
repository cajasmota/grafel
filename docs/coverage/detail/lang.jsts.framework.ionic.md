<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.ionic` — Ionic

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 35

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Context extraction | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/javascript/extractor.go` | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Deep link extraction | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/deepLinks.ts` | — |
| Navigation extraction | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/AppRouter.tsx` | — |
| Screen detection | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/AppRouter.tsx` | — |

### Platform

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Platform branching | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/AppRouter.tsx` | — |

### Native Bridge

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/AppRouter.tsx`<br>`internal/extractors/javascript/testdata/mobile_ionic/deepLinks.ts` | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/discriminator.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/SessionContext.tsx` | — |
| State management | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/SessionContext.tsx`<br>`internal/extractors/javascript/zustand_store.go` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/SessionContext.tsx` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_mobile/App.tsx` | — |
| Module cycle detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Response shape extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

## Framework-specific

### Ionic Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| HOC wrapper recognition | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/testdata/mobile_ionic/SessionContext.tsx` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.ionic ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
