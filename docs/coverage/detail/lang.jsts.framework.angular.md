<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.angular` — Angular

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 18

## Capabilities


### Structure

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `component_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | — |
| `context_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | — |
| `hoc_wrapper_recognition` | — `not_applicable` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2854) | — | — |
| `hook_recognition` | — `not_applicable` | — | — | — | — | — |
| `jsx_template` | — `not_applicable` | — | — | — | — | — |

### Data Flow

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `branch_conditions` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2855_angular_dataflow_test.go`<br>`testdata/fixtures/real-world/typescript/angular_dataflow_component.ts` | — |
| `data_fetching` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2855_angular_dataflow_test.go`<br>`testdata/fixtures/real-world/typescript/angular_dataflow_component.ts` | — |
| `prop_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2855) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2855_angular_dataflow_test.go`<br>`testdata/fixtures/real-world/typescript/angular_dataflow_component.ts` | — |
| `state_management` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2884) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/angular_nav_lifecycle.go`<br>`internal/extractors/javascript/angular_rxjs_guards.go`<br>`internal/extractors/javascript/issue2884_angular_state_test.go`<br>`testdata/fixtures/real-world/typescript/angular_state_management.ts` | RE-GREENED partial->full (#2884, resolves AUDIT #2847). angularStateManagement now emits state_store containers for Angular signals (signal()/computed()) and RxJS BehaviorSubject/Subject service members, plus signalStore()/withState() (ngrx signal store); .set()/.update()/.mutate() (signals) and .next() (subjects) emit state_setter ops + WRITES_TO edges (consistent with React/Vue/Svelte). ngrx Redux Store select/dispatch kept. Verified on the gothinkster angular-realworld files the audit cited (auth.component.ts signals, user.service.ts BehaviorSubject): it now detects the signals + BehaviorSubject state, not just ngrx. |

### Navigation

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `router_pattern` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/angular_nav_lifecycle.go`<br>`internal/extractors/javascript/issue2856_angular_test.go`<br>`testdata/fixtures/real-world/typescript/angular_nav_lifecycle_component.ts` | — |

### Type System

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `enum_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `interface_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |
| `type_alias_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `state_setter_emission` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2751) | `internal/extractors/javascript/angular_nav_lifecycle.go`<br>`internal/extractors/javascript/issue2856_angular_test.go`<br>`testdata/fixtures/real-world/typescript/angular_nav_lifecycle_component.ts` | — |

### Testing

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `tests_linkage` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `constant_propagation` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `env_fallback_recognition` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| `import_resolution_quality` | ✅ `full` | `2026-05-28` | — | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/markup_script.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_angular/app.component.ts` | — |

## Framework-specific

### Angular Internals

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `decorator_recognition` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2847) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | AUDIT(#2847) taxonomy: angular.go angularClassDecorators emits component/service/directive/pipe/module subtypes. Verified on angular-realworld: angular_component x18, angular_service x6, angular_pipe x2, angular_directive x1. |
| `dependency_injection` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2847) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | AUDIT(#2847) taxonomy: constructor-DI -> INJECTED_INTO edges. Verified on angular-realworld (5 INJECTED_INTO->ArticleComponent etc.), incl. modern inject() function-DI. |
| `directive_recognition` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2847) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | AUDIT(#2847) NEW idiom cell: @Directive -> angular_directive subtype. Verified on angular-realworld + nativescript-ng. |
| `guard_interceptor_recognition` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2874) | `internal/extractors/javascript/angular_rxjs_guards.go`<br>`internal/extractors/javascript/issue2874_angular_test.go`<br>`internal/extractors/javascript/testdata/angular_internals/rxjs_guards.ts` | IMPL(#2874): route guards + HTTP interceptors, class AND functional forms. Class form (angularGuardClassRels): an @Injectable class implementing CanActivate/CanActivateChild/CanDeactivate/CanLoad/CanMatch/Resolve or HttpInterceptor gets angular_role=guard|interceptor + an IMPLEMENTS edge to the interface. Functional form (angularFunctionalGuards, program-level pass): export const x: CanActivateFn|…|HttpInterceptorFn = (...) => … → SCOPE.Component subtype angular_guard|angular_interceptor. Proven by issue2874_angular_test.go. |
| `ngmodule_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2847) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | AUDIT(#2847) taxonomy: @NgModule -> angular_module subtype. Verified on real NativeScript-Angular app (angular_module x47). |
| `pipe_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2847) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | AUDIT(#2847) NEW idiom cell: @Pipe -> angular_pipe subtype. Verified on angular-realworld (angular_pipe x2). |
| `rxjs_pattern_detection` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2874) | `internal/extractors/javascript/angular_rxjs_guards.go`<br>`internal/extractors/javascript/issue2874_angular_test.go`<br>`internal/extractors/javascript/testdata/angular_internals/rxjs_guards.ts` | IMPL(#2874): angularRxjsPatterns extracts Observable idioms in Angular class bodies — .pipe(map/switchMap/filter/…) → SCOPE.Operation rxjs_pipeline + one TRANSFORMS edge per operator; .subscribe(...) → rxjs_subscription + SUBSCRIBES_TO edge; new Subject/BehaviorSubject/ReplaySubject/AsyncSubject → rxjs_subject; inline-template `| async` → rxjs_async_pipe component flag. Proven by issue2874_angular_test.go (unit fixture) AND real-data run on testdata/fixtures/real-world angular_component.ts (pipelines x3, subscriptions x2, subjects x1). |
| `service_extraction` | ✅ `full` | `2026-05-28` | — | [link](https://github.com/cajasmota/archigraph/issues/2847) | `internal/extractors/javascript/angular.go`<br>`internal/extractors/javascript/issue2854_angular_test.go` | AUDIT(#2847) NEW idiom cell: @Injectable -> angular_service subtype. Verified on angular-realworld (angular_service x6). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.angular ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
