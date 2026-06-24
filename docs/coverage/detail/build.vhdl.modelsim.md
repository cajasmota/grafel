<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `build.vhdl.modelsim` — ModelSim/QuestaSim

Auto-generated. Back to [summary](../summary.md).

- **Language:** [VHDL](../by-language/vhdl.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-06-24` | 5381 | `internal/extractors/vhdl/extractor.go`<br>`internal/extractors/vhdl/extractor_test.go` | ModelSim/QuestaSim detected from in-HDL synthesis-coverage pragmas — -- synthesis off / -- synthesis on / -- coverage off (#5381 modelsimPragmaRE); emits SCOPE.Component(subtype=tool, tool=modelsim) + file->tool USES edge. Partial: in-HDL pragma signal detection only, NOT .do TCL script / vcom-vsim command / modelsim.ini parsing. Proven by TestVHDL_ModelsimPragma / _NoToolFalsePositive. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update build.vhdl.modelsim ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
