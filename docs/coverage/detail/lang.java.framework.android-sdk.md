<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.android-sdk` — Android SDK (Activity/Fragment routing)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 34

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Context extraction | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/android.go` | extractAndroidContexts() detects getContext()/requireContext()/getApplicationContext()/getBaseContext()/requireActivity() call sites and Context parameter names as SCOPE.Reference context_site entities (#3256) |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Deep link extraction | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/android.go` | extractAndroidDeepLinks() detects <intent-filter> blocks in AndroidManifest.xml with <data android:scheme> as SCOPE.Reference deep_link entities with scheme/host/path URI templates (#3256) |
| Navigation extraction | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/android.go` | adIntentExplicitRE+adFragmentTransactionRE emit navigation edges (#3179) |
| Screen detection | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/android.go` | adActivityClassRE+adFragmentClassRE detect Activity/Fragment screens (#3179) |

### Platform

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Platform branching | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/android.go` | adSdkIntBranchRE detects Build.VERSION.SDK_INT API-level comparisons as platform-branch operations owned by the enclosing class (#3188) |

### Native Bridge

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/android.go` | adUsesPermissionRE+adUsesFeatureRE (manifest android.hardware.*) and adHardwareImportRE (import android.hardware.*) emit native-module references (#3188) |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🔴 `missing` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/android.go` | adSdkIntBranchRE detects Build.VERSION.SDK_INT comparisons as platform-branch control-flow sites; same extractor delivers Platform.platform_branching partial (#3188); the branch control-flow site entity mirrors the Data Flow.branch_conditions surface for Android |
| State management | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/android.go` | adViewModelClassRE+adViewModelProviderRE detect ViewModel/LiveData state (#3179) |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🟢 `partial` | `2026-05-30` | — | `internal/extractors/java/java.go` | Framework-blind Java extractor emits enum_declaration nodes for all Java frameworks including Android SDK/Jetpack; same as gwt/vaadin (partial) |
| Interface extraction | 🟢 `partial` | `2026-05-30` | — | `internal/extractors/java/java.go` | Framework-blind Java extractor emits interface_declaration nodes for all Java frameworks including Android SDK/Jetpack; same as gwt/vaadin (partial) |
| Type alias extraction | — `not_applicable` | — | — | — | Java has no type-alias syntax; all other Java frameworks are not_applicable for this cell |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | — `not_applicable` | — | — | — | Android SDK uses LiveData/observer patterns rather than React-style useState setters; state_setter_emission is a React/JSX-paradigm capability that does not apply |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/junit5.go` | android_sdk added to junit5Frameworks map (#3177) |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3093) | `internal/links/constant_propagation.go`<br>`internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/java.go`<br>`internal/substrate/taint_sites_java.go` | Framework-blind substrate: constant_propagation, effect_propagation, and taint_flow passes emit per-binding/per-finding Confidence values on Java entities via java.go sniffers. EntityRecord.Confidence not yet stamped by the Java extractor directly; MCP min_confidence filtering applies. Partial pending a dedicated confidence-scoring pass writing top-level EntityRecord.Confidence. |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Dead code detection | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Def use chain extraction | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| HTTP effect | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Mutation effect | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Pure function tagging | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Reachability analysis | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Request shape extraction | — `not_applicable` | — | 3154 | — | — |
| Response shape extraction | — `not_applicable` | — | 3154 | — | — |
| Sanitizer recognition | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Schema drift detection | — `not_applicable` | — | — | — | Android SDK is a mobile client framework with no server-side HTTP handlers; schema drift detection requires a producer-consumer HTTP endpoint pair; request_shape_extraction and response_shape_extraction are also not_applicable for this record |
| Taint sink detection | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Taint source detection | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Template pattern catalog | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Vulnerability finding | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.android-sdk ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
