package solidity_test

import "testing"

// lineFixture exercises every way Solidity line attribution used to drift: a
// contract header that wraps across lines, multi-line NatSpec above it, doc
// comments above a member, and a block comment above another. Fixture line 1
// is the SPDX comment.
const lineFixture = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "./Helper.sol";

/**
 * @title Vault
 * @notice Multi-line NatSpec. These newlines used to be blanked away with the
 *         rest of the comment, so every line below it counted short.
 */
contract Vault is
    IERC20,
    Helper
{
    event Deposited(address who);

    /// A doc comment. Scrubbing it to blanks let the leading whitespace class
    /// cross it, dragging the match start up onto the first blank line above.
    modifier onlyPositive(uint256 amount) {
        require(amount > 0);
        _;
    }

    /* A block comment
       spanning two lines. */
    function deposit(uint256 amount)
        external
        onlyPositive(amount)
    {
        emit Deposited(msg.sender);
    }
}
`

// TestSolidity_LineAttribution pins the reported line of every entity in
// lineFixture. Nothing else in this package asserts StartLine, which is how
// four separate defects survived: multi-line comments lost their newlines, the
// leading whitespace class crossed line boundaries, members were anchored on
// the contract's match start rather than its opening brace, and imports were
// emitted with no line at all.
func TestSolidity_LineAttribution(t *testing.T) {
	ents := runSolidity(t, lineFixture, "contracts/Vault.sol")

	for _, tc := range []struct {
		name string
		kind string
		line int
	}{
		{"IERC20", "SCOPE.Component", 4},
		{"Helper", "SCOPE.Component", 5},
		{"Vault", "SCOPE.Component", 12},
		{"Vault.Deposited", "SCOPE.Operation", 16},
		{"Vault.onlyPositive", "SCOPE.Operation", 20},
		{"Vault.deposit", "SCOPE.Operation", 27},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ent := solFind(ents, tc.name, tc.kind)
			if ent == nil {
				t.Fatalf("no %s entity named %q", tc.kind, tc.name)
			}
			if ent.StartLine != tc.line {
				t.Errorf("StartLine = %d, want %d", ent.StartLine, tc.line)
			}
		})
	}
}
