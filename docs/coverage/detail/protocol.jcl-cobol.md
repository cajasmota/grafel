<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `protocol.jcl-cobol` — JCL → COBOL job bridge

Auto-generated. Back to [summary](../summary.md).

- **Language:** [multi](../by-language/multi.md)
- **Category:** [protocol](../by-category/protocol.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Cross repo linkage | ✅ `full` | `2026-05-28` | 2843 | `internal/extractors/cobol/testdata/payroll.cbl`<br>`internal/extractors/jcl/extractor.go`<br>`internal/extractors/jcl/extractor_test.go` | — |
| Method attribution | ✅ `full` | `2026-05-28` | 2843 | `internal/extractors/jcl/extractor.go`<br>`internal/extractors/jcl/testdata/payjob.jcl` | — |
| Service extraction | ✅ `full` | `2026-05-28` | 2843 | `internal/extractors/jcl/extractor.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update protocol.jcl-cobol ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
