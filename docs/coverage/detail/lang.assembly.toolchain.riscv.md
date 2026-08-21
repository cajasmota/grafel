<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.assembly.toolchain.riscv` — RISC-V as (gas)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [assembly](../by-language/assembly.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 4909 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | RISC-V control flow is classified with line precision (#4909): jal/jalr → edge_kind=call (callMnemonics 146-151; the destination label is taken as the last operand so `jal ra, target` resolves to target, ra being a register is skipped). The RISC-V branch family is recognised in branchMnemonics — `j` (unconditional jump pseudo, treated as a tail_call branch), beqz/bnez/blez/bgez/bltz/bgtz (compare-vs-x0 pseudos) and bltu/bgeu/bgtu/bleu (unsigned compares); beq/bne/blt/bge overlap the ARM/m68k spellings already present. self-recursion and intra-file local-label resolution (#2836) apply unchanged. Proven by TestExtractRISCV (riscv.s.fixture). |
| Core extraction | 🟢 `partial` | `2026-06-12` | 4909 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | RISC-V is the same gas line parser as gnu-as: global labels → procedures, .L*/numeric → SCOPE.CodeBlock anchors, .text/.data/.section → sections, .equ/.set → SCOPE.Constant(equate). detectDialect (#4909) classifies the RISC-V dialect ahead of arm64 (both use x-registers) via the ecall gate, the .option directive, the jal/jalr+ra idiom, or the RISC-V ABI register file ra/sp/gp/tp/a0-a7/t0-t6/s0-s11 as an instruction operand; the file entity is stamped dialect=riscv. The ecall environment-call is the RISC-V OS/EE boundary gate (syscallMnemonics, #4909) and emits the synthetic CALLS→__syscall effect with has_syscall/syscall_count; ebreak is deliberately excluded (debugger trap, not an OS syscall). Proven by TestExtractRISCV asserting dialect, jal call, ecall syscall, beqz/bnez branches, the .equ constant and the .text section. STATUS RETAGGED full -> partial (#6432). The published meaning of the ✅/`full` tier is *fixture-proven, resolves the general case* — that is the legend printed on every by-language page — and nothing in this tree measures this extractor's recall: internal/quality/golden/ carries no fixture for it, and internal/quality/golden/baseline.json records no entity_found/entity_expected figures for it. The unit tests cited above prove the shapes they name; they do not measure recall over real sources. #6424 established what that difference is worth: solidity carried core_extraction=full on exactly this class of evidence, and the first graded fixture written for it scored 19/40 entities, because a contract declared with base-constructor arguments silently dropped its entire file — every function, event, modifier and state variable in it. This extractor is line/regex-oriented rather than AST-resolved, which is `partial`'s own published meaning, so the tier is wrong on both halves of its definition. Retag to full only when a graded fixture exists for this extractor and its measured recall is recorded in baseline.json, the way lang.solidity.core now is. |
| Import resolution quality | ✅ `full` | `2026-06-12` | 4909 | `internal/extractors/assembly/extractor.go`<br>`internal/extractors/assembly/extractor_test.go` | RISC-V gas uses the same .include "file" directive as the other gas dialects → IMPORTS edge (buildIncludeEntities, shared path), and .extern symbols carry locality=external. Proven within the shared gnu-as include path; RISC-V adds no dialect-specific include form. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.assembly.toolchain.riscv ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
