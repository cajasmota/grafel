package solidity_test

// Issue #6425, the two arms left open after #6699 shipped the Solidity-keyword
// over-fire: the Yul arm, and the CALLS under-fire on capitalised targets.
//
// Every claim below was measured on the parent commit (`278301296`) with the
// extractor, not read off the issue.
//
// ── What the issue gets right, and what it gets wrong ────────────────────────
//
// The issue's headline — "a Yul function inside `assembly { }` is emitted as a
// contract operation" — is NO LONGER TRUE. `braceDepths`/`isMemberPos`
// (extractor.go:557) already gate member scanning to brace-depth zero, so
// `function helper(a) -> b` two braces deep is not a member. That landed as a
// side effect of #6423 (`7a599bbc8`) and is pinned here as a regression guard,
// declared as such rather than as this change's work.
//
// What IS still live is the same defect one layer down: `collectCallsFromBody`
// is depth-blind, so the CALLS scan runs straight through `assembly { … }`.
// Measured on the parent commit, this body:
//
//	assembly {
//	    function helper(a) -> b { b := add(a, 1) }
//	    let p := mload(0x40)
//	    mstore(p, sload(0))
//	    sstore(0, helper(p))
//	}
//
// emits SIX CALLS edges — `helper`, `add`, `mload`, `mstore`, `sload`,
// `sstore` — none of which names a Solidity entity. `helper` is a Yul-local
// function; the other five are EVM opcodes. So the Yul function still reaches
// the graph, just as a dangling call target rather than as an operation.
//
// The issue's example builtin set is also partly stale: `keccak256` is already
// suppressed (it is in `solidityKeywords`), while `mstore`/`sload`/`mload`/
// `sstore`/`add` are not. Only the Yul half of that list is live.
//
// ── Why exclusion, and not a builtin denylist ────────────────────────────────
//
// The fix blanks `assembly { … }` blocks out of the CALLS scan entirely rather
// than adding Yul opcodes to `solidityKeywords`. Two reasons, both structural:
//
//  1. Yul is a separate language nested inside Solidity, and Yul CANNOT call a
//     Solidity function — inline assembly reaches Solidity variables and
//     `.selector`/`.address`, never a function body. So there is no true
//     Solidity CALLS edge inside an assembly block to lose. Excluding the
//     block is therefore lossless, which a denylist is not obliged to be.
//  2. A denylist of opcodes is exactly the hand-typed list that goes stale:
//     Yul's builtin set is versioned and grows with every EVM fork
//     (`tload`/`tstore` arrived in Cancun, `mcopy` with it). Exclusion needs no
//     list at all, so there is nothing to keep in sync with a reference.
//
// The Yul-local `helper` falls out of the same rule, which the denylist
// approach could never have caught: it is a user-written name, so no builtin
// list contains it. It is now emitted as NOTHING — not an operation, not a
// call target. That is the correct answer: a Yul-local function is scoped to
// its assembly block, is not addressable from Solidity, and has no ABI
// presence, so there is no cross-entity relationship for the graph to hold.
//
// ── The under-fire arm, and the discriminator it uses ────────────────────────
//
// `callBareRE` (`\b([a-z_][A-Za-z0-9_]+)\s*\(`) requires a lowercase first
// character, so `IERC20(token).transfer(…)` yields no edge to `IERC20`.
// Widening that class to `[A-Za-z_]` was measured on this issue and REJECTED:
// it recovers `IERC20` but also mints `Point(1,2)` (struct construction),
// `emit Transfer(…)` and `revert InsufficientBalance(…)` as calls.
//
// The discriminator used instead is receiver context — one of the three the
// issue's own measurement named as viable. A capitalised name is a call target
// only in the form `Name(expr).member(…)`: the parenthesised group must be
// followed by `.`. That is precisely the explicit-type-conversion-then-call
// shape, and it excludes all three over-fire shapes structurally, because a
// struct literal, an `emit` and a `revert` are each a complete statement — no
// `.` follows.
//
// Answering the design question this issue left open: an explicit type
// conversion in receiver position IS recorded as a call. Every builtin
// Solidity type name is lowercase (`uint256`, `bytes32`, `address`), so a
// CAPITALISED receiver-form conversion always names a user-defined contract,
// interface or library — the cross-contract dependency is the whole point of
// the edge. The lowercase case (`uint256(v)`) is untouched and stays
// undecided; nothing here settles it.
//
// Both directions are pinned below, in SEPARATE members, because
// `collectCallsFromBody` keeps a per-body `seen` set and co-locating cases lets
// one mask another.

import "testing"

const yulCapFixture = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract Yard {
    uint256 public total;

    function yulOnly(uint256 x) public {
        assembly {
            function helper(a) -> b { b := add(a, 1) }
            let p := mload(0x40)
            mstore(p, sload(0))
            sstore(0, helper(p))
        }
    }

    function yulSandwich(uint256 x) public {
        preStep(x);
        assembly {
            mstore(0x40, sload(0))
        }
        postStep(x);
    }

    function yulFlagged() public {
        assembly ("memory-safe") {
            mstore(0x40, shr(1, total))
        }
    }

    function receiverForm(address token) public {
        IERC20(token).transfer(msg.sender, total);
    }

    function structLiteral() public {
        Point memory p = Point(1, 2);
        total = p.x;
    }

    function eventEmit() public {
        emit Transfer(total);
    }

    function customError() public {
        revert InsufficientBalance(total);
    }

    function newContract() public {
        Widget w = new Widget(total);
        total = w.id;
    }

    function dottedCapitalisedLeaf() public {
        Registry.Wrap(total).ping();
    }
}
`

// TestSolidity_6425_YulBodiesAreNotSolidityCalls is the over-fire arm. All six
// targets fired on the parent commit.
func TestSolidity_6425_YulBodiesAreNotSolidityCalls(t *testing.T) {
	ents := runSolidity(t, yulCapFixture, "Yard.sol")

	for _, target := range []string{"helper", "add", "mload", "mstore", "sload", "sstore"} {
		if solHasRel(ents, "Yard.yulOnly", "SCOPE.Operation", "CALLS", target) {
			t.Errorf("Yard.yulOnly has CALLS -> %q; that name is Yul, not Solidity", target)
		}
	}
}

// TestSolidity_6425_YulExclusionKeepsSurroundingCalls is the isolation partner:
// the assembly block is blanked, but the Solidity statements on either side of
// it are not. It fails if the exclusion runs past the block's closing brace or
// starts before its keyword.
func TestSolidity_6425_YulExclusionKeepsSurroundingCalls(t *testing.T) {
	ents := runSolidity(t, yulCapFixture, "Yard.sol")

	for _, target := range []string{"preStep", "postStep"} {
		if !solHasRel(ents, "Yard.yulSandwich", "SCOPE.Operation", "CALLS", target) {
			t.Errorf("Yard.yulSandwich lost CALLS -> %q; the assembly exclusion is over-broad", target)
		}
	}
	// The block between them is still excluded — otherwise this test would
	// pass with no exclusion at all.
	for _, target := range []string{"mstore", "sload"} {
		if solHasRel(ents, "Yard.yulSandwich", "SCOPE.Operation", "CALLS", target) {
			t.Errorf("Yard.yulSandwich has CALLS -> %q; the assembly block was not excluded", target)
		}
	}
}

// TestSolidity_6425_FlaggedAssemblyIsExcluded pins `assembly ("memory-safe")`,
// whose flag list is the one form where the `assembly` KEYWORD itself is
// followed by `(` and so matches callBareRE. On the parent commit this member
// emitted `CALLS -> assembly` alongside the opcodes.
func TestSolidity_6425_FlaggedAssemblyIsExcluded(t *testing.T) {
	ents := runSolidity(t, yulCapFixture, "Yard.sol")

	for _, target := range []string{"assembly", "mstore", "shr"} {
		if solHasRel(ents, "Yard.yulFlagged", "SCOPE.Operation", "CALLS", target) {
			t.Errorf("Yard.yulFlagged has CALLS -> %q; a flagged assembly block is still Yul", target)
		}
	}
}

// TestSolidity_6425_YulFunctionIsNotAMember is a REGRESSION guard for work that
// already landed (#6423, `7a599bbc8`), not a pin for this change. It passed on
// the parent commit and is declared as such rather than presented as coverage
// this change earned.
func TestSolidity_6425_YulFunctionIsNotAMember(t *testing.T) {
	ents := runSolidity(t, yulCapFixture, "Yard.sol")

	if e := solFind(ents, "Yard.helper", "SCOPE.Operation"); e != nil {
		t.Error("Yard.helper is emitted as a contract operation; a Yul function is not a contract member")
	}
	if solHasRelPartial(ents, "Yard", "SCOPE.Component", "CONTAINS", "Yard.helper") {
		t.Error("Yard CONTAINS Yard.helper; a Yul function is not a contract member")
	}
	// Positive control: the enclosing Solidity function IS a member, so an
	// empty entity list cannot green this test.
	if e := solFind(ents, "Yard.yulOnly", "SCOPE.Operation"); e == nil {
		t.Fatal("Yard.yulOnly is missing; the fixture did not extract")
	}
}

// TestSolidity_6425_CapitalisedReceiverIsACall is the under-fire arm.
func TestSolidity_6425_CapitalisedReceiverIsACall(t *testing.T) {
	ents := runSolidity(t, yulCapFixture, "Yard.sol")

	if !solHasRel(ents, "Yard.receiverForm", "SCOPE.Operation", "CALLS", "IERC20") {
		t.Error("Yard.receiverForm has no CALLS -> IERC20; a capitalised receiver-form conversion names a user-defined type")
	}
	// The member reached through it must survive unchanged.
	if !solHasRel(ents, "Yard.receiverForm", "SCOPE.Operation", "CALLS", "transfer") {
		t.Error("Yard.receiverForm lost CALLS -> transfer")
	}
}

// TestSolidity_6425_CapitalisedNonReceiversAreNotCalls is the negative half of
// the same change, and the reason a bare character-class widening is not the
// fix. Each shape lives in its own member so the per-body `seen` set cannot let
// one mask another. All three FIRE under `[A-Za-z_]`; none may fire here.
func TestSolidity_6425_CapitalisedNonReceiversAreNotCalls(t *testing.T) {
	ents := runSolidity(t, yulCapFixture, "Yard.sol")

	for _, tc := range []struct{ caller, target, why string }{
		{"Yard.structLiteral", "Point", "struct construction"},
		{"Yard.eventEmit", "Transfer", "event emission"},
		{"Yard.customError", "InsufficientBalance", "custom error revert"},
		{"Yard.newContract", "Widget", "`new` contract creation"},
	} {
		if solHasRel(ents, tc.caller, "SCOPE.Operation", "CALLS", tc.target) {
			t.Errorf("%s has CALLS -> %q; %s is not an invocation of %q",
				tc.caller, tc.target, tc.why, tc.target)
		}
	}
}

// TestSolidity_6425_DottedCapitalisedLeafIsNotDuplicated pins the one guard in
// the new capitalised loop that no other case reaches: a capitalised name in
// receiver position may also be the MEMBER half of a dotted expression, as in
// `Registry.Wrap(total).ping()`. callDotRE already emits `Registry.Wrap` with
// the root keyword checks applied; letting the capitalised loop emit a bare
// `Wrap` beside it is exactly the double-edge phantom that solBuiltinNamespaces
// was added to stop for `abi.encode`.
//
// Written because the guard was measured ALIVE: dropping the
// `solQualifierBefore` skip left the whole package green AND the solidity-mini
// grade unchanged at 14/14 with 0 forbidden hits and the same 142 edges, so
// nothing in reach observed it. The fixture source contains no
// `Type.Member(x).call()` shape, which is why the CLI could not see it either.
func TestSolidity_6425_DottedCapitalisedLeafIsNotDuplicated(t *testing.T) {
	ents := runSolidity(t, yulCapFixture, "Yard.sol")

	if solHasRel(ents, "Yard.dottedCapitalisedLeaf", "SCOPE.Operation", "CALLS", "Wrap") {
		t.Error("Yard.dottedCapitalisedLeaf has a bare CALLS -> Wrap; the dotted `Registry.Wrap` already names it")
	}
	// Positive control on the same expression: the dotted edge and the member
	// reached through it both survive, so this cannot be greened by dropping
	// the whole expression.
	for _, target := range []string{"Registry.Wrap", "ping"} {
		if !solHasRel(ents, "Yard.dottedCapitalisedLeaf", "SCOPE.Operation", "CALLS", target) {
			t.Errorf("Yard.dottedCapitalisedLeaf lost CALLS -> %q", target)
		}
	}
}
