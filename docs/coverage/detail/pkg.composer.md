<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `pkg.composer` — composer.json

Auto-generated. Back to [summary](../summary.md).

- **Language:** [php](../by-language/php.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Lockfile parsing | ✅ `full` | — | — | `internal/extractors/cross/manifest/extractor.go` | composer.lock parsed via parseComposerLock: packages/packages-dev arrays emitting locked deps with resolved versions |
| Manifest parsing | ✅ `full` | — | — | `internal/extractors/cross/manifest/extractor.go` | composer.json parsed via parseComposerJSON: require/require-dev maps emitting runtime and dev deps with version constraints |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update pkg.composer ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
