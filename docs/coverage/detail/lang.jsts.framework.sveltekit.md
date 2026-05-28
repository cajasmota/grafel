<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.sveltekit` — SvelteKit

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 17

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `component_extraction` | ✅ `full` | — | — | [link](2880) | `internal/custom/javascript/issue2880_metafw_routing_test.go`<br>`internal/custom/javascript/svelte.go` | — |
| `hook_recognition` | — `not_applicable` | — | — | — | — | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `data_loaders` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/svelte.go` | — |

### Server

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `hydration_boundaries` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/svelte.go` | — |
| `server_components` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/metafw_server.go`<br>`internal/custom/javascript/svelte.go` | — |

### Routing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `route_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/sveltekit.yaml` | — |
| `router_pattern` | ✅ `full` | — | — | [link](2880) | `internal/custom/javascript/issue2880_metafw_routing_test.go`<br>`internal/custom/javascript/svelte.go` | — |

### Build

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `static_generation` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/svelte.go` | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `enum_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `interface_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `type_alias_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `state_setter_emission` | ✅ `full` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/svelte.go`<br>`internal/extractors/svelte/extractor.go` | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `tests_linkage` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `sanitizer_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_sveltekit/+page.server.ts` | — |
| `taint_sink_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_sveltekit/+page.server.ts` | — |
| `taint_source_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_sveltekit/+page.server.ts` | — |
| `vulnerability_finding` | ✅ `full` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_sveltekit/+page.server.ts` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.sveltekit ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
