<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.vhdl.core` — VHDL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [VHDL](../by-language/vhdl.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Core extraction | 🟢 `partial` | `2026-06-24` | 5381 | `internal/extractors/vhdl/extractor.go`<br>`internal/extractors/vhdl/extractor_test.go` | Regex bootstrap (no tree-sitter grammar). entity/architecture/package/package-body -> SCOPE.Component (architecture carries a PORT_OF edge to its entity, package_body a PORT_OF to its package); function/procedure -> SCOPE.Operation; entity port clause `port ( name : in|out|inout|buffer|linkage type )` -> SCOPE.Schema(subtype=port) with direction + width (the downto/to vector range) props, CONTAINS-wired from the owning entity (#5381 buildVHDLPortEntities); component instantiations `inst : Comp [generic map (...)] port map (...)` -> USES edges carrying instance_name + component_type + parameterized props so the instance topology graph is navigable (#5381 collectVHDLInstantiations); library/use -> IMPORTS. Partial: no full type resolution / no signal-level dataflow / no configuration-or-generate elaboration / signals skipped (regex limits). Proven by TestVHDL_EntityPorts / _PortDedup / _InstanceTopology / _ComponentInstantiation. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.vhdl.core ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
