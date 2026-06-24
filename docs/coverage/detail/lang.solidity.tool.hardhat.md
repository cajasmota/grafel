<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.solidity.tool.hardhat` — Hardhat

Auto-generated. Back to [summary](../summary.md).

- **Language:** [solidity](../by-language/solidity.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🔴 `missing` | — | 5371 | — | Hardhat dependency graph lives in package.json (npm — already covered by build.npm/pkg.npm) and hardhat.config plugin wiring (jsts side). Not synthesised from .sol. Follow-up under epic #5360. |
| Target extraction | 🟢 `partial` | `2026-06-24` | 5371 | `internal/extractors/solidity/frameworks.go`<br>`internal/extractors/solidity/frameworks_test.go` | Hardhat recognised from .sol source: scanImportFrameworks flags hardhat/* imports (e.g. import "hardhat/console.sol"); matched contracts are stamped Properties[framework]=hardhat + hardhat=true. Proven by TestSolidity_Hardhat_Console. Partial (honest): hardhat.config.{js,ts} task/plugin parsing, deploy scripts, and hardhat-deploy artifacts are jsts-side and out of this .sol-only extractor's reach — follow-up under #5360. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.solidity.tool.hardhat ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
