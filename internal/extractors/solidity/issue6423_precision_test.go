package solidity_test

// Issue #6423, review follow-up. The recall work in #6423 opened two new ways
// for the extractor to mint an entity that names nothing in the source. Both
// are regressions introduced by that change — neither shape produced anything
// at all on the parent commit, because neither code path existed — so both are
// pinned here rather than left to #6425, which covers the phantoms that
// predate #6423.
//
//  1. An UNTERMINATED contract. findContracts records the byte span of every
//     contract so extractSolidity can mask it out before the file-level scan.
//     When extractBracedBody finds no closing '}' it returns an empty body, so
//     the span degenerated to the declaration header and the mask left the
//     entire contract body VISIBLE. findFileLevelDecls then emitted every
//     member of that contract as a bare, top-level entity with no CONTAINS
//     owner. Measured: `contract Broken {` with no closing brace yielded
//     `SCOPE.Operation inside` at file level.
//
//  2. A NESTED STATEMENT that happens to start a line. specialFunctionRE and
//     constructorRE were matched against the whole contract body, nested
//     function bodies included, and they are line-anchored rather than
//     scope-aware. A `fallback();` statement inside another function's body
//     therefore minted `SCOPE.Operation Weird.fallback` plus a CONTAINS edge
//     from the contract. findStateVariables already tracked brace depth for
//     exactly this reason; the member scans now do too.
//
// The third test here pins the property the masking exists to provide in the
// first place. Neutering maskRanges is caught today only by an unrelated
// #6135 state-variable test; nothing in the #6423 suite asserted that a
// contract member is ABSENT at file level.

import "testing"

// unterminatedContractFixture is a contract whose closing brace is missing —
// a truncated file, a bad merge, or a file mid-edit. Every member shape the
// file-level scan knows how to emit is inside it, so a mask that stops at the
// header leaks one of each.
const unterminatedContractFixture = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract Broken {
    uint256 public cap;

    error BrokenEmpty();

    struct Entry {
        address owner;
    }

    enum Mode {
        Open,
        Closed
    }

    type Price is uint128;

    function inside() public returns (uint256) {
        return cap;
    }
`

// TestSolidity_6423_UnterminatedContractLeaksNoFileLevelEntities pins fix 1.
// The assertion is on ABSENCE of the bare names, because the bare name is the
// tell: a genuine member is emitted as `Broken.inside`, and a leak is emitted
// as `inside` with no owner at all. Asserted for each of the five shapes the
// file-level scan can emit, so a fix that only guards the function half is
// still red.
func TestSolidity_6423_UnterminatedContractLeaksNoFileLevelEntities(t *testing.T) {
	ents := runSolidity(t, unterminatedContractFixture, "contracts/Broken.sol")

	for _, want := range []struct{ name, kind string }{
		{"inside", "SCOPE.Operation"},
		{"BrokenEmpty", "SCOPE.Operation"},
		{"Entry", "SCOPE.Schema"},
		{"Mode", "SCOPE.Schema"},
		{"Price", "SCOPE.Schema"},
	} {
		if got := solFind(ents, want.name, want.kind); got != nil {
			t.Errorf("phantom file-level %s [%s] at L%d-%d — the unterminated contract's body was not masked",
				want.name, want.kind, got.StartLine, got.EndLine)
		}
	}

	// Nothing but the file carrier and the contract itself may survive: a
	// member of an unterminated contract has no measurable span, so the
	// extractor emits none. Stated as a whole-set assertion so a shape this
	// test does not name by hand cannot leak either.
	for i := range ents {
		if ents[i].Subtype == "file" || ents[i].Name == "Broken" {
			continue
		}
		t.Errorf("unexpected entity %q [%s/%s] from an unterminated contract",
			ents[i].Name, ents[i].Kind, ents[i].Subtype)
	}
}

// nestedStatementFixture puts a `fallback();` and a `receive();` call at the
// start of a line inside another function's body. Both are ordinary
// statements — dispatching to this contract's own fallback is legal Solidity
// — and neither is a declaration.
const nestedStatementFixture = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract Weird {
    function caller() public {
        fallback();
        receive();
    }

    fallback() external payable {
        revert();
    }
}
`

// TestSolidity_6423_NestedStatementMintsNoMember pins fix 2, bidirectionally:
// the nested statements must mint nothing, AND the real `fallback()` member
// declared at contract level must still be emitted with its CONTAINS edge, so
// deleting specialFunctionRE outright cannot pass.
func TestSolidity_6423_NestedStatementMintsNoMember(t *testing.T) {
	ents := runSolidity(t, nestedStatementFixture, "contracts/Weird.sol")

	// `receive` is only ever a statement here — it has no declaration — so
	// any entity for it is a phantom.
	if got := solFind(ents, "Weird.receive", "SCOPE.Operation"); got != nil {
		t.Errorf("phantom Weird.receive at L%d-%d — a nested `receive();` statement minted a member",
			got.StartLine, got.EndLine)
	}

	// `fallback` IS declared, exactly once, at line 10. A second record (or a
	// record starting at line 7, the nested statement) is the leak.
	var fallbacks []int
	for i := range ents {
		if ents[i].Name == "Weird.fallback" && ents[i].Kind == "SCOPE.Operation" {
			fallbacks = append(fallbacks, ents[i].StartLine)
		}
	}
	if len(fallbacks) != 1 || fallbacks[0] != 10 {
		t.Errorf("Weird.fallback records start at lines %v, want exactly [10] (the declaration)", fallbacks)
	}

	// The contract must carry exactly one CONTAINS edge per real member —
	// `caller` and `fallback`. The phantom arrived with an edge of its own.
	owner := solFind(ents, "Weird", "SCOPE.Component")
	if owner == nil {
		t.Fatal("contract Weird not extracted")
	}
	var contains []string
	for _, r := range owner.Relationships {
		if r.Kind == "CONTAINS" {
			contains = append(contains, r.ToID)
		}
	}
	if len(contains) != 2 {
		t.Errorf("Weird has %d CONTAINS edges, want 2 (caller, fallback): %v", len(contains), contains)
	}
	if solFind(ents, "Weird.caller", "SCOPE.Operation") == nil {
		t.Error("Weird.caller not extracted — the member scan lost a real declaration")
	}
}

// TestSolidity_6423_ContractMembersAreNotAlsoFileLevel is the direct assertion
// on what maskRanges exists to guarantee. #6423 added the file-level scan and
// the mask together, and the mask's only killing test was a pre-existing #6135
// state-variable case — an incidental pin. This asserts the property itself,
// over one member of each shape the file-level scan can emit, using the same
// recallFixture the recall tests use so a change that scores those cannot
// quietly double-emit here.
func TestSolidity_6423_ContractMembersAreNotAlsoFileLevel(t *testing.T) {
	ents := runSolidity(t, recallFixture, "contracts/Token.sol")

	for _, bare := range []struct{ name, kind string }{
		{"mint", "SCOPE.Operation"}, // function member
		{"constructor", "SCOPE.Operation"},
		{"TokenEmpty", "SCOPE.Operation"}, // error member
		{"Receipt", "SCOPE.Schema"},       // struct member
		{"Tier", "SCOPE.Schema"},          // enum member
		{"cap", "SCOPE.Schema"},           // state variable
		{"Minted", "SCOPE.Operation"},     // event member
		{"whenOpen", "SCOPE.Operation"},   // modifier member
	} {
		if got := solFind(ents, bare.name, bare.kind); got != nil {
			t.Errorf("contract member %q also emitted bare at file level (L%d) — the contract body was not masked",
				bare.name, got.StartLine)
		}
	}

	// The qualified forms must still be there; otherwise "absent at file
	// level" is satisfied by extracting nothing at all.
	for _, qual := range []struct{ name, kind string }{
		{"Token.mint", "SCOPE.Operation"},
		{"Token.TokenEmpty", "SCOPE.Operation"},
		{"Token.Receipt", "SCOPE.Schema"},
		{"Token.Tier", "SCOPE.Schema"},
		{"Token.cap", "SCOPE.Schema"},
	} {
		if solFind(ents, qual.name, qual.kind) == nil {
			t.Errorf("%s [%s] missing — the mask swallowed a real member", qual.name, qual.kind)
		}
	}

	// And the genuinely file-level declarations must survive the mask.
	for _, free := range []struct{ name, kind string }{
		{"freeHelper", "SCOPE.Operation"},
		{"TopLevelFailure", "SCOPE.Operation"},
		{"Entry", "SCOPE.Schema"},
	} {
		if solFind(ents, free.name, free.kind) == nil {
			t.Errorf("file-level %s [%s] missing — the mask covered more than the contract", free.name, free.kind)
		}
	}
}
