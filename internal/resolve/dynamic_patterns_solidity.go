package resolve

import "regexp"

// solidityDynamicPatterns are per-language patterns for Solidity.
// Registered via init() into dynamicPatternsByLang.
//
// The Solidity extractor (internal/extractors/solidity/extractor.go) emits
// CALLS edges whose ToID is either:
//   - a bare function name: "_mint", "_transfer"
//   - a dotted member call: "token.transfer", "msg.sender", "abi.encode"
//
// Three categories of patterns:
//
//  1. Solidity globals — EVM builtins that are always in scope.
//     msg.sender / msg.value / block.timestamp / tx.origin, etc.
//
//  2. ABI / address built-ins — abi.encode*, address.call, delegatecall.
//
//  3. OpenZeppelin common patterns — onlyOwner, nonReentrant, whenNotPaused.
//     Gated to lang=="solidity" to avoid false-positive cross-language hits.
//
//  4. ERC-20 / ERC-721 standard interface methods — transfer, balanceOf, etc.
//     These are extremely common Solidity identifiers and unlikely to collide
//     with other language-specific extractors when the language tag is checked.
var solidityDynamicPatterns = []*regexp.Regexp{
	// ── msg context ──────────────────────────────────────────────────────
	regexp.MustCompile(`^msg\.sender$`),    // msg.sender — the calling address
	regexp.MustCompile(`^msg\.value$`),     // msg.value — ETH sent with call
	regexp.MustCompile(`^msg\.data$`),      // msg.data — raw calldata bytes
	regexp.MustCompile(`^msg\.sig$`),       // msg.sig — function selector (4 bytes)
	regexp.MustCompile(`^msg\.gas$`),       // msg.gas (legacy, pre-0.5 alias)

	// ── block context ────────────────────────────────────────────────────
	regexp.MustCompile(`^block\.timestamp$`), // block.timestamp — unix epoch of current block
	regexp.MustCompile(`^block\.number$`),    // block.number — current block height
	regexp.MustCompile(`^block\.difficulty$`), // block.difficulty (pre-merge)
	regexp.MustCompile(`^block\.prevrandao$`), // block.prevrandao (post-merge randomness)
	regexp.MustCompile(`^block\.chainid$`),    // block.chainid
	regexp.MustCompile(`^block\.basefee$`),    // block.basefee (EIP-1559)
	regexp.MustCompile(`^block\.coinbase$`),   // block.coinbase — miner/validator address
	regexp.MustCompile(`^block\.gaslimit$`),   // block.gaslimit

	// ── tx context ───────────────────────────────────────────────────────
	regexp.MustCompile(`^tx\.origin$`),    // tx.origin — original EOA
	regexp.MustCompile(`^tx\.gasprice$`),  // tx.gasprice

	// ── ABI encoding ─────────────────────────────────────────────────────
	regexp.MustCompile(`^abi\.encode$`),            // abi.encode(...)
	regexp.MustCompile(`^abi\.encodePacked$`),      // abi.encodePacked(...)
	regexp.MustCompile(`^abi\.encodeWithSelector$`), // abi.encodeWithSelector(...)
	regexp.MustCompile(`^abi\.encodeWithSignature$`), // abi.encodeWithSignature(...)
	regexp.MustCompile(`^abi\.encodeCall$`),         // abi.encodeCall(...)
	regexp.MustCompile(`^abi\.decode$`),             // abi.decode(...)

	// ── address methods ──────────────────────────────────────────────────
	regexp.MustCompile(`^address\.transfer$`),      // payable(addr).transfer(...)
	regexp.MustCompile(`^address\.send$`),          // payable(addr).send(...)
	regexp.MustCompile(`^address\.call$`),          // addr.call{value:n}(...)
	regexp.MustCompile(`^address\.delegatecall$`),  // addr.delegatecall(...)
	regexp.MustCompile(`^address\.staticcall$`),    // addr.staticcall(...)
	regexp.MustCompile(`^\.transfer$`),             // .transfer(...) — short form
	regexp.MustCompile(`^\.send$`),                 // .send(...) — short form
	regexp.MustCompile(`^\.call$`),                 // .call(...) — short form
	regexp.MustCompile(`^\.delegatecall$`),         // .delegatecall(...)
	regexp.MustCompile(`^\.staticcall$`),           // .staticcall(...)

	// ── String / Bytes helpers ────────────────────────────────────────────
	regexp.MustCompile(`^toString$`),               // uint256.toString() via OpenZeppelin Strings
	regexp.MustCompile(`^concat$`),                 // string.concat(...) — 0.8.12+
	regexp.MustCompile(`^\.length$`),               // array/bytes .length

	// ── ERC-20 standard interface ─────────────────────────────────────────
	// Gated to lang=="solidity" — these names collide with Ruby/Python in other langs.
	regexp.MustCompile(`^ERC20\.transfer$`),        // ERC20.transfer(to, amount)
	regexp.MustCompile(`^ERC20\.transferFrom$`),    // ERC20.transferFrom(from, to, amount)
	regexp.MustCompile(`^ERC20\.approve$`),         // ERC20.approve(spender, amount)
	regexp.MustCompile(`^ERC20\.allowance$`),       // ERC20.allowance(owner, spender)
	regexp.MustCompile(`^ERC20\.balanceOf$`),       // ERC20.balanceOf(account)
	regexp.MustCompile(`^ERC20\.totalSupply$`),     // ERC20.totalSupply()
	regexp.MustCompile(`^ERC20\.mint$`),            // ERC20.mint(to, amount) (non-standard but common)
	regexp.MustCompile(`^ERC20\.burn$`),            // ERC20.burn(from, amount)

	// ── ERC-721 standard interface ────────────────────────────────────────
	regexp.MustCompile(`^ERC721\.ownerOf$`),        // ERC721.ownerOf(tokenId)
	regexp.MustCompile(`^ERC721\.balanceOf$`),      // ERC721.balanceOf(owner)
	regexp.MustCompile(`^ERC721\.transferFrom$`),   // ERC721.transferFrom(from, to, tokenId)
	regexp.MustCompile(`^ERC721\.safeTransferFrom$`), // ERC721.safeTransferFrom(...)
	regexp.MustCompile(`^ERC721\.approve$`),        // ERC721.approve(to, tokenId)
	regexp.MustCompile(`^ERC721\.getApproved$`),    // ERC721.getApproved(tokenId)
	regexp.MustCompile(`^ERC721\.isApprovedForAll$`), // ERC721.isApprovedForAll(owner, op)
	regexp.MustCompile(`^ERC721\.setApprovalForAll$`), // ERC721.setApprovalForAll(op, approved)
	regexp.MustCompile(`^ERC721\.tokenURI$`),       // ERC721.tokenURI(tokenId)
	regexp.MustCompile(`^ERC721\.mint$`),           // ERC721._mint(to, tokenId)

	// ── OpenZeppelin patterns ─────────────────────────────────────────────
	regexp.MustCompile(`^Ownable\.onlyOwner$`),            // Ownable.onlyOwner modifier
	regexp.MustCompile(`^Ownable\.owner$`),                // Ownable.owner() view
	regexp.MustCompile(`^Ownable\.transferOwnership$`),    // Ownable.transferOwnership(newOwner)
	regexp.MustCompile(`^Ownable\.renounceOwnership$`),    // Ownable.renounceOwnership()
	regexp.MustCompile(`^ReentrancyGuard\.nonReentrant$`), // ReentrancyGuard.nonReentrant modifier
	regexp.MustCompile(`^Pausable\.whenNotPaused$`),       // Pausable.whenNotPaused modifier
	regexp.MustCompile(`^Pausable\.whenPaused$`),          // Pausable.whenPaused modifier
	regexp.MustCompile(`^Pausable\.pause$`),               // Pausable.pause()
	regexp.MustCompile(`^Pausable\.unpause$`),             // Pausable.unpause()
	regexp.MustCompile(`^AccessControl\.hasRole$`),        // AccessControl.hasRole(role, account)
	regexp.MustCompile(`^AccessControl\.grantRole$`),      // AccessControl.grantRole(role, account)
	regexp.MustCompile(`^AccessControl\.revokeRole$`),     // AccessControl.revokeRole(role, account)
	regexp.MustCompile(`^SafeERC20\.safeTransfer$`),       // SafeERC20.safeTransfer(token, to, value)
	regexp.MustCompile(`^SafeERC20\.safeTransferFrom$`),   // SafeERC20.safeTransferFrom(...)
	regexp.MustCompile(`^SafeERC20\.safeApprove$`),        // SafeERC20.safeApprove(...)
	regexp.MustCompile(`^Address\.sendValue$`),            // Address.sendValue(payable, amount)
	regexp.MustCompile(`^Address\.functionCall$`),         // Address.functionCall(target, data)
	regexp.MustCompile(`^Address\.isContract$`),           // Address.isContract(addr)
	regexp.MustCompile(`^Strings\.toString$`),             // Strings.toString(uint256)
	regexp.MustCompile(`^ECDSA\.recover$`),                // ECDSA.recover(hash, sig)
	regexp.MustCompile(`^ECDSA\.toEthSignedMessageHash$`), // ECDSA.toEthSignedMessageHash(hash)
}

func init() {
	dynamicPatternsByLang["solidity"] = solidityDynamicPatterns
}
