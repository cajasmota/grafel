<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.remix` — Remix

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Meta Framework
- **Capability cells:** 35

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2857) | `internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/react_shared.go`<br>`internal/custom/javascript/remix.go` | — |
| Hook recognition | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2857) | `internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/react_shared.go`<br>`internal/custom/javascript/remix.go` | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Data loaders | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/remix.go` | — |

### Server

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Hydration boundaries | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/metafw_server.go`<br>`internal/custom/javascript/remix.go` | — |
| Server components | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/metafw_server.go`<br>`internal/custom/javascript/remix.go` | — |

### Routing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Route extraction | ✅ `full` | `2026-05-28` | — | `internal/engine/rules/javascript_typescript/frameworks/remix.yaml` | — |
| Router pattern | ✅ `full` | `2026-05-28` | [link](https://github.com/cajasmota/archigraph/issues/2857) | `internal/custom/javascript/issue2857_meta_structure_test.go`<br>`internal/custom/javascript/remix.go` | — |

### Build

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Static generation | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_metafw_server_test.go`<br>`internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/custom/javascript/remix.go` | — |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Interface extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |
| Type alias extraction | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/extractor.go` | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | ✅ `full` | — | [link](https://github.com/cajasmota/archigraph/issues/2858) | `internal/custom/javascript/issue2858_realdata_test.go`<br>`internal/extractors/javascript/extractor.go`<br>`internal/extractors/javascript/issue2858_metafw_state_setter_test.go` | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-05-28` | — | `internal/extractors/javascript/tests.go` | — |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Def use chain extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Module cycle detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Pure function tagging | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Request shape extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Response shape extraction | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Schema drift detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Taint source detection | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Template pattern catalog | ❌ `missing` | — | backfill:dictionary-completeness | — | — |
| Vulnerability finding | ❌ `missing` | — | backfill:dictionary-completeness | — | — |

## Framework-specific

### Remix Internals

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Remix loader action pair | ✅ `full` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/2878) | `internal/custom/javascript/issue2878_metafw_idioms_test.go`<br>`internal/custom/javascript/remix.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.remix ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
