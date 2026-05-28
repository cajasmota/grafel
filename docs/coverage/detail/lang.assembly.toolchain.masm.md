<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.assembly.toolchain.masm` — MASM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [assembly](../by-language/assembly.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `call_line_precision` | ✅ `full` | `2026-05-28` | — | [link](2744) | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | — |
| `core_extraction` | ⚠️ `partial` | `2026-05-28` | — | [link](2744) | `internal/extractors/assembly/extractor.go` | — |
| `cross_language_link` | ❌ `missing` | — | — | [link](2837) | — | — |
| `import_resolution_quality` | ⚠️ `partial` | `2026-05-28` | — | [link](2744) | `internal/extractors/assembly/extractor.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.assembly.toolchain.masm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
