<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.assembly.toolchain.masm` — MASM

Auto-generated. Back to [summary](../summary.md).

- **Language:** [assembly](../by-language/assembly.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 2744 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | Intel-syntax call/jmp target extraction is shared with NASM and dialect-agnostic (callTarget/cleanTargetToken 800-864 strip near/far/short/ptr/qword keywords and reject [mem]/register operands); MASM EQU constants are parsed (equMasmRE 132). Line-precise like the rest of the extractor. |
| Core extraction | 🟢 `partial` | `2026-06-12` | 2744 | `internal/extractors/assembly/extractor.go` | PARTIAL — labels, calls, EQU constants and the .data/.code section shorthands are extracted, but the MASM structured-procedure syntax is NOT modelled (follow-up #4950): `name PROC`/`name ENDP` procedure framing (only colon-labels start a procedure today, so a PROC with no trailing colon is missed), STRUCT/ENDS record types, and SEGMENT/ENDS segment directives. globlRE/externRE match only the leading-dot gas spellings, so MASM PUBLIC/EXTERN/EXTRN symbol directives are not collected. |
| Import resolution quality | 🟢 `partial` | `2026-06-12` | 2744 | `internal/extractors/assembly/extractor.go` | PARTIAL — MASM uses INCLUDE file (no leading dot/percent) and INCLUDELIB lib; includeRE matches only gas .include / NASM %include, so MASM includes produce no IMPORTS edge (follow-up #4950). PUBLIC/EXTERN external linkage is likewise not surfaced as import/locality. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.assembly.toolchain.masm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
