<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.kotlin.framework.graphql-kotlin` — graphql-kotlin

Auto-generated. Back to [summary](../summary.md).

- **Language:** [kotlin](../by-language/kotlin.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** JVM Backend
- **Capability cells:** 55

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | — `not_applicable` | `2026-06-12` | 4973 | `internal/custom/kotlin/graphql_kotlin.go` | #4973 (split from #4924): graphql-kotlin endpoints are GraphQL operations over a single POST /graphql transport, not versioned REST routes. Deprecation is expressed via the @Deprecated/@GraphQLDescription schema directive on a field, not URL/path/header versioning — the REST endpoint_deprecation_versioning concern does not apply. Schema-directive deprecation extraction, if pursued, is a distinct GraphQL-native capability (follow-up #5010). |
| Endpoint pagination posture | — `not_applicable` | `2026-06-12` | 4973 | `internal/custom/kotlin/graphql_kotlin.go` | #4973 (split from #4924): the REST pagination-posture signal (limit+offset/page/cursor HTTP query-param shape, applyEndpointPagination) does not apply to graphql-kotlin — GraphQL pagination is modelled in the schema (Relay connection/edges/pageInfo data classes), not as endpoint HTTP params. GraphQL-native connection-pattern detection is a distinct follow-up (#5010), not a REST-pagination gap. |
| Endpoint response codes | — `not_applicable` | `2026-06-12` | 4973 | `internal/custom/kotlin/graphql_kotlin.go` | #4973 (split from #4924): graphql-kotlin transports every operation over a single POST /graphql that returns HTTP 200 with errors carried in the GraphQL `errors` array — there is no per-endpoint REST response-code surface. endpoint_response_codes (a REST-status concept) does not apply. |
| Endpoint synthesis | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/graphql_kotlin.go` | class : Query/Mutation/Subscription marker-interface roots → each public member fun is a GraphQL field synthesised as a GRAPHQL endpoint /graphql/<Operation>/<field>. Value-asserted (user, users, createUser) in TestGraphQLKotlin_ResolverFields/_NameRename. File-local: cross-file root composition via SchemaGeneratorConfig not chased. |
| Handler attribution | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/graphql_kotlin.go` | Each synthesised GraphQL field carries handler_name=<Class>.<fun> and resolver_fun, binding the field to its Kotlin resolver function even under @GraphQLName rename (field=createUser, resolver_fun=addUser). Asserted in TestGraphQLKotlin_NameRename. |
| Route extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/graphql_kotlin.go` | GraphQL operation paths /graphql/<Operation>/<field> derived from the Query/Mutation/Subscription supertype; @GraphQLName renames the field segment, @GraphQLIgnore and private/protected/internal funs excluded. Asserted in TestGraphQLKotlin_ResolverFields/_IgnoreAndPrivate. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | — `not_applicable` | — | 4973 | `internal/custom/kotlin/graphql_kotlin.go` | #4973 (split from #4924): graphql-kotlin resolvers return typed Kotlin objects serialised to a JSON GraphQL response — there is no server-side HTML/template view-rendering layer. view_rendering does not apply to a GraphQL API. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-05-30` | — | `internal/custom/kotlin/graphql_kotlin.go` | data class types (and @GraphQLName-renamed classes) referenced by the schema → SCOPE.Schema DTO entities (dto_name, dto_source_class). Asserted User+NewUser in TestGraphQLKotlin_DTOs. File-local: field-return-type→DTO resolution across files not chased. |
| Request validation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Rate limit stamping | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

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
| Transaction boundary extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |
| Transaction propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Transaction rollback rules | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

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
| DB effect | 🟢 `partial` | `2026-06-03` | 4218 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go`<br>`internal/substrate/graphql_client_effects_4218_test.go` | #4218 (verify-first): per-LANGUAGE effect_sinks_kotlin.go db detectors fire on graphql-kotlin resolver bodies and attribute to the exact resolver method. db_read via kotlinDBReadRe (Spring-Data findById) on account; db_write via kotlinDBWriteRe (repo.save) on createOrder. Proven by TestSubstrate_Kotlin_GraphqlKotlin_EffectsAttribute. partial: JPA/Exposed/R2DBC call-shape detection (conf 0.85). |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-06-04` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | — |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_sibling_sweep_test.go` | #3872: the per-LANGUAGE sniffKotlin sniffer (Register("kotlin")) gates only on file content with zero per-framework branching, so graphql-kotlin .kt files dispatch the SAME const/literal sniffer as flagship siblings. Value-asserting test drives the graphql-kotlin idiom and asserts the EXACT literal value + ProvenanceLiteral + Confidence 1.0. |
| Dead code detection | 🟢 `partial` | `2026-06-04` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_kotlin.go`<br>`internal/substrate/structural_kotlin_wave1_test.go` | #3872: the reachability/dead-code pass (internal/links/reachability.go) BFS-roots on the Kotlin entry-point sniffer’s library_export seeds; graphql-kotlin is rooted on the public Query class + resolver fun. Unreached graphql-kotlin entities are flagged dead-code candidates. Value-asserting test asserts the EXACT library_export seed. |
| Def use chain extraction | 🟢 `partial` | `2026-06-04` | — | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_kotlin.go`<br>`internal/substrate/structural_kotlin_wave1_test.go` | #3872: the per-LANGUAGE Kotlin def-use sniffer (RegisterDefUseSniffer("kotlin")) gates on file content with zero per-framework branching, so graphql-kotlin .kt files dispatch the SAME def-use sniffer as flagship siblings. Value-asserting test drives a Query/Mutation class resolver method body and asserts the EXACT function-attributed local def + matching use. |
| Env fallback recognition | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_sibling_sweep_test.go` | #3872: the framework-blind kotlin substrate sniffer recognises the env-fallback idiom regardless of framework; graphql-kotlin dispatches it identically. Test asserts the EXACT env-var name + default literal + ProvenanceEnvFallback + Confidence 0.85 on the graphql-kotlin idiom. |
| Error flow | ✅ `full` | `2026-06-03` | — | `internal/extractor/exception_flow.go`<br>`internal/extractors/kotlin/exception_flow.go`<br>`internal/extractors/kotlin/exception_flow_test.go` | throw X() -> THROWS; try/catch (e: X) -> CATCHES; @ExceptionHandler(X::class) (@ControllerAdvice) + Ktor StatusPages exception<X> -> CATCHES; converges on shared exception node (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🟢 `partial` | `2026-06-03` | 4218 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go`<br>`internal/substrate/graphql_client_effects_4218_test.go` | #4218 (verify-first): per-LANGUAGE effect_sinks_kotlin.go http_out detector (kotlinHTTPRe client.post) fires on a graphql-kotlin resolver driving an outbound call, attributed to createOrder. Proven by TestSubstrate_Kotlin_GraphqlKotlin_EffectsAttribute. partial: Ktor/OkHttp/RestTemplate client call forms; no graphql-kotlin DataLoader model. |
| Import resolution quality | 🟢 `partial` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/kotlin.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_sibling_sweep_test.go` | #3872: the kotlin cross-file import sniffer is framework-blind; graphql-kotlin dispatches it identically. PARTIAL (mirrors all siblings): single-segment binding, no transitive/re-export graph. Test asserts the EXACT ImportSource + ProvenanceCrossFile + Confidence 0.6 on the graphql-kotlin idiom. |
| Module cycle detection | 🟢 `partial` | `2026-06-04` | — | `internal/extractors/kotlin/references.go`<br>`internal/links/module_cycle_pass.go` | #3872: the module-cycle pass (internal/links/module_cycle_pass.go) is language-agnostic Tarjan over the common IMPORTS edge graph that the Kotlin extractor emits; graphql-kotlin’s multi-file imports feed it identically to siblings. PARTIAL (mirrors siblings): no graphql-kotlin-specific cyclic-import fixture asserted end-to-end yet. |
| Mutation effect | 🟢 `partial` | `2026-06-03` | 4218 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_kotlin.go`<br>`internal/substrate/graphql_client_effects_4218_test.go` | #4218 (verify-first): per-LANGUAGE effect_sinks_kotlin.go mutation detector (kotlinMutationRe this.<field> = ...) fires on a graphql-kotlin resolver body and attributes to createOrder (this.lastAmount = amount). Proven by TestSubstrate_Kotlin_GraphqlKotlin_EffectsAttribute. partial: this.field writes only (conf 0.7). |
| Pure function tagging | 🟢 `partial` | `2026-06-04` | — | `internal/links/pure_function_pass.go`<br>`internal/substrate/def_use_kotlin.go`<br>`internal/substrate/structural_kotlin_wave1_test.go` | #3872: the pure-function pass (internal/links/pure_function_pass.go, "zero per-language code") walks every function-like entity and tags those with no stamped effect set. graphql-kotlin functions are tagged identically to siblings; the def-use proof for a Query/Mutation class resolver method body establishes the function entities it walks. PARTIAL (mirrors siblings): no graphql-kotlin-specific memoization fixture asserted end-to-end yet. |
| Reachability analysis | 🟢 `partial` | `2026-06-04` | — | `internal/links/reachability.go`<br>`internal/substrate/entry_points_kotlin.go`<br>`internal/substrate/structural_kotlin_wave1_test.go` | #3872: the framework-blind reachability BFS (internal/links/reachability.go) seeds on the Kotlin entry-point sniffer (internal/substrate/entry_points_kotlin.go); for graphql-kotlin the seed is the public Query class + resolver fun. Value-asserting test asserts the EXACT library_export entry-point Ident for this framework’s idiom. |
| Request shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Request sink dataflow | 🔴 `missing` | — | 3958 | — | No dataflow sniffer covers this framework's request-binding forms yet. The Java sniffer (internal/substrate/dataflow_java.go, #3958) targets Spring MVC/WebFlux @RequestBody/@RequestParam/@PathVariable; Kotlin/Scala have no sniffer at all (no "kotlin"/"scala" slug registered). request_sink_dataflow remains a follow-up for these JVM frameworks. |
| Response shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | 🟢 `partial` | `2026-06-04` | [link](https://github.com/cajasmota/archigraph/issues/2771) | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_kotlin.go` | #3872: the framework-agnostic payload-drift pass dispatches sniffPayloadShapesKotlin by LANGUAGE slug (LanguageForPath->PayloadShapeSnifferFor), so graphql-kotlin producer/consumer shapes feed the same drift join as siblings. PARTIAL (mirrors siblings): no graphql-kotlin-specific drift fixture asserted end-to-end yet. |
| Taint sink detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | 🟢 `partial` | — | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_kotlin.go` | #3872: sniffTemplatePatternsKotlin is registered on the kotlin language slug and gates only on file content (no per-framework branch), so graphql-kotlin dispatches it identically. PARTIAL: mirrors all siblings. |
| Vulnerability finding | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.graphql`](./protocol.graphql.md) hub record (GraphQL),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.kotlin.framework.graphql-kotlin ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
