<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.python` — Python

Auto-generated. Back to [summary](../summary.md).

- **Language:** [python](../by-language/python.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `call_line_precision` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/python/extractor.go` |
| `core_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/python/extractor.go` |
| `discriminates_on` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/python/discriminator.go` |
| `navigates_to` | ❌ `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.python ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
