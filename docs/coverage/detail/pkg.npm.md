<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `pkg.npm` — package.json (npm/yarn/pnpm)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JS/TS](../by-language/jsts.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `lockfile_parsing` | ✅ `full` | — | — | [link](2865) | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/extractor_test.go` | — |
| `manifest_parsing` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/cross/manifest/extractor.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update pkg.npm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
