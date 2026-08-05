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
// (internal/extractor/external_service.go:82-90, ExternalServiceName). What no
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

// TestExternalServiceRefBindsOrDanglesWhenColonSafe6123 is the property the fix
// relies on: a COLON-SAFE canonical ref binds to the service node when one
// exists, and when none exists it is left VERBATIM rather than falling through
// to the byName / kind-hint tiers that produced the mis-bind above. The collider
// is present in both arms, so "dangles" here means "declined a wrong answer that
// was available", not "had nothing to bind to".
//
// THE NAME IS DELIBERATELY CONDITIONAL. An earlier revision called this
// "…ButNeverMisbinds", which was unconditional and FALSE: the guarantee holds
// only while the stub stays under six segments, and adversarial review produced
// a legal docker reference that breaks it. That case is now the last subtest
// here, and the producer-side bound that keeps real refs out of it lives in
// containerServiceRef (internal/custom/csharp/test_doubles.go).
//
// TWO MECHANISMS make the safe case true, and BOTH are asserted, because an
// earlier draft claimed only the first and a mutation disabling it survived:
//
//  1. lookupStructural consumes ANY `scope:`-prefixed stub (handled=true) and
//     returns statusUnmatched for one that is not six segments
//     (internal/resolve/refs.go:2037-2039), so the byName tiers are never
//     reached. Asserted directly in the "structural tier consumes it" subtest,
//     because the end-to-end assertion cannot see this one on its own. This is
//     the LOAD-BEARING one — an inverse mutant changing splitStub to cut at the
//     LAST colon leaves every #6123 assertion passing.
//  2. Redundantly, and only accidentally so: splitStub cuts at the FIRST colon,
//     so the byName probe would be "externalservice:<name>" — a string no entity
//     Name carries. The old `service:<X>` ref had NEITHER protection: not
//     `scope:`-prefixed, and its byName probe is the bare leaf `X`, which real
//     entities do carry.
//
// Mechanism (1) cannot block a LEGITIMATE bind, because the byQualifiedName tier
// runs BEFORE lookupStructural (refs.go:1615) — the "service node present"
// subtest is what pins that ordering.
func TestExternalServiceRefBindsOrDanglesWhenColonSafe6123(t *testing.T) {
	collider := entAt("6666000066660000", "SCOPE.Operation", "postgres", "cs/OrderTests.cs")

	t.Run("structural tier consumes it and declines", func(t *testing.T) {
		idx := BuildIndex([]types.EntityRecord{collider})
		id, status, handled := idx.lookupStructural("scope:externalservice:postgres")
		if !handled {
			t.Fatalf("lookupStructural did not claim `scope:externalservice:postgres` "+
				"(handled=%v) — a non-six-segment `scope:` stub must be consumed there, "+
				"not passed down to the byName / kind-hint tiers", handled)
		}
		if status != statusUnmatched || id != "" {
			t.Fatalf("lookupStructural returned (%q, status=%d), want (\"\", statusUnmatched=%d)",
				id, status, statusUnmatched)
		}
	})

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

	// THE BOUNDARY. Three colons in the name pushes `scope:externalservice:<name>`
	// to six segments, where lookupStructural stops rejecting it and parses it as
	// Format A — parts[4] as a file path, parts[5] as an entity name. This subtest
	// asserts the resolver DOES mis-bind there, which is what makes the
	// producer-side ceiling in containerServiceRef load-bearing rather than
	// defensive decoration. If the resolver is ever taught to distrust an
	// `externalservice` scope-kind, this fails and the producer bound can be
	// revisited.
	t.Run("six segments: the resolver DOES mis-bind, hence the producer bound", func(t *testing.T) {
		// The shape a legal `registry:port/repo:tag@sha256:digest` reference makes.
		const hazard = "scope:externalservice:registry.io:5000/app/pg:15@sha256:deadbeef"
		victim := entAt("8888000088880000", "Class", "deadbeef", "15@sha256")
		idx := BuildIndex([]types.EntityRecord{victim, collider})
		id, status, handled := idx.lookupStructural(hazard)
		if !handled || status != statusRewritten || id != "8888000088880000" {
			t.Fatalf("six-segment external-service stub resolved to (%q, status=%d, handled=%v); "+
				"this test documents that it MIS-BINDS to the entity named parts[5] in the file "+
				"named parts[4]. If the resolver now declines it, containerServiceRef's colon "+
				"ceiling may be relaxed — re-measure before doing so", id, status, handled)
		}
	})
}
