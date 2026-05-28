<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.cobol.embedded.cics` — COBOL CICS

Auto-generated. Back to [summary](../summary.md).

- **Language:** [COBOL](../by-language/cobol.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Verified SHA | Issue | Cites | Notes |
|------------|--------|-------------|--------------|-------|-------|-------|
| `fs_effect` | ✅ `full` | `2026-05-28` | — | [link](2838) | `internal/substrate/effect_sinks_cobol.go` | — |
| `http_effect` | ✅ `full` | `2026-05-28` | — | [link](2838) | `internal/substrate/effect_sinks_cobol.go`<br>`internal/extractors/cobol/depth.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.cobol.embedded.cics ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
