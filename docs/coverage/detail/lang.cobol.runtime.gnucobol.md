<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.cobol.runtime.gnucobol` — GnuCOBOL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [COBOL](../by-language/cobol.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-05-28` | 2743 | `internal/extractors/cobol/extractor.go` | — |
| Core extraction | 🟢 `partial` | `2026-05-28` | 2743 | `internal/classifier/classifier.go`<br>`internal/extractors/cobol/extractor.go` | STATUS RETAGGED full -> partial (#6432). This cell asserted ✅/`full` with no notes at all — no mechanism, no proving test, no measurement — and the ✅/`full` tier is published to users as *fixture-proven, resolves the general case*. internal/quality/golden/ carries no fixture for this record and internal/quality/golden/baseline.json records no recall figures for it, so there is nothing in the tree that could have backed the claim. #6424 established what that is worth: solidity carried core_extraction=full on hand-asserted evidence and its first graded fixture scored 19/40 entities. Retag to full only when a graded fixture exists for this record and its measured recall is recorded in baseline.json. |
| Import resolution quality | ✅ `full` | `2026-05-28` | 2838 | `internal/extractors/cobol/depth.go`<br>`internal/extractors/cobol/extractor.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.cobol.runtime.gnucobol ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
