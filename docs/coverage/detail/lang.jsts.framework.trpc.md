<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.trpc` — tRPC

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** RPC Framework
- **Capability cells:** 25

## Capabilities


### Schema

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Procedure extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/http_endpoint_trpc.go`<br>`internal/engine/rules/javascript_typescript/frameworks/trpc.yaml` | — |
| Schema extraction | ✅ `full` | `2026-05-28` | 2865 | `internal/engine/http_endpoint_trpc.go`<br>`internal/engine/http_endpoint_trpc_schema.go`<br>`internal/engine/http_endpoint_trpc_schema_test.go`<br>`testdata/fixtures/typescript/trpc_input_schema.ts` | — |

### Codegen

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Client codegen | ✅ `full` | — | 2865 | `internal/engine/rules/javascript_typescript/frameworks/trpc.yaml`<br>`internal/engine/trpc_client_codegen_test.go`<br>`testdata/fixtures/typescript/trpc_client_codegen.ts` | — |

### Transport

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transport binding | ✅ `full` | `2026-05-28` | 2906 | `internal/engine/http_endpoint_synthesis.go`<br>`internal/engine/http_endpoint_transport_binding.go`<br>`internal/engine/http_endpoint_transport_binding_test.go`<br>`testdata/fixtures/typescript/trpc_transport_http.ts`<br>`testdata/fixtures/typescript/trpc_transport_http_ws.ts`<br>`testdata/fixtures/typescript/trpc_transport_none.ts`<br>`testdata/fixtures/typescript/trpc_transport_ws.ts` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/jsts.go` | — |
| Constant propagation | ✅ `full` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | — |
| DB effect | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Dead code detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/patterns/dead_module_detector.go` | — |
| Def use chain extraction | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/substrate/def_use_jsts.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | — |
| Fs effect | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| HTTP effect | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | — |
| Module cycle detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Pure function tagging | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Request shape extraction | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/substrate/payload_shapes_jsts.go` | — |
| Response shape extraction | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | ctx.req.body/params shapes are detected by jstsSourceReqRe; the typed input parameter (primary user-input channel in tRPC, post-zod validation) is a known gap — not matched by current sniffer |
| Schema drift detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/payload_drift.go` | — |
| Taint sink detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | ctx.req.body/params shapes are detected by jstsSourceReqRe; the typed input parameter (primary user-input channel in tRPC, post-zod validation) is a known gap — not matched by current sniffer |
| Taint source detection | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | ctx.req.body/params shapes are detected by jstsSourceReqRe; the typed input parameter (primary user-input channel in tRPC, post-zod validation) is a known gap — not matched by current sniffer |
| Template pattern catalog | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/substrate/template_pattern_jsts.go` | — |
| Vulnerability finding | ⚠️ `partial` | `2026-05-29` | 3057 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_trpc/router.ts` | ctx.req.body/params shapes are detected by jstsSourceReqRe; the typed input parameter (primary user-input channel in tRPC, post-zod validation) is a known gap — not matched by current sniffer |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.trpc ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
