<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `build.vhdl.ghdl` — GHDL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [VHDL](../by-language/vhdl.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-06-24` | 5381 | `internal/extractors/vhdl/extractor.go`<br>`internal/extractors/vhdl/extractor_test.go` | GHDL detected from in-HDL simulation pragmas — -- pragma translate_off / -- synthesis translate_off / -- synopsys translate_on / -- rtl_synthesis off (#5381 ghdlPragmaRE); emits SCOPE.Component(subtype=tool, tool=ghdl) + file->tool USES edge. Partial: in-HDL pragma signal detection only, NOT .do/.cf/--std= command-line or Makefile/GHDL-project parsing; no analyse/elaborate dependency-graph extraction. Proven by TestVHDL_GhdlPragma / _NoToolFalsePositive. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update build.vhdl.ghdl ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
