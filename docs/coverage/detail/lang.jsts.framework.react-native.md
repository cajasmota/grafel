<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.react-native` — React Native

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Mobile
- **Capability cells:** 44

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Context extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Deep link extraction | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`testdata/fixtures/real-world/typescript/react_native_navigator.tsx` | — |
| Navigation extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/frameworks/react_native.yaml` | — |
| Screen detection | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/frameworks/react_native.yaml` | — |

### Platform

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Platform branching | ✅ `full` | `2026-05-28` | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/platform_variants.go`<br>`internal/extractors/javascript/testdata/mobile_react_native/AppNavigator.tsx` | — |

### Native Bridge

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | ✅ `full` | — | 2860 | `internal/extractors/javascript/mobile_navigation.go`<br>`internal/extractors/javascript/testdata/mobile_react_native/AppNavigator.tsx`<br>`testdata/fixtures/real-world/typescript/react_native_navigator.tsx` | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/discriminator.go` | — |
| State management | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/mobile_react_native/CartScreen.tsx`<br>`internal/extractors/javascript/zustand_store.go` | useState/useReducer + Zustand; now also recognises Redux/RTK via react_ecosystem.go (RN uses @reduxjs/toolkit + RTK Query heavily). Detailed slice/store extraction in framework_specific[React Ecosystem] (#2894). |

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
| Confidence overlay | 🟢 `partial` | — | 3059 | `internal/links/effect_propagation.go`<br>`internal/substrate/jsts.go` | — |
| Constant propagation | 🟢 `partial` | `2026-05-28` | 3059 | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go` | — |
| DB effect | — `not_applicable` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | Mobile apps (RN/Expo/Ionic/NativeScript) call remote HTTP APIs, not Node.js ORM primitives directly; db_effect N/A at the mobile client layer |
| Dead code detection | 🟢 `partial` | — | 3059 | `internal/patterns/dead_module_detector.go` | — |
| Def use chain extraction | 🟢 `partial` | — | 3059 | `internal/substrate/def_use_jsts.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | — |
| HTTP effect | 🟢 `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_mobile/App.tsx` | — |
| Module cycle detection | 🟢 `partial` | — | 3059 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | — |
| Pure function tagging | 🟢 `partial` | — | 3059 | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | — | 3059 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Request shape extraction | 🟢 `partial` | — | 3059 | `internal/substrate/payload_shapes_jsts.go` | — |
| Response shape extraction | 🟢 `partial` | — | 3059 | `internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | 🟢 `partial` | — | 3059 | `internal/substrate/taint_sites_jsts.go` | — |
| Schema drift detection | 🟢 `partial` | — | 3059 | `internal/links/payload_drift.go` | — |
| Taint sink detection | 🟢 `partial` | — | 3059 | `internal/substrate/taint_sites_jsts.go` | — |
| Taint source detection | 🟢 `partial` | — | 3059 | `internal/substrate/taint_sites_jsts.go` | — |
| Template pattern catalog | 🟢 `partial` | — | 3059 | `internal/substrate/template_pattern_jsts.go` | — |
| Vulnerability finding | 🟢 `partial` | — | 3059 | `internal/links/taint_flow.go` | — |

## Framework-specific

### React Ecosystem

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Atom store extraction | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2908) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_pr2_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Atoms.tsx` | Same atom-store extractor as React (react_ecosystem.go); RN apps use Recoil/Jotai/Valtio/MobX identically. atom/selector (recoil_atom/recoil_selector), Jotai atom/atomWithStorage (jotai_atom), proxy (valtio_proxy), observable/makeAutoObservable (mobx_store) emitted as decorated SCOPE.Component; read/write hooks via USES_HOOK. Decorate-only (#2839). |
| Form library extraction | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2909) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_pr3_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Forms.tsx` | Same form extractor as React (react_ecosystem.go decorateForms); react-hook-form and Formik are RN-compatible (RHF ships a dedicated RN guide; <Field>/Controller wrap RN TextInput). useForm/register/<Controller> (RHF) or useFormik/<Formik>/<Field> (Formik) decorate the component/hook form_library=react_hook_form|formik + form_hooks + form_components + form_fields; RHF resolver (zodResolver/yupResolver/...) -> form_resolver + validation_schema; Formik validationSchema -> validation_schema. Decorate-only (#2839). Partial only for non-literal field names / validation schemas. |
| Redux async flow | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Store.tsx` | Same async-flow extractor as React: createAsyncThunk -> redux_async_thunk; redux-saga watcher/worker decoration; redux-observable epics. |
| Redux store extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Store.tsx` | Same Redux/RTK extractor as React (react_ecosystem.go); RN uses @reduxjs/toolkit + react-redux identically. createStore/combineReducers + configureStore/createSlice (-> redux_slice + redux_reducer + CONTAINS) + createEntityAdapter; useSelector/useDispatch via USES_HOOK; connect via HOC recognition. |
| Rtk query extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Queries.tsx` | Same RTK Query extractor as React (RN uses RTK Query heavily): createApi/injectEndpoints -> rtk_query_api + rtk_query_endpoint (http_linkable) + CONTAINS. |
| Swr extraction | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2908) | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2894_react_ecosystem_pr2_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Swr.tsx` | Same SWR extractor as React (swr is RN-compatible): useSWR/useSWRMutation/useSWRInfinite surface as USES_HOOK; enclosing component/hook decorated swr=true + swr_hooks + swr_keys (literal keys). Decorate-only (#2839). |
| Tanstack query extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2894) | `internal/extractors/javascript/issue2894_react_ecosystem_test.go`<br>`internal/extractors/javascript/react.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/react_ecosystem/Queries.tsx` | TanStack/React Query hooks (useQuery/useMutation/useInfiniteQuery/useSuspenseQuery/useQueryClient) surface as USES_HOOK edges via shared hook_recognition; identical on RN. |

### React Native CLI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Metro config detection | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2879) | `internal/extractors/config/discover.go`<br>`internal/extractors/config/testdata/mobile/rn_cli/metro.config.js` | — |
| Native link recognition | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2879) | `internal/extractors/config/discover.go`<br>`internal/extractors/config/testdata/mobile/rn_cli/react-native.config.js` | — |

### React Native Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| HOC wrapper recognition | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.react-native ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
