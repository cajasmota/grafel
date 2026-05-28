<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jcl.runtime.zos` — IBM z/OS JCL (JES2/JES3)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JCL](../by-language/jcl.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-05-28` | 2843 | `internal/extractors/jcl/extractor.go`<br>`internal/extractors/jcl/extractor_test.go` | — |
| Core extraction | ✅ `full` | `2026-05-28` | 2843 | `internal/extractors/jcl/extractor.go`<br>`internal/extractors/jcl/testdata/payjob.jcl` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jcl.runtime.zos ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
