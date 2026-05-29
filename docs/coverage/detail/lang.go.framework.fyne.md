<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go.framework.fyne` — Fyne (desktop GUI)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** Desktop
- **Capability cells:** 13

## Capabilities


### Process

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| IPC extraction | — `not_applicable` | — | — | — | — |
| Main renderer split | — `not_applicable` | — | — | — | — |

### Native

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Native module imports | 🟢 `partial` | `2026-05-30` | [link](https://github.com/cajasmota/archigraph/issues/3218) | `internal/custom/golang/fyne.go`<br>`internal/custom/golang/fyne_gomobile_test.go` | — |

### Updates

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Constant propagation | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| DB effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Dead code detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Env fallback recognition | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Fs effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| HTTP effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Import resolution quality | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Mutation effect | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Reachability analysis | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go.framework.fyne ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
