<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.go` — Go

Auto-generated. Back to [summary](../summary.md).

- **Language:** [go](../by-language/go.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `call_line_precision` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/golang/extractor.go` |
| `core_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/golang/extractor.go` |
| `discriminates_on` | ❌ `missing` | — | — | — | — |
| `navigates_to` | ❌ `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.go ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
