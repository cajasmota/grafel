<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.rescript.tool.rescript-json` — rescript.json / bsconfig.json (ReScript manifest)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ReScript](../by-language/rescript.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Lockfile parsing | — `not_applicable` | `2026-06-24` | — | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/rescript.go`<br>`internal/extractors/cross/manifest/rescript_test.go` | rescript.json / bsconfig.json is NOT a lockfile — the dependency lists are flat npm package NAMES with no resolved versions. ReScript packages are npm packages, so the resolved dependency tree is recovered by the existing npm/yarn/pnpm lockfile parsers over the sibling lockfile; there is no ReScript-specific lockfile format. |
| Manifest parsing | 🟢 `partial` | `2026-06-24` | 5378 | `internal/classifier/classifier.go`<br>`internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/rescript.go`<br>`internal/extractors/cross/manifest/rescript_test.go` | Parses rescript.json (v11+) / bsconfig.json (legacy, same schema): bs-dependencies (runtime), bs-dev-dependencies (dev), pinned-dependencies (runtime). The dependency lists are flat npm package-name arrays (versions resolve from the sibling package.json — ReScript packages ARE npm packages), so package_manager=npm and the JS-ecosystem package.json/lockfile coverage version-resolves them. JSX version/mode + module/suffix/namespace config surfaced on the project anchor as the rescript_config property. Partial: no per-dependency version (rescript.json carries none). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.rescript.tool.rescript-json ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
