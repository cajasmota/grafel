<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.express` — Express

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 50

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | ✅ `full` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_deprecation.go`<br>`internal/engine/http_endpoint_deprecation_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628 epic: api_version (path /api/vN,/vN) + deprecated/deprecated_since/deprecated_replacement/deprecation_source on http_endpoint_definition via applyEndpointAPIVersion+applyEndpointDeprecation. JS/TS: JSDoc @deprecated on handler, deprecated:true, Sunset/Deprecation response header. |
| Endpoint pagination posture | ✅ `full` | `2026-06-02` | 3628 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628: paginated/pagination_style(offset|page|cursor)/pagination_params/pagination_source on http_endpoint_definition via applyEndpointPagination. Direct signals: DRF pagination_class + DEFAULT_PAGINATION_CLASS, Django Paginator, FastAPI/fastapi-pagination, Spring Pageable/Page<>, Express req.query, Sequelize/Prisma take/skip/.cursor(). Honest-partial: lone limit not stamped. |
| Endpoint response codes | ✅ `full` | `2026-06-02` | 3818 | `internal/engine/http_endpoint_response_codes.go`<br>`internal/engine/http_endpoint_response_codes_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3818: response_codes + success_code + response_codes_source via applyEndpointResponseCodes. Express signals: res.status(NNN), res.sendStatus(NNN), res.statusCode = NNN body literals. Honest-partial: res.status(dynamicVar) skipped; no literal -> absent. |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | 2932 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_test.go`<br>`internal/engine/rules/javascript_typescript/frameworks/express.yaml`<br>`internal/extractors/javascript/framework_dsl.go` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | 2932 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/javascript_typescript/frameworks/express.yaml` | — |
| Route extraction | ✅ `full` | `2026-05-29` | 3062 | `internal/custom/javascript/express.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_jsts_route_3062_test.go`<br>`internal/engine/http_endpoint_synthesis_test.go` | — |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/template_render.go`<br>`internal/extractors/javascript/template_render.go`<br>`internal/extractors/javascript/template_render_test.go` | res.render('view') -> RENDERS SCOPE.Template; response-receiver heuristic; dynamic/template-literal names dropped (#3628) |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-28` | — | `cmd/archigraph/audit2852_jsauth_test.go`<br>`internal/engine/http_endpoint_jsts_auth.go`<br>`internal/engine/http_endpoint_jsts_auth_test.go`<br>`testdata/fixtures/typescript/express_auth.ts` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-02` | — | `internal/custom/javascript/express.go`<br>`internal/custom/javascript/reqresp_dto_test.go`<br>`internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go` | express.go emits endpoint→DTO edges for TypeScript handlers typed via Request<P,ResBody,ReqBody> / Response<ResBody> (#3629/#3607). Complementing that, validation_schema.go now captures runtime validation-schema contracts — Zod (z.object), Joi (Joi.object), Yup (yup.object().shape) — as SCOPE.Schema entities carrying captured field names+scalar types (field_<name> props) and binds them to the route that concretely references the schema (Schema.parse(req.body) / validate(Schema) middleware) via ACCEPTS_INPUT → Schema:<name> (RETURNS when applied to a response value). Unused or dynamically-built schemas yield a schema entity but no endpoint edge (honest-partial). Tests: TestZodSchema_FieldsAndAcceptsInput, TestJoiSchema_FieldsAndAcceptsInput, TestYupSchema_ShapeFieldsAndAcceptsInput, TestZodSchema_UnusedNoBinding, TestDynamicSchema_NoBinding. |
| Request validation | ✅ `full` | — | 2904 | `internal/extractors/javascript/issue2904_validation_linkage_test.go`<br>`internal/extractors/javascript/validation_linkage.go`<br>`testdata/fixtures/typescript/express_validation.ts` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_jsts_middleware.go`<br>`internal/engine/http_endpoint_jsts_middleware_test.go`<br>`testdata/fixtures/typescript/express_middleware.ts` | — |
| Rate limit stamping | ✅ `full` | `2026-06-02` | [link](https://github.com/cajasmota/archigraph/issues/3778) | `internal/engine/http_endpoint_jsts_ratelimit.go`<br>`internal/engine/http_endpoint_jsts_ratelimit_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | express-rate-limit / express-slow-down: resolves windowMs+max to a human rate and stamps rate_limited/rate_limit/rate_limit_scope (route|app)/rate_limit_source on the endpoint op. Imported/config-driven limiters → rate_limited=true with rate omitted (honest-partial). Negative: a limiter defined but never applied to a route is not stamped. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-29` | 3050 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |
| Interface extraction | ✅ `full` | `2026-05-29` | 3050 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-29` | 3050 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |
| Type extraction | ✅ `full` | `2026-05-29` | 3050 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🔴 `missing` | — | 3628 | — | — |
| DI injection point | 🔴 `missing` | — | 3628 | — | — |
| DI scope resolution | 🔴 `missing` | — | 3628 | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-29` | 3050 | `internal/extractors/javascript/tests.go`<br>`internal/extractors/javascript/tests_test.go` | — |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | ✅ `full` | — | 2905 | `internal/extractors/javascript/testdata/substrate_backend_observability/express.ts`<br>`internal/patterns/observability_jsts_extractor.go` | — |
| Metric extraction | 🟢 `partial` | `2026-05-29` | 3050 | `internal/patterns/observability_jsts_extractor.go`<br>`internal/patterns/observability_jsts_extractor_test.go` | Heuristic import-pattern matching (prom-client, OTel metrics): fires when the app imports these specific libraries. Framework-agnostic but not comprehensive. |
| Trace extraction | 🟢 `partial` | `2026-05-29` | 3050 | `internal/patterns/observability_jsts_extractor.go`<br>`internal/patterns/observability_jsts_extractor_test.go` | Heuristic import-pattern matching (OTel tracing, Sentry): fires when the app imports these specific libraries. Framework-agnostic but not comprehensive. |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | ✅ `full` | — | 2903 | `internal/extractors/javascript/testdata/substrate_backend_db/express.ts`<br>`internal/substrate/backend_db_effect_test.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | 2932 | `internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/jsts.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/javascript/config_consumer.go`<br>`internal/extractors/javascript/config_consumer_test.go` | process.env.X, import.meta.env.X, config.get(k) -> config:<key> DEPENDS_ON_CONFIG (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3046 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | Framework-blind BFS reachability seeded from entry_points_jsts.go (exports, main, test entries, lifecycle names). Framework-specific entrypoints (Fastify plugin.register, Hono app.get) rely on graph HTTP endpoint entities as supplementary BFS seeds. |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 3046 | `internal/substrate/def_use_jsts.go`<br>`internal/substrate/def_use_test.go` | Framework-blind heuristic: sniffDefUseJSTS fires on all JS/TS. Nearest-preceding-header attribution is imprecise for nested closures; common module/class-method case is correct. |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/javascript/exception_flow.go`<br>`internal/extractors/javascript/exception_flow_test.go` | throw new X -> THROWS; e instanceof X catch-filter -> CATCHES; untyped throw/catch dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic JS/TS engine pass, fires regardless of framework). Verified to attribute to the enclosing function: LaunchDarkly ldClient.variation/boolVariation/stringVariation, Unleash unleash.isEnabled, OpenFeature client.getBooleanValue, Unleash-React useFlag, Split.io getTreatment, Flagsmith hasFeature, plus GrowthBook gb.isOn/isOff/getFeatureValue and ConfigCat configCatClient.getValue/getValueAsync (receiver-gated). Honest-partial: dynamic keys + non-flag receivers (button.isOn, formData.getValue) emit nothing. |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3046 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | Framework-blind: jstsFSReadRe/jstsFSWriteRe fire on all JS/TS covering Node fs/fs.promises primitives. Syntactic line-based attribution; framework-specific file helpers not covered. |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3046 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | Framework-blind: jstsHTTPRe detects outbound HTTP clients (fetch/axios/got/ky/superagent/XHR). Inbound route-handler effects not captured; confidence 1.0 for matched call sites. |
| Import resolution quality | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_import_resolution/app.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/config.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/nest_app.ts`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3046 | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; applies equally to all JS/TS including these frameworks. Accuracy depends on import resolution quality of the JS/TS extractor. |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3046 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | Framework-blind: jstsMutationRe detects this.field= receiver assignments (confidence 0.7). Plain variable mutations and array mutations not covered. |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3046 | `internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_jsts.go` | Derives from effect propagation: functions without detected effects are tagged pure (confidence 0.30). Absence-of-detection is not proof of absence; framework handler closures may be mis-attributed. |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3046 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | BFS from export/main/test/lifecycle entry points and HTTP endpoint graph entities across CALLS/IMPORTS/REFERENCES edges. Entry-point detection is regex-based heuristic for framework handler functions. |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Request sink dataflow | 🟢 `partial` | `2026-06-02` | 3740 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_jsts.go` | SCOPED request-input → sink DATA_FLOWS_TO (#3628 area #22): intra-fn assignment tracking + multi-hop (≤DataFlowMaxHops=3) local-call propagation AND cross-file propagation into imported helpers. Multi-hop: value followed through nested local calls A→B→C, each bound by exact positional index; full chain in hop_path/hop_count props. Cross-file (#3772): when the callee is an imported symbol resolving (via the CALLS graph) to exactly one same-repo function entity, that file is read and the bounded walk continues there (continueDataFlowJSTS); sink resolves to the callee-file entity. Sources req.body/query/params, ctx.request.body. Sinks ORM create/save/insert, res.json/send, axios/fetch. HONEST-PARTIAL (precision-first): drops reassignment, branch-merge, collection mutation, dynamic keys, embedded-arg (helper(x+1)), spread/rest/destructured params, recursion/entity-cycle, the 4th hop, and external/unresolved/ambiguous imports. NestJS decorator-injected params (@Body/@Query/@Param/@Headers/@Req) are also recognised as request sources (#3902). DEPLOY-DEFERRED (daemon not rebuilt). |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3046 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | Framework-blind: covers DOMPurify.sanitize, validator.escape, lodash.escape, he.encode, parameterised SQL, and zod/joi/yup schema declarations (per hard rule in issue 2772). |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 3046 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | Framework-blind: covers SQL injection (db.query), command injection (exec/eval/new Function), path traversal (fs.* non-literal), XSS (innerHTML/res.send/dangerouslySetInnerHTML), ReDoS. Hono c.json()/Koa ctx.body response API not in sink set. |
| Taint source detection | 🟢 `partial` | `2026-05-29` | 3046 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | Framework-blind via jstsSourceReqRe (req.*/request.*/ctx.request.*). Covers Express/Fastify/Koa req.body/query/params/headers/cookies well. Hapi request.payload and Hono c.req.json()/c.req.param() not matched by current regex. |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3046 | `internal/substrate/template_pattern_jsts.go`<br>`internal/substrate/template_pattern_test.go` | Framework-blind: sniffTemplatePatternsJSTS covers i18n t(), log.*(), and SQL string literals across all JS/TS. Framework-specific templating (Koa ctx.render, Hono c.html) not covered. |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3046 | `internal/links/taint_flow.go`<br>`internal/links/taint_flow_test.go` | Framework-blind: taint_flow.go propagates source-to-sink paths identified by sniffTaintJSTS. Quality inherits from partial taint_source/sink coverage; Hapi/Hono-specific sources are underdetected. |

## Framework-specific

### Express Contract

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Effective contract | 🟢 `partial` | `2026-06-11` | [link](https://github.com/cajasmota/archigraph/issues/4710) | `internal/authposture/express.go`<br>`internal/mcp/effective_contract_express.go`<br>`internal/mcp/effective_contract_express_4710_test.go`<br>`internal/mcp/effective_contract_fw_common.go`<br>`internal/mcp/effective_contract_registry.go` | #4710: the expressContractResolver in the framework-pluggable effective_contract registry (#4601) composes the per-endpoint contract for Express/Fastify/Koa/Hapi/Hono from signals already on the graph (re-extracts nothing): request fields from the handler's VALIDATES->dto:<schema> (zod/joi/celebrate, #3073/#4635) field members + Fastify schema.body via request_body_type + path/query scalars; per-branch numeric response statuses from the JSTS branch analyzer (res.status(NNN).json/reply.code(NNN), substrate analyzeBranchesJSTS); auth posture from the new authposture express resolver (passport/requireAuth/role-middleware). PARTIAL by design — Express is the loosest stack: untyped req.body with NO validation schema yields no request fields (nothing recoverable), and most Express apps carry no structured RBAC so auth resolves authenticated-only/unknown. Value-asserting test TestEffectiveContract_Express_FullContract: zod body fields + 404/409 branch statuses + authenticated posture. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.express ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
