<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.react` — React

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 48

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2735) | `internal/extractors/javascript/react.go` | — |
| Context extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/discriminator.go` | — |
| Data fetching | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/destructure_bindings.go`<br>`internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/react_ecosystem.go` | fetch/axios + react-query hook destructure; now also recognises TanStack/React Query (useQuery/useMutation/useInfiniteQuery) and RTK Query (createApi/injectEndpoints) via react_ecosystem.go. Detailed endpoint/query extraction lives in framework_specific[React Ecosystem]/tanstack_query_extraction + rtk_query_extraction (#2894) to avoid double-counting. |
| Prop extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/javascript/dataflow_react.go`<br>`internal/extractors/javascript/issue2855_react_dataflow_test.go`<br>`internal/extractors/javascript/navigation.go`<br>`testdata/fixtures/real-world/typescript/react_dataflow_component.tsx` | — |
| State management | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2855_react_dataflow_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/zustand_store.go`<br>`testdata/fixtures/real-world/typescript/react_dataflow_component.tsx` | useState/useReducer + Zustand stores; now also recognises Redux/RTK (createStore/combineReducers/configureStore/createSlice) and react-redux useSelector/useDispatch via react_ecosystem.go. Detailed slice/store/thunk extraction lives in framework_specific[React Ecosystem]/redux_store_extraction (#2894) to avoid double-counting. |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/navigation.go` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | 2932 | `internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/jsts.go` | — |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/react_substrate_test.go` | — |
| Dead code detection | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Def use chain extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/react_substrate_test.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/react_substrate_test.go` | — |
| HTTP effect | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/react_substrate_test.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/react_substrate_test.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/extractors/javascript/testdata/substrate_react/cyclic_dep.tsx`<br>`internal/links/module_cycle_pass.go`<br>`internal/substrate/react_substrate_test.go` | — |
| Mutation effect | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/react_substrate_test.go` | — |
| Pure function tagging | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go`<br>`internal/substrate/react_substrate_test.go` | — |
| Reachability analysis | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-29` | — | `internal/substrate/payload_shapes_jsts.go`<br>`internal/substrate/payload_shapes_test.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-29` | — | `internal/substrate/payload_shapes_jsts.go`<br>`internal/substrate/payload_shapes_test.go` | — |
| Sanitizer recognition | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/taint_flow.go`<br>`internal/substrate/react_substrate_test.go`<br>`internal/substrate/taint_sites_jsts.go` | — |
| Schema drift detection | ⚠️ `partial` | `2026-05-29` | 3053 | `internal/links/payload_drift.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Taint sink detection | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/taint_flow.go`<br>`internal/substrate/react_substrate_test.go`<br>`internal/substrate/taint_sites_jsts.go` | — |
| Taint source detection | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/taint_flow.go`<br>`internal/substrate/react_substrate_test.go`<br>`internal/substrate/taint_sites_jsts.go` | — |
| Template pattern catalog | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/template_pattern_pass.go`<br>`internal/substrate/react_substrate_test.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_jsts.go` | — |
| Vulnerability finding | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_react/UserDashboard.tsx`<br>`internal/links/taint_flow.go`<br>`internal/substrate/react_substrate_test.go`<br>`internal/substrate/taint_sites_jsts.go` | — |

## Framework-specific

### React Ecosystem

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Atom store extraction | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2908) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_pr2_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Atoms.tsx` | Recoil atom/selector (recoil_atom/recoil_selector, atom_key stamped), Jotai atom/atomWithStorage/atomWithReset (jotai_atom), Valtio proxy (valtio_proxy), MobX observable/makeAutoObservable/makeObservable (mobx_store) emitted as decorated SCOPE.Component (atom_library + atom_factory). Import package disambiguates the shared atom export (recoil vs jotai). Read/write hooks useRecoilState/useRecoilValue/useAtom/useAtomValue/useSnapshot surface as USES_HOOK; MobX observer is a generic HOC wrapper. Decorate-only (#2839). Partial only for stores created by mutating this in a class constructor (makeAutoObservable(this)) with no declarator value to key on. |
| Form library extraction | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2909) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_pr3_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Forms.tsx` | A component/custom-hook using React Hook Form (useForm/useFormContext/useFieldArray/useController/register/<Controller name>) or Formik (useFormik/<Formik>/<Field>/<FieldArray>/<Form>) is decorated form_library=react_hook_form|formik + form_hooks + form_components (formik JSX) + form_field_count/form_fields (literal field names from register('x') / <Field name="x">). RHF resolver linkage: useForm({resolver: zodResolver(schema)}) stamps form_resolver=zod|yup|joi|superstruct|ajv|vest|class-validator (@hookform/resolvers/*) + validation_schema (schema identifier); Formik validationSchema={schema} stamps validation_schema. The hook calls / JSX already surface generically (USES_HOOK / JSX renders); this adds the form-specific decoration. Decorate-only (#2839). Real-world recall: react-hook-form/react-hook-form 121 RHF consumers (6 resolver, 62 with field sets); jaredpalmer/formik 35 Formik consumers (12 validationSchema, 25 with fields). Partial only for field names / validation schemas expressed as non-literal (computed register names, template-literal <Field name>, inline yup.object()). |
| Redux async flow | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Store.tsx` | createAsyncThunk -> redux_async_thunk operation (action_type stamped). Redux-Saga generator watchers (takeEvery/takeLatest/takeLeading) decorated saga_role=watcher; workers (put/call/select/fork) decorated saga_role=worker. Redux-Observable epics (ofType) decorated redux_epic. Real-world: reduxjs/redux-toolkit examples 4 createAsyncThunk. |
| Redux store extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Store.tsx` | Redux createStore/combineReducers + Redux Toolkit configureStore/createSlice (-> redux_slice with per-reducer redux_reducer operations + CONTAINS edges; actions derived 1:1) + createEntityAdapter. react-redux useSelector/useDispatch/useStore surface as USES_HOOK (generic Structure/hook_recognition). connect/mapStateToProps/mapDispatchToProps recognised as HOC wrappers. Real-world: gothinkster react-redux-realworld 1 store; reduxjs/redux-toolkit examples 15 slices, 26 reducers, 16 stores, 6 entity adapters. |
| Rtk query extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Queries.tsx` | RTK Query createApi + injectEndpoints -> rtk_query_api with per-endpoint rtk_query_endpoint operations (query/mutation kind, http_method, http_path) + CONTAINS edges. Endpoints stamped http_linkable=true for cross-repo HTTP linking. Real-world: reduxjs/redux-toolkit examples 35 apis, 57 endpoints. Partial only for path recovery from non-literal template/computed query specifiers. |
| Swr extraction | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2908) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_pr2_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Swr.tsx` | useSWR/useSWRMutation/useSWRInfinite/useSWRSubscription surface as USES_HOOK edges (generic Structure/hook_recognition); the enclosing component/custom-hook is additionally decorated swr=true + swr_hooks + swr_keys (literal-string SWR keys). Decorate-only (#2839). Partial only for keys expressed as template literals / getKey functions (dynamic /api/users/[id], useSWRInfinite page getter) where no literal string is recoverable. |
| Tanstack query extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Queries.tsx` | TanStack/React Query useQuery/useMutation/useInfiniteQuery/useSuspenseQuery/useQueryClient surface as USES_HOOK edges (generic Structure/hook_recognition); QueryClient + queryKey + invalidateQueries present in call graph. Real-world: TanStack/query react examples 42 useQuery, 6 useMutation, 3 useInfiniteQuery, 7 useSuspenseQuery, 17 useQueryClient. |

### React Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Context HOC | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/2875) | — | Covered by generic Structure/context_extraction (createContext, #611) and Structure/hoc_wrapper_recognition (forwardRef/memo/lazy/connect/withX, extractor.go). Not duplicated here to avoid double-counting. |
| HOC wrapper recognition | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Hook recognition | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2735) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2854_react_test.go`<br>`internal/extractors/javascript/react.go` | — |
| Hooks | — `not_applicable` | — | [link](https://github.com/cajasmota/archigraph/issues/2875) | — | Covered by generic Structure/hook_recognition (react.go USES_HOOK + custom-hook subtype). Not duplicated here to avoid double-counting. |
| JSX template | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Lazy code splitting | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2958) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2875_react_internals_test.go`<br>`internal/extractors/javascript/navigation.go`<br>`internal/extractors/javascript/react_internals.go`<br>`internal/extractors/javascript/testdata/react_internals/AppShell.tsx` | React.lazy(() => import(specifier)) is decorated react_lazy=true unconditionally. lazy_module is stamped for string literals (trimmed) and template literals (${...} normalised to {*} via normalizeTemplateLiteralRoute); computed/call-expression specifiers leave lazy_module unset. Proved by issue2875_react_internals_test.go DynamicPanel + ComputedPanel assertions. |
| Portal recognition | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2875) | `internal/extractors/javascript/issue2875_react_internals_test.go`<br>`internal/extractors/javascript/react_internals.go`<br>`internal/extractors/javascript/testdata/react_internals/AppShell.tsx` | Components calling createPortal / ReactDOM.createPortal are decorated react_portal. |
| Suspense error boundary | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2875) | `internal/extractors/javascript/issue2875_react_internals_test.go`<br>`internal/extractors/javascript/react_internals.go`<br>`internal/extractors/javascript/testdata/react_internals/AppShell.tsx` | Components rendering <Suspense> are decorated react_suspense; class components declaring componentDidCatch / getDerivedStateFromError are decorated react_error_boundary. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.react ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
