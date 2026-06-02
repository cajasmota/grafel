<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.android-sdk` — Android SDK (Activity/Fragment routing)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 36

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Context extraction | ✅ `full` | `2026-06-01` | — | `internal/custom/java/android.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/custom_java_patterns_androidcells_test.go` | Re-verified live via custom_java_patterns dispatch (#3590/#3575): extractAndroidContexts() emits getContext()/requireContext()/getActivity() call sites as SCOPE.Reference context_site through RunCustomExtractors; value-asserting test TestJavaPatternsAndroidContextExtractionLive asserts ProfileFragment.requireContext context_method/context_kind/provenance live |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Deep link extraction | 🔴 `missing` | `2026-06-01` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/android.go`<br>`internal/extractors/custom_java_patterns_androidcells_test.go` | LEFT missing after live re-verification (#3590/#3575): deep links are emitted ONLY from AndroidManifest.xml (<intent-filter> <data android:scheme>), which is NOT live-reachable through the custom_java_patterns dispatcher — the manifest carries no framework marker so ExtractAndroid never runs on it (proven negatively by TestJavaPatternsAndroidManifestNotLiveReachable). extractAndroidDeepLinks() works when called directly; the gap is the dispatcher's source-marker gating. Issue tracks manifest wiring. |
| Navigation extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/java/android.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | Re-wired live via custom_java_patterns dispatch (#3586): adIntentExplicitRE+adFragmentTransactionRE emit navigation edges through RunCustomExtractors; value-asserting smoke test TestJavaPatternsAndroidActivityLive asserts the MainActivity->DetailActivity explicit-Intent navigation operation emits live |
| Screen detection | ✅ `full` | `2026-06-02` | — | `internal/custom/java/android.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | Re-wired live via custom_java_patterns dispatch (#3586): adActivityClassRE+adFragmentClassRE detect Activity/Fragment screens through RunCustomExtractors; value-asserting smoke test TestJavaPatternsAndroidActivityLive asserts the MainActivity SCOPE.UIComponent activity entity emits live |

### Platform

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Platform branching | ✅ `full` | `2026-06-01` | — | `internal/custom/java/android.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/custom_java_patterns_androidcells_test.go` | Re-verified live via custom_java_patterns dispatch (#3590/#3575): adSdkIntBranchRE emits Build.VERSION.SDK_INT comparisons as SCOPE.Operation branch ops through RunCustomExtractors; TestJavaPatternsAndroidPlatformBranchingLive asserts 'Build.VERSION.SDK_INT >= Build.VERSION_CODES.O' with branch_kind/operator/api_level/enclosing_class live |

### Native Bridge

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | 🟢 `partial` | `2026-06-01` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/android.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/custom_java_patterns_androidcells_test.go` | Re-verified PARTIAL live via custom_java_patterns dispatch (#3590/#3575): the Java-source half (import android.hardware.*) IS live-reachable — TestJavaPatternsAndroidNativeModuleJavaImportLive asserts android.hardware.camera2.CameraManager declaration_kind=import live. The MANIFEST half (<uses-permission>/<uses-feature> android.hardware.*) is NOT live-reachable: AndroidManifest.xml carries no custom_java_patterns framework marker so the dispatcher never runs ExtractAndroid on it (proven negatively by TestJavaPatternsAndroidManifestNotLiveReachable). Issue tracks wiring the manifest path. |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | ✅ `full` | `2026-06-01` | — | `internal/custom/java/android.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/custom_java_patterns_androidcells_test.go` | Re-verified live via custom_java_patterns dispatch (#3590/#3575): the same adSdkIntBranchRE platform-branch SCOPE.Operation entity that backs Platform.platform_branching is the Data Flow.branch_conditions control-flow site; proven live by TestJavaPatternsAndroidPlatformBranchingLive |
| State management | ✅ `full` | `2026-06-01` | — | `internal/custom/java/android.go`<br>`internal/custom/java/patterns_dispatch.go`<br>`internal/extractors/custom_java_patterns_androidcells_test.go` | Re-verified live via custom_java_patterns dispatch (#3590/#3575): adViewModelClassRE emits ViewModel subclasses as SCOPE.Component viewmodel through RunCustomExtractors; TestJavaPatternsAndroidStateManagementLive asserts UserViewModel component_kind=viewmodel + provenance live |

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
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/java/config_consumer.go`<br>`internal/extractors/java/config_consumer_test.go` | @Value, @ConfigurationProperties, env.getProperty, @ConfigProperty -> config:<key> (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Dead code detection | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Def use chain extraction | 🟢 `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
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
