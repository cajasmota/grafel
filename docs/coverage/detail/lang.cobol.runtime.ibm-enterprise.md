<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.cobol.runtime.ibm-enterprise` — IBM Enterprise COBOL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [COBOL](../by-language/cobol.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 6

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `call_line_precision` | ✅ `full` | `2026-05-28` | — | [link](2743) | `internal/extractors/cobol/extractor.go` | — |
| `core_extraction` | ✅ `full` | `2026-05-28` | — | [link](2743) | `internal/classifier/classifier.go`<br>`internal/extractors/cobol/extractor.go` | — |
| `db_effect` | ✅ `full` | `2026-05-28` | — | [link](2838) | `internal/substrate/effect_sinks_cobol.go`<br>`internal/extractors/cobol/depth.go` | — |
| `fs_effect` | ✅ `full` | `2026-05-28` | — | [link](2838) | `internal/substrate/effect_sinks_cobol.go` | — |
| `http_effect` | ✅ `full` | `2026-05-28` | — | [link](2838) | `internal/substrate/effect_sinks_cobol.go`<br>`internal/extractors/cobol/depth.go` | — |
| `import_resolution_quality` | ✅ `full` | `2026-05-28` | — | [link](2838) | `internal/extractors/cobol/extractor.go`<br>`internal/extractors/cobol/depth.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.cobol.runtime.ibm-enterprise ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
