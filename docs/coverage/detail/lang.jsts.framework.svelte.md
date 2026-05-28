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
| `component_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2854_test.go` | — |
| `context_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2854_test.go` | — |
| `hoc_wrapper_recognition` | — `not_applicable` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2854) | — | — |
| `hook_recognition` | — `not_applicable` | — | — | — | — | — |
| `jsx_template` | — `not_applicable` | — | — | — | — | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `branch_conditions` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/svelte/UserList.svelte` | — |
| `data_fetching` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/svelte/UserList.svelte` | — |
| `prop_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/svelte/UserList.svelte` | — |
| `state_management` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/svelte/UserList.svelte` | — |

### Navigation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `router_pattern` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2856_test.go`<br>`testdata/fixtures/real-world/svelte/UserList.svelte` | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `enum_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `interface_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `type_alias_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `state_setter_emission` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2856_test.go`<br>`testdata/fixtures/real-world/svelte/UserList.svelte` | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `tests_linkage` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `constant_propagation` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `env_fallback_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `import_resolution_quality` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/markup_script.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_svelte/UserCard.svelte` | — |
| `sanitizer_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_svelte/UserCard.svelte` | — |
| `taint_sink_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_svelte/UserCard.svelte` | — |
| `taint_source_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_svelte/UserCard.svelte` | — |
| `vulnerability_finding` | ✅ `full` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_svelte/UserCard.svelte` | — |

## Framework-specific

### Svelte Internals

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `actions` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go`<br>`internal/extractors/javascript/testdata/svelte_internals/Comp.svelte` | — |
| `context` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go`<br>`internal/extractors/javascript/testdata/svelte_internals/Comp.svelte` | — |
| `props_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go`<br>`internal/extractors/javascript/testdata/svelte_internals/Comp.svelte` | — |
| `reactive_statements` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go`<br>`internal/extractors/javascript/testdata/svelte_internals/Comp.svelte` | — |
| `runes` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go`<br>`internal/extractors/javascript/testdata/svelte_internals/Comp.svelte` | — |
| `sfc_block_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go`<br>`internal/extractors/javascript/testdata/svelte_internals/Comp.svelte` | — |
| `stores` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go`<br>`internal/extractors/javascript/testdata/svelte_internals/Comp.svelte` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.svelte ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
