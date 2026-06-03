<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.nativescript` — NativeScript

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 38

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
| Confidence overlay | 🟢 `partial` | — | 3059 | `internal/links/effect_propagation.go`<br>`internal/substrate/jsts.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/javascript/config_consumer.go`<br>`internal/extractors/javascript/config_consumer_test.go` | process.env.X, import.meta.env.X, config.get(k) -> config:<key> DEPENDS_ON_CONFIG (issue #3641) |
| Constant propagation | 🟢 `partial` | `2026-05-28` | 3059 | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go` | — |
| DB effect | — `not_applicable` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | Mobile apps (RN/Expo/Ionic/NativeScript) call remote HTTP APIs, not Node.js ORM primitives directly; db_effect N/A at the mobile client layer |
| Dead code detection | 🟢 `partial` | — | 3059 | `internal/patterns/dead_module_detector.go` | — |
| Def use chain extraction | 🟢 `partial` | — | 3059 | `internal/substrate/def_use_jsts.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/javascript/exception_flow.go`<br>`internal/extractors/javascript/exception_flow_test.go` | throw new X -> THROWS; e instanceof X catch-filter -> CATCHES; untyped throw/catch dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic JS/TS engine pass, fires regardless of framework). Verified to attribute to the enclosing function: LaunchDarkly ldClient.variation/boolVariation/stringVariation, Unleash unleash.isEnabled, OpenFeature client.getBooleanValue, Unleash-React useFlag, Split.io getTreatment, Flagsmith hasFeature, plus GrowthBook gb.isOn/isOff/getFeatureValue and ConfigCat configCatClient.getValue/getValueAsync (receiver-gated). Honest-partial: dynamic keys + non-flag receivers (button.isOn, formData.getValue) emit nothing. |
| Fs effect | 🟢 `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | — |
| HTTP effect | 🟢 `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_mobile/App.tsx` | — |
| Module cycle detection | 🟢 `partial` | — | 3059 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | — |
| Pure function tagging | 🟢 `partial` | — | 3059 | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | — | 3059 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Request shape extraction | 🟢 `partial` | — | 3059 | `internal/substrate/payload_shapes_jsts.go` | — |
| Response shape extraction | 🟢 `partial` | — | 3059 | `internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | 🟢 `partial` | — | 3059 | `internal/substrate/taint_sites_jsts.go` | — |
| Schema drift detection | 🟢 `partial` | — | 3059 | `internal/links/payload_drift.go` | — |
| Taint sink detection | 🟢 `partial` | — | 3059 | `internal/substrate/taint_sites_jsts.go` | — |
| Taint source detection | 🟢 `partial` | — | 3059 | `internal/substrate/taint_sites_jsts.go` | — |
| Template pattern catalog | 🟢 `partial` | — | 3059 | `internal/substrate/template_pattern_jsts.go` | — |
| Vulnerability finding | 🟢 `partial` | — | 3059 | `internal/links/taint_flow.go` | — |

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
