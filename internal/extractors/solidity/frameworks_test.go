package solidity_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// solProp returns the value of Properties[key] on the named component, or "".
func solProp(ents []types.EntityRecord, name, key string) string {
	c := solFind(ents, name, "SCOPE.Component")
	if c == nil || c.Properties == nil {
		return ""
	}
	return c.Properties[key]
}

// ── OpenZeppelin (full) ──────────────────────────────────────────────────────

// Real OZ-based ERC20 with an Ownable mixin — the canonical wizard output.
const ozTokenSrc = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

contract MyToken is ERC20, Ownable {
    constructor(address initialOwner)
        ERC20("MyToken", "MTK")
        Ownable(initialOwner)
    {}

    function mint(address to, uint256 amount) public onlyOwner {
        _mint(to, amount);
    }
}
`

func TestSolidity_OpenZeppelin_FrameworkStamp(t *testing.T) {
	ents := runSolidity(t, ozTokenSrc, "MyToken.sol")

	if got := solProp(ents, "MyToken", "framework"); got != "openzeppelin" {
		t.Errorf("MyToken framework = %q, want openzeppelin", got)
	}
	if got := solProp(ents, "MyToken", "openzeppelin"); got != "true" {
		t.Errorf("MyToken openzeppelin = %q, want true", got)
	}
}

func TestSolidity_OpenZeppelin_ExtendsEdgeTagged(t *testing.T) {
	ents := runSolidity(t, ozTokenSrc, "MyToken.sol")
	c := solFind(ents, "MyToken", "SCOPE.Component")
	if c == nil {
		t.Fatal("MyToken component not found")
	}
	tagged := map[string]bool{}
	for _, r := range c.Relationships {
		if r.Kind == "EXTENDS" && r.Properties != nil && r.Properties["framework"] == "openzeppelin" {
			tagged[r.ToID] = true
		}
	}
	for _, base := range []string{"ERC20", "Ownable"} {
		if !tagged[base] {
			t.Errorf("EXTENDS %s not tagged framework=openzeppelin", base)
		}
	}
}

// OZ detected purely from the `is …` list even without the canonical import
// (e.g. flattened / vendored source).
func TestSolidity_OpenZeppelin_InheritanceOnly(t *testing.T) {
	src := `pragma solidity ^0.8.20;
contract Vault is ReentrancyGuard, AccessControl {
    function withdraw() external nonReentrant {}
}
`
	ents := runSolidity(t, src, "Vault.sol")
	if got := solProp(ents, "Vault", "framework"); got != "openzeppelin" {
		t.Errorf("Vault framework = %q, want openzeppelin (inheritance-only)", got)
	}
}

// A plain contract with no OZ/forge/hardhat signals must NOT be stamped.
func TestSolidity_NoFramework_NotStamped(t *testing.T) {
	src := `pragma solidity ^0.8.20;
contract Plain {
    uint256 public x;
    function set(uint256 v) public { x = v; }
}
`
	ents := runSolidity(t, src, "Plain.sol")
	if got := solProp(ents, "Plain", "framework"); got != "" {
		t.Errorf("Plain framework = %q, want empty (no signals)", got)
	}
}

// ── Foundry (partial) ────────────────────────────────────────────────────────

// Real forge-std test contract.
const forgeTestSrc = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {MyToken} from "../src/MyToken.sol";

contract MyTokenTest is Test {
    MyToken token;

    function setUp() public {
        token = new MyToken(address(this));
    }

    function test_Mint() public {
        token.mint(address(this), 100);
        assertEq(token.balanceOf(address(this)), 100);
    }
}
`

func TestSolidity_Foundry_TestContract(t *testing.T) {
	ents := runSolidity(t, forgeTestSrc, "MyToken.t.sol")
	if got := solProp(ents, "MyTokenTest", "framework"); got != "foundry" {
		t.Errorf("MyTokenTest framework = %q, want foundry", got)
	}
	if got := solProp(ents, "MyTokenTest", "foundry"); got != "true" {
		t.Errorf("MyTokenTest foundry = %q, want true", got)
	}
	if got := solProp(ents, "MyTokenTest", "foundry_kind"); got != "test" {
		t.Errorf("MyTokenTest foundry_kind = %q, want test", got)
	}
}

func TestSolidity_Foundry_ScriptContract(t *testing.T) {
	src := `pragma solidity ^0.8.20;
import {Script} from "forge-std/Script.sol";
contract Deploy is Script {
    function run() external {}
}
`
	ents := runSolidity(t, src, "Deploy.s.sol")
	if got := solProp(ents, "Deploy", "framework"); got != "foundry" {
		t.Errorf("Deploy framework = %q, want foundry", got)
	}
	if got := solProp(ents, "Deploy", "foundry_kind"); got != "script" {
		t.Errorf("Deploy foundry_kind = %q, want script", got)
	}
}

// ── Hardhat (partial) ────────────────────────────────────────────────────────

func TestSolidity_Hardhat_Console(t *testing.T) {
	src := `pragma solidity ^0.8.20;
import "hardhat/console.sol";
contract Debuggable {
    function ping() public view {
        console.log("ping");
    }
}
`
	ents := runSolidity(t, src, "Debuggable.sol")
	if got := solProp(ents, "Debuggable", "framework"); got != "hardhat" {
		t.Errorf("Debuggable framework = %q, want hardhat", got)
	}
	if got := solProp(ents, "Debuggable", "hardhat"); got != "true" {
		t.Errorf("Debuggable hardhat = %q, want true", got)
	}
}

// ── Precedence: OZ wins over hardhat/foundry when a contract uses both ────────

func TestSolidity_OpenZeppelinWinsPrecedence(t *testing.T) {
	src := `pragma solidity ^0.8.20;
import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "hardhat/console.sol";
contract Mix is ERC20 {
    constructor() ERC20("Mix","MIX") {}
}
`
	ents := runSolidity(t, src, "Mix.sol")
	if got := solProp(ents, "Mix", "framework"); got != "openzeppelin" {
		t.Errorf("Mix framework = %q, want openzeppelin (OZ precedence)", got)
	}
	// hardhat marker still recorded.
	if got := solProp(ents, "Mix", "hardhat"); got != "true" {
		t.Errorf("Mix hardhat marker = %q, want true", got)
	}
}
