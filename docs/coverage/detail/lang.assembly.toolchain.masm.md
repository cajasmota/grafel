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
| Core extraction | ✅ `full` | `2026-06-14` | 5055 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | FULL (deepened #4950 + #5055) — MASM structured constructs are modelled. `name PROC`/`name ENDP` framing opens a SCOPE.Operation(subtype=procedure, framing=proc) and ENDP closes the span and clears the current procedure (procStartRE/procEndRE in buildProcedureEntities) so body calls attribute correctly; PUBLIC symbols are exported (publicRE -> collectExported). #5055 adds the record/segment constructs: `name STRUCT`/`name STRUC` -> SCOPE.Component(subtype=struct) and `name SEGMENT` -> SCOPE.Component(subtype=section), both bounded by the matching `name ENDS` terminator (masmStructRE/masmSegmentRE/masmEndsRE -> buildMasmBlockEntities, framing=masm), paralleling the struct/record types extracted for high-level langs (cpp/csharp SCOPE.Component subtype=struct; erlang subtype=record) and the .text/.data sections. Proven by TestExtractMASMStructured (POINT STRUCT -> struct component bounded by ENDS, _DATA SEGMENT -> section component) plus the wrong-dialect/no-match no-op guards (TestMasmBlocksWrongDialectNoOp, TestMasmBlocksNoMatchNoOp). |
| Import resolution quality | ✅ `full` | `2026-06-12` | 4950 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | FULL (deepened #4950) — MASM cross-unit linkage is now surfaced: `INCLUDE file` and `INCLUDELIB lib` -> IMPORTS edge (masmIncludeRE -> buildIncludeEntities), `EXTERN name:type` / `EXTRN name:type` -> external symbol with CALLS locality=external (masmExternRE -> collectExternal, the optional :PROC/:DWORD type stripped by splitMasmSymbolList), and `PUBLIC name` -> exported symbol (publicRE). Proven by TestExtractMASMStructured (INCLUDE windows.inc + INCLUDELIB kernel32.lib IMPORTS; EXTERN printf:PROC/ExitProcess:PROC external call locality). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.assembly.toolchain.masm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
