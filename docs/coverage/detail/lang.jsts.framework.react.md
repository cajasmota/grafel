<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.react` — React

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 32

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `component_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2735) | `internal/extractors/javascript/react.go` | — |
| `context_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `hoc_wrapper_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `hook_recognition` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2735) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2854_react_test.go`<br>`internal/extractors/javascript/react.go` | — |
| `jsx_template` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `branch_conditions` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/discriminator.go` | — |
| `data_fetching` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/destructure_bindings.go`<br>`internal/extractors/javascript/extractor.go` | — |
| `prop_extraction` | ⚠️ `partial` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2665) | `internal/extractors/javascript/navigation.go` | Covers only JSX navigation props (Link to/href, Navigate to, state object); generic React component prop extraction is not yet implemented. |
| `state_management` | ⚠️ `partial` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2632) | `internal/extractors/javascript/destructure_bindings.go`<br>`internal/extractors/javascript/zustand_store.go` | — |

### Navigation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `router_pattern` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/navigation.go` | — |

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
| `db_effect` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/react_substrate_test.go` | — |
| `dead_code_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| `def_use_chain_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/react_substrate_test.go` | — |
| `env_fallback_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `fs_effect` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/react_substrate_test.go` | — |
| `http_effect` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/react_substrate_test.go` | — |
| `import_resolution_quality` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/react_substrate_test.go`<br>`internal/substrate/substrate.go` | — |
| `module_cycle_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/extractors/javascript/testdata/substrate_react/cyclic_dep.tsx`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/react_substrate_test.go` | — |
| `mutation_effect` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/react_substrate_test.go` | — |
| `pure_function_tagging` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go`<br>`internal/substrate/react_substrate_test.go` | — |
| `reachability_analysis` | ✅ `full` | `2026-05-28` | — | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| `sanitizer_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/taint_flow.go`<br>`internal/substrate/react_substrate_test.go`<br>`internal/substrate/taint_sites_jsts.go` | — |
| `taint_sink_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/taint_flow.go`<br>`internal/substrate/react_substrate_test.go`<br>`internal/substrate/taint_sites_jsts.go` | — |
| `taint_source_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/taint_flow.go`<br>`internal/substrate/react_substrate_test.go`<br>`internal/substrate/taint_sites_jsts.go` | — |
| `template_pattern_catalog` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/template_pattern_pass.go`<br>`internal/substrate/react_substrate_test.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_jsts.go` | — |
| `vulnerability_finding` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/taint_flow.go`<br>`internal/substrate/react_substrate_test.go`<br>`internal/substrate/taint_sites_jsts.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.react ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
