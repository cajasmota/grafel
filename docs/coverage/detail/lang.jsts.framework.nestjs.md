<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.nestjs` — NestJS

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Backend HTTP
- **Capability cells:** 41

## Capabilities


### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint synthesis | ✅ `full` | `2026-05-28` | 2932 | `internal/custom/javascript/nestjs.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/javascript_typescript/frameworks/nestjs.yaml` | — |
| Handler attribution | ✅ `full` | `2026-05-28` | 2932 | `internal/custom/javascript/nestjs.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/rules/javascript_typescript/frameworks/nestjs.yaml` | — |
| Route extraction | 🟢 `partial` | `2026-05-29` | 3062 | `internal/custom/javascript/nestjs.go`<br>`internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_synthesis_test.go` | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | ✅ `full` | `2026-05-28` | — | `cmd/archigraph/audit2852_jsauth_test.go`<br>`internal/engine/http_endpoint_jsts_auth.go`<br>`internal/engine/http_endpoint_jsts_auth_test.go`<br>`testdata/fixtures/typescript/nestjs_auth.ts` | — |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | ✅ `full` | — | 2904 | `internal/extractors/javascript/issue2904_validation_linkage_test.go`<br>`internal/extractors/javascript/validation_linkage.go`<br>`testdata/fixtures/typescript/nestjs_validation.ts` | — |
| Request validation | 🟢 `partial` | `2026-05-29` | 3062 | `internal/extractors/javascript/issue2904_validation_linkage_test.go`<br>`internal/extractors/javascript/validation_linkage.go` | — |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_jsts_middleware.go`<br>`internal/engine/http_endpoint_jsts_middleware_test.go`<br>`testdata/fixtures/typescript/nestjs_middleware.ts` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-29` | 3160 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |
| Interface extraction | ✅ `full` | `2026-05-29` | 3160 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-29` | 3160 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |
| Type extraction | ✅ `full` | `2026-05-29` | 3160 | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | 🟢 `partial` | `2026-05-29` | 3160 | `internal/extractors/javascript/tests.go`<br>`internal/extractors/javascript/tests_test.go` | Framework-blind JS/TS TESTS-edge emitter (tests.go). Covers Jest/Vitest/Mocha it()/test()/describe() blocks; NestJS projects typically use Jest so coverage is good. Multi-hop test-to-production linkage not proven for NestJS-specific fixture. |

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
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | Framework-blind: jstsFSReadRe/jstsFSWriteRe fire on all JS/TS covering Node fs/fs.promises primitives. Syntactic line-based attribution; framework-specific file helpers not covered. |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | Framework-blind: jstsHTTPRe detects outbound HTTP clients (fetch/axios/got/ky/superagent/XHR). Inbound route-handler effects not captured; confidence 1.0 for matched call sites. |
| Import resolution quality | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/testdata/substrate_import_resolution/app.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/config.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/nest_app.ts`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3160 | `internal/links/module_cycle_pass.go` | Language-agnostic Tarjan SCC over IMPORTS edges; applies equally to all JS/TS including NestJS. Accuracy depends on import resolution quality of the JS/TS extractor. |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | Framework-blind: jstsMutationRe detects this.field= receiver assignments (confidence 0.7). Plain variable mutations and array mutations not covered. |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3160 | `internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_jsts.go` | Derives from effect propagation: functions without detected effects are tagged pure (confidence 0.30). Absence-of-detection is not proof of absence; NestJS service-method closures and decorator-wrapped handlers may be mis-attributed. |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3160 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | BFS from export/main/test/lifecycle entry points and HTTP endpoint graph entities across CALLS/IMPORTS/REFERENCES edges. Entry-point detection is regex-based heuristic; NestJS @Controller handler methods are seeded via the endpoint synthesis graph. |
| Request shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | Framework-blind: covers DOMPurify.sanitize, validator.escape, lodash.escape, he.encode, parameterised SQL, and zod/joi/yup schema declarations (per hard rule in issue 2772). |
| Schema drift detection | ✅ `full` | `2026-05-27` | — | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_test.go` | Framework-blind: covers SQL injection (db.query), command injection (exec/eval/new Function), path traversal (fs.* non-literal), XSS (innerHTML/res.send/dangerouslySetInnerHTML), ReDoS. NestJS response via @Res() not in sink set. |
| Taint source detection | ✅ `full` | `2026-05-29` | 3163 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_jsts_nestjs_test.go` | Framework-blind via jstsSourceReqRe (req.*/request.*/ctx.request.*). NestJS @Body()/@Query()/@Param() decorator-injected values are not matched by the current regex; coverage is best-effort for req.* access patterns. |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3160 | `internal/substrate/template_pattern_jsts.go`<br>`internal/substrate/template_pattern_test.go` | Framework-blind: sniffTemplatePatternsJSTS covers i18n t(), log.*(), and SQL string literals across all JS/TS. NestJS-specific template helpers not covered. |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3163 | `internal/links/taint_flow.go`<br>`internal/links/taint_flow_test.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_jsts_nestjs_test.go` | Framework-blind taint_flow.go propagates source-to-sink paths. Decorator-injected sources (@Body/@Query/@Param/@Headers/@Req) now detected (full, #3163); sink detection covers SQL/command/path/XSS/ReDoS but NestJS @Res() response sink not in set. Chain is now source-complete for NestJS; partial on sinks. |

## Framework-specific

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | 🟢 `partial` | `2026-06-02` | 3647 | `internal/custom/javascript/nestjs.go`<br>`internal/custom/javascript/nestjs_di.go`<br>`internal/custom/javascript/nestjs_di_test.go` | NestJS-unique DI/IoC graph (recorded under framework_specific because most http_backend frameworks have no DI container). @Module wiring → BINDS (module→provider/controller/import with an exported flag) and object-form provider tokens {provide, useClass/useValue/useFactory/useExisting} → token BINDS impl; class- and method-level @UseGuards/@UseInterceptors/@UsePipes → USES (controller/handler→guard/interceptor/pipe). Value-asserting tests prove specific edges: UsersModule BINDS UsersService; token CONFIG BINDS ConfigService; AdminController USES JwtAuthGuard; GET getSecret USES RolesGuard. Partial: token→impl BINDS resolves cross-file only when an entity with the matching token/class name exists in another file (regex single-file pass). |
| DI injection point | 🟢 `partial` | `2026-06-02` | 3647 | `internal/custom/javascript/nestjs_di.go`<br>`internal/custom/javascript/nestjs_di_test.go` | Constructor injection → INJECTED_INTO (provider→injecting class), including @Inject(TOKEN) string-literal and identifier custom tokens. Value-asserting tests prove UsersService INJECTED_INTO UsersController, @Inject('CONFIG_TOKEN') normalisation, and reject primitive params / cross-class leakage. Partial: the edge carries the bare type/token name; cross-file binding of that name to its declaring provider entity is the resolver's job and is not proven for cross-file fixtures here. |
| DI scope resolution | ✅ `full` | `2026-06-02` | 3647 | `internal/custom/javascript/nestjs.go`<br>`internal/custom/javascript/nestjs_di.go`<br>`internal/custom/javascript/nestjs_di_test.go` | @Injectable({ scope: Scope.REQUEST }) → di_scope property on the provider entity; @Injectable() classes tagged di_provider=true. Value-asserted by TestNestDIInjectableScope. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.nestjs ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
