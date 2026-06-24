<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `build.vhdl.quartus` — Quartus

Auto-generated. Back to [summary](../summary.md).

- **Language:** [VHDL](../by-language/vhdl.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-06-24` | 5381 | `internal/extractors/vhdl/extractor.go`<br>`internal/extractors/vhdl/extractor_test.go` | Quartus detected from in-HDL Intel/Altera synthesis attributes — attribute altera_attribute / preserve / noprune / keep_user_pin / chip_pin / useioff (#5381 quartusAttrRE); emits SCOPE.Component(subtype=tool, tool=quartus) + file->tool USES edge. Partial: in-HDL attribute signal detection only, NOT .qpf/.qsf project / Tcl-flow parsing. Proven by TestVHDL_QuartusAttrs / _NoToolFalsePositive. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update build.vhdl.quartus ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
