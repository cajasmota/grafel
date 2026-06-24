<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.nestjs` — NestJS

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
| Endpoint pagination posture | 🟢 `partial` | `2026-06-03` | 3872 | `internal/engine/http_endpoint_pagination.go`<br>`internal/engine/http_endpoint_pagination_patterns.go`<br>`internal/engine/http_endpoint_pagination_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3628/#3872: applyEndpointPagination stamps paginated/pagination_style/pagination_params on the nestjs handler via the typed @Query() signature shape — limit:/offset: param declarations classified by classifyParamShape. Partial: typed-signature shape only. Honest-partial: a lone limit is ambiguous and left unstamped. |
| Endpoint response codes | ✅ `full` | `2026-06-02` | 3818 | `internal/engine/http_endpoint_response_codes.go`<br>`internal/engine/http_endpoint_response_codes_test.go`<br>`internal/engine/http_endpoint_synthesis.go` | #3818: response_codes + success_code + response_codes_source via applyEndpointResponseCodes. Nest signals: @HttpCode(NNN), HttpStatus.X enum, and built-in HttpException subclasses mapped to codes (NotFoundException->404, ForbiddenException->403, ConflictException->409, ...). Honest-partial: dynamic status skipped. |
| Endpoint synthesis | ✅ `full` | `2026-05-28` | 2932 | `internal/custom/javascript/nestjs.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/javascript_typescript/frameworks/nestjs.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | 2932 | `internal/custom/javascript/nestjs.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/javascript_typescript/frameworks/nestjs.yaml` | — |
| Route extraction | 🟢 `partial` | `2026-05-29` | 3062 | `internal/custom/javascript/nestjs.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_test.go` | — |
| Websocket route extraction | 🔴 `missing` | `2026-06-14` | — | — | #4965: dedicated websocket_route_extraction not yet implemented for this framework. The capability key was introduced for the rust axum/actix/warp WS extractor (internal/custom/rust/websocket_routes.go); this framework's WebSocket-upgrade idiom is not yet recognised and is a follow-up gap. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | 🔴 `missing` | — | view_rendering:#3628-not-yet-extracted | — | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-06-24` | — | `cmd/grafel/audit2852_jsauth_test.go`<br>`internal/engine/http_endpoint_jsts_auth.go`<br>`internal/engine/http_endpoint_jsts_auth_test.go`<br>`internal/engine/http_endpoint_jsts_authbody.go`<br>`internal/engine/http_endpoint_jsts_authbody_test.go`<br>`testdata/fixtures/typescript/nestjs_auth.ts` | #5499: a NestJS handler with NO @UseGuards / metadata decorator but a body opening with a home-rolled check (await this.authz.assertCan('x') / requireUser()) is now recovered via indexAuthBodyChecks (auth_method=check). |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | `2026-06-12` | — | `internal/custom/javascript/nestjs.go`<br>`internal/custom/javascript/reqresp_dto_test.go`<br>`internal/custom/javascript/validation_schema.go`<br>`internal/custom/javascript/validation_schema_test.go`<br>`internal/dashboard/shape_tree.go`<br>`internal/extractors/javascript/class_validator_fields.go`<br>`internal/extractors/javascript/issue4858_field_validations_test.go` | nestjs.go emits endpoint→DTO edges (#3629/#3607): each route SCOPE.Operation carries ACCEPTS_INPUT → Class:<Dto> for @Body()-typed params and RETURNS → Class:<Ret> for the typed return. Complementing that, validation_schema.go now extracts the class-validator DTO body itself: a class with @IsString/@IsInt/@IsEmail/... decorators is emitted as a SCOPE.Schema entity carrying captured field names+types (field_<name> props; TS annotation refines, else decorator type). Plain classes with no class-validator decorators are skipped (no false schema). #4858: each per-field SCOPE.Schema/field entity now also carries its class-validator constraints as a terse Properties["validations"] list (e.g. "IsString,MaxLength:120,IsOptional"; scalar-arg decorators such as MaxLength/Min/Max fold the bound into the chip), which the dashboard ShapeTree renders as per-field constraint chips next to the type. Tests: TestClassValidator_FieldsFromDecorators, TestClassValidator_PlainClassSkipped, TestDTO_ClassValidatorConstraintsStampedOnField, TestShape_FieldValidationsChips. |
| Request validation | 🟢 `partial` | `2026-05-29` | 3062 | `internal/extractors/javascript/issue2904_validation_linkage_test.go`<br>`internal/extractors/javascript/validation_linkage.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_jsts_middleware.go`<br>`internal/engine/http_endpoint_jsts_middleware_test.go`<br>`testdata/fixtures/typescript/nestjs_middleware.ts` | — |
| Rate limit stamping | 🔴 `missing` | — | [link](https://github.com/cajasmota/grafel/issues/3778) | — | endpoint rate-limit / throttle stamping not yet implemented for this framework; the #3628 child shipped express-rate-limit (JS/TS) + slowapi/django-ratelimit/flask-limiter/DRF (Python). express-slow-down-compatible / framework-native limiters for this framework are future work. |

### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Type graph extraction | — `not_applicable` | — | — | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-only concept; this framework is not a GraphQL server, so it has no GraphQL object-type relationship graph. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-06-02` | — | `internal/extractor/enum_valueset.go`<br>`internal/extractors/javascript/enum_valueset.go`<br>`internal/extractors/javascript/extractor.go` | TS enum_declaration -> SCOPE.Schema/enum AND a value-carrying SCOPE.Enum value-set node (emitTSEnumValueSet, kind_hint=ts_enum): string/numeric explicit members capture values (Status: Active=active; Level: Low=1), implicit/numeric-positional enums recorded value-less (honest-partial). String-literal union aliases type Role = 'admin'|'user' -> SCOPE.Enum (kind_hint=ts_literal_union) values admin,user; unions with any non-literal arm (string|Foo) emit no node. |
| Interface extraction | ✅ `full` | `2026-05-29` | 3160 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-29` | 3160 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |
| Type extraction | ✅ `full` | `2026-05-29` | 3160 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🟢 `partial` | `2026-06-02` | 3647 | `internal/custom/javascript/nestjs.go`<br>`internal/custom/javascript/nestjs_di.go`<br>`internal/custom/javascript/nestjs_di_test.go` | NestJS-unique DI/IoC graph (recorded under framework_specific because most http_backend frameworks have no DI container). @Module wiring → BINDS (module→provider/controller/import with an exported flag) and object-form provider tokens {provide, useClass/useValue/useFactory/useExisting} → token BINDS impl; class- and method-level @UseGuards/@UseInterceptors/@UsePipes → USES (controller/handler→guard/interceptor/pipe). Value-asserting tests prove specific edges: UsersModule BINDS UsersService; token CONFIG BINDS ConfigService; AdminController USES JwtAuthGuard; GET getSecret USES RolesGuard. Partial: token→impl BINDS resolves cross-file only when an entity with the matching token/class name exists in another file (regex single-file pass). |
| DI injection point | 🟢 `partial` | `2026-06-02` | 3647 | `internal/custom/javascript/nestjs_di.go`<br>`internal/custom/javascript/nestjs_di_test.go` | Constructor injection → INJECTED_INTO (provider→injecting class), including @Inject(TOKEN) string-literal and identifier custom tokens. Value-asserting tests prove UsersService INJECTED_INTO UsersController, @Inject('CONFIG_TOKEN') normalisation, and reject primitive params / cross-class leakage. Partial: the edge carries the bare type/token name; cross-file binding of that name to its declaring provider entity is the resolver's job and is not proven for cross-file fixtures here. |
| DI scope resolution | ✅ `full` | `2026-06-02` | 3647 | `internal/custom/javascript/nestjs.go`<br>`internal/custom/javascript/nestjs_di.go`<br>`internal/custom/javascript/nestjs_di_test.go` | @Injectable({ scope: Scope.REQUEST }) → di_scope property on the provider entity; @Injectable() classes tagged di_provider=true. Value-asserted by TestNestDIInjectableScope. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-06-11` | 3160 | `internal/custom/javascript/tests_route_e2e.go`<br>`internal/custom/javascript/tests_route_e2e_test.go`<br>`internal/engine/http_endpoint_e2e_testmap.go`<br>`internal/engine/http_endpoint_e2e_testmap_4399_test.go`<br>`internal/extractors/custom_dispatch.go`<br>`internal/extractors/javascript/tests.go`<br>`internal/extractors/javascript/tests_test.go` | Framework-blind JS/TS TESTS-edge emitter (tests.go). Covers Jest/Vitest/Mocha it()/test()/describe() blocks; NestJS projects typically use Jest so coverage is good. Jest/supertest e2e route-by-string calls (request(app).post('/x')) stamp e2e_route_calls and the shared linkE2ERouteTestsToEndpoints pass (#4351) emits the endpoint TESTS edge. #4399 (epic #4615) — Playwright/Cypress browser-e2e API-call route-hit -> endpoint TESTS edge: the NEW custom_js_tests_route_e2e extractor (jsTestRouteE2EExtractor) captures Playwright request-context calls (request.post('/api/x'), page.request.get('/api/x'), apiContext.fetch('/api/x',{method:'VERB'})) and Cypress calls (cy.request('VERB','/api/x'), cy.request({method,url}), cy.request('/api/x') default GET, cy.intercept('VERB','/api/x'), cy.intercept({method,url})), stamping VERB route pairs onto a one-per-spec test_suite's e2e_route_calls property that the same shared pass links to the http_endpoint_definition. Suite namespaced (e2e_route_suite:<spec>) to avoid by-name re-orphaning (#4343/#4366); dispatch under the canonical custom_js_ prefix + #4769 belt-and-suspenders re-listing. Honest exclusion: built/interpolated URLs (`${base}/x`, cy.request(url)) and a method-less .fetch are dropped (leading-slash literals only); non-spec files emit no suite. RED->GREEN value-asserted in http_endpoint_e2e_testmap_4399_test.go (PW POST + Cy GET link to /api/users; interpolated -> no edge) + tests_route_e2e_test.go. Remainder: multi-hop test-to-production linkage not proven for the NestJS-specific Jest fixture. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | `2026-05-29` | 3062 | `internal/patterns/observability_jsts_extractor.go`<br>`internal/patterns/observability_jsts_extractor_test.go` | — |
| Metric extraction | 🟢 `partial` | `2026-05-29` | 3160 | `internal/patterns/observability_jsts_extractor.go`<br>`internal/patterns/observability_jsts_extractor_test.go` | Heuristic import-pattern matching (prom-client, OTel metrics): fires when the app imports these specific libraries. Framework-agnostic but not comprehensive. |
| Trace extraction | ✅ `full` | — | 2905 | `internal/extractors/javascript/testdata/substrate_backend_observability/nestjs.ts`<br>`internal/patterns/observability_jsts_extractor.go` | — |

### Data

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | ✅ `full` | — | 2903 | `internal/extractors/javascript/testdata/substrate_backend_db/nestjs.ts`<br>`internal/substrate/backend_db_effect_test.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-28` | 2932 | `internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/jsts.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/javascript/config_consumer.go`<br>`internal/extractors/javascript/config_consumer_test.go` | process.env.X, import.meta.env.X, config.get(k) -> config:<key> DEPENDS_ON_CONFIG (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3160 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | Framework-blind BFS reachability seeded from entry_points_jsts.go (exports, main, test entries, lifecycle names). NestJS @Controller/@Injectable decorated classes serve as supplementary BFS seeds via HTTP endpoint graph entities. |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/def_use_jsts.go`<br>`internal/substrate/def_use_test.go` | Framework-blind heuristic: sniffDefUseJSTS fires on all JS/TS. Nearest-preceding-header attribution is imprecise for nested closures; common module/class-method case (standard NestJS service pattern) is correct. |
| Env fallback recognition | ✅ `full` | `2026-05-28` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/javascript/exception_flow.go`<br>`internal/extractors/javascript/exception_flow_test.go` | throw new X -> THROWS; e instanceof X catch-filter -> CATCHES; untyped throw/catch dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic JS/TS engine pass, fires regardless of framework). Verified to attribute to the enclosing function: LaunchDarkly ldClient.variation/boolVariation/stringVariation, Unleash unleash.isEnabled, OpenFeature client.getBooleanValue, Unleash-React useFlag, Split.io getTreatment, Flagsmith hasFeature, plus GrowthBook gb.isOn/isOff/getFeatureValue and ConfigCat configCatClient.getValue/getValueAsync (receiver-gated). Honest-partial: dynamic keys + non-flag receivers (button.isOn, formData.getValue) emit nothing. |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | Framework-blind: jstsFSReadRe/jstsFSWriteRe fire on all JS/TS covering Node fs/fs.promises primitives. Syntactic line-based attribution; framework-specific file helpers not covered. |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | Framework-blind: jstsHTTPRe detects outbound HTTP clients (fetch/axios/got/ky/superagent/XHR). Inbound route-handler effects not captured; confidence 1.0 for matched call sites. |
| Import resolution quality | ✅ `full` | `2026-06-11` | 4515 | `internal/external/synth.go`<br>`internal/external/synth_named_imports.go`<br>`internal/external/synth_named_imports_test.go`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/app.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/config.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/nest_app.ts`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | #4515: a NAMED import of an external framework symbol (import { NotFoundException } from '@nestjs/common') now synthesises a per-symbol external node ext:<pkg>:<Symbol> (subtype symbol, Name = bare symbol), keyed by package so two packages exporting the same name get distinct nodes. A bare-name reference (throw new NotFoundException(), extends BaseX, : SomeType) and the statically-recoverable namespace-member form (ns.Foo from import * as ns) bind to that single node; aliases ({ Foo as Bar }) converge by imported name. Reuses the precise classifier package canon (e.g. ext:org.apache.poi) and only appends the symbol leaf. Combined with #4480, the synthetic exception:NotFoundException node is retargeted onto the per-symbol ext class and dropped -> the imported-exception DUPLICATE disappears. ext: prefix preserved -> NOT a fidelity bug. |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3160 | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; applies equally to all JS/TS including NestJS. Accuracy depends on import resolution quality of the JS/TS extractor. |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | Framework-blind: jstsMutationRe detects this.field= receiver assignments (confidence 0.7). Plain variable mutations and array mutations not covered. |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3160 | `internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_jsts.go` | Derives from effect propagation: functions without detected effects are tagged pure (confidence 0.30). Absence-of-detection is not proof of absence; NestJS service-method closures and decorator-wrapped handlers may be mis-attributed. |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3160 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | BFS from export/main/test/lifecycle entry points and HTTP endpoint graph entities across CALLS/IMPORTS/REFERENCES edges. Entry-point detection is regex-based heuristic; NestJS @Controller handler methods are seeded via the endpoint synthesis graph. |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Request sink dataflow | ✅ `full` | `2026-06-02` | 3935 | `internal/links/dataflow_pass.go`<br>`internal/substrate/dataflow.go`<br>`internal/substrate/dataflow_jsts.go` | NestJS request-input → sink DATA_FLOWS_TO with DTO FIELD-LEVEL taint (#3902 + #3935, epics #3872/#3929): the JS/TS dataflow sniffer recognises NestJS controller-method parameters injected by @Body()/@Query()/@Param()/@Headers()/@Req()/@Request() as request-derived SOURCES (the role req.body plays in Express). dataflow_jsts.go: (a) jstsNestJSHeaders/jstsAllHeaders recover NestJS method headers the shared jstsFuncHeaderRe drops; (b) jstsNestJSParamTaints seeds each decorated param as a tainted root, field = the decorator literal key when present (@Body('email') email → field email; @Param('id') id → field id); (c) #3902 recovers the field from a single static member access off the param (@Body() dto → dto.email → field email); (d) #3935 ADDS object-destructuring projection — jstsTrackDestructure attributes the destructured property as the field: const {name} = dto → field name, const {email: e} = dto → field email (SOURCE prop, not the rebound local), one taint root per binding so multiple destructured fields carry the right field on each sink. Reuses the existing sink set (ORM create/save/insert/update, res.json/send, axios/fetch) and the full multi-hop + cross-file boundary machinery (DataFlowMaxHops=3). HONEST-PARTIAL: whole-object pass-through with no member/destructure keeps field="" (save(dto)); a dynamic member dto[k] and a rest element {...rest} attribute NO field; reassignment, branch-merge, collection mutation, embedded-arg, spread, recursion/entity-cycle, the 4th hop, and external/unresolved imports are still dropped; a constructor-injected (non-request) param is NOT a source. Tests: dataflow_jsts_nestjs_test.go (#3902) & dataflow_jsts_nestjs_bodyfield_test.go (#3935: destructure simple/renamed/multi-field, decorator-key direct, rest & dynamic-member & whole-object negatives). DEPLOY-DEFERRED (daemon not rebuilt). |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | Framework-blind: covers DOMPurify.sanitize, validator.escape, lodash.escape, he.encode, parameterised SQL, and zod/joi/yup schema declarations (per hard rule in issue 2772). |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | Framework-blind: covers SQL injection (db.query), command injection (exec/eval/new Function), path traversal (fs.* non-literal), XSS (innerHTML/res.send/dangerouslySetInnerHTML), ReDoS. NestJS response via @Res() not in sink set. |
| Taint source detection | ✅ `full` | `2026-05-29` | 3163 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_jsts_nestjs_test.go` | Framework-blind via jstsSourceReqRe (req.*/request.*/ctx.request.*). NestJS @Body()/@Query()/@Param() decorator-injected values are not matched by the current regex; coverage is best-effort for req.* access patterns. |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/template_pattern_jsts.go`<br>`internal/substrate/template_pattern_test.go` | Framework-blind: sniffTemplatePatternsJSTS covers i18n t(), log.*(), and SQL string literals across all JS/TS. NestJS-specific template helpers not covered. |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3163 | `internal/links/taint_flow.go`<br>`internal/links/taint_flow_test.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_jsts_nestjs_test.go` | Framework-blind taint_flow.go propagates source-to-sink paths. Decorator-injected sources (@Body/@Query/@Param/@Headers/@Req) now detected (full, #3163); sink detection covers SQL/command/path/XSS/ReDoS but NestJS @Res() response sink not in set. Chain is now source-complete for NestJS; partial on sinks. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.nestjs ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
