<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.fsharp.framework.giraffe` — Giraffe / Saturn (F# HTTP)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [F#](../by-language/fsharp.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint pagination posture | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint response codes | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-12` | 4906 | `internal/engine/http_endpoint_giraffe.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/httproutes/canonicalize.go` | #4906 (was wrongly 'missing'): synthesizeGiraffeRoutes (#4749) scans an F# source for Giraffe verb-combinator chains `GET >=> route "/users"` / `routef "/users/%i"` / routeCi variants (giraffeRouteRe) and Saturn `router { get "/users/:id" handler }` verb operations (saturnRouteRe), synthesising one canonical http_endpoint_definition per (verb,path) via httproutes.Canonicalize(FrameworkGiraffe) — routef printf placeholders (%i/%s/%O/...) rewritten to `{}` and Saturn `:name` colon-params handled. Wired into applyHTTPEndpointSynthesis at http_endpoint_synthesis.go (the F# producer-side branch, gated by giraffeHasRoute so arbitrary F# files are skipped). Proven by TestGiraffe_BasicRoute / _RoutefTypedParam / _SaturnRouter / _CanonicalizeFormat / _InterpolatedRouteDropped / _NonWebFileIgnored. subRoute/forward mount-prefix folding + routeStartsWith/routex are handled as of #4940 (see route_extraction); the only honest exclusion is interpolated/variable paths. |
| Handler attribution | ✅ `full` | `2026-06-12` | 4940 | `internal/custom/fsharp/tests_route_e2e.go`<br>`internal/engine/http_endpoint_giraffe.go`<br>`internal/engine/http_endpoint_synthesis.go` | #4940 (was partial under #4906): the prior route-string test linkage is RETAINED, AND synthesizeGiraffeRoutes now captures the trailing same-file handler symbol of a route — Giraffe `route "/users" >=> listUsers` (giraffeRouteRe group 4, bare ident only) and Saturn `get "/users" listUsers` (saturnRouteRe group 3) — passing it as refName so the shared synthesis-time structural bridge (synthesisHandlerStructuralRef, #4319) emits an endpoint->handler IMPLEMENTS edge bound to the resolved `let`-bound HttpHandler by (file,name). Proven by TestGiraffe_NamedHandlerImplements / _SaturnNamedHandlerImplements (assert a `http_endpoint_synthesis_time_bridge` IMPLEMENTS edge for GET /users). Honest: a COMPOSED handler (a further `>=>` chain), a lambda, or a dotted/qualified name yields no captured symbol — the endpoint still emits, just without the named bridge (no fabricated edge). |
| Route extraction | ✅ `full` | `2026-06-12` | 4940 | `internal/engine/http_endpoint_giraffe.go` | #4940 (was partial under #4906): static Giraffe `route`/`routef`/`routeCi`/`routeStartsWith`/`routex` and Saturn `get/post/put/delete/...`(+f) verb+path registrations are recovered by synthesizeGiraffeRoutes. NEW in #4940: (1) `subRoute "/api" (...)` / `forward "/api" (...)` mount prefixes ARE now folded into nested child routes via balanced-paren span tracking (collectGiraffeMounts/matchCloseParen/prefixAt) — nesting composes left-to-right (`subRoute "/api" (subRoute "/v1" ...)` -> `/api/v1/...`), proven by TestGiraffe_SubRouteFolding / _ForwardFolding; (2) `routeStartsWith "/api"` emits as a literal prefix path and `routex "/users/(\d+)"` regex bodies canonicalise to the positional `{}` wildcard (canonicalizeRoutex), proven by TestGiraffe_RouteStartsWithAndRoutex. Honest exclusion (retained): interpolated / variable paths (`route basePath`, `$"{x}"`) and interpolated/variable subRoute/forward mount prefixes are dropped (only string-literal paths/prefixes emit, proven by TestGiraffe_InterpolatedRouteDropped). |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🟢 `partial` | `2026-06-12` | 4906 | `internal/extractors/fsharp/extractor.go`<br>`internal/extractors/fsharp/fsharp_test.go` | #4906: the F# enum analog is the discriminated union — classifyTypeSubtype emits SCOPE.Component subtype=discriminated_union for `type T = A | B | C` (`= |` / body-leading `|`), proven by TestFSharp_TypeSubtypes. Partial: the DU CASES (A/B/C) are not emitted as individual entities, and a CLI-style `enum` (`type T = | A = 0`) is not distinguished from a DU. Case-level extraction is a follow-up. |
| Interface extraction | 🟢 `partial` | `2026-06-12` | 4906 | `internal/extractors/fsharp/extractor.go`<br>`internal/extractors/fsharp/fsharp_test.go` | #4906: classifyTypeSubtype emits SCOPE.Component subtype=interface for `type T = interface ... end` (and class/struct for those bodies), proven by TestFSharp_TypeSubtypes. Partial: abstract-member surfaces and interface-IMPLEMENTS edges are not yet modelled. |
| Type alias extraction | 🟢 `partial` | `2026-06-12` | 4906 | `internal/extractors/fsharp/extractor.go` | #4906: a `type Foo = Bar` alias is emitted as a SCOPE.Component (subtype per classifyTypeSubtype). Partial-honest: a pure alias falls through to the default 'type' subtype (no distinct 'alias' subtype), so alias-vs-nominal-type is not yet distinguished — a follow-up. |
| Type extraction | 🟢 `partial` | `2026-06-12` | 4906 | `internal/extractors/fsharp/extractor.go`<br>`internal/extractors/fsharp/fsharp_test.go` | #4906 (was wrongly 'missing'): typeRE emits every F# `type T = ...` declaration as a SCOPE.Component, with classifyTypeSubtype distinguishing record (`= {`) / discriminated_union (`= |`) / interface / class / struct, and a type CONTAINS its more-indented members. Proven by TestFSharp_TypeDiscovery + TestFSharp_TypeSubtypes. Partial: record fields / DU cases are not emitted as sub-entities and generic type-param constraints are not modelled. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI injection point | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI scope resolution | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-11` | — | `internal/custom/fsharp/tests_route_e2e.go`<br>`internal/engine/http_endpoint_giraffe.go`<br>`internal/engine/httproutes/canonicalize.go` | Test->endpoint route-hit linkage (#4749, F# slice of tail epic; mirrors Crystal/Kemal #4760 and Swift/Vapor #4755): http_endpoint_giraffe.go synthesizes canonical http_endpoint_definition entities from F# web route registrations — Giraffe `GET >=> route "/users" >=> handler` / `routef "/users/%i"` combinator chains (inside `choose [ ... ]`) and Saturn `router { get "/users/:id" handler }` blocks — with FrameworkGiraffe canonicalisation that rewrites Giraffe routef printf placeholders (%i/%s/%O/...) to the positional `{}` wildcard and handles Saturn `:name` colon params. custom/fsharp/tests_route_e2e.go emits one test_suite per F# test file carrying e2e_route_calls from ASP.NET Core TestServer HttpClient verb helpers (client.GetAsync("/path") / PostAsync / ... and HttpRequestMessage(HttpMethod.X, "/path")); the shared language-agnostic engine.linkE2ERouteTestsToEndpoints pass then emits the TESTS edge to the exercised endpoint (proven by TestGiraffe_E2ERouteTestLinkage + TestGiraffe_BasicRoute/_RoutefTypedParam/_SaturnRouter + TestFSharpRouteE2E_*). F# test DSLs use anonymous closure blocks (Expecto `testCase "..." <| fun _ -> ...` / `testList`; xUnit `[<Fact>]`), so the test_suite is the scope-owner carrying the route hits (F# analog of Ruby #4684 / JS #4680 / Crystal #4760). Local-variable/receiver typing (#4749 part a) is N/A: F# is functional — Giraffe handlers are `let`-bound HttpHandler values composed with `>=>`, not obj.method() receiver calls, and the fsharp base extractor names `let` entities by their bare name with no class-qualified receiver resolver to consume a receiver_type stamp; route-string linkage is the coverage mechanism (mirrors functional Elixir #4688 / Crystal #4760). Honest exclusions: interpolated/variable paths, subRoute/forward mount-prefix folding, and Giraffe routeCi/routeStartsWith/routex variants are documented follow-ups. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Metric extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Trace extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Config consumption | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Feature flag gating | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request sink dataflow | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Response shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.fsharp.framework.giraffe ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
