<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.gwt` — Google Web Toolkit (GWT)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 33

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ⚠️ `partial` | — | 3091 | `internal/custom/java/vaadin_gwt.go` | — |
| Context extraction | — `not_applicable` | — | 3091 | — | GWT compiles Java to client-side JS but uses Java idioms with no React-style concepts; context_extraction is a React/JSX-paradigm capability that does not apply |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | ❌ `missing` | — | — | — | — |
| Data fetching | ❌ `missing` | — | — | — | — |
| Prop extraction | — `not_applicable` | — | 3091 | — | GWT compiles Java to client-side JS but uses Java idioms with no React-style concepts; prop_extraction is a React/JSX-paradigm capability that does not apply |
| State management | — `not_applicable` | — | 3091 | — | GWT compiles Java to client-side JS but uses Java idioms with no React-style concepts; state_management is a React/JSX-paradigm capability that does not apply |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | ⚠️ `partial` | — | 3091 | `internal/custom/java/vaadin_gwt.go` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ❌ `missing` | — | — | — | — |
| Interface extraction | ❌ `missing` | — | — | — | — |
| Type alias extraction | — `not_applicable` | — | 3091 | — | GWT compiles Java to client-side JS but uses Java idioms with no React-style concepts; type_alias_extraction is a React/JSX-paradigm capability that does not apply |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | — `not_applicable` | — | 3091 | — | GWT compiles Java to client-side JS but uses Java idioms with no React-style concepts; state_setter_emission is a React/JSX-paradigm capability that does not apply |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ❌ `missing` | — | — | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3093) | `internal/links/constant_propagation.go`<br>`internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/java.go`<br>`internal/substrate/taint_sites_java.go` | Framework-blind substrate: constant_propagation, effect_propagation, and taint_flow passes emit per-binding/per-finding Confidence values on Java entities via java.go sniffers. EntityRecord.Confidence not yet stamped by the Java extractor directly; MCP min_confidence filtering applies. Partial pending a dedicated confidence-scoring pass writing top-level EntityRecord.Confidence. |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Dead code detection | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Def use chain extraction | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| HTTP effect | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Mutation effect | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Pure function tagging | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Reachability analysis | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Request shape extraction | — `not_applicable` | — | 3154 | — | — |
| Response shape extraction | — `not_applicable` | — | 3154 | — | — |
| Sanitizer recognition | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Schema drift detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Taint source detection | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Template pattern catalog | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Vulnerability finding | ⚠️ `partial` | — | 3154 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/entry_points_java.go`<br>`internal/substrate/taint_sites_java.go`<br>`internal/substrate/template_pattern_java.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.gwt ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
