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
| Context extraction | 🔴 `missing` | — | — | — | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Deep link extraction | 🔴 `missing` | — | — | — | — |
| Navigation extraction | 🟢 `partial` | — | — | `internal/custom/java/android.go` | adIntentExplicitRE+adFragmentTransactionRE emit navigation edges (#3179) |
| Screen detection | 🟢 `partial` | — | — | `internal/custom/java/android.go` | adActivityClassRE+adFragmentClassRE detect Activity/Fragment screens (#3179) |

### Platform

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Platform branching | 🟢 `partial` | — | — | `internal/custom/java/android.go` | adSdkIntBranchRE detects Build.VERSION.SDK_INT API-level comparisons as platform-branch operations owned by the enclosing class (#3188) |

### Native Bridge

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | 🟢 `partial` | — | — | `internal/custom/java/android.go` | adUsesPermissionRE+adUsesFeatureRE (manifest android.hardware.*) and adHardwareImportRE (import android.hardware.*) emit native-module references (#3188) |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🔴 `missing` | — | — | — | — |
| State management | 🟢 `partial` | — | — | `internal/custom/java/android.go` | adViewModelClassRE+adViewModelProviderRE detect ViewModel/LiveData state (#3179) |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | — | — | — |
| Interface extraction | 🔴 `missing` | — | — | — | — |
| Type alias extraction | 🔴 `missing` | — | — | — | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🔴 `missing` | — | — | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | — | — | `internal/custom/java/junit5.go` | android_sdk added to junit5Frameworks map (#3177) |

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
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
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
