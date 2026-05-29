<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.vue` — Vue

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 45

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2854_test.go` | — |
| Context extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2854_test.go` | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/vue/UserCard.vue` | — |
| Data fetching | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/vue/UserCard.vue` | — |
| Prop extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/vue/UserCard.vue` | — |
| State management | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/vue/UserCard.vue` | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | ✅ `full` | `2026-05-28` | — | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2856_test.go`<br>`testdata/fixtures/real-world/vue/UserCard.vue` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2856_test.go`<br>`testdata/fixtures/real-world/vue/UserCard.vue` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/links/payload_drift.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go` | Language-agnostic pass operates over extracted graph; Vue .vue files are processed via markup-script dispatcher but no Vue-specific integration test exists for this cap. |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-29` | 3053 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effect_sinks_markup_script.go` | Vue SFC uses markup-script dispatcher which delegates to sniffEffectsJSTS; reactive DB/FS/mutation patterns fire on <script setup> but no Vue-specific fixture tests this cap. |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/links/payload_drift.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go` | Language-agnostic pass operates over extracted graph; Vue .vue files are processed via markup-script dispatcher but no Vue-specific integration test exists for this cap. |
| Def use chain extraction | ✅ `full` | `2026-05-29` | — | `internal/substrate/def_use_jsts.go`<br>`internal/substrate/def_use_t3_test.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3053 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effect_sinks_markup_script.go` | Vue SFC uses markup-script dispatcher which delegates to sniffEffectsJSTS; reactive DB/FS/mutation patterns fire on <script setup> but no Vue-specific fixture tests this cap. |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3053 | `internal/substrate/effect_sinks_markup_script.go`<br>`internal/substrate/effect_sinks_t3_test.go` | TestSniffEffectsMarkupScript_Vue proves http_effect fires on Vue <script setup>; db/fs/mutation effects not Vue-specifically tested. |
| Import resolution quality | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/markup_script.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_vue/UserCard.vue` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/links/payload_drift.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go` | Language-agnostic pass operates over extracted graph; Vue .vue files are processed via markup-script dispatcher but no Vue-specific integration test exists for this cap. |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3053 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effect_sinks_markup_script.go` | Vue SFC uses markup-script dispatcher which delegates to sniffEffectsJSTS; reactive DB/FS/mutation patterns fire on <script setup> but no Vue-specific fixture tests this cap. |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/links/payload_drift.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go` | Language-agnostic pass operates over extracted graph; Vue .vue files are processed via markup-script dispatcher but no Vue-specific integration test exists for this cap. |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/links/payload_drift.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go` | Language-agnostic pass operates over extracted graph; Vue .vue files are processed via markup-script dispatcher but no Vue-specific integration test exists for this cap. |
| Request shape extraction | ✅ `full` | `2026-05-29` | — | `internal/substrate/payload_shapes_t3.go`<br>`internal/substrate/payload_shapes_t3_test.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-29` | — | `internal/substrate/payload_shapes_t3.go`<br>`internal/substrate/payload_shapes_t3_test.go` | — |
| Sanitizer recognition | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_vue/UserCard.vue` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/module_cycle_pass.go`<br>`internal/links/payload_drift.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go` | Language-agnostic pass operates over extracted graph; Vue .vue files are processed via markup-script dispatcher but no Vue-specific integration test exists for this cap. |
| Taint sink detection | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_vue/UserCard.vue` | — |
| Taint source detection | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_vue/UserCard.vue` | — |
| Template pattern catalog | ✅ `full` | `2026-05-29` | — | `internal/substrate/def_use_t3_test.go`<br>`internal/substrate/template_pattern_markup_script.go` | — |
| Vulnerability finding | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_vue/UserCard.vue` | — |

## Framework-specific

### Vue Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Composition API | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/Comp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| Directive recognition | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/Comp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| Hook recognition | ✅ `full` | `2026-05-28` | — | `internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2854_test.go` | — |
| Options API | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/OptionsComp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| Pinia store | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2890) | `internal/extractors/javascript/testdata/vue_internals/CounterStore.vue`<br>`internal/extractors/vue/issue2890_pinia_test.go`<br>`internal/extractors/vue/pinia_store.go` | — |
| Props emits macros | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/Comp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| Provide inject | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/Comp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| Redux store extraction | 🟢 `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2910) | `internal/extractor/cross_framework_query.go`<br>`internal/extractors/javascript/testdata/vue_internals/CrossFrameworkQuery.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2910_cross_framework_test.go` | Cross-framework reuse of the React-ecosystem Redux detector (#2907) for Vue: framework-agnostic Redux Toolkit configureStore/createStore (redux_store), createSlice (redux_slice), createApi (rtk_query_api), createAsyncThunk (redux_async_thunk), createEntityAdapter used in a Vue SFC <script> are decorated SCOPE.Operation (via=redux) with a CONTAINS edge from the component. Shared detector in internal/extractor/cross_framework_query.go; gated on the @reduxjs/toolkit import. Partial: the regex SFC pass decorates the factory call site only; it does not decompose slices into per-reducer operations / actions or RTK-Query apis into per-endpoint operations the way the React .tsx tree-sitter pass (react_ecosystem.go) does — Redux+RTK is a React-dominant idiom and rare in Vue (Pinia/Vuex dominate, covered by pinia_store). |
| Scoped style extraction | — `not_applicable` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2876) | — | — |
| Sfc block extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/Comp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| Slot extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2876) | `internal/extractors/javascript/testdata/vue_internals/Comp.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2876_internals_test.go` | — |
| Tanstack query extraction | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2910) | `internal/extractor/cross_framework_query.go`<br>`internal/extractors/javascript/testdata/vue_internals/CrossFrameworkQuery.vue`<br>`internal/extractors/vue/extractor.go`<br>`internal/extractors/vue/issue2910_cross_framework_test.go` | Cross-framework reuse of the React-ecosystem TanStack Query detector (#2907) for Vue: @tanstack/vue-query useQuery/useMutation/useInfiniteQuery/useQueries/useQueryClient composables in a Vue SFC <script> are decorated SCOPE.Operation subtype=tanstack_query (query_kind + query_call stamped) with a CONTAINS edge from the component. Shared detector in internal/extractor/cross_framework_query.go; gated on the @tanstack/*-query import so a local useQuery is not mis-tagged. Decorate-only (#2839). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.vue ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
