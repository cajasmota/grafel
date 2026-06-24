<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `pkg.shards` — shards (Crystal)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [crystal](../by-language/crystal.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Lockfile parsing | ✅ `full` | `2026-06-24` | 5366 | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/extractor_test.go` | shard.lock lockfile: parseShardLock reads the resolved shards: map (exact versions, kind=locked); the top-level lockfile-format version: scalar is excluded. |
| Manifest parsing | ✅ `full` | `2026-06-24` | 5366 | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/extractor_test.go` | shard.yml manifest: parseShardYml reads dependencies: (runtime) + development_dependencies: (dev) maps, each shard's nested version: line; project-metadata scalars (name/version/crystal/license) excluded. Emits DEPENDS_ON + DEPENDS_ON_PACKAGE under package_manager=shards. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update pkg.shards ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
