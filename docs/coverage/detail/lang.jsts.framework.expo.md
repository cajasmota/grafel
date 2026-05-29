<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.expo` — Expo

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 38

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Context extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Deep link extraction | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_expo/linking.ts`<br>`testdata/fixtures/real-world/typescript/react_native_navigator.tsx` | — |
| Navigation extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/frameworks/expo.yaml` | — |
| Screen detection | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/frameworks/expo.yaml` | — |

### Platform

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Platform branching | ✅ `full` | `2026-05-28` | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/platform_variants.go`<br>`internal/extractors/javascript/testdata/mobile_expo/StatusBar.ios.tsx` | — |

### Native Bridge

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_expo/linking.ts`<br>`internal/extractors/javascript/testdata/mobile_react_native/AppNavigator.tsx`<br>`testdata/fixtures/real-world/typescript/react_native_navigator.tsx` | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/discriminator.go` | — |
| State management | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2859) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/testdata/mobile_expo/ProfileScreen.tsx`<br>`internal/extractors/javascript/zustand_store.go` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ⚠️ `partial` | — | 3059 | `internal/links/effect_propagation.go`<br>`internal/substrate/jsts.go` | — |
| Constant propagation | ⚠️ `partial` | `2026-05-28` | 3059 | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go` | — |
| DB effect | — `not_applicable` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | Mobile apps (RN/Expo/Ionic/NativeScript) call remote HTTP APIs, not Node.js ORM primitives directly; db_effect N/A at the mobile client layer |
| Dead code detection | ⚠️ `partial` | — | 3059 | `internal/patterns/dead_module_detector.go` | — |
| Def use chain extraction | ⚠️ `partial` | — | 3059 | `internal/substrate/def_use_jsts.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ⚠️ `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | — |
| HTTP effect | ⚠️ `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_mobile/App.tsx` | — |
| Module cycle detection | ⚠️ `partial` | — | 3059 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | ⚠️ `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | — |
| Pure function tagging | ⚠️ `partial` | — | 3059 | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | ⚠️ `partial` | — | 3059 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Request shape extraction | ⚠️ `partial` | — | 3059 | `internal/substrate/payload_shapes_jsts.go` | — |
| Response shape extraction | ⚠️ `partial` | — | 3059 | `internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | ⚠️ `partial` | — | 3059 | `internal/substrate/taint_sites_jsts.go` | — |
| Schema drift detection | ⚠️ `partial` | — | 3059 | `internal/links/payload_drift.go` | — |
| Taint sink detection | ⚠️ `partial` | — | 3059 | `internal/substrate/taint_sites_jsts.go` | — |
| Taint source detection | ⚠️ `partial` | — | 3059 | `internal/substrate/taint_sites_jsts.go` | — |
| Template pattern catalog | ⚠️ `partial` | — | 3059 | `internal/substrate/template_pattern_jsts.go` | — |
| Vulnerability finding | ⚠️ `partial` | — | 3059 | `internal/links/taint_flow.go` | — |

## Framework-specific

### Expo Ecosystem

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Eas build detection | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2879) | `internal/extractors/config/discover.go`<br>`internal/extractors/config/testdata/mobile/expo_config/eas.json` | — |
| Expo config extraction | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2879) | `internal/extractors/config/discover.go`<br>`internal/extractors/config/testdata/mobile/expo_config/app.json` | — |
| Expo router specifics | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/navigation.go` | — |

### Expo Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| HOC wrapper recognition | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.expo ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
