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
| Core extraction | 🟢 `partial` | `2026-06-12` | 4950 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | PARTIAL (deepened #4950) — MASM `name PROC`/`name ENDP` procedure framing is now modelled: a PROC with no trailing colon opens a SCOPE.Operation(subtype=procedure, framing=proc) and ENDP closes the span and clears the current procedure (procStartRE/procEndRE in buildProcedureEntities), so the body's calls attribute correctly. PUBLIC symbols are collected as exported (publicRE -> collectExported). Proven by TestExtractMASMStructured (x86_64.masm.fixture). STILL DEFERRED (follow-up): STRUCT/ENDS record types and SEGMENT/ENDS segment directives are not yet modelled as component/struct entities (only PROC framing and the .code/.data shorthands are recognised). |
| Import resolution quality | ✅ `full` | `2026-06-12` | 4950 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | FULL (deepened #4950) — MASM cross-unit linkage is now surfaced: `INCLUDE file` and `INCLUDELIB lib` -> IMPORTS edge (masmIncludeRE -> buildIncludeEntities), `EXTERN name:type` / `EXTRN name:type` -> external symbol with CALLS locality=external (masmExternRE -> collectExternal, the optional :PROC/:DWORD type stripped by splitMasmSymbolList), and `PUBLIC name` -> exported symbol (publicRE). Proven by TestExtractMASMStructured (INCLUDE windows.inc + INCLUDELIB kernel32.lib IMPORTS; EXTERN printf:PROC/ExitProcess:PROC external call locality). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.assembly.toolchain.masm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
