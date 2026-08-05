package extractor

import (
	"strings"
	"testing"
)

// Issue #6122 — the producer-side bound on the neo4j node-label ref.
//
// The paired resolver measurements live in
// internal/resolve.TestNeo4jNodeLocationRefBindsTheNodeEntity6122; this file
// tests the helper DIRECTLY and in-package, because an end-to-end assertion
// cannot see a refusal that never had a chance to fire (the #6123 lesson: a
// guard covered by a second mechanism looks alive when it is not).

func TestNeo4jNodeTargetIDShape6122(t *testing.T) {
	got := Neo4jNodeTargetID("store/store.go", "Movie")
	const want = "scope:schema:store/store.go#node:Movie"
	if got != want {
		t.Fatalf("Neo4jNodeTargetID = %q, want %q", got, want)
	}
	// The ref must address the entity by the Name the extractor actually mints.
	if !strings.HasSuffix(got, "#"+Neo4jNodeName("Movie")) {
		t.Fatalf("ref %q does not end in the minted entity Name %q — the two must not "+
			"be able to drift apart, which is why both come from this file",
			got, Neo4jNodeName("Movie"))
	}
}

// TestNeo4jNodeTargetIDStaysUnderTheStructuralCeiling6122 is the assertion that
// makes the #6123 trap unreachable: a well-formed ref must not reach
// stubScopeSegments = 6, where lookupStructural stops rejecting and starts
// parsing as Format A.
func TestNeo4jNodeTargetIDStaysUnderTheStructuralCeiling6122(t *testing.T) {
	for _, tc := range []struct{ path, label string }{
		{"store/store.go", "Movie"},
		{"a.go", "X"},
		{"deeply/nested/pkg/graph/queries.go", "PersonProfileNode"},
	} {
		ref := Neo4jNodeTargetID(tc.path, tc.label)
		if ref == "" {
			t.Fatalf("Neo4jNodeTargetID(%q, %q) refused a colon-free input", tc.path, tc.label)
		}
		// Mirrors strings.SplitN(stub, ":", 6) in internal/resolve/refs.go:2037.
		if n := strings.Count(ref, ":") + 1; n >= 6 {
			t.Fatalf("ref %q has %d colon-delimited segments; at 6 the resolver parses it "+
				"as Format A with parts[4] as a file path and parts[5] as an entity name — "+
				"the #6122/#6123 mis-bind", ref, n)
		}
	}
}

// TestNeo4jNodeTargetIDRefusesColonBearingInput6122 pins the refusal itself. A
// deliberately colon-heavy label and a colon-bearing file path are BOTH refused,
// and each is checked on its own so one arm cannot silently cover the other.
func TestNeo4jNodeTargetIDRefusesColonBearingInput6122(t *testing.T) {
	for _, tc := range []struct {
		name, path, label string
	}{
		// Two colons in the path is exactly six segments — the hazard.
		{"path with two colons", "a:b:c/store.go", "Movie"},
		// One colon is only five, but it is refused too: the ceiling is not
		// recomputed at each call site, and nothing legitimate is lost.
		{"path with one colon", "weird:dir/store.go", "Movie"},
		// The colon-heavy label the issue asks for by name. Unreachable from
		// live extraction (every caller's label comes from a regex bounded to
		// [A-Za-z_]\w*) — refused anyway so it stays unreachable.
		{"colon-heavy label", "store/store.go", "My:Weird:Back`ticked:Label"},
		{"label with one colon", "store/store.go", "My:Label"},
		{"empty path", "", "Movie"},
		{"empty label", "store/store.go", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Neo4jNodeTargetID(tc.path, tc.label); got != "" {
				t.Fatalf("Neo4jNodeTargetID(%q, %q) = %q, want \"\" so the caller emits NO "+
					"edge. Dropping the relationship is correct here because the "+
					"alternative is not an honest dangle — it is a ref that parses as "+
					"Format A and binds to an unrelated entity",
					tc.path, tc.label, got)
			}
		})
	}
}
