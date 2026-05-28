<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.svelte` — Svelte

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 22

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `component_extraction` | ❌ `missing` | — | — | — | — | — |
| `context_extraction` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | — | — |
| `hoc_wrapper_recognition` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | — | — |
| `hook_recognition` | — `not_applicable` | — | — | — | — | — |
| `jsx_template` | — `not_applicable` | — | — | — | — | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `branch_conditions` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | — | — |
| `data_fetching` | ❌ `missing` | — | — | — | — | — |
| `prop_extraction` | ❌ `missing` | — | — | — | — | — |
| `state_management` | ❌ `missing` | — | — | — | — | — |

### Navigation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `router_pattern` | ❌ `missing` | — | — | — | — | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `enum_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `interface_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `type_alias_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `state_setter_emission` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | — | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `tests_linkage` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `constant_propagation` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `def_use_chain_extraction` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_markup_script.go` | — |
| `env_fallback_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `import_resolution_quality` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `module_cycle_detection` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/module_cycle_pass.go` | — |
| `pure_function_tagging` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| `template_pattern_catalog` | ⚠️ `partial` | `2026-05-28` | — | — | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_markup_script.go` | — |

## Framework-specific

### Svelte Reactivity

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `reactive_statement_detection` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2739) | — | — |
| `store_pattern_detection` | ❌ `missing` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2739) | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.svelte ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
