<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.spring-graphql` — Spring for GraphQL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 55

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-02` | — | `internal/custom/java/patterns_dispatch.go`<br>`internal/custom/java/spring_graphql.go` | @QueryMapping/@MutationMapping/@SubscriptionMapping + @SchemaMapping(typeName=Query|Mutation|Subscription) controller methods to canonical GRAPHQL endpoint /graphql/<Operation>/<field> (verb GRAPHQL), identical shape to gqlgen/HotChocolate/graphql-kotlin/JS so GraphQL client links (#3667) join. Value-asserted users/createUser/events/allUsers/node in TestSpringGraphQL_*. |
| Handler attribution | ✅ `full` | `2026-06-02` | — | `internal/custom/java/patterns_dispatch.go`<br>`internal/custom/java/spring_graphql.go` | Each endpoint emits handler_name=<Controller>.<method> + resolver_method and a HANDLES edge endpoint to resolver SCOPE.Operation, preserved under name=/field= rename (field=allUsers, resolver_method=usersAlias). Asserted in TestSpringGraphQL_QueryMapping/_NameOverride. |
| Route extraction | 🟢 `partial` | `2026-06-02` | backfill:dictionary-completeness | `internal/custom/java/patterns_dispatch.go`<br>`internal/custom/java/spring_graphql.go` | Operation paths /graphql/<Operation>/<field> derived from annotation; @SchemaMapping(typeName=non-root) field resolvers correctly skipped. PARTIAL: file-local annotation-driven; SDL-only fields and custom spring.graphql.path mount not read. Asserted _SchemaMappingExplicitRoot. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/3995) | `internal/custom/java/framework_auth.go`<br>`internal/custom/java/framework_auth_test.go`<br>`internal/custom/java/spring_graphql.go` | #3862/#3995: spring_graphql.go stamps the flat Spring-compatible auth contract on each synthesized GRAPHQL resolver endpoint. @Secured/@PreAuthorize/@RolesAllowed/@PermitAll on a @QueryMapping/@MutationMapping/@SchemaMapping resolver → auth_required + auth_roles/auth_scopes/auth_permissions (ROLE_/SCOPE_ split like Spring MVC). #3995 BUGFIX: the interleaved-annotation tolerance now uses annArgsRe (one-level nested-paren tolerant) so a SpEL @PreAuthorize("hasRole('ADMIN')") interleaved between the mapping annotation and the method no longer drops the whole endpoint (old \([^)]*\) stopped at the first inner ')'). PARTIAL: same matcher set as Spring MVC; custom GraphQL interceptors / instrumentation-based auth not read. Value-asserting test TestSpringGraphQLAuth_PreAuthorizeNestedParen_Issue3873: @QueryMapping @PreAuthorize(hasRole(ADMIN)) → auth_required=true auth_roles=ADMIN; @PreAuthorize(hasRole(MANAGER) and hasAuthority(SCOPE_write)) → auth_roles=MANAGER auth_scopes=write; unannotated resolver → no auth_required. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/3995) | `internal/custom/java/spring_graphql.go`<br>`internal/custom/java/spring_graphql_test.go` | #3995: DTO-typed @InputArgument/@Argument resolver params and unwrapped return types register scope:schema:spring_dto SCOPE.Schema entities (kind=dto, framework=graphql). PARTIAL: type-name-only; DTO class member fields recovered only when in-file (cross-file is Phase 4, same limit as the Spring MVC sniffer). Asserted in spring_graphql_test.go. |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-02` | — | `internal/custom/java/issue4390_livedrepro_test.go`<br>`internal/custom/java/issue4390_sut_disambiguation_test.go`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/frameworks_java_test.go`<br>`internal/extractors/cross/testmap/resolver.go` | Java JUnit (4/5) deep test->SUT linkage via the shared cross/testmap extractor (#3855), same path that credits Kotlin JVM (#3437). detectJUnit fires on @Test/@ParameterizedTest/@RepeatedTest in *Test.java/*Tests.java/*IT.java (org.junit/junit.jupiter import hints); resolver emits high-confidence TESTS edges for direct SUT calls (new UserService(); userService.create()), medium for class-name subject (UserServiceTest->UserService) when the body has no prod call, and suppresses MockMvc/REST-assured/WebTestClient/AssertJ/Hamcrest/Mockito test-harness noise. Value-asserted in frameworks_java_test.go (TestJUnit_DirectCall_HighConfidence/_MethodCallOnInjectedSUT/_ClassNameSubject/_ParameterizedTest/_MockMvc_NoHTTPClientNoise/_RestAssured_NoDSLNoise). Scope: unit-level test->SUT; framework-handler attribution from HTTP integration tests (MockMvc/REST-assured -> controller endpoint) is out of scope. SUT disambiguation when a test class injects MULTIPLE candidate fields (#4390, extending #4359/#4615): junit5.go resolveJavaTestSubjectDetail picks the ONE system-under-test by priority @InjectMocks (Mockito's explicit SUT marker, overrides stem) then stem-match (OrderServiceTest->OrderService against the injected/constructed non-mock field-type set) then single non-mock injected field then none (ambiguous equals -> no edge); @Mock/@MockBean/@Spy/@SpyBean collaborator types are excluded at every tier so a stubbed collaborator is never linked even when its name matches the test-class stem. Fixtures #4390: internal/custom/java/issue4390_sut_disambiguation_test.go, internal/custom/java/issue4390_livedrepro_test.go. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type alias extraction | — `not_applicable` | `2026-06-04` | — | — | Java has no type alias syntax (no `type X = Y`); the language only has classes/interfaces/enums/records and generics, so there is nothing for the Go-style `type X = Y` alias extractor to lift. Not applicable by language design — mirrors spring-boot/micronaut/jaxrs. |
| Type extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI injection point | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DI scope resolution | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/java/transactional.go`<br>`internal/custom/java/transactional_3863_test.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | #3863: spring_graphql in txFrameworks; @Transactional resolver boundaries + programmatic. |
| Transaction function stamping | ✅ `full` | `2026-06-02` | — | `internal/extractors/java/java.go`<br>`internal/extractors/java/transaction_boundary_test.go`<br>`internal/txscope/txscope.go` | #3863: @Transactional stamping via txscope.DetectJava (framework-agnostic). |
| Transaction propagation | ✅ `full` | `2026-06-02` | — | `internal/custom/java/transactional.go`<br>`internal/custom/java/transactional_3863_test.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | #3863: propagation/TxType captured. |
| Transaction rollback rules | ✅ `full` | `2026-06-02` | — | `internal/custom/java/transactional.go`<br>`internal/custom/java/transactional_3863_test.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | #3863: rollbackFor/noRollbackFor + rollbackOn/dontRollbackOn + programmatic. |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Aspect extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pointcut resolution | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

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
| Dead code detection | 🟢 `partial` | `2026-06-04` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | #3872: dead-code identification is the whole-GRAPH Phase-1B reachability pass (reachability.go) with zero per-language code; a Spring for GraphQL @QueryMapping/@SchemaMapping controller method is an ordinary Java entity, so one never reached from an entry-point is flagged a dead-code candidate exactly as for spring-boot. PARTIAL (mirrors all Java siblings): a method reached only via Spring's annotation-driven schema dispatch (not a direct CALLS edge the seeder follows) can be a false dead-code positive. |
| Def use chain extraction | 🟢 `partial` | `2026-06-04` | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/substrate_structural_gojava_wave1_test.go` | #3872 (verify-first): def_use_java.go registers per-LANGUAGE via RegisterDefUseSniffer("java", …), .java→java file dispatch, zero framework refs. sniffDefUseJava extracts intra-procedural defs/uses and attributes them to the enclosing spring-graphql method via scanJavaFuncHeaders. Proven by TestStructural_Java_SpringGraphql_DefUseAttributes (def+use of local `found` in @QueryMapping bookById). PARTIAL: standard typed-local-binding chains; inter-procedural reaching-defs not modelled. |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/java/exception_flow.go`<br>`internal/extractors/java/exception_flow_test.go` | throw new X + throws clause -> THROWS; catch (A|B e) -> CATCHES; checked-exception model (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic Java engine pass, fires regardless of framework). Java idioms attribute to the enclosing method: LD camelCase boolVariation/stringVariation, Unleash isEnabled, OpenFeature getBooleanValue, FF4j ff4j.check. Honest-partial: Togglz enum keys + dynamic keys miss (no literal). |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🟢 `partial` | `2026-06-03` | 4219 | `internal/substrate/effect_sinks_java.go`<br>`internal/substrate/graphql_effects_java_jsts_3872_test.go` | #3872/#4219: framework-blind java effect sniffer (javaHTTPRe) detects WebClient outbound calls in any method body. Probe TestSpringGraphql_HTTPEffect_Fires drives a @MutationMapping method making webClient.post()...retrieve() and asserts EffectHTTPOut attributed to addUser. PARTIAL: known-client receiver shapes only. |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🟢 `partial` | `2026-06-04` | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | #3872: module-cycle detection is the whole-GRAPH module_cycle_pass over the Java IMPORTS edge graph; a spring-graphql app spans ≥2 ordinary Java packages with import edges, so import cycles among them are detected exactly as for spring-boot. PARTIAL (mirrors all Java siblings): package/type-level import cycles only; spring-graphql annotation/DI wiring is not an import cycle. |
| Mutation effect | 🟢 `partial` | `2026-06-03` | 4219 | `internal/substrate/effect_sinks_java.go`<br>`internal/substrate/graphql_effects_java_jsts_3872_test.go` | #3872/#4219: framework-blind java effect sniffer (javaMutationRe) detects this.<field>= in any method body. Probe TestSpringGraphql_MutationEffect_Fires drives a Spring-for-GraphQL @MutationMapping method (addUser, this.lastCount=...) and asserts EffectMutation attributed to addUser; db_read/db_write also fire (no db_effect cell). PARTIAL: this.field= sink only; no inter-procedural mutation tracking. |
| Pure function tagging | 🟢 `partial` | `2026-06-04` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | #3872: pure-function tagging is the whole-GRAPH Phase-3A pass (pure_function_pass.go) with zero per-language code — it tags any function-like entity the effect pass left effect-free. A spring-graphql method with no stamped effect is tagged a pure candidate exactly as for spring-boot methods. PARTIAL (mirrors all Java siblings): tagging is absence-of-detected-effect, confidence floor 0.30, not a proof of purity. |
| Reachability analysis | 🟢 `partial` | `2026-06-04` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | #3872: reachability is the whole-GRAPH Phase-1B BFS from the Java entry-point set across CALLS/IMPORTS/etc; a Spring for GraphQL @QueryMapping/@SchemaMapping controller method reached transitively from the Java main is marked reachable exactly as for spring-boot. PARTIAL (mirrors all Java siblings): a method reached only via Spring's annotation-driven schema dispatch the static seeder does not model can be under-reached. |
| Request shape extraction | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/3995) | `internal/custom/java/spring_graphql.go`<br>`internal/custom/java/spring_graphql_test.go` | #3995: each DTO-typed @InputArgument/@Argument resolver param emits ACCEPTS_INPUT endpoint→DTO (scope:schema:spring_dto), the GraphQL analogue of Spring MVC @RequestBody. Scalar args (Long/String/int) skipped via gqlShapeBaseType+srrSkipTypes. PARTIAL: signature-DTO-typed args only; inline field-selection sets and cross-file DTO member fields not recovered. Asserted in spring_graphql_test.go (ACCEPTS_INPUT createUser→NewUser / addUser→NewUser; scalar id is NOT an input). |
| Request sink dataflow | 🔴 `missing` | — | 3958 | — | No dataflow sniffer covers this framework's request-binding forms yet. The Java sniffer (internal/substrate/dataflow_java.go, #3958) targets Spring MVC/WebFlux @RequestBody/@RequestParam/@PathVariable; Kotlin/Scala have no sniffer at all (no "kotlin"/"scala" slug registered). request_sink_dataflow remains a follow-up for these JVM frameworks. |
| Response shape extraction | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/3995) | `internal/custom/java/spring_graphql.go`<br>`internal/custom/java/spring_graphql_test.go` | #3995: the unwrapped resolver return type (List<User>/Mono<User>/Flux<Event> → User/Event, via the shared Spring MVC unwrapReturnType) emits RETURNS endpoint→DTO (scope:schema:spring_dto). PARTIAL: type-name-only response DTO; in-file member fields only. Asserted in spring_graphql_test.go (RETURNS createUser→User, users→User unwrapped). |
| Sanitizer recognition | 🟢 `partial` | `2026-06-04` | 3872 | `internal/links/taint_flow.go`<br>`internal/substrate/substrate_java_graphql_taint_test.go`<br>`internal/substrate/taint_sites_java.go` | #3872 (verify-first, vuln-finding sibling sweep): the per-LANGUAGE taint_sites_java.go sanitizer detectors are framework-blind and fire on a Spring-for-GraphQL @QueryMapping resolver body — HtmlUtils.htmlEscape as an XSS sanitizer (javaSanitizerHTMLRe) and PreparedStatement creation as a SQL sanitizer (javaSanitizerSQLRe) — both attributing to the resolver method `bookByName` (scanJavaFuncHeaders accepts the `@QueryMapping public Book bookByName(…)` annotation-prefixed header). Proven by TestSubstrate_Java_SpringGraphQL_SanitizerFires (asserts sanitizer/xss AND sanitizer/sql_injection both attributed to `bookByName`). partial: sanitizer primitives detected per-LANGUAGE regardless of framework; the Spring-GraphQL request-input (@Argument typed args) source is not seeded, so a full source→sink flow is not modelled (see vulnerability_finding, honest-missing). |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🟢 `partial` | `2026-06-04` | 3872 | `internal/links/taint_flow.go`<br>`internal/substrate/substrate_java_graphql_taint_test.go`<br>`internal/substrate/taint_sites_java.go` | #3872 (verify-first, vuln-finding sibling sweep): the per-LANGUAGE taint_sites_java.go SQL-injection sink (javaSinkSQLRe: statement|stmt|jdbcTemplate|connection .execute*/query with a concatenated SQL string) fires on a Spring-for-GraphQL @QueryMapping resolver body and attributes to the resolver method `bookByName`. Proven by TestSubstrate_Java_SpringGraphQL_TaintSinkFires (stmt.executeQuery("… '" + name + "'") flagged sql_injection). partial: the SQL sink anchors on a bare receiver token statement|stmt|jdbcTemplate|connection, so the field-receiver form and other sink shapes are not all covered; security-relevant sink primitives are detected per-LANGUAGE regardless of framework. |
| Taint source detection | 🔴 `missing` | `2026-06-04` | 3872 | `internal/substrate/substrate_java_graphql_taint_test.go`<br>`internal/substrate/taint_sites_java.go` | #3872 (verify-first NEGATIVE, stays missing): the per-LANGUAGE Java taint SOURCE regexes key on servlet / Spring-MVC request accessors, System.getenv/getProperty and ObjectInputStream.readObject. A Spring-for-GraphQL resolver receives untrusted input via the @Argument typed parameter, NOT those request-input sources, so no taint source fires. Proven by TestSubstrate_Java_SpringGraphQL_TaintSourceDoesNotFire (zero sources). Crediting would require an @Argument-aware source model. |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | 3872 | `internal/substrate/substrate_java_graphql_taint_test.go`<br>`internal/substrate/taint_sites_java.go` | #3872 (verify-first NEGATIVE, stays missing): a vulnerability_finding (SecurityFinding) requires a source→sink path and taint_flow.go only seeds its BFS from a TaintKindSource match. The Spring-for-GraphQL request-input idiom (@Argument typed args) is not a recognised taint source (see taint_source_detection), so although the SQL sink and sanitizers fire on the resolver body, no end-to-end request-input→sink finding is emitted. Honest-missing pending an @Argument-aware source model. |

## Related extraction records

This record provides code-level coverage for the
[`protocol.graphql`](./protocol.graphql.md) hub record (GraphQL),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.spring-graphql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
