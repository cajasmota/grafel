<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.pothos` — Pothos (GraphQL)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 49

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | 🔴 `missing` | — | 3628 | — | — |
| Endpoint pagination posture | 🔴 `missing` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: applyEndpointPagination stamps paginated/pagination_style/pagination_params via the cross-language parameters/parameter_schema fallback (limit+offset/page/cursor shape). No framework-specific pagination-class/ORM signal yet for this framework. |
| Endpoint response codes | 🔴 `missing` | — | 3818 | — | — |
| Endpoint synthesis | ✅ `full` | `2026-06-02` | 3619 | `internal/engine/http_endpoint_graphql_jsts_codefirst.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphql_jsts_codefirst_3619_test.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/rules/graphql/frameworks/pothos_jsts.yaml` | — |
| Handler attribution | — `not_applicable` | — | — | `internal/engine/http_endpoint_graphql_jsts_codefirst.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphql_jsts_codefirst_3619_test.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/rules/graphql/frameworks/pothos_jsts.yaml` | Pothos root fields are inline-arrow resolvers (builder.queryField('users', t => ...)) with no addressable handler symbol; the operation endpoint is emitted with NO source_handler (NoHandlerProp keep-path), matching the Apollo resolver-map convention. There is no method symbol to bind a HANDLES edge to. |
| Route extraction | 🟢 `partial` | — | 3619 | `internal/engine/http_endpoint_graphql_jsts_codefirst.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_graphql_jsts_codefirst_3619_test.go`<br>`internal/engine/httproutes/canonicalize.go`<br>`internal/engine/rules/graphql/frameworks/pothos_jsts.yaml` | builder.queryField/mutationField/subscriptionField + the builder.queryType/mutationType/subscriptionType fields:(t)=>({...}) maps -> http:GRAPHQL:/graphql/<Root>/<field> (EXACT canonical shape as gqlgen/Apollo/Strawberry so client links #3667 join). Honest-partial: a non-literal (variable/computed) field name is not recovered. |

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
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/archigraph/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/javascript/graphql_codefirst_typegraph.go`<br>`internal/custom/javascript/graphql_codefirst_typegraph_test.go` | Code-first GraphQL object-type→type graph: an object-typed field emits a GRAPH_RELATES edge between the SCOPE.Schema type nodes (addressed via BuildOperationStructuralRef("graphql",...) — same identity contract as the SDL pass #3805, node reuse/no duplicate), carrying {list, nullable, item_nullable, cardinality:to_one|to_many, field_name, self_ref, graphql_field, framework}. TypeGraphQL @Field(() => [Order]) thunk + Nexus t.list.field({type}) + Pothos t.field({type:[...]}) resolved. Scalar/unresolved targets make NO edge. Value-asserting tests assert exact FromID+ToID+cardinality + node convergence. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | 3619 | — | — |
| Interface extraction | 🔴 `missing` | — | 3619 | — | — |
| Type alias extraction | 🔴 `missing` | — | 3619 | — | — |
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
| DB effect | 🟢 `partial` | `2026-06-02` | 3903 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/substrate_jsts_graphql_codefirst_test.go` | #3903 (verify-first): effect_sinks_jsts.go is registered per-LANGUAGE on "jsts" and dispatched by file extension (LanguageForPath) with zero framework refs; effect_propagation.go binds each attributed sink to its graph entity. Pothos resolvers live in ordinary .ts files, so db_read/db_write ORM primitives are detected and attributed to the enclosing function. Proven by TestSubstrate_JSTS_Pothos_EffectsAttribute (db_read+db_write attributed to persistUser). Partial: attribution covers the standard fn/service/handler forms these codebases contain; the inline-arrow / @Arg-decorated resolver forms have no addressable header so their in-body sinks are not bound there. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | 3619 | — | — |
| Config consumption | 🔴 `missing` | — | 3619 | — | — |
| Constant propagation | 🔴 `missing` | — | 3619 | — | — |
| Dead code detection | 🔴 `missing` | — | 3619 | — | — |
| Def use chain extraction | 🟢 `partial` | `2026-06-02` | 3903 | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use.go`<br>`internal/substrate/def_use_jsts.go`<br>`internal/substrate/substrate_jsts_graphql_codefirst_test.go` | #3903 (verify-first): def_use_jsts.go is registered per-LANGUAGE on "jsts" (file-extension dispatch via LanguageForPath, zero framework refs); def_use_pass.go composes intra-procedural reaching-definitions over the resulting VarDef/VarUse facts. Pothos .ts bodies yield real def-use chains attributed to their enclosing function. Proven by TestSubstrate_JSTS_Pothos_DefUseAttributes. Partial: attribution follows the per-language header scanner (named fns / const-arrows / plain methods bind; inline-arrow resolvers in t.field({...}) do not). |
| Env fallback recognition | 🔴 `missing` | — | 3619 | — | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/javascript/exception_flow.go`<br>`internal/extractors/javascript/exception_flow_test.go` | throw new X -> THROWS; e instanceof X catch-filter -> CATCHES; untyped throw/catch dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic JS/TS engine pass, fires regardless of framework). Verified to attribute to the enclosing function: LaunchDarkly ldClient.variation/boolVariation/stringVariation, Unleash unleash.isEnabled, OpenFeature client.getBooleanValue, Unleash-React useFlag, Split.io getTreatment, Flagsmith hasFeature, plus GrowthBook gb.isOn/isOff/getFeatureValue and ConfigCat configCatClient.getValue/getValueAsync (receiver-gated). Honest-partial: dynamic keys + non-flag receivers (button.isOn, formData.getValue) emit nothing. |
| Fs effect | 🔴 `missing` | — | 3619 | — | — |
| HTTP effect | 🟢 `partial` | `2026-06-02` | 3903 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/substrate_jsts_graphql_codefirst_test.go` | #3903 (verify-first): the per-LANGUAGE effect_sinks_jsts.go http_out detector (fetch/axios/got/ky/...) fires on Pothos .ts bodies and attributes to the enclosing function; effect_propagation.go binds it. Proven by TestSubstrate_JSTS_Pothos_EffectsAttribute (http_out attributed to persistUser). Partial: same standard-form attribution scope as db_effect. |
| Import resolution quality | 🔴 `missing` | — | 3619 | — | — |
| Module cycle detection | 🔴 `missing` | — | 3619 | — | — |
| Mutation effect | 🔴 `missing` | — | 3619 | — | — |
| Pure function tagging | 🔴 `missing` | — | 3619 | — | — |
| Reachability analysis | 🔴 `missing` | — | 3619 | — | — |
| Request shape extraction | 🔴 `missing` | — | 3619 | — | — |
| Request sink dataflow | 🔴 `missing` | — | 3740 | — | — |
| Response shape extraction | 🔴 `missing` | — | 3619 | — | — |
| Sanitizer recognition | 🔴 `missing` | — | 3619 | — | — |
| Schema drift detection | 🔴 `missing` | — | 3619 | — | — |
| Taint sink detection | 🟢 `partial` | `2026-06-02` | 3903 | `internal/links/taint_flow.go`<br>`internal/substrate/substrate_jsts_graphql_codefirst_test.go`<br>`internal/substrate/taint_sites_jsts.go` | #3903 (verify-first): the per-LANGUAGE taint_sites_jsts.go sink detector fires on Pothos .ts bodies — a raw-SQL concat (db.query('... ' + x)) is flagged as a sql_injection sink. Proven by TestSubstrate_JSTS_Pothos_TaintFires. Partial: the security-sensitive sink primitives are detected per-language regardless of framework; full source→sink flow linkage depends on handler attribution (see def_use/effects scope). |
| Taint source detection | 🔴 `missing` | — | 3619 | — | — |
| Template pattern catalog | 🔴 `missing` | — | 3619 | — | — |
| Vulnerability finding | 🔴 `missing` | — | 3619 | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.pothos ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
