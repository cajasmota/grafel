<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.assembly.toolchain.gnu-as` — GNU as (gas)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [assembly](../by-language/assembly.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `call_line_precision` | ✅ `full` | `2026-05-28` | — | [link](2744) | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | — |
| `core_extraction` | ✅ `full` | `2026-05-28` | — | [link](2744) | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | — |
| `cross_language_link` | ✅ `full` | `2026-05-28` | — | [link](2837) | `internal/extractors/cross/abibridge/extractor.go`<br>`internal/extractors/cross/abibridge/extractor_test.go` | — |
| `import_resolution_quality` | ✅ `full` | `2026-05-28` | — | [link](2744) | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.assembly.toolchain.gnu-as ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
