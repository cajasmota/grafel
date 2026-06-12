<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.groovy.framework.grails` — Grails / Ratpack (Groovy HTTP)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [groovy](../by-language/groovy.md)
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
| Endpoint synthesis | ✅ `full` | — | 4914 | `internal/engine/http_endpoint_grails.go`<br>`internal/engine/httproutes/canonicalize.go` | #4914: synthesizeGroovyRoutes (http_endpoint_grails.go) synthesizes canonical http_endpoint_definition entities from Groovy web routes that have NO prior producer (the base groovy.go extractor is Gradle/CALLS-aware but web-framework-blind). Three sources: (a) Grails convention controllers — a `class BookController { def index() {…} }` under grails-app/controllers/ (or any file declaring `class *Controller`) -> ANY /book/index per action method (verb ANY since Grails actions are HTTP-method-agnostic; beforeInterceptor/afterInterceptor lifecycle hooks excluded); (b) explicit UrlMappings.groovy entries `"/book/$id"(controller:'book',action:'show')` -> /book/{id} ($name->{name}, explicit method: honored); (c) Ratpack handler DSL `get('api/books'){…}` -> GET /api/books, `path('x')` -> ANY. Paths are canonicalised via httproutes (FrameworkGrails/FrameworkRatpack, curly/colon). Was wrongly marked fully-missing while actively synthesized — fixture-proven by the engine TestGrails_*/TestRatpack_* suites and the existing tests_linkage TESTS-edge tests that depend on these endpoints. |
| Handler attribution | ✅ `full` | — | 4914 | `internal/engine/http_endpoint_grails.go` | #4914: each synthesized Grails/Ratpack http_endpoint_definition is attributed to its handler — a Grails convention endpoint carries the controller class + action method name (BookController.index -> /book/index), a UrlMappings endpoint carries the mapped controller/action pair, and a Ratpack endpoint carries the verb-DSL handler at its source line. Was wrongly marked missing. |
| Route extraction | ✅ `full` | — | 4914 | `internal/engine/http_endpoint_grails.go` | #4914: route paths/verbs are extracted from all three Groovy web-route forms (Grails controller-convention /<controller>/<action>, explicit UrlMappings.groovy patterns with $-param rewriting, Ratpack get/post/put/delete/patch/options/path handler DSL) by synthesizeGroovyRoutes' grailsActionRe/grailsUrlMappingRe/ratpackRouteRe. Was wrongly marked missing while route_extraction was actively driving endpoint_synthesis. |

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
| Enum extraction | 🟢 `partial` | — | 4914 | `internal/extractors/groovy/types.go`<br>`internal/extractors/groovy/types_test.go` | #4914: the base Groovy tree-sitter extractor (types.go, buildGroovyEnumValueSet) now emits a SCOPE.Enum value-set per `enum` declaration via extractor.EnumEntity(kind_hint=groovy_enum) — one member per constant, with `CONST(<literal>)` single-literal args (int/float/string/bool, quote-stripped) lifted to member values. Value-asserted: TestGroovyTypes_PlainEnumValueSet (Color -> RED,GREEN,BLUE), TestGroovyTypes_ValuedEnum (Status -> ACTIVE=1,INACTIVE=0), TestGroovyTypes_StringValuedEnum, TestGroovyTypes_EnumWithBodyExcludesFieldsAndCtor (Planet keeps only MERCURY,VENUS — not the field/constructor). PARTIAL: multi-arg / computed enum constant values keep only the constant name; the per-constant overriding-method body is not modelled. |
| Interface extraction | 🟢 `partial` | — | 4914 | `internal/extractors/groovy/groovy.go` | #4914: `interface X {…}` IS extracted — the smacker grammar parses it as a class_definition, so the base walk emits it as a SCOPE.Component (with its method declarations as CONTAINS-linked Operations). PARTIAL: the Component subtype is recorded as "class", not distinguished as an interface, and `trait` (parsed as a `declaration` header, not class_definition) is not yet captured. See lang.groovy.base core_extraction. |
| Type alias extraction | 🔴 `missing` | — | 4914 | — | #4914: Groovy has no first-class `typealias`/`typedef` construct (unlike swift/kotlin/dart); the nearest idiom is `import X as Y` which IS captured as an aliased IMPORTS edge (lang.groovy.base import_resolution_quality), not a type-alias node. This dictionary cell stays missing by language design. |
| Type extraction | 🟢 `partial` | — | 4914 | `internal/extractors/groovy/groovy.go`<br>`internal/extractors/groovy/groovy_test.go` | #4914: `class` and `interface` declarations are extracted as SCOPE.Component nodes by the base walk (groovy.go buildClass via class_declaration/class_definition). PARTIAL: only nominal class/interface types are captured — `trait`, `@interface` (annotation type) and generic type parameters are not yet modelled. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI injection point | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI scope resolution | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-11` | — | `internal/custom/groovy/tests_route_e2e.go`<br>`internal/engine/http_endpoint_grails.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/extractors/groovy/groovy.go` | Test->endpoint route-hit linkage (#4749, Groovy slice of tail epic #4615; JVM analog of Java junit5 + Kotlin #4687, mirrors Crystal/Kemal #4760). http_endpoint_grails.go synthesizes canonical http_endpoint_definition entities from Groovy web routes with NO prior producer: Grails convention controllers (class BookController { def show() {…} } -> ANY /book/show, verb ANY since Grails actions are method-agnostic; lifecycle hooks like beforeInterceptor excluded), explicit UrlMappings.groovy (the '/book/$id'(controller:book,action:show) form -> /book/{id}, dollar-params rewritten to curly, explicit method honored), and Ratpack handler DSL (get('api/books'){…} -> GET /api/books, path('x') -> ANY) with curly/colon canonicalisation (FrameworkGrails/FrameworkRatpack). custom/groovy/tests_route_e2e.go emits one test_suite per Groovy *Spec/*Test file carrying e2e_route_calls from Spring MockMvc (mockMvc.perform(get('/path'))), WebTestClient, Ratpack testHttpClient.get(...) and the Grails RestBuilder bare-verb form (get '$baseUrl/books'); the shared language-agnostic engine.linkE2ERouteTestsToEndpoints pass then emits the TESTS edge (proven by TestGrails_*/TestRatpack_* + TestGroovyRouteE2E_* + TestIssue4749_GroovySpockE2ERouteTestsLinkToEndpoints + TestIssue4749_GroovyGrailsAnyVerbE2ELink). Local-variable/receiver typing (#4749 part a) IS implemented for Groovy in the base extractor (internal/extractors/groovy/groovy.go): def c = new FooController(); c.index() -> CALLS FooController.index (TestIssue4749_GroovyLocalVarReceiver_*), with constructor-call phantom suppression and factory/builder-RHS negative-case guard. SCOPE-OWNER: Spock feature methods (def feature-string()) are NOT parsed as methods by the groovy grammar (surface as ERROR nodes), so route hits inside when:/expect: blocks are carried by the suite-level test_suite scope-owner (Groovy analog of Ruby #4684 / JS #4680 / Crystal #4760), NOT a per-feature operation. Honest exclusions: interpolated/variable routes, leading-interpolation routes keep only the static suffix, routeless unit specs emit no suite. |

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
(or use `go run ./tools/coverage update lang.groovy.framework.grails ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
