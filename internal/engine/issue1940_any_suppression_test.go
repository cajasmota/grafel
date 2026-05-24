// Tests for #1940 ANY-verb suppression and #2024 process-flow helpers.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

func TestIsPascalCaseIdentifier(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"TransferReceive", true},
		{"PaymentForm", true},
		{"App", true},
		{"confirmTransfer", false}, // camelCase, not PascalCase
		{"CONSTANT_STYLE", false},  // ALL_CAPS — no lowercase
		{"_private", false},        // leading underscore
		{"", false},                // empty
		{"A", false},               // single uppercase char, no lowercase
	}
	for _, c := range cases {
		got := isPascalCaseIdentifier(c.name)
		if got != c.want {
			t.Errorf("isPascalCaseIdentifier(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsJSXSourceFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"src/components/TransferReceive.jsx", true},
		{"src/pages/Checkout.tsx", true},
		{"src/utils/helpers.ts", false},
		{"src/index.js", false},
		{"TransferReceive.JSX", false}, // case-sensitive
	}
	for _, c := range cases {
		got := isJSXSourceFile(c.path)
		if got != c.want {
			t.Errorf("isJSXSourceFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsEntryCandidate_IncludesSCOPEJSX(t *testing.T) {
	// SCOPE.JSX must now be recognized as an entry candidate (fix #2024).
	if !isEntryCandidate(&graph.Entity{Kind: "SCOPE.JSX"}) {
		t.Error("isEntryCandidate: SCOPE.JSX should be accepted after #2024 fix")
	}
	// Prior kinds must still be accepted.
	for _, k := range []string{"SCOPE.Function", "SCOPE.Operation", "SCOPE.Component", "SCOPE.Class"} {
		if !isEntryCandidate(&graph.Entity{Kind: k}) {
			t.Errorf("isEntryCandidate: %q should remain accepted", k)
		}
	}
	// Non-entry kinds should remain rejected.
	for _, k := range []string{"Route", "http_endpoint_definition", "SCOPE.Schema"} {
		if isEntryCandidate(&graph.Entity{Kind: k}) {
			t.Errorf("isEntryCandidate: %q should remain rejected", k)
		}
	}
}
