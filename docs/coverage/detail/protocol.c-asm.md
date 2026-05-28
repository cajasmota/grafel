<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `protocol.c-asm` — C ↔ asm ABI bridge

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [protocol](../by-category/protocol.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Cross repo linkage | ✅ `full` | `2026-05-28` | 2837 | `internal/extractors/cross/abibridge/extractor.go`<br>`internal/extractors/cross/abibridge/extractor_test.go` | — |
| Method attribution | ✅ `full` | `2026-05-28` | 2837 | `internal/extractors/cross/abibridge/extractor.go`<br>`internal/extractors/cross/abibridge/testdata/crypt.s.fixture` | — |
| Service extraction | ✅ `full` | `2026-05-28` | 2837 | `internal/extractors/cross/abibridge/extractor.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update protocol.c-asm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
