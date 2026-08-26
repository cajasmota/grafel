package solidity_test

// Issue #6425, the CALLS **over-fire** arm only.
//
// `collectCallsFromBody` runs two regexes over a scrubbed function body and
// mints a CALLS edge for every hit. `solidityKeywords` is the denylist that
// keeps language tokens out of that stream, and it was missing eight names:
// `returns`, `type`, `catch`, `try`, `unchecked`, `super`, `this`, `abi`.
//
// Measured on the parent commit, not assumed. Three of the eight reach the
// graph today, and they reach it by two different paths:
//
//   - BARE path (`callBareRE`, `\b([a-z_][A-Za-z0-9_]+)\s*\(`): `type(uint256).max`
//     mints `CALLS -> type`; a `try ... returns (uint256 v)` statement mints
//     `CALLS -> returns`; a `catch (bytes memory raw)` clause mints
//     `CALLS -> catch`.
//   - DOTTED path (`callDotRE`): `abi.encode(...)` mints `CALLS -> abi.encode`
//     *and*, because the bare regex also matches the member half of the same
//     expression, a second edge `CALLS -> encode`. `super.f()` / `this.f()`
//     likewise mint `super.f` / `this.f`.
//
// Two of the eight CANNOT fire through the bare path at all, and no test here
// pretends otherwise:
//
//   - `unchecked` is followed by `{`, never `(`, so `callBareRE` can never
//     match it. Its map entry is defence in depth, not a pinned behaviour.
//   - `try` is likewise always followed by an *expression*, never directly by
//     `(` — measured: a full `try/catch` fixture emits `returns` and `catch`
//     but never `try`. (The grounding on the issue named only `unchecked` as
//     unreachable; `try` is a second one, found by running the extractor.)
//
// A fixture asserting either of those is suppressed via the bare path would be
// vacuous — it would pass on the parent commit too — so neither is asserted.
//
// The UNDER-fire arm of #6425 (`callBareRE` requiring a lowercase first
// character, so `IERC20(token).transfer(...)` yields no edge to `IERC20`) is
// deliberately NOT touched here: `solidity-mini/expected.json` marks it
// `nice_to_have` because whether an explicit type conversion *is* a call is a
// design decision this issue still has to make.

import "testing"

// overfireFixture puts every reachable over-fire shape in a DIFFERENT member,
// so `collectCallsFromBody`'s per-body `seen` dedup cannot let one suppression
// mask another.
const overfireFixture = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract Probe is Base {
    uint256 public total;

    function ceiling() public pure returns (uint256) {
        return type(uint256).max;
    }

    function fingerprint(uint256 amount) public view returns (bytes32) {
        return keccak256(abi.encode(amount));
    }

    function risky(address t) public {
        try IThing(t).ping() returns (uint256 v) {
            total = v;
        } catch (bytes memory raw) {
            dump(raw);
        }
        unchecked {
            total = total + 1;
        }
    }

    function delegated() public {
        super.ping();
        this.ceiling();
    }

    function userReceiver(Codec helper) public {
        helper.encode(total);
    }

    function neighbours() public {
        typeOf(1);
        abiEncode(2);
        catcher(3);
        tryHard(4);
        token.transfer(total);
    }

    function qualifierSuffix() public {
        myabi.encode(5);
    }

    function qualifierPrefix() public {
        abicodec.encode(6);
    }
}
`

// TestSolidity_6425_BareKeywordsAreNotCallTargets pins the bare path. Each of
// these three fired on the parent commit and is visible in the emitted graph
// of `internal/quality/golden/solidity-mini` as a `SCOPE.External` node.
func TestSolidity_6425_BareKeywordsAreNotCallTargets(t *testing.T) {
	ents := runSolidity(t, overfireFixture, "Probe.sol")

	for _, tc := range []struct{ caller, target string }{
		{"Probe.ceiling", "type"},  // `type(uint256).max`
		{"Probe.risky", "returns"}, // `try ... returns (uint256 v)`
		{"Probe.risky", "catch"},   // `catch (bytes memory raw)`
	} {
		if solHasRel(ents, tc.caller, "SCOPE.Operation", "CALLS", tc.target) {
			t.Errorf("%s has CALLS -> %q; %q is a language keyword, not an invocation",
				tc.caller, tc.target, tc.target)
		}
	}

	// Positive control for the same body: the real call in `risky` survives.
	if !solHasRel(ents, "Probe.risky", "SCOPE.Operation", "CALLS", "dump") {
		t.Error("Probe.risky lost CALLS -> dump; the keyword suppression is over-broad")
	}
}

// TestSolidity_6425_BuiltinNamespaceAbi pins the dotted path for `abi`, which
// is an OPAQUE builtin namespace: neither `abi.encode` nor the bare `encode`
// that the same expression also yields names anything in the Solidity
// surface. Both
// edges fired on the parent commit; `solidity-mini/NOTICE.md:124` records the
// bare one as a `SCOPE.External` node literally named `encode`.
func TestSolidity_6425_BuiltinNamespaceAbi(t *testing.T) {
	ents := runSolidity(t, overfireFixture, "Probe.sol")

	for _, target := range []string{"abi.encode", "encode"} {
		if solHasRel(ents, "Probe.fingerprint", "SCOPE.Operation", "CALLS", target) {
			t.Errorf("Probe.fingerprint has CALLS -> %q; `abi.encode(...)` is a language builtin", target)
		}
	}
}

// TestSolidity_6425_UserReceiverKeepsBothEdges is the isolation partner of the
// test above: the LEAF is held constant at `encode` and only the RECEIVER root
// varies (`abi` -> `helper`). It fails if the suppression keys on the leaf
// name rather than on the root, which would silently delete every call to a
// user-defined `encode`.
func TestSolidity_6425_UserReceiverKeepsBothEdges(t *testing.T) {
	ents := runSolidity(t, overfireFixture, "Probe.sol")

	for _, target := range []string{"helper.encode", "encode"} {
		if !solHasRel(ents, "Probe.userReceiver", "SCOPE.Operation", "CALLS", target) {
			t.Errorf("Probe.userReceiver lost CALLS -> %q; `helper` is a user receiver, not a keyword", target)
		}
	}
}

// TestSolidity_6425_TransparentReceivers pins `super` and `this`, which are
// the OPPOSITE case to `abi`: the dotted target names nothing (there is no
// entity `super`), but the member it reaches IS a real function, so the bare
// leaf must survive. Suppressing the leaf here would trade an over-fire for an
// under-fire.
func TestSolidity_6425_TransparentReceivers(t *testing.T) {
	ents := runSolidity(t, overfireFixture, "Probe.sol")

	for _, target := range []string{"super.ping", "this.ceiling"} {
		if solHasRel(ents, "Probe.delegated", "SCOPE.Operation", "CALLS", target) {
			t.Errorf("Probe.delegated has CALLS -> %q; `super`/`this` are keywords, not entities", target)
		}
	}
	for _, target := range []string{"ping", "ceiling"} {
		if !solHasRel(ents, "Probe.delegated", "SCOPE.Operation", "CALLS", target) {
			t.Errorf("Probe.delegated lost CALLS -> %q; the member behind `super.`/`this.` is a real call", target)
		}
	}
}

// TestSolidity_6425_NeighbouringIdentifiersStillCall guards the whole-word
// boundary, on BOTH sides of the identifier. A denylist that matched by prefix
// would delete four perfectly ordinary user calls (`typeOf`, `abiEncode`,
// `catcher`, `tryHard`); a qualifier check that matched by suffix rather than
// walking back to the start of the identifier would delete `myabi.encode`,
// which is the mirror image and the one a `strings.HasSuffix` shortcut gets
// wrong; and a root check that matched by prefix would delete
// `token.transfer`.
func TestSolidity_6425_NeighbouringIdentifiersStillCall(t *testing.T) {
	ents := runSolidity(t, overfireFixture, "Probe.sol")

	for _, target := range []string{
		"typeOf",    // `type` + suffix
		"abiEncode", // `abi` + suffix
		"catcher",   // `catch` + suffix
		"tryHard",   // `try` + suffix
		"token.transfer",
		"transfer",
	} {
		if !solHasRel(ents, "Probe.neighbours", "SCOPE.Operation", "CALLS", target) {
			t.Errorf("Probe.neighbours lost CALLS -> %q; keyword suppression must be whole-word", target)
		}
	}
}

// TestSolidity_6425_QualifierIsMatchedWholeIdentifier is the boundary guard for
// `solQualifierBefore` specifically, and it exists because two weaker versions
// of it were measured to be useless.
//
// The qualifier check keys on the identifier BEFORE the dot, so the shapes that
// break it are a qualifier that merely ends with `abi` (`myabi.encode`) and one
// that merely starts with it (`abicodec.encode`). Both must be asserted on the
// BARE half of the expression, not the dotted half: a `strings.HasSuffix`
// qualifier check drops `encode` and leaves `myabi.encode` untouched, so a test
// naming only the dotted target scores nothing.
//
// They must also sit in SEPARATE members. `collectCallsFromBody` keeps one
// `seen` set per body, so a single `encode` edge satisfies the assertion no
// matter which of the two expressions produced it — measured: with both calls
// in one body, the HasSuffix mutant SURVIVED, because the `abicodec` line
// re-added the `encode` the `myabi` line had lost.
func TestSolidity_6425_QualifierIsMatchedWholeIdentifier(t *testing.T) {
	ents := runSolidity(t, overfireFixture, "Probe.sol")

	for _, tc := range []struct{ caller, dotted string }{
		{"Probe.qualifierSuffix", "myabi.encode"},    // qualifier ENDS in `abi`
		{"Probe.qualifierPrefix", "abicodec.encode"}, // qualifier STARTS with `abi`
	} {
		for _, target := range []string{tc.dotted, "encode"} {
			if !solHasRel(ents, tc.caller, "SCOPE.Operation", "CALLS", target) {
				t.Errorf("%s lost CALLS -> %q; the qualifier check must match a WHOLE identifier, not a prefix or suffix of one",
					tc.caller, target)
			}
		}
	}
}
