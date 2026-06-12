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
| Core extraction | 🟢 `partial` | `2026-06-12` | 2744 | `internal/extractors/assembly/extractor.go` | PARTIAL — colon-labels, calls/branches and EQU constants are extracted, but ARM (legacy armasm/armclang) structured directives are NOT parsed (follow-up #4950): AREA <name>,CODE|DATA section framing (only .text/.data/.section are recognised by sectionRE/sectionShorthands), `name PROC`/ENDP and FUNCTION/ENDFUNC procedure framing, and the `LABEL EQU value` / `LABEL DCD ...` column-1 forms. globlRE/externRE match only the gas .globl/.extern spellings, so armasm EXPORT/IMPORT symbol directives are not collected. |
| Import resolution quality | 🟢 `partial` | `2026-06-12` | 2744 | `internal/extractors/assembly/extractor.go` | PARTIAL — armasm declares cross-unit linkage with EXPORT sym / IMPORT sym and pulls files with GET file / INCLUDE file; none of these are matched by globlRE/externRE/includeRE (gas/NASM spellings only), so external symbols and includes produce no edge (follow-up #4950). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.assembly.toolchain.armasm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
