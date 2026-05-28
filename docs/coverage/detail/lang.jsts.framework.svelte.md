<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.svelte` — Svelte

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 28

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2854_test.go` | — |
| Context extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2854_test.go` | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/svelte/UserList.svelte` | — |
| Data fetching | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/svelte/UserList.svelte` | — |
| Prop extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/svelte/UserList.svelte` | — |
| State management | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2855_dataflow_test.go`<br>`testdata/fixtures/real-world/svelte/UserList.svelte` | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | ✅ `full` | `2026-05-28` | — | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2856_test.go`<br>`testdata/fixtures/real-world/svelte/UserList.svelte` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2856_test.go`<br>`testdata/fixtures/real-world/svelte/UserList.svelte` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/markup_script.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_svelte/UserCard.svelte` | — |
| Sanitizer recognition | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_svelte/UserCard.svelte` | — |
| Taint sink detection | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_svelte/UserCard.svelte` | — |
| Taint source detection | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_svelte/UserCard.svelte` | — |
| Vulnerability finding | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_svelte/UserCard.svelte` | — |

## Framework-specific

### Svelte Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Actions | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/javascript/testdata/svelte_internals/Comp.svelte`<br>`internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go` | — |
| Context | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/javascript/testdata/svelte_internals/Comp.svelte`<br>`internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go` | — |
| Props extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/javascript/testdata/svelte_internals/Comp.svelte`<br>`internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go` | — |
| Reactive statements | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/javascript/testdata/svelte_internals/Comp.svelte`<br>`internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go` | — |
| Redux store extraction | ⚠️ `partial` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2910) | `internal/extractor/cross_framework_query.go`<br>`internal/extractors/javascript/testdata/svelte_internals/CrossFrameworkQuery.svelte`<br>`internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2910_cross_framework_test.go` | Cross-framework reuse of the React-ecosystem Redux detector (#2907) for Svelte: framework-agnostic Redux Toolkit configureStore/createStore (redux_store), createSlice (redux_slice), createApi (rtk_query_api), createAsyncThunk, createEntityAdapter used in a Svelte component <script> are decorated SCOPE.Operation (via=redux) with a CONTAINS edge. Shared detector in internal/extractor/cross_framework_query.go; gated on the @reduxjs/toolkit import. Partial: the regex SFC pass decorates the factory call site only (no per-reducer/per-endpoint decomposition like the React .tsx tree-sitter pass); Redux+RTK is React-dominant and rare in Svelte (svelte/store + runes dominate, covered by stores). |
| Runes | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/javascript/testdata/svelte_internals/Comp.svelte`<br>`internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go` | — |
| Sfc block extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/javascript/testdata/svelte_internals/Comp.svelte`<br>`internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go` | — |
| Stores | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2877) | `internal/extractors/javascript/testdata/svelte_internals/Comp.svelte`<br>`internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2877_test.go` | — |
| Tanstack query extraction | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2910) | `internal/extractor/cross_framework_query.go`<br>`internal/extractors/javascript/testdata/svelte_internals/CrossFrameworkQuery.svelte`<br>`internal/extractors/svelte/extractor.go`<br>`internal/extractors/svelte/issue2910_cross_framework_test.go` | Cross-framework reuse of the React-ecosystem TanStack Query detector (#2907) for Svelte: @tanstack/svelte-query createQuery/createMutation/createInfiniteQuery/createQueries in a Svelte component <script> are decorated SCOPE.Operation subtype=tanstack_query (query_kind + query_call stamped) with a CONTAINS edge from the component. Shared detector in internal/extractor/cross_framework_query.go (the Svelte create* names share one implementation with the Vue use* / Angular inject* shapes); gated on the @tanstack/*-query import. Decorate-only (#2839). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.svelte ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
