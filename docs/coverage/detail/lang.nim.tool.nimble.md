<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.nim.tool.nimble` — nimble (Nim package manager)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [nim](../by-language/nim.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Manifest parsing | ✅ `full` | `2026-06-24` | 5367 | `internal/extractors/cross/manifest/extractor.go`<br>`internal/extractors/cross/manifest/nimble.go`<br>`internal/extractors/cross/manifest/nimble_test.go` | A *.nimble manifest is NimScript; parseNimble mines the requires directives (statement form 'requires "a", "b"' and call form requires("a")) plus per-task taskRequires (flagged is_dev=true), splitting each constraint string into package name + version constraint. The nim compiler floor ('requires "nim >= …"') is kept as a real edge. .nimble is classified nim and suffix-dispatched in IsManifest/detectPackageManager/dispatchParser to package_manager nimble; emits DEPENDS_ON + DEPENDS_ON_PACKAGE edges + SBOM package nodes like every other ecosystem. No nimble lockfile format in wide use, so lockfile_parsing is N/A. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.nim.tool.nimble ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
