// Solidity framework / tool detection (issue #5371, epic #5360 Group A).
//
// The solidity extractor only ever sees `.sol` files — Hardhat/Truffle config
// files classify as jsts and `foundry.toml` as toml, so config-file parsing is
// out of this extractor's reach. Everything here is therefore `.sol`-driven and
// bolts onto the existing contract / IMPORTS / EXTENDS model:
//
//   - OpenZeppelin  — de-facto contract library. Detected from `@openzeppelin/
//     contracts` import paths and from EXTENDS-parents that name
//     a canonical OZ base (ERC20/ERC721/Ownable/…). Contracts
//     and their OZ-base EXTENDS edges are stamped
//     framework="openzeppelin". (genuinely full from .sol)
//   - Foundry       — `import "forge-std/…"` and forge test/script contracts
//     (`is Test` / `is Script`). Stamped framework="foundry".
//     (partial — foundry.toml profiles/remappings are a follow-up)
//   - Hardhat       — `import "hardhat/console.sol"` and other hardhat/* helpers.
//     Stamped framework="hardhat". (partial — config tasks/
//     plugins + deploy scripts are jsts-side follow-ups)
//
// Truffle (truffle-config.js + migrations) and ethers/web3 (JS client) are
// JS-side and intentionally NOT faked here.
package solidity

import "strings"

// canonical OpenZeppelin base contracts/mixins that show up in `is …` lists.
var openzeppelinBases = map[string]bool{
	"ERC20":                   true,
	"ERC20Burnable":           true,
	"ERC20Capped":             true,
	"ERC20Pausable":           true,
	"ERC20Permit":             true,
	"ERC20Votes":              true,
	"ERC20Snapshot":           true,
	"ERC20FlashMint":          true,
	"ERC721":                  true,
	"ERC721Enumerable":        true,
	"ERC721URIStorage":        true,
	"ERC721Burnable":          true,
	"ERC721Pausable":          true,
	"ERC721Royalty":           true,
	"ERC1155":                 true,
	"ERC1155Burnable":         true,
	"ERC1155Pausable":         true,
	"ERC1155Supply":           true,
	"Ownable":                 true,
	"Ownable2Step":            true,
	"AccessControl":           true,
	"AccessControlEnumerable": true,
	"Pausable":                true,
	"ReentrancyGuard":         true,
	"Initializable":           true,
	"UUPSUpgradeable":         true,
	"ERC1967Proxy":            true,
	"Multicall":               true,
	"Nonces":                  true,
	"EIP712":                  true,
}

// frameworkSignals holds what an import scan found for a single `.sol` file.
type frameworkSignals struct {
	openzeppelin bool // any @openzeppelin/contracts import
	foundry      bool // any forge-std import
	hardhat      bool // any hardhat/* import
}

// scanImportFrameworks inspects raw import paths for framework signals.
func scanImportFrameworks(importPaths []string) frameworkSignals {
	var s frameworkSignals
	for _, p := range importPaths {
		switch {
		case strings.HasPrefix(p, "@openzeppelin/contracts"),
			strings.HasPrefix(p, "@openzeppelin/contracts-upgradeable"):
			s.openzeppelin = true
		case strings.HasPrefix(p, "forge-std/"),
			strings.HasPrefix(p, "@forge-std/"):
			s.foundry = true
		case strings.HasPrefix(p, "hardhat/"):
			s.hardhat = true
		}
	}
	return s
}

// isOpenZeppelinBase reports whether a parent name in an `is …` list is a known
// OpenZeppelin base contract or mixin.
func isOpenZeppelinBase(parent string) bool {
	return openzeppelinBases[parent]
}

// foundryTestKind classifies a contract by its inheritance list as a forge
// test/script contract. Returns "test", "script", or "" (not foundry).
func foundryTestKind(extends []string) string {
	for _, p := range extends {
		switch p {
		case "Test", "DSTest":
			return "test"
		case "Script":
			return "script"
		}
	}
	return ""
}

// setProp sets a property on an entity, allocating the map on first use.
func setProp(props *map[string]string, key, val string) {
	if *props == nil {
		*props = make(map[string]string)
	}
	(*props)[key] = val
}
