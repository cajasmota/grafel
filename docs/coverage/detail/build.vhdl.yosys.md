<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `build.vhdl.yosys` — Yosys

Auto-generated. Back to [summary](../summary.md).

- **Language:** [VHDL](../by-language/vhdl.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-06-24` | 5381 | `internal/extractors/vhdl/extractor.go`<br>`internal/extractors/vhdl/extractor_test.go` | Yosys detected from in-HDL synth attributes — attribute top / blackbox / whitebox / gentb_clock / nomem2reg (VHDL-2008 attribute mechanism / GHDL-yosys plugin) (#5381 yosysAttrRE); emits SCOPE.Component(subtype=tool, tool=yosys) + file->tool USES edge. Partial: in-HDL attribute signal detection only, NOT .ys script / synth_* command parsing. Proven by TestVHDL_YosysAttr / _NoToolFalsePositive. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update build.vhdl.yosys ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
