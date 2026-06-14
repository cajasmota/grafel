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
| Core extraction | 🟢 `partial` | `2026-06-14` | 5056 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go`<br>`internal/extractors/assembly/testdata/arm.armasm.fixture` | PARTIAL (deepened #4950) — armasm structured procedure + section framing is now parsed alongside the gas/colon-label path: `name PROC`/ENDP and `name FUNCTION`/ENDFUNC open a SCOPE.Operation(subtype=procedure, framing=proc) even with no trailing colon (procStartRE/procEndRE in buildProcedureEntities; ENDP/ENDFUNC closes the span and clears the current procedure), and `AREA <name>,CODE|DATA` is recognised as a SCOPE.Component(subtype=section) (areaRE in sectionName). EXPORT is collected as an exported symbol (publicRE -> collectExported) so EXPORTed procs carry exported=true. #5056 closed the column-1 data forms and bar-delimited symbols: `LABEL EQU value` (column-1 equate, incl. `|bar.sym| EQU ...`) becomes SCOPE.Constant(subtype=equate) via equMasmRE; `LABEL DCB/DCW/DCD/DCQ/DCFS/DCFD ...` (column-1 Define-Constant data, incl. `|tbl.entry| DCD ...`) becomes SCOPE.Variable(subtype=data) carrying Properties data_form=dcx/data_width/initialiser via dcdRE+buildDataEntities; and the bar-delimited `|name|` form now survives scrubComments (barDelimitedSymbol guards the m68k `|` line-comment rule so `|My.Sym|` is kept while a trailing `| comment` and an in-operand `#(A|B)` bitwise-OR are unaffected). Proven by TestExtractARMArmasmStructured + TestArmasmColumn1NoOp (arm.armasm.fixture). STILL DEFERRED: macro framing (MACRO/MEND) and the RN/SETA/SETL register/variable-alias directives. |
| Import resolution quality | ✅ `full` | `2026-06-12` | 4950 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | FULL (deepened #4950) — armasm cross-unit linkage is now surfaced: `EXPORT sym` -> exported symbol (publicRE), `IMPORT sym` -> external symbol with CALLS locality=external (masmExternRE -> collectExternal), and `GET file` / `INCLUDE file` -> IMPORTS edge with source_module/imported_name/local_name (masmIncludeRE -> buildIncludeEntities). Proven by TestExtractARMArmasmStructured (GET macros.inc + INCLUDE defs.inc IMPORTS edges, IMPORT printf external call locality). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.assembly.toolchain.armasm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
