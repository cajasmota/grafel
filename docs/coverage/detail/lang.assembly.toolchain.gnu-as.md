<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.assembly.toolchain.gnu-as` — GNU as (gas)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [assembly](../by-language/assembly.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 2744 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | Every CALLS edge carries Properties["line"] (1-based) and is classified (#2836): callMnemonics (call/callq, bl/blx/blr, jal/jalr, jsr/bsr) emit edge_kind=call; branchMnemonics (jmp/jXX x86, b/bXX/cbz AArch64, bra/Bcc/dbra m68k, j/beqz/bnez RISC-V) emit edge_kind=branch (buildProcedureEntities 493-653). A self-targeting call/branch carries recursion=self; an unconditional branch (jmp/bra/b/j) to another procedure carries tail_call=true. Targets named via .extern/.globl carry locality=external (collectExternal/collectExported 295-312). Register/memory-indirect operands (*%rax, [rax], (a0)) yield NO edge — callTarget returns "" rather than a fabricated target (cleanTargetToken 827-864). Intra-file branches to local labels (.L*/numeric) are rewritten to a file-scoped structural-ref stub (localLabelStub 676-678) so the resolver binds them to the in-file SCOPE.CodeBlock anchor, not a same-named label in another file. Proven by TestExtractX8664Gas / TestResolverBindsBranchAndCrossFile. |
| Core extraction | ✅ `full` | `2026-06-12` | 2744 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | Line-oriented (mnemonic) hand parser, no tree-sitter grammar (cf. verilog/vhdl/cobol precedent). Emits: global/exported labels → SCOPE.Operation(subtype=procedure) with a body spanning to the next global label (buildProcedureEntities); local labels (.L*, numeric) → SCOPE.CodeBlock(subtype=label) branch anchors (#2836); sections (.text/.data/.bss/.rodata + .section <name>) → SCOPE.Component(subtype=section) (buildSectionEntities 377-422); equates → SCOPE.Constant(subtype=equate) across gas .equ/.set, NASM %define, MASM EQU, and NAME=val (buildConstantEntities 428-462). syscall/OS-boundary EFFECT (#2744 Phase-1A): syscallMnemonics {syscall,sysenter,svc,swi,ecall} plus isSyscall() (int 0x80/80h, m68k trap #0) emit a synthetic CALLS→__syscall with effect=syscall locality=external and stamp has_syscall + syscall_count on the enclosing procedure (85-192,573-593,923-945). Dialect/syntax are recorded as file-entity attributes (detectDialect: x86/x86-64/arm/arm64/riscv/m68k + att/intel) never as separate languages (#2821 taxonomy). Note unregistered-Kind history #2839 — SCOPE.Constant/CodeBlock Kinds must stay registered in internal/types. Proven by TestExtractX8664Gas / TestExtractARM64 / TestExtractM68k / TestExtractRISCV. |
| Import resolution quality | ✅ `full` | `2026-06-12` | 2744 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | gas .include "file" and NASM %include "file" → IMPORTS edge with source_module/imported_name/local_name props (buildIncludeEntities 336-368, includeRE). The ToID is the literal include path (display name is the basename); the resolver binds it on-disk where the path matches. Proven by the cross-file fixtures (xref_main.s / xref_lib.s, TestCrossFileResolution). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.assembly.toolchain.gnu-as ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
