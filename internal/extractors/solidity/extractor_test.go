package solidity_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/solidity"
	"github.com/cajasmota/archigraph/internal/types"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

func runSolidity(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("solidity")
	if !ok {
		t.Fatal("solidity extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "solidity",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func solFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func solFindSubtype(ents []types.EntityRecord, name, kind, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind && ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

func solHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	for i := range ents {
		if ents[i].Name != name || ents[i].Kind != kind {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == edgeKind && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

func solHasRelPartial(ents []types.EntityRecord, name, kind, edgeKind, toIDContains string) bool {
	for i := range ents {
		if ents[i].Name != name || ents[i].Kind != kind {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == edgeKind && strings.Contains(r.ToID, toIDContains) {
				return true
			}
		}
	}
	return false
}

// ── Basic registration ────────────────────────────────────────────────────────

func TestSolidity_Registered(t *testing.T) {
	_, ok := extractor.Get("solidity")
	if !ok {
		t.Fatal("solidity extractor not registered")
	}
}

func TestSolidity_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("solidity")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.sol",
		Content:  []byte{},
		Language: "solidity",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// ── Import extraction ─────────────────────────────────────────────────────────

func TestSolidity_Imports(t *testing.T) {
	src := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "./IERC20.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import {SafeMath} from "./SafeMath.sol";

contract TokenMint {
}
`
	ents := runSolidity(t, src, "TokenMint.sol")

	// Three distinct import paths.
	if !solHasRel(ents, "IERC20", "SCOPE.Component", "IMPORTS", "./IERC20.sol") {
		t.Error("expected IMPORTS edge for ./IERC20.sol")
	}
	if !solHasRel(ents, "Ownable", "SCOPE.Component", "IMPORTS", "@openzeppelin/contracts/access/Ownable.sol") {
		t.Error("expected IMPORTS edge for @openzeppelin/contracts/access/Ownable.sol")
	}
	if !solHasRel(ents, "SafeMath", "SCOPE.Component", "IMPORTS", "./SafeMath.sol") {
		t.Error("expected IMPORTS edge for ./SafeMath.sol")
	}
}

// ── Contract / library / interface declaration ────────────────────────────────

func TestSolidity_ContractDeclaration(t *testing.T) {
	src := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TokenMint {
    uint256 public totalSupply;
}
`
	ents := runSolidity(t, src, "TokenMint.sol")
	c := solFindSubtype(ents, "TokenMint", "SCOPE.Component", "contract")
	if c == nil {
		t.Fatal("expected SCOPE.Component(contract) TokenMint")
	}
}

func TestSolidity_LibraryDeclaration(t *testing.T) {
	src := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}
`
	ents := runSolidity(t, src, "SafeMath.sol")
	c := solFindSubtype(ents, "SafeMath", "SCOPE.Component", "library")
	if c == nil {
		t.Fatal("expected SCOPE.Component(library) SafeMath")
	}
}

func TestSolidity_InterfaceDeclaration(t *testing.T) {
	src := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IERC20 {
    function totalSupply() external view returns (uint256);
    function balanceOf(address account) external view returns (uint256);
    event Transfer(address indexed from, address indexed to, uint256 value);
}
`
	ents := runSolidity(t, src, "IERC20.sol")
	c := solFindSubtype(ents, "IERC20", "SCOPE.Component", "interface")
	if c == nil {
		t.Fatal("expected SCOPE.Component(interface) IERC20")
	}

	// Functions inside interface.
	if solFind(ents, "IERC20.totalSupply", "SCOPE.Operation") == nil {
		t.Error("expected IERC20.totalSupply operation")
	}
	if solFind(ents, "IERC20.balanceOf", "SCOPE.Operation") == nil {
		t.Error("expected IERC20.balanceOf operation")
	}
	// Event inside interface.
	if solFindSubtype(ents, "IERC20.Transfer", "SCOPE.Operation", "event") == nil {
		t.Error("expected IERC20.Transfer event")
	}
}

// ── Inheritance (EXTENDS edges) ───────────────────────────────────────────────

func TestSolidity_Extends(t *testing.T) {
	src := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "./Ownable.sol";
import "./IERC20.sol";

contract TokenMint is Ownable, IERC20 {
    uint256 private _totalSupply;
}
`
	ents := runSolidity(t, src, "TokenMint.sol")

	if !solHasRel(ents, "TokenMint", "SCOPE.Component", "EXTENDS", "Ownable") {
		t.Error("expected EXTENDS Ownable")
	}
	if !solHasRel(ents, "TokenMint", "SCOPE.Component", "EXTENDS", "IERC20") {
		t.Error("expected EXTENDS IERC20")
	}
}

// ── Function extraction ───────────────────────────────────────────────────────

func TestSolidity_Functions(t *testing.T) {
	src := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TokenMint {
    function mint(address to, uint256 amount) public {
        _mint(to, amount);
    }

    function burn(address from, uint256 amount) public {
        _burn(from, amount);
    }

    function _mint(address account, uint256 amount) internal virtual {
        _totalSupply += amount;
    }

    function _burn(address account, uint256 amount) internal virtual {
        _totalSupply -= amount;
    }
}
`
	ents := runSolidity(t, src, "TokenMint.sol")

	for _, fn := range []string{"TokenMint.mint", "TokenMint.burn", "TokenMint._mint", "TokenMint._burn"} {
		if solFindSubtype(ents, fn, "SCOPE.Operation", "function") == nil {
			t.Errorf("expected function %s", fn)
		}
	}
}

// ── Event extraction ──────────────────────────────────────────────────────────

func TestSolidity_Events(t *testing.T) {
	src := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TokenMint {
    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);
}
`
	ents := runSolidity(t, src, "TokenMint.sol")

	if solFindSubtype(ents, "TokenMint.Transfer", "SCOPE.Operation", "event") == nil {
		t.Error("expected event TokenMint.Transfer")
	}
	if solFindSubtype(ents, "TokenMint.Approval", "SCOPE.Operation", "event") == nil {
		t.Error("expected event TokenMint.Approval")
	}
}

// ── Modifier extraction ───────────────────────────────────────────────────────

func TestSolidity_Modifiers(t *testing.T) {
	src := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract StakingVault {
    address public owner;

    modifier onlyOwner() {
        require(msg.sender == owner, "Not owner");
        _;
    }

    modifier nonReentrant() {
        require(!_locked, "Reentrant call");
        _locked = true;
        _;
        _locked = false;
    }
}
`
	ents := runSolidity(t, src, "StakingVault.sol")

	if solFindSubtype(ents, "StakingVault.onlyOwner", "SCOPE.Operation", "modifier") == nil {
		t.Error("expected modifier StakingVault.onlyOwner")
	}
	if solFindSubtype(ents, "StakingVault.nonReentrant", "SCOPE.Operation", "modifier") == nil {
		t.Error("expected modifier StakingVault.nonReentrant")
	}
}

// ── CONTAINS edges ────────────────────────────────────────────────────────────

func TestSolidity_ContainsEdges(t *testing.T) {
	src := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TokenMint {
    event Transfer(address indexed from, address indexed to, uint256 value);

    modifier onlyMinter() {
        require(isMinter(msg.sender), "Not minter");
        _;
    }

    function mint(address to, uint256 amount) public onlyMinter {
        emit Transfer(address(0), to, amount);
    }
}
`
	ents := runSolidity(t, src, "TokenMint.sol")

	// Contract should have CONTAINS edges pointing to member structural refs.
	if !solHasRelPartial(ents, "TokenMint", "SCOPE.Component", "CONTAINS", "TokenMint.Transfer") {
		t.Error("expected CONTAINS edge to TokenMint.Transfer")
	}
	if !solHasRelPartial(ents, "TokenMint", "SCOPE.Component", "CONTAINS", "TokenMint.onlyMinter") {
		t.Error("expected CONTAINS edge to TokenMint.onlyMinter")
	}
	if !solHasRelPartial(ents, "TokenMint", "SCOPE.Component", "CONTAINS", "TokenMint.mint") {
		t.Error("expected CONTAINS edge to TokenMint.mint")
	}
}

// ── CALLS edges ───────────────────────────────────────────────────────────────

func TestSolidity_CallsEdges(t *testing.T) {
	src := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract StakingVault {
    function deposit(uint256 amount) external {
        token.transferFrom(msg.sender, address(this), amount);
        _updateRewards(msg.sender);
    }

    function _updateRewards(address account) internal {
        rewards[account] += calculateReward(account);
    }
}
`
	ents := runSolidity(t, src, "StakingVault.sol")

	// deposit calls token.transferFrom (dotted).
	if !solHasRel(ents, "StakingVault.deposit", "SCOPE.Operation", "CALLS", "token.transferFrom") {
		t.Error("expected CALLS token.transferFrom from deposit")
	}
	// deposit calls _updateRewards (bare).
	if !solHasRel(ents, "StakingVault.deposit", "SCOPE.Operation", "CALLS", "_updateRewards") {
		t.Error("expected CALLS _updateRewards from deposit")
	}
}

// ── Synthetic ERC20 fixture — ≥80% entity recall ─────────────────────────────

// tokenMintSrc is a synthetic ERC20-style TokenMint contract used as the
// acceptance fixture. Expected entities:
//   - SCOPE.Component(contract): TokenMint (subtype=contract)
//   - SCOPE.Operation(function): TokenMint.totalSupply, TokenMint.balanceOf,
//     TokenMint.transfer, TokenMint.allowance, TokenMint.approve,
//     TokenMint.transferFrom, TokenMint.mint, TokenMint.burn
//   - SCOPE.Operation(event): TokenMint.Transfer, TokenMint.Approval
//   - SCOPE.Operation(modifier): TokenMint.onlyOwner
//   - EXTENDS: IERC20
const tokenMintSrc = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "./IERC20.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

contract TokenMint is IERC20, Ownable {
    string public name;
    string public symbol;
    uint8 public decimals;
    uint256 private _totalSupply;
    address public owner;

    mapping(address => uint256) private _balances;
    mapping(address => mapping(address => uint256)) private _allowances;

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);

    modifier onlyOwner() {
        require(msg.sender == owner, "Not owner");
        _;
    }

    constructor(string memory _name, string memory _symbol) {
        name = _name;
        symbol = _symbol;
        decimals = 18;
        owner = msg.sender;
    }

    function totalSupply() public view returns (uint256) {
        return _totalSupply;
    }

    function balanceOf(address account) public view returns (uint256) {
        return _balances[account];
    }

    function transfer(address to, uint256 amount) public returns (bool) {
        _transfer(msg.sender, to, amount);
        return true;
    }

    function allowance(address _owner, address spender) public view returns (uint256) {
        return _allowances[_owner][spender];
    }

    function approve(address spender, uint256 amount) public returns (bool) {
        _approve(msg.sender, spender, amount);
        return true;
    }

    function transferFrom(address from, address to, uint256 amount) public returns (bool) {
        _transfer(from, to, amount);
        _approve(from, msg.sender, _allowances[from][msg.sender] - amount);
        return true;
    }

    function mint(address to, uint256 amount) public onlyOwner {
        _mint(to, amount);
    }

    function burn(address from, uint256 amount) public onlyOwner {
        _burn(from, amount);
    }

    function _transfer(address from, address to, uint256 amount) internal {
        require(from != address(0), "Transfer from zero");
        require(to != address(0), "Transfer to zero");
        _balances[from] -= amount;
        _balances[to] += amount;
        emit Transfer(from, to, amount);
    }

    function _approve(address _owner, address spender, uint256 amount) internal {
        require(_owner != address(0), "Approve from zero");
        require(spender != address(0), "Approve to zero");
        _allowances[_owner][spender] = amount;
        emit Approval(_owner, spender, amount);
    }

    function _mint(address account, uint256 amount) internal {
        require(account != address(0), "Mint to zero");
        _totalSupply += amount;
        _balances[account] += amount;
        emit Transfer(address(0), account, amount);
    }

    function _burn(address account, uint256 amount) internal {
        require(account != address(0), "Burn from zero");
        _balances[account] -= amount;
        _totalSupply -= amount;
        emit Transfer(account, address(0), amount);
    }
}
`

// stakingVaultSrc is a synthetic StakingVault contract.
// Expected entities:
//   - SCOPE.Component(contract): StakingVault
//   - SCOPE.Operation(function): StakingVault.deposit, StakingVault.withdraw,
//     StakingVault.claimRewards, StakingVault._updateRewards,
//     StakingVault.calculateReward, StakingVault.getStakedBalance
//   - SCOPE.Operation(event): StakingVault.Deposited, StakingVault.Withdrawn,
//     StakingVault.RewardClaimed
//   - SCOPE.Operation(modifier): StakingVault.nonReentrant
//   - EXTENDS: ReentrancyGuard
const stakingVaultSrc = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "./IERC20.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";

contract StakingVault is ReentrancyGuard {
    IERC20 public token;
    uint256 public rewardRate;

    mapping(address => uint256) private _stakedBalances;
    mapping(address => uint256) private _rewards;
    mapping(address => uint256) private _lastRewardTime;

    event Deposited(address indexed user, uint256 amount);
    event Withdrawn(address indexed user, uint256 amount);
    event RewardClaimed(address indexed user, uint256 reward);

    bool private _locked;

    modifier nonReentrant() {
        require(!_locked, "Reentrant call");
        _locked = true;
        _;
        _locked = false;
    }

    constructor(address tokenAddress, uint256 _rewardRate) {
        token = IERC20(tokenAddress);
        rewardRate = _rewardRate;
    }

    function deposit(uint256 amount) external nonReentrant {
        require(amount > 0, "Amount must be > 0");
        token.transferFrom(msg.sender, address(this), amount);
        _updateRewards(msg.sender);
        _stakedBalances[msg.sender] += amount;
        emit Deposited(msg.sender, amount);
    }

    function withdraw(uint256 amount) external nonReentrant {
        require(_stakedBalances[msg.sender] >= amount, "Insufficient balance");
        _updateRewards(msg.sender);
        _stakedBalances[msg.sender] -= amount;
        token.transfer(msg.sender, amount);
        emit Withdrawn(msg.sender, amount);
    }

    function claimRewards() external nonReentrant {
        _updateRewards(msg.sender);
        uint256 reward = _rewards[msg.sender];
        require(reward > 0, "No rewards");
        _rewards[msg.sender] = 0;
        token.transfer(msg.sender, reward);
        emit RewardClaimed(msg.sender, reward);
    }

    function _updateRewards(address account) internal {
        _rewards[account] += calculateReward(account);
        _lastRewardTime[account] = block.timestamp;
    }

    function calculateReward(address account) public view returns (uint256) {
        uint256 elapsed = block.timestamp - _lastRewardTime[account];
        return _stakedBalances[account] * rewardRate * elapsed / 1e18;
    }

    function getStakedBalance(address account) external view returns (uint256) {
        return _stakedBalances[account];
    }
}
`

// TestSolidity_TokenMintFixture validates ≥80% entity recall on the ERC20 fixture.
func TestSolidity_TokenMintFixture(t *testing.T) {
	ents := runSolidity(t, tokenMintSrc, "TokenMint.sol")

	type check struct {
		name    string
		kind    string
		subtype string
	}
	expected := []check{
		{"TokenMint", "SCOPE.Component", "contract"},
		{"TokenMint.Transfer", "SCOPE.Operation", "event"},
		{"TokenMint.Approval", "SCOPE.Operation", "event"},
		{"TokenMint.onlyOwner", "SCOPE.Operation", "modifier"},
		{"TokenMint.totalSupply", "SCOPE.Operation", "function"},
		{"TokenMint.balanceOf", "SCOPE.Operation", "function"},
		{"TokenMint.transfer", "SCOPE.Operation", "function"},
		{"TokenMint.allowance", "SCOPE.Operation", "function"},
		{"TokenMint.approve", "SCOPE.Operation", "function"},
		{"TokenMint.transferFrom", "SCOPE.Operation", "function"},
		{"TokenMint.mint", "SCOPE.Operation", "function"},
		{"TokenMint.burn", "SCOPE.Operation", "function"},
		{"TokenMint._transfer", "SCOPE.Operation", "function"},
		{"TokenMint._approve", "SCOPE.Operation", "function"},
		{"TokenMint._mint", "SCOPE.Operation", "function"},
		{"TokenMint._burn", "SCOPE.Operation", "function"},
	}

	hit, miss := 0, 0
	for _, ex := range expected {
		if solFindSubtype(ents, ex.name, ex.kind, ex.subtype) != nil {
			hit++
		} else {
			miss++
			t.Logf("MISS: %s %s(%s)", ex.kind, ex.name, ex.subtype)
		}
	}
	recall := float64(hit) / float64(len(expected))
	t.Logf("TokenMint fixture recall: %d/%d = %.0f%%", hit, len(expected), recall*100)
	if recall < 0.80 {
		t.Errorf("entity recall %.0f%% < 80%% threshold", recall*100)
	}

	// EXTENDS edges.
	if !solHasRel(ents, "TokenMint", "SCOPE.Component", "EXTENDS", "IERC20") {
		t.Error("expected EXTENDS IERC20")
	}
	if !solHasRel(ents, "TokenMint", "SCOPE.Component", "EXTENDS", "Ownable") {
		t.Error("expected EXTENDS Ownable")
	}
}

// TestSolidity_StakingVaultFixture validates ≥80% entity recall on the Vault fixture.
func TestSolidity_StakingVaultFixture(t *testing.T) {
	ents := runSolidity(t, stakingVaultSrc, "StakingVault.sol")

	type check struct {
		name    string
		kind    string
		subtype string
	}
	expected := []check{
		{"StakingVault", "SCOPE.Component", "contract"},
		{"StakingVault.Deposited", "SCOPE.Operation", "event"},
		{"StakingVault.Withdrawn", "SCOPE.Operation", "event"},
		{"StakingVault.RewardClaimed", "SCOPE.Operation", "event"},
		{"StakingVault.nonReentrant", "SCOPE.Operation", "modifier"},
		{"StakingVault.deposit", "SCOPE.Operation", "function"},
		{"StakingVault.withdraw", "SCOPE.Operation", "function"},
		{"StakingVault.claimRewards", "SCOPE.Operation", "function"},
		{"StakingVault._updateRewards", "SCOPE.Operation", "function"},
		{"StakingVault.calculateReward", "SCOPE.Operation", "function"},
		{"StakingVault.getStakedBalance", "SCOPE.Operation", "function"},
	}

	hit, miss := 0, 0
	for _, ex := range expected {
		if solFindSubtype(ents, ex.name, ex.kind, ex.subtype) != nil {
			hit++
		} else {
			miss++
			t.Logf("MISS: %s %s(%s)", ex.kind, ex.name, ex.subtype)
		}
	}
	recall := float64(hit) / float64(len(expected))
	t.Logf("StakingVault fixture recall: %d/%d = %.0f%%", hit, len(expected), recall*100)
	if recall < 0.80 {
		t.Errorf("entity recall %.0f%% < 80%% threshold", recall*100)
	}

	// EXTENDS edge.
	if !solHasRel(ents, "StakingVault", "SCOPE.Component", "EXTENDS", "ReentrancyGuard") {
		t.Error("expected EXTENDS ReentrancyGuard")
	}
}

// TestSolidity_NoFalsePositives verifies that keyword tokens do not appear as
// CALLS edges on any entity extracted from the TokenMint fixture.
func TestSolidity_NoFalsePositives(t *testing.T) {
	ents := runSolidity(t, tokenMintSrc, "TokenMint.sol")

	falsePositiveCandidates := []string{
		"if", "else", "for", "while", "return", "new",
		"public", "private", "internal", "external",
		"pure", "view", "payable", "virtual", "override",
		"constructor", "require", "emit",
	}

	for _, ent := range ents {
		for _, rel := range ent.Relationships {
			if rel.Kind != "CALLS" {
				continue
			}
			for _, kw := range falsePositiveCandidates {
				if rel.ToID == kw {
					t.Errorf("false positive CALLS edge: %s → %q (should be filtered)", ent.Name, kw)
				}
			}
		}
	}
}

// TestSolidity_LanguageTagOnRelationships verifies that all embedded
// relationships have Properties["language"] = "solidity".
func TestSolidity_LanguageTagOnRelationships(t *testing.T) {
	ents := runSolidity(t, tokenMintSrc, "TokenMint.sol")
	for _, ent := range ents {
		for _, r := range ent.Relationships {
			if r.Kind == "IMPORTS" || r.Kind == "EXTENDS" || r.Kind == "CALLS" || r.Kind == "CONTAINS" {
				if r.Properties == nil || r.Properties["language"] != "solidity" {
					t.Errorf("relationship %s → %q missing language=solidity tag", r.Kind, r.ToID)
				}
			}
		}
	}
}
