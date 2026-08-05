package solidity_test

import (
	"fmt"
	"slices"
	"testing"
)

// Issue #6177 — a CALLS edge to a state variable that is not declared `public`
// describes no call in the source, because no getter is synthesised for it.
//
// This file exists because the resolver reads that visibility out of the
// declaration text #6137 keeps in Signature, so the rule is only as good as
// what declSignature produces. The unit tests in internal/resolve feed that
// text by hand; these feed real Solidity through findStateVariables and
// declSignature first, so a change to either shows up here.
//
// solResolve / solCallTargets are the #6135 harness (see
// issue6135_state_variables_test.go); reused deliberately, since #6135 is what
// made these fields eligible for the leaf-name tier at all.

func solVisibilityFieldSrc(declaration string) string {
	return fmt.Sprintf(`// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract Pool {
    %s;
}
`, declaration)
}

// TestSolidity_NonPublicStateVariableCallDangles_6177 walks the declaration
// forms declSignature has to get right. The `internal` and omitted forms get no
// getter, so the bare leaf stub must stay dangling; the three `public` forms do
// get one, so it must still bind.
//
// The dotted `pool.factory` stub dangles throughout, unchanged: it only ever
// matches an entity of that exact name and is not what this rule touches.
func TestSolidity_NonPublicStateVariableCallDangles_6177(t *testing.T) {
	for _, tc := range []struct {
		declaration string
		want        []string
	}{
		{
			// `private` reaches this through the identical path — extractor.go:98
			// blocklists every visibility keyword alike and the predicate is one
			// `\bpublic\b` match — so it is a predicate-table row instead.
			declaration: "address internal factory",
			want:        []string{"<dangling: factory>", "<dangling: pool.factory>"},
		},
		{
			// No visibility keyword: Solidity defaults a state variable to
			// internal, so there is no getter here either.
			declaration: "address factory",
			want:        []string{"<dangling: factory>", "<dangling: pool.factory>"},
		},
		{
			declaration: "address public factory",
			want: []string{
				"<dangling: pool.factory>",
				"Pool.factory [SCOPE.Schema/field] @ contracts/Pool.sol",
			},
		},
		{
			// `public constant` and `public immutable` DO get getters. The
			// constant form also carries an initialiser into Signature, which
			// must not disturb the keyword test.
			declaration: "address public constant factory = address(0)",
			want: []string{
				"<dangling: pool.factory>",
				"Pool.factory [SCOPE.Schema/field] @ contracts/Pool.sol",
			},
		},
		{
			declaration: "address public immutable factory",
			want: []string{
				"<dangling: pool.factory>",
				"Pool.factory [SCOPE.Schema/field] @ contracts/Pool.sol",
			},
		},
	} {
		t.Run(tc.declaration, func(t *testing.T) {
			got := solCallTargets(solResolve(t, map[string]string{
				"contracts/Pool.sol":     solVisibilityFieldSrc(tc.declaration),
				"contracts/Consumer.sol": solConsumerSrc,
			}), "Consumer.poke")
			if !slices.Equal(got, tc.want) {
				t.Errorf("`%s` — Consumer.poke CALLS targets =\n  %v\nwant\n  %v",
					tc.declaration, got, tc.want)
			}
		})
	}
}
