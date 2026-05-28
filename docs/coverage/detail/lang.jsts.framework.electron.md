<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jsts.framework.electron` — Electron

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Desktop
- **Capability cells:** 3

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

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jsts.framework.electron ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
