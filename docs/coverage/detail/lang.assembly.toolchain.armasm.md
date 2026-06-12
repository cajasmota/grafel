<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.assembly.toolchain.armasm` — ARM armasm

Auto-generated. Back to [summary](../summary.md).

- **Language:** [assembly](../by-language/assembly.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 2744 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | ARM/AArch64 bl/blx/blr calls and the b/bXX/cbz/tbz branch family are classified with line precision (callMnemonics/branchMnemonics + edge_kind), shared with the gnu-as path; svc/swi syscall gates emit the __syscall effect. |
| Core extraction | 🟢 `partial` | `2026-06-12` | 4950 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | PARTIAL (deepened #4950) — armasm structured procedure + section framing is now parsed alongside the gas/colon-label path: `name PROC`/ENDP and `name FUNCTION`/ENDFUNC open a SCOPE.Operation(subtype=procedure, framing=proc) even with no trailing colon (procStartRE/procEndRE in buildProcedureEntities; ENDP/ENDFUNC closes the span and clears the current procedure), and `AREA <name>,CODE|DATA` is recognised as a SCOPE.Component(subtype=section) (areaRE in sectionName). EXPORT is collected as an exported symbol (publicRE -> collectExported) so EXPORTed procs carry exported=true. Proven by TestExtractARMArmasmStructured (arm.armasm.fixture). STILL DEFERRED (follow-up): the column-1 data-definition forms `LABEL EQU value` / `LABEL DCD ...` are not yet modelled as constants/data (gas .equ/NASM/MASM EQU equates are), and the bar-delimited `|name|` AREA/label form is unhandled because `|` is scrubbed as an m68k line comment. |
| Import resolution quality | ✅ `full` | `2026-06-12` | 4950 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | FULL (deepened #4950) — armasm cross-unit linkage is now surfaced: `EXPORT sym` -> exported symbol (publicRE), `IMPORT sym` -> external symbol with CALLS locality=external (masmExternRE -> collectExternal), and `GET file` / `INCLUDE file` -> IMPORTS edge with source_module/imported_name/local_name (masmIncludeRE -> buildIncludeEntities). Proven by TestExtractARMArmasmStructured (GET macros.inc + INCLUDE defs.inc IMPORTS edges, IMPORT printf external call locality). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.assembly.toolchain.armasm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
