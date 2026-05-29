<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.sveltekit` — SvelteKit

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 36

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ✅ `full` | — | 2880 | `internal/custom/javascript/issue2880_metafw_routing_test.go`<br>`internal/custom/javascript/svelte.go` | — |
| Hook recognition | — `not_applicable` | — | — | — | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Data loaders | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/svelte.go` | — |

### Server

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Hydration boundaries | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/svelte.go` | — |
| Server components | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/metafw_server.go`<br>`internal/custom/javascript/svelte.go` | — |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Route extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/frameworks/sveltekit.yaml` | — |
| Router pattern | ✅ `full` | — | 2880 | `internal/custom/javascript/issue2880_metafw_routing_test.go`<br>`internal/custom/javascript/svelte.go` | — |

### Build

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Static generation | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/svelte.go` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/svelte.go`<br>`internal/extractors/svelte/extractor.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-29` | 3055 | `internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/jsts.go` | — |
| Constant propagation | ✅ `full` | `2026-05-29` | 3055 | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ✅ `full` | `2026-05-29` | 3055 | `internal/substrate/backend_db_effect_test.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3055 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 3055 | `internal/substrate/def_use_jsts.go`<br>`internal/substrate/def_use_test.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-29` | 3055 | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3055 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3055 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-29` | 3055 | `internal/extractors/javascript/testdata/substrate_import_resolution/app.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/config.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/nest_app.ts`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3055 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3055 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3055 | `internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3055 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-29` | 3055 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-29` | 3055 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_sveltekit/+page.server.ts` | — |
| Schema drift detection | ✅ `full` | `2026-05-29` | 3055 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Taint sink detection | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_sveltekit/+page.server.ts` | — |
| Taint source detection | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_sveltekit/+page.server.ts` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3055 | `internal/substrate/template_pattern_jsts.go`<br>`internal/substrate/template_pattern_test.go` | — |
| Vulnerability finding | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_sveltekit/+page.server.ts` | — |

## Framework-specific

### SvelteKit Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Sveltekit form actions | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2878) | `internal/custom/javascript/issue2878_metafw_idioms_test.go`<br>`internal/custom/javascript/svelte.go` | — |
| Sveltekit load function | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2878) | `internal/custom/javascript/issue2878_metafw_idioms_test.go`<br>`internal/custom/javascript/svelte.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.sveltekit ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
