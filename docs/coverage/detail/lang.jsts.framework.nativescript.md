<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.nativescript` — NativeScript

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 35

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Context extraction | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/testdata/mobile_nativescript/AppShell.tsx` | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Deep link extraction | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_nativescript/nav-service.ts` | — |
| Navigation extraction | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_nativescript/nav-service.ts` | — |
| Screen detection | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_nativescript/nav-service.ts` | — |

### Platform

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Platform branching | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_nativescript/nav-service.ts` | — |

### Native Bridge

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_nativescript/nav-service.ts` | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2885) | `internal/extractors/javascript/branchconditions.go`<br>`internal/extractors/javascript/issue2885_branch_conditions_test.go`<br>`internal/extractors/javascript/testdata/mobile_nativescript/counter-view-model.ts` | FIXED(#2885): general branch-condition pass (branchconditions.go) now emits branch_conditions + BRANCHES_ON for if/ternary/switch member comparisons the discriminator pass misses, e.g. this._counter !== value / this._counter le 0. Proven by counter-view-model.ts modelled on a real extends-Observable NS view-model. |
| State management | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/testdata/mobile_nativescript/main-view-model.ts` | AUDIT(#2847) HOLDS: state_setter fires on 6 real NativeScript-Core view-models (notifyPropertyChange/this.set/set-accessor) from @nativescript app-templates. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/testdata/mobile_nativescript/main-view-model.ts` | — |

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

### NativeScript Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| HOC wrapper recognition | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/testdata/mobile_nativescript/AppShell.tsx` | Genuine HOC signal only for the react-nativescript flavor (memo/withOrientation, recognised by the framework-agnostic React HOC detector in extractor.go). Core/Angular/Vue NativeScript flavors have no HOC equivalent; re-homed out of the shared mobile Structure column so it no longer reads as a paradigm-wide claim. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.nativescript ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
