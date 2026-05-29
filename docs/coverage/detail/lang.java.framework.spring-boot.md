<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.framework.spring-boot` — Spring Boot / Spring MVC

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 48

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/java/frameworks/spring_boot.yaml`<br>`internal/engine/rules/java/frameworks/spring_mvc.yaml`<br>`internal/engine/spring_routes.go` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | — | `internal/engine/java_annotation_routes.go`<br>`internal/engine/spring_routes.go` | — |
| Route extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/java_annotation_routes.go`<br>`internal/engine/spring_routes.go` | Annotation-driven route composition scanned (@RequestMapping/@GetMapping/etc.); path-variable expression resolution not implemented |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-28` | — | `internal/engine/java_auth_policy.go` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/custom/java/spring_request_response.go` | SCOPE.Schema(kind=dto) entities emitted for @RequestBody parameter types and ResponseEntity<T>/Mono<T>/Flux<T> return types; generic collections (List/Map/Set) skipped via srrSkipTypes |
| Request validation | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/engine/java_annotation_params.go` | Bean Validation annotations (@NotNull, @NotBlank, @NotEmpty, @Valid, @Min, @Max, @Size, @Pattern, @Email) captured per handler parameter; required flag set; no field-level constraint recursion |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ⚠️ `partial` | `2026-05-28` | — | `internal/engine/java_annotation_params.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ⚠️ `partial` | `2026-05-29` | [link](#2991) | `internal/custom/java/junit5.go` | Spring Boot test classes use JUnit 5 (@SpringBootTest/@WebMvcTest); @Test/@ParameterizedTest/@RepeatedTest methods extracted as SCOPE.Operation entities with test_annotation property and OWNS edges from test class. TESTS multi-hop via HTTP layer requires separate work. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/java/java.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/java/java.go` | — |
| Type alias extraction | — `not_applicable` | — | — | — | Java has no type alias syntax |
| Type extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/java/java.go` | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | ⚠️ `partial` | `2026-05-28` | backfill:dictionary-completeness | `internal/custom/java/spring_boot.go` | — |
| DI injection point | ⚠️ `partial` | `2026-05-28` | backfill:dictionary-completeness | `internal/custom/java/spring_boot.go` | — |
| DI scope resolution | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction boundary extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Transaction propagation | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Transaction rollback rules | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### AOP

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Advice attribution | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Aspect extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Pointcut resolution | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Metric extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Trace extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ⚠️ `partial` | `2026-05-29` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/effect_sinks_java.go`<br>`internal/substrate/java.go`<br>`internal/substrate/taint_sites_java.go` | Substrate passes emit per-binding/per-finding confidence (Binding.Confidence, TaintMatch.Confidence, EffectMatch.Confidence); constant_propagation stamps substrate_confidence property on entities. Top-level EntityRecord.Confidence field not yet stamped by the Java extractor directly; confidence data not exposed via MCP min_confidence filtering. Full requires a confidence-scoring pass that writes EntityRecord.Confidence. |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ✅ `full` | `2026-05-29` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | Covers JPA/Hibernate (EntityManager.find/persist/merge/remove/flush), Spring Data JPA (JpaRepository.findById/findAll/findBy*/save/saveAll/delete/count/exists), JdbcTemplate/NamedJdbcTemplate (queryFor*/query/queryForList/update/batchUpdate/execute), raw Statement (executeQuery/executeUpdate), Spring Data MongoDB (findBy* patterns). Read and write sides both covered with per-match confidence. Dominant Spring Boot DB access patterns are fully represented. |
| Dead code detection | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/mcp/dead_code.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Def use chain extraction | ⚠️ `partial` | `2026-05-28` | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_java.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Fs effect | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Import resolution quality | ⚠️ `partial` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/java.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | ⚠️ `partial` | `2026-05-28` | — | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_java.go` | — |
| Pure function tagging | ⚠️ `partial` | `2026-05-28` | — | `internal/links/effect_propagation.go`<br>`internal/links/pure_function_pass.go` | — |
| Reachability analysis | ✅ `full` | `2026-05-28` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_java.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_java.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Taint source detection | ⚠️ `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |
| Template pattern catalog | ⚠️ `partial` | `2026-05-28` | — | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern.go`<br>`internal/substrate/template_pattern_java.go` | — |
| Vulnerability finding | ⚠️ `partial` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_java.go` | — |

## Framework-specific

### Spring Boot Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Actuator detection | ❌ `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/2739) | — | — |
| Autoconfiguration detection | ❌ `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/2739) | — | — |
| Profile detection | ❌ `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/2739) | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.framework.spring-boot ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
