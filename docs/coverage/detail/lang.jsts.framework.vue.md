<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.vue` — Vue

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 20

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `component_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2854_test.go` | — |
| `context_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2854_test.go` | — |
| `hook_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2854_test.go` | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `branch_conditions` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/vue/UserCard.vue` | — |
| `data_fetching` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/vue/UserCard.vue` | — |
| `prop_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/vue/UserCard.vue` | — |
| `state_management` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/vue/UserCard.vue` | — |

### Navigation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `router_pattern` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2856_test.go`<br>`testdata/fixtures/real-world/vue/UserCard.vue` | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `enum_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `interface_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `type_alias_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `state_setter_emission` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2856_test.go`<br>`testdata/fixtures/real-world/vue/UserCard.vue` | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `tests_linkage` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `constant_propagation` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `env_fallback_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `import_resolution_quality` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/markup_script.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_vue/UserCard.vue` | — |
| `sanitizer_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_vue/UserCard.vue` | — |
| `taint_sink_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_vue/UserCard.vue` | — |
| `taint_source_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_vue/UserCard.vue` | — |
| `vulnerability_finding` | ✅ `full` | `2026-05-28` | — | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_vue/UserCard.vue` | — |

## Framework-specific

### Vue Internals

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `composition_api` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/Comp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| `directive_recognition` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/Comp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| `options_api` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/OptionsComp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| `pinia_store` | ✅ `full` | `2026-05-29` | — | [link](https://github.com/cajasmota/archigraph/issues/2890) | `internal/extractors/javascript/testdata/vue_internals/CounterStore.vue`<br>`internal/extractors/vue/issue2890_pinia_test.go`<br>`internal/extractors/vue/pinia_store.go` | — |
| `props_emits_macros` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/Comp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| `provide_inject` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/Comp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| `redux_store_extraction` | ⚠️ `partial` | `2026-05-29` | — | [link](https://github.com/cajasmota/archigraph/issues/2910) | `internal/extractor/cross_framework_query.go`<br>`internal/extractors/javascript/testdata/vue_internals/CrossFrameworkQuery.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2910_cross_framework_test.go` | Cross-framework reuse of the React-ecosystem Redux detector (#2907) for Vue: framework-agnostic Redux Toolkit configureStore/createStore (redux_store), createSlice (redux_slice), createApi (rtk_query_api), createAsyncThunk (redux_async_thunk), createEntityAdapter used in a Vue SFC <script> are decorated SCOPE.Operation (via=redux) with a CONTAINS edge from the component. Shared detector in internal/extractor/cross_framework_query.go; gated on the @reduxjs/toolkit import. Partial: the regex SFC pass decorates the factory call site only; it does not decompose slices into per-reducer operations / actions or RTK-Query apis into per-endpoint operations the way the React .tsx tree-sitter pass (react_ecosystem.go) does — Redux+RTK is a React-dominant idiom and rare in Vue (Pinia/Vuex dominate, covered by pinia_store). |
| `scoped_style_extraction` | — `not_applicable` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2876) | — | — |
| `sfc_block_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/Comp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| `slot_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/Comp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| `tanstack_query_extraction` | ✅ `full` | `2026-05-29` | — | [link](https://github.com/cajasmota/archigraph/issues/2910) | `internal/extractor/cross_framework_query.go`<br>`internal/extractors/javascript/testdata/vue_internals/CrossFrameworkQuery.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2910_cross_framework_test.go` | Cross-framework reuse of the React-ecosystem TanStack Query detector (#2907) for Vue: @tanstack/vue-query useQuery/useMutation/useInfiniteQuery/useQueries/useQueryClient composables in a Vue SFC <script> are decorated SCOPE.Operation subtype=tanstack_query (query_kind + query_call stamped) with a CONTAINS edge from the component. Shared detector in internal/extractor/cross_framework_query.go; gated on the @tanstack/*-query import so a local useQuery is not mis-tagged. Decorate-only (#2839). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.vue ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
