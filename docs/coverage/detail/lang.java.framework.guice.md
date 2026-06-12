<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.guice` — Google Guice (DI)

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
| Endpoint synthesis | 🔴 `missing` | — | 3699 | — | — |
| Handler attribution | 🔴 `missing` | — | 3699 | — | — |
| Route extraction | 🔴 `missing` | — | 3699 | — | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 3699 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 3699 | — | — |
| Request validation | 🔴 `missing` | — | 3699 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 3699 | — | — |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-02` | — | `internal/custom/java/issue4390_livedrepro_test.go`<br>`internal/custom/java/issue4390_sut_disambiguation_test.go`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/frameworks_java_test.go`<br>`internal/extractors/cross/testmap/resolver.go` | Java JUnit (4/5) deep test->SUT linkage via the shared cross/testmap extractor (#3855), same path that credits Kotlin JVM (#3437). detectJUnit fires on @Test/@ParameterizedTest/@RepeatedTest in *Test.java/*Tests.java/*IT.java (org.junit/junit.jupiter import hints); resolver emits high-confidence TESTS edges for direct SUT calls (new UserService(); userService.create()), medium for class-name subject (UserServiceTest->UserService) when the body has no prod call, and suppresses MockMvc/REST-assured/WebTestClient/AssertJ/Hamcrest/Mockito test-harness noise. Value-asserted in frameworks_java_test.go (TestJUnit_DirectCall_HighConfidence/_MethodCallOnInjectedSUT/_ClassNameSubject/_ParameterizedTest/_MockMvc_NoHTTPClientNoise/_RestAssured_NoDSLNoise). Scope: unit-level test->SUT; framework-handler attribution from HTTP integration tests (MockMvc/REST-assured -> controller endpoint) is out of scope. SUT disambiguation when a test class injects MULTIPLE candidate fields (#4390, extending #4359/#4615): junit5.go resolveJavaTestSubjectDetail picks the ONE system-under-test by priority @InjectMocks (Mockito's explicit SUT marker, overrides stem) then stem-match (OrderServiceTest->OrderService against the injected/constructed non-mock field-type set) then single non-mock injected field then none (ambiguous equals -> no edge); @Mock/@MockBean/@Spy/@SpyBean collaborator types are excluded at every tier so a stubbed collaborator is never linked even when its name matches the test-class stem. Fixtures #4390: internal/custom/java/issue4390_sut_disambiguation_test.go, internal/custom/java/issue4390_livedrepro_test.go. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3699 | — | — |
| Interface extraction | 🔴 `missing` | — | 3699 | — | — |
| Type alias extraction | — `not_applicable` | `2026-06-04` | — | — | Java has no type alias syntax (no `type X = Y`); the language only has classes/interfaces/enums/records and generics, so there is nothing for the Go-style `type X = Y` alias extractor to lift. Not applicable by language design — mirrors spring-boot/micronaut/jaxrs. |
| Type extraction | 🔴 `missing` | — | 3699 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/java/di_graph.go`<br>`internal/custom/java/di_graph_test.go`<br>`internal/custom/java/patterns_dispatch.go` | #3699: ExtractGuiceDI emits the DI binding GRAPH: bind(Foo.class).to(FooImpl.class) in an AbstractModule emits Foo BINDS FooImpl; bind(...).toProvider(...) emits binding_kind=bind_provider; lifetime parsed from .in(Scopes.SINGLETON)/.asEagerSingleton(). Value-asserted in di_graph_test.go TestGuiceDI_BindTo_Binds (IFoo BINDS Foo, binding_kind=bind_to, lifetime=singleton). |
| DI injection point | ✅ `full` | `2026-06-02` | — | `internal/custom/java/di_graph.go`<br>`internal/custom/java/di_graph_test.go` | #3699: @Inject constructor params and @Inject fields emit INJECTED_INTO (provider type -> consumer class, via=guice_constructor|guice_field), primitives rejected. Value-asserted in di_graph_test.go TestGuiceDI_InjectConstructor_InjectedInto (PaymentGateway INJECTED_INTO BillingService; negative: String yields no edge). Under the shared jakarta_ee candidate token a bind()/AbstractModule signal is required to avoid @Inject false positives (self-gate, TestGuiceDI_SelfGate_NoBindNoModule). |
| DI scope resolution | 🟢 `partial` | `2026-06-02` | — | `internal/custom/java/di_graph.go`<br>`internal/custom/java/di_graph_test.go` | #3699: BINDS edges carry the Guice scope (singleton/eager_singleton/no_scope) parsed from the bind tail. PARTIAL: @Singleton on a class and custom scope annotations are not yet linked to the binding. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3863) | `internal/custom/java/transactional.go`<br>`internal/custom/java/transactional_3863_test.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | #3863 (partial): programmatic transaction boundary detected — UserTransaction.begin()/commit(), Hibernate session.beginTransaction(), JPA em.getTransaction().begin() in a method body emit a SCOPE.Pattern(subtype=transaction_boundary, transaction_boundary=programmatic, tx_api=...). No @Transactional annotation surface for this framework. Honest-partial: boundary credited only where a begin/open call is lexically present. |
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |
| Transaction propagation | 🔴 `missing` | — | 3699 | — | — |
| Transaction rollback rules | 🟢 `partial` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3863) | `internal/custom/java/transactional.go`<br>`internal/custom/java/transactional_3863_test.go`<br>`internal/extractors/custom_java_patterns_smoke_test.go` | #3863 (partial): programmatic rollback detected — setRollbackOnly() / tx.rollback() / userTransaction.rollback() mark rollback=programmatic on the method. No declarative rollbackFor/rollbackOn surface for this framework. |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | 🔴 `missing` | — | 3699 | — | — |
| Aspect extraction | 🔴 `missing` | — | 3699 | — | — |
| Pointcut resolution | 🔴 `missing` | — | 3699 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 3699 | — | — |
| Metric extraction | 🔴 `missing` | — | 3699 | — | — |
| Trace extraction | 🔴 `missing` | — | 3699 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🔴 `missing` | — | 3699 | — | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-06-04` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | #3872: the per-LANGUAGE sniffJava sniffer (Register("java")) gates only on file content with zero per-framework branching, so the graph-wide confidence overlay (#2769) consumes the SAME per-Binding Confidence for guice files as flagship siblings. Value-asserting test drives the Guice AbstractModule (.java) idiom and asserts the EXACT Confidence (literal 1.0 / env-fallback 0.85 / cross-file import 0.6). |
| Config consumption | 🔴 `missing` | — | 3699 | — | — |
| Constant propagation | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_gjj_sweep_test.go` | #3872: the framework-blind java sniffJava sniffer extracts top-level string literals regardless of framework; guice dispatches it identically. Test asserts the EXACT literal value (GUICE_BINDING_NAME="primaryDataSource" literal) + ProvenanceLiteral + Confidence 1.0 on the Guice AbstractModule (.java) idiom. |
| Dead code detection | 🟢 `partial` | `2026-06-04` | 3699 | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | #3872: dead-code identification is the whole-GRAPH Phase-1B reachability pass (reachability.go) with zero per-language code; a Guice AbstractModule / bound class method is an ordinary Java entity, so one never reached from an entry-point is flagged a dead-code candidate exactly as for spring-boot. PARTIAL (mirrors all Java siblings): a method reached only via Guice's runtime bind()/@Provides reflection (not a direct CALLS edge the seeder follows) can be a false dead-code positive. |
| Def use chain extraction | 🟢 `partial` | `2026-06-04` | 3699 | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_java.go`<br>`internal/substrate/substrate_structural_gojava_wave1_test.go` | #3872 (verify-first): def_use_java.go registers per-LANGUAGE via RegisterDefUseSniffer("java", …), .java→java file dispatch, zero framework refs. sniffDefUseJava extracts intra-procedural defs/uses and attributes them to the enclosing guice method via scanJavaFuncHeaders. Proven by TestStructural_Java_Guice_DefUseAttributes (def+use of local `url` in configure()). PARTIAL: standard typed-local-binding chains; inter-procedural reaching-defs not modelled. |
| Env fallback recognition | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_gjj_sweep_test.go` | #3872: the framework-blind java substrate sniffer recognises the env-fallback idiom regardless of framework; guice dispatches it identically. Test asserts the EXACT env-var name + default literal (GUICE_JDBC_URL+default "jdbc:postgresql://localhost/guice") + ProvenanceEnvFallback + Confidence 0.85 on the Guice AbstractModule (.java) idiom. |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/java/exception_flow.go`<br>`internal/extractors/java/exception_flow_test.go` | throw new X + throws clause -> THROWS; catch (A|B e) -> CATCHES; checked-exception model (#3628) |
| Feature flag gating | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | 3699 | — | — |
| HTTP effect | 🔴 `missing` | — | 3699 | — | — |
| Import resolution quality | 🟢 `partial` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_gjj_sweep_test.go` | #3872: the java cross-file import sniffer is framework-blind; guice dispatches it identically. PARTIAL (mirrors all siblings): single-segment binding, no transitive/re-export graph. Test asserts the EXACT ImportSource (com.google.inject) + ProvenanceCrossFile + Confidence 0.6 on the Guice AbstractModule (.java) idiom. |
| Module cycle detection | 🟢 `partial` | `2026-06-04` | 3699 | `internal/links/module_cycle_pass.go` | #3872: module-cycle detection is the whole-GRAPH module_cycle_pass over the Java IMPORTS edge graph; a guice app spans ≥2 ordinary Java packages with import edges, so import cycles among them are detected exactly as for spring-boot. PARTIAL (mirrors all Java siblings): package/type-level import cycles only; guice annotation/DI wiring is not an import cycle. |
| Mutation effect | 🔴 `missing` | — | 3699 | — | — |
| Pure function tagging | 🟢 `partial` | `2026-06-04` | 3699 | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | #3872: pure-function tagging is the whole-GRAPH Phase-3A pass (pure_function_pass.go) with zero per-language code — it tags any function-like entity the effect pass left effect-free. A guice method with no stamped effect is tagged a pure candidate exactly as for spring-boot methods. PARTIAL (mirrors all Java siblings): tagging is absence-of-detected-effect, confidence floor 0.30, not a proof of purity. |
| Reachability analysis | 🟢 `partial` | `2026-06-04` | 3699 | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | #3872: reachability is the whole-GRAPH Phase-1B BFS from the Java entry-point set across CALLS/IMPORTS/etc; a Guice AbstractModule / bound class method reached transitively from the Java main is marked reachable exactly as for spring-boot. PARTIAL (mirrors all Java siblings): a method reached only via Guice's runtime bind()/@Provides reflection the static seeder does not model can be under-reached. |
| Request shape extraction | 🔴 `missing` | — | 3699 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 3958 | — | No dataflow sniffer covers this framework's request-binding forms yet. The Java sniffer (internal/substrate/dataflow_java.go, #3958) targets Spring MVC/WebFlux @RequestBody/@RequestParam/@PathVariable; Kotlin/Scala have no sniffer at all (no "kotlin"/"scala" slug registered). request_sink_dataflow remains a follow-up for these JVM frameworks. |
| Response shape extraction | 🔴 `missing` | — | 3699 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 3699 | — | — |
| Schema drift detection | 🟢 `partial` | `2026-06-04` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | #3872: the framework-agnostic payload-drift pass dispatches sniffPayloadShapesJava by LANGUAGE slug (LanguageForPath->PayloadShapeSnifferFor), so guice producer/consumer shapes feed the same drift join as siblings. PARTIAL (mirrors siblings): no guice-specific drift fixture asserted end-to-end yet. |
| Taint sink detection | 🔴 `missing` | — | 3699 | — | — |
| Taint source detection | 🔴 `missing` | — | 3699 | — | — |
| Template pattern catalog | 🟢 `partial` | — | — | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_java.go` | #3872: sniffTemplatePatternsJava is registered on the java language slug and gates only on file content (no per-framework branch), so guice dispatches it identically. PARTIAL: mirrors all siblings. |
| Vulnerability finding | 🔴 `missing` | — | 3699 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.guice ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
