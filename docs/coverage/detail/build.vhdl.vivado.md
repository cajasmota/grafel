<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `build.vhdl.vivado` — Vivado

Auto-generated. Back to [summary](../summary.md).

- **Language:** [VHDL](../by-language/vhdl.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-06-24` | 5381 | `internal/extractors/vhdl/extractor.go`<br>`internal/extractors/vhdl/extractor_test.go` | Vivado detected from in-HDL Xilinx synthesis attributes — attribute keep / dont_touch / mark_debug / ram_style / async_reg / max_fanout / use_dsp / fsm_encoding / syn_keep / syn_preserve (#5381 vivadoAttrRE); emits SCOPE.Component(subtype=tool, tool=vivado) + file->tool USES edge. Partial: in-HDL attribute signal detection only, NOT .xpr/.tcl project parsing. Proven by TestVHDL_VivadoAttrs / _NoToolFalsePositive. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update build.vhdl.vivado ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
