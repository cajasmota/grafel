# Indexer Capabilities & Coverage Matrix

Canonical reference for everything the archigraph indexer understands —
languages, frameworks, ORMs, build/package systems, message brokers,
observability, security, custom protocols, and configuration. Per-cell
status is one of:

- `✅` — fully implemented and exercised by tests.
- `⚠️` — partial: works in the common case but with documented holes.
- `❌` — not implemented (links to issue when one exists).
- `n/a` — not applicable to the row's context.

Footnotes cite the implementing file(s). The "Gaps to file" section at
the end aggregates every `❌` without a tracking issue.

Cross-cutting addendum: in addition to per-language tree-sitter
extractors, the indexer runs an `_engine` tier of YAML-declared
pattern detectors (40 detectors at `internal/engine/rules/_engine/`)
that are cross-language: cache eviction policies, comment markers,
column schema, connection pool, CSRF heuristic, decorator extractor,
error handling, file upload, framework version, hierarchy, library
boundary, logging config, naming convention, rate limit, redis key,
resilience pattern, SQL injection, transaction changeset, validation
confidence, etc. They are referenced in the appropriate sections below
rather than duplicated here.

Source of truth (commit at audit time): `git log -1 --oneline` against
`origin/main` —
`25c7b2cc feat(extractor/jsts): static enumeration of object-subscript`.

---

## Section 1 — Languages

Tree-sitter (TS) means the extractor uses the `smacker/go-tree-sitter`
binding for that language. REGEX means there is no available tree-sitter
grammar in the vendored bindings (or the team opted out) and the
extractor uses regex-based scanning. Entity & relationship kinds listed
are what the **per-language extractor** emits directly; engine passes
add many more (CALLS line precision is preserved as `Properties["line"]`
when emitted by tree-sitter passes). DISCRIMINATES_ON and NAVIGATES_TO
columns refer to per-language extractor support; both ride on top of
existing AST walks and therefore only land on TS-grammar languages.

| Lang | Grammar | Core entity kinds emitted | Core relationship kinds | CALLS line precision | DISCRIMINATES_ON | NAVIGATES_TO | Notes |
|------|---------|----------------------------|--------------------------|----------------------|-------------------|--------------|-------|
| go         | TS    | Operation, Component (struct/interface), Schema (type_alias) | CALLS, IMPORTS, DEPENDS_ON, IMPLEMENTS, REFERENCES | ✅ [^go-line] | ❌ no issue | ❌ no issue | Method-value `s.M` CALLS with `via_value=true`. [^go-extr] |
| python     | TS    | Operation (function/method), Component (class/module), Schema (field) | CALLS, IMPORTS, CONTAINS, DISCRIMINATES_ON | ✅ [^py-line] | ✅ [^py-disc] | ❌ no issue | QualifiedName module-path-qualified. Migration emit env-gated. [^py-extr] |
| javascript | TS    | Operation, Component, Schema, UIComponent, JSX, Variable | CALLS, IMPORTS, DISCRIMINATES_ON, NAVIGATES_TO, USES_HOOK, HAS_PROPS, RENDERS | ✅ [^js-line] | ✅ [^js-disc] | ✅ [^js-nav] | Shared extractor for JS+TS. |
| typescript | TS    | (same as javascript)        | (same as javascript)     | ✅                   | ✅                | ✅            | Driven by `internal/extractors/javascript` package on `.ts`/`.tsx`. |
| java       | TS    | Operation, Component (class/interface), Schema | CALLS, IMPORTS, EXTENDS, IMPLEMENTS, CONTAINS | ✅ [^java-line] | ❌ no issue | n/a | Annotation routes composed in engine. |
| kotlin     | TS    | Operation, Component, Schema | CALLS, IMPORTS, EXTENDS, IMPLEMENTS | ✅ [^kotlin-line] | ❌ no issue | n/a | Spring/Quarkus annotation passes work cross-Java/Kotlin. |
| ruby       | TS    | Operation (def/method), Component (class/module) | CALLS, IMPORTS (`require`), CONTAINS, EXTENDS | ✅ [^ruby-line] | ❌ no issue | n/a | Eloquent-style ActiveRecord recognized. |
| rust       | TS    | Operation (fn), Component (struct/trait/enum) | CALLS, IMPORTS (`use`), IMPLEMENTS | ✅ [^rust-line] | ❌ no issue | n/a | |
| php        | TS    | Operation, Component (class/interface/trait) | CALLS, IMPORTS (`use`), EXTENDS, IMPLEMENTS | ⚠️ partial — present on most call expressions but not asserted in every fixture | ❌ no issue | n/a | Eloquent helper at `internal/extractors/php/eloquent.go`. |
| csharp     | TS    | Operation, Component (class/interface/struct/record) | CALLS, IMPORTS (`using`), EXTENDS, IMPLEMENTS | ⚠️ partial | ❌ no issue | n/a | |
| scala      | TS    | Operation, Component (class/object/trait) | CALLS, IMPORTS, EXTENDS | ⚠️ partial | ❌ no issue | n/a | |
| swift      | TS    | Operation, Component (class/struct/protocol) | CALLS, IMPORTS, EXTENDS | ⚠️ partial | ❌ no issue | n/a | |
| dart       | TS    | Operation, Component (class/mixin) | CALLS, IMPORTS, EXTENDS | ⚠️ partial | ❌ no issue | n/a | Flutter framework rules present. |
| cpp        | TS    | Operation, Component (class/struct) | CALLS, IMPORTS (`#include`), EXTENDS | ⚠️ partial | ❌ no issue | n/a | |
| clojure    | TS    | Operation (defn), Component (defrecord/defprotocol) | CALLS, IMPORTS (`ns`/`require`) | ⚠️ partial | ❌ no issue | n/a | |
| groovy     | TS    | Operation, Component | CALLS, IMPORTS, EXTENDS | ⚠️ partial | ❌ no issue | n/a | Grails recognised. |
| shell      | TS    | Operation (function), Component (script) | CALLS, IMPORTS (source) | ⚠️ minimal | ❌ no issue | n/a | |
| sql        | TS    | Datastore (table), Schema (column), Operation (proc) | DEFINES, ACCESSES_TABLE | n/a | n/a | n/a | Migrations parsed; CREATE TABLE → Datastore. |
| proto      | TS    | Service (gRPC), Operation (rpc), Schema (message/field) | CONTAINS, RETURNS | n/a | n/a | n/a | Feeds gRPC cross-repo linker. |
| graphql    | TS    | Schema, Operation (Query/Mutation/Subscription) | n/a | n/a | n/a | n/a | SDL extraction. |
| hcl        | TS    | InfraResource, Config | DEPENDS_ON | n/a | n/a | n/a | Terraform/OpenTofu/Vault. |
| yaml       | TS    | Config, Operation (CI step), InfraResource (k8s) | DEPENDS_ON_CONFIG | n/a | n/a | n/a | k8s, GH Actions, GitLab CI parsed semantically by engine passes. |
| html       | TS    | UIComponent (template), Operation (handler ref) | RENDERS | n/a | n/a | n/a | Used by Razor, Vue, etc. |
| css        | TS    | Stylesheet | n/a | n/a | n/a | n/a | |
| vue        | TS    | UIComponent, Operation (SFC `<script>`) | RENDERS, USES_HOOK | ⚠️ | ❌ | ❌ | TS grammar used for the `<script>` block. |
| dockerfile | TS    | InfraResource (image/stage) | DEPENDS_ON, EXPOSES | n/a | n/a | n/a | |
| haskell    | TS    | Operation, Component (data/newtype/class) | CALLS, IMPORTS | ⚠️ | ❌ | n/a | |
| erlang     | TS    | Operation (function), Component (module) | CALLS, IMPORTS | ⚠️ | ❌ | n/a | No `frameworks/` rules dir. |
| crystal    | TS    | Operation, Component | CALLS, IMPORTS | ⚠️ | ❌ | n/a | |
| fsharp     | TS    | Operation, Component (module/type) | CALLS, IMPORTS | ⚠️ | ❌ | n/a | |
| ocaml      | TS    | Operation, Component (module) | CALLS, IMPORTS | ⚠️ | ❌ | n/a | |
| nim        | TS    | Operation (proc), Component (type) | CALLS, IMPORTS | ⚠️ | ❌ | n/a | |
| pony       | TS    | Operation, Component (actor/class) | CALLS, USE | ⚠️ | ❌ | n/a | |
| solidity   | TS    | Operation, Component (contract) | CALLS, IMPORTS | ⚠️ | ❌ | n/a | |
| sml        | TS    | Operation (fun), Component (structure) | CALLS, IMPORTS | ⚠️ | ❌ | n/a | |
| zig        | TS    | Operation (fn), Component (struct) | CALLS, IMPORTS | ⚠️ | ❌ | n/a | |
| verilog    | TS    | Component (module) | n/a | n/a | n/a | n/a | RTL — limited to module-graph. |
| vhdl       | TS    | Component (entity/architecture) | n/a | n/a | n/a | n/a | |
| just       | TS    | Operation (recipe) | DEPENDS_ON | n/a | n/a | n/a | Justfile recipes. |
| razor      | TS    | UIComponent (.razor) | RENDERS | n/a | n/a | n/a | Uses HTML grammar; no tree-sitter-razor. |
| markdown   | REGEX | Document, Heading | CONTAINS | n/a | n/a | n/a | No TS grammar used. |
| fish       | REGEX | Operation (function) | CALLS | ⚠️ | n/a | n/a | |
| lisp       | REGEX | Operation (defn/defun), Component | CALLS, IMPORTS | ⚠️ | n/a | n/a | Common Lisp / Scheme / Racket shared. |
| elm        | REGEX | Operation, Component (module/type) | CALLS, IMPORTS | ⚠️ | n/a | n/a | No bundled TS grammar. |
| reasonml   | REGEX | Operation, Component | CALLS, IMPORTS | ⚠️ | n/a | n/a | No bundled TS grammar. |
| rescript   | TS    | Operation, Component | CALLS, IMPORTS | ⚠️ | ❌ | n/a | Note: comment in extractor says "no TS"; in practice the bundled rescript grammar is used. |
| astro      | REGEX | UIComponent (.astro), Operation (frontmatter) | RENDERS, IMPORTS | ⚠️ | ❌ | ❌ | |
| svelte     | REGEX | UIComponent (.svelte), Operation (`<script>`) | RENDERS, USES_HOOK | ⚠️ | ❌ | ❌ | |
| bazel      | REGEX | InfraResource (target) | BAZEL_DEPENDS_ON | n/a | n/a | n/a | BUILD/BUCK/WORKSPACE — `py_*`, `java_*`, `go_*`, `cc_*`, `<lang>_*` catch-all. Pants & Buck deferred. [^bazel] |
| idris      | REGEX | Operation, Component | CALLS, IMPORTS | ⚠️ | n/a | n/a | |
| elixir     | TS via internal/extractors/elixir | Operation (def/defp), Component (defmodule) | CALLS, IMPORTS (`alias`/`import`/`use`), CONTAINS | ⚠️ | ❌ | n/a | Phoenix routes composed in engine pass. |

[^go-line]: `internal/extractors/golang/call_line_property_test.go` — every CALLS edge gets `Properties["line"]`.
[^go-extr]: `internal/extractors/golang/extractor.go` (kinds documented in file header).
[^py-line]: `internal/extractors/python/call_line_property_test.go`.
[^py-disc]: `internal/extractors/python/discriminator.go` + issue #2654 / #2666 tests.
[^py-extr]: `internal/extractors/python/extractor.go` lines 1411-1421 emit `SCOPE.Schema(subtype=field)` per Django model field.
[^js-line]: `internal/extractors/javascript/call_line_property_test.go`.
[^js-disc]: `internal/extractors/javascript/discriminator.go` (#2654).
[^js-nav]: `internal/extractors/javascript/navigation.go` covers Expo Router, React Navigation, Next.js Link/router, react-router-dom v6+ (#2655, #2658, #2665, #2671).
[^java-line]: `internal/extractors/java/call_line_property_test.go`.
[^kotlin-line]: `internal/extractors/kotlin/call_line_property_test.go`.
[^ruby-line]: `internal/extractors/ruby/call_line_property_test.go`.
[^rust-line]: `internal/extractors/rust/call_line_property_test.go`.
[^bazel]: `internal/extractors/bazel/parser.go` header.

---

## Section 2 — Frameworks (HTTP/RPC routing per language)

Endpoint synthesis = a `synthesize<Framework>` function in the engine
that emits a synthetic `http_endpoint_definition` entity with canonical
path + verb + framework. Handler attribution = the synthesis function
links the synthetic to a handler entity (Controller / Operation /
ViewSet) by reference kind+name; the route-canonicalizer in
`internal/engine/httproutes/canonicalize.go` normalizes the path.
Auth coverage = a dedicated pass that attaches a structured
`auth_policy` to each endpoint. Tests linkage = TESTS edges
auto-synthesized from a test client call to the production handler
(currently only DRF via HTTP-router multi-hop, #2549).

| Framework | Language | Endpoint synthesis | Handler attribution | Auth coverage | Tests linkage | Notes |
|---|---|---|---|---|---|---|
| Django (URLconf) | Python | ✅ [^syn-django] | ✅ ViewSet method via DRF actions | ⚠️ partial (login_required / permission_classes via #1942 follow-up — handler-only, no policy struct) | ⚠️ DRF tests via HTTP-router multi-hop [^tests-drf] | `mount_point` rewrite from #2677. |
| Django REST Framework | Python | ✅ [^syn-django] | ✅ [^drf-act] | ⚠️ partial | ⚠️ [^tests-drf] | DRF actions + cross-file routers. |
| Flask | Python | ✅ [^syn-flask] | ✅ def line | ❌ no issue | ❌ no issue | `@bp.route` + `@bp.<verb>`. |
| FastAPI | Python | ✅ [^syn-fastapi] | ✅ | ❌ no issue | ❌ no issue | `@app.<verb>` and `@router.<verb>`. |
| Starlette | Python | ✅ [^syn-starlette] | ✅ | ❌ no issue | ❌ no issue | From #2690 wave. |
| Tornado | Python | ✅ [^syn-tornado] | ✅ verb-method (`get`/`post` on RequestHandler subclass) | ❌ | ❌ | From #2690. |
| Pyramid | Python | ✅ [^syn-pyramid] | ✅ | ❌ | ❌ | From #2690. |
| Sanic | Python | ❌ no issue yet | ❌ | ❌ | ❌ | YAML rule `frameworks/sanic.yaml` present; no synthesizer wired. |
| Litestar | Python | ❌ no issue yet | ❌ | ❌ | ❌ | YAML rule present; no synthesizer. |
| Bottle | Python | ❌ no issue yet | ❌ | ❌ | ❌ | YAML rule present; no synthesizer. |
| aiohttp | Python | ❌ no issue yet | ❌ | ❌ | ❌ | YAML rule present; no synthesizer. |
| Robyn | Python | ❌ no issue yet | ❌ | ❌ | ❌ | YAML rule present; no synthesizer. |
| Express | JS/TS | ✅ [^syn-express] | ✅ | ❌ | ❌ | |
| Koa | JS/TS | ❌ no issue yet | ❌ | ❌ | ❌ | No YAML rule, no synthesizer; widely deployed — gap. |
| NestJS | JS/TS | ✅ [^syn-nest] | ✅ decorator-based | ⚠️ partial (`@UseGuards` recognised by CSRF/decorator detectors, not surfaced as auth_policy) | ❌ | |
| Fastify | JS/TS | ✅ [^syn-fastify] | ✅ | ❌ | ❌ | |
| Hono | JS/TS | ❌ no issue yet | ❌ | ❌ | ❌ | No YAML rule, no synthesizer. |
| Next.js API routes | JS/TS | ✅ [^syn-nextapi] | ✅ | ❌ | ❌ | Pages + app router. |
| tRPC | JS/TS | ✅ [^syn-trpc] | ✅ | ❌ | ❌ | `.query` → GET, `.mutation` → POST, `.subscription` → SUBSCRIBE (#2693). |
| GraphQL resolvers (Apollo / yoga) | JS/TS | ✅ [^syn-gql] | ✅ | n/a | n/a | Emits one synthetic per resolver field. |
| react-router-dom (NAVIGATES_TO) | JS/TS | ✅ NAVIGATES_TO | n/a | n/a | n/a | Phase 3 #2671. [^js-nav] |
| Next.js Link/router (NAVIGATES_TO) | JS/TS | ✅ NAVIGATES_TO | n/a | n/a | n/a | [^js-nav] |
| Expo Router (NAVIGATES_TO) | JS/TS | ✅ NAVIGATES_TO | n/a | n/a | n/a | #2655 / #2658 / #2665. [^js-nav] |
| React Navigation (NAVIGATES_TO) | JS/TS | ✅ | n/a | n/a | n/a | [^js-nav] |
| Angular | JS/TS | ❌ no issue yet | ❌ | ❌ | ❌ | YAML rule present (`angular.yaml`); no router synth. |
| Remix | JS/TS | ❌ no issue yet | ❌ | ❌ | ❌ | YAML rule present; conventions overlap Next; no synth. |
| Astro | JS/TS | ❌ no issue yet | ❌ | ❌ | ❌ | YAML rule + extractor present; no endpoint synth. |
| Nuxt | JS/TS | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present; no synth. |
| Svelte / SvelteKit | JS/TS | ❌ no issue yet | ❌ | ❌ | ❌ | |
| gin | Go | ✅ [^syn-go-routers] | ✅ via Go AST rewrite [^go-routes] | ❌ | ❌ | Shared `synthesizeGoRouters`. |
| echo | Go | ✅ [^syn-go-routers] | ✅ [^go-routes] | ❌ | ❌ | |
| chi | Go | ✅ [^syn-go-routers] | ✅ [^go-routes] | ❌ | ❌ | #2682. |
| fiber | Go | ✅ [^syn-go-routers] | ✅ [^go-routes] | ❌ | ❌ | #2682. |
| gorilla/mux | Go | ✅ [^syn-gorilla] | ✅ | ❌ | ❌ | #2698. |
| net/http stdlib | Go | ✅ [^syn-stdlib] | ✅ | ❌ | ❌ | #2698. |
| huma | Go | ✅ [^syn-huma] | ✅ | ❌ | ❌ | #2698. |
| beego | Go | ❌ no issue yet | ❌ | ❌ | ❌ | YAML rule present. |
| buffalo | Go | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| fasthttp | Go | ❌ no issue yet | ❌ | ❌ | ❌ | |
| hertz | Go | ❌ no issue yet | ❌ | ❌ | ❌ | |
| iris | Go | ❌ no issue yet | ❌ | ❌ | ❌ | |
| kratos | Go | ❌ no issue yet | ❌ | ❌ | ❌ | |
| revel | Go | ❌ no issue yet | ❌ | ❌ | ❌ | |
| go-zero | Go | ❌ no issue yet | ❌ | ❌ | ❌ | |
| Spring Boot / Spring MVC | Java | ✅ [^syn-spring] (composed from AST pass [^spring-route]) | ✅ | ✅ [^auth-java] | ❌ | `@RestController` + `@RequestMapping` + `@GetMapping/...`. |
| Spring Boot (Kotlin) | Kotlin | ✅ [^spring-kotlin] | ✅ | ✅ [^auth-java] (shares resolver) | ❌ | |
| Spring WebFlux | Java | ✅ (shares Spring synthesis) | ✅ | ⚠️ partial | ❌ | YAML rule present. |
| Quarkus | Java | ✅ via JAX-RS synthesis [^syn-jaxrs] | ✅ | ✅ — `application.properties` security model parsed [^auth-java] | ❌ | |
| Micronaut | Java | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| JAX-RS / Jakarta EE | Java | ✅ [^syn-jaxrs] | ✅ | ✅ via `@PermitAll`/`@RolesAllowed`/`@DenyAll` [^auth-java] | ❌ | |
| Dropwizard | Java | ⚠️ partial — JAX-RS subset only | ⚠️ | ❌ | ❌ | YAML rule present. |
| Play Framework | Java/Scala | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present for both. |
| Vert.x | Java | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Apache Struts | Java | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| GWT | Java | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Vaadin | Java | ❌ no issue yet | ❌ | ❌ | ❌ | |
| Ktor | Kotlin | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| http4k | Kotlin | ❌ no issue yet | ❌ | ❌ | ❌ | |
| Laravel | PHP | ✅ [^syn-laravel] | ✅ | ❌ | ❌ | #2680. |
| Symfony | PHP | ❌ — confirms #2717 (was already listed) | ❌ | ❌ | ❌ | YAML rule present; no synth. |
| Slim | PHP | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| CakePHP | PHP | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| CodeIgniter | PHP | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Yii | PHP | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Laminas / Zend | PHP | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Drupal | PHP | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| WordPress | PHP | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Magento | PHP | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Rails | Ruby | ✅ [^syn-rails] | ✅ via expected controller path resolution | ❌ | ❌ | #2696. |
| Sinatra | Ruby | ✅ [^syn-rails] | ✅ | ❌ | ❌ | #2696. |
| Grape | Ruby | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Hanami | Ruby | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Padrino | Ruby | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Roda | Ruby | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| ASP.NET Core | C# | ✅ [^syn-aspnet] | ✅ | ❌ | ❌ | #2700. |
| ASP.NET MVC | C# | ⚠️ subset of ASP.NET Core synth — attribute routes only | ⚠️ | ❌ | ❌ | YAML rule present. |
| Blazor Server / WebAssembly | C# | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Phoenix | Elixir | ✅ [^syn-phoenix] | ✅ | ❌ | ❌ | #2700. |
| Phoenix LiveView | Elixir | ⚠️ partial — pages indexed as routes; LiveView lifecycle hooks not surfaced | ⚠️ | ❌ | ❌ | YAML present. |
| Absinthe (GraphQL) | Elixir | ⚠️ — covered by GraphQL SDL extractor (`internal/engine/graphql_subscriptions.go`) | ⚠️ | n/a | n/a | YAML present. |
| Rocket | Rust | ✅ [^syn-rocket] | ✅ | ❌ | ❌ | #2700. |
| Axum | Rust | ✅ [^syn-axum] | ✅ | ❌ | ❌ | #2680 partial — covered with multi-router composition. |
| Actix | Rust | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Hyper | Rust | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Warp | Rust | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Tide | Rust | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Poem | Rust | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Gotham | Rust | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Vapor | Swift | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Hummingbird | Swift | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Kitura | Swift | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Perfect | Swift | ❌ no issue yet | ❌ | ❌ | ❌ | |
| Akka HTTP / Pekko HTTP | Scala | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| http4s | Scala | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Finatra | Scala | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Lagom | Scala | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Scalatra | Scala | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Compojure / Ring | Clojure | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Pedestal / Reitit / Luminus | Clojure | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Lapis | Lua | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| OpenResty | Lua | ❌ no issue yet | ❌ | ❌ | ❌ | YAML present. |
| Shelf / Dart Frog / Angel3 / Serverpod | Dart | ❌ no issue yet | ❌ | ❌ | ❌ | YAML rules present. |
| gRPC services | multi | ✅ — Java/Kotlin, Go, Python, Node/TS [^grpc] | ✅ via `SCOPE.GrpcMethod` synthetic | ❌ | ❌ | Same-name cross-repo linkage via `internal/links/grpc_pass.go`. |

[^syn-django]: `synthesizeDjangoFromComposed` — `internal/engine/http_endpoint_synthesis.go:568`. Inputs from `internal/engine/django_routes.go` + `django_urlconf_nested.go` + `django_drf_actions.go`.
[^drf-act]: `internal/engine/django_drf_actions.go`.
[^tests-drf]: `internal/engine/tests_edges.go` — `ApplyTestsMultiHopViaHTTP` (#2549). Currently DRF-specific.
[^syn-flask]: `synthesizeFlask` — `internal/engine/http_endpoint_synthesis.go:722`.
[^syn-fastapi]: `synthesizeFastAPI` — `internal/engine/http_endpoint_synthesis.go:794`.
[^syn-starlette]: `synthesizeStarlette` — `internal/engine/http_endpoint_synthesis.go:863`.
[^syn-tornado]: `synthesizeTornado` — `internal/engine/http_endpoint_synthesis.go:1001`.
[^syn-pyramid]: `synthesizePyramid` — `internal/engine/http_endpoint_synthesis.go:1392`.
[^syn-express]: `synthesizeExpress` — `internal/engine/http_endpoint_synthesis.go:1591`.
[^syn-nest]: `synthesizeNestJS` — `internal/engine/http_endpoint_synthesis.go:1733`.
[^syn-fastify]: `synthesizeFastify` — `internal/engine/http_endpoint_jsts_extra.go`.
[^syn-nextapi]: `synthesizeNextAPIRoute` — `internal/engine/http_endpoint_jsts_extra.go`.
[^syn-trpc]: `synthesizeTRPC` — `internal/engine/http_endpoint_trpc.go:113` (#2693).
[^syn-gql]: `synthesizeGraphQLResolvers` — `internal/engine/http_endpoint_synthesis.go:1797`.
[^syn-go-routers]: `synthesizeGoRouters` — `internal/engine/response_shape_go.go:60`. Framework inferred from imports: gin / echo / chi / fiber.
[^go-routes]: AST receiver-method binding pass — `internal/engine/go_routes.go`.
[^syn-gorilla]: `synthesizeGorillaMux` — `internal/engine/http_endpoint_go_trio.go`.
[^syn-stdlib]: `synthesizeNetHTTPStdlib` — `internal/engine/http_endpoint_go_trio.go`.
[^syn-huma]: `synthesizeHuma` — `internal/engine/http_endpoint_go_trio.go`.
[^syn-spring]: `synthesizeSpringFromComposed` — `internal/engine/http_endpoint_synthesis.go:512`.
[^spring-route]: `internal/engine/spring_routes.go`.
[^spring-kotlin]: `internal/engine/spring_routes_kotlin.go` (#1421).
[^syn-jaxrs]: `synthesizeJAXRS` — `internal/engine/http_endpoint_synthesis.go:677`; annotation composition in `internal/engine/java_annotation_routes.go`.
[^auth-java]: `internal/engine/java_auth_policy.go` — `AuthPolicy` resolver Phase 1 of #1942. Java/Kotlin handler annotations + Quarkus config + framework default. Phases 2-4 (Python / NestJS / Go) deferred.
[^syn-laravel]: `synthesizeLaravel` — `internal/engine/http_endpoint_php_producer.go` (#2680).
[^syn-rails]: `synthesizeRailsRoutes` + `synthesizeSinatra` — `internal/engine/http_endpoint_ruby_producer.go:151,164` (#2696).
[^syn-aspnet]: `synthesizeASPNetCore` — `internal/engine/aspnet_core_routes.go` (#2700).
[^syn-phoenix]: `synthesizePhoenix` — `internal/engine/phoenix_routes.go:285` (#2700).
[^syn-rocket]: `synthesizeRocket` — `internal/engine/rocket_routes.go` (#2700).
[^syn-axum]: `synthesizeAxumRoutes` — `internal/engine/http_endpoint_axum.go`.
[^grpc]: `internal/engine/grpc_edges.go` — `synthesizeJavaGRPC`, `synthesizeGoGRPC`, plus Python and Node/TS sections.

### HTTP client (consumer-side) synthesis

Each language above also has a consumer-side client synthesizer that
emits matching synthetic `http_endpoint_call` entities so the cross-repo
HTTP linker (`internal/links/http_pass.go`) can pair them by Name.

| Language | Client synthesis | File |
|---|---|---|
| JS/TS (fetch/axios) | ✅ | `internal/engine/http_endpoint_synthesis.go` (`synthesizeFetchAxiosWithRuntime`) + `http_endpoint_jsts_client_1483.go` |
| Python (requests/httpx) | ✅ | `http_endpoint_python_client.go` |
| Go (net/http, resty, etc.) | ✅ | `http_endpoint_go_client.go` |
| Java (RestTemplate/WebClient/Feign/Retrofit) | ✅ | `http_endpoint_java_client.go` |
| Kotlin (Retrofit/Ktor client) | ✅ | `http_endpoint_kotlin_client.go` |
| Ruby (Faraday/Net::HTTP/HTTParty) | ✅ | `http_endpoint_ruby_client.go` |
| Rust (reqwest) | ✅ | `http_endpoint_rust_client.go` |
| C# (HttpClient) | ✅ | `http_endpoint_csharp_client.go` |
| PHP (Guzzle/curl) | ✅ | `http_endpoint_php_client.go` |
| Elixir (HTTPoison/Tesla/Req) | ✅ | called from synthesis dispatch (`synthesizeElixirHTTPClients`) |

---

## Section 3 — ORMs and database access

Three orthogonal signals exist per ORM:

1. **Model entity** — whether a model class is recognized as a SCOPE.Component subtype="model"/"entity" by either a per-language extractor or the ormlink cross-pass.
2. **Migration tracking** — whether migrations emit SCOPE.Schema / SCOPE.Datastore entities.
3. **Field-level edges** — whether `READS_FIELD`/`WRITES_FIELD` to `SCOPE.Schema(subtype=field)` are emitted.
4. **Query analysis** — whether call sites get `QUERIES` edges by `internal/engine/orm_queries*.go`.

The "Model→Table" mapping is handled by `internal/extractors/cross/ormlink/extractor.go` (MAPS_TO + BACKED_BY edges). The list below tracks **deep** support; YAML detection rules exist for every entry but on their own only mark a file as belonging to the ORM.

| ORM | Language | Model entity | Migration tracking | Field-level edges | Query analysis (QUERIES) |
|---|---|---|---|---|---|
| Django ORM | Python | ✅ [^orm-django-model] | ✅ [^orm-django-mig] (env-gated emit) | ✅ READS_FIELD/WRITES_FIELD via filter_keys [^orm-django-field] | ✅ [^orm-py] |
| SQLAlchemy | Python | ✅ via ormlink (`__tablename__`) | ⚠️ Alembic detected by YAML; no engine pass | ⚠️ partial — column extractor [^col-schema] for `Column()`/`mapped_column()`; no READS_FIELD wiring | ✅ session.query + select() [^orm-py] |
| SQLModel | Python | ⚠️ YAML only | ❌ | ❌ | ⚠️ overlaps SQLAlchemy via select() pattern |
| Tortoise ORM | Python | ⚠️ YAML only | ❌ | ❌ | ✅ [^orm-py] |
| Peewee | Python | ⚠️ YAML only | ❌ | ❌ | ✅ [^orm-py] |
| Pony ORM | Python | ⚠️ YAML only | ❌ | ❌ | ❌ no issue yet |
| MongoEngine | Python | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Beanie | Python | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Prisma | JS/TS | ✅ via ormlink (`@@map`) + dedicated Prisma rules | ⚠️ Prisma schema rules; no migration entity | ⚠️ column extractor for `model { field Type }` [^col-schema] | ✅ `prisma.<model>.<verb>` [^orm-jsts] |
| TypeORM | JS/TS | ✅ via ormlink (`@Entity`) | ❌ | ⚠️ column extractor for `@Column(...)` [^col-schema] | ✅ Repository.find/save/delete [^orm-jsts] |
| Sequelize | JS/TS | ✅ via ormlink (`sequelize.define`) | ❌ | ❌ | ✅ [^orm-jsts] |
| Mongoose | JS/TS | ⚠️ via ormlink in test fixtures; weak | ❌ | ❌ | ✅ [^orm-jsts] |
| MikroORM | JS/TS | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Drizzle | JS/TS | ⚠️ YAML only | ❌ | ❌ | ❌ — listed as out-of-phase in `orm_queries_jsts.go` |
| Knex | JS/TS | ⚠️ YAML only | ❌ | ❌ | ⚠️ covered as raw-SQL only |
| Supabase | JS/TS | ⚠️ YAML only | ❌ | ❌ | ✅ `<client>.from('<table>').<verb>` [^orm-jsts] |
| GORM | Go | ✅ via ormlink + per-file scan in `orm_queries_other.go` | ❌ | ❌ | ✅ [^orm-other] |
| ent | Go | ⚠️ YAML only | ❌ | ❌ | ❌ — out of phase in `orm_queries_other.go` |
| sqlx | Go | ❌ | ❌ | ❌ | ❌ |
| pgx | Go | ❌ | ❌ | ❌ | ❌ |
| sqlc | Go | ❌ | ❌ | ❌ | ❌ |
| bun | Go | ❌ | ❌ | ❌ | ❌ |
| Hibernate / JPA | Java | ✅ via ormlink (`@Entity` + `@Table`) | ❌ | ⚠️ column extractor for `@Column` [^col-schema] | ⚠️ EntityManager.find + Spring Data repo methods [^orm-other] |
| Spring Data JPA | Java | ✅ via ormlink | ❌ | ⚠️ inherits Hibernate column extractor | ⚠️ [^orm-other] |
| MyBatis | Java | ⚠️ YAML only | ❌ | ❌ | ❌ |
| jOOQ | Java | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Exposed / Ktorm / SQLDelight / Room | Kotlin | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Entity Framework Core | C# | ⚠️ YAML rule + column extractor (EF fluent API) [^col-schema] | ❌ | ⚠️ partial column-level metadata only | ❌ |
| Dapper / NHibernate / npgsql | C# | ⚠️ YAML only | ❌ | ❌ | ❌ |
| ActiveRecord | Ruby | ✅ via ormlink (Rails class naming) | ❌ | ❌ | ✅ [^orm-other] |
| Sequel / mongoid / ROM | Ruby | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Eloquent | PHP | ⚠️ — `internal/extractors/php/eloquent.go` exists with limited extraction | ❌ | ❌ | ❌ — out of phase in `orm_queries_other.go` |
| Doctrine | PHP | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Propel / RedBeanPHP | PHP | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Diesel | Rust | ⚠️ YAML only | ❌ | ❌ | ❌ — out of phase |
| SeaORM | Rust | ⚠️ YAML only | ❌ | ❌ | ❌ |
| sqlx (Rust) | Rust | ⚠️ YAML only | ❌ | ❌ | ❌ |
| rusqlite | Rust | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Ecto | Elixir | ✅ via ormlink (`schema "name" do`) | ❌ | ❌ | ❌ |
| Slick / Doobie / Quill / ScalikeJDBC | Scala | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Drift / Floor / Isar / Hive | Dart | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Core Data / SwiftData / GRDB / Realm | Swift | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Datomic / next.jdbc / HoneySQL / Korma | Clojure | ⚠️ YAML only | ❌ | ❌ | ❌ |
| Raw SQL (cross-language) | * | n/a | n/a | n/a | ✅ via `internal/extractors/cross/dbmap/` — emits SCOPE.DataAccess + ACCESSES_TABLE for `SELECT/INSERT/UPDATE/DELETE/JOIN` against string literals across all languages [^dbmap] |
| MongoDB aggregate stages | * | n/a | n/a | n/a | ✅ via `_engine/mongodb_aggregate_extractor.yaml` |
| Redis key patterns | Python/Node/Java/Go | n/a | n/a | n/a | ✅ via `_engine/redis_key_extractor.yaml` |

[^orm-django-model]: `internal/extractors/python/django_relational.go` + `extractor.go` (Schema field emission lines 1411-1421).
[^orm-django-mig]: `internal/extractors/python/django_migration.go`; `ARCHIGRAPH_EMIT_MIGRATION_ENTITIES` env-gates emission of Migration entities.
[^orm-django-field]: `internal/engine/orm_field_edges.go` (#2279). Phase A intra-file only.
[^orm-py]: `internal/engine/orm_queries_python.go` (#723).
[^orm-jsts]: `internal/engine/orm_queries_jsts.go`.
[^orm-other]: `internal/engine/orm_queries_other.go` — Go gorm, Java JPA + Spring Data, Ruby ActiveRecord.
[^col-schema]: `internal/engine/rules/_engine/column_schema_extractor.yaml` — covers hibernate, sqlalchemy, prisma, typeorm, efcore.
[^dbmap]: `internal/extractors/cross/dbmap/extractor.go` + `orms.go` — GORM, database/sql, SQLAlchemy, psycopg2, Hibernate/JPA, ActiveRecord, Ecto, Prisma, TypeORM, Sequelize, Diesel.

---

## Section 4 — Build, package, and dependency systems

Per-manifest extraction emits a SCOPE.Component entity per declared
dependency and a DEPENDS_ON(kind=external_dependency) edge from the
project node. Usage analysis (used / unused / phantom) is provided by
`internal/extractors/cross/deplinker/`.

| Manifest / format | Language | Extraction | Lock-file parsing | Usage analysis | Notes |
|---|---|---|---|---|---|
| package.json (npm/yarn/pnpm) | JS/TS | ✅ [^mani] | ❌ | ✅ [^deplink] | `dependencies` + `devDependencies` parsed. |
| go.mod | Go | ✅ [^mani] | ⚠️ go.sum not parsed | ✅ [^deplink] | |
| Cargo.toml | Rust | ✅ [^mani] | ❌ Cargo.lock not parsed | ✅ [^deplink] | |
| pyproject.toml | Python | ✅ [^mani] | ❌ poetry.lock / uv.lock not parsed | ✅ [^deplink] | Detected as `pip` regardless of poetry/uv backend. |
| requirements.txt | Python | ✅ [^mani] | n/a | ✅ [^deplink] | |
| Pipfile / Pipfile.lock | Python | ❌ no issue yet | ❌ | ❌ | Not in manifest dispatcher. |
| pom.xml | Java | ✅ [^mani] | n/a | ✅ [^deplink] | |
| build.gradle / build.gradle.kts | Java/Kotlin | ❌ no issue yet | ❌ | ❌ | YAML rule recognises files; no manifest parser. |
| Gemfile | Ruby | ✅ [^mani] | ❌ Gemfile.lock not parsed | ✅ [^deplink] | |
| composer.json | PHP | ❌ no issue yet | ❌ | ❌ | YAML present; no manifest parser. |
| Cargo workspace | Rust | ⚠️ — single Cargo.toml only; workspace members not walked | n/a | ⚠️ | |
| pubspec.yaml | Dart | ✅ [^mani] | ❌ pubspec.lock not parsed | ✅ [^deplink] | |
| .csproj / packages.config | C# | ❌ no issue yet | ❌ | ❌ | YAML rule present; no manifest parser. |
| mix.exs | Elixir | ❌ no issue yet | ❌ | ❌ | YAML present. |
| Podfile / Package.swift | Swift | ❌ no issue yet | ❌ | ❌ | `internal/extractors/swift/package.go` exists but emits no dependency entities. |
| build.sbt | Scala | ❌ no issue yet | ❌ | ❌ | |
| deps.edn / project.clj | Clojure | ❌ no issue yet | ❌ | ❌ | |
| BUILD / BUCK / WORKSPACE | Bazel | ✅ — emits BAZEL_DEPENDS_ON edges between targets [^bazel] | n/a | n/a | Pants and Buck deferred to M6a/M6b. |
| Dockerfile | n/a | ✅ via dockerfile extractor + config-discovery promote | n/a | n/a | InfraResource emitted. |
| docker-compose.yml | n/a | ✅ via config-discovery + docker frameworks YAML | n/a | n/a | Service edges; depends_on graph emitted. |
| Makefile | n/a | ⚠️ config-discovery promotes to SCOPE.Config; no target graph extraction | n/a | n/a | |
| Justfile | n/a | ✅ recipe extraction [^just] | n/a | n/a | |

[^mani]: `internal/extractors/cross/manifest/extractor.go` — supported filenames `package.json`, `go.mod`, `Cargo.toml`, `pyproject.toml`, `pom.xml`, `requirements.txt`, `pubspec.yaml`, `Gemfile`.
[^deplink]: `internal/extractors/cross/deplinker/extractor.go` — `used | unused | phantom` triad against IMPORTS edges.
[^just]: `internal/extractors/just/just.go`.

---

## Section 5 — Message brokers, async, and event systems

Each row is a producer/consumer detection pass that emits a
`SCOPE.MessageTopic` (or broker-specific kind) by canonicalized topic
name + broker prefix; cross-repo linkage joins by Name (P7,
`internal/links/topic_pass.go`).

| Broker / system | Producer / Consumer extraction | Topic linkage | Schema awareness | Cross-repo edges | Notes |
|---|---|---|---|---|---|
| Apache Kafka | ✅ [^msg-kafka] | ✅ broker prefix `kafka:` | ⚠️ schema-registry not parsed | ✅ via P7 | Wave 1 of #726. Wrapper detection at `kafka_wrapper_edges.go`. |
| RabbitMQ | ✅ [^msg-rabbit] | ✅ `rabbitmq:` | ❌ | ✅ | Wave 2. |
| AWS SQS | ✅ [^msg-sqs] | ✅ `sqs:` | ❌ | ✅ | Wave 2. Includes IaC SNS→SQS fan-out [^msg-sns]. |
| AWS SNS (IaC-declared) | ✅ [^msg-sns] | ✅ via SNS→SQS link | ❌ | ✅ | #1596. |
| GCP Pub/Sub | ✅ [^msg-pubsub] | ✅ `gcppubsub:` | ❌ | ✅ | Wave 3. |
| NATS | ✅ [^msg-nats] | ✅ `nats:` | ❌ | ✅ | Wave 3. |
| Apache Pulsar | ✅ [^msg-pulsar] | ✅ `pulsar:` | ❌ | ✅ | #515. |
| Redis pub/sub + Streams | ✅ [^msg-redis] | ✅ `redis:` | ❌ | ✅ | #930. |
| AWS EventBridge | ✅ [^msg-eventbus] | ✅ `event:eventbridge:` | ❌ | ✅ | #927 — emits `SCOPE.EventBusEvent`. |
| Azure Event Grid | ✅ [^msg-eventbus] | ✅ `event:eventgrid:` | ❌ | ✅ | #927. |
| CloudEvents | ✅ [^msg-eventbus] | ✅ `event:cloudevents:` | ❌ | ✅ | #927. |
| Celery (Python) | ✅ [^msg-celery] | ✅ — task name as topic | n/a | ✅ via P7 | Publisher edges via `.delay()` / `.apply_async()` / `send_task("name")`. |
| BullMQ / bull (Node) | ✅ via scheduled_jobs pass [^sched] | ✅ — queue name | n/a | ⚠️ via topic pass | |
| Sidekiq (Ruby) | ⚠️ — YAML rule present; no producer/consumer edge pass | ❌ | n/a | ❌ | No issue yet. |
| Dramatiq (Python) | ⚠️ — YAML rule present; not in `scheduled_jobs_edges.go` dispatch | ❌ | n/a | ❌ | No issue yet. |
| Debezium / Kafka Connect | ✅ [^msg-cdc] | ✅ via CDC connector entity | ⚠️ table→topic mapping; column drift not tracked | ✅ | #1708. |
| Django signals | ✅ [^msg-djsig] | ✅ signal-name pubsub | n/a | ⚠️ intra-repo only | |
| Event bus generic (`event_bus_edges.go`) | ✅ — managed cloud event buses | ✅ | ❌ | ✅ | |
| Webhook inbound | ✅ [^msg-webhook] — Stripe, GitHub, Twilio, Slack, SendGrid, Mailgun, and more | ✅ — tags `http_endpoint.is_webhook=true` + SUBSCRIBES_TO to external | n/a | n/a | Confidence scoring (high/med/low). |
| WebSocket | ✅ [^msg-ws] — emits SCOPE.WebSocketChannel + WS_SUBSCRIBES_TO / WS_EMITS / WS_CONNECTS | n/a | ❌ | ⚠️ cross-repo by channel name | #727. |
| Server-Sent Events (SSE) | ✅ [^msg-sse] — STREAMS_FROM / STREAMS_TO | n/a | n/a | n/a | #727. |
| GraphQL subscriptions | ✅ [^msg-gqlsub] — GRAPHQL_PUBLISHES / GRAPHQL_SUBSCRIBES | ✅ — `graphql_sub:<field>` | ✅ via SDL extraction | ✅ | #727. |
| Apache Kafka Streams / Faust | ❌ no issue yet | ❌ | ❌ | ❌ | |
| ActiveMQ / Artemis | ❌ no issue yet | ❌ | ❌ | ❌ | |
| Solace | ❌ no issue yet | ❌ | ❌ | ❌ | |
| MQTT | ❌ no issue yet | ❌ | ❌ | ❌ | |
| ZeroMQ | ❌ no issue yet | ❌ | ❌ | ❌ | |
| ROS topics | n/a | n/a | n/a | n/a | Out of scope. |

[^msg-kafka]: `internal/engine/kafka_edges.go` + `kafka_wrapper_edges.go`.
[^msg-rabbit]: `internal/engine/rabbitmq_edges.go`.
[^msg-sqs]: `internal/engine/sqs_edges.go`.
[^msg-sns]: `internal/engine/iac_sns_edges.go` (#1596).
[^msg-pubsub]: `internal/engine/pubsub_edges.go`.
[^msg-nats]: `internal/engine/nats_edges.go`.
[^msg-pulsar]: `internal/engine/pulsar_edges.go`.
[^msg-redis]: `internal/engine/redis_pubsub_edges.go` (#930).
[^msg-eventbus]: `internal/engine/event_bus_edges.go` (#927).
[^msg-celery]: `internal/engine/scheduled_jobs_edges.go` (`synthesizeCeleryCallSiteEdges`) + `internal/extractors/python/celery.go`.
[^sched]: `internal/engine/scheduled_jobs_edges.go`. Frameworks covered: Celery, APScheduler, schedule (Py), node-cron, bull/bullmq, Quarkus @Scheduled, Spring @Scheduled, Java Quartz, Go robfig/cron, AWS EventBridge schedule, Kubernetes CronJob, GitHub Actions schedule.
[^msg-cdc]: `internal/engine/debezium_cdc_edges.go` (#1708).
[^msg-djsig]: `internal/engine/django_signal_pubsub_edges.go`.
[^msg-webhook]: `internal/engine/webhooks_edges.go` (#728).
[^msg-ws]: `internal/engine/websocket_edges.go` (#727).
[^msg-sse]: `internal/engine/sse_edges.go` (#727).
[^msg-gqlsub]: `internal/engine/graphql_subscriptions.go` (#727).

### Workflow / orchestration engines

| Engine | Workflow extraction | Activity extraction | Invocation edges | Cross-repo | File |
|---|---|---|---|---|---|
| Temporal (Python, Go, Java, Kotlin) | ✅ | ✅ | ✅ STARTS_WORKFLOW + EXECUTES_ACTIVITY | ✅ | `internal/engine/workflow_edges.go` (#934) |
| Cadence (Java) | ✅ | ✅ | ✅ | ⚠️ | same |
| AWS Step Functions (ASL JSON / CDK / TF / CFN) | ✅ — emits SCOPE.StateMachine | ✅ via Task states → Lambda | ✅ STEPFUNCTION_STEP_INVOKES | ✅ | same |
| Camunda BPMN | ❌ no issue yet | ❌ | ❌ | ❌ | |
| Airflow DAGs | ⚠️ partial — `etl_pipeline_1638_test.go` exercises some DAG patterns; no dedicated extractor | ❌ | ❌ | ❌ | |

### Serverless

| Provider | Invoker (CALLS) | Handler (HANDLES) | Cross-repo identity | File |
|---|---|---|---|---|
| AWS Lambda | ✅ | ✅ | `aws-lambda:<name>` | `internal/engine/serverless_edges.go` (#925) |
| Google Cloud Functions | ✅ | ✅ | `gcp-cloudfunction:<name>` | same |
| Azure Functions | ✅ | ✅ | `azure-function:<name>` | same |
| Cloudflare Workers | ❌ no issue yet | ❌ | ❌ | |
| Vercel Functions | ⚠️ via Next.js API synth — handler side only | ⚠️ | ❌ | |

---

## Section 6 — Observability, logging, and metrics

| Area | Coverage | File |
|---|---|---|
| Generic logging-config extractor (Python `logging`, Go `slog`/standard log, Node `winston`/`pino`, .NET NLog/Serilog, log4j.xml/logback.xml) | ✅ — emits SCOPE.Pattern(kind=logging_config) | `internal/engine/rules/_engine/logging_config_extractor.yaml` |
| Comment markers (TODO/FIXME/HACK/XXX) | ✅ all languages | `_engine/comment_marker_extractor.yaml` |
| Error-handling style detector (try/catch, Go `err != nil`, Rust `?`/match) | ✅ Java/JS/TS/Python/Go/Rust/Elixir | `_engine/error_handling_detector.yaml` |
| Resilience patterns (Resilience4j, Hystrix, Polly, sony/gobreaker, afex/hystrix-go) | ✅ Java/Kotlin/C#/Go | `_engine/resilience_pattern_extractor.yaml` |
| Rate-limit middleware/decorators (express-rate-limit, Django, Spring, ASP.NET, golang.org/x/time/rate, Flask-Limiter) | ✅ | `_engine/rate_limit.yaml` |
| Cache eviction policies (Spring @Cacheable/@CacheEvict, Caffeine/Guava/EhCache, Redis maxmemory-policy/EXPIRE, node-cache, lru-cache, ioredis, cachetools, Django CACHES, Flask-Caching, groupcache/bigcache/ristretto, .NET MemoryCache, Rails.cache/Dalli) | ✅ | `_engine/cache_eviction_detector.yaml` |
| Connection pool config (HikariCP, Druid, pgbouncer, SQLAlchemy, Django) | ✅ | `_engine/connection_pool_extractor.yaml` |
| OpenTelemetry instrumentation | ❌ no issue yet — only project-internal use of `otel` is recognised in extractor spans; no edge emission for application traces. | not implemented |
| Prometheus client | ❌ no issue yet | |
| Sentry SDK | ❌ no issue yet | |
| Datadog APM/StatsD | ❌ no issue yet | |
| New Relic | ❌ no issue yet | |
| Honeycomb / Lightstep | ❌ no issue yet | |
| Structured log event extraction (correlating log statements → events) | ❌ no issue yet | |
| Log format catalog (per-service log shape inventory) | ❌ no issue yet | |

---

## Section 7 — Security primitives

| Primitive | Coverage | File |
|---|---|---|
| Auth policy resolver (handler-level annotation/class inheritance + framework config + framework default) | ✅ Java/Kotlin only (Phase 1 of #1942). Phases 2-4 (Python / NestJS / Go) explicitly deferred. | `internal/engine/java_auth_policy.go` |
| `@PermitAll`, `@DenyAll`, `@RolesAllowed`, `@Secured`, `@PreAuthorize` | ✅ Java/Kotlin | same |
| Quarkus `quarkus.http.auth.permission.*` config | ✅ | same |
| Spring Security `@Secured` / `@PreAuthorize` | ✅ | same |
| Django auth (`login_required`, `permission_classes`) | ⚠️ recognised by framework YAML (`python/frameworks/django.yaml`); not lifted into AuthPolicy | |
| Flask-Login / FastAPI `Depends(get_current_user)` | ❌ no issue yet | |
| NestJS `@UseGuards` | ⚠️ extracted as decorator; no auth_policy struct | |
| Express middleware-chain auth (`passport.authenticate`) | ❌ no issue yet | |
| Rails `before_action :authenticate_user!` | ❌ no issue yet | |
| ASP.NET `[Authorize]` | ❌ no issue yet | |
| JWT verify call recognition | ⚠️ recognised opportunistically by webhook signature-verification heuristic | `webhooks_edges.go` |
| OAuth flow recognition | ❌ no issue yet | |
| RBAC policy graph | ❌ no issue yet | |
| CSRF heuristic detector (Spring Security, csurf, NestJS CsrfGuard, Django csrf_exempt, Rails protect_from_forgery, Laravel VerifyCsrfToken, Symfony csrf_protection, Go gorilla/csrf, nosurf) | ✅ | `_engine/csrf_heuristic_detector.yaml` |
| SQL injection heuristic (f-string / `.format()` / `%` interpolation into SQL) | ✅ | `_engine/sql_injection_detector.yaml` |
| File-upload endpoint detector (multer, formidable, busboy, NestJS `@UploadedFile`, FastAPI UploadFile, Flask `request.files`, Django `request.FILES`, Spring `@RequestPart`, Apache Commons FileUpload, Go `r.FormFile`, .NET IFormFile, Rails Active Storage / CarrierWave / Shrine) | ✅ | `_engine/file_upload_detector.yaml` |
| Crypto primitive recognition (hash/sign/verify) | ❌ no issue yet — only as part of webhook signature heuristics | |
| Validation lib recognition (Joi, zod, class-validator, pydantic, marshmallow, FluentValidation, validate.js, yup) | ⚠️ — `validation_confidence_enricher.yaml` exists as a confidence enricher; no first-class Validator entity emitted | `_engine/validation_confidence_enricher.yaml` |
| Secret material extraction | ✅ — `internal/secrets/` runs as a Phase 1 security audit | `internal/secrets/` (see `archigraph-security-audit` skill) |

---

## Section 8 — Custom / domain-specific

| Surface | Coverage | File |
|---|---|---|
| GraphQL SDL (Query/Mutation/Subscription) | ✅ — extractor + engine subscription pass | `internal/extractors/graphql/` + `internal/engine/graphql_subscriptions.go` |
| Apollo Server / Yoga resolvers | ✅ | `synthesizeGraphQLResolvers` |
| Apollo Client / urql / Relay / graphql-request | ⚠️ — YAML rules detect; consumer-side edge emission only for subscriptions | `internal/engine/rules/graphql/frameworks/*.yaml` |
| Hasura | ⚠️ — YAML rule detect; no live runtime introspection | `graphql/frameworks/hasura.yaml` |
| AWS AppSync | ⚠️ same | `graphql/frameworks/aws_appsync.yaml` |
| OpenAPI spec | ✅ — operations extracted; cross-repo linker pairs with handlers and clients | `internal/links/openapi_pass.go` + `internal/engine/rules/openapi/language.yaml` |
| Protobuf .proto | ✅ — service/rpc/message extraction; feeds gRPC linker | `internal/extractors/proto/` + `internal/engine/grpc_edges.go` |
| gRPC services (Java/Kotlin, Go, Python, Node/TS) | ✅ | `internal/engine/grpc_edges.go` (#725) |
| Terraform / OpenTofu | ✅ — HCL extractor + framework rules | `internal/extractors/hcl/` + `internal/engine/rules/hcl/frameworks/terraform.yaml` |
| Vault / Nomad / Packer / Waypoint / OpenTofu | ✅ via HCL extractor + per-framework YAML | `internal/engine/rules/hcl/frameworks/*` |
| AWS CDK | ✅ — dedicated language ruleset | `internal/engine/rules/cdk/frameworks/aws_cdk.yaml` |
| Pulumi | ✅ — dedicated language ruleset | `internal/engine/rules/pulumi/frameworks/pulumi.yaml` |
| CloudFormation | ⚠️ — recognised by Step Functions ASL pass; no first-class resource extraction | `workflow_edges.go` |
| Kubernetes manifests | ✅ — kubernetes extractor + framework rule | `internal/engine/rules/kubernetes/frameworks/kubernetes_manifests.yaml` |
| Helm charts | ❌ no issue yet | |
| Ansible playbooks (core / lint / navigator / molecule / AWX) | ✅ via YAML rules | `internal/engine/rules/ansible/frameworks/` |
| Crossplane / Argo CD / Flux | ❌ no issue yet | |
| tRPC procedures | ✅ — `synthesizeTRPC` | `internal/engine/http_endpoint_trpc.go` |
| MongoDB aggregation pipelines | ✅ — `_engine/mongodb_aggregate_extractor.yaml` | |

---

## Section 9 — Configuration / metadata

| Surface | Coverage | File |
|---|---|---|
| .env (names only — values stripped at extraction boundary) | ✅ — emitted as SCOPE.Config with env-var name list; values never enter the graph | `internal/extractors/config/discover.go` (test `TestDiscover_EnvNeverLeaksValues` enforces the boundary) |
| .yaml / .yml | ✅ — yaml extractor + config-discovery promotion | `internal/extractors/yaml/` + `config/discover.go` |
| .toml | ✅ — pyproject + Cargo paths parsed; other toml promoted as Config | `manifest/extractor.go` + `config/discover.go` |
| .properties | ✅ — application.properties parsed by Quarkus auth pass + promoted as Config | `java_auth_policy.go` |
| .ini / setup.cfg / flake8 / mypy / pytest.ini | ✅ — promoted as SCOPE.Config; no semantic parsing | `config/discover.go` |
| ConstantBinding emission (literal → reference graph edges) | ⚠️ — discriminator extractor captures comparison constants per-function; no dedicated ConstantBinding pass | `discriminator.go` per language |
| Webpack config | ❌ no issue yet | `_engine/react_nextjs_enricher.yaml` covers Next-specific config only |
| Vite config | ❌ no issue yet | same |
| Next.js config (next.config.js) | ⚠️ partial — recognised by react/nextjs enricher | `_engine/react_nextjs_enricher.yaml` |
| tsconfig.json | ⚠️ promoted as Config; no semantic parsing of `paths` for module resolution | `config/discover.go` |
| jest.config / vitest.config / playwright.config | ❌ no issue yet | |
| eslintrc / prettierrc | ⚠️ promoted as Config only | |
| Docker Dockerfile | ✅ | `internal/extractors/dockerfile/` |
| docker-compose.yml service-edge extraction | ✅ via framework rules | `engine/rules/docker/frameworks/docker_compose.yaml` |
| GitHub Actions workflows (`.github/workflows/*.yml`) | ✅ — `synthesizeGitHubActionsSchedule` + framework YAML | `scheduled_jobs_edges.go` + `engine/rules/cicd/frameworks/github_actions.yaml` |
| GitLab CI | ⚠️ — YAML framework rule present; no first-class step graph | `engine/rules/cicd/frameworks/gitlab_ci.yaml` |
| CircleCI / Azure Pipelines / Bitbucket Pipelines / Buildkite / Travis CI | ⚠️ same — YAML rules only | `engine/rules/cicd/frameworks/*.yaml` |
| Jenkins (Jenkinsfile / Pipeline DSL) | ❌ no issue yet | |

---

## Cross-cutting `_engine` detectors

These 40 detectors run cross-language as YAML-registered pattern
detectors. Each emits SCOPE.Pattern or augments existing entities with
structured properties:

cache_eviction_detector, column_schema_extractor,
comment_marker_extractor, connection_pool_extractor,
consumes_api_enricher, coupling_score_enricher,
csrf_heuristic_detector, dead_module_detector, decorator_extractor,
deployment_topology, error_handling_detector, file_upload_detector,
framework_version_enricher, hierarchy_extractor, lib_boundary_enricher,
logging_config_extractor, migration_sequence_enricher,
mongo_query_enricher, mongodb_aggregate_extractor,
naming_convention_detector, onboarding_entry_enricher, pattern_taxonomy,
port_aggregation, property_test_detector, rate_limit, re_export_detector,
react_nextjs_enricher, redis_key_extractor,
resilience_pattern_extractor, shared_test_helper_detector,
singleton_detector, snapshot_test_detector, sql_injection_detector,
sql_join_count_extractor, test_fixture_detector, test_quality_enricher,
transaction_changeset_enricher, type_alias_extractor,
unused_dependency_enricher, validation_confidence_enricher.

All registrations live in `internal/engine/rules/_engine/`.

---

## Cross-repo linkers (`internal/links/`)

| Pass | Method | Identity | Implemented |
|---|---|---|---|
| P1 (`import_pass.go`) | import | external package name | ✅ |
| P3 (`label_pass.go`) | label | shared label | ✅ |
| P4 (`http_pass.go`) | http | `http:<VERB>:<canonical-path>` | ✅ |
| P5 (`openapi_pass.go`) | openapi | spec operation | ✅ |
| P6 (`grpc_pass.go`) | grpc | `grpc:<Service>/<Method>` | ✅ |
| P7 (`topic_pass.go`) | topic | broker-prefixed topic | ✅ |
| Phantom edges (`phantom_edges.go`) | inferred | resolution of unresolved fetches | ✅ |
| Same-as (`sameas_pass.go`) | sameas | repo-aliased same entity | ✅ |
| String pass (`string_pass.go`) | string | shared string literal | ✅ |

---

## Gaps to file as issues

These ❌ rows have no tracking issue and are candidates for the
follow-up backlog generated by this audit.

### Languages
- DISCRIMINATES_ON support for Go, Java, Kotlin, Ruby, Rust, PHP, C#, Scala, Swift, Dart, C++, Clojure (extending the python/javascript pattern from #2654/#2666).
- NAVIGATES_TO support for Swift/SwiftUI, Kotlin/Jetpack Compose, Flutter (Navigator/GoRouter), Angular Router, Vue Router.
- Tree-sitter substitution for Haskell, Erlang, F#, Crystal, OCaml, Solidity, ReasonML, Idris, Elm (currently regex / unbundled). File one issue per language.

### Frameworks (HTTP/RPC)
- Python: Sanic, Litestar, Bottle, aiohttp, Robyn — YAML rules exist, no synthesizers.
- JS/TS: Koa, Hono, Angular routes, Remix, Astro, Nuxt, Svelte/SvelteKit endpoint synthesis.
- Go: beego, buffalo, fasthttp, hertz, iris, kratos, revel, go-zero.
- Java: Micronaut, Play Framework, Vert.x, Apache Struts, GWT, Vaadin.
- Kotlin: Ktor, http4k.
- PHP: Symfony, Slim, CakePHP, CodeIgniter, Yii, Laminas, Drupal, WordPress, Magento.
- Ruby: Grape, Hanami, Padrino, Roda.
- C#: Blazor, ASP.NET MVC (full coverage beyond attribute routes).
- Elixir: Phoenix LiveView lifecycle hooks.
- Rust: Actix, Hyper, Warp, Tide, Poem, Gotham.
- Swift: Vapor, Hummingbird, Kitura, Perfect.
- Scala: Akka HTTP/Pekko, http4s, Finatra, Lagom, Scalatra.
- Clojure: Compojure, Ring, Pedestal, Reitit, Luminus.
- Lua: Lapis, OpenResty.
- Dart: Shelf, Dart Frog, Angel3, Serverpod.

### Auth coverage
- Phase 2 of #1942: Python (Django auth, FastAPI Depends).
- Phase 3 of #1942: NestJS `@UseGuards` → AuthPolicy.
- Phase 4 of #1942: Go middleware chains.
- Ruby `before_action`, ASP.NET `[Authorize]`, Express `passport.authenticate`, Flask-Login.
- OAuth flow recognition, RBAC policy graph, crypto-primitive recognition.

### Tests linkage
- Extend `tests_edges.go` HTTP multi-hop pass beyond DRF to: pytest+requests/httpx against FastAPI/Flask, supertest against Express/Nest/Fastify, Go testing+httptest, Spring `MockMvc`, RSpec request specs, Laravel HTTP tests, PHPUnit, xUnit/MSTest against ASP.NET.

### ORMs
- Out-of-phase work explicitly enumerated in `internal/engine/orm_queries_jsts.go` / `_other.go`: Drizzle, MikroORM EntityManager, ent, Hibernate `session.get`, MyBatis, jOOQ, Exposed, Ktorm, Eloquent, Doctrine, Diesel, SeaORM, sqlx (Rust/Go), EF Core, Dapper, Pony, MongoEngine, Beanie, Sequel, mongoid, ROM, Slick, Doobie, Quill, ScalikeJDBC, Drift, Floor, Isar, Hive, Core Data, SwiftData, GRDB, Realm, Datomic, next.jdbc, HoneySQL, Korma.
- Migration entity tracking for everything except Django ORM.
- READS_FIELD/WRITES_FIELD generalization beyond Django Phase A intra-file.

### Build / package
- Gradle (.gradle/.gradle.kts), composer.json, csproj, mix.exs, build.sbt, deps.edn, Pipfile, Package.swift, Podfile — manifest parsers + lock-file parsing.
- Cargo workspace member walking.

### Message brokers
- Kafka Streams / Faust, ActiveMQ/Artemis, Solace, MQTT, ZeroMQ — producer/consumer edge passes.
- Cloudflare Workers serverless edges.
- Sidekiq + Dramatiq cross-repo topic linkage.
- Schema-registry awareness for Kafka.
- Camunda BPMN, full Airflow DAG extraction.

### Observability
- OpenTelemetry, Prometheus client, Sentry, Datadog APM/StatsD, New Relic, Honeycomb, Lightstep — span/event/metric extraction.
- Structured log event correlation; per-service log format catalog.

### Security
- All non-Java/Kotlin auth (see Phase 2-4 above).
- Crypto primitive recognition surface (hash/sign/verify call graph beyond webhook heuristics).
- First-class Validator entity emission for Joi/zod/class-validator/pydantic/marshmallow/FluentValidation/yup/validate.js.

### Config / metadata
- Helm charts; Crossplane / Argo CD / Flux; Jenkinsfile pipeline graph.
- Semantic parsing of webpack/vite/jest/vitest/playwright configs.
- tsconfig.json `paths` resolution into IMPORTS edges.
- First-class ConstantBinding pass (literal → reference).
- GitLab CI / CircleCI / Azure Pipelines / Bitbucket / Buildkite / Travis CI step graph (currently YAML-detection only).

---

*Living document. Update on every PR that adds an extractor, framework
synthesizer, ORM matcher, broker pass, or cross-repo linker.*
