package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6123 — the two resolver facts the C# test-doubles fix depends on.
//
// #6123 was filed as "DEPENDS_ON_SERVICE targets a `service:` node no pass
// creates". Grounding it showed the premise is only half right, and the wrong
// half is the important one: passes DO create `service:<svc>` entities
// (internal/extractor/external_service.go:87, ExternalServiceName). What no
// pass creates is one for a Testcontainers image — and, separately, a
// `service:<X>` ref could never have addressed a service node even where one
// existed, because LookupStatusHint reaches byName through splitStub
// (refs.go:2658), which cuts at the FIRST colon and probes with the REMAINDER.
//
// These two tests pin that pair of behaviours so a later resolver change cannot
// silently take the producer fix's ground away. Neither asserts a count; both
// assert which entity an endpoint lands on.
//
// NOT a test of the fix itself — the behavioural gate for that is
// cmd/grafel.TestCustomExtractorDependsOnServiceDoesNotMisbind6123.

// TestServiceNameRefCannotAddressAServiceNodeAndMisbinds6123 shows why the old
// ref shape was unusable in BOTH directions: the real service node is present
// and is NOT what the ref binds to.
func TestServiceNameRefCannotAddressAServiceNodeAndMisbinds6123(t *testing.T) {
	svc := entAt("5555000055550000", "SCOPE.ExternalService", "service:postgres", "<external-service>")
	svc.QualifiedName = "scope:externalservice:postgres"
	entities := []types.EntityRecord{
		svc,
		// The leaf-name collision: an unrelated call-site operation that merely
		// happens to be called "postgres". This is the shape the C# extractor
		// hit — its collider was SCOPE.Operation "PostgreSqlContainer".
		entAt("6666000066660000", "SCOPE.Operation", "postgres", "cs/OrderTests.cs"),
		// A second same-named entity so the bare name is genuinely ambiguous and
		// the DEPENDS_ON_SERVICE operation-family hint (refs.go:1782) is what
		// picks a winner — exactly the path taken in the field.
		entAt("7777000077770000", "Component", "postgres", "infra/postgres.tf"),
	}
	idx := BuildIndex(entities)
	rels := []types.RelationshipRecord{
		{FromID: "5555000055550000", ToID: "service:postgres", Kind: "DEPENDS_ON_SERVICE"},
	}
	References(rels, idx)

	if rels[0].ToID == "5555000055550000" {
		t.Fatalf("`service:postgres` now binds to the service node by Name — splitStub " +
			"no longer eats the prefix. The #6123 producer fix rests on it doing so; " +
			"re-check internal/custom/csharp/test_doubles.go before relaxing anything here.")
	}
	if rels[0].ToID != "6666000066660000" {
		t.Fatalf("`service:postgres` bound to %q, want the SCOPE.Operation collider "+
			"6666000066660000 — this test is meant to demonstrate the mis-bind, so if the "+
			"shape has changed the demonstration must be rebuilt, not deleted", rels[0].ToID)
	}
}

// TestExternalServiceRefBindsOrDanglesButNeverMisbinds6123 is the property the
// fix relies on: the canonical ref binds to the service node when one exists,
// and when none exists it is left VERBATIM rather than falling through to the
// byName / kind-hint tiers that produced the mis-bind above. The collider is
// present in both arms, so "dangles" here means "declined a wrong answer that
// was available", not "had nothing to bind to".
func TestExternalServiceRefBindsOrDanglesButNeverMisbinds6123(t *testing.T) {
	collider := entAt("6666000066660000", "SCOPE.Operation", "postgres", "cs/OrderTests.cs")

	t.Run("service node present: binds to it", func(t *testing.T) {
		svc := entAt("5555000055550000", "SCOPE.ExternalService", "service:postgres", "<external-service>")
		svc.QualifiedName = "scope:externalservice:postgres"
		idx := BuildIndex([]types.EntityRecord{svc, collider})
		rels := []types.RelationshipRecord{
			{FromID: "6666000066660000", ToID: "scope:externalservice:postgres", Kind: "DEPENDS_ON_SERVICE"},
		}
		References(rels, idx)
		if rels[0].ToID != "5555000055550000" {
			t.Fatalf("canonical ref bound to %q, want the service node 5555000055550000", rels[0].ToID)
		}
	})

	t.Run("no service node: stays verbatim, does not take the collider", func(t *testing.T) {
		idx := BuildIndex([]types.EntityRecord{collider})
		rels := []types.RelationshipRecord{
			{FromID: "6666000066660000", ToID: "scope:externalservice:postgres", Kind: "DEPENDS_ON_SERVICE"},
		}
		References(rels, idx)
		if rels[0].ToID != "scope:externalservice:postgres" {
			t.Fatalf("canonical ref was rewritten to %q; it must be left verbatim so the edge "+
				"dangles honestly. lookupStructural (refs.go:2037-2039) is supposed to consume "+
				"any `scope:` stub that is not six segments and return statusUnmatched without "+
				"consulting byName", rels[0].ToID)
		}
	})
}
