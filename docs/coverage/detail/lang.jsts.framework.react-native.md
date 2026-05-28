<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.react-native` — React Native

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 16

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `context_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Navigation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `deep_link_extraction` | ✅ `full` | — | — | [link](2860) | `internal/extractors/javascript/mobile_navigation.go`<br>`testdata/fixtures/real-world/typescript/react_native_navigator.tsx` | — |
| `navigation_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/react_native.yaml` | — |
| `screen_detection` | ✅ `full` | `2026-05-28` | — | — | `internal/engine/rules/javascript_typescript/frameworks/react_native.yaml` | — |

### Platform

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `platform_branching` | ✅ `full` | `2026-05-28` | — | [link](2860) | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/platform_variants.go`<br>`internal/extractors/javascript/testdata/mobile_react_native/AppNavigator.tsx` | — |

### Native Bridge

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `native_module_imports` | ✅ `full` | — | — | [link](2860) | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_react_native/AppNavigator.tsx`<br>`testdata/fixtures/real-world/typescript/react_native_navigator.tsx` | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `branch_conditions` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/discriminator.go` | — |
| `state_management` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/mobile_react_native/CartScreen.tsx`<br>`internal/extractors/javascript/zustand_store.go` | useState/useReducer + Zustand; now also recognises Redux/RTK via react_ecosystem.go (RN uses @reduxjs/toolkit + RTK Query heavily). Detailed slice/store extraction in framework_specific[React Ecosystem] (#2894). |

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
| `env_fallback_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `import_resolution_quality` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_mobile/App.tsx` | — |

## Framework-specific

### React Ecosystem

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `atom_store_extraction` | ✅ `full` | `2026-05-29` | — | [link](https://github.com/cajasmota/archigraph/issues/2908) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_pr2_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Atoms.tsx` | Same atom-store extractor as React (react_ecosystem.go); RN apps use Recoil/Jotai/Valtio/MobX identically. atom/selector (recoil_atom/recoil_selector), Jotai atom/atomWithStorage (jotai_atom), proxy (valtio_proxy), observable/makeAutoObservable (mobx_store) emitted as decorated SCOPE.Component; read/write hooks via USES_HOOK. Decorate-only (#2839). |
| `form_library_extraction` | ✅ `full` | `2026-05-29` | — | [link](https://github.com/cajasmota/archigraph/issues/2909) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_pr3_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Forms.tsx` | Same form extractor as React (react_ecosystem.go decorateForms); react-hook-form and Formik are RN-compatible (RHF ships a dedicated RN guide; <Field>/Controller wrap RN TextInput). useForm/register/<Controller> (RHF) or useFormik/<Formik>/<Field> (Formik) decorate the component/hook form_library=react_hook_form|formik + form_hooks + form_components + form_fields; RHF resolver (zodResolver/yupResolver/...) -> form_resolver + validation_schema; Formik validationSchema -> validation_schema. Decorate-only (#2839). Partial only for non-literal field names / validation schemas. |
| `redux_async_flow` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Store.tsx` | Same async-flow extractor as React: createAsyncThunk -> redux_async_thunk; redux-saga watcher/worker decoration; redux-observable epics. |
| `redux_store_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Store.tsx` | Same Redux/RTK extractor as React (react_ecosystem.go); RN uses @reduxjs/toolkit + react-redux identically. createStore/combineReducers + configureStore/createSlice (-> redux_slice + redux_reducer + CONTAINS) + createEntityAdapter; useSelector/useDispatch via USES_HOOK; connect via HOC recognition. |
| `rtk_query_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Queries.tsx` | Same RTK Query extractor as React (RN uses RTK Query heavily): createApi/injectEndpoints -> rtk_query_api + rtk_query_endpoint (http_linkable) + CONTAINS. |
| `swr_extraction` | ✅ `full` | `2026-05-29` | — | [link](https://github.com/cajasmota/archigraph/issues/2908) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_pr2_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Swr.tsx` | Same SWR extractor as React (swr is RN-compatible): useSWR/useSWRMutation/useSWRInfinite surface as USES_HOOK; enclosing component/hook decorated swr=true + swr_hooks + swr_keys (literal keys). Decorate-only (#2839). |
| `tanstack_query_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Queries.tsx` | TanStack/React Query hooks (useQuery/useMutation/useInfiniteQuery/useSuspenseQuery/useQueryClient) surface as USES_HOOK edges via shared hook_recognition; identical on RN. |

### React Native CLI

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `metro_config_detection` | ✅ `full` | `2026-05-29` | — | [link](https://github.com/cajasmota/archigraph/issues/2879) | `internal/extractors/config/discover.go`<br>`internal/extractors/config/testdata/mobile/rn_cli/metro.config.js` | — |
| `native_link_recognition` | ✅ `full` | `2026-05-29` | — | [link](https://github.com/cajasmota/archigraph/issues/2879) | `internal/extractors/config/discover.go`<br>`internal/extractors/config/testdata/mobile/rn_cli/react-native.config.js` | — |

### React Native Internals

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `hoc_wrapper_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.react-native ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
