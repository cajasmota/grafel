<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.solidity.framework.openzeppelin` — OpenZeppelin Contracts

Auto-generated. Back to [summary](../summary.md).

- **Language:** [solidity](../by-language/solidity.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Discriminates on | ✅ `full` | `2026-06-24` | 5371 | `internal/extractors/solidity/extractor.go`<br>`internal/extractors/solidity/frameworks.go`<br>`internal/extractors/solidity/frameworks_test.go` | OpenZeppelin (de-facto Solidity contract library) recognised purely from .sol source: scanImportFrameworks flags any @openzeppelin/contracts(-upgradeable) import, and isOpenZeppelinBase matches a canonical OZ base/mixin in the 'is ...' inheritance list (ERC20/ERC721/ERC1155 + variants, Ownable/Ownable2Step, AccessControl(+Enumerable), Pausable, ReentrancyGuard, Initializable, UUPSUpgradeable, EIP712, ...). Matched contracts are stamped Properties[framework]=openzeppelin + openzeppelin=true and each OZ-base EXTENDS edge carries Properties[framework]=openzeppelin. Detection fires from inheritance ALONE (flattened/vendored source with no import). Proven by TestSolidity_OpenZeppelin_FrameworkStamp / _ExtendsEdgeTagged / _InheritanceOnly / _OpenZeppelinWinsPrecedence and the negative TestSolidity_NoFramework_NotStamped. |
| Import resolution quality | ✅ `full` | `2026-06-24` | 5371 | `internal/extractors/solidity/frameworks.go`<br>`internal/extractors/solidity/frameworks_test.go` | @openzeppelin/contracts and @openzeppelin/contracts-upgradeable import paths are classified as the OpenZeppelin library binding (scanImportFrameworks) and surfaced on the contract + EXTENDS edges. Existing IMPORTS edges already carry the full OZ module path (token/ERC20/ERC20.sol etc.) via buildImportEntities. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.solidity.framework.openzeppelin ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
