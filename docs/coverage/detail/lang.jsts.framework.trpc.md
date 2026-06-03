<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.trpc` — tRPC

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 54

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Federation extraction | — `not_applicable` | — | 3623 | — | Apollo GraphQL Federation directives (@key/@external/@requires/@provides/extend type) do not exist in this RPC framework; not applicable. |
| Procedure extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_trpc.go`<br>`internal/engine/rules/javascript_typescript/frameworks/trpc.yaml` | — |
| Schema extraction | ✅ `full` | `2026-05-28` | 2865 | `internal/engine/http_endpoint_trpc.go`<br>`internal/engine/http_endpoint_trpc_schema.go`<br>`internal/engine/http_endpoint_trpc_schema_test.go`<br>`testdata/fixtures/typescript/trpc_input_schema.ts` | — |
| Type graph extraction | — `not_applicable` | — | 3804 | — | GraphQL schema type→type graph (object-typed field -> referenced object type with list/nullable cardinality) is a GraphQL-SDL concept; gRPC/protobuf/tRPC message schemas are modelled separately and have no GraphQL object-type relationship graph. |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | ✅ `full` | — | 2865 | `internal/engine/rules/javascript_typescript/frameworks/trpc.yaml`<br>`internal/engine/trpc_client_codegen_test.go`<br>`testdata/fixtures/typescript/trpc_client_codegen.ts` | — |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | ✅ `full` | `2026-05-28` | 2906 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_transport_binding.go`<br>`internal/engine/http_endpoint_transport_binding_test.go`<br>`testdata/fixtures/typescript/trpc_transport_http.ts`<br>`testdata/fixtures/typescript/trpc_transport_http_ws.ts`<br>`testdata/fixtures/typescript/trpc_transport_none.ts`<br>`testdata/fixtures/typescript/trpc_transport_ws.ts` | — |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Endpoint deprecation versioning | — `not_applicable` | — | — | — | HTTP endpoint deprecation/versioning (Sunset/Deprecation headers, /v1 route segments) is an HTTP-API concept; tRPC versions via proto package + service evolution, not HTTP route versioning. |
| Endpoint pagination posture | — `not_applicable` | — | — | — | HTTP limit/offset/page/cursor pagination posture is an HTTP-endpoint concept; tRPC paginates via in-message fields / server-streaming, not HTTP query params. |
| Endpoint response codes | — `not_applicable` | — | — | — | HTTP status-code sets do not apply to tRPC, which signals outcome via zod-typed input on the trailer, not HTTP 2xx/4xx codes. |
| Endpoint synthesis | — `not_applicable` | — | — | — | HTTP http_endpoint synthesis (path+verb producer endpoints) does not apply to tRPC; service registration is captured as transport_binding. tRPC method addressing is package.Service/Method, not an HTTP path+verb. |
| Handler attribution | — `not_applicable` | — | — | — | No HTTP handler->route attribution for tRPC. RPC-method->service binding is modelled by procedure_extraction (rpc/service) + the service-impl in schema_extraction, not by an HTTP route table. |
| Route extraction | — `not_applicable` | — | — | — | tRPC has no HTTP route paths; the HTTP route extractor emits 0 entities on a tRPC service impl. RPC method addressing is package.Service/Method, surfaced via procedure_extraction + transport_binding. |

### View

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| View rendering | — `not_applicable` | — | — | — | tRPC services render no server-side views/templates; responses are protobuf messages (tRPC returns plain typed values), not rendered views. |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | — | 4041 | `internal/engine/http_endpoint_jsts_auth.go`<br>`internal/engine/http_endpoint_trpc_auth.go`<br>`internal/engine/http_endpoint_trpc_auth_test.go`<br>`testdata/fixtures/typescript/trpc_auth.ts`<br>`testdata/fixtures/typescript/trpc_auth_imported.ts` | tRPC auth is now detected (#4041): applyTRPCAuthBinding re-walks the routers and stamps auth_required=true + auth_method=trpc_middleware + auth_middleware (MCP signal-1) on each procedure built from an auth-enforcing middleware (t.middleware throwing TRPCError UNAUTHORIZED/FORBIDDEN or guarding a ctx principal) or a protectedProcedure builder (BASE.use(isAuthed)), plus inline `.use(({ctx,next})=>{...throw...})` per-procedure. VALUE-ASSERTING tests prove auth on getUser/deleteAll and NO auth on publicProcedure / logging-middleware procedures. HONEST PARTIAL: resolution is same-file for in-file builders/middleware (HIGH confidence); an imported protectedProcedure is credited by name convention (MEDIUM). Residual gap → still missing: an auth middleware defined in ANOTHER module that is composed into a NON-conventionally-named local builder (cross-file binding not chased, mirroring the same-file scope of synthesizeTRPC/applyTRPCSchemaBinding). The DEPLOY-DEFERRED gRPC siblings (c-cpp.grpc, elixir.grpc, scala.scalapb-grpc, grpc-net) remain open under #4041. |

### Validation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DTO extraction | — `not_applicable` | — | — | — | tRPC request/response payloads are protobuf messages (tRPC: zod-typed inputs), surfaced under schema_extraction; MVC DTO inference does not model them. HTTP MVC DTO extractor yields 0 entities on a tRPC service. |
| Request validation | — `not_applicable` | — | — | — | tRPC has no MVC request-validation pipeline; message-field constraints live in proto (tRPC validates via the zod input schema, surfaced as schema, not an MVC validator). HTTP MVC validation extractor yields 0 entities here. |

### Middleware

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Middleware coverage | — `not_applicable` | — | — | — | tRPC cross-cutting concerns use interceptors (tRPC: middleware links), not the HTTP framework's Use*/plug/filter request-pipeline. The HTTP middleware extractor yields 0 entities on a tRPC service; interceptor cataloguing is potential future work. |
| Rate limit stamping | — `not_applicable` | — | — | — | HTTP endpoint rate-limit/throttle stamping is an HTTP-middleware concept; tRPC throttling is done via interceptors/server options, not HTTP rate limiters. |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-06-03` | — | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | language-level type-system sniffer is framework-agnostic; fires on enums in tRPC service/contract code. VERIFIED on the tRPC idiom. |
| Interface extraction | ✅ `full` | `2026-06-03` | — | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | framework-agnostic; fires on the tRPC service impl/contract interface. VERIFIED on the tRPC idiom. |
| Type alias extraction | ✅ `full` | `2026-06-03` | — | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | framework-agnostic; fires on type aliases in tRPC code. VERIFIED on the tRPC idiom. |
| Type extraction | ✅ `full` | `2026-06-03` | — | `internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue1343_ts_type_extraction_test.go` | framework-agnostic; fires on every tRPC service-impl class / payload struct / case class. VERIFIED on the tRPC idiom. |

### DI

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DI binding extraction | — `not_applicable` | — | — | — | tRPC has no DI container; procedures are plain functions/closures. The NestJS DI sniffer (@Injectable/@Module) yields 0 entities on a tRPC router. No DI mechanism to surface. |
| DI injection point | — `not_applicable` | — | — | — | tRPC has no DI container; procedures are plain functions/closures. The NestJS DI sniffer (@Injectable/@Module) yields 0 entities on a tRPC router. No DI mechanism to surface. |
| DI scope resolution | — `not_applicable` | — | — | — | tRPC has no DI container; procedures are plain functions/closures. The NestJS DI sniffer (@Injectable/@Module) yields 0 entities on a tRPC router. No DI mechanism to surface. |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-03` | — | `internal/extractors/javascript/tests.go`<br>`internal/extractors/javascript/tests_test.go` | test-framework sniffer keys on the standard test macros, not the framework-under-test; links tRPC service/router tests like any other test. VERIFIED a named test case is extracted from a tRPC test idiom. |

### Observability

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Log extraction | 🟢 `partial` | — | — | `internal/patterns/observability_jsts_extractor.go`<br>`internal/patterns/observability_jsts_extractor_test.go` | logging sniffer is library/call-keyed (framework-agnostic); fires on a tRPC service that logs. VERIFIED on the tRPC idiom. PARTIAL: heuristic. |
| Metric extraction | 🟢 `partial` | — | — | `internal/patterns/observability_jsts_extractor.go`<br>`internal/patterns/observability_jsts_extractor_test.go` | metric sniffer is library-keyed (framework-agnostic); fires for tRPC services that instrument metrics. VERIFIED on the tRPC idiom. PARTIAL: heuristic. |
| Trace extraction | 🟢 `partial` | — | — | `internal/patterns/observability_jsts_extractor.go`<br>`internal/patterns/observability_jsts_extractor_test.go` | trace sniffer is library-keyed (framework-agnostic); fires for tRPC services that instrument traces. PARTIAL: heuristic. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/jsts.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/javascript/config_consumer.go`<br>`internal/extractors/javascript/config_consumer_test.go` | process.env.X, import.meta.env.X, config.get(k) -> config:<key> DEPENDS_ON_CONFIG (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | — |
| DB effect | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3057 | `internal/patterns/dead_module_detector.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 3057 | `internal/substrate/def_use_jsts.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/javascript/exception_flow.go`<br>`internal/extractors/javascript/exception_flow_test.go` | throw new X -> THROWS; e instanceof X catch-filter -> CATCHES; untyped throw/catch dropped (#3628) |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-29` | 3057 | `internal/substrate/payload_shapes_jsts.go` | — |
| Request sink dataflow | — `not_applicable` | — | — | — | The request->sink dataflow sniffer roots taint at HTTP MVC binders / request accessors. tRPC methods take a strongly-typed protobuf request (tRPC: a zod-typed input) + call context, with no HTTP MVC request-root to seed; not applicable to this transport. |
| Response shape extraction | 🟢 `partial` | `2026-05-29` | 3057 | `internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | ctx.req.body/params shapes are detected by jstsSourceReqRe; the typed input parameter (primary user-input channel in tRPC, post-zod validation) is a known gap — not matched by current sniffer |
| Schema drift detection | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/payload_drift.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | ctx.req.body/params shapes are detected by jstsSourceReqRe; the typed input parameter (primary user-input channel in tRPC, post-zod validation) is a known gap — not matched by current sniffer |
| Taint source detection | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | ctx.req.body/params shapes are detected by jstsSourceReqRe; the typed input parameter (primary user-input channel in tRPC, post-zod validation) is a known gap — not matched by current sniffer |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3057 | `internal/substrate/template_pattern_jsts.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | ctx.req.body/params shapes are detected by jstsSourceReqRe; the typed input parameter (primary user-input channel in tRPC, post-zod validation) is a known gap — not matched by current sniffer |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.trpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
