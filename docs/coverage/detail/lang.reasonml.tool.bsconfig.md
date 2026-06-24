<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.reasonml.tool.bsconfig` — bsconfig.json (BuckleScript/Reason manifest)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ReasonML](../by-language/reasonml.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Lockfile parsing | — `not_applicable` | — | — | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/rescript.go`<br>`internal/extractors/cross/manifest/rescript_test.go` | bsconfig.json is NOT a lockfile — the dependency lists are flat npm package NAMES with no resolved versions. Reason/BuckleScript packages are npm packages, so the resolved dependency tree is recovered by the existing npm/yarn/pnpm lockfile parsers over the sibling lockfile; there is no Reason-specific lockfile format. |
| Manifest parsing | 🟢 `partial` | `2026-06-24` | 5379 | `internal/classifier/classifier.go`<br>`internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/rescript.go`<br>`internal/extractors/cross/manifest/rescript_test.go` | bsconfig.json is the BuckleScript/Reason manifest — identical schema to ReScript's rescript.json, parsed by the SHARED language-agnostic parser added in #5378 (no duplication). bs-dependencies (runtime), bs-dev-dependencies (dev), pinned-dependencies (runtime) are flat npm package-name arrays (versions resolve from the sibling package.json — Reason/BuckleScript packages ARE npm packages), so package_manager=npm and the JS-ecosystem package.json/lockfile coverage version-resolves them. JSX version/mode + module/suffix/namespace config surfaced on the project anchor as the rescript_config property. Partial: no per-dependency version (bsconfig carries none). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.reasonml.tool.bsconfig ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
