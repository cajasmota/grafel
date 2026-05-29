<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.angular` — Angular

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 42

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | — |
| Context extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2855_angular_dataflow_test.go`<br>`testdata/fixtures/real-world/typescript/angular_dataflow_component.ts` | — |
| Data fetching | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2855_angular_dataflow_test.go`<br>`testdata/fixtures/real-world/typescript/angular_dataflow_component.ts` | — |
| Prop extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2855_angular_dataflow_test.go`<br>`testdata/fixtures/real-world/typescript/angular_dataflow_component.ts` | — |
| State management | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2884) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/angular_nav_lifecycle.go`<br>`internal/extractors/javascript/angular_rxjs_guards.go`<br>`internal/extractors/javascript/issue2884_angular_state_test.go`<br>`testdata/fixtures/real-world/typescript/angular_state_management.ts` | RE-GREENED partial->full (#2884, resolves AUDIT #2847). angularStateManagement now emits state_store containers for Angular signals (signal()/computed()) and RxJS BehaviorSubject/Subject service members, plus signalStore()/withState() (ngrx signal store); .set()/.update()/.mutate() (signals) and .next() (subjects) emit state_setter ops + WRITES_TO edges (consistent with React/Vue/Svelte). ngrx Redux Store select/dispatch kept. Verified on the gothinkster angular-realworld files the audit cited (auth.component.ts signals, user.service.ts BehaviorSubject): it now detects the signals + BehaviorSubject state, not just ngrx. |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/angular_nav_lifecycle.go`<br>`internal/extractors/javascript/issue2856_angular_test.go`<br>`testdata/fixtures/real-world/typescript/angular_nav_lifecycle_component.ts` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/javascript/angular_nav_lifecycle.go`<br>`internal/extractors/javascript/issue2856_angular_test.go`<br>`testdata/fixtures/real-world/typescript/angular_nav_lifecycle_component.ts` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Import resolution quality | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/markup_script.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_angular/app.component.ts` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Request shape extraction | 🟢 `partial` | `2026-05-29` | 3053 | `internal/substrate/payload_shapes_jsts.go`<br>`internal/substrate/payload_shapes_test.go` | Framework-blind jsts payload sniffer fires on Angular .ts files; req.body field extraction + axios/fetch consumer shapes proved by jsts-generic tests but no Angular-specific proving fixture/test exists. |
| Response shape extraction | 🟢 `partial` | `2026-05-29` | 3053 | `internal/substrate/payload_shapes_jsts.go`<br>`internal/substrate/payload_shapes_test.go` | Framework-blind jsts payload sniffer fires on Angular .ts files; res.json inline response shapes proved by jsts-generic tests but no Angular-specific proving fixture/test exists. |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Schema drift detection | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Taint source detection | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3053 | `internal/links/effect_propagation.go`<br>`internal/links/reachability.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/entry_points_jsts.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/template_pattern_jsts.go` | Framework-blind jsts sniffer fires on Angular .ts files; React-specific tests (react_substrate_test.go) prove each sniffer, but no Angular-specific proving fixture/test exists yet. |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3186 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_jsts_metafw_test.go` | Angular is a client framework (no server request source); taint_flow.go pairs the DOM XSS sink (.innerHTML=) against the recognised sanitizer (DOMPurify.sanitize). Sink/sanitizer-driven, framework-blind → partial. |

## Framework-specific

### Angular Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Decorator recognition | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2847) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | AUDIT(#2847) taxonomy: angular.go angularClassDecorators emits component/service/directive/pipe/module subtypes. Verified on angular-realworld: angular_component x18, angular_service x6, angular_pipe x2, angular_directive x1. |
| Dependency injection | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2847) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | AUDIT(#2847) taxonomy: constructor-DI -> INJECTED_INTO edges. Verified on angular-realworld (5 INJECTED_INTO->ArticleComponent etc.), incl. modern inject() function-DI. |
| Directive recognition | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2847) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | AUDIT(#2847) NEW idiom cell: @Directive -> angular_directive subtype. Verified on angular-realworld + nativescript-ng. |
| Guard interceptor recognition | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2874) | `internal/extractors/javascript/angular_rxjs_guards.go`<br>`internal/extractors/javascript/issue2874_angular_test.go`<br>`internal/extractors/javascript/testdata/angular_internals/rxjs_guards.ts` | IMPL(#2874): route guards + HTTP interceptors, class AND functional forms. Class form (angularGuardClassRels): an @Injectable class implementing CanActivate/CanActivateChild/CanDeactivate/CanLoad/CanMatch/Resolve or HttpInterceptor gets angular_role=guard|interceptor + an IMPLEMENTS edge to the interface. Functional form (angularFunctionalGuards, program-level pass): export const x: CanActivateFn|…|HttpInterceptorFn = (...) => … → SCOPE.Component subtype angular_guard|angular_interceptor. Proven by issue2874_angular_test.go. |
| Ngmodule extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2847) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | AUDIT(#2847) taxonomy: @NgModule -> angular_module subtype. Verified on real NativeScript-Angular app (angular_module x47). |
| Pipe extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2847) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | AUDIT(#2847) NEW idiom cell: @Pipe -> angular_pipe subtype. Verified on angular-realworld (angular_pipe x2). |
| RxJS pattern detection | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2874) | `internal/extractors/javascript/angular_rxjs_guards.go`<br>`internal/extractors/javascript/issue2874_angular_test.go`<br>`internal/extractors/javascript/testdata/angular_internals/rxjs_guards.ts` | IMPL(#2874): angularRxjsPatterns extracts Observable idioms in Angular class bodies — .pipe(map/switchMap/filter/…) → SCOPE.Operation rxjs_pipeline + one TRANSFORMS edge per operator; .subscribe(...) → rxjs_subscription + SUBSCRIBES_TO edge; new Subject/BehaviorSubject/ReplaySubject/AsyncSubject → rxjs_subject; inline-template `| async` → rxjs_async_pipe component flag. Proven by issue2874_angular_test.go (unit fixture) AND real-data run on testdata/fixtures/real-world angular_component.ts (pipelines x3, subscriptions x2, subjects x1). |
| Service extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2847) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | AUDIT(#2847) NEW idiom cell: @Injectable -> angular_service subtype. Verified on angular-realworld (angular_service x6). |
| Tanstack query extraction | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2910) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2910_angular_tanstack_test.go`<br>`internal/extractors/javascript/react_ecosystem.go`<br>`internal/extractors/javascript/testdata/angular_internals/tanstack_query.ts` | Cross-framework reuse of the React-ecosystem TanStack Query detector (#2907) for Angular: the @tanstack/angular-query-experimental adapter (injectQuery/injectMutation/injectInfiniteQuery/injectQueries) inside an Angular class body is decorated SCOPE.Operation subtype=tanstack_query (query_kind + query_call stamped, via=tanstack_query) with a CONTAINS edge from the component class. Angular .ts is parsed by the javascript tree-sitter extractor, so this lives in react_ecosystem.go (isTanstackQueryPkg extended to match the angular-query-experimental adapter) wired into angular.go. NgRx (Angular's own store) and RTK/RTK-Query are already covered (angular.go state_management + the framework-agnostic react_ecosystem.go createApi/configureStore AST pass that fires on any .ts importing @reduxjs/toolkit). Decorate-only (#2839). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.angular ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
