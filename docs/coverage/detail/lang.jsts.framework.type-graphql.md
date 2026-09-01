<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.type-graphql` — TypeGraphQL (GraphQL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 50

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🟢 `partial` | `2026-06-03` | 3872 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628/#3872: applyEndpointPagination stamps paginated/pagination_style/pagination_params on the type-graphql GraphQL operation via the typed @Arg() signature shape — limit:/offset: param declarations classified by classifyParamShape. Partial: typed-signature shape only. Honest-partial: a lone limit is ambiguous and left unstamped. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-02` | 3619 | `internal/engine/http_endpoint_graphql_jsts_codefirst.go`<br>`internal/engine/http_endpoint_resolve.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphql_jsts_codefirst_3619_test.go`<br>`internal/engine/httproutes/canonicalize.go` | — |
| Handler attribution | ✅ `full` | — | 3619 | `internal/engine/http_endpoint_graphql_jsts_codefirst.go`<br>`internal/engine/http_endpoint_resolve.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphql_jsts_codefirst_3619_test.go`<br>`internal/engine/httproutes/canonicalize.go` | @Query/@Mutation/@Subscription method in an @Resolver class -> http:GRAPHQL:/graphql/<Root>/<field>; source_handler=SCOPE.Operation:<method> rebinds to a HANDLES (IMPLEMENTS) edge against the extracted method symbol (proven end-to-end in TestResolve_TypeGraphQL_HandlesEdge). |
| Route extraction | 🟢 `partial` | — | 3619 | `internal/engine/http_endpoint_graphql_jsts_codefirst.go`<br>`internal/engine/http_endpoint_resolve.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphql_jsts_codefirst_3619_test.go`<br>`internal/engine/httproutes/canonicalize.go` | Root @Query/@Mutation/@Subscription methods only; @FieldResolver (non-root) methods are skipped, matching gqlgen/spring-graphql. Field name = method name or the { name: '...' } decorator option. Honest-partial: a dynamic/computed name option is not recovered. |
| Websocket route extraction | — `not_applicable` | `2026-06-14` | — | — | #4965: GraphQL/gRPC/OpenAPI-doc/service-abstraction framework with no HTTP WebSocket-upgrade route surface (WS, if used, is provided by the host HTTP framework, not this layer). |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 3619 | — | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | 🔴 `missing` | — | 3619 | — | — |
| Request validation | 🔴 `missing` | — | 3619 | — | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | 🔴 `missing` | — | 3619 | — | — |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/grafel/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/javascript/graphql_codefirst_typegraph.go`<br>`internal/custom/javascript/graphql_codefirst_typegraph_test.go` | Code-first GraphQL object-type→type graph: an object-typed field emits a GRAPH_RELATES edge between the SCOPE.Schema type nodes (addressed via BuildOperationStructuralRef("graphql",...) — same identity contract as the SDL pass #3805, node reuse/no duplicate), carrying {list, nullable, item_nullable, cardinality:to_one|to_many, field_name, self_ref, graphql_field, framework}. TypeGraphQL @Field(() => [Order]) thunk + Nexus t.list.field({type}) + Pothos t.field({type:[...]}) resolved. Scalar/unresolved targets make NO edge. Value-asserting tests assert exact FromID+ToID+cardinality + node convergence. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3619 | — | — |
| Interface extraction | 🔴 `missing` | — | 3619 | — | — |
| Type alias extraction | ✅ `full` | `2026-06-03` | 3963 | `internal/extractors/javascript/extractor.go`<br>`internal/patterns/type_alias_extractor.go` | #3963 wave1-structural: TS type_alias extractor emits alias_name/alias_of for type-graphql context/arg-shape aliases (type MyContext = ...). Probe TestW1jr_TypeAlias_TypeGraphqlContextAlias asserts MyContext alias_of=BaseContext & RequestContext. |
| Type extraction | 🔴 `missing` | — | 3619 | — | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🔴 `missing` | — | 3619 | — | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🔴 `missing` | — | 3619 | — | — |
| Metric extraction | 🔴 `missing` | — | 3619 | — | — |
| Trace extraction | 🔴 `missing` | — | 3619 | — | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | 🟢 `partial` | `2026-06-02` | 3903 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/substrate_jsts_graphql_codefirst_test.go` | #3903 (verify-first): effect_sinks_jsts.go is registered per-LANGUAGE on "jsts" and dispatched by file extension (LanguageForPath) with zero framework refs; effect_propagation.go binds each attributed sink to its graph entity. TypeGraphQL resolvers live in ordinary .ts files, so db_read/db_write ORM primitives are detected and attributed to the enclosing function. Proven by TestSubstrate_JSTS_TypeGraphQL_EffectsAttribute (db_read+db_write attributed to persistAccount). Partial: attribution covers the standard fn/service/handler forms these codebases contain; the inline-arrow / @Arg-decorated resolver forms have no addressable header so their in-body sinks are not bound there. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-06-04` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | #3872: the per-LANGUAGE sniffJSTS sniffer (Register("jsts")) gates only on file content with zero per-framework branching, so the graph-wide confidence overlay (#2769) consumes the SAME per-Binding Confidence for type-graphql files as flagship siblings. Value-asserting test drives the type-graphql @ObjectType/@Resolver module (.ts) idiom and asserts the EXACT Confidence (literal 1.0 / env-fallback 0.85 / cross-file import 0.6). |
| Config consumption | 🔴 `missing` | — | 3619 | — | — |
| Constant propagation | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_gjj_sweep_test.go` | #3872: the framework-blind jsts sniffJSTS sniffer extracts top-level string literals regardless of framework; type-graphql dispatches it identically. Test asserts the EXACT literal value (TYPEGQL_ROUTE="/api/graphql" literal) + ProvenanceLiteral + Confidence 1.0 on the type-graphql @ObjectType/@Resolver module (.ts) idiom. |
| Dead code detection | 🟢 `partial` | `2026-06-03` | 3076 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_jsts.go` | #3076 wave1-structural: reachability/dead-code BFS flags type-graphql resolver-class methods unreferenced by the buildSchema resolvers array; jsts entry points via entry_points_jsts.go. |
| Def use chain extraction | 🟢 `partial` | `2026-06-02` | 3903 | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/substrate_jsts_graphql_codefirst_test.go` | #3903 (verify-first): def_use_jsts.go is registered per-LANGUAGE on "jsts" (file-extension dispatch via LanguageForPath, zero framework refs); def_use_pass.go composes intra-procedural reaching-definitions over the resulting VarDef/VarUse facts. TypeGraphQL .ts bodies yield real def-use chains attributed to their enclosing function. Proven by TestSubstrate_JSTS_TypeGraphQL_DefUseAttributes. Partial: attribution follows the per-language header scanner (named fns / const-arrows / plain methods bind; inline-arrow resolvers in t.field({...}) do not). |
| Env fallback recognition | ✅ `full` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_gjj_sweep_test.go` | #3872: the framework-blind jsts substrate sniffer recognises the env-fallback idiom regardless of framework; type-graphql dispatches it identically. Test asserts the EXACT env-var name + default literal (TYPEGQL_PLAYGROUND+default "http://localhost:5000/playground") + ProvenanceEnvFallback + Confidence 0.85 on the type-graphql @ObjectType/@Resolver module (.ts) idiom. |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/javascript/exception_flow.go`<br>`internal/extractors/javascript/exception_flow_test.go` | throw new X -> THROWS; e instanceof X catch-filter -> CATCHES; untyped throw/catch dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic JS/TS engine pass, fires regardless of framework). Verified to attribute to the enclosing function: LaunchDarkly ldClient.variation/boolVariation/stringVariation, Unleash unleash.isEnabled, OpenFeature client.getBooleanValue, Unleash-React useFlag, Split.io getTreatment, Flagsmith hasFeature, plus GrowthBook gb.isOn/isOff/getFeatureValue and ConfigCat configCatClient.getValue/getValueAsync (receiver-gated). Honest-partial: dynamic keys + non-flag receivers (button.isOn, formData.getValue) emit nothing. |
| Fs effect | 🔴 `missing` | — | 3619 | — | — |
| HTTP effect | 🟢 `partial` | `2026-06-02` | 3903 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/substrate_jsts_graphql_codefirst_test.go` | #3903 (verify-first): the per-LANGUAGE effect_sinks_jsts.go http_out detector (fetch/axios/got/ky/...) fires on TypeGraphQL .ts bodies and attributes to the enclosing function; effect_propagation.go binds it. Proven by TestSubstrate_JSTS_TypeGraphQL_EffectsAttribute (http_out attributed to persistAccount). Partial: same standard-form attribution scope as db_effect. |
| Import resolution quality | 🟢 `partial` | `2026-06-04` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go`<br>`internal/substrate/substrate_cap_gjj_sweep_test.go` | #3872: the jsts cross-file import sniffer is framework-blind; type-graphql dispatches it identically. PARTIAL (mirrors all siblings): single-segment binding, no transitive/re-export graph. Test asserts the EXACT ImportSource (type-graphql) + ProvenanceCrossFile + Confidence 0.6 on the type-graphql @ObjectType/@Resolver module (.ts) idiom. |
| Module cycle detection | 🟢 `partial` | `2026-06-03` | 3076 | `internal/links/module_cycle_pass.go` | #3076 wave1-structural: Tarjan SCC over IMPORTS detects cycles among type-graphql resolver/type modules; IMPORTS emitted by the jsts extractor. |
| Mutation effect | 🟢 `partial` | `2026-06-03` | 4219 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/graphql_effects_java_jsts_3872_test.go` | #3872/#4219: framework-blind jsts effect sniffer (jstsMutationRe) detects this.<field>= in any function body. Probe TestTypeGraphql_MutationEffect_Fires asserts EffectMutation attributed to the named persistAccount service the @Mutation method delegates to (this.lastEmail=...); db_read/db_write also fire (no db_effect cell). PARTIAL (mirrors #3903): @Arg-decorated method params defeat the header regex, so effectful work is credited via the delegated service. |
| Pure function tagging | 🟢 `partial` | `2026-06-03` | 3076 | `internal/links/pure_function_pass.go` | #3076 wave1-structural: language-agnostic pure-function pass tags type-graphql @Query/@Mutation decorator methods left un-stamped by the effect pass. Method-attribution idiom proven in TestW1jr_DefUseJSTS_TypeGraphqlResolverMethod. |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | 3076 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_jsts.go` | #3076 wave1-structural: reachability BFS reaches type-graphql @Resolver methods through CALLS/IMPORTS edges from the jsts extractor; entry points lifted by entry_points_jsts.go. |
| Request shape extraction | 🔴 `missing` | — | 3619 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🔴 `missing` | — | 3619 | — | — |
| Sanitizer recognition | 🟢 `partial` | `2026-06-04` | 3872 | `internal/links/taint_flow.go`<br>`internal/substrate/substrate_jsts_graphql_codefirst_test.go`<br>`internal/substrate/taint_sites_jsts.go` | #3872 (verify-first, vuln-finding sibling sweep): the per-LANGUAGE taint_sites_jsts.go sanitizer detectors are framework-blind and fire on TypeGraphQL .ts bodies — validator.escape as an XSS sanitizer (jstsSanitizerHTMLRe) and parameterised db.query(sql, [params]) as a SQL sanitizer (jstsSanitizerSQLRe) — both attributing to the persistAccount helper. Proven by TestSubstrate_JSTS_TypeGraphQL_SanitizerFires (asserts sanitizer/xss AND sanitizer/sql_injection both attributed to persistAccount). Same verify-first basis as the #3903 taint_sink credit. partial: sanitizer primitives detected per-LANGUAGE regardless of framework; the TypeGraphQL request-input (resolver args) source is not seeded, so a full source→sink flow is not modelled (see vulnerability_finding, honest-missing). |
| Schema drift detection | 🟢 `partial` | `2026-06-04` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | #3872: the framework-agnostic payload-drift pass dispatches sniffPayloadShapesJsts by LANGUAGE slug (LanguageForPath->PayloadShapeSnifferFor), so type-graphql producer/consumer shapes feed the same drift join as siblings. PARTIAL (mirrors siblings): no type-graphql-specific drift fixture asserted end-to-end yet. |
| Taint sink detection | 🟢 `partial` | `2026-06-02` | 3903 | `internal/links/taint_flow.go`<br>`internal/substrate/substrate_jsts_graphql_codefirst_test.go`<br>`internal/substrate/taint_sites_jsts.go` | #3903 (verify-first): the per-LANGUAGE taint_sites_jsts.go sink detector fires on TypeGraphQL .ts bodies — a raw-SQL concat (db.query('... ' + x)) is flagged as a sql_injection sink. Proven by TestSubstrate_JSTS_TypeGraphQL_TaintFires. Partial: the security-sensitive sink primitives are detected per-language regardless of framework; full source→sink flow linkage depends on handler attribution (see def_use/effects scope). |
| Taint source detection | 🔴 `missing` | — | 3619 | — | — |
| Template pattern catalog | 🟢 `partial` | — | — | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_jsts.go` | #3872: sniffTemplatePatternsJsts is registered on the jsts language slug and gates only on file content (no per-framework branch), so type-graphql dispatches it identically. PARTIAL: mirrors all siblings. |
| Vulnerability finding | 🔴 `missing` | — | 3619 | — | — |

## Related extraction records

This record provides code-level coverage for the
[`protocol.graphql`](./protocol.graphql.md) hub record (GraphQL),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.type-graphql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
