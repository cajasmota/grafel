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
| Tests linkage | ✅ `full` | `2026-06-02` | — | `internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/frameworks_java_test.go`<br>`internal/extractors/cross/testmap/resolver.go` | Java JUnit (4/5) deep test->SUT linkage via the shared cross/testmap extractor (#3855), same path that credits Kotlin JVM (#3437). detectJUnit fires on @Test/@ParameterizedTest/@RepeatedTest in *Test.java/*Tests.java/*IT.java (org.junit/junit.jupiter import hints); resolver emits high-confidence TESTS edges for direct SUT calls (new UserService(); userService.create()), medium for class-name subject (UserServiceTest->UserService) when the body has no prod call, and suppresses MockMvc/REST-assured/WebTestClient/AssertJ/Hamcrest/Mockito test-harness noise. Value-asserted in frameworks_java_test.go (TestJUnit_DirectCall_HighConfidence/_MethodCallOnInjectedSUT/_ClassNameSubject/_ParameterizedTest/_MockMvc_NoHTTPClientNoise/_RestAssured_NoDSLNoise). Scope: unit-level test->SUT; framework-handler attribution from HTTP integration tests (MockMvc/REST-assured -> controller endpoint) is out of scope. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type alias extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
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
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/java/exception_flow.go`<br>`internal/extractors/java/exception_flow_test.go` | throw new X + throws clause -> THROWS; catch (A|B e) -> CATCHES; checked-exception model (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/3995) | `internal/custom/java/spring_graphql.go`<br>`internal/custom/java/spring_graphql_test.go` | #3995: each DTO-typed @InputArgument/@Argument resolver param emits ACCEPTS_INPUT endpoint→DTO (scope:schema:spring_dto), the GraphQL analogue of Spring MVC @RequestBody. Scalar args (Long/String/int) skipped via gqlShapeBaseType+srrSkipTypes. PARTIAL: signature-DTO-typed args only; inline field-selection sets and cross-file DTO member fields not recovered. Asserted in spring_graphql_test.go (ACCEPTS_INPUT createUser→NewUser / addUser→NewUser; scalar id is NOT an input). |
| Request sink dataflow | 🔴 `missing` | — | 3958 | — | No dataflow sniffer covers this framework's request-binding forms yet. The Java sniffer (internal/substrate/dataflow_java.go, #3958) targets Spring MVC/WebFlux @RequestBody/@RequestParam/@PathVariable; Kotlin/Scala have no sniffer at all (no "kotlin"/"scala" slug registered). request_sink_dataflow remains a follow-up for these JVM frameworks. |
| Response shape extraction | 🟢 `partial` | `2026-06-03` | [link](https://github.com/cajasmota/archigraph/issues/3995) | `internal/custom/java/spring_graphql.go`<br>`internal/custom/java/spring_graphql_test.go` | #3995: the unwrapped resolver return type (List<User>/Mono<User>/Flux<Event> → User/Event, via the shared Spring MVC unwrapReturnType) emits RETURNS endpoint→DTO (scope:schema:spring_dto). PARTIAL: type-name-only response DTO; in-file member fields only. Asserted in spring_graphql_test.go (RETURNS createUser→User, users→User unwrapped). |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

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
