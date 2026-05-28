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
| `data_fetching` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/destructure_bindings.go`<br>`internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/react_ecosystem.go` | fetch/axios + react-query hook destructure; now also recognises TanStack/React Query (useQuery/useMutation/useInfiniteQuery) and RTK Query (createApi/injectEndpoints) via react_ecosystem.go. Detailed endpoint/query extraction lives in framework_specific[React Ecosystem]/tanstack_query_extraction + rtk_query_extraction (#2894) to avoid double-counting. |
| `prop_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/javascript/dataflow_react.go`<br>`internal/extractors/javascript/issue2855_react_dataflow_test.go`<br>`internal/extractors/javascript/navigation.go`<br>`testdata/fixtures/real-world/typescript/react_dataflow_component.tsx` | — |
| `state_management` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2855_react_dataflow_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/zustand_store.go`<br>`testdata/fixtures/real-world/typescript/react_dataflow_component.tsx` | useState/useReducer + Zustand stores; now also recognises Redux/RTK (createStore/combineReducers/configureStore/createSlice) and react-redux useSelector/useDispatch via react_ecosystem.go. Detailed slice/store/thunk extraction lives in framework_specific[React Ecosystem]/redux_store_extraction (#2894) to avoid double-counting. |

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

## Framework-specific

### React Ecosystem

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `redux_async_flow` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Store.tsx` | createAsyncThunk -> redux_async_thunk operation (action_type stamped). Redux-Saga generator watchers (takeEvery/takeLatest/takeLeading) decorated saga_role=watcher; workers (put/call/select/fork) decorated saga_role=worker. Redux-Observable epics (ofType) decorated redux_epic. Real-world: reduxjs/redux-toolkit examples 4 createAsyncThunk. |
| `redux_store_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Store.tsx` | Redux createStore/combineReducers + Redux Toolkit configureStore/createSlice (-> redux_slice with per-reducer redux_reducer operations + CONTAINS edges; actions derived 1:1) + createEntityAdapter. react-redux useSelector/useDispatch/useStore surface as USES_HOOK (generic Structure/hook_recognition). connect/mapStateToProps/mapDispatchToProps recognised as HOC wrappers. Real-world: gothinkster react-redux-realworld 1 store; reduxjs/redux-toolkit examples 15 slices, 26 reducers, 16 stores, 6 entity adapters. |
| `rtk_query_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Queries.tsx` | RTK Query createApi + injectEndpoints -> rtk_query_api with per-endpoint rtk_query_endpoint operations (query/mutation kind, http_method, http_path) + CONTAINS edges. Endpoints stamped http_linkable=true for cross-repo HTTP linking. Real-world: reduxjs/redux-toolkit examples 35 apis, 57 endpoints. Partial only for path recovery from non-literal template/computed query specifiers. |
| `tanstack_query_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Queries.tsx` | TanStack/React Query useQuery/useMutation/useInfiniteQuery/useSuspenseQuery/useQueryClient surface as USES_HOOK edges (generic Structure/hook_recognition); QueryClient + queryKey + invalidateQueries present in call graph. Real-world: TanStack/query react examples 42 useQuery, 6 useMutation, 3 useInfiniteQuery, 7 useSuspenseQuery, 17 useQueryClient. |

### React Internals

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `context_hoc` | — `not_applicable` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2875) | — | Covered by generic Structure/context_extraction (createContext, #611) and Structure/hoc_wrapper_recognition (forwardRef/memo/lazy/connect/withX, extractor.go). Not duplicated here to avoid double-counting. |
| `hooks` | — `not_applicable` | — | — | [link](https://github.com/cajasmota/archigraph/issues/2875) | — | Covered by generic Structure/hook_recognition (react.go USES_HOOK + custom-hook subtype). Not duplicated here to avoid double-counting. |
| `lazy_code_splitting` | ⚠️ `partial` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2875) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2875_react_internals_test.go`<br>`internal/extractors/javascript/react_internals.go`<br>`internal/extractors/javascript/testdata/react_internals/AppShell.tsx` | React.lazy(() => import('mod')) is decorated react_lazy + lazy_module (the code-split target). Partial: the dynamic-import specifier is recovered only when it is a string literal; computed/template specifiers are not resolved. |
| `portal_recognition` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2875) | `internal/extractors/javascript/issue2875_react_internals_test.go`<br>`internal/extractors/javascript/react_internals.go`<br>`internal/extractors/javascript/testdata/react_internals/AppShell.tsx` | Components calling createPortal / ReactDOM.createPortal are decorated react_portal. |
| `suspense_error_boundary` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2875) | `internal/extractors/javascript/issue2875_react_internals_test.go`<br>`internal/extractors/javascript/react_internals.go`<br>`internal/extractors/javascript/testdata/react_internals/AppShell.tsx` | Components rendering <Suspense> are decorated react_suspense; class components declaring componentDidCatch / getDerivedStateFromError are decorated react_error_boundary. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.react ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
