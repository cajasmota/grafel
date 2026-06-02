<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.electron` — Electron

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Desktop
- **Capability cells:** 15

## Capabilities


### Process

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| IPC extraction | ✅ `full` | — | 2865 | `internal/engine/electron_detect_test.go`<br>`internal/engine/rules/javascript_typescript/frameworks/electron.yaml`<br>`testdata/fixtures/typescript/electron_ipc.ts`<br>`testdata/fixtures/typescript/electron_preload.ts` | — |
| Main renderer split | ✅ `full` | `2026-05-28` | 2865 | `internal/engine/electron_detect_test.go`<br>`internal/engine/rules/javascript_typescript/frameworks/electron.yaml`<br>`testdata/fixtures/typescript/electron_ipc.ts`<br>`testdata/fixtures/typescript/electron_preload.ts` | — |

### Native

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | ✅ `full` | — | 2865 | `internal/engine/electron_detect_test.go`<br>`internal/engine/rules/javascript_typescript/frameworks/electron.yaml`<br>`testdata/fixtures/typescript/electron_ipc.ts`<br>`testdata/fixtures/typescript/electron_preload.ts` | — |

### Updates

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🟢 `partial` | — | 3059 | `internal/links/effect_propagation.go`<br>`internal/substrate/jsts.go` | — |
| Config consumption | ✅ `full` | `2026-06-02` | 3641 | `internal/extractor/config_key.go`<br>`internal/extractors/javascript/config_consumer.go`<br>`internal/extractors/javascript/config_consumer_test.go` | process.env.X, import.meta.env.X, config.get(k) -> config:<key> DEPENDS_ON_CONFIG (issue #3641) |
| Constant propagation | 🟢 `partial` | — | 3059 | `internal/links/constant_propagation.go`<br>`internal/substrate/jsts.go` | — |
| DB effect | 🟢 `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | Electron main-process runs full Node.js; ORM/DB libraries like Sequelize/TypeORM/Prisma apply |
| Dead code detection | 🟢 `partial` | — | 3059 | `internal/patterns/dead_module_detector.go` | — |
| Env fallback recognition | 🟢 `partial` | — | 3059 | `internal/substrate/jsts.go` | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | — |
| HTTP effect | 🟢 `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | — |
| Import resolution quality | 🟢 `partial` | — | 3059 | `internal/substrate/jsts.go` | — |
| Mutation effect | 🟢 `partial` | — | 3059 | `internal/substrate/effect_sinks_jsts.go` | — |
| Reachability analysis | 🟢 `partial` | — | 3059 | `internal/links/reachability.go`<br>`internal/substrate/entry_points_jsts.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.electron ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
