<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `pkg.pipfile` — Pipfile / Pipfile.lock

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Lockfile parsing | 🟢 `partial` | `2026-05-29` | 3075 | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/pylock_test.go` | — |
| Manifest parsing | 🟢 `partial` | `2026-05-29` | 3075 | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/pylock_test.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update pkg.pipfile ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
