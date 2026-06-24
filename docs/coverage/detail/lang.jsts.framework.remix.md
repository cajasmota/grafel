<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.remix` — Remix

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 39

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/grafel/issues/2857) | `internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/react_shared.go`<br>`internal/custom/javascript/remix.go` | — |
| Hook recognition | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/grafel/issues/2857) | `internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/react_shared.go`<br>`internal/custom/javascript/remix.go` | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Data loaders | ✅ `full` | — | [link](https://github.com/cajasmota/grafel/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/remix.go` | — |

### Server

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Hydration boundaries | ✅ `full` | — | [link](https://github.com/cajasmota/grafel/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/metafw_server.go`<br>`internal/custom/javascript/remix.go` | — |
| Server components | ✅ `full` | — | [link](https://github.com/cajasmota/grafel/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/metafw_server.go`<br>`internal/custom/javascript/remix.go` | — |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Route extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/frameworks/remix.yaml` | — |
| Router pattern | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/grafel/issues/2857) | `internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/remix.go` | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🟢 `partial` | `2026-06-24` | 5499 | `internal/engine/http_endpoint_jsts_auth.go`<br>`internal/engine/http_endpoint_jsts_authbody.go`<br>`internal/engine/http_endpoint_jsts_authbody_test.go` | #5499: home-rolled / custom auth checks in handler bodies. indexAuthBodyChecks scans each route-handler / server-action / loader body opener for an inline authorization check — a require*/authorize/assertCan/checkPermission/getServerSession-family call (jsAuthCheckLeafSet) or an `if (!session|!user) throw|redirect('/login')` guard — and stamps the AuthPolicy contract (auth_required/auth_method=check/auth_guard/auth_roles/auth_permissions/auth_scopes + the JSON source chain), i.e. the AUTHORIZES relation from the endpoint to its check callee. Confined to the body opener (no false positives on ordinary calls). Honest-partial: a dynamically-dispatched / runtime-assembled check, and framework session middleware config, are out of scope here. |

### Build

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Static generation | ✅ `full` | — | [link](https://github.com/cajasmota/grafel/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/remix.go` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | ✅ `full` | — | [link](https://github.com/cajasmota/grafel/issues/2858) | `internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2858_metafw_state_setter_test.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-05-29` | 3055 | `internal/links/effect_propagation.go`<br>`internal/links/taint_flow.go`<br>`internal/substrate/jsts.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/javascript/config_consumer.go`<br>`internal/extractors/javascript/config_consumer_test.go` | process.env.X, import.meta.env.X, config.get(k) -> config:<key> DEPENDS_ON_CONFIG (issue #3641) |
| Constant propagation | ✅ `full` | `2026-05-29` | 3055 | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| DB effect | ✅ `full` | `2026-05-29` | 3055 | `internal/substrate/backend_db_effect_test.go`<br>`internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | — |
| Dead code detection | 🟢 `partial` | `2026-05-29` | 3055 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 3055 | `internal/substrate/def_use_jsts.go`<br>`internal/substrate/def_use_test.go` | — |
| Env fallback recognition | ✅ `full` | `2026-05-29` | 3055 | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/javascript/exception_flow.go`<br>`internal/extractors/javascript/exception_flow_test.go` | throw new X -> THROWS; e instanceof X catch-filter -> CATCHES; untyped throw/catch dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic JS/TS engine pass, fires regardless of framework). Verified to attribute to the enclosing function: LaunchDarkly ldClient.variation/boolVariation/stringVariation, Unleash unleash.isEnabled, OpenFeature client.getBooleanValue, Unleash-React useFlag, Split.io getTreatment, Flagsmith hasFeature, plus GrowthBook gb.isOn/isOff/getFeatureValue and ConfigCat configCatClient.getValue/getValueAsync (receiver-gated). Honest-partial: dynamic keys + non-flag receivers (button.isOn, formData.getValue) emit nothing. |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3055 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3055 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-29` | 3055 | `internal/extractors/javascript/testdata/substrate_import_resolution/app.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/config.ts`<br>`internal/extractors/javascript/testdata/substrate_import_resolution/nest_app.ts`<br>`internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go`<br>`internal/substrate/substrate.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3055 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3055 | `internal/substrate/effect_sinks_jsts.go`<br>`internal/substrate/effects_test.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3055 | `internal/links/pure_function_pass.go`<br>`internal/substrate/effect_sinks_jsts.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3055 | `internal/links/reachability.go`<br>`internal/links/reachability_test.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Request shape extraction | ✅ `full` | `2026-05-29` | 3055 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Response shape extraction | ✅ `full` | `2026-05-29` | 3055 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Sanitizer recognition | 🟢 `partial` | `2026-05-29` | 3186 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_jsts_metafw_test.go` | Framework-blind: parameterised SQL, DOMPurify.sanitize/validator.escape/lodash.escape/he.encode, and zod/joi/yup schema declarations (hard rule per #2772). |
| Schema drift detection | ✅ `full` | `2026-05-29` | 3055 | `internal/links/payload_drift.go`<br>`internal/mcp/payload_drift_tool.go`<br>`internal/substrate/payload_shapes.go`<br>`internal/substrate/payload_shapes_jsts.go` | — |
| Taint sink detection | 🟢 `partial` | `2026-05-29` | 3186 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_jsts_metafw_test.go` | Framework-blind via jstsSink* regexes: SQL injection (db.query non-literal), command injection (exec/eval/new Function), path traversal (fs.* non-literal), XSS (res.send/innerHTML), ReDoS. Not Remix-specific. |
| Taint source detection | 🟢 `partial` | `2026-05-29` | 3186 | `internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_jsts_metafw_test.go` | Framework-blind: Remix loader/action route params via jstsSourceLoaderParamsRe (params.*), and request.formData()/json()/text() via jstsSourceMetaFrameworkRe (#3186). No resolved data-flow binding, so partial. |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3055 | `internal/substrate/template_pattern_jsts.go`<br>`internal/substrate/template_pattern_test.go` | — |
| Vulnerability finding | 🟢 `partial` | `2026-05-29` | 3186 | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_jsts_metafw_test.go` | taint_flow.go propagates Remix source (params.*, request.formData/json) to SQL/command/path/XSS sinks. Heuristic regex chain, no precise data-flow resolution → partial. |

## Framework-specific

### Remix Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Remix loader action pair | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/grafel/issues/2878) | `internal/custom/javascript/issue2878_metafw_idioms_test.go`<br>`internal/custom/javascript/remix.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.remix ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
