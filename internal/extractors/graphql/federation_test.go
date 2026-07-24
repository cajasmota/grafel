package graphql_test

// Issue #3623 (epic #3607) — Apollo Federation extraction tests.
//
// These assert the EXACT federation signal, not len>0:
//   - `type User @key(fields:"id")`  -> User entity federated=true key_fields=id
//   - `extend type Product @key(fields:"id") { reviews }` -> FEDERATES edge to
//     Product carrying key_fields and the @external/@requires/@provides buckets.

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// findSchemaEntity returns the SCOPE.Schema entity named name, or nil.
func findSchemaEntity(entities []types.EntityRecord, name string) *types.EntityRecord {
	for i := range entities {
		if entities[i].Kind == "SCOPE.Schema" && entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

func TestFederation_KeyType_MarkedAsEntity(t *testing.T) {
	src := `type User @key(fields: "id") {
  id: ID!
  name: String!
}
`
	entities := extractGQL(t, "users.graphql", src)
	user := findSchemaEntity(entities, "User")
	if user == nil {
		t.Fatal("User schema entity not emitted")
	}
	if user.Properties["federated"] != "true" {
		t.Errorf("User federated = %q, want \"true\"", user.Properties["federated"])
	}
	if user.Properties["federation"] != "apollo" {
		t.Errorf("User federation = %q, want \"apollo\"", user.Properties["federation"])
	}
	if user.Properties["key_fields"] != "id" {
		t.Errorf("User key_fields = %q, want \"id\"", user.Properties["key_fields"])
	}
}

func TestFederation_CompositeKey(t *testing.T) {
	src := `type Product @key(fields: "id sku") {
  id: ID!
  sku: String!
}
`
	entities := extractGQL(t, "products.graphql", src)
	p := findSchemaEntity(entities, "Product")
	if p == nil {
		t.Fatal("Product schema entity not emitted")
	}
	if p.Properties["key_fields"] != "id sku" {
		t.Errorf("Product key_fields = %q, want \"id sku\"", p.Properties["key_fields"])
	}
}

func TestFederation_NonKeyType_NotFederated(t *testing.T) {
	src := `type Plain {
  id: ID!
}
`
	entities := extractGQL(t, "plain.graphql", src)
	pl := findSchemaEntity(entities, "Plain")
	if pl == nil {
		t.Fatal("Plain schema entity not emitted")
	}
	if pl.Properties["federated"] == "true" {
		t.Errorf("Plain must NOT be marked federated; properties=%v", pl.Properties)
	}
}

func TestFederation_Shareable(t *testing.T) {
	src := `type Money @shareable {
  amount: Int!
  currency: String!
}
`
	entities := extractGQL(t, "money.graphql", src)
	m := findSchemaEntity(entities, "Money")
	if m == nil {
		t.Fatal("Money schema entity not emitted")
	}
	if m.Properties["shareable"] != "true" {
		t.Errorf("Money shareable = %q, want \"true\"", m.Properties["shareable"])
	}
}

func TestFederation_ExtendType_EmitsFederatesEdge(t *testing.T) {
	src := `extend type Product @key(fields: "id") {
  id: ID! @external
  reviews: [Review!]!
}
`
	entities := extractGQL(t, "reviews_subgraph.graphql", src)

	feds := gqlRelsByKind(entities, "FEDERATES")
	if len(feds) != 1 {
		t.Fatalf("want exactly 1 FEDERATES edge, got %d: %+v", len(feds), feds)
	}
	rel := feds[0]
	if rel.ToID != "Product" {
		t.Errorf("FEDERATES ToID = %q, want \"Product\" (the owning entity)", rel.ToID)
	}
	if rel.Properties.Get("federation") != "apollo" {
		t.Errorf("federation = %q, want \"apollo\"", rel.Properties.Get("federation"))
	}
	if rel.Properties.Get("import_kind") != "federation_extend" {
		t.Errorf("import_kind = %q, want \"federation_extend\"", rel.Properties.Get("import_kind"))
	}
	if rel.Properties.Get("key_fields") != "id" {
		t.Errorf("key_fields = %q, want \"id\"", rel.Properties.Get("key_fields"))
	}
	if rel.Properties.Get("external_fields") != "id" {
		t.Errorf("external_fields = %q, want \"id\"", rel.Properties.Get("external_fields"))
	}

	// The legacy IMPORTS edge must still be emitted for back-compat.
	if !hasGQLRel(entities, "IMPORTS", "Product") {
		t.Error("legacy IMPORTS edge to Product must still be emitted")
	}
}

func TestFederation_ExtendType_RequiresAndProvides(t *testing.T) {
	src := `extend type User @key(fields: "id") {
  id: ID! @external
  email: String! @external
  shippingEstimate: Int! @requires(fields: "weight")
  account: Account! @provides(fields: "plan")
}
`
	entities := extractGQL(t, "shipping_subgraph.graphql", src)

	feds := gqlRelsByKind(entities, "FEDERATES")
	if len(feds) != 1 {
		t.Fatalf("want 1 FEDERATES edge, got %d", len(feds))
	}
	p := feds[0].Properties
	if p.Get("external_fields") != "id,email" {
		t.Errorf("external_fields = %q, want \"id,email\"", p.Get("external_fields"))
	}
	if p.Get("requires_fields") != "shippingEstimate" {
		t.Errorf("requires_fields = %q, want \"shippingEstimate\"", p.Get("requires_fields"))
	}
	if p.Get("provides_fields") != "account" {
		t.Errorf("provides_fields = %q, want \"account\"", p.Get("provides_fields"))
	}
}

func TestFederation_PlainExtend_NoFederatesEdge(t *testing.T) {
	// A non-federation `extend type` (no @key, no field directives) keeps the
	// historical IMPORTS-only behaviour and emits no FEDERATES edge.
	src := `extend type Query {
  extraField: String
}
`
	entities := extractGQL(t, "ext.graphql", src)
	if feds := gqlRelsByKind(entities, "FEDERATES"); len(feds) != 0 {
		t.Errorf("plain extend must emit no FEDERATES edge, got %d: %+v", len(feds), feds)
	}
	if !hasGQLRel(entities, "IMPORTS", "Query") {
		t.Error("plain extend must still emit IMPORTS edge to Query")
	}
}
