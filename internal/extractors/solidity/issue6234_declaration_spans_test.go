package solidity_test

// Issue #6234: extractBracedBody scanned forward for the next '{' with no
// bound, so a bodiless declaration returned the FOLLOWING member's body. That
// body supplied the bodiless declaration's EndLine and, through
// collectCallsFromBody, its CALLS edges. A second defect in the same lines
// computed EndLine from the body's newline count added to the declaration's
// start line, which drops every newline inside the declaration itself.

import (
	"slices"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// spanFixture line 1 is the SPDX comment. It carries one of each shape that
// used to be attributed wrongly: a bodiless function and a bodiless modifier
// each followed by a bodied member, a function whose signature wraps, and an
// event whose parameter list wraps. `last` is bodied and comes last, so a
// regression that widens a span has somewhere to run to.
const spanFixture = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

abstract contract Base {
    event Multi(
        address indexed who,
        uint256 amount
    );

    function mustOverride(uint256 a) public virtual returns (uint256);

    function implemented() public {
        transferSomething();
        emitSomething();
    }

    modifier bodiless() virtual;

    modifier withBody() {
        require(true);
        _;
    }

    function wrappedSignature(
        uint256 a,
        address b
    )
        public
        returns (uint256)
    {
        doWork();
        return a;
    }

    function last() public {
        doLast();
    }
}
`

// TestSolidity_DeclarationSpans (#6234) pins EndLine for every member of
// spanFixture. Nothing in this package asserted EndLine on a function, event
// or modifier before, which is how both defects survived;
// TestSolidity_LineAttribution pins StartLine only.
func TestSolidity_DeclarationSpans(t *testing.T) {
	ents := runSolidity(t, spanFixture, "contracts/Base.sol")

	for _, tc := range []struct {
		name    string
		subtype string
		start   int
		end     int
	}{
		{"Base.Multi", "event", 5, 8},
		// Bodiless: the span is the declaration, ending at its ';'. Before
		// the fix this read 13, the closing brace of `implemented`.
		{"Base.mustOverride", "function", 10, 10},
		{"Base.implemented", "function", 12, 15},
		{"Base.bodiless", "modifier", 17, 17},
		{"Base.withBody", "modifier", 19, 22},
		// The declaration wraps over six lines before its '{'. Before the fix
		// this read 27, being the start line plus the body's newlines alone.
		{"Base.wrappedSignature", "function", 24, 33},
		{"Base.last", "function", 35, 37},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ent := solFindSubtype(ents, tc.name, "SCOPE.Operation", tc.subtype)
			if ent == nil {
				t.Fatalf("no %s entity named %q", tc.subtype, tc.name)
			}
			if ent.StartLine != tc.start {
				t.Errorf("StartLine = %d, want %d", ent.StartLine, tc.start)
			}
			if ent.EndLine != tc.end {
				t.Errorf("EndLine = %d, want %d", ent.EndLine, tc.end)
			}
		})
	}
}

// TestSolidity_BodilessDeclarationMakesNoCalls (#6234): the phantom CALLS
// edges were the sharper half of the defect. A bodiless declaration collected
// the next member's calls, additively, so one call site produced two CALLS
// records on two different functions. Asserted bidirectionally: the bodiless
// member must have none AND the bodied one must keep its own, so deleting the
// edges outright cannot pass this.
func TestSolidity_BodilessDeclarationMakesNoCalls(t *testing.T) {
	ents := runSolidity(t, spanFixture, "contracts/Base.sol")

	for _, tc := range []struct {
		name    string
		subtype string
		want    []string
	}{
		{"Base.mustOverride", "function", nil},
		{"Base.implemented", "function", []string{"emitSomething", "transferSomething"}},
		{"Base.wrappedSignature", "function", []string{"doWork"}},
		{"Base.last", "function", []string{"doLast"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ent := solFindSubtype(ents, tc.name, "SCOPE.Operation", tc.subtype)
			if ent == nil {
				t.Fatalf("no %s entity named %q", tc.subtype, tc.name)
			}
			if got := solRawCallees(ent); !slices.Equal(got, tc.want) {
				t.Errorf("CALLS = %v, want %v", got, tc.want)
			}
		})
	}
}

// solRawCallees returns the callee names on the pre-resolve CALLS edges of one
// entity, sorted, since nothing promises source order. The existing
// solCallTargets describes what edges BOUND to and so needs resolved records.
func solRawCallees(ent *types.EntityRecord) []string {
	var out []string
	for _, rel := range ent.Relationships {
		if rel.Kind == "CALLS" {
			out = append(out, rel.ToID)
		}
	}
	slices.Sort(out)
	return out
}
