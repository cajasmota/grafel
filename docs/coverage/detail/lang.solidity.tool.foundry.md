<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.solidity.tool.foundry` — Foundry (forge)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [solidity](../by-language/solidity.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🔴 `missing` | — | 5371 | — | foundry.toml dependencies/remappings + git-submodule lib/ tree are not parsed (foundry.toml classifies as toml, outside the .sol extractor). Follow-up under epic #5360. |
| Target extraction | 🟢 `partial` | `2026-06-24` | 5371 | `internal/extractors/solidity/frameworks.go`<br>`internal/extractors/solidity/frameworks_test.go` | Foundry recognised from .sol source: scanImportFrameworks flags forge-std/* (and @forge-std/*) imports, and foundryTestKind classifies a contract by its inheritance list — 'is Test'/'is DSTest' -> foundry_kind=test, 'is Script' -> foundry_kind=script. Matched contracts are stamped Properties[framework]=foundry + foundry=true (+foundry_kind). Proven by TestSolidity_Foundry_TestContract / _Foundry_ScriptContract. Partial (honest): foundry.toml profiles/remappings/[dependencies] and forge cheatcode/dependency-graph extraction are out of this .sol-only extractor's reach (foundry.toml classifies as toml) — follow-up under #5360. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.solidity.tool.foundry ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
