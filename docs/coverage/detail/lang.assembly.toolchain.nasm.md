<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.assembly.toolchain.nasm` — NASM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [assembly](../by-language/assembly.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-05-28` | 2744 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | — |
| Core extraction | 🟢 `partial` | `2026-05-28` | 2744 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | STATUS RETAGGED full -> partial (#6432). This cell asserted ✅/`full` with no notes at all — no mechanism, no proving test, no measurement — and the ✅/`full` tier is published to users as *fixture-proven, resolves the general case*. internal/quality/golden/ carries no fixture for this record and internal/quality/golden/baseline.json records no recall figures for it, so there is nothing in the tree that could have backed the claim. #6424 established what that is worth: solidity carried core_extraction=full on hand-asserted evidence and its first graded fixture scored 19/40 entities. Retag to full only when a graded fixture exists for this record and its measured recall is recorded in baseline.json. |
| Import resolution quality | ✅ `full` | `2026-05-28` | 2744 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.assembly.toolchain.nasm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
