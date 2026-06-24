<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.astro` — Astro

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 40

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/grafel/issues/2857) | `internal/extractors/astro/extractor.go`<br>`internal/extractors/astro/issue2857_routing_test.go` | — |
| Hook recognition | — `not_applicable` | — | — | — | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Data loaders | ✅ `full` | — | [link](https://github.com/cajasmota/grafel/issues/2858) | `internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/extractors/astro/extractor.go`<br>`internal/extractors/astro/issue2858_server_test.go` | — |

### Server

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Hydration boundaries | ✅ `full` | — | [link](https://github.com/cajasmota/grafel/issues/2858) | `internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/extractors/astro/extractor.go`<br>`internal/extractors/astro/issue2858_server_test.go` | — |
| Server components | ✅ `full` | — | [link](https://github.com/cajasmota/grafel/issues/2858) | `internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/extractors/astro/extractor.go`<br>`internal/extractors/astro/issue2858_server_test.go` | — |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Route extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/frameworks/astro.yaml` | — |
| Router pattern | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/grafel/issues/2857) | `internal/extractors/astro/extractor.go`<br>`internal/extractors/astro/issue2857_routing_test.go` | — |

### Auth

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Auth coverage | 🔴 `missing` | — | 5499 | — | #5499: meta_framework Auth lane added. This framework is predominantly static/build-time and carries no server-side auth-check idiom this pass recognises; honest missing pending a framework-specific auth surface. |

### Build

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Static generation | ✅ `full` | — | [link](https://github.com/cajasmota/grafel/issues/2858) | `internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/extractors/astro/extractor.go`<br>`internal/extractors/astro/issue2858_server_test.go` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | — `not_applicable` | — | [link](https://github.com/cajasmota/grafel/issues/2858) | `internal/extractors/astro/extractor.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/markup_script.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/javascript/config_consumer.go`<br>`internal/extractors/javascript/config_consumer_test.go` | process.env.X, import.meta.env.X, config.get(k) -> config:<key> DEPENDS_ON_CONFIG (issue #3641) |
| Constant propagation | 🟢 `partial` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/markup_script.go` | — |
| DB effect | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_markup_script.go` | — |
| Dead code detection | 🟢 `partial` | — | 3057 | `internal/extractors/astro/extractor.go` | framework-blind dead code detection applies to Astro via substrate reachability analysis (#3183) |
| Def use chain extraction | 🟢 `partial` | `2026-05-29` | 3057 | `internal/substrate/def_use_jsts.go`<br>`internal/substrate/def_use_markup_script.go` | — |
| Env fallback recognition | 🟢 `partial` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/markup_script.go` | — |
| Error flow | ✅ `full` | `2026-06-02` | 3628 | `internal/extractor/exception_flow.go`<br>`internal/extractors/javascript/exception_flow.go`<br>`internal/extractors/javascript/exception_flow_test.go` | throw new X -> THROWS; e instanceof X catch-filter -> CATCHES; untyped throw/catch dropped (#3628) |
| Feature flag gating | 🟢 `partial` | `2026-06-03` | 3706 | `internal/engine/feature_flag_edges.go`<br>`internal/engine/feature_flag_edges_test.go`<br>`internal/engine/orm_queries.go` | flag-check call sites -> feature:<key> + GATED_BY (framework-agnostic JS/TS engine pass, fires regardless of framework). Verified to attribute to the enclosing function: LaunchDarkly ldClient.variation/boolVariation/stringVariation, Unleash unleash.isEnabled, OpenFeature client.getBooleanValue, Unleash-React useFlag, Split.io getTreatment, Flagsmith hasFeature, plus GrowthBook gb.isOn/isOff/getFeatureValue and ConfigCat configCatClient.getValue/getValueAsync (receiver-gated). Honest-partial: dynamic keys + non-flag receivers (button.isOn, formData.getValue) emit nothing. |
| Fs effect | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_markup_script.go` | — |
| HTTP effect | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_markup_script.go` | — |
| Import resolution quality | 🟢 `partial` | `2026-05-29` | 3057 | `internal/substrate/jsts.go`<br>`internal/substrate/markup_script.go` | — |
| Module cycle detection | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/module_cycle_pass.go` | — |
| Mutation effect | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_markup_script.go` | — |
| Pure function tagging | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/pure_function_pass.go` | — |
| Reachability analysis | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_jsts.go` | — |
| Request shape extraction | 🟢 `partial` | `2026-05-29` | 3057 | `internal/substrate/payload_shapes_t3.go` | — |
| Response shape extraction | 🟢 `partial` | `2026-05-29` | 3057 | `internal/substrate/payload_shapes_t3.go` | — |
| Sanitizer recognition | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_astro/UserPage.astro` | — |
| Schema drift detection | 🟢 `partial` | `2026-05-29` | 3057 | `internal/links/payload_drift.go` | — |
| Taint sink detection | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_astro/UserPage.astro` | — |
| Taint source detection | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_astro/UserPage.astro` | — |
| Template pattern catalog | 🟢 `partial` | `2026-05-29` | 3057 | `internal/substrate/template_pattern_markup_script.go` | — |
| Vulnerability finding | ✅ `full` | `2026-05-28` | — | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_jsts.go`<br>`internal/substrate/taint_sites_markup_script.go`<br>`internal/substrate/uimm_substrate_test.go`<br>`testdata/fixtures/typescript/substrate_astro/UserPage.astro` | — |

## Framework-specific

### Astro Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Astro frontmatter fetch | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/grafel/issues/2878) | `internal/extractors/astro/extractor.go`<br>`internal/extractors/astro/issue2878_idioms_test.go` | — |
| Astro island directive | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/grafel/issues/2878) | `internal/extractors/astro/extractor.go`<br>`internal/extractors/astro/issue2878_idioms_test.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.astro ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
