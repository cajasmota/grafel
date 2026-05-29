<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.gatsby` — Gatsby

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 35

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2857) | `internal/custom/javascript/gatsby.go`<br>`internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/react_shared.go` | — |
| Hook recognition | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2857) | `internal/custom/javascript/gatsby.go`<br>`internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/react_shared.go` | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Data loaders | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/gatsby.go`<br>`internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go` | — |

### Server

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Hydration boundaries | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/gatsby.go`<br>`internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go` | — |
| Server components | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/gatsby.go`<br>`internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/metafw_server.go` | — |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Route extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2857) | `internal/custom/javascript/gatsby.go`<br>`internal/custom/javascript/issue2857_meta_structure_test.go` | — |
| Router pattern | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2857) | `internal/custom/javascript/gatsby.go`<br>`internal/custom/javascript/issue2857_meta_structure_test.go` | — |

### Build

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Static generation | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/gatsby.go`<br>`internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2858_metafw_state_setter_test.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/jsts.go` | — |
| Constant propagation | ✅ `full` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_gatsby/api-handler.ts` | — |
| DB effect | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Dead code detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/patterns/dead_module_detector.go` | — |
| Def use chain extraction | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/substrate/def_use_jsts.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_gatsby/api-handler.ts` | — |
| Fs effect | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_gatsby/api-handler.ts` | — |
| Module cycle detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Pure function tagging | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Request shape extraction | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/substrate/payload_shapes_jsts.go` | — |
| Response shape extraction | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_gatsby/api-handler.ts` | — |
| Schema drift detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/payload_drift.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_gatsby/api-handler.ts` | — |
| Taint source detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_gatsby/api-handler.ts` | — |
| Template pattern catalog | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/substrate/template_pattern_jsts.go` | — |
| Vulnerability finding | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_gatsby/api-handler.ts` | — |

## Framework-specific

### Gatsby Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Gatsby graphql pagequery | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2878) | `internal/custom/javascript/gatsby.go`<br>`internal/custom/javascript/issue2878_metafw_idioms_test.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.gatsby ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
