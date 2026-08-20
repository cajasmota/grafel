package solidity_test

// Issue #6423: four recall defects in the Solidity scanner, each measured on
// the solidity-mini golden fixture (#6424) before this test existed.
//
//  1. `contract X is ERC20("n","s")` matched nothing at all, because
//     contractRE's inheritance group was `[A-Za-z_][A-Za-z0-9_,\s]*` — a class
//     that cannot hold `(`. findContracts only walks matched bodies, so the
//     contract AND every member inside it vanished. Measured: MyToken.sol
//     yielded a file carrier plus one IMPORTS edge and nothing else.
//  2. `constructor`, `receive()` and `fallback()` emitted nothing: functionRE
//     requires the literal `function` keyword and all three sit in the CALLS
//     denylist.
//  3. Free (file-level) functions, `struct`, `enum`, `error` and `type X is Y`
//     had no code path at all, at either file or contract level. The gap also
//     cost *resolution*: `Vault.deposit --[CALLS]--> computeFee` was emitted
//     but dangled as a bare string because the callee was never extracted.
//  4. Modifier *usage* was never linked. declBody jumps from the parameter
//     list's `)` straight to the body's `{`, so the attribute section — where
//     `onlyOwner` lives — was never read.

import (
	"slices"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// recallFixture carries one instance of every shape #6423 names, in both the
// positions the golden fixture asserts them (file level and contract level),
// plus the shapes that already worked so a regression in the working half is
// still a failure. The `is` clause deliberately mixes a parenthesised
// base-constructor call with a plain parent, which is the form that used to
// delete the whole file.
const recallFixture = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";

error TopLevelFailure(address caller);

enum Mode {
    Open,
    Closed
}

struct Entry {
    address owner;
    uint256 amount;
}

type Price is uint128;

function freeHelper(uint256 amount) pure returns (uint256) {
    return amount / 2;
}

contract Token is ERC20("My Token", "MTK"), Ownable {
    uint256 public cap;

    error TokenEmpty();

    struct Receipt {
        bytes32 id;
    }

    enum Tier {
        Basic,
        Premium
    }

    event Minted(address indexed to, uint256 amount);

    modifier whenOpen() {
        require(true);
        _;
    }

    constructor(uint256 initialCap) Ownable(msg.sender) {
        cap = initialCap;
    }

    receive() external payable {
        cap = cap + 1;
    }

    fallback() external payable {
        revert TopLevelFailure(msg.sender);
    }

    function mint(address to, uint256 amount) public whenOpen returns (uint256) {
        cap = freeHelper(cap);
        emit Minted(to, amount);
        return cap;
    }

    function guarded() public onlyOwner {
        cap = 0;
    }
}
`

// solRawCallTargets returns the raw CALLS targets emitted on one entity.
func solRawCallTargets(t *testing.T, ents []types.EntityRecord, name string) []string {
	t.Helper()
	rec := solFind(ents, name, "SCOPE.Operation")
	if rec == nil {
		t.Fatalf("entity %q not extracted", name)
	}
	var out []string
	for _, r := range rec.Relationships {
		if r.Kind == "CALLS" {
			out = append(out, r.ToID)
		}
	}
	slices.Sort(out)
	return out
}

// solContains reports whether the named contract carries a CONTAINS edge to
// rec, matched on the structural ref the extractor builds for rec's kind.
func solContains(ents []types.EntityRecord, contract string, rec *types.EntityRecord) bool {
	var want string
	switch rec.Kind {
	case "SCOPE.Schema":
		want = extractor.BuildSchemaFieldStructuralRef("solidity", rec.SourceFile, rec.Name)
	default:
		want = extractor.BuildOperationStructuralRef("solidity", rec.SourceFile, rec.Name)
	}
	owner := solFind(ents, contract, "SCOPE.Component")
	if owner == nil {
		return false
	}
	for _, r := range owner.Relationships {
		if r.Kind == "CONTAINS" && r.ToID == want {
			return true
		}
	}
	return false
}

// TestSolidity_6423_BaseConstructorArgsKeepTheFile covers defect 1. The
// whole-file loss is what makes this the severe one: every other assertion in
// this file would also fail while the contract itself is missing, so the
// contract's own presence is asserted separately and first.
func TestSolidity_6423_BaseConstructorArgsKeepTheFile(t *testing.T) {
	ents := runSolidity(t, recallFixture, "contracts/Token.sol")

	if solFind(ents, "Token", "SCOPE.Component") == nil {
		t.Fatalf("contract Token not extracted — `is ERC20(\"n\",\"s\")` still deletes the file")
	}
	for _, want := range []struct{ name, kind string }{
		{"Token.mint", "SCOPE.Operation"},
		{"Token.Minted", "SCOPE.Operation"},
		{"Token.whenOpen", "SCOPE.Operation"},
		{"Token.cap", "SCOPE.Schema"},
	} {
		if solFind(ents, want.name, want.kind) == nil {
			t.Errorf("member %s [%s] not extracted", want.name, want.kind)
		}
	}
}

// TestSolidity_6423_InheritanceListIsParenAware covers the parse half of
// defect 1: the parenthesised base-constructor arguments must be stripped and
// the top-level comma must not split inside them. Splitting the raw list on
// every comma yields the parents `ERC20(` and `)` — both wrong, and the second
// is not even an identifier.
func TestSolidity_6423_InheritanceListIsParenAware(t *testing.T) {
	ents := runSolidity(t, recallFixture, "contracts/Token.sol")
	rec := solFind(ents, "Token", "SCOPE.Component")
	if rec == nil {
		t.Fatal("contract Token not extracted")
	}
	var parents []string
	for _, r := range rec.Relationships {
		if r.Kind == "EXTENDS" {
			parents = append(parents, r.ToID)
		}
	}
	slices.Sort(parents)
	if !slices.Equal(parents, []string{"ERC20", "Ownable"}) {
		t.Errorf("EXTENDS targets = %v, want [ERC20 Ownable]", parents)
	}
}

// TestSolidity_6423_ConstructorReceiveFallback covers defect 2.
func TestSolidity_6423_ConstructorReceiveFallback(t *testing.T) {
	ents := runSolidity(t, recallFixture, "contracts/Token.sol")

	for _, name := range []string{"Token.constructor", "Token.receive", "Token.fallback"} {
		rec := solFind(ents, name, "SCOPE.Operation")
		if rec == nil {
			t.Errorf("%s not extracted", name)
			continue
		}
		if !solContains(ents, "Token", rec) {
			t.Errorf("%s extracted but the contract has no CONTAINS edge to it", name)
		}
	}
}

// TestSolidity_6423_FileLevelDeclarations covers the file-level half of
// defect 3. `type X is Y` is included because it is the one shape whose
// keyword collides with the `type(uint256).max` builtin.
func TestSolidity_6423_FileLevelDeclarations(t *testing.T) {
	ents := runSolidity(t, recallFixture, "contracts/Token.sol")

	for _, want := range []struct{ name, kind string }{
		{"freeHelper", "SCOPE.Operation"},
		{"TopLevelFailure", "SCOPE.Operation"},
		{"Mode", "SCOPE.Schema"},
		{"Entry", "SCOPE.Schema"},
		{"Price", "SCOPE.Schema"},
	} {
		if solFind(ents, want.name, want.kind) == nil {
			t.Errorf("file-level %s [%s] not extracted", want.name, want.kind)
		}
	}
}

// TestSolidity_6423_ContractLevelDeclarations covers the contract-level half
// of defect 3. It is asserted separately on purpose: a file-level-only fix
// scores the test above and leaves this one red, which is exactly the partial
// fix the golden fixture was built to catch.
func TestSolidity_6423_ContractLevelDeclarations(t *testing.T) {
	ents := runSolidity(t, recallFixture, "contracts/Token.sol")

	for _, want := range []struct{ name, kind string }{
		{"Token.TokenEmpty", "SCOPE.Operation"},
		{"Token.Receipt", "SCOPE.Schema"},
		{"Token.Tier", "SCOPE.Schema"},
	} {
		rec := solFind(ents, want.name, want.kind)
		if rec == nil {
			t.Errorf("contract-level %s [%s] not extracted", want.name, want.kind)
			continue
		}
		if !solContains(ents, "Token", rec) {
			t.Errorf("%s extracted but the contract has no CONTAINS edge to it", want.name)
		}
	}
}

// TestSolidity_6423_ModifierUsageIsLinked covers defect 4. Both a modifier
// declared in the same body (`whenOpen`) and one inherited from a base
// contract in another file (`onlyOwner`) are asserted, so a same-body-only fix
// cannot score the class.
func TestSolidity_6423_ModifierUsageIsLinked(t *testing.T) {
	ents := runSolidity(t, recallFixture, "contracts/Token.sol")

	if got := solRawCallTargets(t, ents, "Token.mint"); !slices.Contains(got, "whenOpen") {
		t.Errorf("Token.mint CALLS = %v, want it to include whenOpen", got)
	}
	if got := solRawCallTargets(t, ents, "Token.guarded"); !slices.Contains(got, "onlyOwner") {
		t.Errorf("Token.guarded CALLS = %v, want it to include onlyOwner", got)
	}
}

// TestSolidity_6423_AttributeScanDoesNotMintKeywords is the precision guard on
// defect 4. The attribute section between `)` and `{` holds visibility,
// mutability, `virtual`, `override` and `returns (T)` alongside the modifier
// names; a scan that takes every identifier there mints all of them as call
// targets and the recall number still goes up.
func TestSolidity_6423_AttributeScanDoesNotMintKeywords(t *testing.T) {
	ents := runSolidity(t, recallFixture, "contracts/Token.sol")

	forbidden := []string{"public", "external", "internal", "payable", "pure",
		"view", "virtual", "override", "returns", "uint256", "msg"}
	for _, name := range []string{"Token.mint", "Token.guarded", "Token.receive", "Token.fallback"} {
		if solFind(ents, name, "SCOPE.Operation") == nil {
			continue // reported by the recall test above
		}
		got := solRawCallTargets(t, ents, name)
		for _, bad := range forbidden {
			if slices.Contains(got, bad) {
				t.Errorf("%s CALLS %q — the attribute scan minted a keyword (got %v)", name, bad, got)
			}
		}
	}
}
