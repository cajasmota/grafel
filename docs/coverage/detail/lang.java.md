<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java` — Java

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites |
|------------|--------|-------------|--------------|-------|-------|
| `call_line_precision` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/java/java.go` |
| `core_extraction` | ✅ `full` | `2026-05-28` | — | — | `internal/extractors/java/java.go` |
| `discriminates_on` | ❌ `missing` | — | — | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
